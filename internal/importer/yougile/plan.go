package yougile

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/importer/pack"
)

// ErrTooBig — задач больше, чем переносим за раз.
var ErrTooBig = fmt.Errorf("на доске YouGile больше %d задач — перенесите её таблицей по частям", importer.MaxRows)

// Source — откуда карточка: YouGile. Входит во внешний ключ, и ключ
// задачи там — её идентификатор, одинаковый во всех выгрузках.
const Source = "yougile"

type task struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	ColumnID           string   `json:"columnId"`
	Assigned           []string `json:"assigned"`
	Completed          bool     `json:"completed"`
	CompletedTimestamp int64    `json:"completedTimestamp"`
	Archived           bool     `json:"archived"`
	Deleted            bool     `json:"deleted"`
	Timestamp          int64    `json:"timestamp"`
	// Номера задачи для людей: в проекте («DEV-12») и сквозной.
	IDTaskProject string            `json:"idTaskProject"`
	IDTaskCommon  string            `json:"idTaskCommon"`
	Subtasks      []string          `json:"subtasks"`
	Stickers      map[string]string `json:"stickers"`
	Deadline      *struct {
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

// Plan собирает доску YouGile в промежуточную модель — для переноса
// по API с экрана.
//
// Тем же путём, что и выгрузчик (Board), и тем же разбором, что пакет
// переноса: два пути к одной доске не должны расходиться в том, что
// переносят. Разница одна — чаты: по запросу на задачу, а человек
// на экране ждёт ответа, поэтому здесь без них, и отчёт это называет.
func (c *Client) Plan(ctx context.Context, boardID string) (importer.Plan, error) {
	b, err := c.Board(ctx, boardID, FetchOptions{})
	if err != nil {
		return importer.Plan{}, err
	}
	if len(b.Cards) > importer.MaxRows {
		return importer.Plan{}, ErrTooBig
	}
	pkg := &pack.Package{Manifest: pack.Manifest{Source: pack.Source{System: Source}}, Boards: []pack.Board{b}}
	return pkg.Plan(1)
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
			// «Бизнес: Бизнес» — стикер-флажок, у которого значение
			// повторяет имя: метка тогда одно слово, а не два одинаковых.
			name, value := strings.TrimSpace(s.Name), strings.TrimSpace(st.Name)
			if strings.EqualFold(name, value) || value == "" {
				labels[s.ID][st.ID] = name
			} else {
				labels[s.ID][st.ID] = name + ": " + value
			}
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
