package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/findias/takt/internal/store"
)

// Два `takt migrate` разом не мешают друг другу (PROMPT-TESTING.md,
// уровень 2).
//
// Миграции катит отдельная команда — задача Helm или сервис compose,
// — и в обычном ходе она одна. Но задача, перезапущенная кластером,
// пока прежняя ещё идёт, или `takt migrate`, запущенный руками во время
// выкладки, дают два прохода по одной базе. Каждая миграция идёт в своей
// транзакции, и полусхемы не остаётся; зато второй проход, видевший ту
// же миграцию неприменённой, падает на уже созданном объекте — выкладка
// краснеет без всякой поломки.
func TestTwoMigrationRunsAtOnceBothSucceed(t *testing.T) {
	ctx := context.Background()
	url := freshDatabase(t)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range errs {
		db, err := store.Open(ctx, url)
		if err != nil {
			t.Fatalf("подключение %d: %v", i, err)
		}
		t.Cleanup(db.Close)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = db.Migrate(ctx)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("проход %d упал: %v", i+1, err)
		}
	}

	check, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var applied int
	if err := check.Pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if want := countMigrations(t); applied != want {
		t.Errorf("применено %d миграций из %d", applied, want)
	}
}
