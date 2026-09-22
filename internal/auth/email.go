package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Смена почты (ROADMAP 23.6). Почта — это имя для входа, поэтому
// меняется она осторожнее имени: с паролем, с проверкой, что адрес
// свободен, и с записью в журнал каждой организации человека — владелец
// должен видеть, что вход его участника теперь зовётся иначе.

var (
	// ErrEmailInvalid — не адрес.
	ErrEmailInvalid = errors.New("это не похоже на почту: нужен адрес вида имя@домен")
	// ErrEmailTaken — адрес уже у другой учётной записи.
	ErrEmailTaken = errors.New("эта почта уже у другой учётной записи")
	// ErrEmailSame — адрес тот же, что сейчас. Отдельным отказом, как
	// и с паролем: «готово» на ничего не изменившее вводит в заблуждение.
	ErrEmailSame = errors.New("это и есть нынешняя почта")
	// ErrEmailManaged — почту ведёт не takt, а корпоративный вход или
	// каталог. Сменённая здесь, она вернулась бы при следующем входе
	// или выгрузке каталога.
	ErrEmailManaged = errors.New(
		"почту ведёт корпоративный вход или каталог организации — её меняют там")
)

// NormalizeEmail приводит адрес к виду, в котором он хранится,
// и отвергает то, что адресом не является.
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	// Имя с угловыми скобками («Анна <a@b>») ParseAddress примет,
	// а хранить надо голый адрес — поэтому и сверка с исходным.
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || !strings.Contains(email[strings.LastIndex(email, "@")+1:], ".") {
		return "", ErrEmailInvalid
	}
	return email, nil
}

// EmailManaged — ведёт ли почту человека кто-то, кроме takt: провайдер
// корпоративного входа или каталог (SCIM) хоть одной его организации.
func EmailManaged(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID string) (bool, error) {
	var managed bool
	err := q.QueryRow(ctx, `
		select u.oidc_subject is not null
		       or exists (select 1 from memberships m
		                   where m.user_id = u.id and m.external_id is not null)
		  from users u where u.id = $1`, userID).Scan(&managed)
	return managed, err
}

// ChangeEmail меняет почту самому себе. Текущий пароль спрашивается
// по той же причине, что при смене пароля: из украденной сессии почту
// сменили бы, и хозяин не вошёл бы больше своим адресом.
//
// Новая почта помечается неподтверждённой (миграция 0061): корпоративный
// вход по ней запись не привяжет.
func ChangeEmail(ctx context.Context, pool *pgxpool.Pool, userID, current, raw string) (string, error) {
	email, err := NormalizeEmail(raw)
	if err != nil {
		return "", err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Подпись в журнале берётся политикой из области транзакции;
	// организации нет — запись идёт в журнал каждой организации человека.
	if _, err := tx.Exec(ctx, `select set_config('app.current_user', $1, true)`, userID); err != nil {
		return "", err
	}

	var hash, was string
	err = tx.QueryRow(ctx,
		`select password_hash, email from users where id = $1 for update`, userID).Scan(&hash, &was)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSession
	}
	if err != nil {
		return "", err
	}
	managed, err := EmailManaged(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	if managed {
		return "", ErrEmailManaged
	}
	if !CheckPassword(hash, current) {
		return "", ErrWrongPassword
	}
	if strings.EqualFold(was, email) {
		return "", ErrEmailSame
	}
	if err := SetEmail(ctx, tx, userID, was, email, true); err != nil {
		return "", err
	}
	return email, tx.Commit(ctx)
}

// SetEmail пишет новую почту и оставляет след в журнале. Общая часть
// смены самим человеком и владельцем; проверки прав — у вызывающих.
// Снимок — в том же виде old/new, что пишут триггеры журнала: экран
// «Команда» назовёт его «почта: было → стало» без особого случая.
// В журнал — каждой организации, где человек состоит: при смене
// владельцем она одна, при смене самим — все.
func SetEmail(ctx context.Context, tx pgx.Tx, userID, was, email string, unconfirmed bool) error {
	_, err := tx.Exec(ctx,
		`update users set email = $2, email_unconfirmed = $3 where id = $1`, userID, email, unconfirmed)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrEmailTaken
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
		select m.org_id, (select app_current_user()), 'update', 'users', $1,
		       jsonb_build_object(
		           'old', jsonb_build_object('name', u.name, 'email', $2::text),
		           'new', jsonb_build_object('name', u.name, 'email', $3::text))
		  from memberships m join users u on u.id = m.user_id
		 where m.user_id = $1
		   and m.org_id = coalesce((select app_current_org()), m.org_id)`, userID, was, email)
	return err
}
