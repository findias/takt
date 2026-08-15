// Package team — подразделения организации, их состав и наблюдение.
//
// Дерево подразделений живёт в базе с девятой миграции: `parent_id` —
// источник истины, `ancestor_ids` — путь от корня, который поддерживает
// триггер. Здесь только обвязка: операции, которые это дерево меняют.
//
// Правила, которые этот пакет не проверяет, потому что их держит база
// и потому что дублировать их в коде — значит завести второй источник
// истины: глубина вложенности, циклы, наследование роли вниз, право
// раздавать доступ. Код лишь переводит отказ базы в понятную ошибку.
package team

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

var (
	ErrNotFound = errors.New("не найдено")
	// ErrNotEmpty — архивировать узел с живыми потомками или досками
	// нельзя: иначе доска остаётся у команды, которой больше нет,
	// и перестаёт быть видна кому бы то ни было.
	ErrNotEmpty = errors.New("сначала перенесите вложенные команды и доски")
	// ErrForbidden — отказ политики. Приходить сюда он не должен:
	// маршруты уже требуют владельца. Если пришёл — значит появился путь
	// в обход проверки, и ответить надо запретом, а не пятисоткой.
	ErrForbidden = errors.New("недостаточно прав")
)

// TreeError — отказ, пришедший из ограничений дерева: глубина или цикл.
// Сообщение приходит из базы: оно там написано для человека, и второй
// раз формулировать то же самое в коде незачем.
type TreeError struct{ Reason string }

func (e *TreeError) Error() string { return e.Reason }

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

type Team struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
	// Depth — длина пути от корня, от единицы. Клиенту она нужна не для
	// отрисовки (дерево он строит по parentId), а чтобы показать, что
	// глубже вкладывать уже нельзя.
	Depth   int `json:"depth"`
	Members int `json:"members"`
	// Boards считается по видимым доскам: у наблюдателя и у постороннего
	// одно и то же дерево покажет разные числа, и это правильно.
	Boards int `json:"boards"`
}

type Member struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	// Ведущий — тот, у кого есть запись администратора именно на этом
	// узле. Отдельной пометки в составе нет намеренно: два поля,
	// описывающие одно и то же, рано или поздно расходятся.
	Lead    bool      `json:"lead"`
	AddedAt time.Time `json:"addedAt"`
}

type Observer struct {
	ID       string  `json:"id"`
	UserID   string  `json:"userId"`
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	TeamID   *string `json:"teamId"`
	TeamName *string `json:"teamName"`
}

// List возвращает дерево плоским списком, родители раньше потомков.
func (s *Service) List(ctx context.Context, orgID, userID string) ([]Team, error) {
	out := []Team{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select t.id, t.name, t.parent_id, cardinality(t.ancestor_ids),
			       (select count(*) from team_members tm where tm.team_id = t.id),
			       (select count(*) from boards b
			         where b.team_id = t.id and b.archived_at is null)
			  from teams t
			 where t.archived_at is null
			 order by t.ancestor_ids, t.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Team
			if err := rows.Scan(&t.ID, &t.Name, &t.ParentID, &t.Depth,
				&t.Members, &t.Boards); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, orgID, actorID, name string, parentID *string) (Team, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Team{}, fmt.Errorf("у команды должно быть название")
	}

	var t Team
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			insert into teams (org_id, name, parent_id) values ($1, $2, $3)
			returning id, name, parent_id, cardinality(ancestor_ids)`,
			orgID, name, parentID).
			Scan(&t.ID, &t.Name, &t.ParentID, &t.Depth)
	})
	return t, translate(err)
}

func (s *Service) Rename(ctx context.Context, orgID, actorID, teamID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("у команды должно быть название")
	}
	return s.explain(ctx, orgID, actorID,
		s.exec(ctx, orgID, actorID,
			`update teams set name = $2 where id = $1 and archived_at is null`, teamID, name),
		`select exists (select 1 from teams where id = $1 and archived_at is null)`, teamID)
}

// Move переносит подразделение под другого родителя; nil делает его
// корневым. Перенос переписывает путь всему поддереву — это делает
// триггер, и он же отвергает цикл и выход за предел глубины.
func (s *Service) Move(ctx context.Context, orgID, actorID, teamID string, parentID *string) error {
	return s.explain(ctx, orgID, actorID,
		s.exec(ctx, orgID, actorID,
			`update teams set parent_id = $2 where id = $1 and archived_at is null`,
			teamID, parentID),
		`select exists (select 1 from teams where id = $1 and archived_at is null)`, teamID)
}

func (s *Service) Archive(ctx context.Context, orgID, actorID, teamID string) error {
	return translate(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var children, boards int
		err := tx.QueryRow(ctx, `
			select (select count(*) from teams
			         where parent_id = $1 and archived_at is null),
			       (select count(*) from boards
			         where team_id = $1 and archived_at is null)`, teamID).
			Scan(&children, &boards)
		if err != nil {
			return err
		}
		if children > 0 || boards > 0 {
			return ErrNotEmpty
		}

		tag, err := tx.Exec(ctx,
			`update teams set archived_at = now()
			  where id = $1 and archived_at is null`, teamID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

// --- состав ---

func (s *Service) Members(ctx context.Context, orgID, userID, teamID string) ([]Member, error) {
	out := []Member{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select u.id, u.name, u.email, tm.added_at,
			       exists (select 1 from team_admins a
			                where a.team_id = tm.team_id and a.user_id = tm.user_id)
			  from team_members tm join users u on u.id = tm.user_id
			 where tm.team_id = $1
			 order by tm.added_at`, teamID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.AddedAt, &m.Lead); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// AddMember вписывает человека в подразделение.
//
// Роли в составе нет: кто здесь распоряжается — вопрос с одним ответом,
// и отвечает на него запись администратора, а не пометка рядом с именем.
func (s *Service) AddMember(ctx context.Context, orgID, actorID, teamID, userID string) error {
	return translate(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Человек обязан состоять в организации: команда — это структура
		// внутри арендатора, а не способ пригласить постороннего.
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
			insert into team_members (org_id, team_id, user_id)
			values ($1, $2, $3)
			on conflict (team_id, user_id) do nothing`,
			orgID, teamID, userID)
		return err
	}))
}

func (s *Service) RemoveMember(ctx context.Context, orgID, actorID, teamID, userID string) error {
	// Проверяем не команду, а само членство: убрать того, кого нет, —
	// это «нечего убирать», а не «нельзя».
	return s.explain(ctx, orgID, actorID,
		s.exec(ctx, orgID, actorID,
			`delete from team_members where team_id = $1 and user_id = $2`, teamID, userID),
		`select exists (select 1 from team_members where team_id = $1 and user_id = $2)`,
		teamID, userID)
}

// --- наблюдение ---

func (s *Service) Observers(ctx context.Context, orgID, userID string) ([]Observer, error) {
	out := []Observer{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select o.id, u.id, u.name, u.email, o.team_id, t.name
			  from observers o
			  join users u on u.id = o.user_id
			  left join teams t on t.id = o.team_id
			 order by u.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o Observer
			if err := rows.Scan(&o.ID, &o.UserID, &o.Name, &o.Email,
				&o.TeamID, &o.TeamName); err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

// Grant делает человека наблюдателем: за одним подразделением вместе с его
// поддеревом, если команда указана, или за всей организацией, если нет.
func (s *Service) Grant(ctx context.Context, orgID, actorID, userID string, teamID *string) (Observer, error) {
	var o Observer
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			insert into observers (org_id, user_id, team_id, granted_by)
			values ($1, $2, $3, $4)
			returning id`, orgID, userID, teamID, actorID).Scan(&o.ID)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			select u.id, u.name, u.email, o.team_id, t.name
			  from observers o
			  join users u on u.id = o.user_id
			  left join teams t on t.id = o.team_id
			 where o.id = $1`, o.ID).
			Scan(&o.UserID, &o.Name, &o.Email, &o.TeamID, &o.TeamName)
	})
	return o, translate(err)
}

func (s *Service) Revoke(ctx context.Context, orgID, actorID, observerID string) error {
	return s.exec(ctx, orgID, actorID,
		`delete from observers where id = $1`, observerID)
}

// --- администраторы подразделений ---

// Admin — кто отвечает за поддерево. Полномочие над узлом, а не свойство
// человека: один и тот же человек бывает администратором направления
// и рядовым участником соседнего, а роль у него одна.
type Admin struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	TeamID   string `json:"teamId"`
	TeamName string `json:"teamName"`
}

func (s *Service) Admins(ctx context.Context, orgID, userID string) ([]Admin, error) {
	out := []Admin{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select a.id, u.id, u.name, u.email, t.id, t.name
			  from team_admins a
			  join users u on u.id = a.user_id
			  join teams t on t.id = a.team_id
			 order by t.name, u.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Admin
			if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &a.Email,
				&a.TeamID, &a.TeamName); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// GrantAdmin ставит человека отвечать за подразделение вместе с его
// поддеревом. Раздаёт это только владелец организации: полномочие,
// размножающее само себя, перестаёт быть ограниченным.
func (s *Service) GrantAdmin(ctx context.Context, orgID, actorID, userID, teamID string) (Admin, error) {
	var a Admin
	err := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			insert into team_admins (org_id, user_id, team_id, granted_by)
			values ($1, $2, $3, $4) returning id`,
			orgID, userID, teamID, actorID).Scan(&a.ID)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			select u.id, u.name, u.email, t.id, t.name
			  from team_admins a
			  join users u on u.id = a.user_id
			  join teams t on t.id = a.team_id
			 where a.id = $1`, a.ID).
			Scan(&a.UserID, &a.Name, &a.Email, &a.TeamID, &a.TeamName)
	})
	return a, translate(err)
}

func (s *Service) RevokeAdmin(ctx context.Context, orgID, actorID, adminID string) error {
	return s.exec(ctx, orgID, actorID, `delete from team_admins where id = $1`, adminID)
}

// --- служебное ---

func (s *Service) exec(ctx context.Context, orgID, actorID, sql string, args ...any) error {
	return translate(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// Ноль строк здесь значит одно из двух: строки нет или её
			// не видно политике. Для того, кто спрашивает, разницы нет,
			// и подтверждать существование недоступного не нужно.
			return ErrNotFound
		}
		return nil
	}))
}

// explain объясняет, почему изменение ничего не задело.
//
// Дерево видно всем, поэтому «не найдено» само по себе сбивает с толку:
// человек видит подразделение в списке и получает ответ, что его нет.
// Проверка отвечает на вопрос «а было ли что менять»: если было — значит
// не хватило прав, и так и надо сказать.
func (s *Service) explain(ctx context.Context, orgID, actorID string, err error, probe string, args ...any) error {
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	var exists bool
	if failed := s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, probe, args...).Scan(&exists)
	}); failed != nil {
		return err
	}
	if exists {
		return ErrForbidden
	}
	return ErrNotFound
}

// translate переводит отказ ограничений дерева в ошибку с человеческим
// текстом. Всё остальное проходит как есть.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23514": // check_violation: глубина или цикл
			return &TreeError{Reason: pgErr.Message}
		case "42501": // insufficient_privilege: не прошла политика
			return ErrForbidden
		case "23503": // foreign_key_violation: нет такого родителя
			return ErrNotFound
		case "23505": // unique_violation: полномочие уже выдано
			return &TreeError{Reason: "такое полномочие уже выдано"}
		}
	}
	return err
}
