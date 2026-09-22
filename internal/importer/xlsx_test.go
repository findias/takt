package importer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// Книга собирается в самой проверке: так видно, из чего она состоит,
// и не нужен двоичный образец, который никто не сможет прочитать глазами.

type sheetXML struct{ name, rows string }

func makeXLSX(t *testing.T, shared []string, sheets ...sheetXML) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	var book, rels strings.Builder
	book.WriteString(`<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, s := range sheets {
		fmt.Fprintf(&book, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, s.name, i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		put(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`+
				s.rows+`</sheetData></worksheet>`)
	}
	book.WriteString(`</sheets></workbook>`)
	rels.WriteString(`</Relationships>`)
	put("xl/workbook.xml", book.String())
	put("xl/_rels/workbook.xml.rels", rels.String())
	var sst strings.Builder
	sst.WriteString(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, s := range shared {
		sst.WriteString(s)
	}
	sst.WriteString(`</sst>`)
	put("xl/sharedStrings.xml", sst.String())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWorkbookReadsTheChosenSheetWithExcelLineNumbers(t *testing.T) {
	raw := makeXLSX(t,
		[]string{
			`<si><t>Название</t></si>`, `<si><t>Срок</t></si>`, `<si><t>Оценка</t></si>`,
			// Часть слова жирная — строка хранится кусками.
			`<si><r><t>Сверить </t></r><r><rPr><b/></rPr><t>остатки</t></r></si>`,
			`<si><t>Лист задач</t></si>`,
		},
		sheetXML{"Сводка", `<row r="1"><c r="A1" t="s"><v>4</v></c></row>`},
		sheetXML{"Задачи", `
			<row r="1"><c r="A1" t="s"><v>4</v></c></row>
			<row r="3"><c r="A3" t="s"><v>0</v></c><c r="B3" t="s"><v>1</v></c><c r="C3" t="s"><v>2</v></c></row>
			<row r="4"><c r="A4" t="s"><v>3</v></c><c r="B4"><v>46295</v></c><c r="C4"><v>2.5</v></c></row>
			<row r="7"><c r="A7" t="inlineStr"><is><t>Заказать тару</t></is></c><c r="C7"><v>много</v></c></row>`},
	)
	if !IsXLSX(raw) {
		t.Fatal("книга не узнана")
	}
	table, sheets, _, err := Read(raw, "Задачи")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(sheets, ",") != "Сводка,Задачи" {
		t.Fatalf("листы: %q", sheets)
	}
	// Без выбора берётся первый лист с таблицей: «Сводка» — одна
	// подпись, таблицы в ней нет.
	if _, _, used, err := Read(raw, ""); err != nil || used != "Задачи" {
		t.Fatalf("лист по умолчанию: %q, %v", used, err)
	}
	// Подпись отчёта в одну ячейку над таблицей заголовками не считается.
	if strings.Join(table.Headers, "|") != "Название|Срок|Оценка" {
		t.Fatalf("заголовки: %q", table.Headers)
	}
	p, err := Build(table, Suggest(table.Headers))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 2 || p.Cards[0].Title != "Сверить остатки" || p.Cards[1].Title != "Заказать тару" {
		t.Fatalf("карточки: %+v", p.Cards)
	}
	// Дата пришла числом — днями Excel — и понята как дата.
	if p.Cards[0].Due == nil || p.Cards[0].Due.Format("2006-01-02") != "2026-09-30" ||
		len(p.Dates) != 1 || p.Dates[0].Format != "excel" {
		t.Fatalf("срок %v, даты поняты как %+v", p.Cards[0].Due, p.Dates)
	}
	// Претензия называет строку так, как её видно в Excel: седьмая,
	// хотя в таблице она вторая.
	if len(p.Problems) != 1 || p.Problems[0].Row != 7 {
		t.Fatalf("претензии: %+v", p.Problems)
	}
}

func TestNotAWorkbookAndMissingSheetSayWhatIsWrong(t *testing.T) {
	if _, _, _, err := Read([]byte("PK\x03\x04мусор"), ""); err != ErrNotXLSX {
		t.Fatalf("испорченная книга: %v", err)
	}
	raw := makeXLSX(t, nil, sheetXML{"Лист1", `<row r="1"><c r="A1"><v>1</v></c></row>`})
	if _, _, _, err := Read(raw, "Нет такого"); err != ErrNoSheet {
		t.Fatalf("чужой лист: %v", err)
	}
	// Подделанный номер строки не заставляет заводить память под миллиард.
	raw = makeXLSX(t, nil, sheetXML{"Лист1", `<row r="999999999"><c r="A1"><v>1</v></c></row>`})
	if _, _, _, err := Read(raw, ""); err == nil || !strings.Contains(err.Error(), "за пределом Excel") {
		t.Fatalf("строка за пределом: %v", err)
	}
}

func TestCSVLineNumbersSurviveBlankLines(t *testing.T) {
	table, err := ReadCSV([]byte("Title,Estimate\n\nFirst,1\n\n\nSecond,lots\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(table, Suggest(table.Headers))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) != 1 || p.Problems[0].Row != 6 {
		t.Fatalf("претензия указывает на строку %+v, в файле это шестая", p.Problems)
	}
}
