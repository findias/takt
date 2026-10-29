package report

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// «Требует внимания» (ROADMAP 42.3, срез 5): пять групп по тому, что
// застряло на сегодня. Карточка в работе без беды не попадает никуда;
// в очереди без исполнителя — норма, не «без исполнителя»; одна
// карточка с двумя бедами стоит в обеих группах.
func TestAttentionGroupsWhatIsStuck(t *testing.T) {
	f := newFixture(t)
	d := func(v float64) *float64 { return &v }
	blocked := f.card(spec{title: "Заблокирована", created: 10, started: 2, finished: -1})
	over := f.card(spec{title: "Старше обещания", created: 20, started: 15, finished: -1})
	overdue := f.card(spec{title: "Просрочена", created: 10, started: 2, finished: -1, due: d(4)})
	f.card(spec{title: "В очереди без исполнителя", created: 10, started: -1, finished: -1})
	stale := f.card(spec{title: "Без движения", created: 20, started: 9, finished: -1})
	fine := f.card(spec{title: "Идёт как надо", created: 3, started: 1, finished: -1})
	f.card(spec{title: "Сделана и просрочена", created: 10, started: 8, finished: 1, outcome: "done", due: d(3)})

	f.inTenant(f.owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `update boards set sle_days = 10 where id = $1`, f.board); err != nil {
			return err
		}
		// В колонку карточки вошли тогда же, когда начали; «идёт как
		// надо» — вчера.
		if _, err := tx.Exec(f.ctx, `update cards set column_entered_at = started_at
			 where started_at is not null and board_id = $1`, f.board); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `insert into card_blocks (org_id, card_id, reason, blocked_at)
			 values ($1, $2, 'ждём акт', now() - interval '3 days')`, f.orgID, blocked); err != nil {
			return err
		}
		// Исполнители есть у всех, кроме «старше обещания»: она одна
		// попадёт и в «без исполнителя».
		for _, id := range []string{blocked, overdue, stale, fine} {
			if _, err := tx.Exec(f.ctx, `insert into card_assignees (org_id, card_id, user_id) values ($1, $2, $3)`,
				f.orgID, id, f.member); err != nil {
				return err
			}
		}
		return nil
	})

	got, err := f.svc.Attention(f.ctx, f.orgID, f.owner, Filter{Boards: []string{f.board}})
	if err != nil {
		t.Fatal(err)
	}
	titles := func(list []AttentionCard) (out []string) {
		for _, c := range list {
			out = append(out, c.Title)
		}
		return out
	}
	want := map[string][]string{
		"blocked":     {"Заблокирована"},
		"overPromise": {"Старше обещания"},
		"overdue":     {"Просрочена"},
		"unassigned":  {"Старше обещания"},
		"stale":       {"Старше обещания", "Без движения"},
	}
	groups := map[string][]AttentionCard{"blocked": got.Blocked, "overPromise": got.OverPromise,
		"overdue": got.Overdue, "unassigned": got.Unassigned, "stale": got.Stale}
	for name, w := range want {
		if g := titles(groups[name]); !equalStrings(g, w) {
			t.Errorf("%s: %v, ожидалось %v", name, g, w)
		}
	}
	if got.Blocked[0].Reason != "ждём акт" || got.Blocked[0].Days < 2.9 || got.Blocked[0].Days > 3.1 {
		t.Errorf("блокировка: %+v — ожидались причина и три дня", got.Blocked[0])
	}
	if got.Overdue[0].Days != 4 || got.Overdue[0].DueOn == nil {
		t.Errorf("просрочка: %+v — ожидались четыре дня и срок", got.Overdue[0])
	}
	if got.Truncated {
		t.Error("отрезано, хотя карточек семь")
	}
	_ = over
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
