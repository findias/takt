package board

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/realtime"
	"github.com/findias/takt/internal/store"
)

// Итерация закрывается сама, когда кончился её последний день
// (ROADMAP 34.10, миграция 0071).
//
// Руками закрывали когда придётся, и отчёт «что успели» зависел от
// того, кто и когда вспомнил. Проход устроен как снятие блокировок по
// сроку (expiry.go): своё имя задачи, сначала список организаций без
// арендатора, потом каждая — со своим арендатором.

// CloseTask — имя задачи в области транзакции; политики 0071 узнают её
// по нему.
const CloseTask = "close_iterations"

// CloseDueIterations закрывает итерации, чей последний день прошёл,
// и отвечает, сколько закрыл. Незакрытые карточки остаются в них
// несделанными: переносит их человек, из отчёта или из панели карточки.
//
// Заведённые уже после своего конца не закрываются: их пишут задним
// числом, и полночь, которая была до них, дала бы пустой отчёт.
func (s *Service) CloseDueIterations(ctx context.Context) (int, error) {
	var orgs []string
	err := s.db.InScope(ctx, store.Scope{Task: CloseTask}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select distinct org_id from iterations
			 where closed_at is null and ends_on < current_date
			   and created_at < (ends_on + 1)::timestamptz`)
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

	closed := 0
	var failed []error
	for _, orgID := range orgs {
		n, err := s.closeDueIn(ctx, orgID)
		closed += n
		if err != nil {
			failed = append(failed, err)
		}
	}
	return closed, errors.Join(failed...)
}

func (s *Service) closeDueIn(ctx context.Context, orgID string) (int, error) {
	closed := 0
	err := s.db.InScope(ctx, store.Scope{OrgID: orgID, Task: CloseTask}, func(tx pgx.Tx) error {
		// Моментом полуночи после последнего дня, а не моментом прохода:
		// иначе в состав попадало бы то, что сделали, пока сервер лежал.
		// Гонка экземпляров безвредна: строку забирает один `update`.
		rows, err := tx.Query(ctx, `
			update iterations set closed_at = (ends_on + 1)::timestamptz
			 where closed_at is null and ends_on < current_date
			   and created_at < (ends_on + 1)::timestamptz
			returning board_id`)
		if err != nil {
			return err
		}
		boards := map[string]bool{}
		for rows.Next() {
			var boardID string
			if err := rows.Scan(&boardID); err != nil {
				rows.Close()
				return err
			}
			boards[boardID] = true
			closed++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		// Открытая доска узнаёт сама: иначе полоса итераций показывала бы
		// «Закрыть итерацию» у уже закрытой до перезагрузки.
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
		return nil
	})
	return closed, err
}
