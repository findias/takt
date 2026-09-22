package board

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Задачи человека со всех досок (просьба владельца 22.09.2026: «вывод
// задач по пользователю с разных досок»).
//
// Видимость — политиками базы, как везде: в список попадают карточки
// только тех досок, что видит спрашивающий, даже если человек работает
// и на закрытых. Иначе вкладка «Задачи» стала бы окном в чужие доски.

// TaskLimit — сколько задач отдаём за раз. У человека после переноса
// бывают сотни карточек; больше таблица без листания не покажет.
const TaskLimit = 500

// Task — карточка в списке задач человека.
type Task struct {
	ID              string     `json:"id"`
	Number          string     `json:"number"`
	Title           string     `json:"title"`
	BoardID         string     `json:"boardId"`
	BoardName       string     `json:"boardName"`
	Column          string     `json:"column"`
	ColumnKind      string     `json:"columnKind"`
	Priority        string     `json:"priority"`
	DueOn           *string    `json:"dueOn"`
	StartedAt       *time.Time `json:"startedAt"`
	ColumnEnteredAt time.Time  `json:"columnEnteredAt"`
	Outcome         *string    `json:"outcome"`
	Blocked         bool       `json:"blocked"`
	Labels          []Label    `json:"labels"`
}

// TaskList — задачи и то, всё ли показано.
type TaskList struct {
	Tasks []Task `json:"tasks"`
	// Truncated — задач больше TaskLimit, показаны первые.
	Truncated bool `json:"truncated"`
}

// Tasks — карточки, где personID исполнитель, на досках, которые видит
// viewerID. withDone — и законченные; без него — только идущая работа.
// Порядок: сначала то, у чего есть срок, по сроку; затем по важности.
func (s *Service) Tasks(ctx context.Context, orgID, viewerID, personID string, withDone bool) (TaskList, error) {
	out := TaskList{Tasks: []Task{}}
	err := s.db.InTenant(ctx, orgID, viewerID, func(tx pgx.Tx) error {
		// Человек не из этой организации — «не найден», а не пустой
		// список: пустой читался бы как «у него нет задач».
		var member bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from memberships where org_id = $1 and user_id = $2)`,
			orgID, personID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `
			select c.id, c.number, c.title, b.id, b.name, col.name, col.kind,
			       c.priority, to_char(c.due_on, 'YYYY-MM-DD'), c.started_at,
			       c.column_entered_at, c.outcome,
			       exists (select 1 from card_blocks k
			                where k.card_id = c.id and k.unblocked_at is null)
			  from card_assignees a
			  join cards c on c.id = a.card_id
			  join boards b on b.id = c.board_id
			  join board_columns col on col.id = c.column_id
			 where a.user_id = $1
			   and c.archived_at is null and b.archived_at is null
			   and ($2 or c.outcome is null)
			 order by c.due_on nulls last,
			          case c.priority when 'highest' then 0 when 'high' then 1
			                          when 'medium' then 2 else 3 end,
			          b.name, col.position, c.position
			 limit $3`, personID, withDone, TaskLimit+1)
		if err != nil {
			return err
		}
		index := map[string]int{}
		var ids []string
		for rows.Next() {
			var t Task
			if err := rows.Scan(&t.ID, &t.Number, &t.Title, &t.BoardID, &t.BoardName,
				&t.Column, &t.ColumnKind, &t.Priority, &t.DueOn, &t.StartedAt,
				&t.ColumnEnteredAt, &t.Outcome, &t.Blocked); err != nil {
				rows.Close()
				return err
			}
			t.Labels = []Label{}
			if len(out.Tasks) == TaskLimit {
				out.Truncated = true
				continue
			}
			index[t.ID] = len(out.Tasks)
			ids = append(ids, t.ID)
			out.Tasks = append(out.Tasks, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		labels, err := tx.Query(ctx, `
			select cl.card_id, `+labelColumns+labelJoins+`
			  join card_labels cl on cl.label_id = l.id
			 where cl.card_id = any($1)
			 order by l.kind, lower(l.name)`, ids)
		if err != nil {
			return err
		}
		defer labels.Close()
		for labels.Next() {
			var cardID string
			var l Label
			if err := labels.Scan(&cardID, &l.ID, &l.Name, &l.Tone, &l.Scope, &l.ScopeID,
				&l.ScopeName, &l.Archived, &l.Kind); err != nil {
				return err
			}
			i := index[cardID]
			out.Tasks[i].Labels = append(out.Tasks[i].Labels, l)
		}
		return labels.Err()
	})
	if errors.Is(err, ErrNotFound) {
		return TaskList{}, ErrNotFound
	}
	return out, err
}
