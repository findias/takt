package yougile

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer/pack"
)

// Выгрузка доски в пакет переноса (docs/import-package.md) — для
// takt-fetch, который работает снаружи закрытого контура.
//
// В отличие от переноса по API на экране (Plan), в пакет идёт всё, что
// формат умеет нести: подзадачи — частями, чаты задач — обсуждением.
// Цена — число запросов: по одному на чат каждой задачи. Поэтому
// выгрузчик терпелив к пределу YouGile (Client.Patient) и говорит,
// сколько сделано (progress).

type message struct {
	ID         int64  `json:"id"`
	FromUserID string `json:"fromUserId"`
	Text       string `json:"text"`
	TextHTML   string `json:"textHtml"`
	Timestamp  int64  `json:"timestamp"`
	Deleted    bool   `json:"deleted"`
}

// FetchOptions — что забирать и как говорить о ходе дела.
type FetchOptions struct {
	// Chats — забирать ли чаты задач. Без них быстрее вдвое и больше:
	// запрос на задачу.
	Chats bool
	// Progress — строка о ходе дела для человека; nil — молча.
	Progress func(string)
}

// Board собирает доску YouGile в доску пакета.
func (c *Client) Board(ctx context.Context, boardID string, opt FetchOptions) (pack.Board, error) {
	progress := func(format string, args ...any) {
		if opt.Progress != nil {
			opt.Progress(fmt.Sprintf(format, args...))
		}
	}
	var title string
	boards, err := c.Boards(ctx)
	if err != nil {
		return pack.Board{}, err
	}
	for _, b := range boards {
		if b.ID == boardID {
			title = b.Title
		}
	}
	if title == "" {
		return pack.Board{}, ErrNotFound
	}
	out := pack.Board{ExternalID: boardID, Title: title}

	type column struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		BoardID string `json:"boardId"`
		Deleted bool   `json:"deleted"`
	}
	cols, err := all[column](ctx, c, "columns", url.Values{"boardId": {boardID}})
	if err != nil {
		return pack.Board{}, err
	}
	onBoard := map[string]bool{}
	for _, col := range cols {
		if col.BoardID != boardID || col.Deleted {
			continue
		}
		onBoard[col.ID] = true
		out.Columns = append(out.Columns, pack.Column{ExternalID: col.ID, Title: strings.TrimSpace(col.Title)})
	}
	if len(out.Columns) == 0 {
		return pack.Board{}, ErrNotFound
	}

	type user struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		RealName string `json:"realName"`
	}
	users, err := all[user](ctx, c, "users", nil)
	if err != nil {
		return pack.Board{}, err
	}
	known := map[string]bool{}
	for _, u := range users {
		p := pack.Person{ExternalID: u.ID, Name: strings.TrimSpace(u.RealName)}
		if e := strings.TrimSpace(u.Email); e != "" {
			p.Email = &e
		}
		if p.Name == "" && p.Email != nil {
			p.Name = *p.Email
		}
		out.People = append(out.People, p)
		known[u.ID] = true
	}

	labelNames, priority, err := c.stickers(ctx)
	if err != nil {
		return pack.Board{}, err
	}
	usedLabel := map[string]bool{}

	// Задачи колонок, затем подзадачи, которых в колонках нет: у YouGile
	// подзадача — обычная задача, и колонки у неё может не быть вовсе.
	var tasks []task
	seen := map[string]bool{}
	columnOf := map[string]string{}
	for i, col := range out.Columns {
		progress("колонка %d из %d: %s", i+1, len(out.Columns), col.Title)
		list, err := all[task](ctx, c, "tasks", url.Values{"columnId": {col.ExternalID}})
		if err != nil {
			return pack.Board{}, err
		}
		for _, t := range list {
			if t.ColumnID == col.ExternalID && !seen[t.ID] {
				seen[t.ID] = true
				tasks = append(tasks, t)
				columnOf[t.ID] = t.ColumnID
			}
		}
	}
	parentOf := map[string]string{}
	for i := 0; i < len(tasks); i++ {
		for _, child := range tasks[i].Subtasks {
			if _, ok := parentOf[child]; !ok {
				parentOf[child] = tasks[i].ID
			}
			if seen[child] {
				continue
			}
			seen[child] = true
			// Цепочка подзадач длиннее, чем takt переносит за раз, —
			// испорченные данные или чужой сервер, водящий по кругу.
			if len(tasks) > pack.MaxCards {
				return pack.Board{}, ErrTooBig
			}
			var t task
			if err := c.do(ctx, http.MethodGet, "tasks/"+url.PathEscape(child), nil, nil, &t); err != nil {
				if errors.Is(err, ErrNotFound) {
					continue
				}
				return pack.Board{}, err
			}
			// Подзадача без колонки этой доски встаёт в колонку родителя:
			// иначе ей на доске места нет, а терять часть работы нельзя.
			if !onBoard[t.ColumnID] {
				t.ColumnID = columnOf[tasks[i].ID]
			}
			columnOf[t.ID] = t.ColumnID
			tasks = append(tasks, t)
		}
	}

	var archived, files int
	for i, t := range tasks {
		if t.Deleted {
			continue
		}
		if t.Archived {
			archived++
			continue
		}
		card := pack.Card{
			ExternalID:  t.ID,
			Title:       strings.TrimSpace(t.Title),
			Description: description(t),
			Column:      t.ColumnID,
			CreatedAt:   millis(t.Timestamp),
		}
		if p := parentOf[t.ID]; p != "" {
			card.Parent = &p
		}
		if t.Completed {
			card.FinishedAt = millis(t.CompletedTimestamp)
		}
		if t.Deadline != nil {
			if d := millis(t.Deadline.Deadline); d != nil {
				card.Due = d.Format("2006-01-02")
			}
		}
		for _, id := range t.Assigned {
			if known[id] {
				card.Assignees = append(card.Assignees, id)
			}
		}
		for sid, state := range t.Stickers {
			if p := priority[sid][state]; p != "" {
				card.Priority = p
			} else if name := labelNames[sid][state]; name != "" {
				id := sid + ":" + state
				card.Labels = append(card.Labels, id)
				if !usedLabel[id] {
					usedLabel[id] = true
					out.Labels = append(out.Labels, pack.Label{ExternalID: id, Name: name})
				}
			}
		}
		sort.Strings(card.Labels)
		if opt.Chats {
			if (i+1)%25 == 0 {
				progress("чаты: %d из %d задач", i+1, len(tasks))
			}
			msgs, err := all[message](ctx, c, "chats/"+url.PathEscape(t.ID)+"/messages", nil)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return pack.Board{}, err
			}
			for _, m := range msgs {
				if m.Deleted {
					continue
				}
				text := strings.TrimSpace(m.Text)
				if text == "" && m.TextHTML != "" {
					text = strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(breaks.ReplaceAllString(m.TextHTML, "\n"), "")))
				}
				if text == "" {
					// Сообщение без текста — вложение: файлы в пакет не едут.
					files++
					continue
				}
				at := m.Timestamp
				if at == 0 {
					at = m.ID // у YouGile номер сообщения — его время в миллисекундах
				}
				comment := pack.Comment{Text: text}
				if when := millis(at); when != nil {
					comment.At = *when
				} else {
					comment.At = time.Now().UTC()
				}
				if known[m.FromUserID] {
					author := m.FromUserID
					comment.Author = &author
				}
				card.Comments = append(card.Comments, comment)
			}
			sort.SliceStable(card.Comments, func(a, b int) bool { return card.Comments[a].At.Before(card.Comments[b].At) })
		}
		out.Cards = append(out.Cards, card)
	}

	if archived > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("задачи из архива YouGile: %d", archived))
	}
	if files > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("сообщения чатов без текста (вложения): %d", files))
	}
	if !opt.Chats {
		out.Lost = append(out.Lost, "чаты задач — выгружены без них (--no-chats)")
	}
	out.Lost = append(out.Lost, "файлы, права доступа и учёт времени — их в карточке нет")
	return out, nil
}
