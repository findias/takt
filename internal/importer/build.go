package importer

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// SourceTable — откуда карточка: таблица. Входит во внешний ключ,
// поэтому одинаковый номер задачи из таблицы и из Jira — разные ключи.
const SourceTable = "table"

// Build раскладывает строки таблицы по полям карточек.
//
// Отказ целиком — только когда перенести нельзя ничего: не назначен
// заголовок или одно поле назначено двум колонкам. Всё остальное —
// претензия к строке, а строка либо едет без негодного поля, либо
// не едет, и остальные от этого не страдают.
func Build(t Table, m Mapping) (Plan, error) {
	if len(m) != len(t.Headers) {
		return Plan{}, fmt.Errorf("сопоставление описывает %d колонок, а в файле их %d", len(m), len(t.Headers))
	}
	col := map[Field]int{}
	for i, f := range m {
		if !Known(f) {
			return Plan{}, fmt.Errorf("незнакомое поле %q", f)
		}
		if f == FieldNone {
			continue
		}
		if j, ok := col[f]; ok {
			return Plan{}, fmt.Errorf("колонки «%s» и «%s» назначены одному полю — оставьте одну", t.Headers[j], t.Headers[i])
		}
		col[f] = i
	}
	if _, ok := col[FieldTitle]; !ok {
		return Plan{}, fmt.Errorf("назначьте колонку с заголовком карточки: без заголовка карточку не завести")
	}

	p := Plan{Source: SourceTable, Rows: len(t.Rows)}
	cell := func(row []string, f Field) string {
		i, ok := col[f]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	// Даты понимаются колонкой целиком, а не каждая по отдельности:
	// 03.04 в одной строке и 13.04 в другой — один и тот же порядок
	// дня и месяца, и решать его построчно значит разложить одну
	// колонку двумя способами.
	formats := map[Field]string{}
	for _, f := range []Field{FieldDue, FieldCreated, FieldDone} {
		i, ok := col[f]
		if !ok {
			continue
		}
		var values []string
		for _, row := range t.Rows {
			if i < len(row) && strings.TrimSpace(row[i]) != "" {
				values = append(values, strings.TrimSpace(row[i]))
			}
		}
		if format := dateFormatOf(values); format != "" {
			formats[f] = format
			p.Dates = append(p.Dates, DateFormat{Field: f, Header: t.Headers[i], Format: format})
		}
	}

	seenColumn := map[string]bool{}
	seenKey := map[string]int{}
	for n, row := range t.Rows {
		// Номер — тот, что человек видит в Excel. Без записанных номеров
		// строки идут подряд за заголовками: первая строка задач — вторая.
		line := n + 2
		if n < len(t.Lines) {
			line = t.Lines[n]
		}
		problem := func(f Field, value, format string, args ...any) {
			p.Problems = append(p.Problems, Problem{Row: line, Field: f, Value: value, Message: fmt.Sprintf(format, args...)})
		}

		c := Card{Row: line, Title: cell(row, FieldTitle)}
		if c.Title == "" {
			p.Problems = append(p.Problems, Problem{Row: line, Field: FieldTitle, Skipped: true,
				Message: "нет заголовка — заголовок обязателен, строка не переносится"})
			continue
		}
		c.Column = cell(row, FieldColumn)
		if c.Column != "" && !seenColumn[strings.ToLower(c.Column)] {
			seenColumn[strings.ToLower(c.Column)] = true
			p.Columns = append(p.Columns, c.Column)
		}
		c.Description = cell(row, FieldDescription)

		for _, a := range split(cell(row, FieldAssignees)) {
			if !strings.Contains(a, "@") {
				problem(FieldAssignees, a, "«%s» — не почта: исполнитель находится только по почте", a)
				continue
			}
			c.Assignees = append(c.Assignees, strings.ToLower(a))
		}
		c.Labels = split(cell(row, FieldLabels))

		if raw := cell(row, FieldEstimate); raw != "" {
			v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
			switch {
			case err != nil || math.IsNaN(v) || math.IsInf(v, 0):
				problem(FieldEstimate, raw, "оценка «%s» — не число, карточка переедет без оценки", raw)
			case v <= 0 || v >= 1e8:
				problem(FieldEstimate, raw, "оценка «%s» — должна быть больше нуля, карточка переедет без оценки", raw)
			default:
				v = math.Round(v*100) / 100
				c.Estimate = &v
			}
		}

		if raw := cell(row, FieldPriority); raw != "" {
			if pr, ok := priorityWords[strings.ToLower(raw)]; ok {
				c.Priority = pr
			} else {
				problem(FieldPriority, raw, "приоритет «%s» незнаком — карточка переедет с обычным", raw)
			}
		}

		for _, date := range []struct {
			f   Field
			dst **time.Time
		}{{FieldDue, &c.Due}, {FieldCreated, &c.Created}, {FieldDone, &c.Done}} {
			f, dst := date.f, date.dst
			raw := cell(row, f)
			if raw == "" {
				continue
			}
			d, ok := parseDate(raw, formats[f])
			if !ok {
				problem(f, raw, "дата «%s» не понята — поле не переносится", raw)
				continue
			}
			*dst = &d
		}

		// Ключ, по которому второй прогон узнаёт перенесённое. Свой ключ
		// из файла надёжнее всего; без него — заголовок, а одинаковые
		// заголовки нумеруются по порядку появления.
		key := cell(row, FieldExternal)
		if key == "" {
			key = "title:" + strings.ToLower(c.Title)
		}
		seenKey[key]++
		if seenKey[key] > 1 {
			if _, own := col[FieldExternal]; own && !strings.HasPrefix(key, "title:") {
				p.Problems = append(p.Problems, Problem{Row: line, Field: FieldExternal, Value: key, Skipped: true,
					Message: fmt.Sprintf("ключ «%s» уже встречался выше — строка не переносится", key)})
				continue
			}
			key += "#" + strconv.Itoa(seenKey[key])
		}
		c.ExternalID = key
		p.Cards = append(p.Cards, c)
	}
	if name, ok := exportLimits[len(t.Rows)]; ok {
		p.Lost = append(p.Lost, fmt.Sprintf("в файле ровно %d строк — столько за раз отдаёт выгрузка %s; если задач в источнике больше, остальные в файл не попали — выгрузите их отдельным файлом", len(t.Rows), name))
	}
	return p, nil
}

// exportLimits — на скольких строках обрезают выгрузку известные
// платформы, молча: CSV облачной Jira — на тысяче задач, xlsx monday —
// на десяти тысячах элементов. Файл ровно такой длины — не удача,
// а повод спросить, всё ли приехало (ROADMAP 23.5); сказать это
// можно только до переноса.
var exportLimits = map[int]string{
	1000:  "CSV облачной Jira",
	10000: "Excel из monday",
}

// split делит перечисление: запятая, точка с запятой, перевод строки.
func split(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Три вида дат — то, как их пишут выгрузки на самом деле: ISO (Google
// Таблицы, YouGile, наш экспорт), через точки (Excel в русской
// локали, Kaiten) и «22/Sep/26 10:26 AM» (CSV из Jira).
var dateLayouts = map[string][]string{
	"iso": {"2006-01-02", "2006-01-02 15:04", "2006-01-02 15:04:05",
		"2006-01-02T15:04:05", time.RFC3339},
	"dotted": {"02.01.2006", "2.1.2006", "02.01.2006 15:04", "2.1.2006 15:04",
		"02.01.2006 15:04:05", "02.01.06"},
	"jira": {"02/Jan/06 3:04 PM", "2/Jan/06 3:04 PM", "02/Jan/06 15:04", "02/Jan/06", "2/Jan/06"},
}

// Четвёртый вид — число: так Excel хранит дату в ячейке, а в текст
// её превращает стиль, который мы не читаем. Дни от 30.12.1899,
// дробная часть — время суток.
var dateOrder = []string{"iso", "dotted", "jira", "excel"}

// Excel считает дни от 30.12.1899 (с ошибкой 1900 года, которую эта
// точка отсчёта и учитывает); 2958465 — 31.12.9999, его последний день.
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// ExcelDate читает дату ячейки Excel (дни от 30.12.1899).
func ExcelDate(s string) (time.Time, bool) { return parseExcelDate(s) }

func parseExcelDate(s string) (time.Time, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 1 || v > 2958465 {
		return time.Time{}, false
	}
	days := math.Floor(v)
	secs := math.Round((v - days) * 86400)
	return excelEpoch.AddDate(0, 0, int(days)).Add(time.Duration(secs) * time.Second), true
}

// dateFormatOf выбирает вид, которым читается больше всего значений.
func dateFormatOf(values []string) string {
	best, count := "", 0
	for _, f := range dateOrder {
		n := 0
		for _, v := range values {
			if _, ok := parseDate(v, f); ok {
				n++
			}
		}
		if n > count {
			best, count = f, n
		}
	}
	return best
}

func parseDate(s, format string) (time.Time, bool) {
	if format == "excel" {
		return parseExcelDate(s)
	}
	for _, layout := range dateLayouts[format] {
		if d, err := time.Parse(layout, s); err == nil {
			return d.UTC(), true
		}
	}
	return time.Time{}, false
}

// ColumnKind — какой колонкой работы считать колонку файла: queue,
// in_progress, done или пусто — название незнакомо. Незнакомое
// обычно стадия посередине — «Ревью», «На согласовании», — и решает,
// куда её поставить, тот, кто спрашивает.
func ColumnKind(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case doneWords[n]:
		return "done"
	case progressWords[n]:
		return "in_progress"
	case queueWords[n]:
		return "queue"
	}
	return ""
}
