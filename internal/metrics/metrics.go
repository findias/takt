// Package metrics — метрики потока по доске.
//
// Всё считается из отметок, заложенных третьей и четвёртой миграциями:
// когда работа началась, когда закончилась, чем закончилась. Ничего
// не хранится посчитанным: хранимый показатель — это поле, которое никто
// не обновляет, и первым же расхождением он перестаёт быть показателем.
//
// Kanban Guide определяет четыре базовые меры потока: незавершённая
// работа, время цикла, возраст работы и пропускная способность. Здесь
// ровно они, плюс прогноз, который из них следует.
package metrics

import (
	"context"
	"errors"
	"math/rand"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
)

// ErrNoData — считать не из чего. Не ошибка, а положение дел: доска ещё
// не проработала ни одной задачи, и любое число здесь было бы выдумкой.
var ErrNoData = errors.New("нет завершённой работы, считать не из чего")

type Report struct {
	// Days — окно, за которое считалось. Метрики потока без окна
	// бессмысленны: команда полугодовой давности — другая команда.
	Days int `json:"days"`

	// CycleTime — сколько дней проходит от начала работы до финиша.
	// Проценты, а не среднее: распределение времени цикла всегда
	// с длинным хвостом, и среднее по нему не отвечает ни на один вопрос.
	CycleTime *Percentiles `json:"cycleTime"`
	// Finished — сами точки: когда карточка закончена и за сколько дней.
	// Процентили отвечают «сколько обычно», а точки — «как оно
	// распределено»: три случая по двадцать дней и двадцать по три дают
	// одинаковую медиану и совершенно разный разговор на разборе.
	Finished []FinishedCard `json:"finished"`
	// Throughput — сколько карточек доведено до конца по неделям.
	Throughput []WeeklyCount `json:"throughput"`
	// WIP — сколько работы идёт прямо сейчас.
	WIP int `json:"wip"`
	// Aging — что идёт сейчас и сколько уже идёт. Главный оперативный
	// показатель: время цикла говорит о прошлом, возраст — о том, что
	// прямо сейчас застряло.
	Aging []AgingCard `json:"aging"`
	// Flow — три полосы по дням: в очереди, в работе, сделано.
	Flow []FlowDay `json:"flow"`
	// Forecast — сколько дней займёт довести до конца столько-то карточек.
	Forecast []ForecastPoint `json:"forecast"`
	// Discarded — сколько карточек убрано с доски незавершёнными.
	// Показывается рядом с пропускной способностью намеренно: в неё они
	// не входят, и молчать об их числе значит скрывать половину картины.
	Discarded int `json:"discarded"`
}

type Percentiles struct {
	P50 float64 `json:"p50"`
	P85 float64 `json:"p85"`
	P95 float64 `json:"p95"`
	// Count — на скольких карточках это посчитано. Проценты по трём
	// карточкам — не проценты, и клиент обязан иметь возможность
	// об этом сказать.
	Count int `json:"count"`
}

// FinishedCard — доведённая до конца карточка на точечной диаграмме.
type FinishedCard struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// FinishedOn — день по календарю, а не отметка времени: точки
	// расставляются по дням, и час на диаграмме не разглядеть.
	FinishedOn string  `json:"finishedOn"`
	Days       float64 `json:"days"`
}

type WeeklyCount struct {
	Week  string `json:"week"`
	Count int    `json:"count"`
}

type AgingCard struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Column string  `json:"column"`
	Days   float64 `json:"days"`
	// Blocked — заблокированная карточка стареет, ничего не делая;
	// в списке старения это первое, что нужно видеть.
	Blocked bool `json:"blocked"`
}

type FlowDay struct {
	Day        string `json:"day"`
	Queued     int    `json:"queued"`
	InProgress int    `json:"inProgress"`
	Done       int    `json:"done"`
}

type ForecastPoint struct {
	Cards int `json:"cards"`
	// Days — за столько дней укладываются 50, 85 и 95 испытаний из ста.
	P50 int `json:"p50"`
	P85 int `json:"p85"`
	P95 int `json:"p95"`
}

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

// Report собирает отчёт по доске за последние days дней.
func (s *Service) Report(ctx context.Context, orgID, userID, boardID string, days int) (Report, error) {
	if days < 7 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	report := Report{Days: days, Throughput: []WeeklyCount{}, Aging: []AgingCard{},
		Flow: []FlowDay{}, Finished: []FinishedCard{}}

	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		// Доска должна быть видна: недоступная неотличима от несуществующей.
		var exists bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from boards
			                where id = $1 and archived_at is null)`, boardID).
			Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNoData
		}

		if err := s.cycleTime(ctx, tx, boardID, days, &report); err != nil {
			return err
		}
		if err := s.throughput(ctx, tx, boardID, days, &report); err != nil {
			return err
		}
		if err := s.aging(ctx, tx, boardID, &report); err != nil {
			return err
		}
		return s.flow(ctx, tx, boardID, days, &report)
	})
	if err != nil {
		return Report{}, err
	}

	report.Forecast = forecast(report.Throughput)
	return report, nil
}

func (s *Service) cycleTime(ctx context.Context, tx pgx.Tx, boardID string, days int, out *Report) error {
	var p Percentiles
	// Считается только по доведённому до конца: выброшенная карточка
	// не имеет времени цикла, она имеет время до отказа, а это другая
	// величина и другой разговор.
	err := tx.QueryRow(ctx, `
		select coalesce(percentile_cont(0.50) within group (order by d), 0),
		       coalesce(percentile_cont(0.85) within group (order by d), 0),
		       coalesce(percentile_cont(0.95) within group (order by d), 0),
		       count(*)
		  from (
			select extract(epoch from (finished_at - started_at)) / 86400.0 as d
			  from cards
			 where board_id = $1 and outcome = 'done'
			   and started_at is not null and finished_at is not null
			   and finished_at >= now() - make_interval(days => $2)
		  ) t`, boardID, days).Scan(&p.P50, &p.P85, &p.P95, &p.Count)
	if err != nil {
		return err
	}
	if p.Count > 0 {
		out.CycleTime = &p
	}

	if err := tx.QueryRow(ctx, `
		select count(*) from cards
		 where board_id = $1 and outcome = 'discarded'
		   and updated_at >= now() - make_interval(days => $2)`,
		boardID, days).Scan(&out.Discarded); err != nil {
		return err
	}

	// Точки той же выборки, что и процентили: разойтись им негде,
	// потому что условие одно и то же.
	rows, err := tx.Query(ctx, `
		select id, title, to_char(finished_at, 'YYYY-MM-DD'),
		       extract(epoch from (finished_at - started_at)) / 86400.0
		  from cards
		 where board_id = $1 and outcome = 'done'
		   and started_at is not null and finished_at is not null
		   and finished_at >= now() - make_interval(days => $2)
		 order by finished_at`, boardID, days)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c FinishedCard
		if err := rows.Scan(&c.ID, &c.Title, &c.FinishedOn, &c.Days); err != nil {
			return err
		}
		out.Finished = append(out.Finished, c)
	}
	return rows.Err()
}

func (s *Service) throughput(ctx context.Context, tx pgx.Tx, boardID string, days int, out *Report) error {
	// Недели без единой законченной карточки тоже нужны: без них прогноз
	// считает, что команда всегда что-то доводит до конца, а это неправда.
	rows, err := tx.Query(ctx, `
		with weeks as (
			select generate_series(
				date_trunc('week', now() - make_interval(days => $2)),
				date_trunc('week', now()),
				interval '1 week') as week
		)
		select to_char(w.week, 'YYYY-MM-DD'),
		       count(c.id) filter (where c.outcome = 'done')
		  from weeks w
		  left join cards c
		    on c.board_id = $1
		   and date_trunc('week', c.finished_at) = w.week
		 group by w.week
		 order by w.week`, boardID, days)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var wc WeeklyCount
		if err := rows.Scan(&wc.Week, &wc.Count); err != nil {
			return err
		}
		out.Throughput = append(out.Throughput, wc)
	}
	return rows.Err()
}

func (s *Service) aging(ctx context.Context, tx pgx.Tx, boardID string, out *Report) error {
	rows, err := tx.Query(ctx, `
		select c.id, c.title, col.name,
		       extract(epoch from (now() - c.started_at)) / 86400.0,
		       exists (select 1 from card_blocks b
		                where b.card_id = c.id and b.unblocked_at is null)
		  from cards c
		  join board_columns col on col.id = c.column_id
		 where c.board_id = $1 and c.archived_at is null
		   and c.started_at is not null and c.finished_at is null
		 order by c.started_at`, boardID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a AgingCard
		if err := rows.Scan(&a.ID, &a.Title, &a.Column, &a.Days, &a.Blocked); err != nil {
			return err
		}
		out.Aging = append(out.Aging, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	out.WIP = len(out.Aging)
	return nil
}

// flow — три полосы по дням.
//
// Считается из отметок карточки, а не из журнала переходов. Разница
// принципиальная и её стоит знать: по отметкам видно «в очереди — в работе
// — сделано», но не видно, в какой именно колонке карточка стояла. Полная
// диаграмма накопления по колонкам требует проигрывания журнала, а он
// у нас есть; когда понадобится, это делается отдельно и стоит дороже.
func (s *Service) flow(ctx context.Context, tx pgx.Tx, boardID string, days int, out *Report) error {
	rows, err := tx.Query(ctx, `
		with days as (
			select generate_series(
				date_trunc('day', now() - make_interval(days => $2)),
				date_trunc('day', now()),
				interval '1 day') as day
		)
		select to_char(d.day, 'YYYY-MM-DD'),
		       count(c.id) filter (
		           where c.created_at <= d.day + interval '1 day'
		             and (c.started_at is null or c.started_at > d.day + interval '1 day')),
		       count(c.id) filter (
		           where c.started_at <= d.day + interval '1 day'
		             and (c.finished_at is null or c.finished_at > d.day + interval '1 day')),
		       count(c.id) filter (where c.finished_at <= d.day + interval '1 day')
		  from days d
		  left join cards c on c.board_id = $1 and c.archived_at is null
		 group by d.day
		 order by d.day`, boardID, days)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f FlowDay
		if err := rows.Scan(&f.Day, &f.Queued, &f.InProgress, &f.Done); err != nil {
			return err
		}
		out.Flow = append(out.Flow, f)
	}
	return rows.Err()
}

// forecast — прогноз методом Монте-Карло.
//
// Вопрос «когда будет готово N карточек» имеет ответ только в виде
// вероятности, и притворяться иначе — главный способ обмануть себя
// в планировании. Испытание берёт случайные недели из прошлого
// и складывает их, пока не наберётся нужное число; тысяча испытаний
// даёт распределение, из которого и берутся проценты.
//
// Никакой хитрости: прошлое команды — единственное, что у нас есть,
// и прогноз честно говорит, что будет, если дальше будет как было.
func forecast(weekly []WeeklyCount) []ForecastPoint {
	samples := make([]int, 0, len(weekly))
	total := 0
	for _, w := range weekly {
		samples = append(samples, w.Count)
		total += w.Count
	}
	// Без единой законченной карточки прогноз невозможен: складывать
	// нули можно бесконечно.
	if total == 0 || len(samples) < 4 {
		return nil
	}

	// Источник случайности с постоянным зерном: один и тот же прошлый
	// поток обязан давать один и тот же прогноз, иначе два человека
	// увидят разные числа и потратят день на выяснение, чей верный.
	source := rand.New(rand.NewSource(int64(total*1000 + len(samples))))

	const trials = 1000
	out := []ForecastPoint{}
	for _, target := range []int{5, 10, 20} {
		results := make([]int, 0, trials)
		for i := 0; i < trials; i++ {
			done, weeks := 0, 0
			for done < target && weeks < 520 {
				done += samples[source.Intn(len(samples))]
				weeks++
			}
			results = append(results, weeks*7)
		}
		sort.Ints(results)
		out = append(out, ForecastPoint{
			Cards: target,
			P50:   results[len(results)*50/100],
			P85:   results[len(results)*85/100],
			P95:   results[len(results)*95/100],
		})
	}
	return out
}
