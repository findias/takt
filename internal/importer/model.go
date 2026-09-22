// Package importer — переезд к нам из чужих досок и таблиц (ROADMAP, этап 23).
//
// Источники разные, а работа одна: разобрать чужое, сопоставить своему,
// завести. Поэтому посередине стоит промежуточная модель — карточки
// с полями, которые у нас есть, — а источник только её поставщик.
// Иначе третий источник перепишет второй.
//
// Пакет ничего не знает о базе. Разбор файла и сборка карточек —
// здесь и проверяются без неё; заводит доску и карточки internal/board,
// и делает это дважды: на сухом прогоне в транзакции, которая
// откатывается, и по-настоящему. Так предпросмотр показывает ровно
// то, что случится, — вплоть до людей, которых не нашли по почте.
package importer

import "time"

// Field — поле карточки, в которое ложится колонка файла.
type Field string

const (
	// Пусто — колонка файла не переносится.
	FieldNone        Field = ""
	FieldTitle       Field = "title"
	FieldColumn      Field = "column"
	FieldAssignees   Field = "assignees"
	FieldLabels      Field = "labels"
	FieldEstimate    Field = "estimate"
	FieldPriority    Field = "priority"
	FieldDue         Field = "due"
	FieldCreated     Field = "created"
	FieldDone        Field = "done"
	FieldDescription Field = "description"
	FieldExternal    Field = "external"
)

// Fields — все поля, в порядке, в котором их предлагает экран.
var Fields = []Field{
	FieldTitle, FieldColumn, FieldAssignees, FieldLabels, FieldEstimate,
	FieldPriority, FieldDue, FieldCreated, FieldDone, FieldDescription, FieldExternal,
}

// Known — есть ли такое поле.
func Known(f Field) bool {
	if f == FieldNone {
		return true
	}
	for _, x := range Fields {
		if x == f {
			return true
		}
	}
	return false
}

// Table — таблица как есть: заголовки и строки текста. Номер строки
// для человека — номер в файле, считая строку заголовков первой.
type Table struct {
	Headers []string
	Rows    [][]string
	// Номер каждой строки в файле: пустые строки пропускаются, и без
	// него «строка 14» в отчёте указала бы не туда. Пусто — строки
	// идут подряд сразу за заголовками.
	Lines []int
}

// Mapping — какой колонке файла какое поле. Длина равна числу
// заголовков; пустое поле — колонка не переносится.
type Mapping []Field

// Card — карточка промежуточной модели.
type Card struct {
	// Строка файла — чтобы отчёт мог назвать её.
	Row         int
	Title       string
	Column      string
	Description string
	// Ключ, по которому второй прогон узнаёт уже перенесённое.
	ExternalID string
	// Number — номер в источнике для человека («DEV-12»); пусто — ключ
	// и есть номер (таблица) или номера у источника нет.
	Number string
	// Почты: человек сопоставляется по почте, и только по ней.
	Assignees []string
	Labels    []string
	Estimate  *float64
	// low, medium, high, highest; пусто — как у доски по умолчанию.
	Priority string
	Due      *time.Time
	Created  *time.Time
	Done     *time.Time

	// То, чего в таблице нет, а в пакете переноса есть (docs/import-package.md).
	// Parent — внешний ключ родителя: карточка станет его частью.
	Parent   string
	Links    []Link
	Comments []Comment
	// History — история задачи в источнике (системные сообщения), текстом.
	History []Comment
	// Unmatched — исполнители, у которых источник не дал почты: найти
	// их нельзя, но назвать в отчёте нужно.
	Unmatched []Unmatched
}

// Unmatched — исполнитель без почты. Key — его идентификатор в источнике:
// двух тёзок различает только он, а метка человека (0062) у каждого своя.
type Unmatched struct {
	Key  string
	Name string
}

// Link — связь с другой карточкой того же переноса: blocks или relates.
type Link struct {
	Kind string
	To   string
}

// Comment — реплика обсуждения. Автор ищется по почте; не найден —
// реплика пишется от имени переносящего и начинается с имени автора.
type Comment struct {
	// AuthorKey — ключ автора, как у исполнителя: почта или
	// source:<идентификатор>; по нему автора находит выбор по людям.
	AuthorKey   string
	AuthorEmail string
	AuthorName  string
	At          time.Time
	Text        string
}

// Problem — что в строке не так. Плохая строка не роняет импорт:
// она либо переезжает без негодного поля, либо не переезжает вовсе
// (нет заголовка), и то и другое называется в отчёте.
type Problem struct {
	Row     int    `json:"row"`
	Field   Field  `json:"field,omitempty"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message"`
	// Строка не переедет совсем.
	Skipped bool `json:"skipped"`
}

// DateFormat — как понята колонка дат. Говорится вслух: 03.04 —
// это третье апреля или четвёртое марта, решает файл, а не мы,
// и человек обязан это увидеть до переноса.
type DateFormat struct {
	Field  Field  `json:"field"`
	Header string `json:"header"`
	// iso, dotted, jira, excel.
	Format string `json:"format"`
}

// Plan — разобранный файл: карточки, претензии к строкам и колонки
// доски в порядке первого появления.
type Plan struct {
	Source   string
	Cards    []Card
	Problems []Problem
	Columns  []string
	Dates    []DateFormat
	Rows     int
	// SourceName — как источник назвать человеку: «из YouGile: Иван Петров».
	SourceName string
	// ColumnKinds — разметка колонок, которую источник знает сам
	// (пакет переноса); пусто — угадывается по названию.
	ColumnKinds map[string]string
	// Lost — что источник знает, а мы не переносим, словами для отчёта.
	// Молчаливая потеря хуже названной: «чат и файлы остались в YouGile»
	// человек должен прочитать до переноса, а не обнаружить после.
	Lost []string
	// Names — имя человека источника по его почте. Нужно метке человека:
	// не найденного по почте называют на карточке по имени, а почту
	// таблица даёт без имени — тогда меткой служит сама почта.
	Names map[string]string
	// HistoryCollected — история задач собрана вместе с планом (пакет
	// с историей): дотягивать её потом не нужно, даже пустую.
	HistoryCollected bool
}
