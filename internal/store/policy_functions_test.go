package store_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
)

// Функция, которая читает таблицу, пишется на plpgsql, а не на sql.
//
// Цена этого правила замерена 21.09.2026 (миграция 0054): SQL-функции
// с агрегатом внутри PostgreSQL не встраивает и строит план их тела
// заново при каждом вызове. Функции политик вызываются из каждого
// запроса к каждой таблице с политикой — и снимок доски тратил 71 мс
// на планирование одного и того же «какие доски видит человек».
// У plpgsql план кешируется на соединение: 13 мс на тот же снимок.
//
// Функции, читающие одни настройки (`app_current_org()`), остаются
// на sql: их встраивают в запрос, и это быстрее кеша. Поэтому
// проверяется не язык вообще, а язык функции, в теле которой есть
// таблица.
func TestPolicyHelpersCachePlans(t *testing.T) {
	// Исключения — решения, а не недосмотр: здесь названо, почему.
	exceptions := []string{
		// Вызывается триггером при смене роли в организации — раз
		// на правку состава, а не на каждый запрос.
		"app_other_person_owners",
	}

	db := isolationStore(t)
	var slow []string
	err := db.InScope(context.Background(), store.Scope{}, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `
			select p.proname
			  from pg_proc p
			  join pg_namespace n on n.oid = p.pronamespace
			  join pg_language l on l.oid = p.prolang
			 where n.nspname = 'public'
			   and l.lanname = 'sql'
			   and p.prosrc ~* '\m(from|join)\M'
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
				slow = append(slow, name)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(slow) > 0 {
		t.Errorf("SQL-функции читают таблицы и планируются на каждом вызове: %v — "+
			"перепишите на plpgsql (образец — миграция 0054) или объясните исключение здесь", slow)
	}
}
