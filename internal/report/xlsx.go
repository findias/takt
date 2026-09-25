package report

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Книга .xlsx без сторонней библиотеки — по той же причине, по какой
// её читает импорт (internal/importer/xlsx.go): библиотека привезла бы
// в поставку, которую проверяет ИБ, десятки тысяч строк ради двух
// листов и двух диаграмм. Книга — это zip с XML внутри, и нужного
// здесь немного: строки, числа, даты, закреплённая шапка, фильтр
// и две диаграммы на листе сводки.
//
// Писать самим даёт и второе: лист «Данные» уходит в ответ прямо
// из курсора базы. Библиотечные потоковые писатели складывают лист
// во временный файл и собирают архив в конце.
//
// Строки пишутся прямо в ячейку (inlineStr), без таблицы общих
// строк: её пришлось бы держать в памяти до конца, а повторяются
// в отчёте короткие слова, и экономия не стоит памяти.

const (
	nsMain  = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkg   = "http://schemas.openxmlformats.org/package/2006/relationships"
	nsChart = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	nsDraw  = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsXdr   = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"

	relSheet   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
	relStyles  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relDrawing = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing"
	relChart   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"
	relDoc     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
)

// Стили ячеек — номера в cellXfs, порядок как в stylesXML.
const (
	styleNone = iota
	styleBold
	styleDate
	styleDateTime
	styleDecimal
	stylePercent
	styleTitle
)

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="` + nsMain + `">
<numFmts count="3"><numFmt numFmtId="164" formatCode="yyyy-mm-dd"/><numFmt numFmtId="165" formatCode="yyyy-mm-dd hh:mm"/><numFmt numFmtId="166" formatCode="0.0"/></numFmts>
<fonts count="3"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font><font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font><font><b/><sz val="14"/><name val="Calibri"/><family val="2"/></font></fonts>
<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="7">
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>
<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="166" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="9" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/>
</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>`

// cell — одна ячейка. Пустая ячейка (kind 0) не пишется вовсе.
type cell struct {
	kind  byte // 0 пусто, 's' строка, 'n' число
	s     string
	n     float64
	style int
}

func str(s string) cell { return cell{kind: 's', s: s} }
func bold(s string) cell {
	return cell{kind: 's', s: s, style: styleBold}
}
func num(n float64) cell { return cell{kind: 'n', n: n} }
func dec(n float64) cell { return cell{kind: 'n', n: n, style: styleDecimal} }
func pct(n float64) cell { return cell{kind: 'n', n: n, style: stylePercent} }

func optDec(p *float64) cell {
	if p == nil {
		return cell{}
	}
	return dec(*p)
}

// Даты — числом дней от 30.12.1899, как их хранит сам Excel: строка
// «2026-09-24» в ячейке — это текст, по нему не построить сводную
// и не отфильтровать «за сентябрь».
func excelTime(t time.Time) float64 {
	epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return t.UTC().Sub(epoch).Hours() / 24
}

func day(t time.Time) cell { return cell{kind: 'n', n: math.Floor(excelTime(t)), style: styleDate} }
func stamp(t time.Time) cell {
	return cell{kind: 'n', n: excelTime(t), style: styleDateTime}
}
func optStamp(t *time.Time) cell {
	if t == nil {
		return cell{}
	}
	return stamp(*t)
}

// isoDay — дата вида 2026-09-24 ячейкой-датой.
func isoDay(s string) cell {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return str(s)
	}
	return day(t)
}

func optISODay(s *string) cell {
	if s == nil {
		return cell{}
	}
	return isoDay(*s)
}

// colName — буквы колонки: 0 → A, 26 → AA.
func colName(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

// Предел строки в ячейке у Excel — 32 767 знаков; длиннее он
// объявляет книгу испорченной.
const maxCellText = 32767

// sheetRows пишет строки листа в поток.
type sheetRows struct {
	w   *bufio.Writer
	n   int // номер последней записанной строки, с 1
	err error
}

func (s *sheetRows) row(cells ...cell) {
	s.n++
	if s.err != nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<row r="%d">`, s.n)
	for i, c := range cells {
		if c.kind == 0 {
			continue
		}
		ref := colName(i) + strconv.Itoa(s.n)
		style := ""
		if c.style != 0 {
			style = fmt.Sprintf(` s="%d"`, c.style)
		}
		switch c.kind {
		case 's':
			text := c.s
			if len([]rune(text)) > maxCellText {
				text = string([]rune(text)[:maxCellText])
			}
			fmt.Fprintf(&b, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">`, ref, style)
			// EscapeText заменяет и знаки, которых в XML быть не может
			// (управляющие из названий, перенесённых из других систем):
			// без этого одна такая карточка портит всю книгу.
			_ = xml.EscapeText(&b, []byte(text))
			b.WriteString(`</t></is></c>`)
		case 'n':
			fmt.Fprintf(&b, `<c r="%s"%s><v>%s</v></c>`, ref, style,
				strconv.FormatFloat(c.n, 'f', -1, 64))
		}
	}
	b.WriteString(`</row>`)
	_, s.err = s.w.WriteString(b.String())
}

// blank — пустая строка-разделитель.
func (s *sheetRows) blank() { s.n++ }

// xlsxBook — книга из двух листов: «Сводка» с диаграммами и «Данные».
// Части пишутся в zip по порядку: сперва всё постоянное, затем лист
// данных строками, затем сводка, которой нужны все строки до неё.
type xlsxBook struct {
	zw     *zip.Writer
	data   *sheetRows
	names  [2]string // сводка, данные
	width  int       // колонок в листе данных
	closed bool
}

func xmlAttr(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return strings.ReplaceAll(b.String(), `"`, "&quot;")
}

func newXLSXBook(w io.Writer, summaryName, dataName string, header []string, widths []float64) (*xlsxBook, error) {
	book := &xlsxBook{zw: zip.NewWriter(w), names: [2]string{summaryName, dataName}, width: len(header)}
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/drawings/drawing1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/>
<Override PartName="/xl/charts/chart1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/>
<Override PartName="/xl/charts/chart2.xml" ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/>
</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="` + nsPkg + `"><Relationship Id="rId1" Type="` + relDoc + `" Target="xl/workbook.xml"/></Relationships>`},
		// Сводка — первым листом: руководитель открывает файл и видит
		// ответ, а таблица на тысячи строк лежит следующей вкладкой.
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="` + nsMain + `" xmlns:r="` + nsRel + `">
<bookViews><workbookView activeTab="0"/></bookViews>
<sheets><sheet name="` + xmlAttr(summaryName) + `" sheetId="1" r:id="rId1"/><sheet name="` + xmlAttr(dataName) + `" sheetId="2" r:id="rId2"/></sheets>
</workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="` + nsPkg + `">
<Relationship Id="rId1" Type="` + relSheet + `" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="` + relSheet + `" Target="worksheets/sheet2.xml"/>
<Relationship Id="rId3" Type="` + relStyles + `" Target="styles.xml"/>
</Relationships>`},
		{"xl/styles.xml", stylesXML},
	}
	for _, p := range parts {
		f, err := book.zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(f, p.body); err != nil {
			return nil, err
		}
	}

	f, err := book.zw.Create("xl/worksheets/sheet2.xml")
	if err != nil {
		return nil, err
	}
	w2 := bufio.NewWriterSize(f, 64<<10)
	// Шапка закреплена: на тысячной строке без неё не понять, какая
	// колонка — время цикла, а какая — возраст.
	fmt.Fprintf(w2, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="%s" xmlns:r="%s"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetFormatPr defaultRowHeight="15"/>`, nsMain, nsRel)
	writeCols(w2, widths)
	w2.WriteString(`<sheetData>`) // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	book.data = &sheetRows{w: w2}
	cells := make([]cell, len(header))
	for i, h := range header {
		cells[i] = bold(h)
	}
	book.data.row(cells...)
	return book, book.data.err
}

func writeCols(w *bufio.Writer, widths []float64) {
	if len(widths) == 0 {
		return
	}
	w.WriteString(`<cols>`) // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	for i, width := range widths {
		fmt.Fprintf(w, `<col min="%d" max="%d" width="%s" customWidth="1"/>`, i+1, i+1,
			strconv.FormatFloat(width, 'f', -1, 64))
	}
	w.WriteString(`</cols>`) // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
}

// dataRow — строка листа «Данные».
func (b *xlsxBook) dataRow(cells ...cell) error {
	b.data.row(cells...)
	return b.data.err
}

// chartSpec — диаграмма на листе сводки. Ряды берутся из колонок
// таблицы, которая уже лежит на листе: диаграмма ссылается на ячейки,
// а не держит свою копию чисел, и правка ячейки её перерисовывает.
type chartSpec struct {
	kind     string // "bar" или "area" (накопительная, стопкой)
	title    string
	firstRow int // первая строка значений, с 1; над ней — строка шапки
	lastRow  int
	catCol   int   // колонка подписей (дат)
	valCols  []int // колонки рядов
	colors   []string
	anchor   [2]int // левый верхний угол: колонка и строка, с 0
}

// finish закрывает лист данных и пишет сводку: её строки уже собраны
// вызывающим, диаграммы ссылаются на них.
//
// Диаграмм всегда две: их части названы в [Content_Types].xml ещё
// до первой строки, когда сводки нет, а часть, названная и не
// записанная, для Excel — испорченная книга.
func (b *xlsxBook) finish(summary func(*sheetRows) [2]chartSpec, widths []float64) error {
	d := b.data
	if d.err != nil {
		return d.err
	}
	// Фильтр на шапке — первое, что делают с такой таблицей руками.
	fmt.Fprintf(d.w, `</sheetData><autoFilter ref="A1:%s%d"/></worksheet>`,
		colName(b.width-1), max(d.n, 1))
	if err := d.w.Flush(); err != nil {
		return err
	}

	f, err := b.zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="%s" xmlns:r="%s"><sheetPr><pageSetUpPr fitToPage="1"/></sheetPr><sheetViews><sheetView tabSelected="1" workbookViewId="0"/></sheetViews><sheetFormatPr defaultRowHeight="15"/>`, nsMain, nsRel)
	writeCols(w, widths)
	w.WriteString(`<sheetData>`) // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	rows := &sheetRows{w: w}
	charts := summary(rows)
	if rows.err != nil {
		return rows.err
	}
	// Сводку печатают — альбомом и в ширину страницы, чтобы
	// диаграммы не уезжали на отдельные листы бумаги.
	w.WriteString(`</sheetData><pageMargins left="0.5" right="0.5" top="0.6" bottom="0.6" header="0.3" footer="0.3"/><pageSetup paperSize="9" orientation="landscape" fitToWidth="1" fitToHeight="0"/><drawing r:id="rId1"/></worksheet>`) // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	if err := w.Flush(); err != nil {
		return err
	}

	sheetRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="` + nsPkg + `"><Relationship Id="rId1" Type="` + relDrawing + `" Target="../drawings/drawing1.xml"/></Relationships>`
	var drawing, drawingRels strings.Builder
	fmt.Fprintf(&drawing, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<xdr:wsDr xmlns:xdr="%s" xmlns:a="%s">`, nsXdr, nsDraw)
	fmt.Fprintf(&drawingRels, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="%s">`, nsPkg)
	for i, c := range charts {
		fmt.Fprintf(&drawing, `<xdr:twoCellAnchor><xdr:from><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:to><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to><xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="%d" name="%s"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="%s"><c:chart xmlns:c="%s" xmlns:r="%s" r:id="rId%d"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:twoCellAnchor>`,
			c.anchor[0], c.anchor[1], c.anchor[0]+chartCols, c.anchor[1]+chartRows,
			i+2, xmlAttr(c.title), nsChart, nsChart, nsRel, i+1)
		fmt.Fprintf(&drawingRels, `<Relationship Id="rId%d" Type="%s" Target="../charts/chart%d.xml"/>`,
			i+1, relChart, i+1)
	}
	drawing.WriteString(`</xdr:wsDr>`)
	drawingRels.WriteString(`</Relationships>`)

	parts := []struct{ name, body string }{
		{"xl/worksheets/_rels/sheet1.xml.rels", sheetRels},
		{"xl/drawings/drawing1.xml", drawing.String()},
		{"xl/drawings/_rels/drawing1.xml.rels", drawingRels.String()},
	}
	for i, c := range charts {
		parts = append(parts, struct{ name, body string }{
			fmt.Sprintf("xl/charts/chart%d.xml", i+1), b.chartXML(c)})
	}
	for _, p := range parts {
		f, err := b.zw.Create(p.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, p.body); err != nil {
			return err
		}
	}
	b.closed = true
	return b.zw.Close()
}

// Размер диаграммы в ячейках листа.
const (
	chartCols = 9
	chartRows = 18
)

// ref — адрес ячейки листа сводки для формулы диаграммы.
func (b *xlsxBook) ref(col, row int) string {
	return "'" + strings.ReplaceAll(b.names[0], "'", "''") + "'!$" + colName(col) + "$" + strconv.Itoa(row)
}

// span — столбец ячеек сводки с first по last строку.
func (b *xlsxBook) span(col, first, last int) string {
	return b.ref(col, first) + ":$" + colName(col) + "$" + strconv.Itoa(last)
}

// chartXML — диаграмма DrawingML. Порядок элементов задан схемой,
// и Excel, в отличие от LibreOffice, книгу с переставленными
// элементами объявляет испорченной — поэтому текст написан целиком,
// а не собирается из кусков в произвольном порядке.
func (b *xlsxBook) chartXML(c chartSpec) string {
	var s strings.Builder
	fmt.Fprintf(&s, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<c:chartSpace xmlns:c="%s" xmlns:a="%s" xmlns:r="%s"><c:roundedCorners val="0"/><c:chart>`, nsChart, nsDraw, nsRel)
	s.WriteString(`<c:title><c:tx><c:rich><a:bodyPr/><a:p><a:pPr><a:defRPr sz="1200" b="1"/></a:pPr><a:r><a:rPr lang="ru-RU" sz="1200" b="1"/><a:t>`)
	_ = xml.EscapeText(&s, []byte(c.title))
	s.WriteString(`</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title><c:autoTitleDeleted val="0"/><c:plotArea><c:layout/>`)

	cat := b.span(c.catCol, c.firstRow, c.lastRow)
	series := func() {
		for i, col := range c.valCols {
			fmt.Fprintf(&s, `<c:ser><c:idx val="%d"/><c:order val="%d"/><c:tx><c:strRef><c:f>%s</c:f></c:strRef></c:tx>`,
				i, i, b.ref(col, c.firstRow-1))
			fmt.Fprintf(&s, `<c:spPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></c:spPr>`, c.colors[i%len(c.colors)])
			if c.kind == "bar" {
				s.WriteString(`<c:invertIfNegative val="0"/>`)
			}
			val := b.span(col, c.firstRow, c.lastRow)
			fmt.Fprintf(&s, `<c:cat><c:numRef><c:f>%s</c:f></c:numRef></c:cat><c:val><c:numRef><c:f>%s</c:f></c:numRef></c:val></c:ser>`,
				cat, val)
		}
	}
	switch c.kind {
	case "bar":
		s.WriteString(`<c:barChart><c:barDir val="col"/><c:grouping val="clustered"/><c:varyColors val="0"/>`)
		series()
		s.WriteString(`<c:gapWidth val="40"/><c:axId val="1001"/><c:axId val="1002"/></c:barChart>`)
	default:
		s.WriteString(`<c:areaChart><c:grouping val="stacked"/><c:varyColors val="0"/>`)
		series()
		s.WriteString(`<c:axId val="1001"/><c:axId val="1002"/></c:areaChart>`)
	}
	// Столбцам недель — ось подписей: на календарной оси столбец
	// занимает один день из семи, и неделя выглядит штрихом.
	if c.kind == "bar" {
		s.WriteString(`<c:catAx><c:axId val="1001"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="b"/><c:numFmt formatCode="dd.mm" sourceLinked="0"/><c:majorTickMark val="out"/><c:minorTickMark val="none"/><c:tickLblPos val="nextTo"/><c:crossAx val="1002"/><c:crosses val="autoZero"/><c:auto val="0"/><c:lblAlgn val="ctr"/><c:lblOffset val="100"/><c:noMultiLvlLbl val="0"/></c:catAx>`)
	} else {
		s.WriteString(`<c:dateAx><c:axId val="1001"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="b"/><c:numFmt formatCode="dd.mm" sourceLinked="0"/><c:majorTickMark val="out"/><c:minorTickMark val="none"/><c:tickLblPos val="nextTo"/><c:crossAx val="1002"/><c:crosses val="autoZero"/><c:auto val="1"/><c:lblOffset val="100"/><c:baseTimeUnit val="days"/></c:dateAx>`)
	}
	s.WriteString(`<c:valAx><c:axId val="1002"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="l"/><c:majorGridlines><c:spPr><a:ln w="6350"><a:solidFill><a:srgbClr val="D9DEDC"/></a:solidFill></a:ln></c:spPr></c:majorGridlines><c:numFmt formatCode="0" sourceLinked="0"/><c:majorTickMark val="none"/><c:minorTickMark val="none"/><c:tickLblPos val="nextTo"/><c:crossAx val="1001"/><c:crosses val="autoZero"/><c:crossBetween val="between"/></c:valAx>`)
	s.WriteString(`</c:plotArea>`)
	if len(c.valCols) > 1 {
		s.WriteString(`<c:legend><c:legendPos val="b"/><c:overlay val="0"/></c:legend>`)
	}
	s.WriteString(`<c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/></c:chart></c:chartSpace>`)
	return s.String()
}
