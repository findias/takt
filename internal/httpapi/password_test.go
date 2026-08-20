package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// request собирает запрос, которому нужен свой заголовок, — таких
// в проверках доступа мало, и держать ради них второй клиент незачем.
func (a *api) request(method, path string, body any) *http.Request {
	a.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		a.t.Fatal(err)
	}
	req, err := http.NewRequest(method, a.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("content-type", "application/json")
	return req
}

// Смена пароля и обрыв чужих сессий.
//
// Проверяется не «маршрут отвечает 204», а то, ради чего он заведён:
// прежний пароль перестаёт пускать, а сессии, открытые до смены,
// перестают работать. Строка сессии в базе выбрана вместо
// самодостаточного токена именно ради этого.

// signIn открывает ещё одну сессию тому же человеку — как второй браузер.
func (a *api) signIn(email, password string) *session {
	a.t.Helper()
	s := a.session()
	s.email = email
	s.mustDo("POST", "/api/auth/login",
		map[string]any{"email": email, "password": password}, http.StatusOK)
	return s
}

func TestPasswordChangeClosesOtherSessions(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Смена пароля")
	other := a.signIn(owner.email, "parol12345")
	other.mustDo("GET", "/api/me", nil, http.StatusOK)

	owner.mustDo("PUT", "/api/me/password", map[string]any{
		"current": "parol12345", "next": "novyy-parol-12345"}, http.StatusNoContent)

	// Своя сессия остаётся: человек только что подтвердил, что это он.
	owner.mustDo("GET", "/api/me", nil, http.StatusOK)
	// Чужая — нет. Иначе смена замка не отбирает выданные ключи.
	if code, _ := other.do("GET", "/api/me", nil); code != http.StatusUnauthorized {
		t.Errorf("сессия, открытая до смены пароля: код %d, ожидался 401", code)
	}

	// Прежним паролем больше не входят, новым — входят.
	if code, _, _ := a.login(owner.email, "parol12345"); code != http.StatusUnauthorized {
		t.Errorf("вход прежним паролем: код %d, ожидался 401", code)
	}
	if code, raw, _ := a.login(owner.email, "novyy-parol-12345"); code != http.StatusOK {
		t.Errorf("вход новым паролем: код %d, тело: %s", code, raw)
	}
}

func TestPasswordChangeRefusesWhatItShould(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отказы смены")

	// Не зная текущего пароля, сменить нельзя: иначе украденная сессия
	// запирает хозяина снаружи.
	owner.mustDo("PUT", "/api/me/password", map[string]any{
		"current": "не тот", "next": "novyy-parol-12345"}, http.StatusForbidden)
	// Короткий новый пароль — то же правило, что при регистрации.
	owner.mustDo("PUT", "/api/me/password", map[string]any{
		"current": "parol12345", "next": "korotk"}, http.StatusBadRequest)
	// Смена на тот же пароль — отказ, а не молчаливый успех.
	owner.mustDo("PUT", "/api/me/password", map[string]any{
		"current": "parol12345", "next": "parol12345"}, http.StatusBadRequest)

	// Ни одна из неудач не должна была ничего изменить.
	if code, _, _ := a.login(owner.email, "parol12345"); code != http.StatusOK {
		t.Errorf("после отказов прежний пароль перестал работать: код %d", code)
	}
}

// Пришедшему от провайдера пароль здесь не заводят: его учётную запись
// закрывают у провайдера, и второй вход мимо него сделал бы увольнение
// неполным.
func TestFederatedIdentityHasNoPasswordToChange(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Корпоративный вход")
	// Личность у провайдера своя у каждого прогона: пара «издатель +
	// sub» уникальна, и прошлый прогон занял бы её насовсем.
	if _, err := a.impl.db.Pool.Exec(context.Background(),
		`update users set oidc_issuer = 'https://provider.test', oidc_subject = $2
		  where id = $1`, owner.userID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	raw := owner.mustDo("PUT", "/api/me/password", map[string]any{
		"current": "parol12345", "next": "novyy-parol-12345"}, http.StatusConflict)
	if got, _ := field(t, raw, "error").(string); got == "" {
		t.Errorf("отказ без объяснения: %s", raw)
	}
}

func TestSignOutEverywhereKeepsThisBrowser(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Выйти везде")
	other := a.signIn(owner.email, "parol12345")

	owner.mustDo("DELETE", "/api/me/sessions", nil, http.StatusNoContent)

	owner.mustDo("GET", "/api/me", nil, http.StatusOK)
	if code, _ := other.do("GET", "/api/me", nil); code != http.StatusUnauthorized {
		t.Errorf("чужая сессия после «выйти везде»: код %d, ожидался 401", code)
	}
}

// Ключом ни то ни другое: у служебной личности нет ни пароля, ни сессий,
// и отказ называет, чем её отзывают на самом деле.
func TestKeyChangesNoPasswordAndHasNoSessions(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Ключи и пароли")
	raw := owner.mustDo("POST", "/api/clients", map[string]any{
		"name": "обмен", "scopes": []string{"boards:read"}}, http.StatusCreated)
	token, _ := field(t, raw, "token").(string)

	for _, call := range []struct{ method, path string }{
		{"PUT", "/api/me/password"},
		{"DELETE", "/api/me/sessions"},
	} {
		req := a.request(call.method, call.path, map[string]any{
			"current": "parol12345", "next": "novyy-parol-12345"})
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := a.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s ключом: код %d, ожидался 403", call.method, call.path, resp.StatusCode)
		}
	}
}
