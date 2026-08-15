package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/konkov/agile/internal/config"
	"github.com/konkov/agile/internal/realtime"
	"github.com/konkov/agile/internal/store"
)

// Тесты HTTP-слоя идут против настоящей базы и настоящего маршрутизатора:
// именно здесь живут границы доступа, и проверять их заглушками — значит
// проверять заглушки. Запуск: make test-integration.
//
// Каждый тест работает в своей организации со своими пользователями,
// поэтому тесты не мешают друг другу и порядок их не важен.

type api struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
}

func newAPI(t *testing.T) *api {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("не задан TEST_DATABASE_URL, интеграционные тесты пропущены")
	}
	db, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("подключение к тестовой базе: %v", err)
	}
	t.Cleanup(db.Close)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Оповещения слушает настоящий узел: поток изменений — часть API,
	// и проверять его заглушкой значит проверять заглушку.
	hub := realtime.NewHub(db, log)
	hubCtx, stopHub := context.WithCancel(context.Background())
	go hub.Run(hubCtx)
	t.Cleanup(stopHub)

	srv := httptest.NewServer(
		New(config.Config{BaseURL: "http://example.test"}, db, log, hub).Handler())
	t.Cleanup(srv.Close)

	return &api{t: t, server: srv, client: srv.Client()}
}

// session — отдельный клиент со своими cookie: так в одном тесте живут
// несколько пользователей одновременно.
type session struct {
	api    *api
	client *http.Client
	userID string
	email  string
}

func (a *api) session() *session {
	a.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		a.t.Fatal(err)
	}
	return &session{api: a, client: &http.Client{Jar: jar}}
}

func (s *session) do(method, path string, body any) (int, []byte) {
	s.api.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			s.api.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.api.server.URL+path, reader)
	if err != nil {
		s.api.t.Fatal(err)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.api.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (s *session) mustDo(method, path string, body any, want int) []byte {
	s.api.t.Helper()
	code, raw := s.do(method, path, body)
	if code != want {
		s.api.t.Fatalf("%s %s: код %d, ожидался %d; тело: %s", method, path, code, want, raw)
	}
	return raw
}

// registerOrg заводит нового человека вместе с новой организацией.
func (a *api) registerOrg(name string) *session {
	a.t.Helper()
	s := a.session()
	s.email = "api-" + uuid.NewString()[:8] + "@example.test"
	s.mustDo("POST", "/api/auth/register", map[string]any{
		"email": s.email, "password": "parol12345", "name": "Тест", "orgName": name,
	}, http.StatusOK)
	var me struct{ ID string }
	body := s.mustDo("GET", "/api/me", nil, http.StatusOK)
	if err := json.Unmarshal(body, &me); err != nil {
		a.t.Fatal(err)
	}
	s.userID = me.ID
	return s
}

func field(t *testing.T, raw []byte, path ...string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("разбор ответа: %v; тело: %s", err, raw)
	}
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("поле %q: ожидался объект, получено %T", key, v)
		}
		v = m[key]
	}
	return v
}

// --- собственно тесты ---

func TestHealthNeedsNoSession(t *testing.T) {
	a := newAPI(t)
	code, raw := a.session().do("GET", "/healthz", nil)
	if code != http.StatusOK {
		t.Fatalf("здоровье: код %d, тело %s", code, raw)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	a := newAPI(t)
	anon := a.session()
	for _, path := range []string{"/api/me", "/api/orgs", "/api/team", "/api/boards"} {
		if code, _ := anon.do("GET", path, nil); code != http.StatusUnauthorized {
			t.Errorf("GET %s без сессии: код %d, ожидался 401", path, code)
		}
	}
}

func TestBoardOfAnotherOrgIsIndistinguishableFromMissing(t *testing.T) {
	a := newAPI(t)
	alice := a.registerOrg("Команда А")
	bob := a.registerOrg("Команда Б")

	created := alice.mustDo("POST", "/api/boards", map[string]any{"name": "Своя"}, http.StatusCreated)
	boardID, _ := field(t, created, "id").(string)
	if boardID == "" {
		t.Fatal("не получен идентификатор доски")
	}
	alice.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)

	// Чужая доска обязана выглядеть как несуществующая: подтверждать её
	// существование постороннему незачем.
	if code, _ := bob.do("GET", "/api/boards/"+boardID, nil); code != http.StatusNotFound {
		t.Errorf("чужая доска: код %d, ожидался 404", code)
	}

	// И операция над ней тоже.
	code, _ := bob.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": uuid.NewString(), "title": "Чужая"},
	})
	if code != http.StatusNotFound {
		t.Errorf("операция над чужой доской: код %d, ожидался 404", code)
	}
}

func TestViewerMayReadButNotWrite(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Команда с наблюдателем")

	created := owner.mustDo("POST", "/api/boards", map[string]any{"name": "Поток"}, http.StatusCreated)
	boardID := field(t, created, "id").(string)
	snap := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	columns := field(t, snap, "columns").([]any)
	columnID := columns[0].(map[string]any)["id"].(string)

	// Приглашение открывается по секретной ссылке: у приглашённого может
	// не быть аккаунта, и организация ему заранее неизвестна.
	invited := a.session()
	invited.email = "viewer-" + uuid.NewString()[:8] + "@example.test"
	inv := owner.mustDo("POST", "/api/invites", map[string]any{
		"email": invited.email, "role": "viewer"}, http.StatusCreated)
	link, _ := field(t, inv, "link").(string)
	if link == "" {
		t.Fatalf("приглашение не вернуло ссылку, тело: %s", inv)
	}
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")
	token := parts[len(parts)-1]

	invited.mustDo("POST", "/api/invites/"+token+"/accept", map[string]any{
		"name": "Наблюдатель", "password": "parol12345",
	}, http.StatusOK)

	// Читать наблюдателю можно.
	invited.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)

	// Писать — нет, и это 403, а не 404: доску он видит.
	code, raw := invited.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Нельзя"},
	})
	if code != http.StatusForbidden {
		t.Errorf("запись наблюдателем: код %d, ожидался 403; тело: %s", code, raw)
	}
}

func TestOnlyOwnerManagesTeam(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Команда с участником")
	stranger := a.registerOrg("Посторонняя команда")

	// Посторонний не владеет этой организацией — административные ручки
	// закрыты. Он вообще в ней не состоит, поэтому доступа нет ни к чему.
	if code, _ := stranger.do("GET", "/api/team", nil); code != http.StatusOK {
		t.Logf("GET /api/team посторонним: код %d (у него своя организация)", code)
	}
	code, _ := stranger.do("PUT", "/api/members/"+owner.userID+"/role",
		map[string]any{"role": "viewer"})
	if code == http.StatusOK {
		t.Error("посторонний сменил роль в чужой организации")
	}

	// Владелец не может разжаловать сам себя, если он единственный владелец:
	// иначе организация останется без администратора.
	code, raw := owner.do("PUT", "/api/members/"+owner.userID+"/role",
		map[string]any{"role": "member"})
	if code == http.StatusOK {
		t.Errorf("единственный владелец понизил себя: тело %s", raw)
	}
}

func TestOperationIsIdempotentOverHTTP(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Идемпотентность")
	created := owner.mustDo("POST", "/api/boards", map[string]any{"name": "Поток"}, http.StatusCreated)
	boardID := field(t, created, "id").(string)
	snap := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)

	op := map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Одна", "place": "end"},
	}
	first := owner.mustDo("POST", "/api/boards/"+boardID+"/operations", op, http.StatusOK)
	second := owner.mustDo("POST", "/api/boards/"+boardID+"/operations", op, http.StatusOK)

	// Повтор с тем же идентификатором возвращает сохранённый результат,
	// а не создаёт вторую карточку.
	firstID := field(t, first, "patch", "cards").([]any)[0].(map[string]any)["id"]
	secondID := field(t, second, "patch", "cards").([]any)[0].(map[string]any)["id"]
	if firstID != secondID {
		t.Errorf("повтор операции создал другую карточку: %v против %v", firstID, secondID)
	}

	after := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	if cards := field(t, after, "cards").([]any); len(cards) != 1 {
		t.Errorf("на доске %d карточек, ожидалась одна", len(cards))
	}
}

func TestMalformedRequestsAreBadRequests(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Кривые запросы")
	created := owner.mustDo("POST", "/api/boards", map[string]any{"name": "Поток"}, http.StatusCreated)
	boardID := field(t, created, "id").(string)

	cases := []struct {
		name string
		body any
		want int
	}{
		{"нет operationId", map[string]any{"type": "CREATE_CARD", "payload": map[string]any{}}, http.StatusBadRequest},
		{"неизвестный тип", map[string]any{"operationId": uuid.NewString(), "type": "ЧТО_ТО", "payload": map[string]any{}}, http.StatusBadRequest},
		{"кривой идентификатор", map[string]any{
			"operationId": uuid.NewString(), "type": "UPDATE_COLUMN",
			"payload": map[string]any{"columnId": "не-uuid", "name": "Х"}}, http.StatusBadRequest},
	}
	for _, c := range cases {
		if code, raw := owner.do("POST", "/api/boards/"+boardID+"/operations", c.body); code != c.want {
			t.Errorf("%s: код %d, ожидался %d; тело: %s", c.name, code, c.want, raw)
		}
	}
}
