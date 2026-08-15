package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Выгрузка. Проверяется не «файл получился», а три вещи, из-за которых
// её и делают: в ней есть всё своё, в ней нет ничего чужого и в ней
// нет секретов.

// raw возвращает ещё и заголовки: у выгрузки они часть ответа.
func (s *session) raw(path string) (*http.Response, []byte) {
	s.api.t.Helper()
	req, err := http.NewRequest("GET", s.api.server.URL+path, nil)
	if err != nil {
		s.api.t.Fatal(err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.api.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(resp.Body); err != nil {
		s.api.t.Fatal(err)
	}
	return resp, body.Bytes()
}

func TestExportCarriesOwnDataAndNotAnyoneElses(t *testing.T) {
	a := newAPI(t)
	alice := a.registerOrg("Своя")
	bob := a.registerOrg("Чужая")

	created := alice.mustDo("POST", "/api/boards",
		map[string]any{"name": "Наша доска"}, http.StatusCreated)
	boardID := field(t, created, "id").(string)
	snap := alice.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)
	alice.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Наша карточка"},
	}, http.StatusOK)

	bob.mustDo("POST", "/api/boards", map[string]any{"name": "Чужая доска"}, http.StatusCreated)

	resp, body := alice.raw("/api/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("выгрузка: код %d, тело %s", resp.StatusCode, body)
	}
	// Выгрузка нужна как файл, а не как страница в браузере.
	if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("выгрузка отдана не вложением: %q", got)
	}

	var dump map[string]json.RawMessage
	if err := json.Unmarshal(body, &dump); err != nil {
		t.Fatalf("выгрузка не разбирается: %v; начало: %.200s", err, body)
	}
	for _, key := range []string{"version", "exported_at", "org", "boards", "cards", "people"} {
		if _, ok := dump[key]; !ok {
			t.Errorf("в выгрузке нет раздела %q", key)
		}
	}
	if !bytes.Contains(body, []byte("Наша доска")) || !bytes.Contains(body, []byte("Наша карточка")) {
		t.Error("своё в выгрузку не попало")
	}
	if bytes.Contains(body, []byte("Чужая доска")) {
		t.Error("в выгрузку попала доска другой организации")
	}
	if !bytes.Contains(body, []byte(alice.email)) {
		t.Error("участники в выгрузку не попали")
	}
	if bytes.Contains(body, []byte(bob.email)) {
		t.Error("в выгрузку попал человек из другой организации")
	}
}

// Подпись вебхука и хеш ключа — не данные организации, а средства
// проверки. Утечка файла не должна становиться утечкой доступа.
func TestExportCarriesNoSecrets(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("С секретами")

	hook := owner.mustDo("POST", "/api/webhooks", map[string]any{
		"name": "Проба", "url": "http://example.test/hook", "events": []string{"card.created"},
	}, http.StatusCreated)
	secret, _ := field(t, hook, "secret").(string)
	if secret == "" {
		t.Fatalf("вебхук не вернул подпись, тело: %s", hook)
	}

	client := owner.mustDo("POST", "/api/clients", map[string]any{
		"name": "Робот", "scopes": []string{"boards:read"},
	}, http.StatusCreated)
	token, _ := field(t, client, "token").(string)
	if token == "" {
		t.Fatalf("клиент не вернул ключ, тело: %s", client)
	}

	invitee := "invite-" + uuid.NewString()[:8] + "@example.test"
	owner.mustDo("POST", "/api/invites",
		map[string]any{"email": invitee, "role": "member"}, http.StatusCreated)

	_, body := owner.raw("/api/export?audit=1")

	for what, needle := range map[string]string{
		"подпись вебхука": secret,
		"ключ доступа":    token,
		"поле token_hash": "token_hash",
		"поле secret":     `"secret"`,
		"хеш пароля":      "password_hash",
	} {
		if bytes.Contains(body, []byte(needle)) {
			t.Errorf("в выгрузке нашлось: %s", what)
		}
	}
	// А сами записи о вебхуке, ключе и приглашении — на месте: секрет убран,
	// не запись.
	for _, needle := range []string{"Проба", "Робот", invitee} {
		if !bytes.Contains(body, []byte(needle)) {
			t.Errorf("в выгрузке нет записи %q", needle)
		}
	}
}

// Журнал больше всех остальных разделов вместе взятых, поэтому кладётся
// только по просьбе. Сама выгрузка при этом всегда попадает в журнал:
// это то действие, о котором служба безопасности спрашивает «кто и когда».
func TestAuditIsExportedOnlyOnRequestAndExportItselfIsRecorded(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("С журналом")
	owner.mustDo("POST", "/api/boards", map[string]any{"name": "Доска"}, http.StatusCreated)

	var plain map[string]json.RawMessage
	_, body := owner.raw("/api/export")
	if err := json.Unmarshal(body, &plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["audit_events"]; ok {
		t.Error("журнал попал в выгрузку без просьбы")
	}

	var full map[string]json.RawMessage
	_, body = owner.raw("/api/export?audit=1")
	if err := json.Unmarshal(body, &full); err != nil {
		t.Fatalf("выгрузка с журналом не разбирается: %v", err)
	}
	var events []map[string]any
	if err := json.Unmarshal(full["audit_events"], &events); err != nil {
		t.Fatalf("журнал не разбирается: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("журнал запрошен, но пуст")
	}

	// Первая выгрузка уже была — значит, её след обязан быть во второй.
	var exports int
	for _, entry := range events {
		if entry["subject"] == "export" {
			exports++
		}
	}
	if exports == 0 {
		t.Error("выгрузка не записана в журнал")
	}
}

func TestExportIsOwnerOnly(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Закрытая")

	member := a.session()
	member.email = "member-" + uuid.NewString()[:8] + "@example.test"
	inv := owner.mustDo("POST", "/api/invites",
		map[string]any{"email": member.email, "role": "member"}, http.StatusCreated)
	link := field(t, inv, "link").(string)
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")
	member.mustDo("POST", "/api/invites/"+parts[len(parts)-1]+"/accept", map[string]any{
		"name": "Участник", "password": "parol12345"}, http.StatusOK)

	// Участнику отказ виден словами: файл содержит переписку и почтовые
	// адреса всех, то есть больше, чем видит каждый по отдельности.
	if code, _ := member.do("GET", "/api/export", nil); code != http.StatusForbidden {
		t.Errorf("выгрузка участником: код %d, ожидался 403", code)
	}
	if code, _ := a.session().do("GET", "/api/export", nil); code != http.StatusUnauthorized {
		t.Errorf("выгрузка без сессии: код %d, ожидался 401", code)
	}
}
