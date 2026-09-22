package importer

import "strings"

// headerWords — как поле называют в заголовках чужих выгрузок: наши
// собственные подписи на обоих языках, Jira, YouGile, Kaiten, Weeek,
// Asana. Это только предложение — экран показывает его, человек
// правит. Угадывать по одному образцу файла нельзя (ROADMAP 23.5),
// поэтому словарь держится коротким: лучше не предложить, чем
// предложить не то.
var headerWords = map[string]Field{
	"заголовок": FieldTitle, "название": FieldTitle, "задача": FieldTitle,
	"title": FieldTitle, "summary": FieldTitle, "name": FieldTitle, "task name": FieldTitle,

	"колонка": FieldColumn, "статус": FieldColumn, "этап": FieldColumn,
	"column": FieldColumn, "status": FieldColumn, "section/column": FieldColumn, "section": FieldColumn,

	"исполнитель": FieldAssignees, "исполнители": FieldAssignees, "ответственный": FieldAssignees,
	"assignee": FieldAssignees, "assignees": FieldAssignees, "assignee email": FieldAssignees,

	"метки": FieldLabels, "метка": FieldLabels, "теги": FieldLabels, "стикеры": FieldLabels,
	"labels": FieldLabels, "label": FieldLabels, "tags": FieldLabels,

	"оценка": FieldEstimate, "story points": FieldEstimate, "estimate": FieldEstimate,
	"story point estimate": FieldEstimate,

	"приоритет": FieldPriority, "priority": FieldPriority,

	"срок": FieldDue, "дедлайн": FieldDue, "due": FieldDue, "due date": FieldDue, "deadline": FieldDue,

	"создана": FieldCreated, "дата создания": FieldCreated, "created": FieldCreated,
	"created at": FieldCreated, "created date": FieldCreated,

	"завершена": FieldDone, "дата завершения": FieldDone, "выполнена": FieldDone,
	"done": FieldDone, "resolved": FieldDone, "completed at": FieldDone, "done at": FieldDone,

	"описание": FieldDescription, "description": FieldDescription, "notes": FieldDescription,

	"ключ": FieldExternal, "внешний ключ": FieldExternal, "id": FieldExternal,
	"issue key": FieldExternal, "key": FieldExternal, "task id": FieldExternal,
}

// Suggest предлагает сопоставление по заголовкам. Каждое поле
// достаётся одной колонке — первой подходящей: две колонки «Статус»
// в одном файле значат, что вторую человек назначит сам.
func Suggest(headers []string) Mapping {
	m := make(Mapping, len(headers))
	taken := map[Field]bool{}
	for i, h := range headers {
		f, ok := headerWords[strings.ToLower(strings.TrimSpace(h))]
		if !ok || taken[f] {
			continue
		}
		m[i] = f
		taken[f] = true
	}
	return m
}

// priorityWords — приоритет чужими словами. Наши четыре ступени
// и то, как их называют Jira и русские трекеры; «Blocker» и «Critical»
// у Jira выше «Highest» не бывают у нас — это высшая ступень.
var priorityWords = map[string]string{
	"low": "low", "lowest": "low", "низкий": "low", "низший": "low",
	"medium": "medium", "normal": "medium", "средний": "medium", "обычный": "medium",
	"high": "high", "высокий": "high", "важный": "high",
	"highest": "highest", "critical": "highest", "blocker": "highest", "urgent": "highest",
	"наивысший": "highest", "срочный": "highest", "критический": "highest",
	// Как ступени называет стикер приоритета YouGile и русские таблицы
	// (перенос 22.09.2026: «Важно / Нормально / Не важно» уехали метками).
	"не важно": "low", "неважно": "low", "не срочно": "low", "низко": "low",
	"нормально": "medium", "средне": "medium",
	"важно": "high", "высоко": "high",
	"очень важно": "highest", "критично": "highest", "срочно": "highest", "блокер": "highest",
}

// Какой колонкой работы считать колонку файла. Доска без точки старта
// и финиша копит журнал, а метрики потока по нему не считает — поэтому
// у новой доски колонки размечаются сразу; ошиблись — правится
// на доске одной кнопкой, как у любой колонки.
var (
	doneWords = map[string]bool{
		"готово": true, "сделано": true, "выполнено": true, "закрыто": true, "завершено": true,
		"done": true, "closed": true, "resolved": true, "complete": true, "completed": true,
	}
	queueWords = map[string]bool{
		"очередь": true, "бэклог": true, "новые": true, "новая": true, "запланировано": true,
		"к выполнению": true, "идеи": true, "сделать": true, "нужно сделать": true,
		"backlog": true, "to do": true, "todo": true, "new": true, "open": true,
		"planned": true, "ideas": true, "queue": true,
	}
	progressWords = map[string]bool{
		"в работе": true, "в процессе": true, "делаем": true,
		"in progress": true, "doing": true, "in development": true,
	}
)

// Образец таблицы, который отдаёт экран импорта. Русский — с точкой
// с запятой, как его сохраняет Excel в русской локали; английский —
// с запятой. Заголовки — из словаря выше, поэтому образец
// сопоставляется сам, без правки на экране.
const (
	SampleRU = `Заголовок;Колонка;Исполнитель;Метки;Оценка;Приоритет;Срок;Создана;Завершена;Описание;Ключ
Согласовать макет главной;В работе;anna@example.test;дизайн;3;высокий;30.09.2026;01.09.2026;;Макет в двух вариантах;OLD-1
Перенести справочник клиентов;Очередь;boris@example.test, vera@example.test;данные, миграция;5;обычный;;02.09.2026;;;OLD-2
Настроить выгрузку отчёта;Готово;vera@example.test;;2;низкий;;20.08.2026;05.09.2026;;OLD-3
`
	SampleEN = `Title,Column,Assignee,Labels,Estimate,Priority,Due date,Created,Done,Description,Key
Agree the home page mock-up,In progress,anna@example.test,design,3,high,2026-09-30,2026-09-01,,Two variants of the mock-up,OLD-1
Move the customer directory,Queue,"boris@example.test, vera@example.test","data; migration",5,medium,,2026-09-02,,,OLD-2
Set up the report export,Done,vera@example.test,,2,low,,2026-08-20,2026-09-05,,OLD-3
`
)

// PriorityOf — наша ступень приоритета по чужому слову.
func PriorityOf(word string) (string, bool) {
	p, ok := priorityWords[strings.ToLower(strings.TrimSpace(word))]
	return p, ok
}

// IsPriorityName — называет ли поле источника приоритет: у YouGile
// приоритет — это стикер, и назван он как угодно.
func IsPriorityName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "приоритет", "важность", "срочность", "priority", "importance", "urgency":
		return true
	}
	return false
}
