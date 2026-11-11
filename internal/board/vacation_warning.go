package board

import (
	"context"

	"github.com/findias/takt/internal/store"
	"github.com/jackc/pgx/v5"
)

// Порог относится к доске, а не к отпуску: разным командам нужно
// разное время на подготовку. Меняет тот, кто может писать на доску.
func (s *Service) SetVacationWarningDays(ctx context.Context, orgID, actorID, boardID string, days *int) error {
	if days != nil && (*days < 0 || *days > 365) {
		return badRequestf("дней до предупреждения об отпуске — от 0 до 365")
	}
	return s.explained(ctx, orgID, actorID, boardID, translateAccess(s.db.InScope(ctx,
		store.Scope{OrgID: orgID, UserID: actorID}, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `update boards
                set vacation_warning_days=$2, version=version+1
                where id=$1 and archived_at is null`, boardID, days)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
			return nil
		})))
}
