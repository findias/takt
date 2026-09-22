package importer

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Разбор .xlsx без сторонней библиотеки. Книга — это zip с XML внутри,
// и для переноса из неё нужно немного: названия листов, общие строки
// и значения ячеек. Библиотека вроде excelize привезла бы десятки
// тысяч строк кода, который умеет писать диаграммы, — в поставку,
// которую проверяет ИБ, ради чтения одной таблицы.
//
// Формулы не вычисляем: берём значение, которое Excel сохранил вместе
// с формулой. Стили не читаем — даты приходят числом (дни от 30.12.1899),
// и их узнаёт разбор дат колонки (вид «excel»).

// Пределы распаковки. Пять мегабайт сжатого могут развернуться
// в гигабайты — «бомба сжатия»; книга на пять тысяч строк укладывается
// в единицы мегабайт XML.
const (
	maxXLSXPart  = 64 << 20
	maxXLSXParts = 2000
	// Пределы самого Excel: 1 048 576 строк и 16 384 колонки.
	maxSheetRows    = 1 << 20
	maxSheetColumns = 1 << 14
)

var (
	// ErrNotXLSX — файл не книга Excel. Отдельной ошибкой: по ней
	// различается «это не xlsx» и «xlsx, но испорченная».
	ErrNotXLSX = errors.New("файл не похож на книгу Excel (.xlsx)")
	// ErrNoSheet — такого листа в книге нет.
	ErrNoSheet = errors.New("такого листа в книге нет — выберите лист из списка")
)

// IsXLSX — книга ли это: zip начинается с «PK».
func IsXLSX(raw []byte) bool {
	return bytes.HasPrefix(raw, []byte("PK\x03\x04"))
}

// Workbook — листы книги в порядке, в каком их видит Excel.
type Workbook struct {
	Sheets []string
	paths  map[string]string
	files  map[string]*zip.File
	shared []string
}

// OpenXLSX читает оглавление книги и общие строки.
func OpenXLSX(raw []byte) (*Workbook, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, ErrNotXLSX
	}
	if len(zr.File) > maxXLSXParts {
		return nil, fmt.Errorf("в книге больше %d частей — это не таблица задач", maxXLSXParts)
	}
	wb := &Workbook{paths: map[string]string{}, files: map[string]*zip.File{}}
	for _, f := range zr.File {
		wb.files[f.Name] = f
	}

	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := wb.decode("xl/workbook.xml", &book); err != nil {
		return nil, ErrNotXLSX
	}
	var rels struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := wb.decode("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, ErrNotXLSX
	}
	target := map[string]string{}
	for _, r := range rels.Rel {
		t := r.Target
		// Путь бывает от корня книги («/xl/worksheets/…») и от xl/.
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Join("xl", t)
		}
		target[r.ID] = t
	}
	for _, s := range book.Sheets {
		if p, ok := target[s.RID]; ok {
			wb.Sheets = append(wb.Sheets, s.Name)
			wb.paths[s.Name] = p
		}
	}
	if len(wb.Sheets) == 0 {
		return nil, ErrNotXLSX
	}

	// Общих строк может не быть вовсе: книга из одних чисел.
	if _, ok := wb.files["xl/sharedStrings.xml"]; ok {
		var sst struct {
			SI []richText `xml:"si"`
		}
		if err := wb.decode("xl/sharedStrings.xml", &sst); err != nil {
			return nil, fmt.Errorf("общие строки книги не читаются: %w", err)
		}
		wb.shared = make([]string, len(sst.SI))
		for i, si := range sst.SI {
			wb.shared[i] = si.text()
		}
	}
	return wb, nil
}

// richText — строка Excel: либо целиком в <t>, либо кусками в <r><t>
// (часть слова жирная — уже куски).
type richText struct {
	T    string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (r richText) text() string {
	if len(r.Runs) == 0 {
		return r.T
	}
	var b strings.Builder
	b.WriteString(r.T)
	for _, run := range r.Runs {
		b.WriteString(run.T)
	}
	return b.String()
}

func (wb *Workbook) decode(name string, into any) error {
	f, ok := wb.files[name]
	if !ok {
		return fmt.Errorf("в книге нет %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(io.LimitReader(rc, maxXLSXPart)).Decode(into)
}

// Sheet читает лист в таблицу. Пустое имя — первый лист.
func (wb *Workbook) Sheet(name string) (Table, error) {
	if name == "" {
		name = wb.Sheets[0]
	}
	p, ok := wb.paths[name]
	if !ok {
		return Table{}, ErrNoSheet
	}
	var ws struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				Ref    string   `xml:"r,attr"`
				Type   string   `xml:"t,attr"`
				V      string   `xml:"v"`
				Inline richText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := wb.decode(p, &ws); err != nil {
		return Table{}, fmt.Errorf("лист «%s» не читается: %w", name, err)
	}

	var grid [][]string
	for i, row := range ws.Rows {
		// Пустые строки Excel не пишет вовсе, а номер строки
		// стоит в атрибуте; без него — по порядку.
		n := row.R
		if n == 0 {
			n = i + 1
		}
		// Больше строк у Excel не бывает; номер сверх — испорченный
		// или подделанный файл, и память под него не заводим.
		if n > maxSheetRows {
			return Table{}, fmt.Errorf("лист «%s» не читается: строка %d за пределом Excel", name, n)
		}
		for len(grid) < n {
			grid = append(grid, nil)
		}
		var cells []string
		for j, c := range row.Cells {
			col := j
			if c.Ref != "" {
				col = columnIndex(c.Ref)
			}
			if col < 0 || col >= maxSheetColumns {
				continue
			}
			for len(cells) <= col {
				cells = append(cells, "")
			}
			cells[col] = wb.value(c.Type, c.V, c.Inline)
		}
		grid[n-1] = cells
	}

	// Строка заголовков — первая, где заполнено хотя бы две ячейки.
	// Выгрузки ставят над таблицей подпись отчёта в одну ячейку и дату
	// выгрузки; принять их за заголовки значило бы предложить человеку
	// сопоставлять «Отчёт по задачам за сентябрь». Таблица из одной
	// колонки бывает тоже — тогда берётся первая непустая строка.
	head := -1
	for i, rec := range grid {
		if filled(rec) >= 2 {
			head = i
			break
		}
		if head < 0 && !blank(rec) {
			head = i
		}
	}
	var t Table
	if head < 0 {
		return Table{}, ErrEmpty
	}
	t.Headers = trimAll(grid[head])
	for i := head + 1; i < len(grid); i++ {
		rec := grid[i]
		if blank(rec) {
			continue
		}
		t.Rows = append(t.Rows, rec)
		t.Lines = append(t.Lines, i+1)
		if len(t.Rows) > MaxRows {
			return Table{}, fmt.Errorf("в файле больше %d строк — перенесите его по частям", MaxRows)
		}
	}
	if len(t.Rows) == 0 {
		return Table{}, ErrEmpty
	}
	return t, nil
}

func (wb *Workbook) value(kind, v string, inline richText) string {
	switch kind {
	case "s":
		i, err := strconv.Atoi(v)
		if err != nil || i < 0 || i >= len(wb.shared) {
			return ""
		}
		return wb.shared[i]
	case "inlineStr":
		return inline.text()
	case "b":
		if v == "1" {
			return "TRUE"
		}
		return "FALSE"
	}
	// Число, строка формулы, ошибка — как сохранено.
	return v
}

func filled(rec []string) int {
	n := 0
	for _, v := range rec {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

// columnIndex — номер колонки по ссылке ячейки: «C12» → 2, «AA3» → 26.
func columnIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
		if n > maxSheetColumns {
			return maxSheetColumns
		}
	}
	return n - 1
}

// Read читает файл любого из двух видов: книгу Excel узнаём по zip,
// всё остальное — CSV. Листы — только у книги.
//
// Без выбранного листа берётся первый, на котором есть таблица: книги
// часто начинаются со сводки или обложки, и отказ «в файле нет строк»
// про книгу, где задачи лежат на втором листе, — неправда. Список
// листов возвращается и при отказе: выбрать другой лист человек
// может, только видя, какие есть.
func Read(raw []byte, sheet string) (t Table, sheets []string, used string, err error) {
	if !IsXLSX(raw) {
		t, err = ReadCSV(raw)
		return t, nil, "", err
	}
	wb, err := OpenXLSX(raw)
	if err != nil {
		return Table{}, nil, "", err
	}
	if sheet != "" {
		t, err = wb.Sheet(sheet)
		return t, wb.Sheets, sheet, err
	}
	var first error
	for _, name := range wb.Sheets {
		t, err = wb.Sheet(name)
		if err == nil {
			return t, wb.Sheets, name, nil
		}
		if first == nil {
			first = err
		}
	}
	return Table{}, wb.Sheets, wb.Sheets[0], first
}
