// Package apiclient — сервисные клиенты: доступ к API не от человека.
//
// Токен принадлежит организации, а не человеку: интеграция живёт дольше
// того, кто её завёл, и увольнение сотрудника не должно ронять обмен
// с соседней системой.
//
// У клиента есть собственная личность в users, и через неё он состоит
// в организации ровно как человек. Так все политики доступа работают без
// второй ветки «а если это не человек», и в журнале действий у токена
// оказывается имя, а не пустота.
package apiclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/store"
)

var (
	ErrNotFound = errors.New("клиент не найден")
	// ErrInvalidToken — предъявленный токен не открывает ничего: его нет,
	// он отозван или истёк. Причину наружу не называем: подсказывать,
	// что ключ «почти правильный», незачем.
	ErrInvalidToken = errors.New("ключ недействителен")
	ErrExists       = errors.New("клиент с таким названием уже есть")
)

// Разрешения. Список закрытый и короткий: набор прав, придуманный
// умозрительно, всегда оказывается неверным, а сузить выданное разрешение
// потом нельзя — только отозвать ключ целиком.
const (
	ScopeBoardsRead    = "boards:read"
	ScopeBoardsWrite   = "boards:write"
	ScopeStructureRead = "structure:read"
	ScopeAuditRead     = "audit:read"
	// ScopeSCIM — заведение и отключение людей и групп провайдером.
	// Отдельное разрешение, потому что это единственный ключ, которому
	// нужны права владельца: заводить людей и команды — его действие.
	ScopeSCIM = "scim:write"
)

func KnownScope(scope string) bool {
	switch scope {
	case ScopeBoardsRead, ScopeBoardsWrite, ScopeStructureRead, ScopeAuditRead, ScopeSCIM:
		return true
	}
	return false
}

type Client struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Prefix string   `json:"prefix"`
	Scopes []string `json:"scopes"`
	// Token приходит только в ответе на создание: в базе лежит лишь хеш,
	// и показать значение повторно невозможно.
	Token      string     `json:"token,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

func (s *Service) List(ctx context.Context, orgID, userID string) ([]Client, error) {
	out := []Client{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, name, prefix, scopes, created_at, expires_at, last_used_at
			  from api_clients
			 where revoked_at is null
			 order by created_at desc`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Client
			if err := rows.Scan(&c.ID, &c.Name, &c.Prefix, &c.Scopes,
				&c.CreatedAt, &c.ExpiresAt, &c.LastUsedAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// Create заводит клиента вместе с его служебной личностью и возвращает
// токен — единственный раз, когда он вообще существует в открытом виде.
func (s *Service) Create(ctx context.Context, orgID, actorID, name string, scopes []string, expiresAt *time.Time) (Client, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Client{}, fmt.Errorf("у клиента должно быть название")
	}

	cleaned := []string{}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if !KnownScope(scope) {
			return Client{}, fmt.Errorf("неизвестное разрешение %q", scope)
		}
		if !seen[scope] {
			seen[scope] = true
			cleaned = append(cleaned, scope)
		}
	}
	if len(cleaned) == 0 {
		return Client{}, fmt.Errorf("у клиента должно быть хотя бы одно разрешение")
	}

	token, err := newToken()
	if err != nil {
		return Client{}, err
	}

	// Роль служебной личности выводится из разрешений, а не задаётся
	// отдельно: два источника правды о том, что клиенту можно, разошлись
	// бы в первый же день.
	role := auth.RoleViewer
	if seen[ScopeBoardsWrite] {
		role = auth.RoleMember
	}
	// Заведение людей и команд — действие владельца, и политики базы
	// требуют именно этого. Ключ от этого не становится всесильным:
	// на доски он не пройдёт, там проверяется разрешение, а его у него
	// нет.
	if seen[ScopeSCIM] {
		role = auth.RoleOwner
	}

	c := Client{Name: name, Scopes: cleaned, Token: token, Prefix: token[:8], ExpiresAt: expiresAt}
	err = s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		// Личность клиента не может войти по паролю: хеш заведомо
		// непригоден, а сессий у неё не бывает.
		var botID string
		if err := tx.QueryRow(ctx, `
			insert into users (email, name, password_hash, kind)
			values ($1, $2, '!', 'service') returning id`,
			"client-"+hex.EncodeToString([]byte(c.Prefix))+"@clients.invalid",
			name).Scan(&botID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`insert into memberships (org_id, user_id, role) values ($1, $2, $3)`,
			orgID, botID, role); err != nil {
			return err
		}

		return tx.QueryRow(ctx, `
			insert into api_clients
				(org_id, name, user_id, token_hash, prefix, scopes, created_by, expires_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8)
			returning id, created_at`,
			orgID, name, botID, hashToken(token), c.Prefix, cleaned, actorID, expiresAt).
			Scan(&c.ID, &c.CreatedAt)
	})
	if err != nil {
		if strings.Contains(err.Error(), "api_clients_name_idx") {
			return Client{}, ErrExists
		}
		return Client{}, err
	}
	return c, nil
}

// Revoke отзывает ключ и исключает служебную личность из организации.
// Личность остаётся в базе: на неё ссылается журнал действий, и вырезав
// её, мы потеряли бы автора у всего, что клиент успел сделать.
func (s *Service) Revoke(ctx context.Context, orgID, actorID, clientID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var botID string
		err := tx.QueryRow(ctx, `
			update api_clients set revoked_at = now()
			 where id = $1 and revoked_at is null
			 returning user_id`, clientID).Scan(&botID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`delete from memberships where org_id = $1 and user_id = $2`, orgID, botID)
		return err
	})
}

// Authenticate превращает предъявленный токен в действующее лицо.
//
// Область открывается самим токеном: организация до этого неизвестна —
// то же исключение, что и у приглашения, и по той же причине.
func (s *Service) Authenticate(ctx context.Context, token string) (auth.Principal, []string, error) {
	var p auth.Principal
	var scopes []string

	err := s.db.InScope(ctx, store.Scope{APIToken: hashToken(token)}, func(tx pgx.Tx) error {
		var clientID string
		err := tx.QueryRow(ctx, `
			select id, org_id, user_id, scopes
			  from api_clients
			 where revoked_at is null
			   and (expires_at is null or expires_at > now())`).
			Scan(&clientID, &p.OrgID, &p.ID, &scopes)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}

		// Отметка о последнем использовании: по ней видно, живёт ли
		// интеграция, и её же смотрят, решая, можно ли отозвать ключ.
		_, err = tx.Exec(ctx,
			`update api_clients set last_used_at = now() where id = $1`, clientID)
		return err
	})
	if err != nil {
		return auth.Principal{}, nil, err
	}

	// Роль и имя читаются от лица самого клиента: он состоит
	// в организации как все, и никакого особого пути здесь нет.
	err = s.db.InTenant(ctx, p.OrgID, p.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select u.email, u.name, o.name, o.slug, m.role, o.estimate_unit
			  from memberships m
			  join users u on u.id = m.user_id
			  join orgs o on o.id = m.org_id
			 where m.org_id = $1 and m.user_id = $2`, p.OrgID, p.ID).
			Scan(&p.Email, &p.Name, &p.OrgName, &p.OrgSlug, &p.Role, &p.EstimateUnit)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Principal{}, nil, ErrInvalidToken
	}
	return p, scopes, err
}

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
