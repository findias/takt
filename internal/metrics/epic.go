package metrics

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/board"
)

// Эпик с прогнозом (ROADMAP 42.3, срез 4). К «готово X из Y» на портфеле
// — когда будет готово остальное и как рос объём. Прогноз — тем же
// Монте-Карло, что на «Потоке», по пропускной способности досок команд,
// где лежат части: прошлое этих досок — единственное, что есть.

// EpicForecast — ответ по одному эпику. Листья — работа поддерева без
// собственных частей, через все доски: промежуточный узел — это его
// листья, и считать его отдельно значило бы считать дважды.
type EpicForecast struct {
	Total     int `json:"total"`
	Done      int `json:"done"`
	Remaining int `json:"remaining"`
	// Weeks — сколько недель прошлого взято; Finished — сколько за них
	// доведено на досках частей; Boards — сколько таких досок. Три числа,
	// без которых дате нельзя верить: прогноз по двум карточкам
	// за три месяца — гадание.
	Weeks    int `json:"weeks"`
	Finished int `json:"finished"`
	Boards   int `json:"boards"`
	// Forecast — пусто, когда считать не из чего: ни одной доведённой
	// части или ни одной доведённой карточки на досках частей. Пусто
	// и когда всё сделано — тогда Remaining равен нулю.
	Forecast *EpicDates `json:"forecast"`
	// Scope — объём и сделанное по неделям с первой части: рост объёма
	// виден отдельно от прогресса.
	Scope []ScopePoint `json:"scope"`
}

// EpicDates — даты, к которым укладываются 50, 85 и 95 испытаний из ста.
type EpicDates struct {
	P50 string `json:"p50"`
	P85 string `json:"p85"`
	P95 string `json:"p95"`
}

type ScopePoint struct {
	Week  string `json:"week"`
	Scope int    `json:"scope"`
	Done  int    `json:"done"`
}

// epicWeeks — сколько недель прошлого берёт прогноз эпика.
const epicWeeks = 12

func (s *Service) EpicForecast(ctx context.Context, orgID, userID, cardID string) (EpicForecast, error) {
	out := EpicForecast{Scope: []ScopePoint{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		// Корень должен быть виден; иначе — «нет», как у дерева.
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from cards where id = $1 and archived_at is null)`,
			cardID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return board.ErrNotFound
		}
		type leaf struct {
			board    string
			created  time.Time
			finished *time.Time
		}
		rows, err := tx.Query(ctx, `
			with recursive down(card, depth) as (
				select id, 0 from cards where id = $1
				union all
				select l.to_card, d.depth + 1
				  from down d join card_links l on l.from_card = d.card and l.kind = 'subtask'
				 where d.depth < $2
				   and not exists (select 1 from cards a where a.id = l.to_card and a.archived_at is not null)
			)
			select distinct c.board_id, c.created_at,
			       case when c.outcome = 'done' or c.done_at is not null
			            then coalesce(c.finished_at, c.done_at) end
			  from down d
			  join cards c on c.id = d.card
			 where d.depth > 0
			   and not exists (select 1 from card_links l join cards k on k.id = l.to_card
			                    where l.from_card = c.id and l.kind = 'subtask' and k.archived_at is null)`,
			cardID, board.MaxSubtaskDepth)
		if err != nil {
			return err
		}
		var leaves []leaf
		boards := map[string]bool{}
		for rows.Next() {
			var l leaf
			if err := rows.Scan(&l.board, &l.created, &l.finished); err != nil {
				rows.Close()
				return err
			}
			leaves = append(leaves, l)
			boards[l.board] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		out.Total = len(leaves)
		for _, l := range leaves {
			if l.finished != nil {
				out.Done++
			}
		}
		out.Remaining = out.Total - out.Done
		out.Boards = len(boards)
		out.Weeks = epicWeeks
		if len(leaves) == 0 {
			return nil
		}

		// Объём и сделанное по неделям с первой части до сейчас.
		first := leaves[0].created
		for _, l := range leaves {
			if l.created.Before(first) {
				first = l.created
			}
		}
		now := time.Now().UTC()
		for edge := weekEnd(first); ; edge = edge.AddDate(0, 0, 7) {
			if edge.After(now) {
				edge = now
			}
			p := ScopePoint{Week: edge.Format(time.DateOnly)}
			for _, l := range leaves {
				if !l.created.After(edge) {
					p.Scope++
				}
				if l.finished != nil && !l.finished.After(edge) {
					p.Done++
				}
			}
			out.Scope = append(out.Scope, p)
			if edge.Equal(now) {
				break
			}
		}

		// Пропускная способность досок частей — по неделям, как на «Потоке».
		ids := make([]string, 0, len(boards))
		for id := range boards {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		wrows, err := tx.Query(ctx, `
			with weeks as (
				select generate_series(
					date_trunc('week', now() - make_interval(days => $2)),
					date_trunc('week', now()),
					interval '1 week') as week
			)
			select count(c.id) filter (where c.outcome = 'done' and date_trunc('week', c.finished_at) = w.week)
			  from weeks w
			  left join cards c
			    on c.board_id = any($1::uuid[])
			   and date_trunc('week', c.finished_at) = w.week
			   and not `+board.IsItem("c")+`
			 group by w.week
			 order by w.week`, ids, epicWeeks*7)
		if err != nil {
			return err
		}
		var samples []int
		for wrows.Next() {
			var n int
			if err := wrows.Scan(&n); err != nil {
				wrows.Close()
				return err
			}
			samples = append(samples, n)
			out.Finished += n
		}
		wrows.Close()
		if err := wrows.Err(); err != nil {
			return err
		}
		// Без доведённых частей — «считать не из чего», а не дата:
		// прошлое доски говорит о доске, а не об этом эпике.
		if out.Remaining == 0 || out.Done == 0 || out.Finished == 0 {
			return nil
		}
		p50, p85, p95 := simulate(samples, out.Remaining)
		day := func(weeks int) string { return now.AddDate(0, 0, weeks*7).Format(time.DateOnly) }
		out.Forecast = &EpicDates{P50: day(p50), P85: day(p85), P95: day(p95)}
		return nil
	})
	return out, err
}

// weekEnd — ближайшее воскресенье (конец недели) не раньше t.
func weekEnd(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
	for t.Weekday() != time.Sunday {
		t = t.AddDate(0, 0, 1)
	}
	return t
}
