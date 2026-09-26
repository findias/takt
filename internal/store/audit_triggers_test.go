package store_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/demo"
	"github.com/findias/takt/internal/store/testdb"
)

// Журнал действий: каждый триггер пишет, секреты не пишутся, и каждую
// запись экран умеет назвать (PROMPT-TESTING.md, уровень 2).
//
// Три дыры, которые не видит никто, кроме этой проверки. Триггер,
// объявленный, но не срабатывающий (не та колонка в аргументе, не то
// событие), молчит. Новое поле с секретом уезжает в payload целиком:
// audit_write вычитает только известные имена. И запись о таблице,
// которой нет в подписях клиента, показывается названием таблицы —
// так было бы с team_admins, у которой до этапа 31 не было даже
// триггера.

// секреты — имена полей, которых в журнале быть не должно ни на каком
// уровне вложенности.
var секреты = regexp.MustCompile(`"(token_hash|password_hash|secret|secret_hash|client_secret|api_key|token)"\s*:`)

// пишутКодом — записи журнала, которые делает код, а не триггер:
// у них нет таблицы-источника с org_id или нужна причина словами.
var пишутКодом = []string{"users", "password_links", "export"}

func TestAuditTriggersWriteNamedAndSecretFreeEntries(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	box, err := demo.FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("организация: %v", err)
	}
	t.Cleanup(func() { _ = demo.RemoveSandbox(context.Background(), db, box.OrgID) })

	admin := adminConn(t, ctx)
	seedWhatDemoLeavesEmpty(t, ctx, admin, box.OrgID, box.OwnerID)

	audited := auditedTables(t, ctx, admin)
	if len(audited) < 8 {
		t.Fatalf("таблиц под журналом %d — меньше известного; запрос сломан", len(audited))
	}

	// 1. Каждый триггер срабатывает: у наполненной организации есть
	// запись о каждой таблице под журналом.
	for _, table := range audited {
		var n int
		if err := admin.QueryRow(ctx,
			`select count(*) from audit_events where org_id = $1 and subject = $2`,
			box.OrgID, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 && table != "cards" {
			// cards пишет только удаление, а демо карточек не удаляет:
			// его проверяет board/delete_test.
			t.Errorf("%s под журналом, но записей о ней нет: триггер не срабатывает", table)
		}
	}

	// 2. Секретов нет нигде в payload.
	rows, err := admin.Query(ctx,
		`select subject, payload::text from audit_events where org_id = $1`, box.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var subject, payload string
		if err := rows.Scan(&subject, &payload); err != nil {
			t.Fatal(err)
		}
		if m := секреты.FindString(payload); m != "" {
			t.Errorf("в журнал о %s попало поле %s", subject, m)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// 3. Каждую запись экран называет словами, в обоих языках.
	want := append(append([]string{}, audited...), пишутКодом...)
	for _, lang := range []string{"ru", "en"} {
		named := feedSubjects(t, lang)
		var missing []string
		for _, table := range want {
			if !named[table] {
				missing = append(missing, table)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("в подписях журнала (%s.ts, feed.subjects) нет %v: запись покажется названием таблицы", lang, missing)
		}
	}
}

func auditedTables(t *testing.T, ctx context.Context, admin *pgx.Conn) []string {
	rows, err := admin.Query(ctx, `
		select distinct c.relname
		  from pg_trigger tr
		  join pg_class c on c.oid = tr.tgrelid
		  join pg_proc p on p.oid = tr.tgfoid
		 where not tr.tgisinternal and p.proname like 'audit%'
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

// feedSubjects читает ключи feed.subjects из каталога подписей клиента.
// Разбор текстом, а не исполнением: каталог — TypeScript, а проверке
// нужен только список ключей внутри одного объекта.
func feedSubjects(t *testing.T, lang string) map[string]bool {
	_, self, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(self), "..", "..", "web", "src", "shared", "i18n", lang+".ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("каталог подписей: %v", err)
	}
	block := regexp.MustCompile(`(?s)subjects:\s*\{(.*?)\}`).FindSubmatch(raw)
	if block == nil {
		t.Fatalf("%s: не найден feed.subjects", path)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*([a-z_]+):`).FindAllSubmatch(block[1], -1) {
		out[string(m[1])] = true
	}
	if len(out) < 5 {
		t.Fatalf("%s: в feed.subjects нашлось %d ключей — разбор сломан", path, len(out))
	}
	return out
}
