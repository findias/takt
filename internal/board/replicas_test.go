package board

import (
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Фоновые задачи на нескольких репликах (PROMPT-TESTING.md, уровень 2).
//
// Каждая реплика `takt serve` гоняет снятие блокировок по сроку,
// уведомления по времени и закрытие итераций сама — CHANGELOG обещает,
// что это безопасно. Отдельные тесты задач гоняют их по одной и по
// разу; здесь четыре реплики разом, по два прохода каждая. Двойников
// не даёт база (уникальные индексы, условия на открытое), но двойник,
// отбитый индексом, у второй реплики — ошибка, и проход падает целиком:
// у заказчика это записи «ошибка» раз в минуту и недоснятое за ней.
func TestBackgroundTasksRunOnSeveralReplicasAtOnce(t *testing.T) {
	f := newFixture(t)

	expired := f.createCard("Срок вышел", f.columnA)
	f.blockUntil(expired, time.Now().Add(2*time.Hour))
	f.backdate(expired, time.Now().Add(-time.Hour))

	ending := f.createCard("Срок вот-вот", f.columnA)
	f.blockUntil(ending, time.Now().Add(2*time.Hour))

	due := f.iteration("Кончилась вчера")
	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": ending, "iterationId": due.ID})
	f.inTenant(func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `
			update iterations set starts_on = current_date - 10, ends_on = current_date - 1,
			       created_at = now() - interval '12 days'
			 where id = $1`, due.ID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `
			update iteration_cards set added_at = now() - interval '5 days'
			 where iteration_id = $1`, due.ID)
		return err
	})

	const replicas, passes = 4, 2
	var wg sync.WaitGroup
	errs := make(chan error, replicas*passes*3)
	start := make(chan struct{})
	for r := 0; r < replicas; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for p := 0; p < passes; p++ {
				if _, err := f.svc.ExpireBlocks(f.ctx); err != nil {
					errs <- err
				}
				if _, err := f.svc.NotifyDue(f.ctx); err != nil {
					errs <- err
				}
				if _, err := f.svc.CloseDueIterations(f.ctx); err != nil {
					errs <- err
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("проход реплики упал: %v", err)
	}

	if n, _ := f.eventsOf(expired, "block_expired"); n != 1 {
		t.Errorf("снятие по сроку записано %d раз, ждали один", n)
	}
	if at, _ := f.closedAt(expired); at.IsZero() {
		t.Error("блокировка с вышедшим сроком не снята")
	}
	if at, _ := f.closedAt(ending); !at.IsZero() {
		t.Error("снята блокировка, чей срок не вышел")
	}

	var closed int
	var dup int
	f.inTenant(func(tx pgx.Tx) error {
		if err := tx.QueryRow(f.ctx,
			`select count(*) from iterations where id = $1 and closed_at is not null`, due.ID).Scan(&closed); err != nil {
			return err
		}
		return tx.QueryRow(f.ctx, `
			select count(*) from (
			  select 1 from notifications where card_id = $1
			   group by recipient_id, reason, source having count(*) > 1) d`, ending).Scan(&dup)
	})
	if closed != 1 {
		t.Error("итерация, кончившаяся вчера, не закрыта")
	}
	if dup != 0 {
		t.Errorf("уведомлений-двойников: %d", dup)
	}
}
