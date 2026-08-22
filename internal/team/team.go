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
	//
	// Правило держит база (миграция 0045), а эта ошибка осталась ради
	// одного: сравнивать с ней. Считал его раньше здешний код — и мимо
	// него прошёл каталог по SCIM, который убирает группу своим
	// запросом. Одно правило, две двери, охраняется одна: третий такой
	// случай за день.
	ErrNotEmpty = errors.New("сначала перенесите вложенные команды и доски")
	// ErrForbidden — отказ политики. Приходить сюда он не должен:
	// маршруты уже требуют владельца. Если пришёл — значит появился путь
	// в обход проверки, и ответить надо запретом, а не пятисоткой.
	ErrForbidden = errors.New("недостаточно прав")
	// ErrNotOrgMember — назвали человека, которого в организации нет.
	// Отдельно от ErrNotFound потому, что «не найдено» здесь отправляет
	// искать не то: ищут подразделение или запись, а не сходится человек.
	// Так же отвечает и назначение исполнителя на доске.
	ErrNotOrgMember = errors.New("это может быть только участник организации")
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
	// FromDirectory — узел заведён каталогом по SCIM.
	//
	// Различие не косметическое: состав такого узла ведёт каталог,
	// и полная замена состава при следующей синхронизации сотрёт
	// вписанных руками. Провайдеры шлют именно замену, и выбирает
	// это не наша сторона; значит, единственное, что мы можем
	// не делать, — молчать об этом. Прогон 22 августа 2026: человек
	// вписал участника (204), каталог прислал пустой список, участник
	// исчез, и нигде не было сказано, что так будет.
	FromDirectory bool `json:"fromDirectory"`
}

// Board — доска подразделения в его составе. Отдаётся ровно то, чем
// доска называется и открывается: ключ показывает, какими номерами она
// нумерует задачи, а видимость отвечает на вопрос, который к структуре
// и задают, — «кто это видит».
type Board struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Key        string `json:"key"`
	Visibility string `json:"visibility"`
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
			         where b.team_id = t.id and b.archived_at is null),
			       t.external_id is not null
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
				&t.Members, &t.Boards, &t.FromDirectory); err != nil {
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

// Archive убирает подразделение в архив. Пустоту узла проверяет база
// (миграция 0045): здесь этого больше нет намеренно — правило, лежащее
// в двух местах, однажды оказывается выполненным в одном.
func (s *Service) Archive(ctx context.Context, orgID, actorID, teamID string) error {
	return translate(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
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

// Archived перечисляет убранные подразделения — иначе вернуть их будет
// неоткуда.
//
// До 22 августа 2026 возврата не было вовсе: «Убрать подразделение»
// уносило узел из дерева без вопроса и без дороги назад. Это нарушало
// собственное правило проекта сразу с двух сторон — «необратимое
// спрашивает, обратимое — нет»: действие не спрашивало, как обратимое,
// и не возвращалось, как необратимое. У досок архив с возвратом есть
// с самого начала; у подразделений его просто забыли.
//
// Родитель показывается именем, а не идентификатором: из архива
// выбирают, что вернуть, и «Ядро» без ответа на «чьё ядро» — выбор
// вслепую. Пустое имя родителя значит корень.
func (s *Service) Archived(ctx context.Context, orgID, userID string) ([]ArchivedTeam, error) {
	out := []ArchivedTeam{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select t.id, t.name, coalesce(p.name, ''), p.archived_at is not null,
			       t.archived_at
			  from teams t
			  left join teams p on p.id = t.parent_id
			 where t.archived_at is not null
			 order by t.archived_at desc`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a ArchivedTeam
			if err := rows.Scan(&a.ID, &a.Name, &a.ParentName,
				&a.ParentArchived, &a.ArchivedAt); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, translate(err)
}

// ArchivedTeam — строка архива подразделений.
type ArchivedTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// ParentName пуст у корневого подразделения.
	ParentName string `json:"parentName"`
	// ParentArchived говорит, что вернуть этот узел сейчас нельзя:
	// его старший тоже в архиве. Отвечать на это отказом после нажатия
	// значило бы держать кнопку, которая заведомо не сработает.
	ParentArchived bool      `json:"parentArchived"`
	ArchivedAt     time.Time `json:"archivedAt"`
}

// Restore возвращает подразделение из архива.
//
// Родитель обязан быть живым: узел возвращается туда, откуда ушёл,
// а возвращать его под архивированного старшего — значит поставить его
// в дерево, которого не видно. Отказ называет старшего по имени,
// потому что порядок действий («сперва верните его») человеку иначе
// придётся угадывать.
func (s *Service) Restore(ctx context.Context, orgID, actorID, teamID string) error {
	return translate(s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var parentName *string
		err := tx.QueryRow(ctx, `
			select p.name
			  from teams t
			  left join teams p on p.id = t.parent_id and p.archived_at is not null
			 where t.id = $1 and t.archived_at is not null`, teamID).Scan(&parentName)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if parentName != nil {
			return &TreeError{Reason: "сначала верните из архива подразделение «" +
				*parentName + "»: внутри него это и лежит"}
		}

		tag, err := tx.Exec(ctx,
			`update teams set archived_at = null
			  where id = $1 and archived_at is not null`, teamID)
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

// Boards отдаёт доски подразделения.
//
// Список тот же, что считает `List` числом: политики решают, какие доски
// видны спрашивающему, и разным людям одно и то же подразделение честно
// покажет разное. Отбор по team_id, а не по видимости: доска остаётся
// доской подразделения, даже если видна всей организации, — команда
// у неё отметка о принадлежности, а не только правило доступа.
func (s *Service) Boards(ctx context.Context, orgID, userID, teamID string) ([]Board, error) {
	out := []Board{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select b.id, b.name, b.key, b.visibility
			  from boards b
			 where b.team_id = $1 and b.archived_at is null
			 order by b.name`, teamID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Board
			if err := rows.Scan(&b.ID, &b.Name, &b.Key, &b.Visibility); err != nil {
				return err
			}
			out = append(out, b)
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
		if err := ensureInOrg(ctx, tx, orgID, userID); err != nil {
			return err
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

// Observers перечисляет надзор — и только тот, который что-то значит.
//
// Записи на архивированные подразделения отсеиваются, потому что
// не действуют: `app_observed_teams()` считает наблюдаемое по живым
// узлам, и надзор за убранным узлом прекращается сам собой. Показывать
// его значило бы держать в интерфейсе слово, за которым ничего нет, —
// от этого уже избавились в 4.2, убрав пометку ведущего, и тут то же
// самое, только хуже: наблюдение обещает надзор, а надзора нет,
// и не знают об этом обе стороны сразу.
//
// Записи не удаляются: узел бывает возвращают из архива, и надзор
// возвращается вместе с ним. Отсеивается показ, а не право.
//
// Наблюдение за организацией целиком (`team_id is null`) остаётся
// всегда: у него нет узла, которому нечего архивировать.
func (s *Service) Observers(ctx context.Context, orgID, userID string) ([]Observer, error) {
	out := []Observer{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select o.id, u.id, u.name, u.email, o.team_id, t.name
			  from observers o
			  join users u on u.id = o.user_id
			  left join teams t on t.id = o.team_id
			 where o.team_id is null or t.archived_at is null
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
		if err := ensureInOrg(ctx, tx, orgID, userID); err != nil {
			return err
		}
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
	// Наблюдение видно всем в организации, поэтому «не найдено» на видимой
	// строке сбивало бы с толку: человек видит запись в списке и слышит,
	// что её нет. Спрашиваем, было ли что снимать, и отвечаем «нельзя».
	err := s.exec(ctx, orgID, actorID, `delete from observers where id = $1`, observerID)
	return s.explain(ctx, orgID, actorID, err,
		`select exists (select 1 from observers where id = $1)`, observerID)
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

// Admins — то же правило, что и у наблюдения: администратор
// архивированного узла не распоряжается ничем, потому что
// `app_admin_teams()` считает область по живым узлам. Запись остаётся
// на случай возврата, из списка уходит.
func (s *Service) Admins(ctx context.Context, orgID, userID string) ([]Admin, error) {
	out := []Admin{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select a.id, u.id, u.name, u.email, t.id, t.name
			  from team_admins a
			  join users u on u.id = a.user_id
			  join teams t on t.id = a.team_id
			 where t.archived_at is null
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
		if err := ensureInOrg(ctx, tx, orgID, userID); err != nil {
			return err
		}
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

// ensureInOrg требует, чтобы человек состоял в организации.
//
// Это и есть та самая граница, которую у таблиц личности держит код,
// а не политики: `users` под RLS не попадает намеренно (миграция 0002),
// и запрос по чужому идентификатору вернёт чужую строку — политики его
// не остановят. Значит, всякий раз, когда наружу принимается
// идентификатор человека, состоять в организации он обязан не по вере,
// а по проверке.
//
// Отсутствие проверки видно не отказом, а тишиной: строка заводится,
// и следом список отдаёт имя и почту постороннего — join к `users`
// возвращает того, кого ему назвали. Так и было у наблюдения
// и у прав администратора подразделения до 22 августа: состав
// подразделения проверял, а эти двое — нет.
//
// Отказ называет причину, а не «не найдено»: «не найдено» отправляет
// искать не то — ищут подразделение или запись, а не сходится человек.
func ensureInOrg(ctx context.Context, tx pgx.Tx, orgID, userID string) error {
	var member bool
	if err := tx.QueryRow(ctx, `
		select exists (select 1 from memberships
		                where org_id = $1 and user_id = $2)`,
		orgID, userID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return ErrNotOrgMember
	}
	return nil
}

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
