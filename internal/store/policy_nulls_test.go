package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/demo"
	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/store/testdb"
)

// Функция политик не отвечает «неизвестно» (PROMPT-TESTING.md, уровень 2).
//
// В условии политики NULL ведёт себя как «ложь», и потому незаметен.
// Но стоит ему оказаться под `not (...)` — внутри функции или в чужой
// политике, — и «неизвестно» становится пропуском: так 26.09 владелец
// корневого узла выпадал из запрета в app_can_erase, потому что у корня
// пустой родитель. Правило проверяется на всех функциях каталога сразу:
// новая функция без строчки здесь попадает под него сама.
//
// Каждая функция app_*, отвечающая boolean или uuid[], вызывается
// с NULL во всех аргументах в трёх областях: без организации и человека
// (служебная задача), рядовым участником и владельцем. Ответ обязан
// быть не NULL.
func TestPolicyFunctionsNeverAnswerUnknown(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	box, err := demo.FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("организация: %v", err)
	}
	t.Cleanup(func() { _ = demo.RemoveSandbox(context.Background(), db, box.OrgID) })

	admin := adminConn(t, ctx)
	var member string
	if err := admin.QueryRow(ctx, `
		select user_id from memberships where org_id = $1 and role = 'member' limit 1`,
		box.OrgID).Scan(&member); err != nil {
		t.Fatalf("участник: %v", err)
	}

	calls := policyCalls(t, ctx, admin)
	if len(calls) < 10 {
		t.Fatalf("функций политик нашлось %d — меньше известного; запрос сломан", len(calls))
	}

	scopes := []struct {
		name  string
		scope store.Scope
	}{
		{"без организации и человека", store.Scope{}},
		{"рядовой участник", store.Scope{OrgID: box.OrgID, UserID: member}},
		{"владелец", store.Scope{OrgID: box.OrgID, UserID: box.OwnerID}},
	}
	for _, sc := range scopes {
		err := db.InScope(ctx, sc.scope, func(tx pgx.Tx) error {
			for _, call := range calls {
				var isNull bool
				// #sql-склейка: имя функции и типы аргументов взяты из каталога pg_proc
				if err := tx.QueryRow(ctx, `select (`+call+`) is null`).Scan(&isNull); err != nil {
					t.Errorf("%s: %s: %v", sc.name, call, err)
					continue
				}
				if isNull {
					t.Errorf("%s: %s отвечает NULL — под not() это станет пропуском", sc.name, call)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", sc.name, err)
		}
	}
}

// policyCalls строит вызов каждой функции app_* с NULL нужного типа
// в каждом аргументе.
func policyCalls(t *testing.T, ctx context.Context, admin *pgx.Conn) []string {
	rows, err := admin.Query(ctx, `
		select p.proname,
		       coalesce(array_agg(format_type(a.t, null) order by a.n)
		                filter (where a.t is not null), '{}')
		  from pg_proc p
		  join pg_namespace ns on ns.oid = p.pronamespace
		  left join lateral unnest(p.proargtypes::oid[]) with ordinality as a(t, n) on true
		 where ns.nspname = 'public' and p.proname like 'app\_%'
		   and p.prorettype in ('boolean'::regtype, 'uuid[]'::regtype)
		 group by p.oid, p.proname
		 order by 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		var types []string
		if err := rows.Scan(&name, &types); err != nil {
			t.Fatal(err)
		}
		args := make([]string, len(types))
		for i, typ := range types {
			args[i] = fmt.Sprintf("null::%s", typ)
		}
		out = append(out, pgx.Identifier{name}.Sanitize()+"("+strings.Join(args, ", ")+")")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
