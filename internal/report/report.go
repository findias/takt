// Package report — выгрузка для руководства (этап 24): карточки
// по параметрам одной плоской таблицей и сводка потока за период.
//
// Руководитель просит «пришлите табличку», и без выгрузки эта просьба
// стоит тимлиду вечера: собрать карточки с нескольких досок, досчитать
// время цикла, свести по неделям. Здесь то же делает база.
//
// Отбор один на всё: строки, которые идут в лист «Данные», сперва
// откладываются во временную таблицу, и сводка считается из неё же.
// Разойтись «Данным» и «Сводке» в том, что считать, поэтому негде —
// так же, как у метрик доски условие одно на все запросы.
//
// Видимость — политиками базы, как везде: в выгрузку попадают только
// доски, которые спрашивающий видит на экране.
package report

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
)

// MaxRows — сколько карточек берёт одна выгрузка. Для организации
// на полторы сотни человек это годы работы; больше просят, когда
// забыли сузить период, и лучше сказать об этом, чем молча прислать
// файл, который Excel открывает минуту.
const MaxRows = 50_000

// MaxDays — самый длинный период. Сводка раскладывает поток по дням,
// и пять лет дней — предел, после которого диаграмма перестаёт быть
// диаграммой.
const MaxDays = 5 * 366

// flowDailyUpTo — до какой длины периода накопительная идёт по дням.
// Дальше — по неделям: и точек на диаграмме меньше, и база не считает
// дни × карточки на пятилетнем периоде.
const flowDailyUpTo = 120 * 24 * time.Hour

// Состояния карточки — те же, что видит человек на доске: ждёт, идёт,
// сделана, отброшена.
const (
	StateQueued    = "queued"
	StateActive    = "active"
	StateDone      = "done"
	StateDiscarded = "discarded"
)

var states = []string{StateQueued, StateActive, StateDone, StateDiscarded}

var priorities = []string{"low", "medium", "high", "highest"}

var (
	// ErrBadFilter — параметры не складываются в вопрос. Текст
	// ошибки говорит, какой именно параметр и что с ним сделать.
	ErrBadFilter = errors.New("параметры выгрузки")
	// ErrTooMany — под параметры попадает больше MaxRows карточек.
	ErrTooMany = errors.New("слишком много карточек для одной выгрузки")
)

// BadFilter — параметр, с которым выгрузку не собрать.
type BadFilter struct{ Message string }

func (e *BadFilter) Error() string { return e.Message }
func (e *BadFilter) Unwrap() error { return ErrBadFilter }

// TooMany — сколько карточек попало и сколько можно.
type TooMany struct{ Total, Limit int }

func (e *TooMany) Error() string {
	return fmt.Sprintf("под параметры попадает %d карточек, выгрузка берёт не больше %d — "+
		"сузьте период или выберите доски", e.Total, e.Limit)
}
func (e *TooMany) Unwrap() error { return ErrTooMany }

// Filter — параметры отбора. Понятия те же, что на доске: второй
// словарь для отчёта значил бы, что «в работе» на доске и в отчёте —
// разные вещи.
//
// Пустой список значит «любые».
type Filter struct {
	// From и To — период, обе даты включительно. В выгрузку попадает
	// карточка, которая жила в периоде: заведена до его конца и
	// не закончена до его начала.
	From, To    time.Time
	Boards      []string
	Teams       []string // подразделение берёт и всё, что внутри него
	Assignees   []string
	Labels      []string
	Priorities  []string
	States      []string
	Iteration   string
	WithArchive bool // и карточки, убранные в архив
}

// Check проверяет параметры и возвращает понятный отказ.
func (f *Filter) Check() error {
	if f.From.IsZero() || f.To.IsZero() {
		return &BadFilter{"укажите период: from и to датами вида 2026-09-01"}
	}
	if f.To.Before(f.From) {
		return &BadFilter{"конец периода раньше начала — поменяйте даты местами"}
	}
	if f.To.Sub(f.From) > MaxDays*24*time.Hour {
		return &BadFilter{fmt.Sprintf("период длиннее %d дней — выгрузите его частями", MaxDays)}
	}
	for _, p := range f.Priorities {
		if !slices.Contains(priorities, p) {
			return &BadFilter{"приоритет бывает low, medium, high или highest"}
		}
	}
	for _, s := range f.States {
		if !slices.Contains(states, s) {
			return &BadFilter{"состояние бывает queued, active, done или discarded"}
		}
	}
	return nil
}

// Row — строка листа «Данные»: одна карточка со всем, что о ней
// спрашивают. Поля JSON — как в API, а не как в базе.
type Row struct {
	Number      string     `json:"number"`
	Title       string     `json:"title"`
	Board       string     `json:"board"`
	Team        string     `json:"team"`
	Column      string     `json:"column"`
	State       string     `json:"state"`
	Priority    string     `json:"priority"`
	Estimate    *float64   `json:"estimate"`
	Assignees   string     `json:"assignees"`
	Labels      string     `json:"labels"`
	Iteration   string     `json:"iteration"`
	Parent      string     `json:"parent"`
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	DueOn       *string    `json:"dueOn"`
	CycleDays   *float64   `json:"cycleTimeDays"`
	AgeDays     *float64   `json:"ageDays"`
	Blocked     bool       `json:"blocked"`
	BlockReason string     `json:"blockReason"`
	Imported    bool       `json:"imported"`
	Archived    bool       `json:"archived"`
	// Container — эпик, карточка с внуками (этап 32.6): строкой в данных
	// остаётся, но в сводке не считается — её работа посчитана частями.
	Container bool `json:"container"`
}

// Summary — лист «Сводка»: то, что показывает «Поток», но за период
// и по отобранным карточкам.
type Summary struct {
	// Chosen — отбор словами. Файл пересылают, и получатель должен
	// видеть, какие доски и чьи карточки в нём, не спрашивая автора.
	Chosen     Chosen       `json:"chosen"`
	Cards      int          `json:"cards"`
	Finished   int          `json:"finished"`
	Discarded  int          `json:"discarded"`
	WIP        int          `json:"wip"`
	CycleTime  *Percentiles `json:"cycleTime"`
	Age        *Percentiles `json:"age"`
	Throughput []WeekCount  `json:"throughput"`
	// FlowStep — шаг накопительной: день или неделя. Длинный период
	// по дням — тысячи точек, которых на диаграмме не различить.
	FlowStep   string         `json:"flowStep"`
	Flow       []FlowDay      `json:"flow"`
	Iterations []IterationRow `json:"iterations"`
	Teams      []TeamRow      `json:"teams"`
}

// Chosen — названия выбранного в отборе; пустой список — «любые».
type Chosen struct {
	Boards     []string `json:"boards"`
	Teams      []string `json:"teams"`
	Assignees  []string `json:"assignees"`
	Labels     []string `json:"labels"`
	Iteration  string   `json:"iteration"`
	Priorities []string `json:"priorities"`
	States     []string `json:"states"`
}

// Percentiles — медиана и 85-я: первая говорит, как обычно, вторая —
// что обещать.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P85 float64 `json:"p85"`
}

type WeekCount struct {
	Week  string `json:"week"`
	Count int    `json:"count"`
}

type FlowDay struct {
	Day        string `json:"day"`
	Queued     int    `json:"queued"`
	InProgress int    `json:"inProgress"`
	Done       int    `json:"done"`
}

// IterationRow — выполнение итерации: сколько было в ней и сколько
// из этого сделано. Считается по отобранным карточкам, а не по всей
// итерации: отчёт отвечает на свой вопрос.
type IterationRow struct {
	Name     string `json:"name"`
	Board    string `json:"board"`
	StartsOn string `json:"startsOn"`
	EndsOn   string `json:"endsOn"`
	Closed   bool   `json:"closed"`
	Planned  int    `json:"planned"`
	Done     int    `json:"done"`
}

// TeamRow — разрез по подразделениям: чьё подразделение ведёт доску.
type TeamRow struct {
	Team     string   `json:"team"`
	Cards    int      `json:"cards"`
	Finished int      `json:"finished"`
	WIP      int      `json:"wip"`
	CycleP50 *float64 `json:"cycleTimeP50"`
	CycleP85 *float64 `json:"cycleTimeP85"`
}

// Sink принимает выгрузку: сперва строки по одной, потом сводку.
// Строки идут прямо из курсора базы — отчёт на десятки тысяч карточек
// не собирается в памяти целиком.
type Sink interface {
	Row(Row) error
	Summary(Summary) error
}

type Service struct {
	db *store.Store
	// limit — MaxRows; полем, чтобы проверка предела не заводила
	// пятьдесят тысяч карточек.
	limit int
}

func New(db *store.Store) *Service { return &Service{db: db, limit: MaxRows} }

// Count — сколько карточек попадёт в выгрузку. Экран спрашивает его,
// пока человек меняет параметры: «под них попадает 812 карточек» —
// ответ раньше, чем файл.
func (s *Service) Count(ctx context.Context, orgID, userID string, f Filter) (int, error) {
	if err := f.Check(); err != nil {
		return 0, err
	}
	var n int
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		where, args := f.sql()
		// #sql-склейка: условие собирает Filter.sql из постоянных кусков, значения идут параметрами
		return tx.QueryRow(ctx, `select count(*) `+where, args...).Scan(&n)
	})
	return n, err
}

// Export отбирает карточки, отдаёт их в sink по одной и затем сводку.
// Больше MaxRows — отказ до первой строки: половина отчёта хуже, чем
// никакого, её принимают за весь.
//
// Приёмник открывается только после подсчёта: открытый приёмник уже
// начал ответ, и отказать после него можно лишь оборванным файлом.
func (s *Service) Export(ctx context.Context, orgID, userID string, f Filter, open func() (Sink, error)) error {
	if err := f.Check(); err != nil {
		return err
	}
	return s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		where, args := f.sql()
		// Временная таблица живёт до конца транзакции: отбор делается
		// один раз, и «Данные» со «Сводкой» считаются из одних строк.
		// #sql-склейка: условие собирает Filter.sql из постоянных кусков, значения идут параметрами
		if _, err := tx.Exec(ctx, `
			create temp table report_cards on commit drop as
			select c.id, not `+store.NotContainer+` as container `+where, args...); err != nil {
			return err
		}
		var total int
		if err := tx.QueryRow(ctx, `select count(*) from report_cards`).Scan(&total); err != nil {
			return err
		}
		if total > s.limit {
			return &TooMany{Total: total, Limit: s.limit}
		}
		// Сводка считается раньше строк, хотя пишется после них: её
		// поломка тогда — отказ до начала ответа, а не оборванный файл.
		// Оборванный CSV от целого не отличить.
		sum, err := summarize(ctx, tx, f)
		if err != nil {
			return err
		}
		sum.Cards = total
		sink, err := open()
		if err != nil {
			return err
		}
		if err := streamRows(ctx, tx, sink); err != nil {
			return err
		}
		return sink.Summary(sum)
	})
}

// sql — условие отбора: from, join и where, начиная с «from».
// Собирается параметрами, пустой список параметра условием не ставится.
func (f *Filter) sql() (string, []any) {
	args := []any{f.From, f.To.AddDate(0, 0, 1), f.WithArchive}
	// $1 — начало периода, $2 — день после его конца: «до конца
	// периода включительно» — это «раньше следующего дня».
	q := `
		  from cards c
		  join boards b on b.id = c.board_id
		 where b.archived_at is null
		   and ($3 or c.archived_at is null)
		   and c.created_at < $2
		   and (c.finished_at is null or c.finished_at >= $1)`
	add := func(cond string, v any) {
		args = append(args, v)
		q += fmt.Sprintf("\n\t\t   and "+cond, len(args))
	}
	if len(f.Boards) > 0 {
		add("c.board_id = any($%d::uuid[])", f.Boards)
	}
	if len(f.Teams) > 0 {
		// Подразделение берёт и вложенные: у каждого узла в
		// ancestor_ids есть он сам и все старшие.
		add(`exists (select 1 from teams t
		              where t.id = b.team_id and t.ancestor_ids && $%d::uuid[])`, f.Teams)
	}
	if len(f.Assignees) > 0 {
		add(`exists (select 1 from card_assignees a
		              where a.card_id = c.id and a.user_id = any($%d::uuid[]))`, f.Assignees)
	}
	if len(f.Labels) > 0 {
		add(`exists (select 1 from card_labels l
		              where l.card_id = c.id and l.label_id = any($%d::uuid[]))`, f.Labels)
	}
	if len(f.Priorities) > 0 {
		add("c.priority = any($%d::text[])", f.Priorities)
	}
	if len(f.States) > 0 {
		add(`(case when c.outcome is not null then c.outcome
		           when c.started_at is not null then 'active'
		           else 'queued' end) = any($%d::text[])`, f.States)
	}
	if f.Iteration != "" {
		add(`exists (select 1 from iteration_cards i
		              where i.card_id = c.id and i.iteration_id = $%d::uuid
		                and i.removed_at is null)`, f.Iteration)
	}
	return q, args
}

func streamRows(ctx context.Context, tx pgx.Tx, sink Sink) error {
	// Порядок — как люди ищут в таблице: доска, затем номер числом,
	// а не строкой, где «ПОСТ-10» стоит раньше «ПОСТ-9».
	rows, err := tx.Query(ctx, `
		select c.number, c.title, b.name, coalesce(t.name, ''), col.name,
		       case when c.outcome is not null then c.outcome
		            when c.started_at is not null then 'active'
		            else 'queued' end,
		       c.priority, c.estimate::float8,
		       coalesce((select string_agg(coalesce(nullif(u.name, ''), u.email), ', '
		                                   order by coalesce(nullif(u.name, ''), u.email))
		                   from card_assignees a join users u on u.id = a.user_id
		                  where a.card_id = c.id), ''),
		       coalesce((select string_agg(l.name, ', ' order by lower(l.name))
		                   from card_labels cl join labels l on l.id = cl.label_id
		                  where cl.card_id = c.id), ''),
		       coalesce((select i.name from iteration_cards ic
		                   join iterations i on i.id = ic.iteration_id
		                  where ic.card_id = c.id and ic.removed_at is null
		                  order by i.starts_on desc limit 1), ''),
		       coalesce((select p.number from card_links k join cards p on p.id = k.from_card
		                  where k.to_card = c.id and k.kind = 'subtask' limit 1), ''),
		       c.created_at, c.started_at, c.finished_at,
		       to_char(c.due_on, 'YYYY-MM-DD'),
		       case when c.outcome = 'done' and c.started_at is not null
		            then extract(epoch from (c.finished_at - c.started_at)) / 86400.0 end,
		       case when c.started_at is not null and c.finished_at is null
		            then extract(epoch from (now() - c.started_at)) / 86400.0 end,
		       k.id is not null, coalesce(k.reason, ''),
		       c.external_source is not null, c.archived_at is not null, r.container
		  from report_cards r
		  join cards c on c.id = r.id
		  join boards b on b.id = c.board_id
		  join board_columns col on col.id = c.column_id
		  left join teams t on t.id = b.team_id
		  left join lateral (select id, reason from card_blocks
		                      where card_id = c.id and unblocked_at is null
		                      order by blocked_at desc limit 1) k on true
		 order by b.name, b.id, substring(c.number from '(\d+)$')::bigint, c.number`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Number, &r.Title, &r.Board, &r.Team, &r.Column, &r.State,
			&r.Priority, &r.Estimate, &r.Assignees, &r.Labels, &r.Iteration, &r.Parent,
			&r.CreatedAt, &r.StartedAt, &r.FinishedAt, &r.DueOn, &r.CycleDays, &r.AgeDays,
			&r.Blocked, &r.BlockReason, &r.Imported, &r.Archived, &r.Container); err != nil {
			return err
		}
		r.CreatedAt = r.CreatedAt.UTC()
		r.StartedAt, r.FinishedAt = utc(r.StartedAt), utc(r.FinishedAt)
		r.CycleDays, r.AgeDays = round2(r.CycleDays), round2(r.AgeDays)
		if err := sink.Row(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

func summarize(ctx context.Context, tx pgx.Tx, f Filter) (Summary, error) {
	sum := Summary{Throughput: []WeekCount{}, Flow: []FlowDay{},
		Iterations: []IterationRow{}, Teams: []TeamRow{}}
	from, next := f.From, f.To.AddDate(0, 0, 1)

	chosen, err := names(ctx, tx, f)
	if err != nil {
		return sum, err
	}
	sum.Chosen = chosen

	// Сделанное и отброшенное — законченное внутри периода: карточка,
	// жившая в периоде и закрытая после него, в нём не закрывалась.
	var c50, c85, a50, a85 *float64
	if err := tx.QueryRow(ctx, `
		select count(*) filter (where c.outcome = 'done' and c.finished_at < $1),
		       count(*) filter (where c.outcome = 'discarded' and c.finished_at < $1),
		       count(*) filter (where c.started_at is not null and c.finished_at is null),
		       percentile_cont(0.50) within group (order by d.cycle),
		       percentile_cont(0.85) within group (order by d.cycle),
		       percentile_cont(0.50) within group (order by d.age),
		       percentile_cont(0.85) within group (order by d.age)
		  from report_cards r
		  join cards c on c.id = r.id
		  cross join lateral (select
		       case when c.outcome = 'done' and c.started_at is not null and c.finished_at < $1
		            then extract(epoch from (c.finished_at - c.started_at)) / 86400.0 end as cycle,
		       case when c.started_at is not null and c.finished_at is null
		            then extract(epoch from (now() - c.started_at)) / 86400.0 end as age) d
		 where not r.container`,
		next).Scan(&sum.Finished, &sum.Discarded, &sum.WIP, &c50, &c85, &a50, &a85); err != nil {
		return sum, err
	}
	if c50 != nil {
		sum.CycleTime = &Percentiles{P50: *round2(c50), P85: *round2(c85)}
	}
	if a50 != nil {
		sum.Age = &Percentiles{P50: *round2(a50), P85: *round2(a85)}
	}

	// Недели без единой законченной тоже нужны: без них график врёт,
	// что команда что-то доводит до конца каждую неделю.
	rows, err := tx.Query(ctx, `
		with weeks as (
			select generate_series(date_trunc('week', $1::timestamptz),
			                       date_trunc('week', $2::timestamptz - interval '1 day'),
			                       interval '1 week') as week)
		select to_char(w.week, 'YYYY-MM-DD'), count(c.id)
		  from weeks w
		  left join (report_cards r join cards c on c.id = r.id and not r.container)
		    on c.outcome = 'done' and c.finished_at >= $1 and c.finished_at < $2
		   and date_trunc('week', c.finished_at) = w.week
		 group by w.week order by w.week`, from, next)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var w WeekCount
		if err := rows.Scan(&w.Week, &w.Count); err != nil {
			rows.Close()
			return sum, err
		}
		sum.Throughput = append(sum.Throughput, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// Накопительная — те же три полосы, что на «Потоке»: из отметок
	// карточки, на конец каждого шага. Точка стоит на последнем дне
	// шага, но не позже конца периода.
	sum.FlowStep = "day"
	if next.Sub(from) > flowDailyUpTo {
		sum.FlowStep = "week"
	}
	rows, err = tx.Query(ctx, `
		with steps as (
			select least(s + $3::interval, $2::timestamptz) as edge
			  from generate_series($1::timestamptz, $2::timestamptz - interval '1 day',
			                       $3::interval) as s)
		select to_char(d.edge - interval '1 day', 'YYYY-MM-DD'),
		       count(c.id) filter (
		           where c.created_at < d.edge
		             and (c.started_at is null or c.started_at >= d.edge)
		             and (c.finished_at is null or c.finished_at >= d.edge)),
		       count(c.id) filter (
		           where c.started_at < d.edge
		             and (c.finished_at is null or c.finished_at >= d.edge)),
		       count(c.id) filter (where c.finished_at < d.edge)
		  from steps d
		  left join (report_cards r join cards c on c.id = r.id and not r.container) on true
		 group by d.edge order by d.edge`, from, next, "1 "+sum.FlowStep)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var d FlowDay
		if err := rows.Scan(&d.Day, &d.Queued, &d.InProgress, &d.Done); err != nil {
			rows.Close()
			return sum, err
		}
		sum.Flow = append(sum.Flow, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// Итерации, пересекающие период, у которых есть отобранные карточки.
	rows, err = tx.Query(ctx, `
		select i.name, b.name, to_char(i.starts_on, 'YYYY-MM-DD'),
		       to_char(i.ends_on, 'YYYY-MM-DD'), i.closed_at is not null,
		       count(*), count(*) filter (where c.outcome = 'done')
		  from iterations i
		  join boards b on b.id = i.board_id
		  join iteration_cards ic on ic.iteration_id = i.id and ic.removed_at is null
		  join report_cards r on r.id = ic.card_id and not r.container
		  join cards c on c.id = r.id
		 where i.starts_on < $2 and i.ends_on >= $1
		 group by i.id, i.name, b.name, i.starts_on, i.ends_on, i.closed_at
		 order by i.starts_on, b.name, i.name`, from, next)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var it IterationRow
		if err := rows.Scan(&it.Name, &it.Board, &it.StartsOn, &it.EndsOn, &it.Closed,
			&it.Planned, &it.Done); err != nil {
			rows.Close()
			return sum, err
		}
		sum.Iterations = append(sum.Iterations, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// Разрез по подразделению, которое ведёт доску. Доска без
	// подразделения — отдельной строкой с пустым именем: подставлять
	// сюда слово значило бы решать за экран, на каком языке он говорит.
	rows, err = tx.Query(ctx, `
		select coalesce(t.name, ''), count(*),
		       count(*) filter (where c.outcome = 'done' and c.finished_at < $1),
		       count(*) filter (where c.started_at is not null and c.finished_at is null),
		       percentile_cont(0.50) within group (order by d.cycle),
		       percentile_cont(0.85) within group (order by d.cycle)
		  from report_cards r
		  join cards c on c.id = r.id
		  join boards b on b.id = c.board_id
		  left join teams t on t.id = b.team_id
		  cross join lateral (select
		       case when c.outcome = 'done' and c.started_at is not null and c.finished_at < $1
		            then extract(epoch from (c.finished_at - c.started_at)) / 86400.0 end as cycle) d
		 where not r.container
		 group by t.id, t.name
		 order by t.name nulls last`, next)
	if err != nil {
		return sum, err
	}
	defer rows.Close()
	for rows.Next() {
		var tr TeamRow
		if err := rows.Scan(&tr.Team, &tr.Cards, &tr.Finished, &tr.WIP,
			&tr.CycleP50, &tr.CycleP85); err != nil {
			return sum, err
		}
		tr.CycleP50, tr.CycleP85 = round2(tr.CycleP50), round2(tr.CycleP85)
		sum.Teams = append(sum.Teams, tr)
	}
	return sum, rows.Err()
}

// names — названия выбранного. Невидимое спрашивающему не называется:
// политики базы отдают только то, что он и так видит на экране.
func names(ctx context.Context, tx pgx.Tx, f Filter) (Chosen, error) {
	c := Chosen{Priorities: nonNil(f.Priorities), States: nonNil(f.States),
		Boards: []string{}, Teams: []string{}, Assignees: []string{}, Labels: []string{}}
	lists := []struct {
		ids []string
		q   string
		out *[]string
	}{
		{f.Boards, `select name from boards where id = any($1::uuid[]) order by lower(name)`, &c.Boards},
		{f.Teams, `select name from teams where id = any($1::uuid[]) order by lower(name)`, &c.Teams},
		{f.Assignees, `select coalesce(nullif(name, ''), email) from users
		                where id = any($1::uuid[]) order by lower(coalesce(nullif(name, ''), email))`, &c.Assignees},
		{f.Labels, `select name from labels where id = any($1::uuid[]) order by lower(name)`, &c.Labels},
	}
	for _, l := range lists {
		if len(l.ids) == 0 {
			continue
		}
		rows, err := tx.Query(ctx, l.q, l.ids)
		if err != nil {
			return c, err
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return c, err
		}
		*l.out = got
	}
	if f.Iteration != "" {
		err := tx.QueryRow(ctx, `select name from iterations where id = $1::uuid`, f.Iteration).Scan(&c.Iteration)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return c, err
		}
	}
	return c, nil
}

// round2 — дни до сотых: 18.250000000008683 — это шум деления
// секунд на сутки, а не точность.
func round2(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := math.Round(*p*100) / 100
	return &v
}

// utc — время без пояса сервера: файл читают в другом поясе, и
// «+03:00» у одних строк и «Z» у других сбивает сортировку.
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
