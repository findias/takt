package importer

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// Разбор проверяется без базы: он ничего о ней не знает, и ошибка
// в нём должна находиться здесь, а не на сквозном прогоне.

func TestExcelCSVInWindows1251WithSemicolons(t *testing.T) {
	// Так сохраняет Excel в русской локали: ;, Windows-1251, без BOM.
	raw, err := charmap.Windows1251.NewEncoder().Bytes([]byte(
		"Название;Статус;Ответственный\r\nСверить остатки;В работе;anna@example.test\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	table, err := ReadCSV(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(table.Headers, "|") != "Название|Статус|Ответственный" {
		t.Fatalf("заголовки прочитаны как %q", table.Headers)
	}
	if table.Rows[0][0] != "Сверить остатки" {
		t.Fatalf("строка прочитана как %q", table.Rows[0])
	}
	m := Suggest(table.Headers)
	if m[0] != FieldTitle || m[1] != FieldColumn || m[2] != FieldAssignees {
		t.Fatalf("сопоставление предложено как %q", m)
	}
}

func TestBOMAndCommasAndEmptyLines(t *testing.T) {
	table, err := ReadCSV([]byte("\xef\xbb\xbfSummary,Status\n\nFix login,Done\n,\n"))
	if err != nil {
		t.Fatal(err)
	}
	if table.Headers[0] != "Summary" || len(table.Rows) != 1 {
		t.Fatalf("BOM или пустые строки не сняты: %q, %d строк", table.Headers, len(table.Rows))
	}
}

func TestFileWithOnlyHeadersSaysWhatIsExpected(t *testing.T) {
	if _, err := ReadCSV([]byte("Заголовок\n")); err != ErrEmpty {
		t.Fatalf("ожидался отказ «нет строк», получено %v", err)
	}
}

func TestBadRowDoesNotSinkTheImport(t *testing.T) {
	table := Table{
		Headers: []string{"Заголовок", "Оценка", "Приоритет", "Исполнитель", "Срок"},
		Rows: [][]string{
			{"Первая", "3,5", "Высокий", "Anna@Example.test, Борис", "22.09.2026"},
			{"", "1", "", "", ""},
			{"Третья", "много", "как получится", "", "31.02.2026"},
		},
	}
	p, err := Build(table, Suggest(table.Headers))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 2 {
		t.Fatalf("переедут %d карточек, ожидалось 2: строка без заголовка — одна", len(p.Cards))
	}
	first := p.Cards[0]
	if first.Estimate == nil || *first.Estimate != 3.5 || first.Priority != "high" {
		t.Fatalf("оценка или приоритет прочитаны не так: %+v", first)
	}
	if len(first.Assignees) != 1 || first.Assignees[0] != "anna@example.test" {
		t.Fatalf("исполнители: %q — почта в нижнем регистре, имя без почты отброшено", first.Assignees)
	}
	if first.Due == nil || first.Due.Format("2006-01-02") != "2026-09-22" {
		t.Fatalf("срок прочитан как %v", first.Due)
	}
	third := p.Cards[1]
	if third.Estimate != nil || third.Priority != "" || third.Due != nil {
		t.Fatalf("негодные поля должны были отпасть: %+v", third)
	}

	// Каждая претензия называет строку так, как её видно в Excel.
	byRow := map[int][]string{}
	for _, pr := range p.Problems {
		byRow[pr.Row] = append(byRow[pr.Row], string(pr.Field))
	}
	if strings.Join(byRow[2], ",") != "assignees" {
		t.Fatalf("строка 2: %q", byRow[2])
	}
	if strings.Join(byRow[3], ",") != "title" {
		t.Fatalf("строка 3: %q", byRow[3])
	}
	if strings.Join(byRow[4], ",") != "estimate,priority,due" {
		t.Fatalf("строка 4: %q", byRow[4])
	}
}

func TestDatesAreReadByColumnAndSaidAloud(t *testing.T) {
	for _, c := range []struct {
		values []string
		format string
		want   string
	}{
		{[]string{"2026-09-01", "2026-09-22 10:30"}, "iso", "2026-09-01"},
		{[]string{"01.09.2026", "22.09.2026 10:30"}, "dotted", "2026-09-01"},
		{[]string{"01/Sep/26 10:26 AM", "22/Sep/26 3:00 PM"}, "jira", "2026-09-01"},
	} {
		table := Table{Headers: []string{"Title", "Created"}}
		for _, v := range c.values {
			table.Rows = append(table.Rows, []string{"x " + v, v})
		}
		p, err := Build(table, Suggest(table.Headers))
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Dates) != 1 || p.Dates[0].Format != c.format {
			t.Fatalf("%q понято как %+v, ожидалось %s", c.values, p.Dates, c.format)
		}
		if got := p.Cards[0].Created.Format("2006-01-02"); got != c.want {
			t.Fatalf("%q: первая дата %s", c.values, got)
		}
	}
}

func TestMappingWithoutTitleOrTwiceTheSameFieldIsRefused(t *testing.T) {
	table := Table{Headers: []string{"A", "B"}, Rows: [][]string{{"1", "2"}}}
	if _, err := Build(table, Mapping{FieldColumn, FieldNone}); err == nil ||
		!strings.Contains(err.Error(), "заголовком") {
		t.Fatalf("без заголовка ожидался отказ с подсказкой, получено %v", err)
	}
	if _, err := Build(table, Mapping{FieldTitle, FieldTitle}); err == nil ||
		!strings.Contains(err.Error(), "«A» и «B»") {
		t.Fatalf("два заголовка: ожидался отказ с именами колонок, получено %v", err)
	}
}

func TestKeysTellRowsApart(t *testing.T) {
	// Без своего ключа одинаковые заголовки нумеруются — и второй
	// прогон того же файла узнает каждую строку.
	table := Table{Headers: []string{"Title"}, Rows: [][]string{{"Созвон"}, {"Созвон"}, {"Отчёт"}}}
	p, err := Build(table, Suggest(table.Headers))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{p.Cards[0].ExternalID, p.Cards[1].ExternalID, p.Cards[2].ExternalID}
	if strings.Join(got, "|") != "title:созвон|title:созвон#2|title:отчёт" {
		t.Fatalf("ключи: %q", got)
	}

	// Свой ключ, повторившийся в файле, — это ошибка файла: вторая
	// строка не едет и названа.
	table = Table{Headers: []string{"Title", "Key"}, Rows: [][]string{{"А", "K-1"}, {"Б", "K-1"}}}
	p, err = Build(table, Suggest(table.Headers))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 1 || len(p.Problems) != 1 || !p.Problems[0].Skipped {
		t.Fatalf("повтор ключа: %d карточек, претензии %+v", len(p.Cards), p.Problems)
	}
}

func TestSamplesMapThemselves(t *testing.T) {
	for name, sample := range map[string]string{"ru": SampleRU, "en": SampleEN} {
		table, err := ReadCSV([]byte(sample))
		if err != nil {
			t.Fatal(name, err)
		}
		m := Suggest(table.Headers)
		for i, f := range m {
			if f == FieldNone {
				t.Errorf("%s: колонка образца «%s» не сопоставилась сама", name, table.Headers[i])
			}
		}
		p, err := Build(table, m)
		if err != nil {
			t.Fatal(name, err)
		}
		if len(p.Problems) != 0 || len(p.Cards) != 3 {
			t.Errorf("%s: образец разобран с претензиями %+v", name, p.Problems)
		}
		if len(p.Cards[1].Labels) != 2 || len(p.Cards[1].Assignees) != 2 {
			t.Errorf("%s: перечисления в образце: %+v", name, p.Cards[1])
		}
	}
}

func TestColumnKindsFromNames(t *testing.T) {
	for name, want := range map[string]string{
		"Готово": "done", "Done": "done", "В работе": "in_progress", "In Progress": "in_progress",
		"Бэклог": "queue", "To Do": "queue", "Нужно сделать": "queue", "Review": "", "На согласовании": "",
	} {
		if got := ColumnKind(name); got != want {
			t.Errorf("%s: %s, ожидалось %s", name, got, want)
		}
	}
}

// Файл ровно той длины, на которой режет выгрузку известная платформа,
// назван в отчёте: на строку меньше — тишина.
func TestFileCutAtAKnownExportLimitIsNamed(t *testing.T) {
	table := func(n int) Table {
		tb := Table{Headers: []string{"Summary"}}
		for i := range n {
			tb.Rows = append(tb.Rows, []string{fmt.Sprintf("Задача %d", i)})
			tb.Lines = append(tb.Lines, i+2)
		}
		return tb
	}
	for _, c := range []struct {
		rows int
		want string
	}{
		{1000, "ровно 1000 строк — столько за раз отдаёт выгрузка CSV облачной Jira"},
		{10000, "ровно 10000 строк — столько за раз отдаёт выгрузка Excel из monday"},
		{999, ""},
	} {
		p, err := Build(table(c.rows), Suggest([]string{"Summary"}))
		if err != nil {
			t.Fatal(err)
		}
		lost := strings.Join(p.Lost, " | ")
		if c.want == "" && lost != "" || !strings.Contains(lost, c.want) {
			t.Errorf("%d строк: %q", c.rows, lost)
		}
	}
}
