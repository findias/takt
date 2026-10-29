package report

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/board"
)

// Сводка «требует внимания» для тимлида (ROADMAP 42.3, срез 5): что
// на сегодня застряло под тот же отбор, что у выгрузки, — без периода:
// застревание спрашивают про «сейчас», а не про квартал. Пять вопросов,
// одна карточка может отвечать на несколько — тогда она в каждом.

// AttentionCard — строка сводки; Days — сколько дней длится то, из-за
// чего она здесь: блокировка, работа, просрочка, стояние в колонке.
type AttentionCard struct {
	ID      string  `json:"id"`
	BoardID string  `json:"boardId"`
	Board   string  `json:"board"`
	Number  string  `json:"number"`
	Title   string  `json:"title"`
	Column  string  `json:"column"`
	Days    float64 `json:"days"`
	// Reason — причина блокировки; у остальных групп пусто.
	Reason string `json:"reason,omitempty"`
	// DueOn — срок у просроченных.
	DueOn *string `json:"dueOn,omitempty"`
}

type Attention struct {
	// Blocked — с открытой блокировкой, Days — с её начала.
	Blocked []AttentionCard `json:"blocked"`
	// OverPromise — в работе дольше обещания доски (её нет — группа
	// пуста: сравнивать не с чем).
	OverPromise []AttentionCard `json:"overPromise"`
	// Overdue — срок прошёл, карточка не сделана.
	Overdue []AttentionCard `json:"overdue"`
	// Unassigned — в работе, а исполнителя нет. В очереди без
	// исполнителя — норма, туда не смотрим.
	Unassigned []AttentionCard `json:"unassigned"`
	// Stale — в работе и в одной колонке дольше «без движения» доски
	// (boards.stale_days, пусто — семь дней).
	Stale []AttentionCard `json:"stale"`
	// Truncated — строк больше предела, и хвост отрезан: у сводки
	// на тысячу застрявших карточек другая проблема, чем вёрстка.
	Truncated bool `json:"truncated"`
}

// attentionLimit — сколько карточек берётся из базы. Больше — отрезано
// с пометкой: тысяча строк в пяти группах не читается.
const attentionLimit = 1000

// DefaultStaleDays — «без движения», когда у доски своего нет.
const DefaultStaleDays = 7

// Attention отвечает на пять вопросов тимлида под отбор. Период отбора
// не действует: берётся всё незаконченное на сегодня.
func (s *Service) Attention(ctx context.Context, orgID, userID string, f Filter) (Attention, error) {
	out := Attention{Blocked: []AttentionCard{}, OverPromise: []AttentionCard{}, Overdue: []AttentionCard{},
		Unassigned: []AttentionCard{}, Stale: []AttentionCard{}}
	// Период — «всё»: условие периода в sql() обязательно, а здесь
	// застрявшее интересует независимо от того, когда заведено. Начало
	// периода на незаконченное не действует (оно отсекает законченное
	// до начала), конец — завтра, чтобы взять заведённое сегодня.
	f.To = time.Now().UTC().AddDate(0, 0, 1)
	f.From = f.To.AddDate(0, 0, -MaxDays)
	f.WithArchive = false
	if err := f.Check(); err != nil {
		return out, err
	}
	return out, s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		where, args := f.sql()
		now := time.Now().UTC()
		// Пункты (подзадачи) своей колонки не имеют — они там, где
		// родитель, — и стояние в колонке у них не их. Их не берём.
		// #sql-склейка: условие собирает Filter.sql из постоянных кусков, значения идут параметрами
		rows, err := tx.Query(ctx, `
			select c.id, c.board_id, b.name, c.number, c.title,
			       (select col.name from board_columns col where col.id = c.column_id),
			       bl.blocked_at, coalesce(bl.reason, ''),
			       c.started_at, c.column_entered_at, to_char(c.due_on, 'YYYY-MM-DD'),
			       c.due_on < current_date,
			       b.sle_days, coalesce(b.stale_days, `+itoa(DefaultStaleDays)+`),
			       exists (select 1 from card_assignees a where a.card_id = c.id)
			  from (select c.id `+where+`
			          and c.finished_at is null
			          and not `+board.IsItem("c")+`) r
			  join cards c on c.id = r.id
			  join boards b on b.id = c.board_id
			  left join lateral (select x.blocked_at, x.reason from card_blocks x
			                      where x.card_id = c.id and x.unblocked_at is null
			                      order by x.blocked_at limit 1) bl on true
			 where bl.blocked_at is not null
			    or (b.sle_days is not null and c.started_at < now() - make_interval(days => b.sle_days))
			    or c.due_on < current_date
			    or (c.started_at is not null
			        and not exists (select 1 from card_assignees a where a.card_id = c.id))
			    or (c.started_at is not null
			        and c.column_entered_at < now() - make_interval(days => coalesce(b.stale_days, `+itoa(DefaultStaleDays)+`)))
			 order by b.name, c.number
			 limit `+itoa(attentionLimit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var card AttentionCard
			var blockedAt, startedAt, entered *time.Time
			var reason string
			var dueOn *string
			var overdue *bool
			var sle, stale *int
			var assigned bool
			if err := rows.Scan(&card.ID, &card.BoardID, &card.Board, &card.Number, &card.Title, &card.Column,
				&blockedAt, &reason, &startedAt, &entered, &dueOn, &overdue, &sle, &stale, &assigned); err != nil {
				return err
			}
			n++
			if n > attentionLimit {
				out.Truncated = true
				break
			}
			days := func(since time.Time) float64 { return round1(now.Sub(since).Hours() / 24) }
			if blockedAt != nil {
				c := card
				c.Days, c.Reason = days(*blockedAt), reason
				out.Blocked = append(out.Blocked, c)
			}
			if sle != nil && startedAt != nil && now.Sub(*startedAt) > time.Duration(*sle)*24*time.Hour {
				c := card
				c.Days = days(*startedAt)
				out.OverPromise = append(out.OverPromise, c)
			}
			if overdue != nil && *overdue && dueOn != nil {
				c := card
				due, _ := time.Parse(time.DateOnly, *dueOn)
				c.Days, c.DueOn = float64(int(now.Sub(due).Hours()/24)), dueOn
				out.Overdue = append(out.Overdue, c)
			}
			if startedAt != nil && !assigned {
				c := card
				c.Days = days(*startedAt)
				out.Unassigned = append(out.Unassigned, c)
			}
			if startedAt != nil && entered != nil && stale != nil &&
				now.Sub(*entered) > time.Duration(*stale)*24*time.Hour {
				c := card
				c.Days = days(*entered)
				out.Stale = append(out.Stale, c)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// Худшее — сверху: сводку читают, чтобы решить, куда идти первым.
		for _, g := range []*[]AttentionCard{&out.Blocked, &out.OverPromise, &out.Overdue, &out.Unassigned, &out.Stale} {
			sort.SliceStable(*g, func(i, j int) bool { return (*g)[i].Days > (*g)[j].Days })
		}
		return nil
	})
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
