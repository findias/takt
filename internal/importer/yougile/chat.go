package yougile

import (
	"context"
	"errors"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer/pack"
)

// Чат задачи YouGile: реплики людей — обсуждением, системные сообщения —
// историей задачи (ROADMAP 23.7).
//
// Системное от человеческого API не отличает ничем, кроме параметра
// includeSystem: без него системных нет, с ним — есть. Поэтому чат
// читается дважды и сравнивается по номерам сообщений. Образца системных
// сообщений у нас нет, и угадывать их по тексту или автору значило бы
// однажды отнести реплику человека к истории.

// Chat — чат задачи, разобранный на реплики и историю.
type Chat struct {
	Comments []pack.Comment
	History  []pack.Comment
	// Files — сообщений без текста (вложения): файлы не переносятся.
	Files int
}

// TaskChat читает чат задачи. known — кто из авторов известен (users
// компании): у неизвестного автор не ставится. withHistory — читать ли
// и системные сообщения (второй запрос на задачу).
func (c *Client) TaskChat(ctx context.Context, taskID string, known map[string]bool, withHistory bool) (Chat, error) {
	path := "chats/" + url.PathEscape(taskID) + "/messages"
	plain, err := all[message](ctx, c, path, nil)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Chat{}, err
	}
	var out Chat
	human := map[int64]bool{}
	for _, m := range plain {
		human[m.ID] = true
		if m.Deleted {
			continue
		}
		text := messageText(m)
		if text == "" {
			// Сообщение без текста — вложение: файлы в пакет не едут.
			out.Files++
			continue
		}
		out.Comments = append(out.Comments, entry(m, text, known))
	}
	if withHistory {
		full, err := all[message](ctx, c, path, url.Values{"includeSystem": {"true"}})
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Chat{}, err
		}
		for _, m := range full {
			if human[m.ID] || m.Deleted {
				continue
			}
			if text := messageText(m); text != "" {
				out.History = append(out.History, entry(m, text, known))
			}
		}
	}
	byTime := func(list []pack.Comment) {
		sort.SliceStable(list, func(a, b int) bool { return list[a].At.Before(list[b].At) })
	}
	byTime(out.Comments)
	byTime(out.History)
	return out, nil
}

func messageText(m message) string {
	text := strings.TrimSpace(m.Text)
	if text == "" && m.TextHTML != "" {
		text = strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(breaks.ReplaceAllString(m.TextHTML, "\n"), "")))
	}
	return text
}

func entry(m message, text string, known map[string]bool) pack.Comment {
	at := m.Timestamp
	if at == 0 {
		at = m.ID // у YouGile номер сообщения — его время в миллисекундах
	}
	e := pack.Comment{Text: text}
	if when := millis(at); when != nil {
		e.At = *when
	} else {
		e.At = time.Now().UTC()
	}
	if known[m.FromUserID] {
		author := m.FromUserID
		e.Author = &author
	}
	return e
}
