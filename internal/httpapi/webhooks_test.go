package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/webhook"
)

// Вебхуки: подписка, подпись, повтор и журнал доставок.

// receiver — сервер на другой стороне подписки.
type receiver struct {
	server *httptest.Server
	mu     sync.Mutex
	got    []received
	status int
}

type received struct {
	event     string
	delivery  string
	signature string
	timestamp string
	body      []byte
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{status: http.StatusOK}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, received{
			event:     req.Header.Get("X-Event"),
			delivery:  req.Header.Get("X-Delivery-Id"),
			signature: req.Header.Get("X-Signature"),
			timestamp: req.Header.Get("X-Timestamp"),
			body:      body,
		})
		status := r.status
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *receiver) received() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]received(nil), r.got...)
}

func (r *receiver) answer(status int) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
}

// drain разбирает очередь одним заходом работника, как это делает сервер.
func drain(t *testing.T) int {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	db, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sent, err := webhook.NewWorker(db, slog.New(slog.NewTextHandler(io.Discard, nil))).
		Once(context.Background())
	if err != nil {
		t.Fatalf("разбор очереди: %v", err)
	}
	return sent
}

func TestWebhookDeliversSignedEvent(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	target := newReceiver(t)

	raw := owner.mustDo("POST", "/api/webhooks", map[string]any{
		"name": "Соседняя система", "url": target.server.URL,
		"events": []string{"card.created"},
	}, http.StatusCreated)
	secret, _ := field(t, raw, "secret").(string)
	if secret == "" {
		t.Fatalf("подписка заведена без ключа подписи: %s", raw)
	}

	boardID := owner.board("Найм")
	snapshot := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": snap.Columns[0].ID, "title": "Задача"},
	}, http.StatusOK)

	if sent := drain(t); sent == 0 {
		t.Fatal("работник не нашёл доставку в очереди")
	}

	got := target.received()
	if len(got) != 1 {
		t.Fatalf("получатель принял %d запросов, ожидался один", len(got))
	}
	if got[0].event != "card.created" {
		t.Errorf("событие %q", got[0].event)
	}
	if got[0].delivery == "" {
		t.Error("доставка пришла без идентификатора: повтор не отличить от нового события")
	}

	// Подпись считается от тела вместе с меткой времени: без метки
	// перехваченный запрос можно повторить когда угодно.
	unix, err := strconv.ParseInt(got[0].timestamp, 10, 64)
	if err != nil {
		t.Fatalf("метка времени нечитаема: %q", got[0].timestamp)
	}
	want := webhook.Sign(secret, time.Unix(unix, 0), got[0].body)
	if got[0].signature != want {
		t.Errorf("подпись не сходится:\n  пришла %s\n  ждали %s", got[0].signature, want)
	}

	// Подписка на другое событие ничего не доставляет.
	owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_COLUMN",
		"payload":     map[string]any{"name": "Ещё колонка"},
	}, http.StatusOK)
	drain(t)
	if len(target.received()) != 1 {
		t.Error("доставлено событие, на которое не подписывались")
	}
}

// Доставка гарантируется «не менее одного раза»: отказ получателя —
// повод повторить, а не забыть.
func TestFailedDeliveryIsKeptWithItsReasonAndCanBeRetried(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	target := newReceiver(t)
	target.answer(http.StatusInternalServerError)

	raw := owner.mustDo("POST", "/api/webhooks", map[string]any{
		"name": "Сломанный", "url": target.server.URL,
		"events": []string{"card.created"},
	}, http.StatusCreated)
	hookID, _ := field(t, raw, "id").(string)

	boardID := owner.board("Найм")
	snapshot := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": snap.Columns[0].ID, "title": "Задача"},
	}, http.StatusOK)

	drain(t)

	raw = owner.mustDo("GET", "/api/webhooks/"+hookID+"/deliveries", nil, http.StatusOK)
	var journal struct {
		Deliveries []struct {
			ID         string  `json:"id"`
			Attempts   int     `json:"attempts"`
			Delivered  bool    `json:"delivered"`
			LastStatus *int    `json:"lastStatus"`
			LastError  *string `json:"lastError"`
		} `json:"deliveries"`
	}
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	if len(journal.Deliveries) != 1 {
		t.Fatalf("в журнале %d доставок, ожидалась одна", len(journal.Deliveries))
	}
	d := journal.Deliveries[0]
	if d.Delivered || d.Attempts != 1 || d.LastStatus == nil || *d.LastStatus != 500 {
		t.Fatalf("неудачная доставка записана неверно: %+v", d)
	}
	if d.LastError == nil || *d.LastError == "" {
		t.Error("причина отказа не сохранена")
	}

	// Получателя починили — досдаём вручную, не выдумывая событие заново.
	target.answer(http.StatusOK)
	owner.mustDo("POST", "/api/deliveries/"+d.ID+"/retry", nil, http.StatusNoContent)
	drain(t)

	raw = owner.mustDo("GET", "/api/webhooks/"+hookID+"/deliveries", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	if !journal.Deliveries[0].Delivered {
		t.Errorf("после повтора доставка не отмечена доставленной: %+v", journal.Deliveries[0])
	}
}

// Подписка выносит данные наружу, поэтому заводит и видит её владелец.
func TestOnlyOwnerManagesWebhooks(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")

	if code, _ := member.do("GET", "/api/webhooks", nil); code != http.StatusForbidden {
		t.Errorf("участник видит подписки: код %d", code)
	}
	if code, _ := member.do("POST", "/api/webhooks", map[string]any{
		"name": "Своя", "url": "http://example.test", "events": []string{"card.created"},
	}); code != http.StatusForbidden {
		t.Errorf("участник завёл подписку: код %d", code)
	}

	// Адрес проверяется: подписка на «куда-нибудь» ничего не доставит,
	// а разбираться будут потом и долго.
	for _, bad := range []string{"", "ftp://example.test", "просто текст"} {
		if code, _ := owner.do("POST", "/api/webhooks", map[string]any{
			"name": "Кривая", "url": bad, "events": []string{"card.created"},
		}); code != http.StatusBadRequest {
			t.Errorf("адрес %q принят: код %d", bad, code)
		}
	}
	if code, _ := owner.do("POST", "/api/webhooks", map[string]any{
		"name": "Без событий", "url": "http://example.test", "events": []string{},
	}); code != http.StatusBadRequest {
		t.Errorf("подписка без событий принята: код %d", code)
	}
}
