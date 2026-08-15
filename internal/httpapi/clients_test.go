package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Сервисные клиенты: доступ к API не от человека.

// withToken выполняет запрос ключом, а не сессией.
func (a *api) withToken(token, method, path string, body any) (int, []byte) {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, a.server.URL+path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (s *session) apiClient(name string, scopes ...string) (id, token string) {
	s.api.t.Helper()
	raw := s.mustDo("POST", "/api/clients",
		map[string]any{"name": name, "scopes": scopes}, http.StatusCreated)
	id, _ = field(s.api.t, raw, "id").(string)
	token, _ = field(s.api.t, raw, "token").(string)
	if id == "" || token == "" {
		s.api.t.Fatalf("создание ключа: %s", raw)
	}
	return id, token
}

func TestServiceClientWorksLikeAMemberWithinItsScopes(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	boardID := owner.board("Найм")
	_, token := owner.apiClient("Интеграция", "boards:read", "boards:write")

	// Ключ читает и пишет доски.
	if code, body := a.withToken(token, "GET", "/api/boards", nil); code != http.StatusOK {
		t.Fatalf("чтение досок ключом: код %d; тело: %s", code, body)
	}
	code, body := a.withToken(token, "POST", "/api/boards", map[string]any{"name": "Из интеграции"})
	if code != http.StatusCreated {
		t.Fatalf("создание доски ключом: код %d; тело: %s", code, body)
	}

	// Разрешения, которого нет, не даёт ничего — даже если роль позволяет.
	if code, _ := a.withToken(token, "GET", "/api/audit", nil); code != http.StatusForbidden {
		t.Errorf("журнал без разрешения: код %d, ожидался 403", code)
	}
	if code, _ := a.withToken(token, "GET", "/api/teams", nil); code != http.StatusForbidden {
		t.Errorf("структура без разрешения: код %d, ожидался 403", code)
	}

	// Действия ключа именные: у служебной личности есть имя, и оно
	// попадает в ленту доски наравне с человеческими.
	raw := owner.mustDo("GET", "/api/boards/"+boardID+"/events", nil, http.StatusOK)
	_ = raw

	// Ключ только для чтения не пишет, потому что и роль у него читающая.
	_, readOnly := owner.apiClient("Отчёты", "boards:read")
	if code, _ := a.withToken(readOnly, "POST", "/api/boards",
		map[string]any{"name": "Нельзя"}); code != http.StatusForbidden {
		t.Errorf("ключ на чтение создал доску: код %d, ожидался 403", code)
	}
}

// Ключ живёт в организации и не видит соседнюю: изоляция для него
// ровно та же, что и для человека, потому что механизм тот же.
func TestServiceClientSeesOnlyItsOwnOrganisation(t *testing.T) {
	a := newAPI(t)
	first := a.registerOrg("Первая")
	second := a.registerOrg("Вторая")
	first.board("Своя доска")
	foreign := second.board("Чужая доска")

	_, token := first.apiClient("Интеграция", "boards:read")

	raw := a.mustToken(token, "GET", "/api/boards", nil, http.StatusOK)
	var list struct {
		Boards []struct{ Name string } `json:"boards"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Boards) != 1 || list.Boards[0].Name != "Своя доска" {
		t.Fatalf("ключ видит %+v", list.Boards)
	}
	if code, _ := a.withToken(token, "GET", "/api/boards/"+foreign, nil); code != http.StatusNotFound {
		t.Errorf("чужая доска по ключу: код %d, ожидался 404", code)
	}
}

func TestRevokedAndUnknownTokensAreRejected(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	id, token := owner.apiClient("Интеграция", "boards:read")

	if code, _ := a.withToken(token, "GET", "/api/boards", nil); code != http.StatusOK {
		t.Fatal("свежий ключ не работает")
	}

	owner.mustDo("DELETE", "/api/clients/"+id, nil, http.StatusNoContent)

	if code, _ := a.withToken(token, "GET", "/api/boards", nil); code != http.StatusUnauthorized {
		t.Errorf("отозванный ключ работает: код %d, ожидался 401", code)
	}
	if code, _ := a.withToken(uuid.NewString(), "GET", "/api/boards", nil); code != http.StatusUnauthorized {
		t.Errorf("выдуманный ключ принят: код %d, ожидался 401", code)
	}

	// Повторный отзыв ничего не меняет.
	if code, _ := owner.do("DELETE", "/api/clients/"+id, nil); code != http.StatusNotFound {
		t.Errorf("повторный отзыв: код %d, ожидался 404", code)
	}
}

// Список ключей — это список того, что имеет доступ к данным, и он же
// подсказка, что стоит украсть.
func TestOnlyOwnerManagesClientsAndTokenIsShownOnce(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")

	raw := owner.mustDo("POST", "/api/clients",
		map[string]any{"name": "Интеграция", "scopes": []string{"boards:read"}},
		http.StatusCreated)
	token, _ := field(t, raw, "token").(string)
	prefix, _ := field(t, raw, "prefix").(string)
	if token == "" || prefix == "" || !strings.HasPrefix(token, prefix) {
		t.Fatalf("ключ выдан без токена или без узнаваемого начала: %s", raw)
	}

	// В списке токена уже нет — только его начало.
	raw = owner.mustDo("GET", "/api/clients", nil, http.StatusOK)
	if strings.Contains(string(raw), token) {
		t.Error("токен виден в списке ключей")
	}
	if !strings.Contains(string(raw), prefix) {
		t.Error("по списку нельзя узнать свой ключ")
	}

	if code, _ := member.do("GET", "/api/clients", nil); code != http.StatusForbidden {
		t.Errorf("участник видит список ключей: код %d, ожидался 403", code)
	}
	if code, _ := member.do("POST", "/api/clients",
		map[string]any{"name": "Свой", "scopes": []string{"boards:read"}}); code != http.StatusForbidden {
		t.Errorf("участник завёл ключ: код %d, ожидался 403", code)
	}

	// Разрешения проверяются на входе: выдумать своё нельзя.
	if code, _ := owner.do("POST", "/api/clients",
		map[string]any{"name": "Всё", "scopes": []string{"everything"}}); code != http.StatusBadRequest {
		t.Errorf("выдуманное разрешение принято: код %d, ожидался 400", code)
	}
	if code, _ := owner.do("POST", "/api/clients",
		map[string]any{"name": "Пустой", "scopes": []string{}}); code != http.StatusBadRequest {
		t.Errorf("ключ без разрешений принят: код %d, ожидался 400", code)
	}
}

// mustToken — как withToken, но с ожидаемым кодом.
func (a *api) mustToken(token, method, path string, body any, want int) []byte {
	a.t.Helper()
	code, raw := a.withToken(token, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s ключом: код %d, ожидался %d; тело: %s", method, path, code, want, raw)
	}
	return raw
}

// Контракт для тех, кто снаружи: версия в адресе, коды ошибок, предел
// частоты и безопасный повтор.
func TestVersionedPathAndErrorCodes(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	_, token := owner.apiClient("Интеграция", "boards:read")

	// Адрес с версией — обещание чужим; внутренний путь остаётся своим.
	code, _ := a.withToken(token, "GET", "/api/v1/boards", nil)
	if code != http.StatusOK {
		t.Fatalf("версионированный путь: код %d, ожидался 200", code)
	}

	// У ошибки есть машиночитаемый код рядом с текстом: текст пишется
	// для человека и может меняться, код — нет.
	_, raw := a.withToken(token, "GET", "/api/v1/boards/"+uuid.NewString(), nil)
	if got, _ := field(t, raw, "code").(string); got != "not_found" {
		t.Errorf("код ошибки %q, ожидался not_found; тело: %s", got, raw)
	}
	_, raw = a.withToken(token, "GET", "/api/v1/audit", nil)
	if got, _ := field(t, raw, "code").(string); got != "forbidden" {
		t.Errorf("код отказа %q, ожидался forbidden; тело: %s", got, raw)
	}
	_, raw = a.withToken("выдумка", "GET", "/api/v1/boards", nil)
	if got, _ := field(t, raw, "code").(string); got != "unauthenticated" {
		t.Errorf("код неавторизованного %q; тело: %s", got, raw)
	}
}

// Повтор изменяющего вызова не должен заводить вторую доску.
func TestIdempotencyKeyReplaysTheSameAnswer(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	_, token := owner.apiClient("Интеграция", "boards:read", "boards:write")
	key := uuid.NewString()

	first := a.withKey(token, key, "POST", "/api/v1/boards", map[string]any{"name": "Одна"})
	second := a.withKey(token, key, "POST", "/api/v1/boards", map[string]any{"name": "Одна"})

	if string(first) != string(second) {
		t.Fatalf("повтор вернул другой ответ:\n%s\n%s", first, second)
	}

	raw := a.mustToken(token, "GET", "/api/v1/boards", nil, http.StatusOK)
	var list struct {
		Boards []struct{ Name string } `json:"boards"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Boards) != 1 {
		t.Fatalf("после повтора досок %d, ожидалась одна: %+v", len(list.Boards), list.Boards)
	}

	// Тот же ключ к другому вызову — ошибка клиента, а не просьба
	// вернуть прошлое.
	req := a.requestWithKey(token, key, "POST", "/api/v1/teams", map[string]any{"name": "Команда"})
	if req.code != http.StatusConflict {
		t.Errorf("ключ, использованный для другого вызова: код %d, ожидался 409", req.code)
	}
	if got, _ := field(t, req.body, "code").(string); got != "idempotency_key_reused" {
		t.Errorf("код ошибки %q; тело: %s", got, req.body)
	}
}

type keyed struct {
	code int
	body []byte
}

func (a *api) requestWithKey(token, key, method, path string, body any) keyed {
	a.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		a.t.Fatal(err)
	}
	req, err := http.NewRequest(method, a.server.URL+path, strings.NewReader(string(raw)))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", key)

	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	return keyed{code: resp.StatusCode, body: answer}
}

func (a *api) withKey(token, key, method, path string, body any) []byte {
	a.t.Helper()
	got := a.requestWithKey(token, key, method, path, body)
	if got.code != http.StatusCreated {
		a.t.Fatalf("%s %s с ключом повтора: код %d; тело: %s", method, path, got.code, got.body)
	}
	return got.body
}

// Описание контракта читается без ключа: требовать вход, чтобы прочитать,
// как войти, — замкнутый круг.
func TestContractIsPublishedAndValid(t *testing.T) {
	a := newAPI(t)
	code, raw := a.session().do("GET", "/api/v1/openapi.json", nil)
	if code != http.StatusOK {
		t.Fatalf("описание контракта: код %d", code)
	}

	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("описание не разбирается: %v", err)
	}
	if doc.OpenAPI == "" || doc.Info.Version != "v1" {
		t.Errorf("описание без версии: %+v", doc)
	}
	// Описание обязано покрывать то, ради чего оно вообще есть.
	for _, path := range []string{"/boards", "/boards/{id}/operations", "/audit"} {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("в описании нет %s", path)
		}
	}
}
