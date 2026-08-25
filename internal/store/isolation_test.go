package store_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/store/testdb"
)

// Изоляция организаций держится политиками базы, а не проверками в коде.
// Значит, таблица, забывшая политики, ломает главное обещание проекта
// и при этом не ломает ничего больше: запросы работают, просто отдают
// лишнее. Проверено разбором стратегии проверок — таблица с org_id
// и без политик отдаёт чужую строку запросу, у которого арендатор
// выставлен как положено, тем же путём, которым ходит любой запрос
// человека.
//
// EnsureTenantIsolation на старте стережёт другое: что роль подключения
// не обходит политики сама. Таблицы не смотрит никто, кроме этой
// проверки.
func TestEveryTenantTableIsUnderForcedRLS(t *testing.T) {
	// Таблицы личности под RLS не попадают намеренно (миграция 0002):
	// к ним обращаются до того, как известна организация, — на входе,
	// при проверке сессии и при выборе активной организации. Их запросы
	// ограничены конкретным user_id, и эта граница проверяется в коде.
	// Список исключений короткий и закрытый: новая строка здесь —
	// это решение, а не недосмотр.
	exceptions := []string{"memberships", "orgs", "sessions", "users", "schema_migrations"}

	db := isolationStore(t)
	var bare []string
	err := db.InScope(context.Background(), store.Scope{}, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `
			select c.relname
			  from pg_class c
			  join pg_namespace n on n.oid = c.relnamespace
			 where n.nspname = 'public' and c.relkind = 'r'
			   and not (c.relrowsecurity and c.relforcerowsecurity)
			 order by 1`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			if !slices.Contains(exceptions, name) {
				bare = append(bare, name)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("перечень таблиц: %v", err)
	}
	if len(bare) > 0 {
		t.Errorf("таблицы без включённого и принудительного RLS: %v;\n"+
			"такая таблица отдаёт чужие строки запросу с правильно выставленным арендатором. "+
			"Включите row level security и force row level security миграцией и опишите политики; "+
			"если таблица не арендаторская — внесите её в список исключений здесь, с объяснением",
			bare)
	}
}

func isolationStore(t *testing.T) *store.Store {
	t.Helper()
	return testdb.Shared(t)
}
