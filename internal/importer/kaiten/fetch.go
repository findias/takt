package kaiten

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer/pack"
)

// Source — имя источника в пакете.
const Source = "kaiten"

// ErrTooBig — доска больше, чем takt переносит за раз. Выгрузка
// останавливается, дочитав до предела, а не тянет всё ради отказа.
type ErrTooBig struct {
	Title string
	Limit int
}

func (e ErrTooBig) Error() string {
	return fmt.Sprintf("на доске «%s» больше %d карточек — takt переносит до %d за раз; разделите доску в Kaiten", e.Title, e.Limit, e.Limit)
}

// FetchOptions — что забирать и как говорить о ходе дела.
type FetchOptions struct {
	// Comments — забирать ли комментарии: это запрос на каждую карточку.
	Comments bool
	// Progress — строка о ходе дела для человека; nil — молча.
	Progress func(string)
	// Lane — имя метки для дорожки на языке того, кто собирает:
	// метка заводится в takt так, как названа в пакете. nil — по-русски.
	Lane func(title string) string
}

type column struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	SortOrder float64 `json:"sort_order"`
	// Type — 1 очередь, 2 в работе, 3 готово.
	Type int `json:"type"`
	// ColumnID — у подколонки: колонка, внутри которой она стоит.
	ColumnID *int64 `json:"column_id"`
}

type lane struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	SortOrder float64 `json:"sort_order"`
}

type member struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type card struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	// State — 1 очередь, 2 в работе, 3 готово.
	State    int      `json:"state"`
	ColumnID int64    `json:"column_id"`
	LaneID   *int64   `json:"lane_id"`
	Members  []member `json:"members"`
	Tags     []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Size        *float64 `json:"size"`
	DueDate     *string  `json:"due_date"`
	Created     string   `json:"created"`
	CompletedAt *string  `json:"completed_at"`
	DoneAt      *string  `json:"last_moved_to_done_at"`
	ASAP        bool     `json:"asap"`
	ParentsIDs  []int64  `json:"parents_ids"`
	Blockers    []struct {
		BlockerCardID *int64 `json:"blocker_card_id"`
		Released      bool   `json:"released"`
	} `json:"blockers"`
}

type comment struct {
	Text    string  `json:"text"`
	Created string  `json:"created"`
	Type    int     `json:"type"`
	Author  *member `json:"author"`
	Deleted bool    `json:"deleted"`
}

// Board собирает доску Kaiten в доску пакета.
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
		ID      int64    `json:"id"`
		Title   string   `json:"title"`
		Columns []column `json:"columns"`
		Lanes   []lane   `json:"lanes"`
	}
	if err := c.get(ctx, "/boards/"+boardID, nil, &info); err != nil {
		return pack.Board{}, err
	}
	if info.ID == 0 {
		return pack.Board{}, ErrNotKaiten
	}
	out := pack.Board{ExternalID: boardID, Title: strings.TrimSpace(info.Title)}

	// Колонки доски — листья: колонка без подколонок или подколонка.
	// В колонке с подколонками карточка лежит в одной из них, и столбец
	// takt получает имя «Колонка: подколонка» — иначе три «В работе»
	// разных колонок слились бы в одну.
	sort.SliceStable(info.Columns, func(i, j int) bool { return info.Columns[i].SortOrder < info.Columns[j].SortOrder })
	subs := map[int64][]column{}
	for _, col := range info.Columns {
		if col.ColumnID != nil {
			subs[*col.ColumnID] = append(subs[*col.ColumnID], col)
		}
	}
	columnOf := map[int64]string{}
	add := func(col column, title string, kind int) {
		id := strconv.FormatInt(col.ID, 10)
		columnOf[col.ID] = id
		out.Columns = append(out.Columns, pack.Column{ExternalID: id, Title: title, Kind: kindOf(kind)})
	}
	for _, col := range info.Columns {
		if col.ColumnID != nil {
			continue
		}
		title := strings.TrimSpace(col.Title)
		if len(subs[col.ID]) == 0 {
			add(col, title, col.Type)
			continue
		}
		for _, sub := range subs[col.ID] {
			kind := sub.Type
			if kind == 0 {
				kind = col.Type
			}
			add(sub, title+": "+strings.TrimSpace(sub.Title), kind)
		}
		// Карточка, лежащая прямо в колонке с подколонками, встаёт
		// в её первую подколонку — так её и видно на доске Kaiten.
		columnOf[col.ID] = columnOf[subs[col.ID][0].ID]
	}
	if len(out.Columns) == 0 {
		return pack.Board{}, ErrNotFound
	}

	// Дорожки Kaiten — не колонки: это ряды поперёк доски. У takt рядов
	// своих нет — ряды строятся группировкой по свойству, — поэтому
	// дорожка едет меткой, и доска в takt раскладывается по ней той же
	// группировкой «по метке». Одна дорожка — это просто доска, метка
	// ей не нужна.
	laneName := opt.Lane
	if laneName == nil {
		laneName = func(t string) string { return "Дорожка: " + t }
	}
	laneLabel := map[int64]string{}
	if len(info.Lanes) > 1 {
		for _, l := range info.Lanes {
			if t := strings.TrimSpace(l.Title); t != "" {
				laneLabel[l.ID] = laneName(t)
			}
		}
	}

	cards, err := c.cards(ctx, boardID, out.Title, progress)
	if err != nil {
		return pack.Board{}, err
	}
	onBoard := map[int64]bool{}
	for _, k := range cards {
		onBoard[k.ID] = true
	}

	people := map[string]pack.Person{}
	person := func(m *member) *string {
		if m == nil || m.ID == 0 {
			return nil
		}
		id := strconv.FormatInt(m.ID, 10)
		if _, ok := people[id]; !ok {
			p := pack.Person{ExternalID: id, Name: strings.TrimSpace(m.FullName)}
			if e := strings.TrimSpace(m.Email); e != "" {
				p.Email = &e
			}
			if p.Name == "" {
				p.Name = strings.TrimSpace(m.Username)
			}
			if p.Name == "" && p.Email != nil {
				p.Name = *p.Email
			}
			people[id] = p
		}
		return &id
	}
	labels := map[string]bool{}
	blocks := map[int64][]int64{}
	var manyParents, unreadComments int
	for _, k := range cards {
		for _, b := range k.Blockers {
			if !b.Released && b.BlockerCardID != nil && onBoard[*b.BlockerCardID] {
				blocks[*b.BlockerCardID] = append(blocks[*b.BlockerCardID], k.ID)
			}
		}
	}

	for i, k := range cards {
		id := strconv.FormatInt(k.ID, 10)
		col, ok := columnOf[k.ColumnID]
		if !ok {
			// Колонка, которой нет в описании доски, — карточка встанет
			// в первую, и импорт назовёт это в предпросмотре.
			col = strconv.FormatInt(k.ColumnID, 10)
		}
		pc := pack.Card{
			ExternalID: id,
			Number:     id,
			Title:      strings.TrimSpace(k.Title),
			Column:     col,
			CreatedAt:  moment(k.Created),
		}
		if k.Description != nil {
			pc.Description = strings.TrimSpace(*k.Description)
		}
		if k.State == 3 {
			pc.FinishedAt = firstMoment(k.CompletedAt, k.DoneAt)
		}
		if k.ASAP {
			pc.Priority = "highest"
		}
		if k.DueDate != nil {
			if d := moment(*k.DueDate); d != nil {
				pc.Due = d.Format("2006-01-02")
			}
		}
		if k.Size != nil && *k.Size > 0 {
			v := *k.Size
			pc.Estimate = &v
		}
		for i := range k.Members {
			if pid := person(&k.Members[i]); pid != nil {
				pc.Assignees = append(pc.Assignees, *pid)
			}
		}
		for _, t := range k.Tags {
			if n := strings.TrimSpace(t.Name); n != "" {
				pc.Labels = append(pc.Labels, n)
				labels[n] = true
			}
		}
		if k.LaneID != nil && laneLabel[*k.LaneID] != "" {
			pc.Labels = append(pc.Labels, laneLabel[*k.LaneID])
			labels[laneLabel[*k.LaneID]] = true
		}
		// У карточки Kaiten родителей бывает несколько, у takt — один:
		// дерево, а не граф. Берётся первый с этой доски, остальные
		// считаются и называются.
		var parents int
		for _, p := range k.ParentsIDs {
			if !onBoard[p] {
				continue
			}
			parents++
			if pc.Parent == nil && p != k.ID {
				s := strconv.FormatInt(p, 10)
				pc.Parent = &s
			}
		}
		if parents > 1 {
			manyParents++
		}
		for _, to := range blocks[k.ID] {
			pc.Links = append(pc.Links, pack.Link{Kind: "blocks", To: strconv.FormatInt(to, 10)})
		}
		if opt.Comments {
			list, err := c.comments(ctx, id)
			switch {
			case errors.Is(err, ErrUnreachable) || ctx.Err() != nil:
				return pack.Board{}, err
			case err != nil:
				// Один непрочитанный разговор не роняет доску.
				unreadComments++
			}
			for _, cm := range list {
				at := moment(cm.Created)
				body := cm.Text
				if cm.Type == 2 {
					body = plain(body)
				}
				body = strings.TrimSpace(body)
				if cm.Deleted || at == nil || body == "" {
					continue
				}
				pc.Comments = append(pc.Comments, pack.Comment{Author: person(cm.Author), At: *at, Text: body})
			}
			if (i+1)%100 == 0 {
				progress("комментарии: %d из %d карточек", i+1, len(cards))
			}
		}
		out.Cards = append(out.Cards, pc)
	}

	for _, p := range people {
		out.People = append(out.People, p)
	}
	sort.Slice(out.People, func(i, j int) bool { return out.People[i].ExternalID < out.People[j].ExternalID })
	for l := range labels {
		out.Labels = append(out.Labels, pack.Label{ExternalID: l, Name: l})
	}
	sort.Slice(out.Labels, func(i, j int) bool { return out.Labels[i].Name < out.Labels[j].Name })

	if len(laneLabel) > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("дорожки Kaiten стали метками: %d — разложите доску по ним группировкой «По метке»", len(laneLabel)))
	}
	if manyParents > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("карточки с несколькими родителями — переедут под первым: %d", manyParents))
	}
	if unreadComments > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("карточки, чьи комментарии Kaiten не отдал: %d", unreadComments))
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
		out.Lost = append(out.Lost, fmt.Sprintf("люди без почты в ответе Kaiten: %d — сопоставьте их в предпросмотре вручную", withoutEmail))
	}
	out.Lost = append(out.Lost, "архивные карточки, причины блокировок, вложения, чек-листы, учёт времени и история перемещений — их в карточке нет")
	return out, nil
}

// cards — все живые карточки доски, страницами по сто.
func (c *Client) cards(ctx context.Context, boardID, title string, progress func(string, ...any)) ([]card, error) {
	var out []card
	for offset := 0; ; offset += page {
		var got []card
		q := url.Values{
			"board_id": {boardID}, "condition": {"1"},
			"limit": {strconv.Itoa(page)}, "offset": {strconv.Itoa(offset)},
		}
		if err := c.get(ctx, "/cards", q, &got); err != nil {
			return nil, err
		}
		out = append(out, got...)
		if len(out) > pack.MaxCards {
			return nil, ErrTooBig{Title: title, Limit: pack.MaxCards}
		}
		progress("карточки: %d", len(out))
		if len(got) < page {
			return out, nil
		}
	}
}

func (c *Client) comments(ctx context.Context, cardID string) ([]comment, error) {
	var out []comment
	err := c.get(ctx, "/cards/"+url.PathEscape(cardID)+"/comments", nil, &out)
	return out, err
}

// kindOf — разметка колонки по её типу в Kaiten.
func kindOf(t int) *string {
	var k string
	switch t {
	case 1:
		k = "queue"
	case 2:
		k = "in_progress"
	case 3:
		k = "done"
	default:
		return nil
	}
	return &k
}

// moment — время Kaiten: ISO 8601 с поясом.
func moment(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func firstMoment(list ...*string) *time.Time {
	for _, s := range list {
		if s != nil {
			if t := moment(*s); t != nil {
				return t
			}
		}
	}
	return nil
}

var (
	breaks = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>`)
	tags   = regexp.MustCompile(`<[^>]*>`)
)

// plain — комментарий в HTML (type 2) текстом: переносы на месте
// абзацев, без разметки.
func plain(s string) string {
	return html.UnescapeString(tags.ReplaceAllString(breaks.ReplaceAllString(s, "\n"), ""))
}
