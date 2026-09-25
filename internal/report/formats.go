package report

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/findias/takt/internal/i18n"
)

// Три формата из одного набора строк. XLSX — людям: плоский лист
// и сводка с диаграммами. CSV — тем же людям, у кого не Excel.
// JSON — для обработки: поля как в API, значения — коды, а не слова.

// Колонки листа «Данные». Названия — на языке запроса: файл читает
// человек, и английскому посетителю незачем разбирать русскую шапку.
var dataColumns = []struct {
	ru    string
	width float64
}{
	{"Номер", 11}, {"Название", 48}, {"Доска", 20}, {"Подразделение", 18},
	{"Колонка", 16}, {"Состояние", 12}, {"Приоритет", 12}, {"Оценка", 9},
	{"Исполнители", 24}, {"Метки", 24}, {"Итерация", 16}, {"Родитель", 11}, {"Эпик", 24},
	{"Заявки", 22},
	{"Заведена", 17}, {"Начата", 17}, {"Закончена", 17}, {"Срок", 11},
	{"Время цикла, дней", 12}, {"Возраст, дней", 12}, {"Заблокирована", 12},
	{"Причина блокировки", 28}, {"Перенесена", 11}, {"В архиве", 10},
	// Описание — последним: самое длинное, и в конце таблицы оно
	// не раздвигает строки между столбцами, которые сравнивают.
	{"Описание", 60},
}

func header(ctx context.Context) ([]string, []float64) {
	names := make([]string, len(dataColumns))
	widths := make([]float64, len(dataColumns))
	for i, c := range dataColumns {
		names[i] = i18n.Name(ctx, c.ru)
		widths[i] = c.width
	}
	return names, widths
}

// words — слова для кодов состояния и приоритета, те же, что на доске.
type words struct {
	state    map[string]string
	priority map[string]string
	refKind  map[string]string
	yes      string
}

// refs — заявки одной строкой: «RDS 12345; Проблема PRB-7». Точка
// с запятой, а не запятая: в номере или адресе заявки запятая бывает.
func (w words) refs(list []Ref) string {
	parts := make([]string, len(list))
	for i, r := range list {
		parts[i] = w.refKind[r.Kind] + " " + r.Ref
	}
	return strings.Join(parts, "; ")
}

func wordsFor(ctx context.Context) words {
	n := func(ru string) string { return i18n.Name(ctx, ru) }
	return words{
		state: map[string]string{
			StateQueued: n("в очереди"), StateActive: n("в работе"),
			StateDone: n("сделано"), StateDiscarded: n("отброшено"),
		},
		priority: map[string]string{
			"highest": n("Наивысший"), "high": n("Высокий"),
			"medium": n("Средний"), "low": n("Низкий"),
		},
		// Названия видов — те же, что в разделе «Заявки» карточки.
		refKind: map[string]string{
			"rds": "RDS", "zno": n("ЗНО"), "zni": n("ЗНИ"), "problem": n("Проблема"),
		},
		yes: n("да"),
	}
}

func (w words) flag(b bool) string {
	if b {
		return w.yes
	}
	return ""
}

// ---- CSV ----

type csvSink struct {
	w     *csv.Writer
	words words
}

// NewCSV — плоская таблица. Метка порядка байтов впереди: без неё
// Excel открывает UTF-8 как Windows-1251, и кириллица превращается
// в кракозябры — первое, что увидит руководитель.
func NewCSV(ctx context.Context, w io.Writer) (Sink, error) {
	if _, err := io.WriteString(w, "\uFEFF"); err != nil {
		return nil, err
	}
	s := &csvSink{w: csv.NewWriter(w), words: wordsFor(ctx)}
	names, _ := header(ctx)
	return s, s.w.Write(names)
}

// cellText обезвреживает текст, который табличный редактор принял бы
// за формулу: название карточки «=HYPERLINK(...)» из чужой системы
// иначе становится ссылкой в книге руководителя (CSV injection).
func cellText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func csvStamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04")
}

func csvDays(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', 1, 64)
}

func (s *csvSink) Row(r Row) error {
	est := ""
	if r.Estimate != nil {
		est = strconv.FormatFloat(*r.Estimate, 'f', -1, 64)
	}
	due := ""
	if r.DueOn != nil {
		due = *r.DueOn
	}
	return s.w.Write([]string{
		r.Number, cellText(r.Title), cellText(r.Board), cellText(r.Team), cellText(r.Column),
		s.words.state[r.State], s.words.priority[r.Priority], est,
		cellText(r.Assignees), cellText(r.Labels), cellText(r.Iteration), r.Parent, cellText(r.Epic),
		cellText(s.words.refs(r.Refs)), csvStamp(&r.CreatedAt), csvStamp(r.StartedAt), csvStamp(r.FinishedAt), due,
		csvDays(r.CycleDays), csvDays(r.AgeDays), s.words.flag(r.Blocked),
		cellText(r.BlockReason), s.words.flag(r.Imported), s.words.flag(r.Archived),
		cellText(r.Description),
	})
}

// Summary у CSV нет: плоский файл — одна таблица, а сводка с
// диаграммами — в XLSX.
func (s *csvSink) Summary(Summary) error {
	s.w.Flush()
	return s.w.Error()
}

// ---- JSON ----

type jsonSink struct {
	w     *bufio.Writer
	first bool
}

// NewJSON пишет {"from", "to", "limit", "cards": [...], "summary": {...}}
// по мере прихода строк.
func NewJSON(w io.Writer, f Filter) (Sink, error) {
	b := bufio.NewWriterSize(w, 32<<10)
	head, err := json.Marshal(struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Limit int    `json:"limit"`
	}{f.From.Format(time.DateOnly), f.To.Format(time.DateOnly), MaxRows})
	if err != nil {
		return nil, err
	}
	b.Write(head[:len(head)-1]) // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	_, err = b.WriteString(`,"cards":[`)
	return &jsonSink{w: b, first: true}, err
}

func (s *jsonSink) Row(r Row) error {
	if !s.first {
		s.w.WriteByte(',') // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	}
	s.first = false
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.w.Write(body)
	return err
}

func (s *jsonSink) Summary(sum Summary) error {
	body, err := json.Marshal(sum)
	if err != nil {
		return err
	}
	s.w.WriteString(`],"summary":`) // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	s.w.Write(body)                 // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	s.w.WriteString("}\n")          // #nosec G104 -- bufio.Writer запоминает первую ошибку записи и отдаёт её из Flush, а Flush проверяется
	return s.w.Flush()
}

// ---- XLSX ----

type xlsxSink struct {
	ctx   context.Context
	book  *xlsxBook
	words words
	f     Filter
	now   time.Time
}

// NewXLSX — книга: «Сводка» первым листом, «Данные» вторым.
func NewXLSX(ctx context.Context, w io.Writer, f Filter) (Sink, error) {
	names, widths := header(ctx)
	book, err := newXLSXBook(w, i18n.Name(ctx, "Сводка"), i18n.Name(ctx, "Данные"), names, widths)
	if err != nil {
		return nil, err
	}
	return &xlsxSink{ctx: ctx, book: book, words: wordsFor(ctx), f: f, now: time.Now()}, nil
}

func (s *xlsxSink) Row(r Row) error {
	est := cell{}
	if r.Estimate != nil {
		est = num(*r.Estimate)
	}
	return s.book.dataRow(
		str(r.Number), str(r.Title), str(r.Board), str(r.Team), str(r.Column),
		str(s.words.state[r.State]), str(s.words.priority[r.Priority]), est,
		str(r.Assignees), str(r.Labels), str(r.Iteration), str(r.Parent), str(r.Epic),
		str(s.words.refs(r.Refs)), stamp(r.CreatedAt), optStamp(r.StartedAt), optStamp(r.FinishedAt), optISODay(r.DueOn),
		optDec(r.CycleDays), optDec(r.AgeDays), str(s.words.flag(r.Blocked)),
		str(r.BlockReason), str(s.words.flag(r.Imported)), str(s.words.flag(r.Archived)),
		str(r.Description),
	)
}

// Summary раскладывает лист сводки: сверху — что это за файл и
// главные числа, ниже — таблицы, справа от двух из них — диаграммы
// по их же ячейкам.
func (s *xlsxSink) Summary(sum Summary) error {
	n := func(ru string) string { return i18n.Name(s.ctx, ru) }
	build := func(w *sheetRows) (charts [2]chartSpec) {
		w.row(cell{kind: 's', s: n("Выгрузка карточек"), style: styleTitle})
		w.row(str(n("Период")), day(s.f.From), day(s.f.To))
		w.row(str(n("Собрано")), stamp(s.now))
		chosen := []struct {
			ru   string
			list []string
		}{
			{"Доски", sum.Chosen.Boards}, {"Подразделения", sum.Chosen.Teams},
			{"Исполнители", sum.Chosen.Assignees}, {"Метки", sum.Chosen.Labels},
		}
		for _, c := range chosen {
			if len(c.list) > 0 {
				w.row(str(n(c.ru)), str(strings.Join(c.list, ", ")))
			}
		}
		if sum.Chosen.Iteration != "" {
			w.row(str(n("Итерация")), str(sum.Chosen.Iteration))
		}
		if len(sum.Chosen.Priorities) > 0 {
			w.row(str(n("Приоритет")), str(s.join(sum.Chosen.Priorities, s.words.priority)))
		}
		if len(sum.Chosen.States) > 0 {
			w.row(str(n("Состояние")), str(s.join(sum.Chosen.States, s.words.state)))
		}
		if s.f.WithArchive {
			w.row(str(n("С карточками из архива")))
		}
		w.blank()

		w.row(bold(n("Карточек в выгрузке")), num(float64(sum.Cards)))
		w.row(str(n("Сделано за период")), num(float64(sum.Finished)))
		w.row(str(n("Отброшено за период")), num(float64(sum.Discarded)))
		w.row(str(n("В работе сейчас")), num(float64(sum.WIP)))
		if sum.CycleTime != nil {
			w.row(str(n("Время цикла, медиана, дней")), dec(sum.CycleTime.P50))
			w.row(str(n("Время цикла, 85-я процентиль, дней")), dec(sum.CycleTime.P85))
		}
		if sum.Age != nil {
			w.row(str(n("Возраст незавершённого, медиана, дней")), dec(sum.Age.P50))
			w.row(str(n("Возраст незавершённого, 85-я процентиль, дней")), dec(sum.Age.P85))
		}
		w.blank()

		// Пропускная способность: таблица и столбцы рядом с ней.
		w.row(bold(n("Пропускная способность по неделям")))
		w.row(bold(n("Неделя с")), bold(n("Сделано")))
		first := w.n + 1
		for _, wk := range sum.Throughput {
			w.row(isoDay(wk.Week), num(float64(wk.Count)))
		}
		// Недель и дней в периоде всегда хотя бы по одной: период
		// не короче дня, и ряды диаграмм не бывают пустыми.
		charts[0] = chartSpec{kind: "bar", title: n("Сделано по неделям"),
			firstRow: first, lastRow: w.n, catCol: 0, valCols: []int{1},
			colors: []string{"3F7D6B"}, anchor: [2]int{chartCol, first - 2}}
		nextFree := first - 2 + chartRows + 1 // первая строка под диаграммой, с 0
		w.blank()

		// Накопительная: три полосы стопкой, как на «Потоке».
		w.row(bold(n("Накопительная диаграмма потока")))
		stepWord := n("День")
		if sum.FlowStep == "week" {
			stepWord = n("Конец недели")
		}
		w.row(bold(stepWord), bold(n("Сделано")), bold(n("В работе")), bold(n("В очереди")))
		first = w.n + 1
		for _, d := range sum.Flow {
			w.row(isoDay(d.Day), num(float64(d.Done)), num(float64(d.InProgress)), num(float64(d.Queued)))
		}
		charts[1] = chartSpec{kind: "area", title: n("Накопительная диаграмма потока"),
			firstRow: first, lastRow: w.n, catCol: 0, valCols: []int{1, 2, 3},
			colors: []string{"3F7D6B", "D8A13A", "A9B4B0"},
			anchor: [2]int{chartCol, max(first-2, nextFree)}}
		nextFree = max(first-2, nextFree) + chartRows + 1
		w.blank()
		// Таблицы ниже шире двух колонок и ушли бы под диаграммы.
		for w.n < nextFree {
			w.blank()
		}

		if len(sum.Iterations) > 0 {
			w.row(bold(n("Выполнение итераций")))
			w.row(bold(n("Итерация")), bold(n("Доска")), bold(n("Начало")), bold(n("Конец")),
				bold(n("Взято")), bold(n("Сделано")), bold(n("Доля")))
			for _, it := range sum.Iterations {
				share := cell{}
				if it.Planned > 0 {
					share = pct(float64(it.Done) / float64(it.Planned))
				}
				w.row(str(it.Name), str(it.Board), isoDay(it.StartsOn), isoDay(it.EndsOn),
					num(float64(it.Planned)), num(float64(it.Done)), share)
			}
			w.blank()
		}

		w.row(bold(n("По подразделениям")))
		w.row(bold(n("Подразделение")), bold(n("Карточек")), bold(n("Сделано")),
			bold(n("В работе")), bold(n("Цикл, медиана")), bold(n("Цикл, 85-я")))
		for _, t := range sum.Teams {
			name := t.Team
			if name == "" {
				name = n("Без подразделения")
			}
			w.row(str(name), num(float64(t.Cards)), num(float64(t.Finished)),
				num(float64(t.WIP)), optDec(t.CycleP50), optDec(t.CycleP85))
		}
		return charts
	}
	return s.book.finish(build, []float64{46, 17, 14, 14, 10, 10, 10})
}

// chartCol — колонка, с которой начинаются диаграммы: правее таблиц
// пропускной способности и потока (они в четыре колонки).
const chartCol = 5

func (s *xlsxSink) join(codes []string, dict map[string]string) string {
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = dict[c]
	}
	return strings.Join(out, ", ")
}
