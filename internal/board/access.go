package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/konkov/agile/internal/store"
)

// Доступ к доске: чья она команда, кому видна, кто вписан поимённо.
//
// Правила держит база (миграции 0006 и 0009), здесь только операции. Одно
// правило приходится объяснять именно тут, потому что база отказывает
// в нём голым нарушением политики: доску нельзя перевести в состояние,
// в котором сам её не видишь. Postgres проверяет политику select и на
// новом ряде, и это не придирка — иначе доска, закрытая вокруг чужих
// людей, стала бы неисправимой: редактировать невидимую доску не может
// никто, включая владельца организации.

var (
	// ErrWouldLoseAccess — изменение отняло бы доступ у того, кто его делает.
	ErrWouldLoseAccess = errors.New(
		"после этого доска станет вам не видна: впишите себя в её состав или выберите команду, в которой состоите")
	// ErrTeamRequired — командная доска без команды не бывает.
	ErrTeamRequired = errors.New("для командной доски нужно выбрать команду")
)

const (
	VisibilityOrg     = "org"
	VisibilityTeam    = "team"
	VisibilityPrivate = "private"
)

type Person struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

type Access struct {
	Visibility string   `json:"visibility"`
	TeamID     *string  `json:"teamId"`
	TeamName   *string  `json:"teamName"`
	Members    []Person `json:"members"`
}

// Access читает, кому доска видна и кто вписан в неё поимённо.
func (s *Service) Access(ctx context.Context, orgID, userID, boardID string) (Access, error) {
	access := Access{Members: []Person{}}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			select b.visibility, b.team_id, t.name
			  from boards b left join teams t on t.id = b.team_id
			 where b.id = $1 and b.archived_at is null`, boardID).
			Scan(&access.Visibility, &access.TeamID, &access.TeamName)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		// Состав закрытой доски виден владельцу организации и самому
		// вписанному: список участников доски найма сам по себе сведения.
		// Остальным вернётся пустой список, и это не ошибка.
		rows, err := tx.Query(ctx, `
			select u.id, u.name, u.email
			  from board_members bm join users u on u.id = bm.user_id
			 where bm.board_id = $1
			 order by bm.added_at`, boardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Person
			if err := rows.Scan(&p.UserID, &p.Name, &p.Email); err != nil {
				return err
			}
			access.Members = append(access.Members, p)
		}
		return rows.Err()
	})
	return access, err
}

// SetAccess меняет видимость доски и её команду одним действием: порознь
// их менять нельзя — командная доска без команды не проходит проверку,
// а команда без командной видимости ни на что не влияет.
func (s *Service) SetAccess(ctx context.Context, orgID, actorID, boardID, visibility string, teamID *string) error {
	switch visibility {
	case VisibilityOrg, VisibilityTeam, VisibilityPrivate:
	default:
		return fmt.Errorf("%w: неизвестная видимость %q", ErrBadRequest, visibility)
	}
	if visibility == VisibilityTeam && teamID == nil {
		return ErrTeamRequired
	}
	if visibility != VisibilityTeam {
		// Команда остаётся отметкой о принадлежности и при других
		// видимостях; на доступ она влияет только при `team`.
		if visibility == VisibilityOrg {
			teamID = nil
		}
	}

	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			update boards set visibility = $2, team_id = $3
			 where id = $1 and archived_at is null`, boardID, visibility, teamID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	return translateAccess(err)
}

func (s *Service) AddMember(ctx context.Context, orgID, actorID, boardID, userID string) error {
	return s.memberOp(ctx, orgID, actorID, boardID, func(tx pgx.Tx) error {
		var member bool
		if err := tx.QueryRow(ctx, `
			select exists (select 1 from memberships
			                where org_id = $1 and user_id = $2)`,
			orgID, userID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return ErrNotFound
		}
		_, err := tx.Exec(ctx, `
			insert into board_members (org_id, board_id, user_id)
			values ($1, $2, $3) on conflict do nothing`, orgID, boardID, userID)
		return err
	})
}

func (s *Service) RemoveMember(ctx context.Context, orgID, actorID, boardID, userID string) error {
	return s.memberOp(ctx, orgID, actorID, boardID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`delete from board_members where board_id = $1 and user_id = $2`,
			boardID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Service) memberOp(ctx context.Context, orgID, actorID, boardID string, fn func(pgx.Tx) error) error {
	return translateAccess(s.db.InScope(ctx,
		store.Scope{OrgID: orgID, UserID: actorID}, func(tx pgx.Tx) error {
			// Доска должна существовать и быть видна: иначе состав чужой
			// доски правился бы по прямому идентификатору.
			var exists bool
			if err := tx.QueryRow(ctx, `
				select exists (select 1 from boards
				                where id = $1 and archived_at is null)`,
				boardID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
			return fn(tx)
		}))
}

func translateAccess(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "42501":
			// Политика отказала. Для смены видимости это почти всегда
			// одно и то же: новый ряд перестал быть виден автору.
			return ErrWouldLoseAccess
		case pgErr.Code == "23514" && pgErr.ConstraintName == "boards_team_required":
			return ErrTeamRequired
		case pgErr.Code == "23503":
			return ErrNotFound
		}
	}
	return err
}

// SetSLE записывает обещание доски: за сколько дней работа её проходит
// и с какой вероятностью.
//
// Отдельным вызовом, а не операцией над доской: обещание не меняет
// ни порядок карточек, ни их состав, и проводить его через тот же канал
// значило бы делать вид, что меняет. Пустое значение убирает обещание —
// доска, у которой ещё нет истории, обещать не может.
func (s *Service) SetSLE(ctx context.Context, orgID, actorID, boardID string, days *int, probability int) error {
	if days != nil && *days <= 0 {
		return badRequestf("срок обещания считается в днях и не бывает нулевым")
	}
	if probability < 50 || probability > 99 {
		return badRequestf("вероятность обещания — от 50 до 99 процентов")
	}
	return translateAccess(s.db.InScope(ctx,
		store.Scope{OrgID: orgID, UserID: actorID}, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				update boards set sle_days = $2, sle_probability = $3
				 where id = $1 and archived_at is null`, boardID, days, probability)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				// Либо доски нет, либо она не досталась на запись —
				// различает это общий разбор ниже.
				return ErrNotFound
			}
			return nil
		}))
}

// --- архив ---

// Archive убирает доску с глаз, не удаляя её. Журнал переходов и все
// карточки остаются: по ним считается поток, и вырезать их значило бы
// потерять историю, которую больше неоткуда взять.
func (s *Service) Archive(ctx context.Context, orgID, actorID, boardID string) error {
	return s.setArchived(ctx, orgID, actorID, boardID, true)
}

// Restore возвращает доску из архива.
func (s *Service) Restore(ctx context.Context, orgID, actorID, boardID string) error {
	return s.setArchived(ctx, orgID, actorID, boardID, false)
}

// ErrNameMismatch — подтверждение не совпало с названием доски.
//
// Вопрос «вы уверены?» отвечают не читая; вопрос «наберите название»
// невозможно ответить, не посмотрев, что именно удаляешь. Это
// единственный смысл требования, и потому оно стоит на сервере,
// а не только в диалоге.
var ErrNameMismatch = errors.New("название не совпало — доска не удалена")

// ErrBoardNotArchived — доску сперва убирают в архив.
var ErrBoardNotArchived = errors.New("удалить можно только доску из архива")

// Delete стирает доску насовсем — вместе с карточками, колонками,
// итерациями, обсуждениями и журналом её потока.
//
// Что остаётся: записи в журнале действий. У них нет внешних ключей
// на доску, а сама запись об удалении делается триггером в тот же миг
// и хранит удалённую строку целиком. После этого нельзя узнать, как шла
// работа на доске, но всегда можно узнать, кто её убрал и что это была
// за доска.
//
// Три условия, и каждое отвечает своему виду ошибки: право — владелец
// организации, состояние — доска в архиве, намерение — набранное
// название. Первые два держатся политикой в базе, третье проверяется
// здесь: базе не с чем сравнивать намерение.
func (s *Service) Delete(ctx context.Context, orgID, actorID, boardID, confirmName string) error {
	return translateAccess(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		if err := requireOwner(ctx, tx); err != nil {
			return err
		}

		var name string
		var archived *time.Time
		err := tx.QueryRow(ctx,
			`select name, archived_at from boards where id = $1`, boardID).Scan(&name, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if archived == nil {
			return ErrBoardNotArchived
		}
		if strings.TrimSpace(confirmName) != name {
			return ErrNameMismatch
		}

		// Журнал потока стирается отдельным запросом: внешнего ключа
		// на доску у него нет намеренно — событие хранит колонку такой,
		// какой она была, — и каскад его не заберёт.
		if _, err := tx.Exec(ctx,
			`delete from card_events where board_id = $1`, boardID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `delete from boards where id = $1`, boardID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

func (s *Service) setArchived(ctx context.Context, orgID, actorID, boardID string, archived bool) error {
	return translateAccess(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			update boards set archived_at = case when $2 then now() else null end
			 where id = $1 and (archived_at is null) = $2`, boardID, archived)
		if err != nil {
			return err
		}
		// Ноль строк — доска не найдена или уже в нужном состоянии.
		// Для вызывающего это одно и то же: делать нечего.
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

// Archived перечисляет убранные доски — иначе вернуть их будет неоткуда.
func (s *Service) Archived(ctx context.Context, orgID, userID string) ([]Info, error) {
	out := []Info{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, name, version from boards
			 where archived_at is not null
			 order by archived_at desc`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Info
			if err := rows.Scan(&b.ID, &b.Name, &b.Version); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}
