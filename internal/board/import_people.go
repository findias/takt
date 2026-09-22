package board

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/importer"
)

// Выбор по людям источника (ROADMAP 23.6, миграция 0064).
//
// У каждого исполнителя и автора источника — ключ: почта, если источник
// её дал, иначе source:<идентификатор в источнике>. В предпросмотре для
// ключа выбирают одно из трёх:
//
//   - match — сопоставить с участником организации (по умолчанию —
//     тот, у кого та же почта);
//   - create — завести учётную запись и участие (только владелец);
//     войдёт человек по ссылке «задать пароль» или через провайдера;
//   - skip — не переносить: его карточки получат метку человека.
//
// Ненайденный по-прежнему не выдумывается: завести можно только выбором,
// а не молча.

// Действия выбора.
const (
	PersonMatch  = "match"
	PersonCreate = "create"
	PersonSkip   = "skip"
)

// ErrPeopleOwner — заводить людей переносом может только владелец:
// состав организации — его решение, как и приглашение.
var ErrPeopleOwner = errors.New("заводить людей может только владелец организации — сопоставьте их с участниками или не переносите")

// PersonChoice — выбор по одному человеку источника.
type PersonChoice struct {
	Action string `json:"action"`
	// Участник организации — для match.
	UserID string `json:"userId,omitempty"`
	// Для create: почта (обязательна, если источник её не дал), имя
	// и роль; пусто — из источника и «участник».
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	Role  string `json:"role,omitempty"`
}

// ImportPerson — человек источника в отчёте: кто он, на скольких
// карточках и что с ним будет.
type ImportPerson struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	// Карточек, где он исполнитель; у автора одних реплик — ноль.
	Cards  int    `json:"cards"`
	Action string `json:"action"`
	// С кем сопоставлен (match) или кого завели (create).
	UserID   string `json:"userId,omitempty"`
	UserName string `json:"userName,omitempty"`
	// Откуда выбор: auto — нашёлся по почте, saved — сделан при прошлом
	// переносе, chosen — в этом запросе, none — не найден и не выбран.
	Origin string `json:"origin"`
	// Почему выбор не исполнен: почты нет, адрес занят и т. п.
	Problem string `json:"problem,omitempty"`
}

// ImportMember — участник организации: из кого выбирать при
// сопоставлении и кого завёл перенос.
type ImportMember struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	// Link — ссылка «задать пароль» для заведённого переносом; выпускает
	// её HTTP-слой после переноса и показывает один раз.
	Link string `json:"link,omitempty"`
}

// sourcePeople — люди источника по ключу, в порядке «больше карточек —
// раньше».
func sourcePeople(plan importer.Plan) []*ImportPerson {
	byKey := map[string]*ImportPerson{}
	add := func(key, name, email string) *ImportPerson {
		p := byKey[key]
		if p == nil {
			p = &ImportPerson{Key: key, Email: email}
			byKey[key] = p
		}
		if p.Name == "" {
			p.Name = name
		}
		return p
	}
	for _, c := range plan.Cards {
		for _, e := range c.Assignees {
			add(e, plan.Names[e], e).Cards++
		}
		for _, u := range c.Unmatched {
			add(sourceKey(u.Key), u.Name, "").Cards++
		}
		for _, cm := range c.Comments {
			key := cmp.Or(cm.AuthorKey, cm.AuthorEmail)
			if key != "" {
				add(key, cm.AuthorName, cm.AuthorEmail)
			}
		}
	}
	out := make([]*ImportPerson, 0, len(byKey))
	for _, p := range byKey {
		if p.Name == "" {
			p.Name = p.Email
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b *ImportPerson) int {
		if c := cmp.Compare(b.Cards, a.Cards); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

// resolvePeople решает, кто есть кто, и возвращает участника по ключу
// человека источника. Заводит людей, если так выбрано, и на переносе
// запоминает выбор, сделанный руками.
func resolvePeople(ctx context.Context, tx pgx.Tx, orgID, actorID string,
	plan importer.Plan, target ImportTarget, apply bool, rep *ImportReport) (map[string]string, error) {

	people := map[string]string{}
	persons := sourcePeople(plan)
	rep.CanCreatePeople = target.CanCreatePeople

	// Участники — из кого выбирать. Ключи интеграций в выбор не идут:
	// работу за человека они не делают.
	byEmail := map[string]string{}
	names := map[string]string{}
	rows, err := tx.Query(ctx, `
		select u.id, u.name, lower(u.email) from users u
		  join memberships m on m.user_id = u.id and m.org_id = $1
		 where u.kind = 'person' and u.anonymized_at is null
		 order by lower(u.name)`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m ImportMember
		if err := rows.Scan(&m.ID, &m.Name, &m.Email); err != nil {
			rows.Close()
			return nil, err
		}
		rep.Members = append(rep.Members, m)
		byEmail[m.Email], names[m.ID] = m.ID, m.Name
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	saved := map[string]PersonChoice{}
	rows, err = tx.Query(ctx, `
		select person_key, action, coalesce(user_id::text, '') from import_people
		 where org_id = $1 and source = $2`, orgID, plan.Source)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		var c PersonChoice
		if err := rows.Scan(&key, &c.Action, &c.UserID); err != nil {
			rows.Close()
			return nil, err
		}
		saved[key] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var remember []struct{ key, action, userID string }
	for _, p := range persons {
		choice, origin := target.People[p.Key], "chosen"
		if choice.Action == "" {
			choice, origin = saved[p.Key], "saved"
		}
		if choice.Action == "" {
			if id, ok := byEmail[p.Email]; ok && p.Email != "" {
				choice, origin = PersonChoice{Action: PersonMatch, UserID: id}, "auto"
			} else {
				choice, origin = PersonChoice{Action: PersonSkip}, "none"
			}
		}
		p.Origin = origin

		switch choice.Action {
		case PersonMatch:
			if _, ok := names[choice.UserID]; !ok {
				// Сопоставленного прежде исключили, или выбран чужой.
				p.Action, p.Problem = PersonSkip, "выбранного участника нет в организации — выберите другого"
				continue
			}
			p.Action, p.UserID, p.UserName = PersonMatch, choice.UserID, names[choice.UserID]
			people[p.Key] = choice.UserID
			if origin == "chosen" {
				remember = append(remember, struct{ key, action, userID string }{p.Key, PersonMatch, choice.UserID})
			}

		case PersonCreate:
			if !target.CanCreatePeople {
				return nil, ErrPeopleOwner
			}
			id, name, problem, fresh, err := createPerson(ctx, tx, orgID, p, choice, byEmail, names)
			if err != nil {
				return nil, err
			}
			if problem != "" {
				p.Action, p.Problem = PersonSkip, problem
				continue
			}
			p.Action, p.UserID, p.UserName = PersonCreate, id, name
			if !fresh {
				p.Action = PersonMatch
			}
			people[p.Key] = id
			if fresh {
				rep.CreatedPeople = append(rep.CreatedPeople, ImportMember{ID: id, Name: name, Email: p.Email})
			}
			remember = append(remember, struct{ key, action, userID string }{p.Key, PersonMatch, id})

		default:
			p.Action = PersonSkip
			if origin == "chosen" {
				remember = append(remember, struct{ key, action, userID string }{p.Key, PersonSkip, ""})
			}
		}
	}
	for _, p := range persons {
		rep.People = append(rep.People, *p)
	}

	if apply {
		for _, r := range remember {
			if _, err := tx.Exec(ctx, `
				insert into import_people (org_id, source, person_key, action, user_id, decided_by)
				values ($1, $2, $3, $4, nullif($5, '')::uuid, $6)
				on conflict (org_id, source, person_key) do update
				   set action = excluded.action, user_id = excluded.user_id,
				       decided_by = excluded.decided_by, decided_at = now()`,
				orgID, plan.Source, r.key, r.action, r.userID, actorID); err != nil {
				return nil, err
			}
		}
	}
	return people, nil
}

// createPerson заводит учётную запись и участие. Отказ, который человек
// может исправить (почты нет, адрес занят), возвращается словами
// в problem, а не ошибкой: предпросмотр должен его показать у человека,
// а не уронить весь перенос.
func createPerson(ctx context.Context, tx pgx.Tx, orgID string, p *ImportPerson,
	choice PersonChoice, byEmail, names map[string]string) (id, name, problem string, fresh bool, err error) {

	raw := cmp.Or(strings.TrimSpace(choice.Email), p.Email)
	if raw == "" {
		return "", "", "у человека нет почты — впишите её, чтобы завести", false, nil
	}
	email, err := auth.NormalizeEmail(raw)
	if err != nil {
		return "", "", "«" + raw + "» — не почта: впишите адрес вида имя@домен", false, nil
	}
	p.Email = email
	// Почта уже у участника — это не «завести», а «сопоставить»: второго
	// такого же человека не будет.
	if id, ok := byEmail[email]; ok {
		return id, names[id], "", false, nil
	}
	var elsewhere bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from users where lower(email) = $1)`, email).Scan(&elsewhere); err != nil {
		return "", "", "", false, err
	}
	if elsewhere {
		// Учётная запись есть, но в другой организации. Молча зачислить её
		// сюда нельзя — вход не должен менять принадлежность, и перенос
		// тоже: пусть человек примет приглашение сам.
		return "", "", "адрес занят учётной записью вне организации — пригласите человека обычным приглашением", false, nil
	}
	role := cmp.Or(choice.Role, auth.RoleMember)
	if role != auth.RoleMember && role != auth.RoleViewer && role != auth.RoleOwner {
		return "", "", "", false, ErrBadRequest
	}
	name = cmp.Or(strings.TrimSpace(choice.Name), p.Name, email)
	hash, err := auth.UnusablePassword()
	if err != nil {
		return "", "", "", false, err
	}
	if err := tx.QueryRow(ctx, `
		insert into users (email, name, password_hash, awaiting_password)
		values ($1, $2, $3, true) returning id`, email, name, hash).Scan(&id); err != nil {
		return "", "", "", false, err
	}
	if _, err := tx.Exec(ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, $3)`, orgID, id, role); err != nil {
		return "", "", "", false, err
	}
	byEmail[email], names[id] = id, name
	return id, name, "", true, nil
}
