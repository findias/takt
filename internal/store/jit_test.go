package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/findias/takt/internal/store"
)

// JIT коротких RLS-запросов занимал сотни миллисекунд и забивал пул.
// Важно проверить и возврат настройки: приложение делит PostgreSQL
// с другими клиентами, а соединение из пула переживает транзакцию.
func TestScopeDisablesJITWithoutLeakingTheSetting(t *testing.T) {
	ctx := context.Background()
	cfg := isolationStore(t).Pool.Config().Copy()
	cfg.MaxConns, cfg.MinConns = 1, 0
	cfg.ConnConfig.RuntimeParams["jit"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := &store.Store{Pool: pool}
	for _, commit := range []bool{true, false} {
		name := "rollback"
		if commit {
			name = "commit"
		}
		t.Run(name, func(t *testing.T) {
			tx, err := db.BeginScope(ctx, store.Scope{})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			var jit string
			if err := tx.QueryRow(ctx, `show jit`).Scan(&jit); err != nil {
				t.Fatal(err)
			}
			if jit != "off" {
				t.Errorf("короткие запросы области исполняются с jit=%s", jit)
			}
			if commit {
				err = tx.Commit(ctx)
			} else {
				err = tx.Rollback(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `show jit`).Scan(&jit); err != nil {
				t.Fatal(err)
			}
			if jit != "on" {
				t.Errorf("после транзакции в соединении осталось jit=%s", jit)
			}
		})
	}
}
