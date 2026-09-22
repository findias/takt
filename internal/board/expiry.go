package board

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/realtime"
	"github.com/findias/takt/internal/store"
)

// Снятие блокировок по сроку (ROADMAP 28.1).
//
// Снимает проход в самом сервере, а не «когда доску кто-нибудь откроет»:
// доска, на которую неделю не смотрят, копила бы открытый интервал,
// и метрика по ней была бы тем неправдивее, чем реже туда заходят.
// А уникальный индекс «одна открытая блокировка» не дал бы поставить
// новую из-за блокировки, срок которой вышел позавчера.
//
// Человека у прохода нет, и политики базы, требующие человека, отдали
// бы ему ноль строк. Поэтому у него своё имя задачи в области
// транзакции, и политики 0053 открывают ему ровно то, что он делает:
// без арендатора — только список организаций, где пора; в организации —
// закрыть истёкшее, записать событие, поднять версию доски.

// ExpireTask — имя задачи в области транзакции; политики узнают её
// по нему.
const ExpireTask = "expire_blocks"

// ExpireEvery — как часто проходить. Блокировки живут часами и днями,
// обещать секунду незачем и нечем.
const ExpireEvery = time.Minute

// ExpireBlocks закрывает все блокировки с вышедшим сроком и отвечает,
// сколько закрыл.
//
// Гонка нескольких экземпляров сервера безвредна, лок не нужен: строку
// забирает один `update … returning`, событие пишет тот, кому строка
// досталась, остальным не вернулось ничего — и писать им нечего.
func (s *Service) ExpireBlocks(ctx context.Context) (int, error) {
	var orgs []string
	err := s.db.InScope(ctx, store.Scope{Task: ExpireTask}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select distinct org_id from card_blocks
			 where unblocked_at is null and blocked_until <= now()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			orgs = append(orgs, id)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}

	// Организация, на которой проход споткнулся, не держит остальные:
	// их сроки вышли так же.
	closed := 0
	var failed []error
	for _, orgID := range orgs {
		n, err := s.expireIn(ctx, orgID)
		closed += n
		if err != nil {
			failed = append(failed, err)
		}
	}
	return closed, errors.Join(failed...)
}

func (s *Service) expireIn(ctx context.Context, orgID string) (int, error) {
	closed := 0
	err := s.db.InScope(ctx, store.Scope{OrgID: orgID, Task: ExpireTask}, func(tx pgx.Tx) error {
		// Закрывается моментом срока, а не моментом прохода: иначе
		// опоздание прохода попадало бы во время блока, и метрика
		// зависела бы от того, когда крутился фон.
		rows, err := tx.Query(ctx, `
			update card_blocks b set unblocked_at = b.blocked_until
			  from cards c
			 where c.id = b.card_id
			   and b.unblocked_at is null and b.blocked_until <= now()
			returning b.card_id, c.board_id, b.reason, b.blocked_until`)
		if err != nil {
			return err
		}
		type expired struct {
			cardID, boardID, reason string
			until                   time.Time
		}
		var done []expired
		for rows.Next() {
			var e expired
			if err := rows.Scan(&e.cardID, &e.boardID, &e.reason, &e.until); err != nil {
				rows.Close()
				return err
			}
			done = append(done, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		boards := map[string]bool{}
		for _, e := range done {
			eventID, err := logEventID(ctx, tx, orgID, e.boardID, e.cardID, "", "block_expired", nil, nil,
				map[string]any{"reason": e.reason, "until": e.until.UTC()})
			if err != nil {
				return err
			}
			// Снялась сама — исполнитель узнаёт, что можно продолжать,
			// не дожидаясь, пока откроет доску.
			working, err := assigneesOf(ctx, tx, e.cardID)
			if err != nil {
				return err
			}
			if err := notify(ctx, tx, orgID, e.boardID, e.cardID, "", ReasonBlockExpired,
				eventSource(eventID), working); err != nil {
				return err
			}
			boards[e.boardID] = true
		}

		// Открытая доска обязана узнать сама: без оповещения карточка
		// оставалась бы заблокированной до перезагрузки — худший исход
		// для функции, которая обещает «само снимется». Версия растёт,
		// как от любой операции; патча к ней нет, и клиент, увидев
		// пропуск, перечитает снимок.
		for boardID := range boards {
			var version int64
			if err := tx.QueryRow(ctx,
				`update boards set version = version + 1 where id = $1 returning version`,
				boardID).Scan(&version); err != nil {
				return err
			}
			if err := realtime.Notify(ctx, tx, realtime.Change{BoardID: boardID, Version: version}); err != nil {
				return err
			}
		}
		closed = len(done)
		return nil
	})
	return closed, err
}
