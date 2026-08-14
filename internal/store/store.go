// Package store — доступ к PostgreSQL и применение миграций.
//
// PostgreSQL здесь единственное хранилище: данные, полнотекстовый поиск,
// очередь фоновых задач и рассылка событий между репликами через
// LISTEN/NOTIFY. Отдельные Redis, RabbitMQ и поисковый кластер на масштабе
// в сотню пользователей добавляют эксплуатацию, но не возможности.
package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/konkov/agile/migrations"
)

type Store struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("разбор DATABASE_URL: %w", err)
	}
	// Пула соединений в приложении достаточно: внешний PgBouncer на этом
	// масштабе не нужен, а его transaction-режим ещё и несовместим
	// с LISTEN/NOTIFY, на котором держится рассылка событий.
	cfg.MaxConns = 16
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("подключение к базе: %w", err)
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// Scope — область видимости транзакции для политик Row-Level Security.
//
// Изоляция арендаторов держится не на том, что каждый запрос не забыли
// снабдить условием по org_id, а на политиках в самой базе. Они сравнивают
// поля строки с настройками сеанса, которые выставляются здесь.
//
// Пустая область не открывает ничего: политики сравнивают org_id с NULL
// и не пропускают ни одной строки. Забыть выставить область — значит
// увидеть пустую доску, а не чужую.
type Scope struct {
	// OrgID открывает данные одной организации.
	OrgID string
	// InviteToken открывает одну строку приглашения по хешу его токена.
	// Приглашение открывают по секретной ссылке, когда организация ещё
	// неизвестна, а у человека может не быть аккаунта.
	InviteToken string
}

// BeginScope открывает транзакцию с заданной областью видимости.
//
// Настройки транзакционные (третий аргумент set_config = true), поэтому
// соединение возвращается в пул чистым и следующий запрос не унаследует
// чужую область.
func (s *Store) BeginScope(ctx context.Context, scope Scope) (pgx.Tx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		select set_config('app.current_org', $1, true),
		       set_config('app.invite_token', $2, true)`,
		scope.OrgID, scope.InviteToken)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("установка области видимости: %w", err)
	}
	return tx, nil
}

// InScope выполняет fn в транзакции с заданной областью и фиксирует её,
// если fn не вернула ошибку.
func (s *Store) InScope(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	tx, err := s.BeginScope(ctx, scope)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// BeginTenant открывает транзакцию, внутри которой база видит данные только
// одной организации.
func (s *Store) BeginTenant(ctx context.Context, orgID string) (pgx.Tx, error) {
	return s.BeginScope(ctx, Scope{OrgID: orgID})
}

// InTenant выполняет fn в транзакции с областью организации.
func (s *Store) InTenant(ctx context.Context, orgID string, fn func(pgx.Tx) error) error {
	return s.InScope(ctx, Scope{OrgID: orgID}, fn)
}

// WaitReady ждёт, пока база начнёт отвечать. Нужно при первом запуске
// docker compose, когда приложение стартует раньше PostgreSQL.
func (s *Store) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if err := s.Pool.Ping(ctx); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("база не отвечает за %s: %w", timeout, last)
}

// EnsureTenantIsolation проверяет, что роль подключения не обходит
// Row-Level Security.
//
// Для суперпользователя и для роли с атрибутом BYPASSRLS политики не
// применяются вообще — вся изоляция арендаторов при этом молча исчезает.
// Заметить такое по логам невозможно: запросы работают, просто отдают
// лишнее. Поэтому приложение отказывается стартовать, а не надеется
// на аккуратность того, кто настраивал базу.
func (s *Store) EnsureTenantIsolation(ctx context.Context) error {
	var bypasses bool
	err := s.Pool.QueryRow(ctx, `
		select rolsuper or rolbypassrls
		  from pg_roles where rolname = current_user`).Scan(&bypasses)
	if err != nil {
		return fmt.Errorf("проверка прав роли подключения: %w", err)
	}
	if bypasses {
		var role string
		_ = s.Pool.QueryRow(ctx, `select current_user`).Scan(&role)
		return fmt.Errorf(
			"роль %q подключается с правами суперпользователя или BYPASSRLS — "+
				"политики изоляции организаций для неё не действуют; "+
				"заведите отдельную роль без этих прав и укажите её в DATABASE_URL", role)
	}
	return nil
}

// Migrate применяет неприменённые миграции по порядку имён.
//
// Вызывается отдельным шагом (в compose — командой `migrate`, в Kubernetes —
// Job с хуком pre-upgrade), а не при старте приложения. Миграции в entrypoint
// работают на одной реплике и разносят базу на двух, когда обе стартуют
// одновременно.
func (s *Store) Migrate(ctx context.Context) ([]string, error) {
	_, err := s.Pool.Exec(ctx, `
		create table if not exists schema_migrations (
			name       text primary key,
			applied_at timestamptz not null default now()
		)`)
	if err != nil {
		return nil, fmt.Errorf("создание schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("чтение каталога миграций: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var applied []string
	for _, name := range names {
		var exists bool
		err := s.Pool.QueryRow(ctx,
			`select exists (select 1 from schema_migrations where name = $1)`, name).Scan(&exists)
		if err != nil {
			return applied, fmt.Errorf("проверка миграции %s: %w", name, err)
		}
		if exists {
			continue
		}

		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return applied, fmt.Errorf("чтение миграции %s: %w", name, err)
		}

		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return applied, err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("применение миграции %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`insert into schema_migrations (name) values ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("отметка миграции %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("фиксация миграции %s: %w", name, err)
		}
		applied = append(applied, name)
	}
	return applied, nil
}
