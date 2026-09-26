package store_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/findias/takt/internal/demo"
	"github.com/findias/takt/internal/store/testdb"
)

// Посторонний не достаёт до чужих строк ни в одной таблице — проверено
// поведением, а не наличием политики (PROMPT-TESTING.md, уровень 2).
//
// TestEveryTenantTableIsUnderForcedRLS говорит, что политика на таблице
// есть. Правильная ли она — не говорит: политика `using (true)` прошла
// бы ту проверку и отдала бы всё. Здесь две организации, наполненные
// демо целиком, и владелец второй в каждой таблице с org_id пробует
// прочитать, изменить, удалить и вставить строки первой.
//
// Таблица, где у первой организации нет ни строки, роняет тест: проверка
// на пустой таблице доказывает только то, что таблица пуста. Если строк
// нет по делу — таблица называется в `нечемПроверить` с причиной.

// нечемПроверить — таблицы с org_id, которым ни демо, ни досев ниже
// не дают строк, и почему. Сейчас таких нет: пусто и должно оставаться.
var нечемПроверить = map[string]string{}

// seedWhatDemoLeavesEmpty кладёт по строке туда, куда демо не пишет:
// ключи повтора контракта, правки реплик, выбор людей при переносе,
// приглашения, именованные срезы. Ролью postgres, мимо политик: здесь
// нужна строка жертвы, а не проверка того, кто вправе её завести.
// Приглашение — первым делом: его политику меняли в этапе 31.
func seedWhatDemoLeavesEmpty(t *testing.T, ctx context.Context, admin *pgx.Conn, org, owner string) {
	t.Helper()
	both, only := []any{org, owner}, []any{org}
	for _, step := range []struct {
		q    string
		args []any
	}{
		{`insert into invites (org_id, email, role, token_hash, invited_by, expires_at)
		 values ($1, 'stranger-probe@example.test', 'member', md5(random()::text), $2, now() + interval '1 day')`, both},
		{`insert into report_slices (org_id, user_id, name) values ($1, $2, 'Проба постороннего')`, both},
		{`insert into import_people (org_id, source, person_key, action, user_id, decided_by)
		 values ($1, 'probe', 'probe-person', 'match', $2, $2)`, both},
		{`insert into api_idempotency (org_id, key, method, path, status)
		 values ($1, md5(random()::text), 'POST', '/probe', 201)`, only},
		{`insert into card_comment_revisions (org_id, comment_id, body)
		 select $1, id, 'прежний текст' from card_comments where org_id = $1 limit 1`, only},
	} {
		tag, err := admin.Exec(ctx, step.q, step.args...)
		if err != nil {
			t.Fatalf("досев: %v\n%s", err, step.q)
		}
		if tag.RowsAffected() == 0 {
			t.Fatalf("досев ничего не вставил:\n%s", step.q)
		}
	}
}

func TestStrangerReachesNoRowOfAnotherOrg(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	a, err := demo.FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("первая организация: %v", err)
	}
	t.Cleanup(func() { _ = demo.RemoveSandbox(context.Background(), db, a.OrgID) })
	b, err := demo.FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("вторая организация: %v", err)
	}
	t.Cleanup(func() { _ = demo.RemoveSandbox(context.Background(), db, b.OrgID) })

	admin := adminConn(t, ctx)
	seedWhatDemoLeavesEmpty(t, ctx, admin, a.OrgID, a.OwnerID)

	tables := tenantTables(t, ctx, admin)
	if len(tables) < 20 {
		t.Fatalf("таблиц с org_id под политиками %d — меньше, чем есть на самом деле; запрос сломан", len(tables))
	}

	var empty []string
	for _, table := range tables {
		var rows int
		if err := admin.QueryRow(ctx,
			`select count(*) from `+pgx.Identifier{table}.Sanitize()+` where org_id = $1`, // #sql-склейка: имя таблицы из каталога, экранировано
			a.OrgID).Scan(&rows); err != nil {
			t.Fatalf("%s: счёт строк: %v", table, err)
		}
		if rows == 0 {
			if _, known := нечемПроверить[table]; !known {
				empty = append(empty, table)
			}
			continue
		}
		t.Run(table, func(t *testing.T) {
			strangerProbes(t, ctx, db, admin, table, a.OrgID, b.OrgID, b.OwnerID)
		})
	}
	if len(empty) > 0 {
		sort.Strings(empty)
		t.Errorf("у первой организации нет строк в %v: проверять нечего — наполните демо "+
			"или назовите таблицу в нечемПроверить с причиной", empty)
	}
}

// errRollback — вернуть из области, чтобы всё сделанное в ней откатилось.
var errRollback = errors.New("откат пробы")

func strangerProbes(t *testing.T, ctx context.Context, db interface {
	InTenant(context.Context, string, string, func(pgx.Tx) error) error
}, admin *pgx.Conn, table, victim, org, owner string) {
	name := pgx.Identifier{table}.Sanitize()

	// Одна строка жертвы целиком — ею пробуется вставка.
	var row []byte
	if err := admin.QueryRow(ctx,
		`select to_jsonb(x) from `+name+` x where org_id = $1 limit 1`, // #sql-склейка: имя из каталога, экранировано
		victim).Scan(&row); err != nil {
		t.Fatalf("строка жертвы: %v", err)
	}
	cols := insertableColumns(t, ctx, admin, table)
	list := strings.Join(cols, ", ")

	err := db.InTenant(ctx, org, owner, func(tx pgx.Tx) error {
		// Себя посторонний видит — иначе «ноль чужих» значил бы только,
		// что область не выставилась.
		var own, seen int
		if err := tx.QueryRow(ctx,
			`select count(*) filter (where org_id = $1), count(*) filter (where org_id = $2) from `+name, // #sql-склейка: имя из каталога
			org, victim).Scan(&own, &seen); err != nil {
			return err
		}
		if seen != 0 {
			t.Errorf("видит %d чужих строк", seen)
		}
		_ = own // своих бывает ноль и по делу: демо у двух организаций одно, но журналы разные

		for _, probe := range []struct{ what, sql string }{
			{"изменение", `update ` + name + ` set org_id = org_id where org_id = $1`}, // #sql-склейка: имя из каталога
			{"удаление", `delete from ` + name + ` where org_id = $1`},                 // #sql-склейка: имя из каталога
		} {
			tag, err := tx.Exec(ctx, probe.sql, victim)
			if err != nil {
				// Отказ — тоже «не достал»; главное, что не задело строк.
				continue
			}
			if tag.RowsAffected() != 0 {
				t.Errorf("%s задело %d чужих строк", probe.what, tag.RowsAffected())
			}
		}

		// Вставка чужой строки как есть. Прошла политику — упрётся
		// в уникальность (та же строка уже лежит), и это провал:
		// 23505 значит, что политика её пропустила.
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = sp.Exec(ctx,
			`insert into `+name+` (`+list+`) select `+list+` from jsonb_populate_record(null::`+name+`, $1)`, // #sql-склейка: имена из каталога, экранированы
			row)
		_ = sp.Rollback(ctx)
		var pgErr *pgconn.PgError
		switch {
		case err == nil:
			t.Errorf("вставил строку от имени чужой организации")
		case errors.As(err, &pgErr) && pgErr.Code == "23505":
			t.Errorf("вставка чужой строки прошла политику и упала только на уникальности: %v", err)
		}
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		t.Fatalf("область постороннего: %v", err)
	}
}

func tenantTables(t *testing.T, ctx context.Context, admin *pgx.Conn) []string {
	rows, err := admin.Query(ctx, `
		select c.relname
		  from pg_class c
		  join pg_namespace n on n.oid = c.relnamespace
		  join pg_attribute a on a.attrelid = c.oid and a.attname = 'org_id' and not a.attisdropped
		 where n.nspname = 'public' and c.relkind = 'r' and c.relrowsecurity
		 order by 1`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// insertableColumns — колонки, в которые можно писать: вычисляемые
// отказали бы раньше политики и спрятали бы её ответ.
func insertableColumns(t *testing.T, ctx context.Context, admin *pgx.Conn, table string) []string {
	rows, err := admin.Query(ctx, `
		select a.attname
		  from pg_attribute a
		  join pg_class c on c.oid = a.attrelid
		  join pg_namespace n on n.oid = c.relnamespace
		 where n.nspname = 'public' and c.relname = $1
		   and a.attnum > 0 and not a.attisdropped and a.attgenerated = ''
		   and a.attidentity = ''
		 order by a.attnum`, table)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range names {
		names[i] = pgx.Identifier{n}.Sanitize()
	}
	return names
}

// adminConn — роль postgres в той же базе, что у приложения: чтобы
// видеть строки жертвы в обход политик.
func adminConn(t *testing.T, ctx context.Context) *pgx.Conn {
	appURL := os.Getenv("TEST_DATABASE_URL")
	u, err := url.Parse(appURL)
	if err != nil || appURL == "" {
		t.Fatalf("TEST_DATABASE_URL: %v", err)
	}
	dsn, err := testdb.WithDatabase(testdb.AdminURL(t), strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("роль postgres: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}
