package yougile

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

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
	Label      string `json:"label"`
	Timestamp  int64  `json:"timestamp"`
	Deleted    bool   `json:"deleted"`
}

// FetchOptions — что забирать и как говорить о ходе дела.
type FetchOptions struct {
	// Chats — забирать ли чаты задач. Без них быстрее вдвое и больше:
	// запрос на задачу.
	Chats bool
	// History — забирать ли историю задачи (системные сообщения чата):
	// ещё запрос на задачу.
	History bool
	// Later — чаты и историю дотянут потом (перенос по API на экране,
	// ROADMAP 23.7): о них не говорить как о непереносимом.
	Later bool
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
	out := pack.Board{ExternalID: boardID, Title: title, HistoryCollected: opt.History}

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

	people, err := c.People(ctx)
	if err != nil {
		return pack.Board{}, err
	}
	known := map[string]bool{}
	for _, p := range people {
		out.People = append(out.People, p)
		known[p.ExternalID] = true
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
	unread := 0
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
				// Предел запросов, пропавшая сеть и отменённый запрос —
				// про весь перенос: повторять его целиком. Всё прочее —
				// про одну подзадачу (удалена, в чужом проекте, ответ
				// не того вида): она не должна ронять сотни карточек
				// доски, а называется в отчёте числом.
				if errors.Is(err, ErrBusy) || errors.Is(err, ErrUnreachable) || ctx.Err() != nil {
					return pack.Board{}, err
				}
				if !errors.Is(err, ErrNotFound) {
					unread++
				}
				continue
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

	var archived, files, unreadChats int
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
			Number:      cmp.Or(t.IDTaskProject, t.IDTaskCommon),
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
		if opt.Chats || opt.History {
			if (i+1)%25 == 0 {
				progress("чаты: %d из %d задач", i+1, len(tasks))
			}
			chat, err := c.TaskChat(ctx, t.ID, known, opt.History)
			switch {
			// Один непрочитанный чат не должен ронять доску: за ним
			// полчаса уже сделанной работы, и карточка едет без него.
			// Про весь перенос — только пропавшая связь и отменённый
			// запрос.
			case errors.Is(err, ErrUnreachable) || ctx.Err() != nil:
				return pack.Board{}, err
			case err != nil:
				unreadChats++
			default:
				files += chat.Files
				if opt.Chats {
					card.Comments = chat.Comments
				}
				card.History = chat.History
			}
		}
		out.Cards = append(out.Cards, card)
	}

	if unreadChats > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("чаты задач, которые YouGile не отдал: %d", unreadChats))
	}
	if unread > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("подзадачи, которые YouGile не отдал: %d", unread))
	}
	if archived > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("задачи из архива YouGile: %d", archived))
	}
	if files > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("сообщения чатов без текста (вложения): %d", files))
	}
	if !opt.Chats && !opt.Later {
		out.Lost = append(out.Lost, "чаты задач — собраны без них: чаты переносит пакет переноса")
	}
	out.Lost = append(out.Lost, "файлы, права доступа и учёт времени — их в карточке нет")
	return out, nil
}

// People — сотрудники компании YouGile в виде людей пакета: имя, почта,
// если YouGile её отдал.
func (c *Client) People(ctx context.Context) ([]pack.Person, error) {
	type user struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		RealName string `json:"realName"`
	}
	users, err := all[user](ctx, c, "users", nil)
	if err != nil {
		return nil, err
	}
	out := make([]pack.Person, 0, len(users))
	for _, u := range users {
		p := pack.Person{ExternalID: u.ID, Name: strings.TrimSpace(u.RealName)}
		if e := strings.TrimSpace(u.Email); e != "" {
			p.Email = &e
		}
		if p.Name == "" && p.Email != nil {
			p.Name = *p.Email
		}
		out = append(out, p)
	}
	return out, nil
}
