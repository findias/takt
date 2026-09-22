package board

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/realtime"
	"github.com/findias/takt/internal/store"
)

// Уведомления по времени (ROADMAP 29.1): «срок блокировки твоей карточки —
// меньше суток» и «твоя карточка перешагнула обещание доски». Никто ничего
// не сделал — прошло время, — поэтому их замечает служебная задача,
// как снятие по сроку, и сообщает о каждом один раз.

const (
	// DueTask — имя задачи для политик (0059_due_notifications.sql).
	DueTask = "notify_due"

	ReasonBlockEnding = "block_ending"
	ReasonOverPromise = "over_promise"

	// blockEndingWithin — за сколько до срока предупреждать: сутки —
	// ровно столько, чтобы успеть сделать то, чего блокировка ждала.
	blockEndingWithin = 24 * time.Hour

	// DueEvery — как часто проходить. Не каждую минуту, как снятие
	// по сроку: проход обходит все организации, а для «меньше суток»
	// и «дольше обещанного» десять минут опоздания ничего не меняют.
	DueEvery = 10 * time.Minute
)

// NotifyDue проходит по организациям и заводит уведомления по времени.
// Возвращает, сколько записал. Сбой в одной организации не держит
// остальные: их сроки идут так же.
func (s *Service) NotifyDue(ctx context.Context) (int, error) {
	// Организации — таблица личности, вне политик: пройти по всем
	// дешевле, чем открывать задаче чужие блокировки без арендатора.
	rows, err := s.db.Pool.Query(ctx, `select id from orgs`)
	if err != nil {
		return 0, err
	}
	orgs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	total := 0
	var failed []error
	for _, orgID := range orgs {
		n, err := s.notifyDueIn(ctx, orgID)
		total += n
		if err != nil {
			failed = append(failed, err)
		}
	}
	return total, errors.Join(failed...)
}

// NotifyDueIn — тот же проход по одной организации: демонстрационные
// данные заводят его сразу, не дожидаясь фоновой задачи.
func (s *Service) NotifyDueIn(ctx context.Context, orgID string) (int, error) {
	return s.notifyDueIn(ctx, orgID)
}

func (s *Service) notifyDueIn(ctx context.Context, orgID string) (int, error) {
	written := 0
	err := s.db.InScope(ctx, store.Scope{OrgID: orgID, Task: DueTask}, func(tx pgx.Tx) error {
		type due struct {
			reason, source, boardID, cardID string
		}
		var found []due

		// Срок блокировки ближе суток. Источник — блокировка и её срок:
		// передвинули срок — это новое известие.
		rows, err := tx.Query(ctx, `
			select b.id, b.card_id, c.board_id, b.blocked_until
			  from card_blocks b
			  join cards c on c.id = b.card_id
			 where b.unblocked_at is null and c.archived_at is null
			   and b.blocked_until > now()
			   and b.blocked_until <= now() + make_interval(secs => $1)`,
			blockEndingWithin.Seconds())
		if err != nil {
			return err
		}
		for rows.Next() {
			var blockID, cardID, boardID string
			var until time.Time
			if err := rows.Scan(&blockID, &cardID, &boardID, &until); err != nil {
				rows.Close()
				return err
			}
			found = append(found, due{ReasonBlockEnding,
				"block:" + blockID + ":" + until.UTC().Format(time.RFC3339), boardID, cardID})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// Перешагнула обещание — тем же правилом, что метка на доске
		// (agingLabel в клиенте): начата, не закончена, и с начала прошло
		// больше обещанного. Источник — карточка и момент её начала:
		// вернули в очередь и начали снова — это новая история.
		rows, err = tx.Query(ctx, `
			select c.id, c.board_id, c.started_at
			  from cards c
			  join boards b on b.id = c.board_id
			 where b.sle_days is not null and b.archived_at is null
			   and c.archived_at is null and c.finished_at is null
			   and c.started_at is not null
			   and c.started_at < now() - make_interval(days => b.sle_days)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cardID, boardID string
			var started time.Time
			if err := rows.Scan(&cardID, &boardID, &started); err != nil {
				rows.Close()
				return err
			}
			found = append(found, due{ReasonOverPromise,
				"promise:" + cardID + ":" + started.UTC().Format(time.RFC3339), boardID, cardID})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		var notified []string
		for _, d := range found {
			working, err := assigneesOf(ctx, tx, d.cardID)
			if err != nil {
				return err
			}
			got, err := notifyOnce(ctx, tx, orgID, d.boardID, d.cardID, d.reason, d.source, working)
			if err != nil {
				return err
			}
			written += len(got)
			notified = append(notified, got...)
		}
		if len(notified) == 0 {
			return nil
		}
		return realtime.Notify(ctx, tx, realtime.Change{Notified: notified})
	})
	return written, err
}

// notifyOnce — как notify, но для повторяющегося прохода: сказанное
// однажды второй раз не говорится. Возвращает, кому записано сейчас.
// `on conflict` здесь можно: задача видит уведомления своих поводов
// (политика due_remembers), и проверка новой строки на чтение проходит.
func notifyOnce(ctx context.Context, tx pgx.Tx, orgID, boardID, cardID, reason, source string, recipients []string) ([]string, error) {
	if len(recipients) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		insert into notifications (org_id, recipient_id, board_id, card_id, actor_id, reason, source)
		select $1, u.id, $2, $3, null, $4, $5
		  from users u
		 where u.id = any($6::uuid[]) and not ($4 = any(u.muted_notifications))
		on conflict (recipient_id, reason, source) do nothing
		returning recipient_id`,
		orgID, boardID, cardID, reason, source, recipients)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
