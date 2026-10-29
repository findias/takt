package report

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/store/testdb"
)

// Выгрузка проверяется на данных с известным ответом: отметки
// карточек проставляются прямо, как в проверках метрик, — иначе тест
// проверял бы операции, а не отбор.

type fixture struct {
	svc    *Service
	db     *store.Store
	ctx    context.Context
	t      *testing.T
	orgID  string
	owner  string
	member string
	board  string // открыта всей организации
	closed string // закрытая: участник на ней не состоит
	column map[string]string
	seq    int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testdb.Shared(t)
	f := &fixture{svc: New(db), db: db, ctx: ctx, t: t, column: map[string]string{}}
	suffix := uuid.NewString()[:8]

	if err := db.Pool.QueryRow(ctx, `insert into orgs (name, slug) values ($1, $2) returning id`,
		"Выгрузка "+suffix, "report-"+suffix).Scan(&f.orgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, f.orgID) })
	person := func(name, role string) string {
		var id string
		if err := db.Pool.QueryRow(ctx, `insert into users (email, name, password_hash)
			values ($1, $2, 'x') returning id`, uuid.NewString()+"@example.test", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `delete from users where id = $1`, id) })
		if _, err := db.Pool.Exec(ctx, `insert into memberships (org_id, user_id, role) values ($1, $2, $3)`,
			f.orgID, id, role); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.owner = person("Анна", "owner")
	f.member = person("Борис", "member")

	f.inTenant(f.owner, func(tx pgx.Tx) error {
		var project string
		if err := tx.QueryRow(ctx, `insert into projects (org_id, name) values ($1, 'П') returning id`,
			f.orgID).Scan(&project); err != nil {
			return err
		}
		for _, b := range []struct {
			name, key string
			out       *string
		}{{"Поставки", "ПОСТ", &f.board}, {"Тайное", "ТАЙН", &f.closed}} {
			if err := tx.QueryRow(ctx, `insert into boards (org_id, project_id, name, key)
				values ($1, $2, $3, $4) returning id`, f.orgID, project, b.name, b.key).
				Scan(b.out); err != nil {
				return err
			}
			var col string
			if err := tx.QueryRow(ctx, `insert into board_columns (org_id, board_id, name, position, kind)
				values ($1, $2, 'В работе', 'a0', 'in_progress') returning id`, f.orgID, *b.out).Scan(&col); err != nil {
				return err
			}
			f.column[*b.out] = col
		}
		// Закрытой доска становится после того, как владелец на ней
		// состоит: закрытую без участников не видит даже он.
		if _, err := tx.Exec(ctx, `insert into board_members (org_id, board_id, user_id)
			values ($1, $2, $3)`, f.orgID, f.closed, f.owner); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update boards set visibility = 'private' where id = $1`, f.closed)
		return err
	})
	return f
}

func (f *fixture) inTenant(user string, fn func(pgx.Tx) error) {
	f.t.Helper()
	if err := f.db.InTenant(f.ctx, f.orgID, user, fn); err != nil {
		f.t.Fatal(err)
	}
}

// card — карточка с заданной судьбой; дни — от «сейчас» назад,
// отрицательное started/finished — «не было».
type spec struct {
	board             string
	title             string
	created, started  float64
	finished          float64
	outcome, priority string
	// due — срок, дней назад от «сейчас» (отрицательное — впереди);
	// nil — срока нет.
	due *float64
}

func (f *fixture) card(s spec) string {
	f.t.Helper()
	f.seq++
	if s.board == "" {
		s.board = f.board
	}
	if s.priority == "" {
		s.priority = "medium"
	}
	ago := func(d float64) *float64 {
		if d < 0 {
			return nil
		}
		return &d
	}
	var outcome *string
	if s.outcome != "" {
		outcome = &s.outcome
	}
	var id string
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			insert into cards (org_id, board_id, number, column_id, title, position, priority,
			                   created_at, started_at, finished_at, outcome, due_on)
			values ($1, $2, 'ПОСТ-' || $3::int, $4, $5, $6, $7,
			        now() - $8::numeric * interval '1 day',
			        now() - $9::numeric * interval '1 day',
			        now() - $10::numeric * interval '1 day', $11,
			        (now() - $12::numeric * interval '1 day')::date)
			returning id`, f.orgID, s.board, f.seq, f.column[s.board], s.title, uuid.NewString(),
			s.priority, s.created, ago(s.started), ago(s.finished), outcome, s.due).Scan(&id)
	})
	return id
}

// period — последние days дней, сегодня включительно.
func period(days int) Filter {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return Filter{From: today.AddDate(0, 0, -days+1), To: today}
}

// collect собирает выгрузку в память.
type collect struct {
	rows []Row
	sum  *Summary
}

func (c *collect) Row(r Row) error         { c.rows = append(c.rows, r); return nil }
func (c *collect) Summary(s Summary) error { c.sum = &s; return nil }
func (c *collect) titles() (out []string) {
	for _, r := range c.rows {
		out = append(out, r.Title)
	}
	return out
}

func (f *fixture) export(user string, flt Filter) *collect {
	f.t.Helper()
	c := &collect{}
	if err := f.svc.Export(f.ctx, f.orgID, user, flt, func() (Sink, error) { return c, nil }); err != nil {
		f.t.Fatal(err)
	}
	return c
}

// Период берёт жившее в нём: заведённое до конца и не законченное
// до начала. Законченное раньше и заведённое позже — мимо.
func TestPeriodTakesWhatLivedInIt(t *testing.T) {
	f := newFixture(t)
	f.card(spec{title: "Давно сделано", created: 90, started: 80, finished: 60, outcome: "done"})
	f.card(spec{title: "Сделано в периоде", created: 40, started: 20, finished: 5, outcome: "done"})
	f.card(spec{title: "Идёт с прошлого", created: 50, started: 45, finished: -1})
	f.card(spec{title: "Ждёт", created: 3, started: -1, finished: -1})

	flt := period(30)
	got := f.export(f.owner, flt)
	want := []string{"Сделано в периоде", "Идёт с прошлого", "Ждёт"}
	for _, w := range want {
		if !slices.Contains(got.titles(), w) {
			t.Errorf("в выгрузке нет «%s»: %v", w, got.titles())
		}
	}
	if slices.Contains(got.titles(), "Давно сделано") {
		t.Errorf("законченное до периода попало в выгрузку")
	}
	if got.sum.Cards != 3 || got.sum.Finished != 1 || got.sum.WIP != 1 {
		t.Errorf("сводка: карточек %d, сделано %d, в работе %d; ожидалось 3, 1, 1",
			got.sum.Cards, got.sum.Finished, got.sum.WIP)
	}

	// Период, кончившийся десять дней назад: «Ждёт» заведена позже,
	// а «Сделано в периоде» в нём ещё шла и сделанной не считается.
	flt.To = flt.To.AddDate(0, 0, -10)
	got = f.export(f.owner, flt)
	if slices.Contains(got.titles(), "Ждёт") {
		t.Errorf("заведённое после конца периода попало в выгрузку")
	}
	if !slices.Contains(got.titles(), "Сделано в периоде") || got.sum.Finished != 0 {
		t.Errorf("карточка, закрытая после периода, должна быть в строках и не в «сделано»: %v, сделано %d",
			got.titles(), got.sum.Finished)
	}
}

// Закрытая доска, на которой человек не состоит, в выгрузку не
// попадает — ни строкой, ни в сводке, ни названием в отборе.
func TestExportSeesOnlyVisibleBoards(t *testing.T) {
	f := newFixture(t)
	f.card(spec{title: "Открытая", created: 3, started: -1, finished: -1})
	f.card(spec{board: f.closed, title: "Секрет", created: 3, started: 2, finished: 1, outcome: "done"})

	got := f.export(f.member, period(30))
	if slices.Contains(got.titles(), "Секрет") || got.sum.Finished != 0 {
		t.Errorf("участник видит карточку закрытой доски: %v, сделано %d", got.titles(), got.sum.Finished)
	}
	flt := period(30)
	flt.Boards = []string{f.closed}
	got = f.export(f.member, flt)
	if len(got.rows) != 0 || len(got.sum.Chosen.Boards) != 0 {
		t.Errorf("закрытая доска, названная прямо, отдала %d строк и имя %v",
			len(got.rows), got.sum.Chosen.Boards)
	}
	if n, err := f.svc.Count(f.ctx, f.orgID, f.member, period(30)); err != nil || n != 1 {
		t.Errorf("подсчёт для участника: %d, %v; ожидалась одна карточка", n, err)
	}
}

func TestFilterNarrowsByStateAndPriority(t *testing.T) {
	f := newFixture(t)
	f.card(spec{title: "Горит", created: 3, started: 2, finished: -1, priority: "highest"})
	f.card(spec{title: "Фоном", created: 3, started: 2, finished: -1, priority: "low"})
	f.card(spec{title: "Сделана", created: 3, started: 2, finished: 1, outcome: "done", priority: "highest"})

	flt := period(30)
	flt.States = []string{StateActive}
	flt.Priorities = []string{"highest"}
	got := f.export(f.owner, flt)
	if !slices.Equal(got.titles(), []string{"Горит"}) {
		t.Errorf("отбор «в работе, наивысший» дал %v", got.titles())
	}
	if !slices.Equal(got.sum.Chosen.States, []string{"active"}) {
		t.Errorf("отбор не назван в сводке: %+v", got.sum.Chosen)
	}
}

// Больше предела — отказ до первой строки: приёмник даже не открыт.
func TestTooManyRefusesBeforeTheFirstRow(t *testing.T) {
	f := newFixture(t)
	for range 3 {
		f.card(spec{title: "Карточка", created: 3, started: -1, finished: -1})
	}
	f.svc.limit = 2
	opened := false
	err := f.svc.Export(f.ctx, f.orgID, f.owner, period(30), func() (Sink, error) {
		opened = true
		return &collect{}, nil
	})
	var many *TooMany
	if !errors.As(err, &many) || many.Total != 3 || many.Limit != 2 {
		t.Fatalf("ожидался отказ «3 из 2», получено %v", err)
	}
	if opened {
		t.Error("приёмник открыт до отказа: ответ был бы уже начат")
	}
}

func TestBadFilterSaysWhatToFix(t *testing.T) {
	f := period(30)
	f.To, f.From = f.From, f.To
	if err := f.Check(); !errors.Is(err, ErrBadFilter) {
		t.Errorf("конец раньше начала прошёл проверку: %v", err)
	}
	if err := (&Filter{}).Check(); !errors.Is(err, ErrBadFilter) {
		t.Errorf("выгрузка без периода прошла проверку: %v", err)
	}
	g := period(30)
	g.States = []string{"closed"}
	if err := g.Check(); !errors.Is(err, ErrBadFilter) {
		t.Errorf("незнакомое состояние прошло проверку: %v", err)
	}
}

// Книга читается нашим же импортом и открывается как XML во всех
// частях; название, похожее на формулу, остаётся текстом.
func TestXLSXIsAReadableBook(t *testing.T) {
	f := newFixture(t)
	f.card(spec{title: `=HYPERLINK("http://x") и «кавычки» <b>&`, created: 3, started: 2, finished: 1, outcome: "done"})
	f.card(spec{title: "Управляющий \x01 знак", created: 3, started: -1, finished: -1})

	var buf bytes.Buffer
	ctx := i18n.WithLang(f.ctx, i18n.EN)
	flt := period(30)
	err := f.svc.Export(f.ctx, f.orgID, f.owner, flt, func() (Sink, error) { return NewXLSX(ctx, &buf, flt) })
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("книга — не zip: %v", err)
	}
	for _, part := range zr.File {
		r, _ := part.Open()
		d := xml.NewDecoder(r)
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Errorf("%s — не XML: %v", part.Name, err)
				break
			}
		}
		r.Close()
	}

	table, sheets, _, err := importer.Read(buf.Bytes(), "Data")
	if err != nil {
		t.Fatalf("импорт не читает выгрузку: %v", err)
	}
	if !slices.Equal(sheets, []string{"Summary", "Data", "Flow by date"}) {
		t.Errorf("листы %v, ожидались Summary, Data и Flow by date (английский запрос)", sheets)
	}
	// Накопительная рисуется по своему листу: на сводке чисел под ней нет.
	for _, part := range zr.File {
		if part.Name != "xl/charts/chart2.xml" {
			continue
		}
		r, _ := part.Open()
		body, _ := io.ReadAll(r)
		r.Close()
		if !strings.Contains(string(body), "'Flow by date'!$B$2") {
			t.Errorf("накопительная ссылается не на лист потока: %s", body)
		}
	}
	if table.Headers[0] != "Number" || len(table.Rows) != 2 {
		t.Fatalf("шапка %v, строк %d", table.Headers, len(table.Rows))
	}
	titles := []string{table.Rows[0][1], table.Rows[1][1]}
	if !slices.Contains(titles, `=HYPERLINK("http://x") и «кавычки» <b>&`) {
		t.Errorf("название, похожее на формулу, изменилось: %q", titles)
	}
}

// CSV обезвреживает формулы и начинается с метки порядка байтов.
func TestCSVDisarmsFormulas(t *testing.T) {
	f := newFixture(t)
	f.card(spec{title: "=1+1", created: 3, started: -1, finished: -1})

	var buf bytes.Buffer
	err := f.svc.Export(f.ctx, f.orgID, f.owner, period(30), func() (Sink, error) { return NewCSV(f.ctx, &buf) })
	if err != nil {
		t.Fatal(err)
	}
	raw := buf.String()
	if !strings.HasPrefix(raw, "\uFEFF") {
		t.Error("CSV без метки порядка байтов: Excel откроет его в Windows-1251")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(raw, "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][1] != "'=1+1" {
		t.Errorf("формула в названии не обезврежена: %q", rows)
	}
}

// JSON — один документ, поля как в API, значения — коды.
func TestJSONIsOneDocument(t *testing.T) {
	f := newFixture(t)
	f.card(spec{title: "Сделана", created: 5, started: 4, finished: 1, outcome: "done", priority: "high"})

	var buf bytes.Buffer
	flt := period(30)
	err := f.svc.Export(f.ctx, f.orgID, f.owner, flt, func() (Sink, error) { return NewJSON(&buf, flt) })
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Limit   int     `json:"limit"`
		Cards   []Row   `json:"cards"`
		Summary Summary `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("не JSON: %v\n%s", err, buf.String())
	}
	if len(doc.Cards) != 1 || doc.Cards[0].State != "done" || doc.Cards[0].Priority != "high" {
		t.Fatalf("строки: %+v", doc.Cards)
	}
	if c := doc.Cards[0].CycleDays; c == nil || *c < 2.99 || *c > 3.01 {
		t.Errorf("время цикла %v, ожидалось 3", c)
	}
	if doc.Summary.Finished != 1 || doc.Limit != MaxRows {
		t.Errorf("сводка %+v, предел %d", doc.Summary, doc.Limit)
	}
}

// Срез свой у каждого: чужого не видно, не удалить и не занять его имя.
func TestSlicesAreEachPersonsOwn(t *testing.T) {
	f := newFixture(t)
	saved, err := f.svc.SaveSlice(f.ctx, f.orgID, f.owner, "  Прошлый квартал ", "?period=lastQuarter&state=done")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Прошлый квартал" || saved.Query != "period=lastQuarter&state=done" {
		t.Errorf("сохранено %+v: название и отбор не очищены", saved)
	}
	if _, err := f.svc.SaveSlice(f.ctx, f.orgID, f.owner, "прошлый КВАРТАЛ", ""); !errors.Is(err, ErrSliceExists) {
		t.Errorf("одноимённый срез без учёта регистра прошёл: %v", err)
	}
	// У другого человека то же имя свободно, а чужой срез не виден.
	if _, err := f.svc.SaveSlice(f.ctx, f.orgID, f.member, "Прошлый квартал", "state=active"); err != nil {
		t.Errorf("у другого человека имя должно быть свободно: %v", err)
	}
	mine, err := f.svc.Slices(f.ctx, f.orgID, f.member)
	if err != nil || len(mine) != 1 || mine[0].Query != "state=active" {
		t.Errorf("участник видит %+v, %v; ожидался один свой срез", mine, err)
	}
	if err := f.svc.DeleteSlice(f.ctx, f.orgID, f.member, saved.ID); !errors.Is(err, ErrSliceNotFound) {
		t.Errorf("чужой срез удалился или отказ не тот: %v", err)
	}
	if err := f.svc.DeleteSlice(f.ctx, f.orgID, f.owner, saved.ID); err != nil {
		t.Errorf("свой срез не удалился: %v", err)
	}
	if _, err := f.svc.SaveSlice(f.ctx, f.orgID, f.owner, "   ", ""); !errors.Is(err, ErrBadFilter) {
		t.Errorf("срез без названия прошёл: %v", err)
	}
}

// Заявки и описание — в выгрузке. Без них файл, который отдают
// наружу, не отвечает на два первых вопроса о работе: по какой заявке
// она идёт и что в ней надо сделать (замечено владельцем 25.09.2026).
func TestExportCarriesTicketsAndDescription(t *testing.T) {
	f := newFixture(t)
	id := f.card(spec{title: "Перенести оплату", created: 3, started: 2, finished: -1})
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `update cards set description = $2 where id = $1`,
			id, "Шлюз меняется до 15 октября.\nВозвраты — отдельно."); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `
			insert into card_refs (org_id, card_id, kind, ref, created_at)
			values ($1, $2, 'rds', '12345', now() - interval '1 hour'),
			       ($1, $2, 'problem', 'PRB-7', now())`, f.orgID, id)
		return err
	})

	got := f.export(f.owner, period(30))
	if len(got.rows) != 1 {
		t.Fatalf("строк %d, ожидалась одна", len(got.rows))
	}
	row := got.rows[0]
	if !slices.Equal(row.Refs, []Ref{{Kind: "rds", Ref: "12345"}, {Kind: "problem", Ref: "PRB-7"}}) {
		t.Errorf("заявки %+v", row.Refs)
	}
	if row.Description != "Шлюз меняется до 15 октября.\nВозвраты — отдельно." {
		t.Errorf("описание %q", row.Description)
	}

	var buf bytes.Buffer
	flt := period(30)
	if err := f.svc.Export(f.ctx, f.orgID, f.owner, flt, func() (Sink, error) {
		return NewXLSX(i18n.WithLang(f.ctx, i18n.RU), &buf, flt)
	}); err != nil {
		t.Fatal(err)
	}
	table, _, _, err := importer.Read(buf.Bytes(), "Данные")
	if err != nil {
		t.Fatal(err)
	}
	cell := func(name string) string {
		i := slices.Index(table.Headers, name)
		if i < 0 {
			t.Fatalf("нет столбца %q в %v", name, table.Headers)
		}
		return table.Rows[0][i]
	}
	if got := cell("Внешние номера"); got != "RDS 12345; Проблема PRB-7" {
		t.Errorf("столбец «Внешние номера»: %q", got)
	}
	if got := cell("Описание"); !strings.HasPrefix(got, "Шлюз меняется до 15 октября.") {
		t.Errorf("столбец «Описание»: %q", got)
	}
}

// Создано против сделано в сводке (ROADMAP 42.3, срез 7): «создано»
// считает заведённое внутри периода по неделям; заведённое до периода
// в нём не создано, хотя в отбор попадает.
func TestSummaryCountsCreatedByWeek(t *testing.T) {
	f := newFixture(t)
	f.card(spec{title: "Старая", created: 40, started: 40, finished: 1, outcome: "done"})
	f.card(spec{title: "Новая", created: 1, started: -1, finished: -1})

	c := f.export(f.owner, period(14))
	created, finished := 0, 0
	for _, w := range c.sum.Throughput {
		created += w.Created
		finished += w.Count
	}
	if created != 1 || finished != 1 {
		t.Errorf("создано %d, сделано %d; ожидалось по одной", created, finished)
	}
}

// Соблюдение сроков (ROADMAP 42.3, срез 8): среди сделанных со сроком —
// сколько в срок; не сделанная с прошедшим сроком — просрочена сейчас;
// карточка без срока в счёт не идёт ни там, ни там.
func TestSummaryCountsDueDates(t *testing.T) {
	f := newFixture(t)
	d := func(v float64) *float64 { return &v }
	f.card(spec{title: "В срок", created: 10, started: 9, finished: 3, outcome: "done", due: d(2)})
	f.card(spec{title: "С опозданием", created: 10, started: 9, finished: 1, outcome: "done", due: d(4)})
	f.card(spec{title: "Просрочена", created: 10, started: 9, finished: -1, due: d(1)})
	f.card(spec{title: "Срок впереди", created: 10, started: 9, finished: -1, due: d(-5)})
	f.card(spec{title: "Без срока", created: 10, started: 9, finished: 2, outcome: "done"})

	c := f.export(f.owner, period(14))
	if got := c.sum.Due; got.WithDue != 4 || got.Finished != 2 || got.OnTime != 1 || got.OverdueNow != 1 {
		t.Errorf("сроки %+v; ожидалось со сроком 4, сделано 2, в срок 1, просрочено сейчас 1", got)
	}
}

// Блокировки за период (ROADMAP 42.3, срез 6): карточка, заблокированная
// дважды, считается суммой обоих интервалов, а не последним; интервал
// обрезается периодом; открытая считается до сейчас; по причинам —
// как написали, пустая — отдельной строкой.
func TestSummaryCountsBlockedDaysBySumOfSpans(t *testing.T) {
	f := newFixture(t)
	twice := f.card(spec{title: "Дважды", created: 20, started: 19, finished: -1})
	once := f.card(spec{title: "Однажды, с хвостом до периода", created: 40, started: 39, finished: -1})
	f.card(spec{title: "Не блокировалась", created: 5, started: 4, finished: -1})
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		for _, b := range []struct {
			card   string
			reason string
			from   int
			to     *int
		}{
			{twice, "ждём акт", 10, intp(8)},
			{twice, "ждём акт", 4, intp(1)},
			// 20 дней назад — до начала периода в 14 дней: внутрь попадают 14 - 12 = 2 дня.
			{once, "", 20, intp(12)},
		} {
			var until any
			if b.to != nil {
				until = time.Now().AddDate(0, 0, -*b.to)
			}
			if _, err := tx.Exec(f.ctx, `insert into card_blocks (org_id, card_id, reason, blocked_at, unblocked_at)
				 values ($1, $2, $3, now() - $4::int * interval '1 day', $5)`,
				f.orgID, b.card, b.reason, b.from, until); err != nil {
				return err
			}
		}
		return nil
	})
	c := f.export(f.owner, period(14))
	b := c.sum.Blocks
	// «Дважды»: 2 + 3 = 5 дней; «Однажды»: от начала периода (14 дней
	// назад, в полночь UTC) до 12 дней назад — около двух дней.
	if b.Cards != 2 || b.Days < 6.5 || b.Days > 7.5 {
		t.Errorf("блокировки: %+v — ожидались 2 карточки и около 7 дней", b)
	}
	if len(b.Reasons) != 2 || b.Reasons[0].Reason != "ждём акт" || b.Reasons[0].Days < 4.9 || b.Reasons[0].Days > 5.1 ||
		b.Reasons[1].Reason != "" {
		t.Errorf("по причинам: %+v — ожидались «ждём акт» на 5 дней и пустая причина", b.Reasons)
	}
}

func intp(v int) *int { return &v }

// Тренд по итерациям (ROADMAP 42.3, срез 3): доля выполненного
// обещания считается от обещанного на первый день и не меняется
// от добавленного по ходу — иначе она врала бы в обе стороны;
// выброшенное считается отдельно.
func TestIterationShareIgnoresLateAdded(t *testing.T) {
	f := newFixture(t)
	promisedDone := f.card(spec{title: "Обещана и сделана", created: 20, started: 9, finished: 2, outcome: "done"})
	promisedNot := f.card(spec{title: "Обещана, не сделана", created: 20, started: 9, finished: -1})
	lateDone := f.card(spec{title: "Добавлена по ходу и сделана", created: 6, started: 5, finished: 1, outcome: "done"})
	dropped := f.card(spec{title: "Выброшена", created: 20, started: -1, finished: -1})
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		var it string
		if err := tx.QueryRow(f.ctx, `insert into iterations (org_id, board_id, name, starts_on, ends_on, goal)
			 values ($1, $2, 'Неделя 40', current_date - 10, current_date + 3, '') returning id`,
			f.orgID, f.board).Scan(&it); err != nil {
			return err
		}
		for _, c := range []struct {
			card    string
			added   int
			removed *int
		}{{promisedDone, 12, nil}, {promisedNot, 12, nil}, {lateDone, 6, nil}, {dropped, 12, intp(4)}} {
			var removed any
			if c.removed != nil {
				removed = time.Now().AddDate(0, 0, -*c.removed)
			}
			if _, err := tx.Exec(f.ctx, `insert into iteration_cards (org_id, iteration_id, card_id, added_at, removed_at)
				 values ($1, $2, $3, now() - $4::int * interval '1 day', $5)`, f.orgID, it, c.card, c.added, removed); err != nil {
				return err
			}
		}
		return nil
	})
	c := f.export(f.owner, period(14))
	if len(c.sum.Iterations) != 1 {
		t.Fatalf("итераций %d, ожидалась одна", len(c.sum.Iterations))
	}
	it := c.sum.Iterations[0]
	if it.Planned != 2 || it.Added != 1 || it.Dropped != 1 || it.Done != 2 || it.DonePlanned != 1 {
		t.Errorf("итерация %+v; ожидалось обещано 2, добавлено 1, выброшено 1, сделано 2, из обещанного 1", it)
	}
}

// Куда ушли силы (ROADMAP 42.3, срез 2): карточка с двумя метками одной
// области считается у каждой, без метки — своей строкой; эпик —
// ближайший предок на портфеле; недоведённое в разрез не входит.
func TestEffortByLabelAndEpic(t *testing.T) {
	f := newFixture(t)
	both := f.card(spec{title: "Две метки", created: 10, started: 9, finished: 2, outcome: "done"})
	one := f.card(spec{title: "Одна метка", created: 10, started: 9, finished: 1, outcome: "done"})
	f.card(spec{title: "Без меток", created: 10, started: 9, finished: 1, outcome: "done"})
	f.card(spec{title: "Не сделана", created: 10, started: 9, finished: -1})
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		var bug, debt, portfolio, epic string
		if err := tx.QueryRow(f.ctx, `insert into labels (org_id, name, tone) values ($1, 'дефект', 'rose') returning id`, f.orgID).Scan(&bug); err != nil {
			return err
		}
		if err := tx.QueryRow(f.ctx, `insert into labels (org_id, name, tone) values ($1, 'долг', 'slate') returning id`, f.orgID).Scan(&debt); err != nil {
			return err
		}
		for _, pair := range [][2]string{{both, bug}, {both, debt}, {one, bug}} {
			if _, err := tx.Exec(f.ctx, `insert into card_labels (org_id, card_id, label_id) values ($1, $2, $3)`, f.orgID, pair[0], pair[1]); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(f.ctx, `insert into boards (org_id, project_id, name, key, level)
			 select org_id, project_id, 'Портфель', 'ПОРТ', 'portfolio' from boards where id = $1 returning id`, f.board).Scan(&portfolio); err != nil {
			return err
		}
		var col string
		if err := tx.QueryRow(f.ctx, `insert into board_columns (org_id, board_id, name, position, kind)
			 values ($1, $2, 'Эпики', 'a0', 'in_progress') returning id`, f.orgID, portfolio).Scan(&col); err != nil {
			return err
		}
		if err := tx.QueryRow(f.ctx, `insert into cards (org_id, board_id, number, column_id, title, position)
			 values ($1, $2, 'ПОРТ-1', $3, 'Переезд', $4) returning id`, f.orgID, portfolio, col, uuid.NewString()).Scan(&epic); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `insert into card_links (org_id, from_card, to_card, kind) values ($1, $2, $3, 'subtask')`, f.orgID, epic, both)
		return err
	})
	c := f.export(f.owner, period(14))
	e := c.sum.Effort
	if e.Finished != 3 || e.MixedUnits {
		t.Errorf("сделано %d, единицы смешаны %v; ожидалось 3 и нет", e.Finished, e.MixedUnits)
	}
	byLabel := map[string]int{}
	for _, r := range e.ByLabel {
		byLabel[r.Name] = r.Finished
	}
	if byLabel["дефект"] != 2 || byLabel["долг"] != 1 || byLabel[""] != 1 {
		t.Errorf("по меткам: %v — ожидались дефект 2, долг 1, без метки 1", byLabel)
	}
	byEpic := map[string]int{}
	for _, r := range e.ByEpic {
		byEpic[r.Name] = r.Finished
	}
	if byEpic["Переезд"] != 1 || byEpic[""] != 2 {
		t.Errorf("по эпикам: %v — ожидались Переезд 1, без эпика 2", byEpic)
	}
}
