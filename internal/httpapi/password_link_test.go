package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Ссылка «задать пароль» (ROADMAP 23.6). Проверяется то, ради чего она
// одноразовая и адресная: по ней задают пароль один раз и входят сразу;
// второй раз она не пускает; новая гасит прежнюю; прочие сессии человека
// обрываются; выпустить её можно только тому, чьим паролем владелец
// вправе распоряжаться.

// issueLink выпускает ссылку участнику и возвращает её токен.
func (s *session) issueLink(userID string) string {
	s.api.t.Helper()
	raw := s.mustDo("POST", "/api/members/"+userID+"/password-link", nil, http.StatusCreated)
	link, _ := field(s.api.t, raw, "link").(string)
	if !strings.Contains(link, "/password/") {
		s.api.t.Fatalf("ссылка не того вида: %s", raw)
	}
	parts := strings.Split(link, "/")
	return parts[len(parts)-1]
}

func TestPasswordLinkSetsPasswordOnceAndSignsIn(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Ссылка для входа")
	member := owner.join("member")
	// Будто учётную запись завёл перенос: пароль ещё не задан.
	if _, err := a.impl.db.Pool.Exec(context.Background(),
		`update users set awaiting_password = true where id = $1`, member.userID); err != nil {
		t.Fatal(err)
	}
	team := owner.mustDo("GET", "/api/team", nil, http.StatusOK)
	if !strings.Contains(string(team), `"awaitingPassword":true`) {
		t.Fatalf("«Команда» не знает, кто ещё не задал пароль: %s", team)
	}

	first := owner.issueLink(member.userID)
	token := owner.issueLink(member.userID)
	// Новая ссылка гасит прежнюю: действующий вход один.
	stranger := a.session()
	raw := stranger.mustDo("POST", "/api/password-links/lookup", map[string]any{"token": first}, http.StatusNotFound)
	if got, _ := field(t, raw, "code").(string); got != "password_link_invalid" {
		t.Errorf("прежняя ссылка: код %q", got)
	}

	raw = stranger.mustDo("POST", "/api/password-links/lookup", map[string]any{"token": token}, http.StatusOK)
	if got, _ := field(t, raw, "email").(string); got != member.email {
		t.Errorf("ссылка называет не того: %s", raw)
	}
	if got, _ := field(t, raw, "orgName").(string); got != "Ссылка для входа" {
		t.Errorf("ссылка не называет организацию: %s", raw)
	}

	raw = stranger.mustDo("POST", "/api/password-links/use", map[string]any{"token": token, "password": "korotk"}, http.StatusBadRequest)
	if got, _ := field(t, raw, "code").(string); got != "password_short" {
		t.Errorf("короткий пароль: код %q", got)
	}
	// Короткий пароль ссылку не сжёг.
	me := stranger.mustDo("POST", "/api/password-links/use",
		map[string]any{"token": token, "password": "novyy-parol-12345"}, http.StatusOK)
	if got, _ := field(t, me, "email").(string); got != member.email {
		t.Fatalf("вошёл не тот: %s", me)
	}
	// Вошёл сразу — сессия его.
	stranger.mustDo("GET", "/api/me", nil, http.StatusOK)
	// Прежняя сессия человека оборвана, как при смене пароля.
	if code, _ := member.do("GET", "/api/me", nil); code != http.StatusUnauthorized {
		t.Errorf("сессия, открытая до ссылки: код %d, ожидался 401", code)
	}
	if code, _, _ := a.login(member.email, "novyy-parol-12345"); code != http.StatusOK {
		t.Errorf("новым паролем не входят: код %d", code)
	}
	// Второй раз ссылка не пускает.
	a.session().mustDo("POST", "/api/password-links/use",
		map[string]any{"token": token, "password": "eshche-odin-12345"}, http.StatusNotFound)

	team = owner.mustDo("GET", "/api/team", nil, http.StatusOK)
	if strings.Contains(string(team), `"awaitingPassword":true`) {
		t.Errorf("задавший пароль всё ещё «ещё не входил»: %s", team)
	}
}

func TestPasswordLinkRefusesWhatItShould(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отказы ссылки")
	member := owner.join("member")

	// Участник ссылок не выпускает.
	member.mustDo("POST", "/api/members/"+owner.userID+"/password-link", nil, http.StatusForbidden)

	codeOf := func(userID string) string {
		raw := owner.mustDo("POST", "/api/members/"+userID+"/password-link", nil, http.StatusConflict)
		code, _ := field(t, raw, "code").(string)
		return code
	}
	if got := codeOf(owner.userID); got != "password_link_own" {
		t.Errorf("себе: код %q", got)
	}

	elsewhere := a.registerOrg("Ещё одна")
	token := owner.invite(elsewhere.email, "member")
	elsewhere.mustDo("POST", "/api/invites/accept", map[string]any{"token": token}, http.StatusOK)
	if got := codeOf(elsewhere.userID); got != "password_link_elsewhere" {
		t.Errorf("человеку из двух организаций: код %q", got)
	}

	if _, err := a.impl.db.Pool.Exec(context.Background(),
		`update users set oidc_issuer = 'https://provider.test', oidc_subject = $2 where id = $1`,
		member.userID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if got := codeOf(member.userID); got != "password_link_federated" {
		t.Errorf("входящему через провайдера: код %q", got)
	}

	a.session().mustDo("POST", "/api/password-links/lookup",
		map[string]any{"token": "nesushchestvuyushchiy"}, http.StatusNotFound)
}
