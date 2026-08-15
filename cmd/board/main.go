// Команда board — единственный исполняемый файл проекта.
//
// Один образ, две подкоманды:
//
//	board serve     — HTTP API, WebSocket, фоновые задачи (по умолчанию)
//	board migrate   — применить миграции и выйти
//
// Миграции вынесены в отдельную подкоманду намеренно. Запуск их при старте
// приложения работает на одной реплике и разносит базу на двух: обе стартуют
// одновременно и мигрируют параллельно. В compose это отдельный сервис,
// в Kubernetes — Job с хуком pre-upgrade.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/konkov/agile/internal/config"
	"github.com/konkov/agile/internal/httpapi"
	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/webhook"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		log.Error("некорректная конфигурация", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("не удалось открыть пул соединений", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.WaitReady(ctx, 60*time.Second); err != nil {
		log.Error("база недоступна", "err", err)
		os.Exit(1)
	}

	switch command {
	case "migrate":
		applied, err := db.Migrate(ctx)
		if err != nil {
			log.Error("миграции не применились", "err", err)
			os.Exit(1)
		}
		if len(applied) == 0 {
			log.Info("миграции: всё уже применено")
		} else {
			log.Info("миграции применены", "файлы", applied)
		}

	case "serve":
		serve(ctx, cfg, db, log)

	default:
		log.Error("неизвестная команда", "команда", command, "доступные", []string{"serve", "migrate"})
		os.Exit(1)
	}
}

func serve(ctx context.Context, cfg config.Config, db *store.Store, log *slog.Logger) {
	if err := checkMigrated(ctx, db, log); err != nil {
		log.Error("схема базы не готова", "err", err)
		os.Exit(1)
	}
	// Изоляция организаций держится на политиках базы. Если роль подключения
	// их обходит, лучше не запуститься, чем работать без изоляции.
	if err := db.EnsureTenantIsolation(ctx); err != nil {
		log.Error("небезопасная роль подключения", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.New(cfg, db, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Долгий таймаут записи нужен будущим WebSocket-соединениям;
		// обычные запросы ограничены таймаутом чтения заголовков.
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Работник разбирает исходящий ящик вебхуков. Живёт в том же процессе:
	// у нас один образ на установку, и отдельный демон ради одной очереди
	// стоил бы дороже, чем стоит.
	go webhook.NewWorker(db, log).Run(ctx)

	go func() {
		log.Info("сервер запущен", "адрес", cfg.ListenAddr, "baseURL", cfg.BaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("сервер остановился с ошибкой", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("останавливаюсь, даю соединениям закрыться")

	// Запас на корректное закрытие: клиенты должны успеть переподключиться
	// к другой реплике, не потеряв событий.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("не удалось остановиться мягко", "err", err)
	}
}

// checkMigrated не даёт приложению стартовать на непроинициализированной базе:
// понятная ошибка при запуске лучше, чем ошибки в каждом запросе.
func checkMigrated(ctx context.Context, db *store.Store, log *slog.Logger) error {
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`select exists (select 1 from information_schema.tables
		                 where table_schema = 'public' and table_name = 'boards')`).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("таблицы не созданы, выполните `board migrate`")
	}
	log.Info("схема базы на месте")
	return nil
}
