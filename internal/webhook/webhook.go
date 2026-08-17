// Package webhook — подписки на события и их доставка наружу.
//
// Доставка кладётся в исходящий ящик в той же транзакции, что и само
// событие: либо произошло и то, и другое, либо ничего. Отправляет её
// потом работник, поэтому чужой медленный сервер не может замедлить
// перемещение карточки.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
)

var (
	ErrNotFound = errors.New("подписка не найдена")
	ErrExists   = errors.New("подписка с таким названием уже есть")
)

type Hook struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
	// Secret приходит только при создании: подписывать им будем мы,
	// а получатель обязан сохранить его у себя.
	Secret    string    `json:"secret,omitempty"`
	Disabled  bool      `json:"disabled"`
	LastError *string   `json:"lastError"`
	CreatedAt time.Time `json:"createdAt"`
}

type Delivery struct {
	ID         string     `json:"id"`
	WebhookID  string     `json:"webhookId"`
	Event      string     `json:"event"`
	Attempts   int        `json:"attempts"`
	Delivered  bool       `json:"delivered"`
	Failed     bool       `json:"failed"`
	LastStatus *int       `json:"lastStatus"`
	LastError  *string    `json:"lastError"`
	CreatedAt  time.Time  `json:"createdAt"`
	NextTry    *time.Time `json:"nextTry"`
}

type Service struct {
	db *store.Store
	// known — имена событий, которые вообще рассылаются. Приходят снаружи,
	// потому что рождаются они в доске, а доска знает про этот пакет:
	// список здесь был бы вторым и разошёлся бы с первым.
	known []string
}

func New(db *store.Store, known []string) *Service {
	return &Service{db: db, known: known}
}

// Known — то же имя списка наружу: интерфейс предлагает подписаться
// ровно на то, что доставляется.
func (s *Service) Known() []string { return s.known }

func (s *Service) List(ctx context.Context, orgID, userID string) ([]Hook, error) {
	out := []Hook{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, name, url, events, disabled_at is not null, last_error, created_at
			  from webhooks order by created_at desc`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h Hook
			if err := rows.Scan(&h.ID, &h.Name, &h.URL, &h.Events,
				&h.Disabled, &h.LastError, &h.CreatedAt); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, orgID, actorID, name, target string, events []string) (Hook, error) {
	name = strings.TrimSpace(name)
	target = strings.TrimSpace(target)
	if name == "" {
		return Hook{}, fmt.Errorf("у подписки должно быть название")
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Hook{}, fmt.Errorf("адрес должен начинаться с http:// или https://")
	}
	if len(events) == 0 {
		return Hook{}, fmt.Errorf("подписка без событий ничего не доставляет")
	}
	// Имя события проверяется на входе. Принятая подписка на «card.готово»
	// выглядит работающей и не доставляет ничего никогда — а узнают об
	// этом тогда, когда чего-то не дождались, и ищут поломку в доставке.
	for _, event := range events {
		if !slices.Contains(s.known, event) {
			return Hook{}, fmt.Errorf(
				"события %q не бывает; есть такие: %s", event, strings.Join(s.known, ", "))
		}
	}

	secret, err := newSecret()
	if err != nil {
		return Hook{}, err
	}

	h := Hook{Name: name, URL: target, Events: events, Secret: secret}
	err = s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			insert into webhooks (org_id, name, url, secret, events, created_by)
			values ($1, $2, $3, $4, $5, $6)
			returning id, created_at`,
			orgID, name, target, secret, events, actorID).Scan(&h.ID, &h.CreatedAt)
	})
	return h, err
}

func (s *Service) Delete(ctx context.Context, orgID, actorID, hookID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from webhooks where id = $1`, hookID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Deliveries — журнал доставок подписки, свежие первыми. По нему видно,
// что именно не доехало, и его же смотрят, решая, чинить ли получателя.
func (s *Service) Deliveries(ctx context.Context, orgID, userID, hookID string) ([]Delivery, error) {
	out := []Delivery{}
	err := s.db.InTenant(ctx, orgID, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id, webhook_id, event, attempts,
			       delivered_at is not null, failed_at is not null,
			       last_status, last_error, created_at,
			       case when delivered_at is null and failed_at is null
			            then next_attempt_at end
			  from webhook_deliveries
			 where webhook_id = $1
			 order by created_at desc
			 limit 100`, hookID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Delivery
			if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Attempts,
				&d.Delivered, &d.Failed, &d.LastStatus, &d.LastError,
				&d.CreatedAt, &d.NextTry); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// Retry возвращает сдавшуюся доставку в очередь. Ручной повтор нужен
// ровно затем, зачем и журнал: получателя починили, и теперь хочется
// досдать то, что не доехало, не выдумывая события заново.
func (s *Service) Retry(ctx context.Context, orgID, actorID, deliveryID string) error {
	return s.db.InTenant(ctx, orgID, actorID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			update webhook_deliveries
			   set failed_at = null, attempts = 0, next_attempt_at = now(), last_error = null
			 where id = $1 and delivered_at is null`, deliveryID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Enqueue кладёт событие в исходящий ящик — в той же транзакции, в которой
// оно произошло. Вызывается изнутри операции и потому принимает готовую
// транзакцию, а не открывает свою.
func Enqueue(ctx context.Context, tx pgx.Tx, orgID, event string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Подписок на событие обычно ноль или одна: вставка сразу выбирает
	// подходящие, чтобы не тащить их в код и не ходить в базу дважды.
	_, err = tx.Exec(ctx, `
		insert into webhook_deliveries (org_id, webhook_id, event, payload)
		select $1, w.id, $2, $3::jsonb
		  from webhooks w
		 where w.org_id = $1 and w.disabled_at is null and $2 = any (w.events)`,
		orgID, event, string(body))
	return err
}

// Sign подписывает тело ключом подписки вместе с меткой времени.
//
// Метка входит в подписанное намеренно: без неё перехваченный запрос
// можно повторить когда угодно, а с ней получатель отбрасывает старое.
func Sign(secret string, at time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", at.Unix())
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
