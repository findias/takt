package demo

import (
	"context"
	"testing"
	"time"

	"github.com/findias/takt/internal/org"
	"github.com/findias/takt/internal/store/testdb"
)

// Песочница публичного демо (ROADMAP 30.1): посетитель получает свою
// организацию с теми же данными, что стенд, и через срок теряет её
// целиком — вместе с людьми, журналом и всем остальным.

func TestSandboxShowsWhatTheStandShows(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	box, err := FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("песочница: %v", err)
	}
	t.Cleanup(func() {
		_ = RemoveSandbox(context.Background(), db, box.OrgID)
	})

	if err := VerifyOrg(ctx, db, box.OrgID, box.OwnerID); err != nil {
		t.Errorf("песочница показывает не то, что стенд: %v", err)
	}

	// Люди песочницы — только её: почты свои, и все привязаны к ней.
	// Служебная личность ключа интеграции — не человек наполнения,
	// её уносит второе правило уборки (см. следующую проверку).
	var people, bound int
	if err := db.Pool.QueryRow(ctx, `
		select count(*), count(*) filter (where u.sandbox_org_id = $1)
		  from memberships m join users u on u.id = m.user_id
		 where m.org_id = $1 and u.kind = 'person'`, box.OrgID).Scan(&people, &bound); err != nil {
		t.Fatal(err)
	}
	if people != len(People) || bound != people {
		t.Errorf("людей %d, привязано к песочнице %d, ожидалось %d и %d", people, bound, len(People), len(People))
	}

	// Вторая песочница рядом не спорит с первой ни за почты, ни за адрес.
	second, err := FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("вторая песочница: %v", err)
	}
	t.Cleanup(func() {
		_ = RemoveSandbox(context.Background(), db, second.OrgID)
	})
}

func TestSweepRemovesExpiredSandboxesOnly(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	expired, err := FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("песочница: %v", err)
	}
	fresh, err := FillSandbox(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("песочница: %v", err)
	}
	t.Cleanup(func() {
		_ = RemoveSandbox(context.Background(), db, expired.OrgID)
		_ = RemoveSandbox(context.Background(), db, fresh.OrgID)
	})
	if _, err := db.Pool.Exec(ctx,
		`update orgs set sandbox_expires_at = now() - interval '1 minute' where id = $1`,
		expired.OrgID); err != nil {
		t.Fatal(err)
	}

	// Все, кто в ней состоял, — и заведённые наполнением, и служебная
	// личность ключа, которую наполнение к песочнице не привязывало.
	var members []string
	if err := db.Pool.QueryRow(ctx,
		`select array_agg(user_id) from memberships where org_id = $1`, expired.OrgID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if len(members) <= len(People) {
		t.Fatalf("в песочнице %d участников — служебной личности ключа нет, проверять нечего", len(members))
	}

	// Настоящая организация рядом — своя, а не общий счёт: проверки
	// других пакетов идут параллельно по той же базе и сами убирают
	// свои организации, так что счёт «сколько было» плавает.
	var realOwner string
	if err := db.Pool.QueryRow(ctx, `
		insert into users (email, name, password_hash) values ($1, 'Настоящий', '!') returning id`,
		"real-"+expired.OrgID[:8]+"@example.test").Scan(&realOwner); err != nil {
		t.Fatal(err)
	}
	real, err := org.New(db).Create(ctx, "Настоящая рядом с песочницей", realOwner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, real.OrgID)
		_, _ = db.Pool.Exec(context.Background(), `delete from users where id = $1`, realOwner)
	})

	if _, err := SweepSandboxes(ctx, db); err != nil {
		t.Fatalf("уборка: %v", err)
	}

	var left, users int
	if err := db.Pool.QueryRow(ctx, `select count(*) from orgs where id = $1`, expired.OrgID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `select count(*) from users where id = any($1)`, members).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if left != 0 || users != 0 {
		t.Errorf("истёкшая песочница осталась: организаций %d, людей %d (в том числе служебных)", left, users)
	}

	if err := db.Pool.QueryRow(ctx, `select count(*) from orgs where id = $1`, fresh.OrgID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Error("уборка унесла песочницу, у которой срок не вышел")
	}

	if err := db.Pool.QueryRow(ctx, `select count(*) from orgs where id = $1`, real.OrgID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Error("уборка удалила настоящую организацию")
	}
}
