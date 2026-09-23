package monday

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/importer/pack"
)

// Source — имя источника в пакете.
const Source = "monday"

// ByGroup — значение --column: колонки доски — группы monday.
const ByGroup = "group"

// ErrTooBig — доска больше, чем takt переносит за раз.
type ErrTooBig struct {
	Title string
	Limit int
}

func (e ErrTooBig) Error() string {
	return fmt.Sprintf("на доске «%s» больше %d элементов — takt переносит до %d за раз; разделите доску в monday", e.Title, e.Limit, e.Limit)
}

// ErrNoColumn — названной колонки статуса на доске нет.
type ErrNoColumn struct {
	Name      string
	Available []string
}

func (e ErrNoColumn) Error() string {
	if len(e.Available) == 0 {
		return fmt.Sprintf("на доске нет колонки статуса «%s» и нет ни одной другой — возьмите группы: --column group", e.Name)
	}
	return fmt.Sprintf("на доске нет колонки статуса «%s» — есть «%s» или --column group", e.Name, strings.Join(e.Available, "», «"))
}

// FetchOptions — что забирать и как.
type FetchOptions struct {
	// Column — что считать колонками доски: название колонки статуса
	// monday, ByGroup — группы, пусто — первая колонка статуса, кроме
	// приоритета, а без неё — группы.
	Column string
	// Comments — забирать ли обновления (updates) элементов.
	Comments bool
	// Progress — строка о ходе дела для человека; nil — молча.
	Progress func(string)
	// Chosen — какой выбор колонок сделан, для человека; nil — молча.
	Chosen func(byGroup bool, status string)
}

type columnDef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Settings string `json:"settings_str"`
}

type value struct {
	ID    string  `json:"id"`
	Type  string  `json:"type"`
	Text  string  `json:"text"`
	Label *string `json:"label"`
	Date  *string `json:"date"`
	// persons_and_teams — у колонки людей, linked_item_ids — у
	// зависимости: типизированные поля ответа, а не разбор value.
	Persons []struct {
		ID   json.Number `json:"id"`
		Kind string      `json:"kind"`
	} `json:"persons_and_teams"`
	Linked []json.Number `json:"linked_item_ids"`
}

type update struct {
	Body      string `json:"text_body"`
	CreatedAt string `json:"created_at"`
	Creator   *struct {
		ID json.Number `json:"id"`
	} `json:"creator"`
}

type item struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	Group     *struct {
		ID string `json:"id"`
	} `json:"group"`
	Values   []value  `json:"column_values"`
	Updates  []update `json:"updates"`
	Subitems []item   `json:"subitems"`
}

// Поля элемента. Типизированные фрагменты — чтобы не разбирать
// строку value, формат которой у каждой колонки свой.
const itemFields = `id name created_at group { id }
  column_values { id type text
    ... on StatusValue { label }
    ... on DateValue { date }
    ... on PeopleValue { persons_and_teams { id kind } }
    ... on DependencyValue { linked_item_ids } }`

func fieldsWith(comments bool) string {
	f := itemFields
	if comments {
		f += ` updates(limit: 100) { text_body created_at creator { id } }`
	}
	sub := itemFields
	if comments {
		sub += ` updates(limit: 100) { text_body created_at creator { id } }`
	}
	return f + ` subitems { ` + sub + ` }`
}

// Board собирает доску monday в доску пакета.
func (c *Client) Board(ctx context.Context, boardID string, opt FetchOptions) (pack.Board, error) {
	progress := func(format string, args ...any) {
		if opt.Progress != nil {
			opt.Progress(fmt.Sprintf(format, args...))
		}
	}
	if _, err := strconv.ParseInt(boardID, 10, 64); err != nil {
		return pack.Board{}, ErrNotFound
	}
	fields := fieldsWith(opt.Comments)
	var first struct {
		Boards []struct {
			ID      string      `json:"id"`
			Name    string      `json:"name"`
			Columns []columnDef `json:"columns"`
			Groups  []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Position string `json:"position"`
			} `json:"groups"`
			ItemsPage struct {
				Cursor *string `json:"cursor"`
				Items  []item  `json:"items"`
			} `json:"items_page"`
		} `json:"boards"`
	}
	q := `query ($id: [ID!]) { boards(ids: $id) { id name
  columns { id title type settings_str }
  groups { id title position }
  items_page(limit: 100) { cursor items { ` + fields + ` } } } }`
	if err := c.query(ctx, q, map[string]any{"id": []string{boardID}}, &first); err != nil {
		return pack.Board{}, err
	}
	if len(first.Boards) == 0 {
		return pack.Board{}, ErrNotFound
	}
	b := first.Boards[0]
	out := pack.Board{ExternalID: boardID, Title: strings.TrimSpace(b.Name)}

	items := b.ItemsPage.Items
	cursor := b.ItemsPage.Cursor
	progress("элементы: %d", len(items))
	for cursor != nil && *cursor != "" {
		if len(items) > pack.MaxCards {
			return pack.Board{}, ErrTooBig{Title: out.Title, Limit: pack.MaxCards}
		}
		var next struct {
			Page struct {
				Cursor *string `json:"cursor"`
				Items  []item  `json:"items"`
			} `json:"next_items_page"`
		}
		nq := `query ($c: String!) { next_items_page(limit: 100, cursor: $c) { cursor items { ` + fields + ` } } }`
		if err := c.query(ctx, nq, map[string]any{"c": *cursor}, &next); err != nil {
			return pack.Board{}, err
		}
		items = append(items, next.Page.Items...)
		cursor = next.Page.Cursor
		progress("элементы: %d", len(items))
		if len(next.Page.Items) == 0 {
			break
		}
	}

	// Роли колонок monday — по типу, а где тип не решает (какая из
	// дат — срок, какое из чисел — оценка), по названию тем же словарём,
	// что у таблиц: Suggest знает «Due date», «Estimate», «Priority»
	// на обоих языках.
	var titles []string
	for _, col := range b.Columns {
		titles = append(titles, col.Title)
	}
	role := importer.Suggest(titles)
	var statusCols, dateCols []columnDef
	priorityCol, estimateCol, dueCol := "", "", ""
	for i, col := range b.Columns {
		switch col.Type {
		case "status":
			if role[i] == importer.FieldPriority {
				priorityCol = col.ID
			} else {
				statusCols = append(statusCols, col)
			}
		case "date":
			dateCols = append(dateCols, col)
			if role[i] == importer.FieldDue {
				dueCol = col.ID
			}
		case "numbers":
			if role[i] == importer.FieldEstimate {
				estimateCol = col.ID
			}
		}
	}
	if dueCol == "" && len(dateCols) == 1 {
		dueCol = dateCols[0].ID
	}

	// Колонки доски: статус или группы — решает человек флагом;
	// без флага — первая колонка статуса, как у доски-канбана monday.
	var status *columnDef
	switch {
	case opt.Column == ByGroup:
	case opt.Column != "":
		for i := range statusCols {
			if strings.EqualFold(strings.TrimSpace(statusCols[i].Title), strings.TrimSpace(opt.Column)) {
				status = &statusCols[i]
			}
		}
		if status == nil {
			var names []string
			for _, s := range statusCols {
				names = append(names, s.Title)
			}
			return pack.Board{}, ErrNoColumn{Name: opt.Column, Available: names}
		}
	case len(statusCols) > 0:
		status = &statusCols[0]
	}
	if opt.Chosen != nil {
		if status == nil {
			opt.Chosen(true, "")
		} else {
			opt.Chosen(false, status.Title)
		}
	}

	var columnOf func(it item) string
	var withoutStatus int
	if status == nil {
		groups := b.Groups
		sort.SliceStable(groups, func(i, j int) bool {
			pi, _ := strconv.ParseFloat(groups[i].Position, 64)
			pj, _ := strconv.ParseFloat(groups[j].Position, 64)
			return pi < pj
		})
		for _, g := range groups {
			out.Columns = append(out.Columns, pack.Column{ExternalID: "g:" + g.ID, Title: strings.TrimSpace(g.Title)})
		}
		columnOf = func(it item) string {
			if it.Group == nil {
				return ""
			}
			return "g:" + it.Group.ID
		}
	} else {
		labels, done := statusLabels(status.Settings)
		byLabel := map[string]string{}
		for _, l := range labels {
			id := "s:" + l.index
			byLabel[strings.ToLower(l.name)] = id
			var kind *string
			if done[l.index] {
				k := "done"
				kind = &k
			}
			out.Columns = append(out.Columns, pack.Column{ExternalID: id, Title: l.name, Kind: kind})
		}
		statusID := status.ID
		columnOf = func(it item) string {
			for _, v := range it.Values {
				if v.ID == statusID {
					text := v.Text
					if v.Label != nil {
						text = *v.Label
					}
					return byLabel[strings.ToLower(strings.TrimSpace(text))]
				}
			}
			return ""
		}
	}
	if len(out.Columns) == 0 {
		return pack.Board{}, ErrNotFound
	}

	people := map[string]bool{}
	labels := map[string]bool{}
	onBoard := map[string]bool{}
	for _, it := range items {
		onBoard[it.ID] = true
		for _, s := range it.Subitems {
			onBoard[s.ID] = true
		}
	}

	var build func(it item, parent *string, parentColumn string)
	build = func(it item, parent *string, parentColumn string) {
		col := columnOf(it)
		if col == "" {
			col = parentColumn
		}
		if col == "" {
			withoutStatus++
			col = out.Columns[0].ExternalID
		}
		card := pack.Card{
			ExternalID: it.ID,
			Number:     it.ID,
			Title:      strings.TrimSpace(it.Name),
			Column:     col,
			CreatedAt:  moment(it.CreatedAt),
			Parent:     parent,
		}
		for _, v := range it.Values {
			switch {
			case v.Type == "people":
				for _, p := range v.Persons {
					if p.Kind == "person" && p.ID != "" {
						id := p.ID.String()
						card.Assignees = append(card.Assignees, id)
						people[id] = true
					}
				}
			case v.Type == "tags":
				for _, t := range strings.Split(v.Text, ",") {
					if t = strings.TrimSpace(t); t != "" {
						card.Labels = append(card.Labels, t)
						labels[t] = true
					}
				}
			case v.ID == priorityCol:
				text := v.Text
				if v.Label != nil {
					text = *v.Label
				}
				card.Priority = strings.TrimSpace(text)
			case v.ID == dueCol:
				d := v.Text
				if v.Date != nil {
					d = *v.Date
				}
				if t, err := time.Parse("2006-01-02", strings.TrimSpace(d)); err == nil {
					card.Due = t.Format("2006-01-02")
				}
			case v.ID == estimateCol:
				if f, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64); err == nil && f > 0 {
					card.Estimate = &f
				}
			}
		}
		for _, u := range it.Updates {
			at := moment(u.CreatedAt)
			body := strings.TrimSpace(u.Body)
			if at == nil || body == "" {
				continue
			}
			var author *string
			if u.Creator != nil && u.Creator.ID != "" {
				id := u.Creator.ID.String()
				author = &id
				people[id] = true
			}
			card.Comments = append(card.Comments, pack.Comment{Author: author, At: *at, Text: body})
		}
		// Комментарии в ответе monday — от новых к старым; в takt
		// обсуждение читается сверху вниз.
		sort.SliceStable(card.Comments, func(i, j int) bool { return card.Comments[i].At.Before(card.Comments[j].At) })
		out.Cards = append(out.Cards, card)
		for _, s := range it.Subitems {
			id := it.ID
			build(s, &id, col)
		}
	}
	for _, it := range items {
		build(it, nil, "")
	}
	if len(out.Cards) > pack.MaxCards {
		return pack.Board{}, ErrTooBig{Title: out.Title, Limit: pack.MaxCards}
	}

	// Зависимость monday — «этот элемент ждёт те»: те его блокируют.
	// Связь в пакете пишется со стороны блокирующего.
	index := map[string]int{}
	for i, card := range out.Cards {
		index[card.ExternalID] = i
	}
	for _, it := range allItems(items) {
		for _, v := range it.Values {
			if v.Type != "dependency" {
				continue
			}
			for _, from := range v.Linked {
				if i, ok := index[from.String()]; ok && from.String() != it.ID {
					out.Cards[i].Links = append(out.Cards[i].Links, pack.Link{Kind: "blocks", To: it.ID})
				}
			}
		}
	}

	if err := c.people(ctx, &out, people); err != nil {
		return pack.Board{}, err
	}
	for l := range labels {
		out.Labels = append(out.Labels, pack.Label{ExternalID: l, Name: l})
	}
	sort.Slice(out.Labels, func(i, j int) bool { return out.Labels[i].Name < out.Labels[j].Name })

	if withoutStatus > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("элементы без значения колонки — встали в первую колонку: %d", withoutStatus))
	}
	if !opt.Comments {
		out.Lost = append(out.Lost, "комментарии — собраны без них")
	}
	if status != nil && len(b.Groups) > 1 {
		out.Lost = append(out.Lost, fmt.Sprintf("группы monday: %d — колонками стал статус; группы как колонки — --column group", len(b.Groups)))
	}
	withoutEmail := 0
	for _, p := range out.People {
		if p.Email == nil {
			withoutEmail++
		}
	}
	if withoutEmail > 0 {
		out.Lost = append(out.Lost, fmt.Sprintf("люди без почты в ответе monday: %d — сопоставьте их в предпросмотре вручную", withoutEmail))
	}
	out.Lost = append(out.Lost, "даты завершения — у monday их нет: готовые карточки получат момент переноса")
	out.Lost = append(out.Lost, "файлы, учёт времени, формулы, связи между досками и команды в колонке людей — их в карточке нет")
	return out, nil
}

func allItems(items []item) []item {
	var out []item
	for _, it := range items {
		out = append(out, it)
		out = append(out, it.Subitems...)
	}
	return out
}

// people — имена и почты людей, встреченных на доске: users(ids)
// пачками по сто.
func (c *Client) people(ctx context.Context, out *pack.Board, seen map[string]bool) error {
	var ids []string
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	found := map[string]pack.Person{}
	for start := 0; start < len(ids); start += 100 {
		chunk := ids[start:min(start+100, len(ids))]
		var got struct {
			Users []struct {
				ID    json.Number `json:"id"`
				Name  string      `json:"name"`
				Email string      `json:"email"`
			} `json:"users"`
		}
		q := `query ($ids: [ID!]) { users(ids: $ids, limit: 100) { id name email } }`
		if err := c.query(ctx, q, map[string]any{"ids": chunk}, &got); err != nil {
			if errors.Is(err, ErrUnreachable) || ctx.Err() != nil {
				return err
			}
			// Без имён доска не пропадает: люди приедут ключами,
			// и их сопоставят руками.
			break
		}
		for _, u := range got.Users {
			p := pack.Person{ExternalID: u.ID.String(), Name: strings.TrimSpace(u.Name)}
			if e := strings.TrimSpace(u.Email); e != "" {
				p.Email = &e
			}
			if p.Name == "" && p.Email != nil {
				p.Name = *p.Email
			}
			found[p.ExternalID] = p
		}
	}
	for _, id := range ids {
		p, ok := found[id]
		if !ok {
			p = pack.Person{ExternalID: id, Name: id}
		}
		out.People = append(out.People, p)
	}
	return nil
}

type statusLabel struct {
	index string
	name  string
}

// statusLabels — значения колонки статуса по порядку и какие из них
// значат «готово». Порядок — labels_positions_v2, а без него — номер
// значения; «готово» — done_colors, по умолчанию номер 1 (Done).
func statusLabels(settings string) ([]statusLabel, map[string]bool) {
	var s struct {
		Labels    map[string]string `json:"labels"`
		Positions map[string]int    `json:"labels_positions_v2"`
		Done      []int             `json:"done_colors"`
	}
	_ = json.Unmarshal([]byte(settings), &s)
	var out []statusLabel
	for idx, name := range s.Labels {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, statusLabel{index: idx, name: name})
		}
	}
	pos := func(l statusLabel) int {
		if p, ok := s.Positions[l.index]; ok {
			return p
		}
		n, _ := strconv.Atoi(l.index)
		return 1000 + n
	}
	sort.SliceStable(out, func(i, j int) bool {
		if pos(out[i]) != pos(out[j]) {
			return pos(out[i]) < pos(out[j])
		}
		return out[i].index < out[j].index
	})
	done := map[string]bool{}
	if s.Done == nil {
		s.Done = []int{1}
	}
	for _, d := range s.Done {
		done[strconv.Itoa(d)] = true
	}
	return out, done
}

// moment — время monday: ISO 8601 с поясом.
func moment(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		t = t.UTC()
		return &t
	}
	return nil
}
