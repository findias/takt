// Package retention — уборка того, что растёт без предела.
//
// Служебные таблицы копятся и никем не читаются: ключи повтора живут
// сутки, доставленные вебхуки — месяц, просроченные сессии и отработавшие
// приглашения не нужны вовсе. Журнал действий не убирается никогда, если
// организация не попросила об обратном: журнал заводят затем, чтобы
// ответить на вопрос через год, и срок по умолчанию превратил бы его
// в журнал на месяц.
package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
)

const (
	// Смысл ключа повтора — пережить обрыв связи и повтор через минуту.
	// Сутки с запасом.
	idempotencyTTL = 24 * time.Hour
	// Доставленное держим месяц: по нему отвечают на вопрос «дошло ли»,
	// и через месяц его уже не задают.
	//
	// Наружу — потому что это граница автономии: система убирает данные
	// сама, и человек, который смотрит журнал доставок, обязан знать,
	// до какого дня в нём вообще есть что искать. Второе такое число
	// в интерфейсе разошлось бы с этим при первой же правке.
	DeliveredTTL = 30 * 24 * time.Hour
	// Отработавшее приглашение держим месяц после того, как оно перестало
	// действовать. Не сутки: «кого звали в прошлом месяце» — живой вопрос,
	// и отвечать на него из самой таблицы проще, чем из журнала. Тот же
	// срок повторён в политике cleanup на invites — уборщик видит ровно
	// то, что и так обязан стереть, — и расходиться этим двум местам
	// нельзя.
	inviteTTL = 30 * 24 * time.Hour
	// Раз в час: уборка не срочна, а лишний проход по таблицам стоит
	// дороже, чем лишний день хранения.
	interval = time.Hour
)

type Worker struct {
	db  *store.Store
	log *slog.Logger
}

func NewWorker(db *store.Store, log *slog.Logger) *Worker {
	return &Worker{db: db, log: log}
}

func (w *Worker) Run(ctx context.Context) {
	// Первый заход сразу: перезапуск сервера — обычный повод убраться,
	// и ждать час после него незачем.
	for {
		if err := w.Once(ctx); err != nil && ctx.Err() == nil {
			w.log.Error("уборка", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Once проходит по таблицам один раз.
func (w *Worker) Once(ctx context.Context) error {
	return w.db.InScope(ctx, store.Scope{}, func(tx pgx.Tx) error {
		// Тот же срок повторён в политике чтения cleanup_reads: уборщик
		// видит только просроченные ключи, потому что в них лежат тела
		// ответов, то есть данные арендатора. Расходиться этим двум местам
		// нельзя — уборка молча перестанет что-либо находить.
		keys, err := tx.Exec(ctx,
			`delete from api_idempotency
			  where created_at < now() - make_interval(hours => $1)`,
			int(idempotencyTTL.Hours()))
		if err != nil {
			return err
		}

		// Несдавшиеся доставки не трогаем: они ждут ручного повтора,
		// и вычистить их значит потерять единственный след того,
		// что не доехало.
		deliveries, err := tx.Exec(ctx, `
			delete from webhook_deliveries
			 where delivered_at is not null
			   and delivered_at < now() - make_interval(days => $1)`,
			int(DeliveredTTL.Hours()/24))
		if err != nil {
			return err
		}

		// Просроченная сессия не пускает и не читается: тридцать суток
		// её жизни — уже запас, а не спешка. Таблица под RLS не попадает,
		// поэтому уборщику здесь ничего не разрешали особо.
		sessions, err := tx.Exec(ctx, `delete from sessions where expires_at < now()`)
		if err != nil {
			return err
		}

		// Отработавшее приглашение хранит почту приглашённого и после
		// того, как перестало действовать. Это не журнал, а рабочая
		// таблица: факт «звали такого-то» лежит в журнале действий
		// и убирается сроком организации.
		invites, err := tx.Exec(ctx, `
			delete from invites
			 where coalesce(accepted_at, revoked_at, expires_at)
			       < now() - make_interval(days => $1)`,
			int(inviteTTL.Hours()/24))
		if err != nil {
			return err
		}

		// Журнал убирается только там, где организация назвала срок.
		audit, err := tx.Exec(ctx, `
			delete from audit_events a
			 using orgs o
			 where o.id = a.org_id
			   and o.audit_retention_days is not null
			   and a.at < now() - make_interval(days => o.audit_retention_days)`)
		if err != nil {
			return err
		}

		total := keys.RowsAffected() + deliveries.RowsAffected() + audit.RowsAffected() +
			sessions.RowsAffected() + invites.RowsAffected()
		if total > 0 {
			w.log.Info("убрано",
				"ключи повтора", keys.RowsAffected(),
				"доставки", deliveries.RowsAffected(),
				"журнал", audit.RowsAffected(),
				"сессии", sessions.RowsAffected(),
				"приглашения", invites.RowsAffected())
		}
		return nil
	})
}
