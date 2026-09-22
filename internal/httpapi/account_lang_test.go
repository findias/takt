package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// Язык интерфейса у человека (ROADMAP 30.6).
//
// Проверяется то, ради чего он переехал на сервер: выбор, сделанный
// в одном браузере, приезжает в другой — и в «кто я», по которому
// клиент переключится, и в cookie при входе, по которой сервер сразу
// заговорит на выбранном языке.

// langCookieOnLogin входит «вторым браузером» и возвращает cookie языка
// из ответа на вход; пустая строка — сервер её не ставил.
func (a *api) langCookieOnLogin(email, password string) string {
	a.t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		a.t.Fatal(err)
	}
	resp, err := a.client.Post(a.server.URL+"/api/auth/login",
		"application/json", bytes.NewReader(body))
	if err != nil {
		a.t.Fatalf("вход: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("вход: код %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "lang" {
			return c.Value
		}
	}
	return ""
}

func TestChosenLanguageFollowsThePersonToAnotherBrowser(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Язык у человека")

	// Пока не выбирал — решает браузер: ни поля, ни cookie от сервера.
	if got := field(t, owner.mustDo("GET", "/api/me", nil, http.StatusOK), "lang"); got != nil {
		t.Errorf("язык до выбора: %v, ожидалось пусто", got)
	}
	if got := a.langCookieOnLogin(owner.email, "parol12345"); got != "" {
		t.Errorf("cookie языка до выбора: %q — сервер перебил бы выбор браузера", got)
	}

	owner.mustDo("PUT", "/api/me/lang", map[string]any{"lang": "en"}, http.StatusNoContent)

	if got := field(t, owner.mustDo("GET", "/api/me", nil, http.StatusOK), "lang"); got != "en" {
		t.Errorf("язык после выбора: %v, ожидался en", got)
	}
	// Другой браузер: вход сразу приносит язык, и уже первый отказ
	// после входа — по-английски.
	if got := a.langCookieOnLogin(owner.email, "parol12345"); got != "en" {
		t.Errorf("cookie языка при входе из другого браузера: %q, ожидался en", got)
	}
	other := a.signIn(owner.email, "parol12345")
	if got := field(t, other.mustDo("GET", "/api/me", nil, http.StatusOK), "lang"); got != "en" {
		t.Errorf("язык во втором браузере: %v, ожидался en", got)
	}
}

func TestLanguageChoiceRefusesWhatItShould(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отказы языка")

	// Незнакомый язык — отказ с объяснением, а не молча сохранённое
	// значение, которого не прочтёт ни один клиент.
	raw := owner.mustDo("PUT", "/api/me/lang", map[string]any{"lang": "de"}, http.StatusBadRequest)
	if got, _ := field(t, raw, "error").(string); got == "" {
		t.Errorf("отказ без объяснения: %s", raw)
	}
	if got := field(t, owner.mustDo("GET", "/api/me", nil, http.StatusOK), "lang"); got != nil {
		t.Errorf("после отказа язык: %v, ожидалось пусто", got)
	}

	// Ключом — нет: у служебной личности нет интерфейса, язык которого
	// можно было бы выбрать.
	raw = owner.mustDo("POST", "/api/clients", map[string]any{
		"name": "обмен", "scopes": []string{"boards:read"}}, http.StatusCreated)
	token, _ := field(t, raw, "token").(string)
	req := a.request("PUT", "/api/me/lang", map[string]any{"lang": "en"})
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("язык ключом: код %d, ожидался 403", resp.StatusCode)
	}
}
