package report

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// move — запись журнала: карточка встала в колонку вида kind за ago
// дней до «сейчас». Пустой kind — карточка ушла с доски (moved_out,
// archived). Колонка в событии одна на все — вид берётся из снимка,
// как его пишет перемещение.
type move struct {
	kind, typ string
	ago       float64
}

func (f *fixture) history(card string, moves ...move) {
	f.t.Helper()
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		for _, m := range moves {
			payload := `{}`
			if m.kind != "" {
				payload = `{"to": {"kind": "` + m.kind + `"}}`
			}
			if _, err := tx.Exec(f.ctx, `
				insert into card_events (org_id, board_id, card_id, type, to_column, payload, at)
				values ($1, $2, $3, $4, $5, $6::jsonb, now() - $7::numeric * interval '1 day')`,
				f.orgID, f.board, card, m.typ, f.column[f.board], payload, m.ago); err != nil {
				return err
			}
		}
		return nil
	})
}

func (f *fixture) assign(card, user string) {
	f.t.Helper()
	f.inTenant(f.owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `insert into card_assignees (org_id, card_id, user_id) values ($1, $2, $3)`,
			f.orgID, card, user)
		return err
	})
}

// Время в работе — отрезки в колонках работы, без возврата в очередь;
// где истории не хватает, оно пусто, а не ноль.
func TestWorkDaysCountOnlyTimeInWorkColumns(t *testing.T) {
	f := newFixture(t)

	// Цикл восемь дней, из них два дня карточка ждала в очереди после
	// первого захода и последний день стоит сделанной: работы — 2 + 3.
	returned := f.card(spec{title: "Возвращали", created: 10, started: 9, finished: 1, outcome: "done"})
	f.history(returned,
		move{"queue", "created", 10},
		move{"in_progress", "moved", 9},
		move{"queue", "moved", 7},
		move{"in_progress", "moved", 4},
		move{"done", "moved", 1})
	f.assign(returned, f.member)

	// Закрыта, не выходя из колонки работы: отрезок кончается
	// завершением, а не «сейчас».
	closedInPlace := f.card(spec{title: "Закрыта на месте", created: 8, started: 6, finished: 3, outcome: "done"})
	f.history(closedInPlace,
		move{"queue", "created", 8},
		move{"in_progress", "moved", 6})

	// Перенесена из другой системы: начата там раньше, чем заведена
	// у нас, — журнала той работы нет.
	imported := f.card(spec{title: "Перенесена", created: 30, started: 20, finished: -1})
	f.history(imported, move{"in_progress", "created", 2})

	// Первое видимое событие — приход с другой доски.
	arrived := f.card(spec{title: "Пришла", created: 5, started: 4, finished: -1})
	f.history(arrived, move{"in_progress", "moved_in", 4})

	// Не начата — в работе не была.
	queued := f.card(spec{title: "Ждёт", created: 3, started: -1, finished: -1})
	f.history(queued, move{"queue", "created", 3})

	var out collect
	flt := period(30)
	if err := f.svc.Export(f.ctx, f.orgID, f.owner, flt, func() (Sink, error) { return &out, nil }); err != nil {
		t.Fatal(err)
	}
	got := map[string]*float64{}
	for _, r := range out.rows {
		got[r.Title] = r.WorkDays
	}
	near := func(title string, want float64) {
		t.Helper()
		if v := got[title]; v == nil || *v < want-0.01 || *v > want+0.01 {
			t.Errorf("«%s»: время в работе %v, ожидалось %v", title, v, want)
		}
	}
	near("Возвращали", 5)
	near("Закрыта на месте", 3)
	for _, title := range []string{"Перенесена", "Пришла", "Ждёт"} {
		if v := got[title]; v != nil {
			t.Errorf("«%s»: время в работе %v, ожидалось пусто", title, *v)
		}
	}

	// В разрезе по людям — медиана по сделанным, как у цикла.
	var boris *PersonRow
	for i, p := range out.sum.People {
		if p.UserID != nil && *p.UserID == f.member {
			boris = &out.sum.People[i]
		}
	}
	if boris == nil || boris.WorkP50 == nil || *boris.WorkP50 < 4.99 || *boris.WorkP50 > 5.01 {
		t.Errorf("разрез по Борису: %+v", boris)
	}
	if boris != nil && (boris.CycleP50 == nil || *boris.CycleP50 < 7.99 || *boris.CycleP50 > 8.01) {
		t.Errorf("цикл Бориса %v, ожидалось 8", boris.CycleP50)
	}
}
