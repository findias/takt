// Package org — организации, состав команды и приглашения.
//
// Организация здесь и есть арендатор. Всё, что человек видит и меняет,
// принадлежит ровно одной организации, а попасть в неё можно только двумя
// способами: создать самому или принять приглашение.
package org

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/store"
)

const InviteTTL = 7 * 24 * time.Hour

var (
	ErrNotFound      = errors.New("не найдено")
	ErrInviteInvalid = errors.New("ссылка недействительна или срок её действия истёк")
	ErrAlreadyMember = errors.New("этот человек уже в команде")
	ErrLastOwner     = errors.New("в организации должен остаться хотя бы один владелец")

	// ErrSharedIdentity — личность живёт не только здесь. Обезличить её
	// из одной организации значило бы стереть человека там, где о его
	// удалении никто не просил.
	ErrSharedIdentity = errors.New(
		"этот человек состоит и в других организациях: обезличить личность отсюда нельзя, можно только исключить")

	// ErrServiceIdentity — за личностью стоит ключ, а не человек.
	// Персональных данных у неё нет, а обезличивание сломало бы подписи
	// интеграции.
	// Отказ говорит, что делать: ключ убирают отзывом ключа, а не
	// исключением его личности — исключённая личность оставила бы
	// действующий токен без доступа и без объяснения, почему обмен
	// с соседней системой вдруг перестал работать.
	ErrServiceIdentity = errors.New(
		"это служебная личность ключа, а не человек: " +
			"отзовите сам ключ в разделе «Ключи для интеграций»")
)

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

type Member struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	// Kind — человек или ключ. Ключ состоит в организации ровно как
	// человек, и это правильно: политики обходятся без второй ветки,
	// а у действия есть автор с именем. Но предлагать ключу роль,
	// исключение и удаление данных незачем — потому вид и называется.
	Kind     string    `json:"kind"`
	JoinedAt time.Time `json:"joinedAt"`
}

// KindService — личность, за которой стоит ключ интеграции, а не человек.
const KindService = "service"

type Invite struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	// Link заполняется только в ответе на создание: токен в базе не хранится,
	// поэтому показать ссылку повторно невозможно.
	Link string `json:"link,omitempty"`
}

// Create заводит организацию и делает создателя её владельцем.
//
// Столкновение адресов лечится повтором, а не блокировкой. Свободный
// адрес подбирается чтением, а занимается отдельной вставкой — и две
// одинаково названные организации, заведённые в одну секунду, выбирали
// один и тот же свободный адрес: вторая падала «внутренней ошибкой»
// на уникальном индексе. Ловилось это редко и не там, где случалось:
// сквозные проверки, заводящие одноимённую организацию разом, мигали
// на входе. Повтор дешевле блокировки: столкновение — редкость,
// а чтение при повторе уже видит занятое.
func (s *Service) Create(ctx context.Context, name, ownerUserID string) (auth.Membership, error) {
	var m auth.Membership
	var err error
	for попытка := 0; ; попытка++ {
		m, err = s.create(ctx, name, ownerUserID)
		// Повторов столько же, сколько бывает одновременных регистраций
		// в одну секунду с запасом: каждая неудача — это чей-то выигрыш,
		// то есть очередь разбирается за столько же заходов, сколько
		// участников.
		if err == nil || попытка >= 15 || !адресЗанят(err) {
			return m, err
		}
	}
}

// адресЗанят — вставка не прошла по уникальному индексу адреса.
// Именно по нему: остальные нарушения уникальности повторять незачем,
// они не рассосутся.
func адресЗанят(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "orgs_slug_key"
}

func (s *Service) create(ctx context.Context, name, ownerUserID string) (auth.Membership, error) {
	var m auth.Membership
	err := s.db.InScope(ctx, store.Scope{}, func(tx pgx.Tx) error {
		slug, err := uniqueSlug(ctx, tx, name)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx,
			`insert into orgs (name, slug) values ($1, $2) returning id, name, slug`,
			name, slug).Scan(&m.OrgID, &m.OrgName, &m.OrgSlug)
		if err != nil {
			return err
		}
		// Единственное место, где область выставляется посреди транзакции:
		// до вставки арендатора не существует, а членство уже должно
		// попасть в журнал именным.
		if _, err = tx.Exec(ctx, `
			select set_config('app.current_org', $1, true),
			       set_config('app.current_user', $2, true)`,
			m.OrgID, ownerUserID); err != nil {
			return err
		}

		m.Role = auth.RoleOwner
		_, err = tx.Exec(ctx,
			`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
			m.OrgID, ownerUserID)
		return err
	})
	return m, err
}

// Members возвращает состав организации.
func (s *Service) Members(ctx context.Context, orgID string) ([]Member, error) {
	rows, err := s.db.Pool.Query(ctx, `
		select u.id, u.name, u.email, m.role, u.kind, m.created_at
		  from memberships m
		  join users u on u.id = m.user_id
		 where m.org_id = $1
		 order by m.created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.Kind, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetRole меняет роль участника.
func (s *Service) SetRole(ctx context.Context, orgID, actorID, userID, role string) error {
	if role != auth.RoleOwner && role != auth.RoleMember && role != auth.RoleViewer {
		return fmt.Errorf("неизвестная роль %q", role)
	}
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		if role != auth.RoleOwner {
			if err := ensureOtherOwnerExists(ctx, tx, orgID, userID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx,
			`update memberships set role = $3 where org_id = $1 and user_id = $2`,
			orgID, userID, role)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Remove исключает человека из организации. Личность и её членство
// в других организациях не затрагиваются.
func (s *Service) Remove(ctx context.Context, orgID, actorID, userID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		if err := ensureOtherOwnerExists(ctx, tx, orgID, userID); err != nil {
			return err
		}
		// Личность ключа исключать нельзя: токен остался бы действующим,
		// а доступа у него бы не было — обмен с соседней системой
		// сломался бы молча. Ключ убирают отзывом ключа.
		if err := ensureNotService(ctx, tx, userID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`delete from memberships where org_id = $1 and user_id = $2`, orgID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// Сессии исключённого, привязанные к этой организации, переводим
		// на любую другую при следующем запросе — за это отвечает auth.
		_, err = tx.Exec(ctx,
			`update sessions set active_org_id = null
			  where user_id = $1 and active_org_id = $2`, userID, orgID)
		return err
	})
}

// Erase исполняет требование об удалении персональных данных.
//
// Строку `users` при этом не удаляют, и это не обход требования, а его
// единственное честное исполнение: на личность ссылаются подписи под
// работой — журнал действий, назначения, комментарии, — и половина
// ссылок без каскада. Удаление, стирающее историю чужой работы, хуже
// отсутствия удаления.
//
// Поэтому личность остаётся, а персональных данных в ней не остаётся:
// имя, почта и внешняя личность стираются, вход становится невозможен,
// сессии обрываются. Подписи продолжают указывать на того же, кто делал
// работу, — просто у него больше нет имени.
//
// Чего это не покрывает: почта, попавшая в журнал действий вместе
// с приглашением. Журнал только дописывается — переписать историю нельзя
// и владельцу, — и стирается он сроком хранения организации, а не
// точечной правкой. Это тот же механизм, который уже есть, и другого
// честного здесь нет.
func (s *Service) Erase(ctx context.Context, orgID, actorID, userID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		if err := ensureOtherOwnerExists(ctx, tx, orgID, userID); err != nil {
			return err
		}

		// Личность глобальна, а требование пришло в одну организацию.
		var elsewhere int
		if err := tx.QueryRow(ctx,
			`select count(*) from memberships where user_id = $1 and org_id <> $2`,
			userID, orgID).Scan(&elsewhere); err != nil {
			return err
		}
		if elsewhere > 0 {
			return ErrSharedIdentity
		}

		if err := ensureNotService(ctx, tx, userID); err != nil {
			return err
		}

		// Участие снимается первым: его удаление пишется в журнал
		// триггером, и в журнале это видно как отдельный факт.
		if _, err := tx.Exec(ctx,
			`delete from memberships where org_id = $1 and user_id = $2`,
			orgID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from sessions where user_id = $1`, userID); err != nil {
			return err
		}

		// Почта обязана остаться уникальной, поэтому не пустая, а заведомо
		// недостижимая: `.invalid` зарезервирован RFC 2606 и не бывает
		// ничьим адресом. Пустой пароль не совпадёт ни с чем: место хеша
		// занято значением, которое хешем не является.
		tag, err := tx.Exec(ctx, `
			update users
			   set name          = 'Удалённый участник',
			       email         = 'deleted+' || id::text || '@invalid',
			       password_hash = '',
			       oidc_issuer   = null,
			       oidc_subject  = null,
			       anonymized_at = now()
			 where id = $1 and anonymized_at is null`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		// Запись в журнал делается кодом, а не триггером, — единственное
		// такое место, и причина в самой таблице: у `users` нет org_id,
		// а журнал ведётся по организациям. Триггеру неоткуда узнать,
		// в чей журнал писать.
		_, err = tx.Exec(ctx, `
			insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
			values ($1, (select app_current_user()), 'delete', 'users', $2,
			        jsonb_build_object('reason', 'обезличивание по требованию об удалении данных'))`,
			orgID, userID)
		return err
	})
}

// ensureNotService отличает ключ от человека.
//
// Вид спрашивается у самой личности, а не у `api_clients`: список ключей
// видит только владелец организации, а вопрос «человек ли это» задаётся
// и там, где владельца рядом нет.
func ensureNotService(ctx context.Context, tx pgx.Tx, userID string) error {
	var kind string
	if err := tx.QueryRow(ctx, `select kind from users where id = $1`, userID).Scan(&kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if kind == KindService {
		return ErrServiceIdentity
	}
	return nil
}

// ensureOtherOwnerExists не даёт снять последнего владельца: организация
// без владельца становится неуправляемой, и починить это можно только
// руками в базе.
func ensureOtherOwnerExists(ctx context.Context, tx pgx.Tx, orgID, exceptUserID string) error {
	var role string
	err := tx.QueryRow(ctx,
		`select role from memberships where org_id = $1 and user_id = $2`,
		orgID, exceptUserID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role != auth.RoleOwner {
		return nil
	}
	var others int
	err = tx.QueryRow(ctx, `
		select count(*) from memberships
		 where org_id = $1 and role = 'owner' and user_id <> $2`, orgID, exceptUserID).Scan(&others)
	if err != nil {
		return err
	}
	if others == 0 {
		return ErrLastOwner
	}
	return nil
}

// EstimateUnits — в чём организация оценивает работу. Список повторяет
// ограничение схемы (миграция 0014): значения, которого нет в базе,
// быть не должно и в коде.
var EstimateUnits = []string{"points", "hours", "days"}

// SetEstimateUnit меняет единицу оценки организации.
//
// Числа не пересчитываются: тройка остаётся тройкой, менялась только
// подпись под ней. Пересчёт был бы враньём — очки не переводятся в часы
// никаким коэффициентом, это разные способы обещать, а не разные меры
// одного. Поэтому смена — решение владельца, и интерфейс говорит о ней
// прямо, а не показывает как настройку вида.
func (s *Service) SetEstimateUnit(ctx context.Context, orgID, actorID, unit string) error {
	if !slices.Contains(EstimateUnits, unit) {
		return fmt.Errorf("единица оценки бывает только одной из: очки, часы, дни")
	}
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update orgs set estimate_unit = $2 where id = $1`, orgID, unit)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// --- приглашения ---

// Invite создаёт приглашение и возвращает ссылку. Ссылка показывается
// один раз: в базе лежит только хеш токена.
func (s *Service) Invite(ctx context.Context, orgID, invitedBy, email, role, baseURL string) (Invite, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !strings.Contains(email, "@") {
		return Invite{}, fmt.Errorf("укажите почту")
	}
	if role != auth.RoleOwner && role != auth.RoleMember && role != auth.RoleViewer {
		return Invite{}, fmt.Errorf("неизвестная роль %q", role)
	}

	var already bool
	err := s.db.Pool.QueryRow(ctx, `
		select exists (
			select 1 from memberships m join users u on u.id = m.user_id
			 where m.org_id = $1 and lower(u.email) = $2)`, orgID, email).Scan(&already)
	if err != nil {
		return Invite{}, err
	}
	if already {
		return Invite{}, ErrAlreadyMember
	}

	token, err := newToken()
	if err != nil {
		return Invite{}, err
	}

	var inv Invite
	err = s.db.InTenant(ctx, orgID, invitedBy, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			insert into invites (org_id, email, role, token_hash, invited_by, expires_at)
			values ($1, $2, $3, $4, $5, $6)
			returning id, email, role, expires_at, created_at`,
			orgID, email, role, hashToken(token), invitedBy, time.Now().Add(InviteTTL)).
			Scan(&inv.ID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.CreatedAt)
	})
	if err != nil {
		return Invite{}, err
	}
	inv.Link = strings.TrimRight(baseURL, "/") + "/invite/" + token
	return inv, nil
}

// PendingInvites возвращает неиспользованные приглашения организации.
func (s *Service) PendingInvites(ctx context.Context, orgID string) ([]Invite, error) {
	out := []Invite{}
	err := s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, email, role, expires_at, created_at
			  from invites
			 where accepted_at is null and revoked_at is null and expires_at > now()
			 order by created_at desc`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var i Invite
			if err := rows.Scan(&i.ID, &i.Email, &i.Role, &i.ExpiresAt, &i.CreatedAt); err != nil {
				return err
			}
			out = append(out, i)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) RevokeInvite(ctx context.Context, orgID, actorID, inviteID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update invites set revoked_at = now()
			  where id = $1 and accepted_at is null and revoked_at is null`, inviteID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// InviteInfo — то, что можно показать по ссылке до входа в систему.
// Ровно столько, сколько нужно, чтобы человек понял, куда его зовут.
type InviteInfo struct {
	OrgName      string `json:"orgName"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	NeedsAccount bool   `json:"needsAccount"`
}

func (s *Service) LookupInvite(ctx context.Context, token string) (InviteInfo, error) {
	var info InviteInfo
	// Область — сам токен: политика в базе откроет ровно одну строку
	// приглашения и ничего больше.
	err := s.db.InScope(ctx, store.Scope{InviteToken: hashToken(token)}, func(tx pgx.Tx) error {
		var orgID string
		err := tx.QueryRow(ctx, `
			select org_id, email, role from invites
			 where accepted_at is null and revoked_at is null and expires_at > now()`).
			Scan(&orgID, &info.Email, &info.Role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteInvalid
		}
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select name from orgs where id = $1`, orgID).
			Scan(&info.OrgName); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from users where lower(email) = $1)`, info.Email).
			Scan(&exists); err != nil {
			return err
		}
		info.NeedsAccount = !exists
		return nil
	})
	return info, err
}

// Accept добавляет пользователя в организацию по приглашению.
func (s *Service) Accept(ctx context.Context, token, userID string) (auth.Membership, error) {
	var m auth.Membership
	err := s.db.InScope(ctx, store.Scope{InviteToken: hashToken(token)}, func(tx pgx.Tx) error {
		var inviteID, orgID, role string
		err := tx.QueryRow(ctx, `
			select id, org_id, role from invites
			 where accepted_at is null and revoked_at is null and expires_at > now()
			 for update`).Scan(&inviteID, &orgID, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteInvalid
		}
		if err != nil {
			return err
		}

		// Область открывалась токеном, а не организацией: до чтения
		// приглашения ни арендатор, ни человек не были известны. Теперь
		// известны оба, и принятие приглашения попадёт в журнал именным.
		if _, err = tx.Exec(ctx, `
			select set_config('app.current_org', $1, true),
			       set_config('app.current_user', $2, true)`,
			orgID, userID); err != nil {
			return err
		}

		// Повторный переход по ссылке не должен ломаться и не должен
		// понижать роль тому, кто уже в команде.
		_, err = tx.Exec(ctx, `
			insert into memberships (org_id, user_id, role) values ($1, $2, $3)
			on conflict (org_id, user_id) do nothing`, orgID, userID, role)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx,
			`update invites set accepted_at = now(), accepted_by = $2 where id = $1`,
			inviteID, userID)
		if err != nil {
			return err
		}

		return tx.QueryRow(ctx, `
			select o.id, o.name, o.slug, m.role
			  from orgs o join memberships m on m.org_id = o.id
			 where o.id = $1 and m.user_id = $2`, orgID, userID).
			Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &m.Role)
	})
	return m, err
}

// --- служебное ---

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func uniqueSlug(ctx context.Context, tx pgx.Tx, name string) (string, error) {
	base := slugify(name)
	candidate := base
	for attempt := 2; attempt < 100; attempt++ {
		var taken bool
		err := tx.QueryRow(ctx,
			`select exists (select 1 from orgs where lower(slug) = $1)`, candidate).Scan(&taken)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, attempt)
	}
	// Сотня занятых вариантов — повод не гадать дальше, а взять случайный.
	suffix, err := newToken()
	if err != nil {
		return "", err
	}
	return base + "-" + strings.ToLower(suffix[:6]), nil
}

// translit — минимальная таблица для кириллицы: slug должен читаться
// в адресной строке, а не превращаться в набор процентов.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		case translit[r] != "":
			b.WriteString(translit[r])
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(collapseDashes(b.String()), "-")
	if slug == "" {
		slug = "team"
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return slug
}

func collapseDashes(s string) string {
	var b strings.Builder
	var prevDash bool
	for _, r := range s {
		if r == '-' {
			if !prevDash {
				b.WriteRune(r)
			}
			prevDash = true
			continue
		}
		prevDash = false
		b.WriteRune(r)
	}
	return b.String()
}
