package report

import (
	"archive/zip"
	"bytes"
	"io"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5"
)

func (f *fixture) part(parent, child string) {
	f.t.Helper()
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `insert into card_links (org_id, from_card, to_card, kind)
			values ($1, $2, $3, 'subtask')`, f.orgID, parent, child)
		return err
	})
}

func (f *fixture) percent(card string, p int) {
	f.t.Helper()
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `update cards set percent = $2 where id = $1`, card, p)
		return err
	})
}

// Части стоят сразу под своей задачей, а не там, куда их поставил бы
// номер; процент — как на карточке доски.
func TestPartsFollowTheirTaskWithCompletion(t *testing.T) {
	f := newFixture(t)
	// Номера по порядку заведения: задача — первой, чужая карточка —
	// между ней и её частями. По номерам части ушли бы от задачи.
	task := f.card(spec{title: "Задача", created: 5, started: 4, finished: -1})
	other := f.card(spec{title: "Посторонняя", created: 5, started: -1, finished: -1})
	done := f.card(spec{title: "Часть сделана", created: 5, started: 3, finished: 1, outcome: "done"})
	half := f.card(spec{title: "Часть наполовину", created: 5, started: 3, finished: -1})
	deep := f.card(spec{title: "Пункт части", created: 5, started: -1, finished: -1})
	f.part(task, done)
	f.part(task, half)
	f.part(half, deep)
	f.percent(deep, 40)
	_ = other

	var out collect
	flt := period(30)
	if err := f.svc.Export(f.ctx, f.orgID, f.owner, flt, func() (Sink, error) { return &out, nil }); err != nil {
		t.Fatal(err)
	}
	var order []string
	level := map[string]int{}
	pct := map[string]*int{}
	for _, r := range out.rows {
		order = append(order, r.Title)
		level[r.Title] = r.Level
		pct[r.Title] = r.Percent
	}
	want := []string{"Задача", "Часть сделана", "Часть наполовину", "Пункт части", "Посторонняя"}
	if len(order) != len(want) {
		t.Fatalf("строки %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("порядок %v, ожидался %v", order, want)
		}
	}
	for title, l := range map[string]int{"Задача": 0, "Часть сделана": 1, "Часть наполовину": 1, "Пункт части": 2, "Посторонняя": 0} {
		if level[title] != l {
			t.Errorf("«%s»: уровень %d, ожидался %d", title, level[title], l)
		}
	}
	// Листья поддерева задачи: сделанная часть (1) и пункт на 40% (0,4) —
	// 70%. Часть со своим пунктом считается по нему: 40%. Ручной — как есть,
	// сделанная без частей — 100, неначатая без частей — пусто.
	for title, p := range map[string]int{"Задача": 70, "Часть наполовину": 40, "Пункт части": 40, "Часть сделана": 100} {
		if pct[title] == nil || *pct[title] != p {
			t.Errorf("«%s»: процент %v, ожидался %d", title, pct[title], p)
		}
	}
	if pct["Посторонняя"] != nil {
		t.Errorf("у неначатой без частей процент %d, ожидалось пусто", *pct["Посторонняя"])
	}

	// В книге части — группой под задачей.
	var buf bytes.Buffer
	if err := f.svc.Export(f.ctx, f.orgID, f.owner, flt, func() (Sink, error) { return NewXLSX(f.ctx, &buf, flt) }); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range zr.File {
		if part.Name != "xl/worksheets/sheet2.xml" {
			continue
		}
		r, _ := part.Open()
		body, _ := io.ReadAll(r)
		r.Close()
		levels := regexp.MustCompile(`<row r="(\d+)"(?: outlineLevel="(\d)")?>`).FindAllStringSubmatch(string(body), -1)
		got := ""
		for _, m := range levels[1:] {
			if m[2] == "" {
				got += "0"
			} else {
				got += m[2]
			}
		}
		if got != "01120" {
			t.Errorf("уровни строк листа данных %q, ожидалось 01120", got)
		}
	}
}
