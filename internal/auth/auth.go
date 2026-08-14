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
		// Считаем bcrypt и на несуществующем пользователе, чтобы по времени
		// ответа нельзя было перебрать список зарегистрированных адресов.
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
		select o.id, o.name, o.slug, m.role
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
		if err := rows.Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &m.Role); err != nil {
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
		select u.id, u.email, u.name, o.id, o.name, o.slug, m.role
		  from sessions s
		  join users u on u.id = s.user_id
		  join memberships m on m.user_id = u.id and m.org_id = s.active_org_id
		  join orgs o on o.id = m.org_id
		 where s.id = $1 and s.expires_at > now()`, sessionID).
		Scan(&p.ID, &p.Email, &p.Name, &p.OrgID, &p.OrgName, &p.OrgSlug, &p.Role)
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
