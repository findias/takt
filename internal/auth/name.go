package auth

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NameMax — предел длины имени в знаках. Имя стоит в шапке, в подписях
// под работой и в составе: длиннее этого там не поместится ничто.
const NameMax = 100

var (
	ErrNameEmpty   = errors.New("укажите имя")
	ErrNameTooLong = errors.New("имя длиннее 100 знаков — так его никуда не вписать")
	ErrNameSame    = errors.New("это и есть нынешнее имя")
	// ErrNameManaged — имя ведёт корпоративный вход или каталог: провайдер
	// переписывает его при каждом входе (federated.go), каталог — при
	// каждой выгрузке. Сменённое здесь, оно вернулось бы незаметно,
	// и человек решил бы, что правка не сохранилась.
	ErrNameManaged = errors.New(
		"имя ведёт корпоративный вход или каталог организации — его меняют там")
)

// NormalizeName приводит имя к виду, в котором оно хранится: без
// краёв и без двойных пробелов — перевод строки внутри имени сломал бы
// подписи, где оно стоит в одну строку.
func NormalizeName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	switch {
	case name == "":
		return "", ErrNameEmpty
	case utf8.RuneCountInString(name) > NameMax:
		return "", ErrNameTooLong
	}
	return name, nil
}

// ChangeName меняет имя самому себе (ROADMAP 36.2). Пароль не
// спрашивается: имя — не имя для входа, из украденной сессии им
// ничего не отнять. Запись в журнал — каждой организации человека.
func ChangeName(ctx context.Context, pool *pgxpool.Pool, userID, raw string) (string, error) {
	name, err := NormalizeName(raw)
	if err != nil {
		return "", err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select set_config('app.current_user', $1, true)`, userID); err != nil {
		return "", err
	}
	var was string
	err = tx.QueryRow(ctx, `select name from users where id = $1 for update`, userID).Scan(&was)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSession
	}
	if err != nil {
		return "", err
	}
	// Признак тот же, что у почты: провайдер и каталог ведут обоих.
	managed, err := EmailManaged(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	if managed {
		return "", ErrNameManaged
	}
	if was == name {
		return "", ErrNameSame
	}
	if err := SetName(ctx, tx, userID, was, name); err != nil {
		return "", err
	}
	return name, tx.Commit(ctx)
}

// SetName пишет новое имя и оставляет след в журнале — в том же виде
// old/new, что пишут триггеры и SetEmail: экран «Команда» назовёт его
// «имя: было → стало» без особого случая. Проверки прав — у вызывающих.
func SetName(ctx context.Context, tx pgx.Tx, userID, was, name string) error {
	if _, err := tx.Exec(ctx, `update users set name = $2 where id = $1`, userID, name); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		insert into audit_events (org_id, actor_id, action, subject, subject_id, payload)
		select m.org_id, (select app_current_user()), 'update', 'users', $1,
		       jsonb_build_object(
		           'old', jsonb_build_object('name', $2::text, 'email', u.email),
		           'new', jsonb_build_object('name', $3::text, 'email', u.email))
		  from memberships m join users u on u.id = m.user_id
		 where m.user_id = $1
		   and m.org_id = coalesce((select app_current_org()), m.org_id)`, userID, was, name)
	return err
}
