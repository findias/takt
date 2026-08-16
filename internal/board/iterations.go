package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Итерации доски.
//
// Вхождение карточки в итерацию — интервал, а не ссылка. Поле на карточке
// отвечало бы на вопрос «где она сейчас», а спрашивают всегда другое:
// что было в итерации на момент закрытия, что добавили после старта, что
// выкинули по дороге. Перенос карточки в следующий спринт затирал бы
// прошлое, и восстановить его было бы неоткуда.

var (
	// ErrIterationClosed — закрытая итерация не принимает и не отпускает
	// карточки: её состав застыл, и по нему считается сделанное.
	ErrIterationClosed = errors.New("итерация закрыта")
	// ErrCardInAnotherIteration — карточка не может идти в двух итерациях
	// сразу: сделанная работа посчиталась бы дважды.
	ErrCardInAnotherIteration = errors.New("карточка уже в другой итерации")
)

type Iteration struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Goal      string     `json:"goal"`
	StartsOn  string     `json:"startsOn"`
	EndsOn    string     `json:"endsOn"`
	ClosedAt  *time.Time `json:"closedAt"`
	CardCount int        `json:"cardCount"`
}

const iterationFields = `i.id, i.name, i.goal,
	to_char(i.starts_on, 'YYYY-MM-DD'), to_char(i.ends_on, 'YYYY-MM-DD'), i.closed_at`

// Iterations перечисляет итерации доски, свежие первыми, вместе с числом
// карточек, которые в них сейчас.
func (s *Service) Iterations(ctx context.Context, orgID, userID, boardID string) ([]Iteration, error) {
	out := []Iteration{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select `+iterationFields+`,
			       (select count(*) from iteration_cards ic
			         where ic.iteration_id = i.id and ic.removed_at is null)
			  from iterations i
			 where i.board_id = $1
			 order by i.starts_on desc, i.created_at desc`, boardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it Iteration
			if err := rows.Scan(&it.ID, &it.Name, &it.Goal, &it.StartsOn,
				&it.EndsOn, &it.ClosedAt, &it.CardCount); err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) CreateIteration(ctx context.Context, orgID, actorID, boardID, name, goal, startsOn, endsOn string) (Iteration, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Iteration{}, badRequestf("у итерации должно быть название")
	}
	if startsOn == "" || endsOn == "" {
		return Iteration{}, badRequestf("у итерации должны быть даты начала и конца")
	}

	var it Iteration
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Доска должна быть видна. Без этой проверки вставка отлетает
		// нарушением политики, то есть внутренней ошибкой вместо честного
		// «не найдена»: снаружи это выглядит как сломанный сервер там,
		// где на самом деле чужая доска.
		var exists bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from boards
			                where id = $1 and archived_at is null)`, boardID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return tx.QueryRow(ctx, `
			insert into iterations (org_id, board_id, name, goal, starts_on, ends_on)
			values ($1, $2, $3, $4, $5::date, $6::date)
			returning `+strings.ReplaceAll(iterationFields, "i.", ""),
			orgID, boardID, name, strings.TrimSpace(goal), startsOn, endsOn).
			Scan(&it.ID, &it.Name, &it.Goal, &it.StartsOn, &it.EndsOn, &it.ClosedAt)
	})
	return it, translateIteration(err)
}

// CloseIteration замораживает состав. Обратного действия нет намеренно:
// закрытие — это утверждение «вот что было сделано», и переоткрытие
// превращало бы его в черновик, а отчёты — в движущуюся мишень.
func (s *Service) CloseIteration(ctx context.Context, orgID, actorID, boardID, iterationID string) error {
	return translateIteration(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			update iterations set closed_at = now()
			 where id = $1 and board_id = $2 and closed_at is null`, iterationID, boardID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

// CardsAt отвечает на главный вопрос, ради которого вхождение сделано
// интервалом: какие карточки были в итерации в заданный момент. Для
// закрытой итерации это момент закрытия, для открытой — «сейчас».
func (s *Service) CardsAt(ctx context.Context, orgID, userID, iterationID string, at *time.Time) ([]string, error) {
	out := []string{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			with moment as (
				select coalesce($2::timestamptz, closed_at, now()) as t
				  from iterations where id = $1
			)
			select ic.card_id
			  from iteration_cards ic, moment
			 where ic.iteration_id = $1
			   and ic.added_at <= moment.t
			   and (ic.removed_at is null or ic.removed_at > moment.t)
			 order by ic.added_at`, iterationID, at)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

// IterationCard — карточка в отчёте по итерации вместе с тем, что с ней
// стало к моменту отсчёта.
type IterationCard struct {
	ID       string   `json:"id"`
	Number   string   `json:"number"`
	Title    string   `json:"title"`
	Estimate *float64 `json:"estimate"`
	// Доведена до конца к моменту отсчёта. Именно к моменту, а не вообще:
	// работа, законченная через неделю после закрытия спринта, в этом
	// спринте не сделана, и отчёт, который считает иначе, льстит.
	Done bool `json:"done"`
	// Вошла в итерацию позже её начала. Считается по первому входу:
	// карточка, вышедшая и вернувшаяся, добавлена не дважды.
	LateAdd bool `json:"lateAdd"`
	// Убрана из итерации до момента отсчёта — то есть в составе её нет,
	// но она в нём была, и об этом спрашивают на разборе.
	Dropped bool `json:"dropped"`
	// Убрана с доски. К составу отношения не имеет: карточку архивируют
	// и после закрытия итерации, а состав уже застыл.
	Archived bool `json:"archived"`
}

// IterationTotals — сводка отчёта в штуках и в весе.
type IterationTotals struct {
	Committed int `json:"committed"`
	Done      int `json:"done"`
	LateAdded int `json:"lateAdded"`
	Dropped   int `json:"dropped"`
	// ByWeight говорит, можно ли верить весу: если хоть одна карточка
	// состава не оценена, сумма врёт в меньшую сторону. Та же осторожность,
	// что у прогресса подзадач: ошибаться тут можно только против себя.
	ByWeight        bool    `json:"byWeight"`
	CommittedWeight float64 `json:"committedWeight"`
	DoneWeight      float64 `json:"doneWeight"`
}

// IterationReport — то, ради чего вхождение сделано интервалом.
type IterationReport struct {
	Iteration Iteration `json:"iteration"`
	// Момент, на который всё посчитано: закрытие для закрытой итерации,
	// «сейчас» для открытой. Отдаётся наружу, потому что без него числа
	// нельзя ни перепроверить, ни сравнить между собой.
	At     time.Time       `json:"at"`
	Cards  []IterationCard `json:"cards"`
	Totals IterationTotals `json:"totals"`
}

// Report собирает отчёт по итерации на момент её закрытия — или на сейчас,
// если она ещё идёт.
//
// Вопрос «что было в спринте на момент закрытия» и есть причина, по
// которой вхождение карточки моделируется интервалом, а не полем. До сих
// пор ответ на него был записан в базе и не читался ниоткуда.
func (s *Service) IterationReport(ctx context.Context, orgID, userID, boardID, iterationID string) (IterationReport, error) {
	var rep IterationReport
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			select `+iterationFields+`, coalesce(i.closed_at, now())
			  from iterations i
			 where i.id = $1 and i.board_id = $2`, iterationID, boardID).
			Scan(&rep.Iteration.ID, &rep.Iteration.Name, &rep.Iteration.Goal,
				&rep.Iteration.StartsOn, &rep.Iteration.EndsOn,
				&rep.Iteration.ClosedAt, &rep.At)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		// Интервалы схлопываются по карточке: она могла выйти и вернуться,
		// и тогда строк у неё две. В составе она, если хоть один интервал
		// открыт на момент отсчёта; добавленной поздно — по первому входу.
		rows, err := tx.Query(ctx, `
			with moment as (
				select coalesce(closed_at, now()) as t, starts_on
				  from iterations where id = $1
			),
			spans as (
				select ic.card_id,
				       min(ic.added_at) as first_added,
				       bool_or(ic.removed_at is null or ic.removed_at > m.t) as inside
				  from iteration_cards ic, moment m
				 where ic.iteration_id = $1 and ic.added_at <= m.t
				 group by ic.card_id
			)
			select c.id, c.number, c.title, c.estimate,
			       c.outcome = 'done' and c.finished_at is not null and c.finished_at <= m.t,
			       s.first_added::date > m.starts_on,
			       not s.inside,
			       c.archived_at is not null
			  from spans s
			  join cards c on c.id = s.card_id, moment m
			 order by s.first_added`, iterationID)
		if err != nil {
			return err
		}
		defer rows.Close()

		rep.Cards = []IterationCard{}
		rep.Totals.ByWeight = true
		for rows.Next() {
			var c IterationCard
			if err := rows.Scan(&c.ID, &c.Number, &c.Title, &c.Estimate,
				&c.Done, &c.LateAdd, &c.Dropped, &c.Archived); err != nil {
				return err
			}
			rep.Cards = append(rep.Cards, c)

			if c.Dropped {
				rep.Totals.Dropped++
				continue
			}
			rep.Totals.Committed++
			if c.LateAdd {
				rep.Totals.LateAdded++
			}
			if c.Estimate == nil {
				rep.Totals.ByWeight = false
			} else {
				rep.Totals.CommittedWeight += *c.Estimate
			}
			if c.Done {
				rep.Totals.Done++
				if c.Estimate != nil {
					rep.Totals.DoneWeight += *c.Estimate
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// Пустой состав весом не меряется: «0 из 0 очков» — это не ответ.
		if rep.Totals.Committed == 0 {
			rep.Totals.ByWeight = false
		}
		rep.Iteration.CardCount = rep.Totals.Committed
		return nil
	})
	return rep, err
}

// --- операции над карточкой ---

type iterationCardPayload struct {
	CardID      string `json:"cardId"`
	IterationID string `json:"iterationId"`
}

func addToIteration(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw []byte) (Patch, error) {
	p, err := parseIterationCard(raw)
	if err != nil {
		return Patch{}, err
	}
	if err := requireOpenIteration(ctx, tx, boardID, p.IterationID); err != nil {
		return Patch{}, err
	}

	tag, err := tx.Exec(ctx, `
		insert into iteration_cards (org_id, iteration_id, card_id, added_by)
		select $1, $2, $3, $4
		 where exists (select 1 from cards
		                where id = $3 and board_id = $5 and archived_at is null)
		on conflict (iteration_id, card_id) where removed_at is null do nothing`,
		orgID, p.IterationID, p.CardID, actorID, boardID)
	if err != nil {
		return Patch{}, translateIteration(err)
	}
	if tag.RowsAffected() == 0 {
		// Либо карточки нет, либо она уже в этой итерации. Второе —
		// не ошибка: повтор действия должен быть безобидным.
		//
		// Конфликт назван поимённо не для красоты: `on conflict do nothing`
		// без указания индекса проглотил бы и второе ограничение — то,
		// которое не пускает карточку в две итерации сразу, — и запрет
		// молча превратился бы в бездействие.
		return Patch{}, nil
	}

	if err := logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "iteration_added",
		nil, nil, map[string]any{"iterationId": p.IterationID}); err != nil {
		return Patch{}, err
	}
	return Patch{}, nil
}

func removeFromIteration(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID string, raw []byte) (Patch, error) {
	p, err := parseIterationCard(raw)
	if err != nil {
		return Patch{}, err
	}
	if err := requireOpenIteration(ctx, tx, boardID, p.IterationID); err != nil {
		return Patch{}, err
	}

	// Строка не удаляется: интервал закрывается. Удалив её, мы потеряли бы
	// ровно тот факт, ради которого всё это заведено, — что карточка
	// в итерации была и её оттуда убрали.
	tag, err := tx.Exec(ctx, `
		update iteration_cards set removed_at = now(), removed_by = $3
		 where iteration_id = $1 and card_id = $2 and removed_at is null`,
		p.IterationID, p.CardID, actorID)
	if err != nil {
		return Patch{}, err
	}
	if tag.RowsAffected() == 0 {
		return Patch{}, nil
	}

	if err := logEvent(ctx, tx, orgID, boardID, p.CardID, actorID, "iteration_removed",
		nil, nil, map[string]any{"iterationId": p.IterationID}); err != nil {
		return Patch{}, err
	}
	return Patch{}, nil
}

func parseIterationCard(raw []byte) (iterationCardPayload, error) {
	var p iterationCardPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, badRequestf("разбор операции итерации: %v", err)
	}
	if p.CardID == "" || p.IterationID == "" {
		return p, badRequestf("нужны карточка и итерация")
	}
	return p, nil
}

func requireOpenIteration(ctx context.Context, tx pgx.Tx, boardID, iterationID string) error {
	var closed *time.Time
	err := tx.QueryRow(ctx,
		`select closed_at from iterations where id = $1 and board_id = $2`,
		iterationID, boardID).Scan(&closed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if closed != nil {
		return ErrIterationClosed
	}
	return nil
}

func translateIteration(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "iteration_cards_one_open_per_card_idx":
			return ErrCardInAnotherIteration
		case pgErr.Code == "23514" && pgErr.ConstraintName == "iterations_dates_ordered":
			return badRequestf("итерация не может кончаться раньше, чем началась")
		case pgErr.Code == "22008" || pgErr.Code == "22007":
			return badRequestf("непонятная дата")
		case pgErr.Code == "42501":
			// Политика отказала: доска видна, но не для записи.
			return ErrReadOnlyBoard
		}
	}
	return err
}
