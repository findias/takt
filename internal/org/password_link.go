package org

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/auth"
)

// Ссылка «задать пароль» (ROADMAP 23.6, миграция 0063).
//
// Писем takt не шлёт, и ни сброса пароля по почте, ни письма «вас
// завели» нет. Владелец выпускает одноразовую ссылку и передаёт её сам,
// как приглашение. Кто её открыл, задаёт пароль и сразу входит.

// PasswordLinkTTL — сколько живёт ссылка. Как у приглашения: неделя —
// столько нужно, чтобы передать ссылку человеку в отпуске.
const PasswordLinkTTL = 7 * 24 * time.Hour

var (
	// ErrPasswordLinkInvalid — ссылки нет, она использована или истекла.
	// Одним отказом на все три случая: различать их значит подсказывать
	// перебирающему, какие токены когда-то существовали.
	ErrPasswordLinkInvalid = errors.New(
		"ссылка недействительна: её уже использовали или срок истёк — попросите у администратора новую")
	// ErrPasswordLinkOwn — владелец выпускает ссылку себе. Свой пароль
	// меняют в профиле, зная текущий.
	ErrPasswordLinkOwn = errors.New("свой пароль меняют в профиле — ссылка для этого не нужна")
	// ErrPasswordLinkElsewhere — человек состоит и в других организациях:
	// пароль один на все, и распоряжаться им из одной нельзя.
	ErrPasswordLinkElsewhere = errors.New(
		"этот человек состоит и в других организациях — пароль он меняет сам, в своём профиле")
	// ErrPasswordLinkFederated — пароля у человека нет и быть не должно:
	// он входит через провайдера, и доступ ему закрывают там.
	ErrPasswordLinkFederated = errors.New(
		"этот человек входит через корпоративный вход или каталог — пароль ему не нужен")
)

// PasswordLink — выпущенная ссылка. Показывается один раз: в базе лежит
// только отпечаток токена.
type PasswordLink struct {
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// PasswordLinkInfo — что показать открывшему ссылку.
type PasswordLinkInfo struct {
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	OrgName   string    `json:"orgName"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// IssuePasswordLink выпускает ссылку участнику. Прежние неиспользованные
// ссылки этого человека гаснут: действующая ссылка одна, иначе потерянная
// первая так и лежала бы где-то в переписке живым входом.
//
// Права те же, что у смены почты владельцем: только тому, кто состоит
// лишь здесь, и чей вход не ведёт провайдер или каталог. Выпуск ссылки
// — это смена пароля чужими руками.
func (s *Service) IssuePasswordLink(ctx context.Context, orgID, actorID, userID, baseURL string) (PasswordLink, error) {
	if userID == actorID {
		return PasswordLink{}, ErrPasswordLinkOwn
	}
	token, err := newToken()
	if err != nil {
		return PasswordLink{}, err
	}
	out := PasswordLink{
		Link:      strings.TrimRight(baseURL, "/") + "/password/" + token,
		ExpiresAt: time.Now().Add(PasswordLinkTTL).UTC().Truncate(time.Second),
	}
	err = s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		var exists bool
		err := tx.QueryRow(ctx, `
			select true from users u
			  join memberships m on m.user_id = u.id and m.org_id = $1
			 where u.id = $2 and u.anonymized_at is null
			   for update of u`, orgID, userID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := ensureNotService(ctx, tx, userID); err != nil {
			return err
		}
		var elsewhere bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from memberships where user_id = $1 and org_id <> $2)`,
			userID, orgID).Scan(&elsewhere); err != nil {
			return err
		}
		if elsewhere {
			return ErrPasswordLinkElsewhere
		}
		managed, err := auth.EmailManaged(ctx, tx, userID)
		if err != nil {
			return err
		}
		if managed {
			return ErrPasswordLinkFederated
		}

		if _, err := tx.Exec(ctx,
			`delete from password_links where user_id = $1 and used_at is null`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			insert into password_links (token_hash, user_id, org_id, created_by, expires_at)
			values ($1, $2, $3, $4, $5)`,
			hashToken(token), userID, orgID, actorID, out.ExpiresAt); err != nil {
			return err
		}
		// Журнал — кодом, как у обезличивания: у таблицы ссылок нет
		// триггера журнала, а владельцу видеть, кто кому выпускал вход,
		// нужно.
		_, err = tx.Exec(ctx, `
			insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
			values ($1, (select app_current_user()), 'insert', 'password_links', $2,
			        jsonb_build_object('new', jsonb_build_object('user_id', $3::text, 'expires_at', $4::timestamptz)))`,
			orgID, userID, userID, out.ExpiresAt)
		return err
	})
	return out, err
}

// LookupPasswordLink — для кого ссылка. Отказ один на все случаи
// (ErrPasswordLinkInvalid).
func (s *Service) LookupPasswordLink(ctx context.Context, token string) (PasswordLinkInfo, error) {
	var info PasswordLinkInfo
	err := s.db.Pool.QueryRow(ctx, `
		select u.name, u.email, o.name, l.expires_at
		  from password_links l
		  join users u on u.id = l.user_id
		  join orgs  o on o.id = l.org_id
		 where l.token_hash = $1 and l.used_at is null and l.expires_at > now()
		   and u.anonymized_at is null`, hashToken(token)).
		Scan(&info.Name, &info.Email, &info.OrgName, &info.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordLinkInfo{}, ErrPasswordLinkInvalid
	}
	return info, err
}

// UsePasswordLink задаёт пароль по ссылке и гасит её. Возвращает, кто
// вошёл, и организацию, где ссылку выпустили, — туда и откроется сессия.
//
// Прочие сессии человека обрываются, как при смене пароля: ссылку
// выпускают и тому, кто подозревает чужой вход, и оставлять чужие сессии
// жить значило бы сменить замок, не отобрав ключей.
func (s *Service) UsePasswordLink(ctx context.Context, token, password string) (userID, orgID string, err error) {
	if len(password) < auth.MinPasswordLen {
		return "", "", auth.ErrPasswordShort
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", "", err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = tx.QueryRow(ctx, `
		update password_links l set used_at = now()
		  from users u
		 where l.token_hash = $1 and l.used_at is null and l.expires_at > now()
		   and u.id = l.user_id and u.anonymized_at is null
		returning l.user_id, l.org_id`, hashToken(token)).Scan(&userID, &orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrPasswordLinkInvalid
	}
	if err != nil {
		return "", "", err
	}
	// Человек мог за это время уйти из организации или обзавестись
	// корпоративным входом — тогда ссылка больше ни к чему.
	var member bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from memberships where user_id = $1 and org_id = $2)`,
		userID, orgID).Scan(&member); err != nil {
		return "", "", err
	}
	managed, err := auth.EmailManaged(ctx, tx, userID)
	if err != nil {
		return "", "", err
	}
	if !member || managed {
		return "", "", ErrPasswordLinkInvalid
	}

	if _, err := tx.Exec(ctx,
		`update users set password_hash = $2, awaiting_password = false where id = $1`,
		userID, hash); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `delete from sessions where user_id = $1`, userID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `
		select set_config('app.current_org', $1, true),
		       set_config('app.current_user', $2, true)`, orgID, userID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `
		insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
		values ($1, $2, 'update', 'password_links', $2,
		        jsonb_build_object(
		            'old', jsonb_build_object('user_id', $3::text, 'used_at', null),
		            'new', jsonb_build_object('user_id', $3::text, 'used_at', now())))`, orgID, userID, userID); err != nil {
		return "", "", err
	}
	return userID, orgID, tx.Commit(ctx)
}
