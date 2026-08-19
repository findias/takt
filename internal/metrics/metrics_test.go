package metrics

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/store/testdb"
)

// Метрики считаются на данных с известным ответом: отметки проставляются
// прямо, а не через операции, — иначе тест проверял бы операции.

type fixture struct {
	svc     *Service
	db      *store.Store
	ctx     context.Context
	t       *testing.T
	orgID   string
	userID  string
	boardID string
	column  string
	seq     int
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

	f := &fixture{svc: New(db), db: db, ctx: ctx, t: t}
	suffix := uuid.NewString()

	if err := db.Pool.QueryRow(ctx,
		`insert into orgs (name, slug) values ($1, $2) returning id`,
		"Метрики "+suffix[:8], "metrics-"+suffix[:8]).Scan(&f.orgID); err != nil {
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

	f.inTenant(func(tx pgx.Tx) error {
		var projectID string
		if err := tx.QueryRow(ctx,
			`insert into projects (org_id, name) values ($1, 'П') returning id`,
			f.orgID).Scan(&projectID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			insert into boards (org_id, project_id, name, key)
			values ($1, $2, 'Доска', 'ДОСК')
			returning id`, f.orgID, projectID).Scan(&f.boardID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			insert into board_columns (org_id, board_id, name, position, kind)
			values ($1, $2, 'В работе', 'a0', 'in_progress') returning id`,
			f.orgID, f.boardID).Scan(&f.column)
	})
	return f
}

func (f *fixture) inTenant(fn func(pgx.Tx) error) {
	f.t.Helper()
	if err := f.db.InTenant(f.ctx, f.orgID, f.userID, fn); err != nil {
		f.t.Fatal(err)
	}
}

// card заводит карточку с заданной судьбой: сколько дней назад начата,
// сколько дней шла (или nil, если ещё идёт), и чем кончилась.
func (f *fixture) card(title string, startedDaysAgo float64, tookDays *float64, outcome *string) string {
	f.t.Helper()
	// Номер выдаём сами: карточки здесь заводятся в обход операций,
	// а номер обязан быть уникальным — на нём стоит ограничение.
	f.seq++
	var id string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			insert into cards (org_id, board_id, number, column_id, title, position,
			                   created_at, started_at, finished_at, outcome)
			values ($1, $2, 'ДОСК-' || $9::int, $3, $4, $5,
			        now() - $6::numeric * interval '1 day',
			        now() - $6::numeric * interval '1 day',
			        case when $7::numeric is null then null
			             else now() - ($6::numeric - $7::numeric) * interval '1 day' end,
			        $8)
			returning id`,
			f.orgID, f.boardID, f.column, title, uuid.NewString(),
			startedDaysAgo, tookDays, outcome, f.seq).Scan(&id)
	})
	return id
}

func days(v float64) *float64 { return &v }
func done() *string           { s := "done"; return &s }
func discarded() *string      { s := "discarded"; return &s }

func TestCycleTimeIsPercentilesOfFinishedWork(t *testing.T) {
	f := newFixture(t)
	// Времена цикла: 1, 2, 3, 4, 10 дней.
	for _, took := range []float64{1, 2, 3, 4, 10} {
		f.card("Сделано", took+1, days(took), done())
	}
	// Выброшенная карточка времени цикла не имеет: у неё время до отказа,
	// а это другая величина.
	f.card("Передумали", 20, days(15), discarded())

	report, err := f.svc.Report(f.ctx, f.orgID, f.userID, f.boardID, 90)
	if err != nil {
		t.Fatal(err)
	}
	if report.CycleTime == nil {
		t.Fatal("время цикла не посчитано")
	}
	if report.CycleTime.Count != 5 {
		t.Errorf("время цикла посчитано по %d карточкам, ожидалось 5", report.CycleTime.Count)
	}
	if got := report.CycleTime.P50; got < 2.9 || got > 3.1 {
		t.Errorf("медиана %.2f, ожидалось около 3", got)
	}
	// Длинный хвост — то, ради чего берут проценты, а не среднее.
	if report.CycleTime.P95 < 8 {
		t.Errorf("95-й процент %.2f, ожидалось около 10", report.CycleTime.P95)
	}
	if report.Discarded != 1 {
		t.Errorf("выброшенных %d, ожидалась одна", report.Discarded)
	}
}

func TestAgingShowsWhatIsStuckRightNow(t *testing.T) {
	f := newFixture(t)
	f.card("Давняя", 12, nil, nil)
	fresh := f.card("Свежая", 1, nil, nil)
	f.card("Уже готова", 5, days(2), done())

	// Заблокированная стареет, ничего не делая, — это первое, что нужно
	// видеть в списке.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			insert into card_blocks (org_id, card_id, reason, blocked_by)
			values ($1, $2, 'ждём смежников', $3)`,
			f.orgID, fresh, f.userID)
		return err
	})

	report, err := f.svc.Report(f.ctx, f.orgID, f.userID, f.boardID, 90)
	if err != nil {
		t.Fatal(err)
	}
	if report.WIP != 2 {
		t.Fatalf("в работе %d карточек, ожидалось две", report.WIP)
	}
	if len(report.Aging) != 2 || report.Aging[0].Title != "Давняя" {
		t.Fatalf("список старения: %+v", report.Aging)
	}
	if report.Aging[0].Days < 11 || report.Aging[0].Days > 13 {
		t.Errorf("возраст давней карточки %.1f дня", report.Aging[0].Days)
	}
	var blocked bool
	for _, a := range report.Aging {
		if a.ID == fresh {
			blocked = a.Blocked
		}
	}
	if !blocked {
		t.Error("блокировка не отражена в списке старения")
	}
}

// Недели без единой законченной карточки нужны прогнозу: без них он
// считает, что команда всегда что-то доводит до конца.
func TestThroughputKeepsEmptyWeeks(t *testing.T) {
	f := newFixture(t)
	f.card("Одна", 3, days(1), done())

	report, err := f.svc.Report(f.ctx, f.orgID, f.userID, f.boardID, 28)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Throughput) < 4 {
		t.Fatalf("недель в отчёте %d, ожидалось не меньше четырёх", len(report.Throughput))
	}
	total, empty := 0, 0
	for _, w := range report.Throughput {
		total += w.Count
		if w.Count == 0 {
			empty++
		}
	}
	if total != 1 {
		t.Errorf("всего доведено %d, ожидалась одна карточка", total)
	}
	if empty == 0 {
		t.Error("пустые недели выпали из ряда")
	}
}

// Прогноз — вероятностный по определению: вопрос «когда будет готово»
// другого ответа не имеет.
func TestForecastGrowsWithTheAmountOfWork(t *testing.T) {
	weekly := []WeeklyCount{{Count: 3}, {Count: 2}, {Count: 4}, {Count: 3}, {Count: 1}, {Count: 3}}
	points := forecast(weekly)
	if len(points) != 3 {
		t.Fatalf("точек прогноза %d, ожидалось три", len(points))
	}
	for _, p := range points {
		if p.P50 > p.P85 || p.P85 > p.P95 {
			t.Errorf("проценты не по возрастанию: %+v", p)
		}
	}
	if points[0].P85 >= points[2].P85 {
		t.Errorf("двадцать карточек прогнозируются не дольше пяти: %+v", points)
	}

	// Один и тот же прошлый поток обязан давать один и тот же прогноз:
	// иначе двое увидят разные числа и потратят день на выяснение,
	// чей верный.
	again := forecast(weekly)
	for i := range points {
		if points[i] != again[i] {
			t.Fatalf("прогноз не воспроизводится: %+v против %+v", points[i], again[i])
		}
	}

	// Без единой законченной карточки прогноза нет: складывать нули
	// можно бесконечно.
	if forecast([]WeeklyCount{{Count: 0}, {Count: 0}, {Count: 0}, {Count: 0}}) != nil {
		t.Error("прогноз посчитан по пустому потоку")
	}
}

func TestMetricsOfForeignBoardAreNotFound(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)

	if _, err := f.svc.Report(f.ctx, other.orgID, other.userID, f.boardID, 90); !errors.Is(err, ErrNoData) {
		t.Errorf("метрики чужой доски: %v", err)
	}
}
