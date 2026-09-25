// Команда board — единственный исполняемый файл проекта.
//
// Один образ, шесть подкоманд:
//
//	takt serve     — HTTP API, WebSocket, фоновые задачи (по умолчанию)
//	takt migrate   — применить миграции и выйти
//	takt demo      — наполнить пустую базу данными для работы над видом
//	takt doctor    — проверить, что установка сделана правильно
//	takt import    — перенести пакет переноса (docs/import-package.md)
//	takt version   — какая это версия
//
// Миграции вынесены в отдельную подкоманду намеренно. Запуск их при старте
// приложения работает на одной реплике и разносит базу на двух: обе стартуют
// одновременно и мигрируют параллельно. В compose это отдельный сервис,
// в Kubernetes — Job с хуком pre-upgrade.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/config"
	"github.com/findias/takt/internal/demo"
	"github.com/findias/takt/internal/doctor"
	"github.com/findias/takt/internal/httpapi"
	"github.com/findias/takt/internal/realtime"
	"github.com/findias/takt/internal/retention"
	"github.com/findias/takt/internal/store"
	"github.com/findias/takt/internal/version"
	"github.com/findias/takt/internal/webhook"
)

func main() {
	// Журнал — JSON, и это держит подделку строк журнала: перевод строки
	// в пути запроса или в тексте ошибки экранируется внутри значения,
	// а не начинает новую запись. Поэтому находки CodeQL go/log-injection
	// закрыты как ложные; обработчик без экранирования вернёт их в силу.
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	// Версия отвечает до всего остального: её спрашивают в том числе
	// тогда, когда база лежит и приложение не поднимается, — а «какая
	// у вас версия» это первый вопрос при разборе любой поломки.
	if command == "version" {
		fmt.Println(version.Строка())
		return
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

	case "demo":
		switch err := demo.Fill(ctx, db); {
		case errors.Is(err, demo.ErrAlreadyFilled):
			log.Info("демонстрационные данные уже есть — оставляю как есть, доливаю новое",
				"обновить целиком", "make stand (сносит базу)")
			// Долив — то, чему наполнение научилось позже: без него новое
			// обещание сверки требовало бы сносить базу стенда и демо.
			if err := demo.TopUp(ctx, db); err != nil {
				log.Error("долив демонстрационных данных не прошёл", "err", err)
				os.Exit(1)
			}
		case err != nil:
			log.Error("демонстрационные данные не завелись", "err", err)
			os.Exit(1)
		default:
			log.Info("демонстрационные данные готовы",
				"организация", demo.OrgName,
				"вход", demo.People[0].Email,
				"пароль", demo.Password)
		}
		// Сверка сразу после наполнения — и при «уже есть» тоже:
		// стенд протухает от сквозных прогонов, а узнают об этом
		// обычно глазами и не сразу.
		if err := demo.Verify(ctx, db); err != nil {
			log.Error("стенд неполон", "err", err)
			os.Exit(1)
		}
		// Английская копия — по ней снимают английские снимки для README
		// и документации (ROADMAP 30.5).
		switch err := demo.FillEnglish(ctx, db); {
		case errors.Is(err, demo.ErrAlreadyFilled):
			if err := demo.TopUpEnglish(ctx, db); err != nil {
				log.Error("долив английских демонстрационных данных не прошёл", "err", err)
				os.Exit(1)
			}
		case err != nil:
			log.Error("английские демонстрационные данные не завелись", "err", err)
			os.Exit(1)
		default:
			log.Info("английская организация стенда готова",
				"вход", demo.EnglishEmail(demo.People[0]), "пароль", demo.Password)
		}
		if err := demo.VerifyEnglish(ctx, db); err != nil {
			log.Error("английский стенд неполон", "err", err)
			os.Exit(1)
		}
		log.Info("стенд сверен с обещанным")

	case "serve":
		serve(ctx, cfg, db, log)

	case "import":
		// Отчёт — человеку в терминал, а не журналом: это разговор
		// с администратором, а не запись о работе сервера.
		if err := runImport(ctx, db, os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "takt import:", err)
			os.Exit(1)
		}

	case "doctor":
		// Отдельной командой, а не проверкой при старте: спрашивают её
		// после установки и после обновления, а ответ нужен списком —
		// что смотрели, чем кончилось и что делать. Стартующее
		// приложение вместо этого либо работает, либо не работает.
		//
		// Вывод человеку, а не журналу: читает его тот, кто ставил,
		// и читает один раз.
		fmt.Printf("версия: %s\n\n", version.Строка())
		итоги := doctor.Осмотр(ctx, cfg, db)
		for _, и := range итоги {
			знак := "✓"
			if !и.Ладно {
				знак = "✗"
			}
			fmt.Printf("%s %s: %s\n", знак, и.Что, и.Ответ)
			if и.Совет != "" {
				// У неудачи совет — что делать, чтобы заработало.
				// У удачи — замечание: так поставить можно, но знать
				// об этом надо. Одна подпись на оба случая делала бы
				// из замечания требование, а из требования — совет.
				подпись := "что делать"
				if и.Ладно {
					подпись = "стоит знать"
				}
				fmt.Printf("   %s: %s\n", подпись, и.Совет)
			}
		}
		if !doctor.Ладно(итоги) {
			fmt.Println("\nустановка неполна — см. отметки ✗ выше")
			os.Exit(1)
		}
		fmt.Println("\nустановка выглядит рабочей")

	default:
		log.Error("неизвестная команда", "команда", command,
			"доступные", []string{"serve", "migrate", "demo", "doctor", "version"})
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

	// Одно слушающее соединение на процесс раздаёт изменения всем открытым
	// доскам: подписка на канал в базе стоит соединения, и держать его
	// по одному на вкладку нельзя.
	hub := realtime.NewHub(db, log)
	go hub.Run(ctx)

	api := httpapi.New(cfg, db, log, hub)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Долгий таймаут записи нужен будущим WebSocket-соединениям;
		// обычные запросы ограничены таймаутом чтения заголовков.
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Работник разбирает исходящий ящик вебхуков. Живёт в том же процессе:
	// у нас один образ на установку, и отдельный демон ради одной очереди
	// стоил бы дороже, чем стоит.
	//
	// В публичном демо не запускается: доставка ходила бы по адресам,
	// которые вписал прохожий (подписки там и не заводятся — см.
	// handleCreateWebhook, но у демо-данных одна своя есть). На тестовом
	// стенде — по той же причине: пароль входа там напечатан в самой
	// заметке стенда, и войти может любой.
	switch {
	case cfg.Demo:
		go sweepSandboxes(ctx, db, log)
	case !cfg.Stand:
		go webhook.NewWorker(db, log).Run(ctx)
	}

	// Уборщик: служебные таблицы растут без предела, и ключи повтора
	// недельной давности повторять уже некому.
	go retention.NewWorker(db, log).Run(ctx)

	// Блокировки со сроком снимаются сами. Первый проход — сразу:
	// перезапуск не должен проглотить сроки, вышедшие, пока сервер
	// стоял.
	go expireBlocks(ctx, board.New(db), log)

	go func() {
		log.Info("сервер запущен", "адрес", cfg.ListenAddr, "baseURL", cfg.BaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("сервер остановился с ошибкой", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	// Сперва сказать «мне больше не давайте», потом подождать, и только
	// потом закрываться. Балансировщик узнаёт о неготовности не мгновенно;
	// реплика, переставшая отвечать раньше, чем её вычеркнули из списка,
	// выглядит у пользователя как пятисотые на ровном месте. Пауза здесь
	// стоит несколько секунд простоя при выкладке и экономит их же
	// в виде ошибок.
	api.Drain()
	log.Info("останавливаюсь: снят с балансировки, жду вычёркивания", "пауза", drainPause)
	select {
	case <-time.After(drainPause):
	case <-hardStop():
	}

	log.Info("закрываю соединения")

	// Запас на корректное закрытие: клиенты должны успеть переподключиться
	// к другой реплике, не потеряв событий.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("не удалось остановиться мягко", "err", err)
	}
}

// drainPause — сколько ждать между «я не готов» и закрытием соединений.
// Пять секунд покрывают обычное время обновления списка адресов в
// Kubernetes; terminationGracePeriodSeconds в чарте больше этой паузы
// вместе с таймаутом остановки, иначе процесс убьют посреди неё.
const drainPause = 5 * time.Second

// hardStop — второй сигнал означает «хватит ждать». Тот, кто нажал Ctrl+C
// дважды, просит остановиться сейчас, и заставлять его ждать паузу
// на балансировку, которой на его машине нет, незачем.
func hardStop() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	return ch
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
		return errors.New("таблицы не созданы, выполните `takt migrate`")
	}
	log.Info("схема базы на месте")
	return nil
}

// expireBlocks снимает блокировки с вышедшим сроком и закрывает
// итерации с прошедшим концом — сразу и дальше раз в минуту. Сбой
// прохода не останавливает сервер: следующий проход подберёт то же
// самое, сроки от этого не денутся.
func expireBlocks(ctx context.Context, boards *board.Service, log *slog.Logger) {
	tick := time.NewTicker(board.ExpireEvery)
	defer tick.Stop()
	var lastDue time.Time
	for {
		n, err := boards.ExpireBlocks(ctx)
		if err != nil && ctx.Err() == nil {
			log.Error("снятие блокировок по сроку", "err", err)
		}
		if n > 0 {
			log.Info("блокировки сняты по сроку", "сколько", n)
		}
		// Тем же циклом — итерации, чей последний день прошёл: момент
		// закрытия ставится полуночью, так что минута опоздания прохода
		// на отчёт не влияет.
		if n, err := boards.CloseDueIterations(ctx); err != nil && ctx.Err() == nil {
			log.Error("закрытие итераций по календарю", "err", err)
		} else if n > 0 {
			log.Info("итерации закрыты по календарю", "сколько", n)
		}
		// Тем же циклом — уведомления по времени: срок блокировки ближе
		// суток, карточка перешагнула обещание. Реже: проход обходит все
		// организации, а десять минут опоздания здесь ничего не меняют.
		if time.Since(lastDue) >= board.DueEvery {
			lastDue = time.Now()
			if due, err := boards.NotifyDue(ctx); err != nil && ctx.Err() == nil {
				log.Error("уведомления по времени", "err", err)
			} else if due > 0 {
				log.Info("уведомления по времени", "сколько", due)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// sweepSandboxes убирает песочницы демо с вышедшим сроком. Чаще уборщика
// служебных таблиц: срок песочницы обещан на экране, и держать её
// лишний час значит держать место в маленькой бесплатной базе.
// Засыпание площадки уборке не мешает: первый проход — при старте.
func sweepSandboxes(ctx context.Context, db *store.Store, log *slog.Logger) {
	for {
		if n, err := demo.SweepSandboxes(ctx, db); err != nil && ctx.Err() == nil {
			log.Error("уборка песочниц", "err", err)
		} else if n > 0 {
			log.Info("песочницы убраны", "сколько", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}
