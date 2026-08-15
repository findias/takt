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
