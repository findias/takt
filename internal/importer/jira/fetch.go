package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer/pack"
)

// Source — имя источника в пакете.
const Source = "jira"

// ErrTooBig — доска больше, чем takt переносит за раз. Выгрузка
// останавливается, дочитав до предела, а не тянет сто тысяч задач
// ради отказа в конце.
type ErrTooBig struct {
	Title string
	Limit int
}

func (e ErrTooBig) Error() string {
	return fmt.Sprintf("на доске «%s» больше %d задач — takt переносит до %d за раз; сузьте фильтр доски в Jira", e.Title, e.Limit, e.Limit)
}

// FetchOptions — что забирать и как говорить о ходе дела.
type FetchOptions struct {
	// Comments — забирать ли комментарии. В ответе поиска их приходит
	// часть; остальное — запрос на задачу.
	Comments bool
	// Progress — строка о ходе дела для человека; nil — молча.
	Progress func(string)
}

type user struct {
	AccountID   string `json:"accountId"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

// id — ключ человека: accountId в облаке, key (или name) у своей
// установки.
func (u *user) id() string {
	switch {
	case u.AccountID != "":
		return u.AccountID
	case u.Key != "":
		return u.Key
	}
	return u.Name
}

type comment struct {
	Author  *user           `json:"author"`
	Created string          `json:"created"`
	Body    json.RawMessage `json:"body"`
}

type issueLink struct {
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
	OutwardIssue *struct {
		ID string `json:"id"`
	} `json:"outwardIssue"`
}

type issue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Status      struct {
			ID string `json:"id"`
		} `json:"status"`
		Assignee *user    `json:"assignee"`
		Labels   []string `json:"labels"`
		Priority *struct {
			Name string `json:"name"`
		} `json:"priority"`
		DueDate    string `json:"duedate"`
		Created    string `json:"created"`
		Resolution string `json:"resolutiondate"`
		Parent     *struct {
			ID string `json:"id"`
		} `json:"parent"`
		IssueLinks []issueLink `json:"issuelinks"`
		Comment    *struct {
			Comments []comment `json:"comments"`
			Total    int       `json:"total"`
		} `json:"comment"`
	} `json:"fields"`
	// Оценка лежит в поле, которое называет доска, — у каждой установки
	// своё customfield_NNNNN; достаётся отдельно из сырого ответа.
	raw map[string]json.RawMessage
}

// Board собирает доску Jira в доску пакета.
func (c *Client) Board(ctx context.Context, boardID string, opt FetchOptions) (pack.Board, error) {
	progress := func(format string, args ...any) {
		if opt.Progress != nil {
			opt.Progress(fmt.Sprintf(format, args...))
		}
	}
	if _, err := strconv.ParseInt(boardID, 10, 64); err != nil {
		return pack.Board{}, ErrNotFound
	}
	var info struct {
		Name string `json:"name"`
	}
	if err := c.get(ctx, "/rest/agile/1.0/board/"+boardID, nil, &info); err != nil {
		return pack.Board{}, err
	}
	var conf struct {
		Filter struct {
			ID string `json:"id"`
		} `json:"filter"`
		SubQuery struct {
			Query string `json:"query"`
		} `json:"subQuery"`
		ColumnConfig struct {
			Columns []struct {
				Name     string `json:"name"`
				Statuses []struct {
					ID string `json:"id"`
				} `json:"statuses"`
			} `json:"columns"`
		} `json:"columnConfig"`
		Estimation struct {
			Field struct {
				FieldID string `json:"fieldId"`
			} `json:"field"`
		} `json:"estimation"`
	}
	if err := c.get(ctx, "/rest/agile/1.0/board/"+boardID+"/configuration", nil, &conf); err != nil {
		return pack.Board{}, err
	}
	if conf.Filter.ID == "" {
		return pack.Board{}, ErrNotBoard
	}

	// Категория статуса — единственное, что Jira знает о смысле колонки:
	// «к выполнению», «в работе», «готово». Разметку колонки она
	// подсказывает, а не решает: человек в предпросмотре её поправит.
	var statuses []struct {
		ID       string `json:"id"`
		Category struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	if err := c.get(ctx, c.api()+"status", nil, &statuses); err != nil {
		return pack.Board{}, err
	}
	category := map[string]string{}
	for _, s := range statuses {
		category[s.ID] = s.Category.Key
	}

	out := pack.Board{ExternalID: boardID, Title: strings.TrimSpace(info.Name)}
	columnOf := map[string]string{}
	for i, col := range conf.ColumnConfig.Columns {
		// Колонка без статусов пуста всегда: задача в неё попасть не может.
		if len(col.Statuses) == 0 {
			continue
		}
		id := strconv.Itoa(i + 1)
		kinds := map[string]bool{}
		for _, s := range col.Statuses {
			columnOf[s.ID] = id
			kinds[category[s.ID]] = true
		}
		out.Columns = append(out.Columns, pack.Column{ExternalID: id, Title: strings.TrimSpace(col.Name), Kind: kindOf(kinds)})
	}
	if len(out.Columns) == 0 {
		return pack.Board{}, ErrNotFound
	}

	jql := "filter = " + conf.Filter.ID
	if q := strings.TrimSpace(conf.SubQuery.Query); q != "" {
		jql += " AND (" + q + ")"
	}
	jql += " ORDER BY Rank ASC"
	estimate := conf.Estimation.Field.FieldID
	issues, err := c.search(ctx, jql, estimate, out.Title, progress)
	if err != nil {
		return pack.Board{}, err
	}

	people := map[string]pack.Person{}
	person := func(u *user) *string {
		if u == nil || u.id() == "" {
			return nil
		}
		id := u.id()
		if _, ok := people[id]; !ok {
			p := pack.Person{ExternalID: id, Name: strings.TrimSpace(u.DisplayName)}
			if e := strings.TrimSpace(u.Email); e != "" {
				p.Email = &e
			}
			if p.Name == "" && p.Email != nil {
				p.Name = *p.Email
			}
			people[id] = p
		}
		return &id
	}
	labels := map[string]bool{}
	onBoard := map[string]bool{}
	for _, is := range issues {
		if columnOf[is.Fields.Status.ID] != "" {
			onBoard[is.ID] = true
		}
	}

	var outside, unreadComments int
	for i, is := range issues {
		col := columnOf[is.Fields.Status.ID]
		if col == "" {
			// Статус не привязан ни к одной колонке: на доске Jira такой
			// задачи тоже не видно. Называется числом.
			outside++
			continue
		}
		f := is.Fields
		card := pack.Card{
			ExternalID:  is.ID,
			Number:      is.Key,
			Title:       strings.TrimSpace(f.Summary),
			Description: text(f.Description),
			Column:      col,
			CreatedAt:   moment(f.Created),
			FinishedAt:  moment(f.Resolution),
			Priority:    priority(f.Priority),
		}
		if d, err := time.Parse("2006-01-02", f.DueDate); err == nil {
			card.Due = d.Format("2006-01-02")
		}
		if id := person(f.Assignee); id != nil {
			card.Assignees = []string{*id}
		}
		for _, l := range f.Labels {
			if l = strings.TrimSpace(l); l != "" {
				card.Labels = append(card.Labels, l)
				labels[l] = true
			}
		}
		if f.Parent != nil && onBoard[f.Parent.ID] {
			p := f.Parent.ID
			card.Parent = &p
		}
		for _, l := range f.IssueLinks {
			// Каждая связь видна с обеих задач: берётся только исходящая
			// сторона, иначе она приехала бы дважды.
			if l.OutwardIssue == nil || !onBoard[l.OutwardIssue.ID] {
				continue
			}
			kind := "relates"
			if strings.EqualFold(l.Type.Name, "Blocks") {
				kind = "blocks"
			}
			card.Links = append(card.Links, pack.Link{Kind: kind, To: l.OutwardIssue.ID})
		}
		if estimate != "" {
			var v float64
			if err := json.Unmarshal(is.raw[estimate], &v); err == nil && v > 0 {
				card.Estimate = &v
			}
		}
		if opt.Comments && f.Comment != nil {
			list := f.Comment.Comments
			if f.Comment.Total > len(list) {
				all, err := c.comments(ctx, is.ID)
				switch {
				case errors.Is(err, ErrUnreachable) || ctx.Err() != nil:
					return pack.Board{}, err
				case err != nil:
					// Один непрочитанный разговор не роняет доску: карточка
					// едет с тем, что пришло в поиске.
					unreadComments++
				default:
					list = all
				}
			}
			for _, cm := range list {
				at := moment(cm.Created)
				body := strings.TrimSpace(text(cm.Body))
				if at == nil || body == "" {
					continue
				}
				card.Comments = append(card.Comments, pack.Comment{Author: person(cm.Author), At: *at, Text: body})
			}
			if (i+1)%100 == 0 {
				progress("комментарии: %d из %d задач", i+1, len(issues))
			}
		}
		out.Cards = append(out.Cards, card)
	}

	for _, p := range people {
		out.People = append(out.People, p)
	}
	sort.Slice(out.People, func(i, j int) bool { return out.People[i].ExternalID < out.People[j].ExternalID })
	for l := range labels {
		out.Labels = append(out.Labels, pack.Label{ExternalID: l, Name: l})
	}
	sort.Slice(out.Labels, func(i, j int) bool { return out.Labels[i].Name < out.Labels[j].Name })

	if outside > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("задачи в статусах, которых нет среди колонок доски Jira: %d", outside))
	}
	if unreadComments > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("задачи, чьи комментарии Jira отдала не целиком: %d", unreadComments))
	}
	if !opt.Comments {
		out.Lost = append(out.Lost, "комментарии — собраны без них")
	}
	withoutEmail := 0
	for _, p := range out.People {
		if p.Email == nil {
			withoutEmail++
		}
	}
	if withoutEmail > 0 {
		// В облаке почту скрывает сам человек настройкой приватности,
		// и администратор этого не отменит; сказать заранее — значит
		// избавить от поиска поломки в выгрузчике.
		out.Lost = append(out.Lost, fmt.Sprintf("почты людей, скрытые настройкой приватности Jira: %d — сопоставьте их в предпросмотре вручную", withoutEmail))
	}
	out.Lost = append(out.Lost, "вложения, учёт времени, спринты, версии и история изменений — их в карточке нет")
	return out, nil
}

// kindOf — разметка колонки по категориям её статусов.
func kindOf(kinds map[string]bool) *string {
	var k string
	switch {
	case len(kinds) == 1 && kinds["new"]:
		k = "queue"
	case len(kinds) == 1 && kinds["done"]:
		k = "done"
	case kinds["indeterminate"]:
		k = "in_progress"
	default:
		return nil
	}
	return &k
}

// moment — время Jira: «2026-09-01T10:15:30.000+0300», без двоеточия
// в поясе, поэтому RFC 3339 его не читает.
func moment(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

var searchFields = []string{
	"summary", "description", "status", "assignee", "labels", "priority",
	"duedate", "created", "resolutiondate", "parent", "issuelinks", "comment",
}

// search — все задачи по запросу. Облако листает по nextPageToken,
// своя установка — по startAt.
func (c *Client) search(ctx context.Context, jql, estimate, title string, progress func(string, ...any)) ([]issue, error) {
	fields := searchFields
	if estimate != "" {
		fields = append(append([]string{}, searchFields...), estimate)
	}
	var out []issue
	token := ""
	for {
		var page struct {
			Issues        []json.RawMessage `json:"issues"`
			NextPageToken string            `json:"nextPageToken"`
			IsLast        *bool             `json:"isLast"`
			Total         int               `json:"total"`
		}
		body := map[string]any{"jql": jql, "fields": fields, "maxResults": 100}
		path := c.api() + "search"
		if c.Cloud() {
			path += "/jql"
			if token != "" {
				body["nextPageToken"] = token
			}
		} else {
			body["startAt"] = len(out)
		}
		if err := c.post(ctx, path, body, &page); err != nil {
			return nil, err
		}
		for _, raw := range page.Issues {
			var is issue
			if err := json.Unmarshal(raw, &is); err != nil {
				return nil, ErrNotBoard
			}
			var wrap struct {
				Fields map[string]json.RawMessage `json:"fields"`
			}
			_ = json.Unmarshal(raw, &wrap)
			is.raw = wrap.Fields
			out = append(out, is)
		}
		if len(out) > pack.MaxCards {
			return nil, ErrTooBig{Title: title, Limit: pack.MaxCards}
		}
		progress("задачи: %d", len(out))
		switch {
		case len(page.Issues) == 0:
			return out, nil
		case c.Cloud() && (page.NextPageToken == "" || (page.IsLast != nil && *page.IsLast)):
			return out, nil
		case !c.Cloud() && len(out) >= page.Total:
			return out, nil
		}
		token = page.NextPageToken
	}
}

// comments — все комментарии задачи, когда поиск отдал не все.
func (c *Client) comments(ctx context.Context, issueID string) ([]comment, error) {
	var out []comment
	for {
		var page struct {
			Comments []comment `json:"comments"`
			Total    int       `json:"total"`
		}
		q := url.Values{"startAt": {strconv.Itoa(len(out))}, "maxResults": {"100"}}
		if err := c.get(ctx, c.api()+"issue/"+url.PathEscape(issueID)+"/comment", q, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Comments...)
		if len(page.Comments) == 0 || len(out) >= page.Total {
			return out, nil
		}
	}
}
