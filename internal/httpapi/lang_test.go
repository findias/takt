package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// refusal — отказ на запрос с заданным языком: заголовком, как шлёт
// клиент, или cookie, как едет переход браузера.
func (a *api) refusal(method, path, token, accept, cookie string, body string) (int, string) {
	a.t.Helper()
	req, err := http.NewRequest(method, a.server.URL+path, strings.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("content-type", "application/json")
	if accept != "" {
		req.Header.Set("Accept-Language", accept)
	}
	if cookie != "" {
		req.Header.Set("Cookie", "lang="+cookie)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", "lang-"+accept+cookie)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var got struct{ Error string }
	if err := json.Unmarshal(raw, &got); err != nil {
		a.t.Fatalf("%s %s: не JSON: %s", method, path, raw)
	}
	return resp.StatusCode, got.Error
}

// Отказ говорит на языке того, кто спросил, — через все обёртки ответа.
// Обёртка без Unwrap прячет язык молча: отказ просто уходит по-русски,
// и заметить это можно только таким сквозным запросом.
func TestRefusalSpeaksTheLanguageOfTheRequest(t *testing.T) {
	a := newAPI(t)
	login := `{"email":"nobody@example.test","password":"не-тот-пароль"}`

	cases := []struct{ accept, cookie, want string }{
		// Интеграции без заголовка отвечают как прежде.
		{"", "", "неверная почта или пароль"},
		{"en-GB,en;q=0.9", "", "wrong email or password"},
		{"ru-RU,ru;q=0.9,en;q=0.8", "", "неверная почта или пароль"},
		// Выбор в «Оформлении» сильнее языка браузера.
		{"ru-RU", "en", "wrong email or password"},
	}
	for _, c := range cases {
		code, msg := a.refusal("POST", "/api/auth/login", "", c.accept, c.cookie, login)
		if code != http.StatusUnauthorized || msg != c.want {
			t.Errorf("Accept-Language %q, cookie %q: %d %q, ожидался %q", c.accept, c.cookie, code, msg, c.want)
		}
	}

	// Контракт с ключом повтора: ответ идёт через запись для повтора,
	// а она — ещё одна обёртка поверх.
	owner := a.registerOrg("Языки")
	_, token := owner.apiClient("перевод", "boards:write")
	code, msg := a.refusal("POST", "/api/v1/boards", token, "en", "", `{"name":""}`)
	if code != http.StatusBadRequest || msg != "the board needs a name" {
		t.Errorf("отказ контракта: %d %q", code, msg)
	}
}

// speaker — клиент, который каждым запросом называет свой язык,
// как это делает браузерный клиент.
type speaker struct {
	lang string
	next http.RoundTripper
}

func (s speaker) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Accept-Language", s.lang)
	return s.next.RoundTrip(r)
}

// Что сервер заводит сам, он называет на языке того, для кого
// заводит: английский владелец иначе начинал бы с организации
// «Моя команда» и доски с колонками «Очередь», «В работе», «Готово».
func TestServerMadeNamesFollowTheLanguage(t *testing.T) {
	a := newAPI(t)
	s := a.session()
	s.client.Transport = speaker{"en", http.DefaultTransport}
	s.email = "lang-" + uuid.NewString()[:8] + "@example.test"
	s.mustDo("POST", "/api/auth/register", map[string]any{
		"email": s.email, "password": "parol12345", "name": "Test",
	}, http.StatusOK)

	if got := field(t, s.mustDo("GET", "/api/me", nil, http.StatusOK), "orgName"); got != "My team" {
		t.Errorf("организация по умолчанию: %v", got)
	}

	raw := s.mustDo("POST", "/api/boards", map[string]any{"name": "!!"}, http.StatusCreated)
	id, _ := field(t, raw, "id").(string)
	if key := field(t, raw, "key"); key != "BOARD" {
		t.Errorf("ключ доски без букв в названии: %v", key)
	}
	var snap struct {
		Columns []struct{ Name string } `json:"columns"`
	}
	if err := json.Unmarshal(s.mustDo("GET", "/api/boards/"+id, nil, http.StatusOK), &snap); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range snap.Columns {
		names = append(names, c.Name)
	}
	if strings.Join(names, ", ") != "Queue, In progress, Done" {
		t.Errorf("колонки новой доски: %v", names)
	}
}
