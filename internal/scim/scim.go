// Package scim — автоматическое заведение людей и групп извне.
//
// Почему именно SCIM, а не «просто наш API»: корпоративные провайдеры
// (Entra ID, Okta, Keycloak) умеют выгружать сотрудников только этим
// протоколом. Свой, сколь угодно удобный, означал бы, что заводить
// и отключать людей всё равно будет человек руками — а вопрос, который
// задают при покупке, звучит «отключается ли доступ вместе с увольнением».
//
// Три решения, определяющие всё остальное.
//
// ОТКЛЮЧЕНИЕ СНИМАЕТ УЧАСТИЕ, А НЕ УДАЛЯЕТ ЧЕЛОВЕКА. Уволенный теряет
// доступ мгновенно, но его карточки, комментарии и подписи в журнале
// остаются подписанными им. Удалять запись значило бы переписать историю
// задним числом, а именно за неё платят.
//
// DELETE ДЕЛАЕТ ТО ЖЕ, ЧТО active = false. Провайдеры расходятся: одни
// шлют отключение, другие удаление, третьи — оба подряд. Считать их
// разными действиями значит зависеть от настроек чужой системы.
//
// ГРУППЫ — ЭТО КОМАНДЫ, А НЕ РОЛИ. Роль у нас свойство участия
// в организации, и провайдер о ней ничего не знает: в каталоге сотрудник
// состоит в отделе, а не «является владельцем». Отображение групп на роли
// выглядит удобным ровно до первого человека в двух группах.
package scim

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/store"
)

var (
	ErrNotFound = errors.New("не найдено")
	ErrConflict = errors.New("уже есть")
	// ErrNotEmpty — группу нельзя убрать, пока за ней числятся доски
	// или вложенные группы: они остались бы у команды, которой больше
	// нет. Правило общее с ручной архивацией и держит его база
	// (миграция 0045); каталог узнаёт о нём отказом, а не молчанием,
	// потому что решать, куда девать доски, всё равно человеку.
	ErrNotEmpty = errors.New("сначала перенесите доски и вложенные группы")
	// ErrLastOwner — отключаемый остался бы последним владельцем-человеком.
	//
	// Своим видом, а не безымянной ошибкой: без него отказ доезжал
	// до общего разбора и превращался в пятисотку. Провайдер прочтёт
	// это в отчёте синхронизации, и прочесть он должен объяснение,
	// а не «внутреннюю ошибку» — чинить-то ему, а не нам.
	ErrLastOwner = errors.New(
		"нельзя отключить последнего владельца организации: " +
			"назначьте владельцем кого-то ещё и повторите")
)

// DefaultRole — с чем приходит заведённый провайдером. Участник, а не
// владелец: право распоряжаться организацией не должно приезжать
// из каталога сотрудников.
const DefaultRole = auth.RoleMember

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

type User struct {
	ID         string
	ExternalID string
	Email      string
	Name       string
	Active     bool
	CreatedAt  time.Time
}

type Group struct {
	ID         string
	ExternalID string
	Name       string
	Members    []Member
	CreatedAt  time.Time
}

type Member struct {
	ID   string
	Name string
}

// ListUsers отдаёт состав организации. Отключённых в списке нет: для
// провайдера отключённый — тот, у кого снято участие, а участия у него
// больше нет.
func (s *Service) ListUsers(ctx context.Context, orgID, actorID, filterEmail string) ([]User, error) {
	out := []User{}
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select u.id, coalesce(m.external_id, ''), u.email, u.name, m.created_at
			  from memberships m
			  join users u on u.id = m.user_id
			 where m.org_id = $1
			   and ($2 = '' or lower(u.email) = lower($2))
			 order by m.created_at`, orgID, filterEmail)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u := User{Active: true}
			if err := rows.Scan(&u.ID, &u.ExternalID, &u.Email, &u.Name, &u.CreatedAt); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) GetUser(ctx context.Context, orgID, actorID, id string) (User, error) {
	u := User{Active: true}
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select u.id, coalesce(m.external_id, ''), u.email, u.name, m.created_at
			  from memberships m
			  join users u on u.id = m.user_id
			 where m.org_id = $1 and u.id = $2`, orgID, id).
			Scan(&u.ID, &u.ExternalID, &u.Email, &u.Name, &u.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// CreateUser заводит сотрудника или возвращает уже заведённого.
//
// Человек глобален, участие — нет: сотрудник, уже работающий в другой
// организации этой же установки, не заводится заново, ему добавляется
// участие. Иначе один и тот же человек существовал бы дважды и не мог
// бы переключаться между организациями.
func (s *Service) CreateUser(ctx context.Context, orgID, actorID string, in User) (User, error) {
	if in.Email == "" {
		return User{}, fmt.Errorf("не назван userName")
	}
	if in.Name == "" {
		in.Name = in.Email
	}
	out := User{ExternalID: in.ExternalID, Email: in.Email, Name: in.Name, Active: true}

	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var userID string
		err := tx.QueryRow(ctx,
			`select id from users where lower(email) = lower($1)`, in.Email).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			// Пароль случайный и никому не сообщается: заведённый каталогом
			// ходит через корпоративный вход, где ему и закроют доступ.
			hash, herr := auth.HashPassword(unguessable())
			if herr != nil {
				return herr
			}
			err = tx.QueryRow(ctx, `
				insert into users (email, name, password_hash)
				values ($1, $2, $3) returning id`, in.Email, in.Name, hash).Scan(&userID)
		}
		if err != nil {
			return err
		}
		out.ID = userID

		var exists bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from memberships where org_id = $1 and user_id = $2)`,
			orgID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrConflict
		}

		return tx.QueryRow(ctx, `
			insert into memberships (org_id, user_id, role, external_id)
			values ($1, $2, $3, nullif($4, ''))
			returning created_at`, orgID, userID, DefaultRole, in.ExternalID).Scan(&out.CreatedAt)
	})
	return out, err
}

// UpdateUser меняет то немногое, что провайдер вправе менять: имя, почту
// и участие. Роль он менять не может — её назначают здесь, и приезжающая
// из каталога роль снесла бы назначенное вручную при первой же выгрузке.
func (s *Service) UpdateUser(ctx context.Context, orgID, actorID, id string, in User) (User, error) {
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var member bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from memberships where org_id = $1 and user_id = $2)`,
			orgID, id).Scan(&member); err != nil {
			return err
		}
		if !member {
			return ErrNotFound
		}
		if !in.Active {
			return deactivate(ctx, tx, orgID, id)
		}
		if in.Email != "" || in.Name != "" {
			if _, err := tx.Exec(ctx, `
				update users
				   set email = coalesce(nullif($2, ''), email),
				       name  = coalesce(nullif($3, ''), name)
				 where id = $1`, id, in.Email, in.Name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	if !in.Active {
		return User{ID: id, Email: in.Email, Name: in.Name, Active: false}, nil
	}
	return s.GetUser(ctx, orgID, actorID, id)
}

// DeactivateUser снимает участие. Человек остаётся: его подписи в журнале
// и его комментарии никуда не деваются.
func (s *Service) DeactivateUser(ctx context.Context, orgID, actorID, id string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return deactivate(ctx, tx, orgID, id)
	})
}

func deactivate(ctx context.Context, tx pgx.Tx, orgID, userID string) error {
	// Последнего владельца снимать нельзя: организация без владельца
	// не управляется никем, и вернуть его будет некому.
	// Считает та же функция, что и при снятии участника руками: «кто
	// считается владельцем» определено один раз, иначе две копии одного
	// счёта разойдутся — они уже разошлись бы, если бы считали правильно
	// только здесь (миграция 0047).
	var owners int
	if err := tx.QueryRow(ctx,
		`select app_other_person_owners($1, $2)`, orgID, userID).Scan(&owners); err != nil {
		return err
	}
	var role string
	err := tx.QueryRow(ctx,
		`select role from memberships where org_id = $1 and user_id = $2`, orgID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == auth.RoleOwner && owners == 0 {
		return ErrLastOwner
	}

	// Состав команд чистится вместе с участием: команда, в которой числится
	// уволенный, вводит в заблуждение ровно тех, кто по ней распределяет
	// работу.
	if _, err := tx.Exec(ctx,
		`delete from team_members where org_id = $1 and user_id = $2`, orgID, userID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`delete from memberships where org_id = $1 and user_id = $2`, orgID, userID)
	return err
}

// --- группы ---

func (s *Service) ListGroups(ctx context.Context, orgID, actorID, filterName string) ([]Group, error) {
	out := []Group{}
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, coalesce(external_id, ''), name, created_at
			  from teams
			 where archived_at is null
			   and ($1 = '' or lower(name) = lower($1))
			 order by created_at`, filterName)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g Group
			if err := rows.Scan(&g.ID, &g.ExternalID, &g.Name, &g.CreatedAt); err != nil {
				return err
			}
			out = append(out, g)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out {
			members, err := membersOf(ctx, tx, out[i].ID)
			if err != nil {
				return err
			}
			out[i].Members = members
		}
		return nil
	})
	return out, err
}

func (s *Service) GetGroup(ctx context.Context, orgID, actorID, id string) (Group, error) {
	var g Group
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			select id, coalesce(external_id, ''), name, created_at
			  from teams where id = $1 and archived_at is null`, id).
			Scan(&g.ID, &g.ExternalID, &g.Name, &g.CreatedAt)
		if err != nil {
			return err
		}
		g.Members, err = membersOf(ctx, tx, g.ID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	return g, err
}

func (s *Service) CreateGroup(ctx context.Context, orgID, actorID string, in Group) (Group, error) {
	if in.Name == "" {
		return Group{}, fmt.Errorf("не назван displayName")
	}
	out := Group{ExternalID: in.ExternalID, Name: in.Name, Members: []Member{}}
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			insert into teams (org_id, name, external_id)
			values ($1, $2, nullif($3, ''))
			returning id, created_at`, orgID, in.Name, in.ExternalID).
			Scan(&out.ID, &out.CreatedAt)
		if err != nil {
			return err
		}
		for _, m := range in.Members {
			if err := addMember(ctx, tx, orgID, out.ID, m.ID); err != nil {
				return err
			}
		}
		out.Members, err = membersOf(ctx, tx, out.ID)
		return err
	})
	if err != nil && strings.Contains(err.Error(), "teams_external_id") {
		return Group{}, ErrConflict
	}
	return out, err
}

func (s *Service) RenameGroup(ctx context.Context, orgID, actorID, id, name string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update teams set name = $2 where id = $1 and archived_at is null`, id, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// SetMembers заменяет состав целиком. Провайдеры шлют и полную замену,
// и точечные добавления; поддержаны оба, потому что выбирает не наша
// сторона.
func (s *Service) SetMembers(ctx context.Context, orgID, actorID, id string, members []Member) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`delete from team_members where org_id = $1 and team_id = $2`, orgID, id); err != nil {
			return err
		}
		for _, m := range members {
			if err := addMember(ctx, tx, orgID, id, m.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) AddMembers(ctx context.Context, orgID, actorID, id string, members []Member) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		for _, m := range members {
			if err := addMember(ctx, tx, orgID, id, m.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) RemoveMembers(ctx context.Context, orgID, actorID, id string, members []Member) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		for _, m := range members {
			if _, err := tx.Exec(ctx,
				`delete from team_members where org_id = $1 and team_id = $2 and user_id = $3`,
				orgID, id, m.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteGroup убирает команду в архив, а не стирает: доски, отданные
// команде, потеряли бы владельца, а история — объяснение, кому они
// принадлежали.
func (s *Service) DeleteGroup(ctx context.Context, orgID, actorID, id string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update teams set archived_at = now() where id = $1 and archived_at is null`, id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return ErrNotEmpty
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func addMember(ctx context.Context, tx pgx.Tx, orgID, teamID, userID string) error {
	if userID == "" {
		return nil
	}
	// Состоящий в команде обязан состоять в организации: команда — это
	// подмножество, а не отдельный список.
	var member bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from memberships where org_id = $1 and user_id = $2)`,
		orgID, userID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return fmt.Errorf("%w: человек %s не состоит в организации", ErrNotFound, userID)
	}
	_, err := tx.Exec(ctx, `
		insert into team_members (org_id, team_id, user_id)
		values ($1, $2, $3) on conflict (team_id, user_id) do nothing`, orgID, teamID, userID)
	return err
}

func membersOf(ctx context.Context, tx pgx.Tx, teamID string) ([]Member, error) {
	rows, err := tx.Query(ctx, `
		select u.id, u.name from team_members tm
		  join users u on u.id = tm.user_id
		 where tm.team_id = $1
		 order by u.name`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// unguessable — пароль, которым нельзя войти. Колонка обязательная,
// а вход заведённому каталогом положен через провайдера.
func unguessable() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("\x00", 64)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
