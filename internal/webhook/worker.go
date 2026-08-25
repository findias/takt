package webhook

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/store"
)

// Работник разбирает исходящий ящик.
//
// Очередь — таблица, а не брокер: `for update skip locked` даёт ровно то,
// что здесь нужно, а лишняя движущаяся часть дороже в эксплуатации, чем
// один запрос раз в секунду. Несколько работников на одну очередь при
// этом не мешают друг другу: занятые строки пропускаются.

const (
	// Пауза между заходами в пустую очередь. Секунда — компромисс между
	// «доставка почти сразу» и «не будить базу зря».
	idleDelay = time.Second
	// Сколько доставок берём за раз. Больше не нужно: медленный получатель
	// всё равно упирается в собственный ответ, а не в нашу пропускную
	// способность.
	batchSize = 10
	// Сколько ждём ответа. Получатель, думающий дольше, — уже поломка,
	// и держать ради него соединение незачем.
	sendTimeout = 10 * time.Second
	// После скольких неудач сдаёмся. Восемь попыток с удвоением — это
	// около двух часов: если за два часа получатель не ожил, он не оживёт
	// и на девятой.
	maxAttempts = 8
	// Первая пауза перед повтором; дальше удвоение до потолка.
	firstDelay = 30 * time.Second
	maxDelay   = time.Hour
)

// Policy — границы того, что доставка делает сама.
//
// Отдаётся интерфейсу и берётся отсюда, а не переписывается там числами:
// «повторяем, удваивая паузу» — единственное, что было сказано человеку,
// и из этого нельзя узнать ни сколько раз мы повторим, ни что после
// последней неудачи подписку отключим совсем. Автономное поведение
// обязано быть названо целиком: что система делает сама, где
// останавливается и чего не сделает никогда.
//
// Второй набор этих же чисел в клиенте разошёлся бы с первым — так уже
// было со списком событий, который в интерфейсе был вчетверо короче
// доставляемого.
type Policy struct {
	// Сколько раз пытаемся, прежде чем сдаться.
	Attempts int `json:"attempts"`
	// Пауза перед первым повтором и потолок, до которого она удваивается.
	FirstDelaySeconds int `json:"firstDelaySeconds"`
	MaxDelaySeconds   int `json:"maxDelaySeconds"`
	// Сколько ждём ответа получателя.
	TimeoutSeconds int `json:"timeoutSeconds"`
	// Через сколько попыток пройдёт примерно всё отведённое время.
	GiveUpAfterMinutes int `json:"giveUpAfterMinutes"`
}

// CurrentPolicy собирает границы из тех же констант, по которым работает
// доставка: пересчёт, а не второй список.
func CurrentPolicy() Policy {
	total := time.Duration(0)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		total += backoff(attempt)
	}
	return Policy{
		Attempts:           maxAttempts,
		FirstDelaySeconds:  int(firstDelay.Seconds()),
		MaxDelaySeconds:    int(maxDelay.Seconds()),
		TimeoutSeconds:     int(sendTimeout.Seconds()),
		GiveUpAfterMinutes: int(total.Minutes()),
	}
}

type Worker struct {
	db   *store.Store
	http *http.Client
	log  *slog.Logger
}

func NewWorker(db *store.Store, log *slog.Logger) *Worker {
	return &Worker{db: db, http: &http.Client{Timeout: sendTimeout}, log: log}
}

// Run разбирает очередь, пока не отменят контекст.
func (w *Worker) Run(ctx context.Context) {
	for {
		sent, err := w.Once(ctx)
		if err != nil && ctx.Err() == nil {
			w.log.Error("разбор очереди вебхуков", "err", err)
		}
		if sent > 0 {
			// Очередь не пуста — продолжаем без паузы.
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(idleDelay):
		}
	}
}

// Once забирает пачку созревших доставок и отправляет их. Возвращает,
// сколько отправлено, — по этому числу Run решает, спать ли.
func (w *Worker) Once(ctx context.Context) (int, error) {
	type job struct {
		id       string
		hookID   string
		event    string
		payload  []byte
		attempts int
		url      string
		secret   string
	}

	var jobs []job
	// Работник ходит без арендатора: очередь общая. Строки берутся
	// с блокировкой и пропуском занятых, поэтому два работника не отправят
	// одно и то же дважды.
	err := w.db.InScope(ctx, store.Scope{}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select d.id, d.webhook_id, d.event, d.payload::text, d.attempts, w.url, w.secret
			  from webhook_deliveries d
			  join webhooks w on w.id = d.webhook_id
			 where d.delivered_at is null and d.failed_at is null
			   and d.next_attempt_at <= now()
			   and w.disabled_at is null and w.paused_at is null
			 order by d.next_attempt_at
			 limit $1
			 for update of d skip locked`, batchSize)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j job
			if err := rows.Scan(&j.id, &j.hookID, &j.event, &j.payload,
				&j.attempts, &j.url, &j.secret); err != nil {
				return err
			}
			jobs = append(jobs, j)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Срок следующей попытки сдвигается сразу, в той же транзакции:
		// иначе соседний работник схватит ту же доставку, едва мы отпустим
		// блокировку, и отправит второй раз.
		for _, j := range jobs {
			if _, err := tx.Exec(ctx, `
				update webhook_deliveries
				   set attempts = attempts + 1,
				       next_attempt_at = now() + make_interval(secs => $2)
				 where id = $1`, j.id, backoff(j.attempts+1).Seconds()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || len(jobs) == 0 {
		return 0, err
	}

	for _, j := range jobs {
		status, sendErr := w.send(ctx, j.url, j.secret, j.id, j.event, j.payload)
		w.record(ctx, j.id, j.hookID, j.attempts+1, status, sendErr)
	}
	return len(jobs), nil
}

func (w *Worker) send(ctx context.Context, target, secret, deliveryID, event string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	now := time.Now()
	req.Header.Set("content-type", "application/json; charset=utf-8")
	req.Header.Set("X-Event", event)
	// Идентификатор доставки постоянен между попытками: доставка
	// гарантируется «не менее одного раза», и получатель обязан уметь
	// отличить повтор от нового события.
	req.Header.Set("X-Delivery-Id", deliveryID)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", now.Unix()))
	req.Header.Set("X-Signature", Sign(secret, now, body))

	resp, err := w.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("получатель ответил %d", resp.StatusCode)
}

// record отмечает исход попытки. Исчерпав попытки, доставка сдаётся,
// а подписка отключается: молча копить недоставленное годами хуже,
// чем перестать и сказать об этом.
func (w *Worker) record(ctx context.Context, deliveryID, hookID string, attempts, status int, sendErr error) {
	err := w.db.InScope(ctx, store.Scope{}, func(tx pgx.Tx) error {
		if sendErr == nil {
			_, err := tx.Exec(ctx, `
				update webhook_deliveries
				   set delivered_at = now(), last_status = $2, last_error = null
				 where id = $1`, deliveryID, status)
			return err
		}

		text := sendErr.Error()
		var code *int
		if status > 0 {
			code = &status
		}
		if _, err := tx.Exec(ctx, `
			update webhook_deliveries
			   set last_status = $2, last_error = $3,
			       failed_at = case when $4 then now() end
			 where id = $1`, deliveryID, code, text, attempts >= maxAttempts); err != nil {
			return err
		}
		if attempts < maxAttempts {
			return nil
		}
		_, err := tx.Exec(ctx, `
			update webhooks set disabled_at = now(), last_error = $2 where id = $1`,
			hookID, text)
		return err
	})
	if err != nil {
		w.log.Error("отметка о доставке не сохранена", "доставка", deliveryID, "err", err)
	}
}

// backoff — задержка перед попыткой номер n. Удвоение с потолком: частые
// повторы в первые минуты полезны (получатель мог перезапускаться),
// дальше они только шумят.
func backoff(attempt int) time.Duration {
	delay := firstDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > maxDelay {
			return maxDelay
		}
	}
	return delay
}
