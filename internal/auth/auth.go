// Package auth — пароли и сессии.
//
// Сессия хранится строкой в таблице sessions, идентификатор — в httpOnly
// cookie. Осознанный выбор против самодостаточного JWT: сессию нужно уметь
// отзывать мгновенно (увольнение сотрудника, утечка), а на нашем масштабе
// один индексный запрос на запрос к API ничего не стоит.
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
)

var (
	ErrBadCredentials = errors.New("неверная почта или пароль")
	ErrNoSession      = errors.New("нет активной сессии")
)

type User struct {
	ID    string `json:"id"`
	OrgID string `json:"orgId"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// Authenticate проверяет пару почта/пароль и возвращает пользователя.
func Authenticate(ctx context.Context, pool *pgxpool.Pool, email, password string) (User, error) {
	var u User
	var hash string
	err := pool.QueryRow(ctx, `
		select id, org_id, email, name, role, password_hash
		  from users
		 where lower(email) = lower($1)`, email).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.Name, &u.Role, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		// Считаем bcrypt и на несуществующем пользователе, чтобы по времени
		// ответа нельзя было перебрать список зарегистрированных адресов.
		_, _ = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !CheckPassword(hash, password) {
		return User{}, ErrBadCredentials
	}
	return u, nil
}

func CreateSession(ctx context.Context, pool *pgxpool.Pool, userID string) (string, time.Time, error) {
	expires := time.Now().Add(SessionTTL)
	var id string
	err := pool.QueryRow(ctx, `
		insert into sessions (user_id, expires_at) values ($1, $2) returning id`,
		userID, expires).Scan(&id)
	return id, expires, err
}

func DeleteSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) error {
	_, err := pool.Exec(ctx, `delete from sessions where id = $1`, sessionID)
	return err
}

// UserBySession возвращает владельца сессии, попутно удаляя протухшие.
func UserBySession(ctx context.Context, pool *pgxpool.Pool, sessionID string) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `
		select u.id, u.org_id, u.email, u.name, u.role
		  from sessions s
		  join users u on u.id = s.user_id
		 where s.id = $1 and s.expires_at > now()`, sessionID).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.Name, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoSession
	}
	return u, err
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
