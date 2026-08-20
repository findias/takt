// Package auth — личности, членство в организациях и сессии.
//
// Ключевое разделение: пользователь — это личность (почта и пароль,
// глобально уникальные), а участие в организации — отдельная сущность
// со своей ролью. Один человек может состоять в нескольких организациях
// с разными правами, и сессия помнит, в какой из них он сейчас работает.
//
// Сессия хранится строкой в таблице sessions, идентификатор — в httpOnly
// cookie. Осознанный выбор против самодостаточного JWT: сессию нужно уметь
// отзывать мгновенно (увольнение, утечка), а на нашем масштабе один
// индексный запрос на запрос к API ничего не стоит.
package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName     = "board_session"
	SessionTTL     = 30 * 24 * time.Hour
	MinPasswordLen = 8

	RoleOwner  = "owner"
	RoleMember = "member"
	RoleViewer = "viewer"
)

var (
	ErrBadCredentials = errors.New("неверная почта или пароль")
	ErrNoSession      = errors.New("нет активной сессии")
	ErrNoMembership   = errors.New("нет доступа ни к одной организации")

	// ErrWrongPassword — текущий пароль не подошёл. Отдельно
	// от ErrBadCredentials: там неизвестно, кто пришёл, а здесь человек
	// уже вошёл и ошибся в одном поле, и отказ должен говорить именно
	// про это поле.
	ErrWrongPassword = errors.New("текущий пароль не подошёл")

	// ErrFederated — паролем эта личность не входит. Отказ называет, что
	// делать: пароль корпоративной учётной записи меняют у провайдера,
	// и заводить второй пароль здесь значило бы оставить вход, который
	// не закроется при увольнении.
	ErrFederated = errors.New(
		"вы входите через корпоративный провайдер — пароль меняется там же")
)

// Identity — человек безотносительно организации.
type Identity struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Membership — участие личности в организации.
type Membership struct {
	OrgID   string `json:"orgId"`
	OrgName string `json:"orgName"`
	OrgSlug string `json:"orgSlug"`
	Role    string `json:"role"`
	// EstimateUnit — в чём организация оценивает работу: очки, часы, дни.
	// Свойство организации, а не карточки: складывать часы с очками
	// бессмысленно, а прогресс — это сложение.
	EstimateUnit string `json:"estimateUnit"`
}

// Principal — кто выполняет запрос и от имени какой организации.
// Всё, что нужно обработчику, чтобы решить, можно ли ему это делать.
type Principal struct {
	Identity
	Membership
	SessionID string `json:"-"`
}

func (p Principal) CanEdit() bool  { return p.Role == RoleOwner || p.Role == RoleMember }
func (p Principal) CanAdmin() bool { return p.Role == RoleOwner }

func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// Authenticate проверяет пару почта/пароль. Почта глобально уникальна,
// поэтому организация здесь не участвует — она выбирается после входа.
func Authenticate(ctx context.Context, pool *pgxpool.Pool, email, password string) (Identity, error) {
	var u Identity
	var hash string
	err := pool.QueryRow(ctx, `
		select id, email, name, password_hash
		  from users
		 where lower(email) = lower($1)`, email).
		Scan(&u.ID, &u.Email, &u.Name, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		// Считаем bcrypt и на несуществующем пользователе: иначе ответ
		// приходит мгновенно, и по времени видно, заведён ли адрес.
		//
		// Список адресов это сегодня всё равно не прячет, и притворяться
		// не нужно: регистрация открыта, а на занятый адрес она отвечает
		// «такая почта уже зарегистрирована — войдите». Сказать это
		// человеку, заводящему учётную запись, приходится, а других
		// способов узнать, занят ли адрес, ему не оставили. Значит,
		// перечислить адреса можно и без всякого измерения времени.
		//
		// Задержка оставлена намеренно и стоит недорого: попытки входа
		// считаны и ограничены (см. httpapi/attempts.go), а закроется
		// регистрация — и она окажется единственным, что закрывать
		// не придётся. Убрать её значит завести вторую дыру на тот день,
		// когда первую заделают.
		_, _ = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		return Identity{}, ErrBadCredentials
	}
	if err != nil {
		return Identity{}, err
	}
	if !CheckPassword(hash, password) {
		return Identity{}, ErrBadCredentials
	}
	return u, nil
}

// Memberships возвращает все организации пользователя.
func Memberships(ctx context.Context, pool *pgxpool.Pool, userID string) ([]Membership, error) {
	rows, err := pool.Query(ctx, `
		select o.id, o.name, o.slug, m.role, o.estimate_unit
		  from memberships m
		  join orgs o on o.id = m.org_id
		 where m.user_id = $1
		 order by o.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &m.Role, &m.EstimateUnit); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateSession заводит сессию. Активной становится первая организация
// пользователя; если их нет — сессия не создаётся.
func CreateSession(ctx context.Context, pool *pgxpool.Pool, userID string) (string, time.Time, error) {
	memberships, err := Memberships(ctx, pool, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if len(memberships) == 0 {
		return "", time.Time{}, ErrNoMembership
	}

	expires := time.Now().Add(SessionTTL)
	var id string
	err = pool.QueryRow(ctx, `
		insert into sessions (user_id, expires_at, active_org_id)
		values ($1, $2, $3) returning id`,
		userID, expires, memberships[0].OrgID).Scan(&id)
	return id, expires, err
}

// ChangePassword меняет пароль и обрывает все прочие сессии.
//
// Обрыв — не довесок, а половина смысла: пароль меняют, когда он мог
// утечь, а утёкший пароль к этому времени уже мог стать чужой сессией.
// Оставить их жить значило бы сменить замок, не отобрав выданные ключи.
// Своя сессия остаётся: человек только что подтвердил, что это он,
// и выкидывать его из собственного браузера незачем.
//
// Текущий пароль спрашивается по той же причине: сессию могли украсть,
// и смена пароля из украденной сессии заперла бы хозяина снаружи.
func ChangePassword(ctx context.Context, pool *pgxpool.Pool,
	userID, sessionID, current, next string) error {

	var hash string
	var federated bool
	err := pool.QueryRow(ctx, `
		select password_hash, oidc_subject is not null
		  from users where id = $1`, userID).Scan(&hash, &federated)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return err
	}
	if federated {
		return ErrFederated
	}
	if !CheckPassword(hash, current) {
		return ErrWrongPassword
	}

	fresh, err := HashPassword(next)
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx,
		`update users set password_hash = $2 where id = $1`, userID, fresh); err != nil {
		return err
	}
	return RevokeOtherSessions(ctx, pool, userID, sessionID)
}

// RevokeOtherSessions выкидывает человека отовсюду, кроме этого браузера.
//
// Отдельно от смены пароля: сессия утекает и без пароля — чужой
// компьютер, забытая вкладка, — и в этом случае менять пароль незачем,
// а закрыть чужой вход надо.
func RevokeOtherSessions(ctx context.Context, pool *pgxpool.Pool, userID, keep string) error {
	_, err := pool.Exec(ctx,
		`delete from sessions where user_id = $1 and id <> $2`, userID, keep)
	return err
}

func DeleteSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) error {
	_, err := pool.Exec(ctx, `delete from sessions where id = $1`, sessionID)
	return err
}

// SwitchOrg меняет активную организацию сессии. Членство проверяется здесь же:
// подставить чужой идентификатор в запрос не получится.
func SwitchOrg(ctx context.Context, pool *pgxpool.Pool, sessionID, userID, orgID string) error {
	tag, err := pool.Exec(ctx, `
		update sessions set active_org_id = $3
		 where id = $1
		   and exists (select 1 from memberships
		                where user_id = $2 and org_id = $3)`,
		sessionID, userID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoMembership
	}
	return nil
}

// PrincipalBySession возвращает автора запроса вместе с его ролью
// в активной организации.
func PrincipalBySession(ctx context.Context, pool *pgxpool.Pool, sessionID string) (Principal, error) {
	var p Principal
	p.SessionID = sessionID
	err := pool.QueryRow(ctx, `
		select u.id, u.email, u.name, o.id, o.name, o.slug, m.role, o.estimate_unit
		  from sessions s
		  join users u on u.id = s.user_id
		  join memberships m on m.user_id = u.id and m.org_id = s.active_org_id
		  join orgs o on o.id = m.org_id
		 where s.id = $1 and s.expires_at > now()`, sessionID).
		Scan(&p.ID, &p.Email, &p.Name, &p.OrgID, &p.OrgName, &p.OrgSlug, &p.Role,
			&p.EstimateUnit)
	if errors.Is(err, pgx.ErrNoRows) {
		// Либо сессии нет, либо человека исключили из активной организации,
		// пока он работал. Второй случай чиним подбором любой другой.
		return recoverSession(ctx, pool, sessionID)
	}
	return p, err
}

func recoverSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) (Principal, error) {
	var userID string
	err := pool.QueryRow(ctx,
		`select user_id from sessions where id = $1 and expires_at > now()`, sessionID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrNoSession
	}
	if err != nil {
		return Principal{}, err
	}

	memberships, err := Memberships(ctx, pool, userID)
	if err != nil {
		return Principal{}, err
	}
	if len(memberships) == 0 {
		return Principal{}, ErrNoMembership
	}
	if _, err := pool.Exec(ctx,
		`update sessions set active_org_id = $2 where id = $1`,
		sessionID, memberships[0].OrgID); err != nil {
		return Principal{}, err
	}
	return PrincipalBySession(ctx, pool, sessionID)
}

func SetCookie(w http.ResponseWriter, sessionID string, expires time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
