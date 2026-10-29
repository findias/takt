package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/store/testdb"
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
	ctx := context.Background()
	db := testdb.Shared(t)

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

// Пункт родителя — подзадача с той же доски — по колонкам не ходит,
// и его отметки начала и финиша о потоке ничего не говорят. Считай
// метрики их, родитель с тремя пунктами давал бы четыре «сделанных»
// и четыре «в работе» вместо одного.
func TestItemsOfAParentAreNotFlow(t *testing.T) {
	f := newFixture(t)
	parent := f.card("Родитель", 6, nil, nil)
	finished := f.card("Пункт сделан", 5, days(1), done())
	going := f.card("Пункт идёт", 4, nil, nil)
	f.inTenant(func(tx pgx.Tx) error {
		for _, part := range []string{finished, going} {
			if _, err := tx.Exec(f.ctx, `
				insert into card_links (org_id, from_card, to_card, kind)
				values ($1, $2, $3, 'subtask')`, f.orgID, parent, part); err != nil {
				return err
			}
		}
		return nil
	})

	report, err := f.svc.Report(f.ctx, f.orgID, f.userID, f.boardID, 90)
	if err != nil {
		t.Fatal(err)
	}
	if report.CycleTime != nil {
		t.Errorf("время цикла посчитано по пункту родителя: %+v", report.CycleTime)
	}
	if report.WIP != 1 || report.Aging[0].ID != parent {
		t.Errorf("в работе должен быть один родитель, а не его пункты: %+v", report.Aging)
	}
	var total int
	for _, w := range report.Throughput {
		total += w.Count
	}
	if total != 0 {
		t.Errorf("пропускная способность посчитала пункт: %d", total)
	}
}

// Создано против сделано (ROADMAP 42.3, срез 7): у недели считается
// и заведённое. Карточка, заведённая и доведённая в разные недели,
// стоит в «создано» одной и в «сделано» другой; заведённая без
// финиша — только в «создано».
func TestThroughputCountsCreatedAgainstDone(t *testing.T) {
	f := newFixture(t)
	f.card("Давно заведена, доведена сейчас", 20, days(20), done())
	f.card("Заведена сейчас, не доведена", 0.5, nil, nil)

	report, err := f.svc.Report(f.ctx, f.orgID, f.userID, f.boardID, 28)
	if err != nil {
		t.Fatal(err)
	}
	created, finished := 0, 0
	for _, w := range report.Throughput {
		created += w.Created
		finished += w.Count
	}
	if created != 2 || finished != 1 {
		t.Errorf("создано %d, сделано %d; ожидалось 2 и 1", created, finished)
	}
	last := report.Throughput[len(report.Throughput)-1]
	if last.Created < 1 {
		t.Errorf("заведённая сегодня не попала в «создано» текущей недели: %+v", last)
	}
}

// Эпик с прогнозом (ROADMAP 42.3, срез 4): листья поддерева через доски,
// объём по неделям, даты — когда есть доведённая часть и доведённое
// на досках частей; без доведённых частей — null, а не дата.
func TestEpicForecastNeedsADonePart(t *testing.T) {
	f := newFixture(t)
	var portfolio, epic string
	f.inTenant(func(tx pgx.Tx) error {
		if err := tx.QueryRow(f.ctx, `insert into boards (org_id, project_id, name, key, level)
			 select org_id, project_id, 'Портфель', 'ПОРТ', 'portfolio' from boards where id = $1 returning id`,
			f.boardID).Scan(&portfolio); err != nil {
			return err
		}
		var col string
		if err := tx.QueryRow(f.ctx, `insert into board_columns (org_id, board_id, name, position, kind)
			 values ($1, $2, 'Эпики', 'a0', 'in_progress') returning id`, f.orgID, portfolio).Scan(&col); err != nil {
			return err
		}
		return tx.QueryRow(f.ctx, `insert into cards (org_id, board_id, number, column_id, title, position)
			 values ($1, $2, 'ПОРТ-1', $3, 'Переезд', $4) returning id`, f.orgID, portfolio, col, uuid.NewString()).Scan(&epic)
	})
	link := func(child string) {
		f.inTenant(func(tx pgx.Tx) error {
			_, err := tx.Exec(f.ctx, `insert into card_links (org_id, from_card, to_card, kind) values ($1, $2, $3, 'subtask')`,
				f.orgID, epic, child)
			return err
		})
	}
	link(f.card("Часть идёт", 10, nil, nil))
	link(f.card("Часть ждёт", 3, nil, nil))
	// На доске есть доведённое, но не из этого эпика: прогнозу всё
	// ещё не из чего — у эпика ни одной доведённой части.
	f.card("Чужая, сделана", 8, days(6), done())

	got, err := f.svc.EpicForecast(f.ctx, f.orgID, f.userID, epic)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Done != 0 || got.Remaining != 2 || got.Forecast != nil {
		t.Errorf("без доведённых частей: %+v — ожидались 2 части, прогноз пуст", got)
	}
	if len(got.Scope) == 0 || got.Scope[len(got.Scope)-1].Scope != 2 {
		t.Errorf("объём по неделям: %+v — ожидалась последняя точка с двумя", got.Scope)
	}

	link(f.card("Часть сделана", 9, days(7), done()))
	got, err = f.svc.EpicForecast(f.ctx, f.orgID, f.userID, epic)
	if err != nil {
		t.Fatal(err)
	}
	if got.Done != 1 || got.Remaining != 2 || got.Forecast == nil {
		t.Fatalf("с доведённой частью: %+v — ожидался прогноз", got)
	}
	today := time.Now().UTC().Format(time.DateOnly)
	if got.Forecast.P50 < today || got.Forecast.P85 < got.Forecast.P50 || got.Forecast.P95 < got.Forecast.P85 {
		t.Errorf("даты не по порядку: %+v", got.Forecast)
	}
	if got.Finished < 2 || got.Boards != 1 {
		t.Errorf("история: %+v — ожидались две доведённые на одной доске", got)
	}
}
