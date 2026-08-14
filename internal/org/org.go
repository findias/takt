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
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/store"
)

const InviteTTL = 7 * 24 * time.Hour

var (
	ErrNotFound      = errors.New("не найдено")
	ErrInviteInvalid = errors.New("ссылка недействительна или срок её действия истёк")
	ErrAlreadyMember = errors.New("этот человек уже в команде")
	ErrLastOwner     = errors.New("в организации должен остаться хотя бы один владелец")
)

type Service struct {
	db *store.Store
}

func New(db *store.Store) *Service { return &Service{db: db} }

type Member struct {
	UserID   string    `json:"userId"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

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
func (s *Service) Create(ctx context.Context, name, ownerUserID string) (auth.Membership, error) {
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
		select u.id, u.name, u.email, m.role, m.created_at
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
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetRole меняет роль участника.
func (s *Service) SetRole(ctx context.Context, orgID, userID, role string) error {
	if role != auth.RoleOwner && role != auth.RoleMember && role != auth.RoleViewer {
		return fmt.Errorf("неизвестная роль %q", role)
	}
	return s.db.InScope(ctx, store.Scope{}, func(tx pgx.Tx) error {
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
func (s *Service) Remove(ctx context.Context, orgID, userID string) error {
	return s.db.InScope(ctx, store.Scope{}, func(tx pgx.Tx) error {
		if err := ensureOtherOwnerExists(ctx, tx, orgID, userID); err != nil {
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
	err = s.db.InTenant(ctx, orgID, func(tx pgx.Tx) error {
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
	err := s.db.InTenant(ctx, orgID, func(tx pgx.Tx) error {
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

func (s *Service) RevokeInvite(ctx context.Context, orgID, inviteID string) error {
	return s.db.InTenant(ctx, orgID, func(tx pgx.Tx) error {
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
