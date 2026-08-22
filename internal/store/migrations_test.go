package store_test

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/store/testdb"
	"github.com/konkov/agile/migrations"
)

// Схема меняется только миграцией, вперёд и без правок задним числом —
// значит, цепочка обязана применяться с нуля. До 22 августа 2026 это
// не проверялось ничем: `make check` идёт по накопленной базе разработки,
// где всё уже применено, а прогон с чистой базы был обязательным шагом,
// который держался в голове. Дважды он ловил то, чего на базе с данными
// не видно, — и оба раза потому, что кто-то о нём вспомнил.
//
// Проверка заводит свою базу, применяет в ней всю цепочку под ролью
// приложения и убирает базу за собой. Под ролью приложения, а не под той,
// что базу завела: миграции трогают политики и force RLS, а для владельца
// сервера политики не действуют — прогон суперпользователем проверял бы
// не ту цепочку, что применяется на самом деле.
//
// Сравнение с накопленной базой здесь же и по той же причине: цепочка,
// применяющаяся без ошибок, всё ещё может давать не ту схему, что стоит
// у людей, — и разойтись эти две вещи могут молча.
func TestMigrationChainAppliesToAnEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	fresh := freshDatabase(t)

	db, err := store.Open(ctx, fresh)
	if err != nil {
		t.Fatalf("подключение к чистой базе: %v", err)
	}
	defer db.Close()

	applied, err := db.Migrate(ctx)
	if err != nil {
		t.Fatalf("цепочка не применилась с нуля: %v\n"+
			"Применилось до отказа: %d из %d", err, len(applied), countMigrations(t))
	}
	if want := countMigrations(t); len(applied) != want {
		t.Errorf("применилось %d миграций из %d — цепочка неполна", len(applied), want)
	}

	// Второй прогон обязан не делать ничего: миграции применяются
	// по отметке, и повторное применение означало бы, что отметка
	// не ставится, — на живой базе это заметили бы уже отказом.
	again, err := db.Migrate(ctx)
	if err != nil {
		t.Fatalf("повторный прогон миграций: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("повторный прогон применил %v — миграции не отмечаются применёнными", again)
	}

	// Изоляция обязана держаться и на свежей базе: политики ставятся
	// миграциями, и забытая в конце цепочки таблица здесь видна так же,
	// как на накопленной.
	if err := db.EnsureTenantIsolation(ctx); err != nil {
		t.Errorf("свежая база не проходит проверку изоляции: %v", err)
	}

	accumulated := testdb.Open(t)
	compareSchemas(t, ctx, accumulated, db)
}

// compareSchemas сверяет схему, выросшую из цепочки с нуля, со схемой
// накопленной базы. Расхождение значит одно из двух: либо накопленную
// когда-то правили руками мимо миграции, либо миграция написана так,
// что на пустой базе даёт не то же самое. Оба случая надо увидеть
// проверкой, а не на чужой установке.
func compareSchemas(t *testing.T, ctx context.Context, accumulated, fresh *store.Store) {
	t.Helper()
	for _, part := range schemaQueries {
		want := schemaRows(t, ctx, accumulated, part.sql)
		got := schemaRows(t, ctx, fresh, part.sql)
		if diff := firstDifference(want, got); diff != "" {
			t.Errorf("%s: схема с нуля расходится с накопленной.\n%s", part.name, diff)
		}
	}
}

var schemaQueries = []struct {
	name string
	sql  string
}{
	// Читается pg_catalog, а не information_schema: та показывает только
	// то, на что у текущей роли есть права, и таблица, заведённая мимо
	// миграции другой ролью, в ней просто не видна. Проверка, слепая
	// ровно к тому случаю, ради которого написана, — хуже отсутствующей;
	// первая версия этой была именно такой и молчала на подложенной
	// таблице, пока её не выдали индекс и ограничение.
	{"таблицы", `
		select c.relkind::text || ' ' || c.relname
		  from pg_class c join pg_namespace n on n.oid = c.relnamespace
		 where n.nspname = 'public' and c.relkind in ('r', 'v', 'm', 'p', 'S')
		 order by 1`},
	{"столбцы", `
		select c.relname || '.' || a.attname || ' ' ||
		       format_type(a.atttypid, a.atttypmod) ||
		       ' notnull=' || a.attnotnull::text ||
		       ' default=' || coalesce(pg_get_expr(d.adbin, d.adrelid), '—')
		  from pg_attribute a
		  join pg_class c on c.oid = a.attrelid
		  join pg_namespace n on n.oid = c.relnamespace
		  left join pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
		 where n.nspname = 'public' and a.attnum > 0 and not a.attisdropped
		   and c.relkind in ('r', 'v', 'm', 'p')
		 order by 1`},
	{"ограничения", `
		select c.conrelid::regclass::text || ' ' || c.conname || ' ' ||
		       pg_get_constraintdef(c.oid)
		  from pg_constraint c
		  join pg_namespace n on n.oid = c.connamespace
		 where n.nspname = 'public'
		 order by 1`},
	{"индексы", `
		select indexdef from pg_indexes where schemaname = 'public' order by 1`},
	{"политики", `
		select schemaname || '.' || tablename || ' ' || policyname || ' ' ||
		       cmd || ' ' || coalesce(qual, '—') || ' ' || coalesce(with_check, '—')
		  from pg_policies where schemaname = 'public' order by 1`},
	{"функции и триггеры", `
		select tgrelid::regclass::text || ' ' || tgname || ' ' ||
		       pg_get_triggerdef(oid)
		  from pg_trigger where not tgisinternal
		 order by 1`},
}

func schemaRows(t *testing.T, ctx context.Context, db *store.Store, sql string) []string {
	t.Helper()
	rows, err := db.Pool.Query(ctx, sql)
	if err != nil {
		t.Fatalf("чтение схемы: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("чтение схемы: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("чтение схемы: %v", err)
	}
	return out
}

// firstDifference называет первое расхождение, а не вываливает оба списка:
// схема — это тысячи строк, и различие в одной утонет в них без остатка.
func firstDifference(want, got []string) string {
	inGot := map[string]bool{}
	for _, s := range got {
		inGot[s] = true
	}
	inWant := map[string]bool{}
	for _, s := range want {
		inWant[s] = true
	}
	var missing, extra []string
	for _, s := range want {
		if !inGot[s] {
			missing = append(missing, s)
		}
	}
	for _, s := range got {
		if !inWant[s] {
			extra = append(extra, s)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	var b strings.Builder
	if len(missing) > 0 {
		fmt.Fprintf(&b, "есть в накопленной, нет с нуля (%d шт.):\n  %s\n",
			len(missing), strings.Join(head(missing), "\n  "))
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "есть с нуля, нет в накопленной (%d шт.):\n  %s\n",
			len(extra), strings.Join(head(extra), "\n  "))
	}
	return b.String()
}

// head показывает пять строк из списка: расхождение в схеме чаще всего
// одно, а если их полтораста — читать надо не вывод теста, а миграцию.
func head(list []string) []string {
	if len(list) > 5 {
		return append(list[:5:5], fmt.Sprintf("… и ещё %d", len(list)-5))
	}
	return list
}

// freshDatabase заводит пустую базу под ролью приложения и убирает её
// после теста. Имя случайное: параллельные прогоны не должны драться
// за одну базу, а оставшаяся от прошлого падения — молча превращать
// проверку «с нуля» в проверку «с прошлого раза».
func freshDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	appURL := testdb.URL(t)
	adminURL := testdb.AdminURL(t)

	owner, err := testdb.UserOf(appURL)
	if err != nil || owner == "" {
		t.Fatalf("в TEST_DATABASE_URL не разобрать роль: %v", err)
	}

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("подключение под ролью, заводящей базы: %v", err)
	}
	defer admin.Close(ctx)

	name := "board_chain_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err := admin.Exec(ctx,
		`create database "`+name+`" owner "`+owner+`"`); err != nil {
		t.Fatalf("создание базы под проверку: %v", err)
	}
	t.Cleanup(func() {
		drop, err := pgx.Connect(context.Background(), adminURL)
		if err != nil {
			t.Logf("база %s осталась: %v", name, err)
			return
		}
		defer drop.Close(context.Background())
		if _, err := drop.Exec(context.Background(),
			`drop database if exists "`+name+`" with (force)`); err != nil {
			t.Logf("база %s осталась: %v", name, err)
		}
	})

	fresh, err := testdb.WithDatabase(appURL, name)
	if err != nil {
		t.Fatalf("подстановка имени базы: %v", err)
	}
	return fresh
}

func countMigrations(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("чтение каталога миграций: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}
