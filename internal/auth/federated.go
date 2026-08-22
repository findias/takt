package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Вход через корпоративный провайдер: как узнать вернувшегося и что
// делать с пришедшим впервые.
//
// Порядок опознания важен и выбран не случайно.
//
// 1. По паре «издатель + sub». Это единственный неизменный признак:
//    почта у человека меняется, а идентификатор у провайдера — нет.
// 2. По подтверждённой почте — один раз, чтобы связать уже заведённую
//    учётную запись с её владельцем, пришедшим теперь через провайдера.
//    Только подтверждённой: провайдер, разрешающий вписать чужой адрес
//    без проверки, иначе отдал бы чужую учётную запись любому желающему.
// 3. Завести новую.

var ErrUnverifiedEmail = errors.New("провайдер не подтвердил почту")

// FederatedLogin находит или заводит человека по ответу провайдера
// и, если он пришёл впервые, зачисляет его в указанную организацию.
//
// Уже состоящий хоть в одной организации никуда не добавляется: вход
// не должен молча менять принадлежность. Иначе первый же сотрудник
// подрядчика, вошедший через общий провайдер, оказался бы участником
// организации заказчика.
func FederatedLogin(ctx context.Context, pool *pgxpool.Pool,
	issuer, subject, email string, emailVerified bool,
	name, orgSlug, role string) (Identity, error) {

	if subject == "" || issuer == "" {
		return Identity{}, errors.New("провайдер не назвал, кто пришёл")
	}
	// Подтверждённость спрашивается здесь, а не только у того, кто вызвал:
	// правило записано в этом файле, и обещание, живущее в чужом
	// обработчике, второй вызывающий унаследует уже без него.
	if email != "" && !emailVerified {
		return Identity{}, ErrUnverifiedEmail
	}
	if name == "" {
		name = email
	}
	if name == "" {
		name = "Без имени"
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var u Identity
	err = tx.QueryRow(ctx, `
		select id, email, name from users
		 where oidc_issuer = $1 and oidc_subject = $2`, issuer, subject).
		Scan(&u.ID, &u.Email, &u.Name)

	switch {
	case err == nil:
		// Вернувшийся. Почта и имя у провайдера — источник истины: там их
		// и меняют, а расхождение выглядит как «система показывает старое».
		if u.Email != email && email != "" {
			if _, err := tx.Exec(ctx,
				`update users set email = $2, name = $3 where id = $1`, u.ID, email, name); err != nil {
				return Identity{}, err
			}
			u.Email, u.Name = email, name
		}

	case errors.Is(err, pgx.ErrNoRows):
		if email == "" {
			return Identity{}, errors.New("провайдер не назвал почту")
		}
		// Связывание с уже заведённой учётной записью — один раз и только
		// по подтверждённой почте.
		err = tx.QueryRow(ctx, `
			update users set oidc_issuer = $1, oidc_subject = $2
			 where lower(email) = lower($3) and oidc_subject is null
			 returning id, email, name`, issuer, subject, email).
			Scan(&u.ID, &u.Email, &u.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			// Никого не нашлось — заводим.
			//
			// Пароль ставится случайный и никому не сообщается: у записи
			// он есть только потому, что колонка обязательная. Войти им
			// нельзя — угадать 32 случайных байта не выйдет, — и это
			// правильно: человек с корпоративным входом должен ходить
			// через провайдера, где ему и отключат доступ при увольнении.
			hash, herr := HashPassword(unguessable())
			if herr != nil {
				return Identity{}, herr
			}
			err = tx.QueryRow(ctx, `
				insert into users (email, name, password_hash, oidc_issuer, oidc_subject)
				values ($1, $2, $3, $4, $5)
				returning id, email, name`,
				email, name, hash, issuer, subject).Scan(&u.ID, &u.Email, &u.Name)
		}
		if err != nil {
			return Identity{}, err
		}

	default:
		return Identity{}, err
	}

	// Зачисление — только для тех, кто нигде не состоит.
	var member bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from memberships where user_id = $1)`, u.ID).Scan(&member); err != nil {
		return Identity{}, err
	}
	if !member {
		var orgID string
		err := tx.QueryRow(ctx, `select id from orgs where slug = $1`, orgSlug).Scan(&orgID)
		if errors.Is(err, pgx.ErrNoRows) {
			// Отказ, а не тихое создание организации: организация, возникшая
			// из опечатки в настройке, обнаружится через месяц и с людьми
			// внутри.
			return Identity{}, fmt.Errorf("организация %q не найдена", orgSlug)
		}
		if err != nil {
			return Identity{}, err
		}
		// Область выставляется перед зачислением — как при регистрации
		// и при приёме приглашения, и по той же причине: журнал ведёт
		// база, подпись она берёт из app_current_user(), а транзакция
		// эта открыта без области — до ответа провайдера ни арендатор,
		// ни человек не были известны. Без этого появление человека
		// в организации попадало в журнал без подписи, хотя подписать
		// его есть кем: он сам и пришёл. Заодно это делало неправдой
		// сказанное на экране «Команда»: «без подписи» значит там
		// «сделанное не человеком».
		if _, err := tx.Exec(ctx, `
			select set_config('app.current_org', $1, true),
			       set_config('app.current_user', $2, true)`,
			orgID, u.ID); err != nil {
			return Identity{}, err
		}
		if _, err := tx.Exec(ctx,
			`insert into memberships (org_id, user_id, role) values ($1, $2, $3)
			 on conflict (org_id, user_id) do nothing`, orgID, u.ID, role); err != nil {
			return Identity{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Identity{}, err
	}
	return u, nil
}

func unguessable() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Без источника случайности лучше отдать заведомо непригодную
		// строку, чем предсказуемую: войти всё равно нельзя ни тем,
		// ни другим, но предсказуемая однажды окажется угаданной.
		return strings.Repeat("\x00", 64)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
