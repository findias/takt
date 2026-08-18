package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/konkov/agile/internal/board"
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

// queueWorker — работник очереди со своей связью с базой.
func queueWorker(t *testing.T) *webhook.Worker {
	t.Helper()
	db, err := store.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return webhook.NewWorker(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// drain разбирает очередь доставок до конца, а не одной пачкой.
//
// Очередь общая на базу, а пачка — десять: в полном прогоне первыми
// созревшими оказываются чужие доставки, и своя остаётся нетронутой.
// Так проверка и падала под нагрузкой, рассказывая про доставку, за
// которую никто не брался, — «attempts: 0».
func drain(t *testing.T) int {
	t.Helper()
	worker := queueWorker(t)
	total := 0
	for {
		// Неудачная доставка откладывается на потом, поэтому второй раз
		// в этом же цикле она не попадётся и цикл кончается.
		sent, err := worker.Once(context.Background())
		if err != nil {
			t.Fatalf("разбор очереди: %v", err)
		}
		if sent == 0 {
			return total
		}
		total += sent
	}
}

// await разбирает очередь, пока не сбудется ожидаемое.
//
// Работник в очереди не один. Она общая на базу, и рядом идёт настоящий
// сервер — `make run` и `make e2e` поднимают его на той же базе, а
// получатель проверки живёт на этой же машине и потому ему доступен.
// Доставку он забирает с равным правом, и своего прохода тогда не
// хватает: между тем, как чужой работник занял строку, и тем, как он
// записал исход, проходит запрос к получателю. Проверка, заглянувшая
// в журнал в этот миг, видит «попытка есть, ответа нет» и падает на
// ровном месте — так и мигало, оба раза рядом с браузерными сценариями.
//
// Ждать исход, а не свой проход, вдобавок честнее: доставка обещана
// «не менее одного раза», и чей работник её сделает — не дело проверки.
func await(t *testing.T, what string, ready func() bool) {
	t.Helper()
	worker := queueWorker(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := worker.Once(context.Background()); err != nil {
			t.Fatalf("разбор очереди: %v", err)
		}
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("не дождались: %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// journalEntry — строка журнала доставок, как её видит владелец.
type journalEntry struct {
	ID         string  `json:"id"`
	Attempts   int     `json:"attempts"`
	Delivered  bool    `json:"delivered"`
	LastStatus *int    `json:"lastStatus"`
	LastError  *string `json:"lastError"`
}

func deliveries(t *testing.T, owner *session, hookID string) []journalEntry {
	t.Helper()
	raw := owner.mustDo("GET", "/api/webhooks/"+hookID+"/deliveries", nil, http.StatusOK)
	var journal struct {
		Deliveries []journalEntry `json:"deliveries"`
	}
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	return journal.Deliveries
}

// Подписка на несуществующее событие раньше заводилась с кодом 201
// и не доставляла ничего никогда: узнавали об этом тогда, когда
// не дождались, и шли искать поломку в доставке.
//
// Здесь же проверяется, что список событий отдаёт сервер: у интерфейса
// был свой, вчетверо короче, и «работу сделана» — то, ради чего подписку
// чаще всего и заводят, — выбрать было нельзя.
func TestSubscriptionTakesOnlyEventsThatExist(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")

	code, body := owner.do("POST", "/api/webhooks", map[string]any{
		"name": "Выдуманное", "url": "https://example.test/hooks",
		"events": []string{"card.выполнена"},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("подписка на несуществующее событие: код %d; тело: %s", code, body)
	}
	// Отказ обязан называть известные: иначе следующая попытка — тоже
	// угадывание.
	if !strings.Contains(string(body), "card.done") {
		t.Errorf("отказ не называет, на что подписываться можно: %s", body)
	}

	raw := owner.mustDo("GET", "/api/webhooks", nil, http.StatusOK)
	var list struct {
		Events []string `json:"events"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(list.Events, board.EventNames()) {
		t.Errorf("сервер предлагает не то, что доставляет: %v", list.Events)
	}

	// А на то, что бывает, подписка заводится — включая «работа сделана».
	owner.mustDo("POST", "/api/webhooks", map[string]any{
		"name": "Готовое", "url": "https://example.test/hooks",
		"events": []string{"card.done"},
	}, http.StatusCreated)
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

	await(t, "событие не доехало до получателя", func() bool {
		return len(target.received()) > 0
	})

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

	var d journalEntry
	await(t, "отказ получателя не записан в журнал", func() bool {
		list := deliveries(t, owner, hookID)
		if len(list) != 1 {
			t.Fatalf("в журнале %d доставок, ожидалась одна", len(list))
		}
		d = list[0]
		// Попытка засчитана и ответ записан — исход, а не полдела.
		return d.Attempts > 0 && d.LastStatus != nil
	})
	if d.Delivered || *d.LastStatus != 500 {
		t.Fatalf("неудачная доставка записана неверно: %+v", d)
	}
	if d.LastError == nil || *d.LastError == "" {
		t.Error("причина отказа не сохранена")
	}

	// Получателя починили — досдаём вручную, не выдумывая событие заново.
	target.answer(http.StatusOK)
	owner.mustDo("POST", "/api/deliveries/"+d.ID+"/retry", nil, http.StatusNoContent)
	await(t, "после повтора доставка не отмечена доставленной", func() bool {
		list := deliveries(t, owner, hookID)
		return len(list) == 1 && list[0].Delivered
	})
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
