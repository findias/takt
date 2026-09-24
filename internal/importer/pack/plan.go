package pack

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer"
)

// Plan переводит доску пакета в промежуточную модель переноса.
//
// Что в доске не так, становится претензией к карточке, а не отказом
// всему пакету, — как и у таблицы: колонка, которой нет, родитель
// на чужой доске, цикл подзадач. Номер строки у пакета — порядковый
// номер карточки на доске, с единицы.
func (p *Package) Plan(board int) (importer.Plan, error) {
	if board < 1 || board > len(p.Boards) {
		return importer.Plan{}, fmt.Errorf("в пакете %d досок — выберите доску из списка", len(p.Boards))
	}
	b := p.Boards[board-1]
	plan := importer.Plan{
		Source:      p.Manifest.Source.System,
		SourceName:  Systems[p.Manifest.Source.System],
		Rows:        len(b.Cards),
		ColumnKinds: map[string]string{},
		// История собрана выгрузчиком — дотягивать её потом не нужно.
		HistoryCollected: b.HistoryCollected,
	}
	plan.Lost = append(append(plan.Lost, p.Manifest.Lost...), b.Lost...)

	columns := map[string]string{}
	for _, c := range b.Columns {
		title := strings.TrimSpace(c.Title)
		columns[c.ExternalID] = title
		if _, seen := plan.ColumnKinds[strings.ToLower(title)]; seen || title == "" {
			continue
		}
		plan.Columns = append(plan.Columns, title)
		kind := ""
		if c.Kind != nil {
			kind = *c.Kind
		}
		plan.ColumnKinds[strings.ToLower(title)] = kind
	}
	people := map[string]Person{}
	for _, person := range b.People {
		people[person.ExternalID] = person
	}
	labels := map[string]string{}
	for _, l := range b.Labels {
		labels[l.ExternalID] = strings.TrimSpace(l.Name)
	}
	cards := map[string]bool{}
	for _, c := range b.Cards {
		cards[c.ExternalID] = true
	}
	parents := parentsWithoutCycles(b.Cards)
	plan.Portfolio = b.Level == LevelPortfolio

	// Родитель на другой доске пакета (этап 33.6): эпик на портфеле,
	// фича на доске команды. Доски переносятся по одной и в любом
	// порядке, поэтому связь ищется в обе стороны: у карточки этой доски
	// — родитель, переехавший раньше, у родителя с этой доски — части,
	// переехавшие раньше. Какая доска ни переехала бы второй, связь
	// заводит она.
	elsewhere := map[string]string{}
	nameOf := map[string]string{}
	for i, other := range p.Boards {
		if i == board-1 {
			continue
		}
		for _, c := range other.Cards {
			if _, seen := elsewhere[c.ExternalID]; !seen {
				elsewhere[c.ExternalID] = strings.TrimSpace(other.Title)
				nameOf[c.ExternalID] = strings.TrimSpace(strings.TrimSpace(c.Number) + " " + strings.TrimSpace(c.Title))
			}
			if c.Parent != nil && cards[*c.Parent] && !cards[c.ExternalID] {
				plan.ForeignParts = append(plan.ForeignParts, importer.ForeignPart{Parent: *c.Parent, Child: c.ExternalID})
			}
		}
	}

	for i, c := range b.Cards {
		row := i + 1
		problem := func(f importer.Field, value, format string, args ...any) {
			plan.Problems = append(plan.Problems, importer.Problem{Row: row, Field: f, Value: value, Message: fmt.Sprintf(format, args...)})
		}
		card := importer.Card{
			Row: row, Title: strings.TrimSpace(c.Title), Description: c.Description,
			ExternalID: c.ExternalID, Number: strings.TrimSpace(c.Number), Estimate: c.Estimate, Created: c.CreatedAt, Done: c.FinishedAt,
		}
		if card.Title == "" || card.ExternalID == "" {
			plan.Problems = append(plan.Problems, importer.Problem{Row: row, Field: importer.FieldTitle, Skipped: true,
				Message: "карточка без названия или без ключа — не переносится"})
			continue
		}
		column, ok := columns[c.Column]
		if !ok {
			problem(importer.FieldColumn, c.Column, "колонки «%s» на доске пакета нет — карточка встанет в первую", c.Column)
		}
		card.Column = column
		if card.Estimate != nil && *card.Estimate <= 0 {
			problem(importer.FieldEstimate, fmt.Sprint(*card.Estimate), "оценка должна быть больше нуля — карточка переедет без оценки")
			card.Estimate = nil
		}
		if c.Priority != "" {
			if pr, ok := importer.PriorityOf(c.Priority); ok {
				card.Priority = pr
			} else {
				problem(importer.FieldPriority, c.Priority, "приоритет «%s» незнаком — карточка переедет с обычным", c.Priority)
			}
		}
		if c.Due != "" {
			if d, err := time.Parse("2006-01-02", c.Due); err == nil {
				card.Due = &d
			} else {
				problem(importer.FieldDue, c.Due, "срок «%s» — не дата вида 2026-09-30, поле не переносится", c.Due)
			}
		}
		for _, id := range c.Assignees {
			person, ok := people[id]
			switch {
			case !ok:
			case person.Email != nil && strings.TrimSpace(*person.Email) != "":
				email := strings.ToLower(strings.TrimSpace(*person.Email))
				card.Assignees = append(card.Assignees, email)
				if person.Name != "" {
					if plan.Names == nil {
						plan.Names = map[string]string{}
					}
					plan.Names[email] = person.Name
				}
			default:
				card.Unmatched = append(card.Unmatched, importer.Unmatched{Key: person.ExternalID, Name: person.Name})
			}
		}
		for _, id := range c.Labels {
			if name := labels[id]; name != "" {
				card.Labels = append(card.Labels, name)
			}
		}
		if c.Parent != nil && *c.Parent != "" {
			other, onOther := elsewhere[*c.Parent]
			switch {
			case !cards[*c.Parent] && onOther:
				card.Parent = *c.Parent
				card.ParentBoard = other
				card.ParentName = cmp.Or(nameOf[*c.Parent], *c.Parent)
			case !cards[*c.Parent]:
				problem("", *c.Parent, "родителя «%s» на этой доске пакета нет — карточка переедет без него", *c.Parent)
			case parents[c.ExternalID] == "":
				problem("", *c.Parent, "подзадачи замыкаются в цикл через «%s» — связь с родителем не переносится", *c.Parent)
			default:
				card.Parent = *c.Parent
			}
		}
		for _, l := range c.Links {
			switch {
			case l.Kind != "blocks" && l.Kind != "relates":
				problem("", l.Kind, "связь вида «%s» незнакома — не переносится", l.Kind)
			case !cards[l.To] || l.To == c.ExternalID:
				problem("", l.To, "связь с «%s»: такой карточки на этой доске пакета нет — не переносится", l.To)
			default:
				card.Links = append(card.Links, importer.Link{Kind: l.Kind, To: l.To})
			}
		}
		card.Comments = comments(c.Comments, people)
		card.History = comments(c.History, people)
		plan.Cards = append(plan.Cards, card)
	}
	return plan, nil
}

// parentsWithoutCycles — родитель каждой карточки, у которой он есть
// и не замыкает цикла. Цикл в пакете — ошибка источника или выгрузчика,
// но переносить его нельзя: карточка оказалась бы своей же подзадачей.
func parentsWithoutCycles(cards []Card) map[string]string {
	parent := map[string]string{}
	for _, c := range cards {
		if c.Parent != nil && *c.Parent != "" && *c.Parent != c.ExternalID {
			parent[c.ExternalID] = *c.Parent
		}
	}
	out := map[string]string{}
	for child, p := range parent {
		// Цикл — если цепочка родителей возвращается к самой карточке.
		// Карточка, которая лишь ведёт к чужому циклу, своего родителя
		// сохраняет: отброшены будут связи внутри цикла.
		seen := map[string]bool{child: true}
		at := p
		for at != "" && !seen[at] {
			seen[at] = true
			at = parent[at]
		}
		if at != child {
			out[child] = p
		}
	}
	return out
}

// comments — реплики или записи истории пакета в промежуточной модели:
// автор называется по имени и ищется по почте или ключу источника.
func comments(list []Comment, people map[string]Person) []importer.Comment {
	var out []importer.Comment
	for _, cm := range list {
		text := strings.TrimSpace(cm.Text)
		if text == "" {
			continue
		}
		comment := importer.Comment{At: cm.At, Text: text}
		if cm.Author != nil {
			if person, ok := people[*cm.Author]; ok {
				comment.AuthorName = person.Name
				comment.AuthorKey = "source:" + person.ExternalID
				if person.Email != nil && strings.TrimSpace(*person.Email) != "" {
					comment.AuthorEmail = strings.ToLower(strings.TrimSpace(*person.Email))
					comment.AuthorKey = comment.AuthorEmail
				}
			}
		}
		out = append(out, comment)
	}
	return out
}
