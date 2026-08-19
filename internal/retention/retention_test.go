package retention

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/store/testdb"
)

// Уборка. Проверяется не «удалилось», а что удалилось именно то и только
// то: несдавшаяся доставка и журнал без назначенного срока обязаны
// пережить любое число проходов.

type fixture struct {
	db     *store.Store
	worker *Worker
	ctx    context.Context
	t      *testing.T
	orgID  string
	userID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	url := testdb.URL(t)
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("подключение к тестовой базе: %v", err)
	}
	t.Cleanup(db.Close)

	f := &fixture{
		db:     db,
		worker: NewWorker(db, slog.New(slog.NewTextHandler(io.Discard, nil))),
		ctx:    ctx,
		t:      t,
	}
	suffix := uuid.NewString()
	if err := db.Pool.QueryRow(ctx,
		`insert into orgs (name, slug) values ($1, $2) returning id`,
		"Уборка "+suffix[:8], "clean-"+suffix[:8]).Scan(&f.orgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, f.orgID)
	})
	if err := db.Pool.QueryRow(ctx, `
		insert into users (email, name, password_hash)
		values ($1, 'Тестовый', 'x') returning id`,
		suffix+"@example.test").Scan(&f.userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from users where id = $1`, f.userID)
	})
	if _, err := db.Pool.Exec(ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		f.orgID, f.userID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if err := f.db.InTenant(f.ctx, f.orgID, f.userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, sql, args...)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.InTenant(f.ctx, f.orgID, f.userID, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, sql, args...).Scan(&n)
	}); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// Ключ повтора нужен, чтобы пережить обрыв связи и повтор через минуту.
// Недельной давности ключ повторять уже некому.
func TestStaleIdempotencyKeysAreRemoved(t *testing.T) {
	f := newFixture(t)
	f.exec(`
		insert into api_idempotency (org_id, key, method, path, status, body, created_at)
		values ($1, $2, 'POST', '/api/boards', 201, '{}', now() - interval '2 days'),
		       ($1, $3, 'POST', '/api/boards', 201, '{}', now())`,
		f.orgID, "старый-"+uuid.NewString(), "свежий-"+uuid.NewString())

	if err := f.worker.Once(f.ctx); err != nil {
		t.Fatal(err)
	}

	if got := f.count(`select count(*) from api_idempotency`); got != 1 {
		t.Errorf("ключей осталось %d, ожидался один свежий", got)
	}
}

// Несдавшаяся доставка — единственный след того, что не доехало.
// Вычистить её значит потерять его.
func TestOnlyDeliveredWebhooksAreForgotten(t *testing.T) {
	f := newFixture(t)
	var hookID string
	if err := f.db.InTenant(f.ctx, f.orgID, f.userID, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			insert into webhooks (org_id, name, url, secret, events)
			values ($1, 'Проба', 'http://example.test', 'секрет', array['card.created'])
			returning id`, f.orgID).Scan(&hookID)
	}); err != nil {
		t.Fatal(err)
	}

	f.exec(`
		insert into webhook_deliveries (org_id, webhook_id, event, payload, delivered_at, created_at)
		values ($1, $2, 'card.created', '{}', now() - interval '60 days', now() - interval '60 days')`,
		f.orgID, hookID)
	f.exec(`
		insert into webhook_deliveries (org_id, webhook_id, event, payload, failed_at, created_at)
		values ($1, $2, 'card.created', '{}', now() - interval '60 days', now() - interval '60 days')`,
		f.orgID, hookID)
	f.exec(`
		insert into webhook_deliveries (org_id, webhook_id, event, payload, delivered_at, created_at)
		values ($1, $2, 'card.created', '{}', now(), now())`,
		f.orgID, hookID)

	if err := f.worker.Once(f.ctx); err != nil {
		t.Fatal(err)
	}

	if got := f.count(`select count(*) from webhook_deliveries where delivered_at is not null`); got != 1 {
		t.Errorf("доставленных осталось %d, ожидалась одна свежая", got)
	}
	if got := f.count(`select count(*) from webhook_deliveries where failed_at is not null`); got != 1 {
		t.Errorf("несдавшаяся доставка убрана: осталось %d", got)
	}
}

// Журнал заводят затем, чтобы ответить на вопрос через год. Срок
// хранения по умолчанию превратил бы его в журнал на месяц.
func TestAuditIsKeptForeverUnlessOrgSaysOtherwise(t *testing.T) {
	f := newFixture(t)
	// Состарить запись изменением нельзя — журнал только дописывается,
	// политики update у него нет, и попытка молча ничего не меняет.
	// Поэтому старую запись кладём сразу старой.
	f.exec(`
		insert into audit_events (org_id, actor_id, action, subject, at)
		values ($1, $2, 'insert', 'teams', now() - interval '400 days')`,
		f.orgID, f.userID)

	// Рядом со старой — свежая запись: срок не должен унести и её.
	f.exec(`
		insert into audit_events (org_id, actor_id, action, subject)
		values ($1, $2, 'insert', 'teams')`, f.orgID, f.userID)

	const old = `select count(*) from audit_events where at < now() - interval '100 days'`
	const fresh = `select count(*) from audit_events where at > now() - interval '1 day'`

	if err := f.worker.Once(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.count(old); got != 1 {
		t.Fatalf("журнал убран без назначенного срока: старых записей %d", got)
	}

	// Организация назвала срок — теперь убирается.
	if _, err := f.db.Pool.Exec(f.ctx,
		`update orgs set audit_retention_days = 90 where id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.Once(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.count(old); got != 0 {
		t.Errorf("после назначения срока осталось %d записей старше срока", got)
	}
	if got := f.count(fresh); got == 0 {
		t.Error("вместе со старыми унесло и свежие записи")
	}
}

// Срок короче месяца не имеет смысла: журнал, не переживающий
// квартальную проверку, бесполезен. Это держит база.
func TestTooShortRetentionIsRefused(t *testing.T) {
	f := newFixture(t)
	if _, err := f.db.Pool.Exec(f.ctx,
		`update orgs set audit_retention_days = 5 where id = $1`, f.orgID); err == nil {
		t.Error("срок хранения в пять дней принят")
	}
}

// Уборщик ходит без арендатора, но это не делает журнал стираемым
// изнутри организации.
func TestOrganisationStillCannotEraseItsAudit(t *testing.T) {
	f := newFixture(t)
	f.exec(`insert into teams (org_id, name) values ($1, 'Команда')`, f.orgID)

	before := f.count(`select count(*) from audit_events`)
	if before == 0 {
		t.Fatal("след создания команды не записан")
	}
	f.exec(`delete from audit_events`)
	if after := f.count(`select count(*) from audit_events`); after != before {
		t.Errorf("организация стёрла свой журнал: было %d, стало %d", before, after)
	}
}
