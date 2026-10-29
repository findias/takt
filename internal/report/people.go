package report

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Люди в отчёте (ROADMAP 37.1, 37.2).
//
// Отбор по исполнителю был, а увидеть, что делает конкретный человек,
// можно было только скачав файл и отфильтровав его в Excel: экран
// показывал одно число, а сводка резала поток по подразделениям, но не
// по людям. Здесь — разрез по исполнителям под тот же отбор и список
// людей, из которых выбирать: при выбранных досках или подразделениях —
// их люди, а не вся организация.

// PersonRow — строка разреза по исполнителям. Карточка с двумя
// исполнителями считается у обоих: вопрос «что делал Борис» не делится
// пополам оттого, что делал он это вместе с Верой. Строка без UserID —
// карточки, у которых исполнителя нет.
type PersonRow struct {
	UserID   *string  `json:"userId"`
	Name     string   `json:"name"`
	Cards    int      `json:"cards"`
	Finished int      `json:"finished"`
	WIP      int      `json:"wip"`
	CycleP50 *float64 `json:"cycleTimeP50"`
	CycleP85 *float64 `json:"cycleTimeP85"`
	// WorkP50 — медиана времени в работе (work.go) по тем же
	// сделанным карточкам, что и цикл: разница между ними — ожидание.
	WorkP50 *float64 `json:"workTimeP50"`
}

// Person — человек для выбора в отборе.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// People — что экрану отчёта нужно о людях: из кого выбирать
// и разрез под нынешний отбор.
type People struct {
	// Candidates — люди выбранных досок и подразделений; ничего
	// не выбрано — вся организация. Выбранные в отборе исполнители
	// входят всегда: иначе выбор пропал бы из списка, хотя остался
	// в адресе и в отборе.
	Candidates []Person `json:"candidates"`
}

// People отвечает экрану отчёта: кого предлагать в «Исполнителях».
// Что каждый делал — в сводке (Summary.People): с 42.3 экран берёт
// её целиком, и считать разрез ещё раз здесь значило бы считать
// его дважды на каждое движение отбора.
func (s *Service) People(ctx context.Context, orgID, userID string, f Filter) (People, error) {
	out := People{Candidates: []Person{}}
	if err := f.Check(); err != nil {
		return out, err
	}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		var err error
		out.Candidates, err = candidates(ctx, tx, orgID, f)
		return err
	})
	return out, err
}

// candidates — люди выбранных досок и подразделений. Подразделение —
// со всем, что внутри: его состав и составы вложенных. Доска — состав
// подразделения, которое её ведёт, — из вложенных только те, кто доску
// видит (иначе человек в списке, а назначить его нельзя), — поимённый
// состав доски и все, кто стоит исполнителем на её карточках: на общей
// доске работают и люди со стороны. Выбрано и то и другое — объединение:
// список нужен, чтобы найти человека, а не чтобы отобрать карточки.
func candidates(ctx context.Context, tx pgx.Tx, orgID string, f Filter) ([]Person, error) {
	q := `
		select u.id::text, coalesce(nullif(u.name, ''), u.email)
		  from users u
		  join memberships m on m.user_id = u.id and m.org_id = $1
		 where u.kind = 'person' and u.anonymized_at is null`
	args := []any{orgID}
	if len(f.Teams) > 0 || len(f.Boards) > 0 {
		// Параметры — только вместе с условием: неупомянутому в тексте
		// база не может назначить тип и отказывает всему запросу.
		args = append(args, nonNil(f.Teams), nonNil(f.Boards), nonNil(f.Assignees))
		q += `
		   and (u.id = any($4::uuid[])
		     or exists (select 1 from team_members tm join teams t on t.id = tm.team_id
		                 where tm.user_id = u.id and t.archived_at is null
		                   and t.ancestor_ids && $2::uuid[])
		     or exists (select 1 from boards b
		                  join teams t on t.ancestor_ids && array[b.team_id]
		                  join team_members tm on tm.team_id = t.id
		                 where b.id = any($3::uuid[]) and tm.user_id = u.id and t.archived_at is null
		                   and app_sees_board_as(u.id, b.id))
		     or exists (select 1 from board_members bm
		                 where bm.board_id = any($3::uuid[]) and bm.user_id = u.id)
		     or exists (select 1 from card_assignees a join cards c on c.id = a.card_id
		                 where c.board_id = any($3::uuid[]) and a.user_id = u.id))`
	}
	q += `
		 order by lower(coalesce(nullif(u.name, ''), u.email))`
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Person, error) {
		var p Person
		return p, r.Scan(&p.ID, &p.Name)
	})
}

// personRows — разрез по исполнителям над временной таблицей отбора
// (gather). Выбраны исполнители — строки только их: соисполнители
// выбранного в разрезе «что делал Борис» — чужие строки.
func personRows(ctx context.Context, tx pgx.Tx, f Filter) ([]PersonRow, error) {
	next := f.To.AddDate(0, 0, 1)
	args := []any{next, nonNil(f.Assignees)}
	q := `
		select a.user_id::text, coalesce(nullif(u.name, ''), u.email, ''), count(*),
		       count(*) filter (where c.outcome = 'done' and c.finished_at < $1),
		       count(*) filter (where c.started_at is not null and c.finished_at is null),
		       percentile_cont(0.50) within group (order by d.cycle),
		       percentile_cont(0.85) within group (order by d.cycle),
		       percentile_cont(0.50) within group (order by d.work)
		  from report_cards r
		  join cards c on c.id = r.id
		  left join card_assignees a on a.card_id = c.id
		  left join users u on u.id = a.user_id
		  left join lateral (` + workDaysLateral + `) w on true
		  cross join lateral (select
		       case when c.outcome = 'done' and c.started_at is not null and c.finished_at < $1
		            then extract(epoch from (c.finished_at - c.started_at)) / 86400.0 end as cycle,
		       case when c.outcome = 'done' and c.finished_at < $1
		            then w.days end as work) d
		 where cardinality($2::uuid[]) = 0 or a.user_id = any($2::uuid[])
		 group by a.user_id, u.name, u.email
		 order by a.user_id is null, lower(coalesce(nullif(u.name, ''), u.email))`
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PersonRow{}
	for rows.Next() {
		var p PersonRow
		if err := rows.Scan(&p.UserID, &p.Name, &p.Cards, &p.Finished, &p.WIP, &p.CycleP50, &p.CycleP85, &p.WorkP50); err != nil {
			return nil, err
		}
		p.CycleP50, p.CycleP85, p.WorkP50 = round2(p.CycleP50), round2(p.CycleP85), round2(p.WorkP50)
		out = append(out, p)
	}
	return out, rows.Err()
}
