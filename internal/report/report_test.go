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
			                   created_at, started_at, finished_at, outcome)
			values ($1, $2, 'ПОСТ-' || $3::int, $4, $5, $6, $7,
			        now() - $8::numeric * interval '1 day',
			        now() - $9::numeric * interval '1 day',
			        now() - $10::numeric * interval '1 day', $11)
			returning id`, f.orgID, s.board, f.seq, f.column[s.board], s.title, uuid.NewString(),
			s.priority, s.created, ago(s.started), ago(s.finished), outcome).Scan(&id)
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
	if !slices.Equal(sheets, []string{"Summary", "Data"}) {
		t.Errorf("листы %v, ожидались Summary и Data (английский запрос)", sheets)
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
