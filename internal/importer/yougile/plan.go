package yougile

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer"
)

// ErrTooBig — задач больше, чем переносим за раз.
var ErrTooBig = fmt.Errorf("на доске YouGile больше %d задач — перенесите её таблицей по частям", importer.MaxRows)

// Source — откуда карточка: YouGile. Входит во внешний ключ, и ключ
// задачи там — её идентификатор, одинаковый во всех выгрузках.
const Source = "yougile"

type task struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	ColumnID           string            `json:"columnId"`
	Assigned           []string          `json:"assigned"`
	Completed          bool              `json:"completed"`
	CompletedTimestamp int64             `json:"completedTimestamp"`
	Archived           bool              `json:"archived"`
	Deleted            bool              `json:"deleted"`
	Timestamp          int64             `json:"timestamp"`
	Subtasks           []string          `json:"subtasks"`
	Stickers           map[string]string `json:"stickers"`
	Deadline           *struct {
		Deadline int64 `json:"deadline"`
	} `json:"deadline"`
	Checklists []struct {
		Title string `json:"title"`
		Items []struct {
			Title       string `json:"title"`
			IsCompleted bool   `json:"isCompleted"`
		} `json:"items"`
	} `json:"checklists"`
}

type sticker struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
	States  []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Deleted bool   `json:"deleted"`
	} `json:"states"`
}

// Plan собирает доску YouGile в промежуточную модель.
//
// Переносим: колонки, задачи, исполнителей по почте, дедлайн как срок,
// даты заведения и завершения, стикеры как метки («Стикер: значение»;
// стикер приоритета — приоритетом), чек-листы — в описание.
// Не переносим осознанно, и отчёт называет это списком: задачи
// из архива YouGile, подзадачи, чат, файлы, права доступа, учёт
// времени. Молчаливая потеря хуже названной.
func (c *Client) Plan(ctx context.Context, boardID string) (importer.Plan, error) {
	type column struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		BoardID string `json:"boardId"`
		Deleted bool   `json:"deleted"`
	}
	cols, err := all[column](ctx, c, "columns", url.Values{"boardId": {boardID}})
	if err != nil {
		return importer.Plan{}, err
	}
	// Фильтр по доске перепроверяем сами: пропусти YouGile параметр —
	// на доску приехали бы колонки всей компании.
	var live []column
	for _, col := range cols {
		if col.BoardID == boardID && !col.Deleted {
			live = append(live, col)
		}
	}
	if len(live) == 0 {
		return importer.Plan{}, ErrNotFound
	}

	type user struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	users, err := all[user](ctx, c, "users", nil)
	if err != nil {
		return importer.Plan{}, err
	}
	emails := map[string]string{}
	for _, u := range users {
		emails[u.ID] = strings.ToLower(strings.TrimSpace(u.Email))
	}

	labels, priority, err := c.stickers(ctx)
	if err != nil {
		return importer.Plan{}, err
	}

	plan := importer.Plan{Source: Source}
	var archived, withSubtasks int
	for _, col := range live {
		tasks, err := all[task](ctx, c, "tasks", url.Values{"columnId": {col.ID}})
		if err != nil {
			return importer.Plan{}, err
		}
		for _, t := range tasks {
			if t.ColumnID != col.ID || t.Deleted {
				continue
			}
			plan.Rows++
			if plan.Rows > importer.MaxRows {
				return importer.Plan{}, ErrTooBig
			}
			if t.Archived {
				archived++
				continue
			}
			if len(t.Subtasks) > 0 {
				withSubtasks++
			}
			card := importer.Card{
				Row:         plan.Rows,
				Title:       strings.TrimSpace(t.Title),
				Column:      strings.TrimSpace(col.Title),
				Description: description(t),
				ExternalID:  t.ID,
				Created:     millis(t.Timestamp),
			}
			if card.Title == "" {
				plan.Problems = append(plan.Problems, importer.Problem{Row: plan.Rows, Field: importer.FieldTitle, Skipped: true,
					Message: "задача без названия — не переносится"})
				continue
			}
			if t.Completed {
				card.Done = millis(t.CompletedTimestamp)
			}
			if t.Deadline != nil {
				card.Due = millis(t.Deadline.Deadline)
			}
			for _, id := range t.Assigned {
				if e := emails[id]; e != "" {
					card.Assignees = append(card.Assignees, e)
				}
			}
			for sid, state := range t.Stickers {
				if p := priority[sid][state]; p != "" {
					card.Priority = p
				} else if l := labels[sid][state]; l != "" {
					card.Labels = append(card.Labels, l)
				}
			}
			// Порядок стикеров у YouGile — порядок ключей словаря, то есть
			// никакой; метки ставим по алфавиту, чтобы два прогона
			// одного и того же давали одно и то же.
			sort.Strings(card.Labels)
			plan.Cards = append(plan.Cards, card)
		}
	}
	// Одноимённые колонки у YouGile бывают; у нас колонка ищется
	// по названию, и две «Готово» слились бы в одну — так и заводим.
	seen := map[string]bool{}
	for _, col := range live {
		name := strings.TrimSpace(col.Title)
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			plan.Columns = append(plan.Columns, name)
		}
	}

	// Список того, что не едет, — всегда, а числа — где они есть.
	if archived > 0 {
		plan.Lost = append(plan.Lost, fmt.Sprintf("задачи из архива YouGile: %d", archived))
	}
	if withSubtasks > 0 {
		plan.Lost = append(plan.Lost, fmt.Sprintf("подзадачи (задач с ними: %d) — части переносятся следующим срезом", withSubtasks))
	}
	plan.Lost = append(plan.Lost, "чат задач, файлы, права доступа и учёт времени — их в карточке нет")
	return plan, nil
}

// stickers — стикеры компании так, как их переносим: стикер
// приоритета — нашим приоритетом, остальные — метками «Стикер:
// значение». Одно на оба пути — экран и выгрузчик.
func (c *Client) stickers(ctx context.Context) (labels, priority map[string]map[string]string, err error) {
	labels, priority = map[string]map[string]string{}, map[string]map[string]string{}
	stickers, err := all[sticker](ctx, c, "string-stickers", nil)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range stickers {
		if s.Deleted {
			continue
		}
		labels[s.ID] = map[string]string{}
		isPriority := importer.IsPriorityName(s.Name)
		for _, st := range s.States {
			if st.Deleted {
				continue
			}
			if p, ok := importer.PriorityOf(st.Name); ok && isPriority {
				if priority[s.ID] == nil {
					priority[s.ID] = map[string]string{}
				}
				priority[s.ID][st.ID] = p
				continue
			}
			labels[s.ID][st.ID] = strings.TrimSpace(s.Name) + ": " + strings.TrimSpace(st.Name)
		}
	}
	return labels, priority, nil
}

func millis(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

var (
	breaks = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>`)
	tags   = regexp.MustCompile(`<[^>]*>`)
	blank  = regexp.MustCompile(`\n{3,}`)
)

// description — описание текстом: у YouGile оно в HTML его редактора,
// а у нас текст. Чек-листы дописываются следом списком с отметками —
// в карточке своего места для них нет, а терять их нельзя.
func description(t task) string {
	text := breaks.ReplaceAllString(t.Description, "\n")
	text = html.UnescapeString(tags.ReplaceAllString(text, ""))
	text = strings.TrimSpace(blank.ReplaceAllString(text, "\n\n"))
	var b strings.Builder
	b.WriteString(text)
	for _, cl := range t.Checklists {
		if len(cl.Items) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		if cl.Title != "" {
			b.WriteString(strings.TrimSpace(cl.Title) + ":\n")
		}
		for _, it := range cl.Items {
			mark := "[ ]"
			if it.IsCompleted {
				mark = "[x]"
			}
			b.WriteString("- " + mark + " " + strings.TrimSpace(it.Title) + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}
