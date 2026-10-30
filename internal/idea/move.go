package idea

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Перенос идеи между уровнями (ROADMAP 45). Уровень идеи — кому она
// адресована: всей организации или подразделению любой глубины.
// Подали не туда, или подразделение решило, что вопрос «выше нас», —
// идею переносят, а не заводят заново: голоса и история остаются.

var (
	ErrCannotMove = errors.New("перенести идею может её автор, пока по ней не решили, владелец организации или владелец подразделения, которому она адресована")
	// ErrMoveOutside — владелец подразделения выбрал чужое подразделение.
	ErrMoveOutside = errors.New("владелец подразделения переносит идею внутрь своих подразделений или на всю организацию — в чужое подразделение нельзя")
	// ErrSettled — идея решена насовсем; «позже» решением насовсем не считается.
	ErrSettled = errors.New("идея уже решена — взята, отклонена или названа двойником; переносят только открытые")
)

// canMove — вправе ли userID перенести идею: тот, кто её решает,
// и автор, пока решения нет. Решённую насовсем не переносит никто:
// у взятой есть карточка, у отклонённой — ответ, у двойника — та идея.
func canMove(i Idea, userID string) bool {
	if i.Decision != nil && i.Decision.Kind != Later {
		return false
	}
	author := i.AuthorID != nil && *i.AuthorID == userID
	return i.CanDecide || (author && i.Decision == nil)
}

// Move адресует идею teamID (пусто — всей организации). Куда можно:
// владельцу организации и автору — куда угодно; владельцу подразделения —
// внутрь своего поддерева или всей организации. Политика (0087)
// проверяет то же при записи — здесь отказ называется словами до неё.
func (s *Service) Move(ctx context.Context, orgID, userID, ideaID string, teamID *string) (Idea, error) {
	if teamID != nil && strings.TrimSpace(*teamID) == "" {
		teamID = nil
	}
	var out Idea
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		i, err := one(ctx, tx, userID, ideaID)
		if err != nil {
			return err
		}
		if i.Decision != nil && i.Decision.Kind != Later {
			return ErrSettled
		}
		if !i.CanMove {
			return ErrCannotMove
		}
		if teamID != nil {
			var known bool
			if err := tx.QueryRow(ctx,
				`select exists (select 1 from teams where id = $1 and archived_at is null)`,
				*teamID).Scan(&known); err != nil {
				return err
			}
			if !known {
				return ErrTeamUnknown
			}
		}
		if (teamID == nil && i.TeamID == nil) || (teamID != nil && i.TeamID != nil && *teamID == *i.TeamID) {
			out = i
			return nil
		}
		author := i.AuthorID != nil && *i.AuthorID == userID
		if !author {
			var allowed bool
			if err := tx.QueryRow(ctx,
				`select app_is_owner() or $1::uuid is null or $1::uuid = any (app_admin_teams())`,
				teamID).Scan(&allowed); err != nil {
				return err
			}
			if !allowed {
				return ErrMoveOutside
			}
		}
		tag, err := tx.Exec(ctx, `update ideas set team_id = $2 where id = $1`, ideaID, teamID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "42501" {
				return ErrMoveOutside
			}
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrCannotMove
		}
		out, err = one(ctx, tx, userID, ideaID)
		return err
	})
	return out, err
}
