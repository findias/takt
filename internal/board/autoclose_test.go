package board

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Итерация закрывается сама после последнего дня (ROADMAP 34.10).
// Проверяется то, из-за чего устройство именно такое: момент закрытия —
// полночь после конца, а не момент прохода; итерация, чей день ещё идёт,
// и итерация, заведённая задним числом, остаются открытыми; проход
// работает поверх организаций, и политики строк его не съедают.
func TestIterationClosesItselfAfterItsLastDay(t *testing.T) {
	f := newFixture(t)
	due := f.iteration("Кончилась")
	retro := f.iteration("Записана задним числом")
	running := f.iteration("Идёт")
	id := f.createCard("Не успели", f.columnA)
	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": id, "iterationId": due.ID})

	// Сервер прошлых дат не боится, но created_at ставит сам — прошлое
	// сочиняем в обход, как backdate у блокировок.
	f.inTenant(func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `
			update iterations set starts_on = current_date - 10, ends_on = current_date - 3,
			       created_at = now() - interval '12 days'
			 where id = $1`, due.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `
			update iterations set starts_on = current_date - 10, ends_on = current_date - 3
			 where id = $1`, retro.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `
			update iteration_cards set added_at = now() - interval '5 days'
			 where iteration_id = $1`, due.ID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `update iterations set ends_on = current_date where id = $1`, running.ID)
		return err
	})

	if _, err := f.svc.CloseDueIterations(f.ctx); err != nil {
		t.Fatalf("проход по итерациям: %v", err)
	}

	state := map[string]*time.Time{}
	var midnight time.Time
	f.inTenant(func(tx pgx.Tx) error {
		rows, err := tx.Query(f.ctx, `select id::text, closed_at from iterations where board_id = $1`, f.boardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var at *time.Time
			if err := rows.Scan(&id, &at); err != nil {
				return err
			}
			state[id] = at
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRow(f.ctx, `select (current_date - 2)::timestamptz`).Scan(&midnight)
	})

	if at := state[due.ID]; at == nil || !at.Equal(midnight) {
		t.Errorf("кончившаяся итерация закрыта в %v, ждали полночь после конца %v", at, midnight)
	}
	if state[retro.ID] != nil {
		t.Error("итерация, заведённая после своего конца, закрылась сама — её отчёт был бы пустым")
	}
	if state[running.ID] != nil {
		t.Error("итерация закрылась в свой последний день")
	}

	// Незакрытая карточка осталась в отчёте несделанной.
	report, err := f.svc.IterationReport(f.ctx, f.orgID, f.actorID, f.boardID, due.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.Committed != 1 || report.Totals.Done != 0 {
		t.Errorf("отчёт закрытой сама: %+v, ждали одну несделанную", report.Totals)
	}

	// Второй проход ничего не находит: закрытое не закрывается дважды.
	if n, err := f.svc.closeDueIn(f.ctx, f.orgID); err != nil || n != 0 {
		t.Errorf("второй проход закрыл %d (%v), ждали 0", n, err)
	}
}
