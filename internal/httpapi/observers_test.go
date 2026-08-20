package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Права на структуру: кто выдал наблюдение, тот его и снимает.
//
// Маршрутов администратора подразделения не было ни в одной проверке
// HTTP-слоя, хотя политика его полномочия описывает с этапа 4.1 —
// и ровно там HTTP разошёлся с политикой: выдачу она разрешала,
// а отзыв маршрут требовал от владельца.

// member заводит человека в организации через приглашение.
func (a *api) member(owner *session, role string) *session {
	a.t.Helper()
	s := a.session()
	s.email = "chelovek-" + uuid.NewString()[:8] + "@example.test"
	inv := owner.mustDo("POST", "/api/invites",
		map[string]any{"email": s.email, "role": role}, http.StatusCreated)
	link, _ := field(a.t, inv, "link").(string)
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")
	s.mustDo("POST", "/api/invites/accept", map[string]any{
		"token": parts[len(parts)-1], "name": "Человек", "password": "parol12345",
	}, http.StatusOK)
	var me struct{ ID string }
	_ = json.Unmarshal(s.mustDo("GET", "/api/me", nil, http.StatusOK), &me)
	s.userID = me.ID
	return s
}

func TestSubtreeAdminRevokesWhatItGranted(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Дерево")
	dev := owner.team("Разработка", nil)
	head := a.member(owner, "member")
	watcher := a.member(owner, "viewer")
	stranger := a.member(owner, "member")

	owner.mustDo("POST", "/api/team-admins",
		map[string]any{"userId": head.userID, "teamId": dev}, http.StatusCreated)

	granted := head.mustDo("POST", "/api/observers",
		map[string]any{"userId": watcher.userID, "teamId": dev}, http.StatusCreated)
	obsID, _ := field(t, granted, "id").(string)

	// Посторонний участник не снимает чужое — и слышит «нельзя»,
	// а не «не найдено»: запись он видит в списке.
	code, raw := stranger.do("DELETE", "/api/observers/"+obsID, nil)
	if code != http.StatusForbidden {
		t.Fatalf("посторонний снял наблюдение: код %d, тело: %s", code, raw)
	}
	if text, _ := field(t, raw, "error").(string); !strings.Contains(text, "администратор") {
		t.Errorf("отказ не называет, кто может: %s", raw)
	}

	// А тот, кто выдал, — снимает.
	head.mustDo("DELETE", "/api/observers/"+obsID, nil, http.StatusNoContent)
}

// Люди ключу не отдаются вовсе: в дереве подразделений почт нет,
// а в составе организации, у наблюдателей и у администраторов — есть.
func TestKeyGetsNoPeople(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Ключи и люди")

	tokenFor := func(name string, scopes ...string) string {
		raw := owner.mustDo("POST", "/api/clients",
			map[string]any{"name": name, "scopes": scopes}, http.StatusCreated)
		token, _ := field(t, raw, "token").(string)
		if token == "" {
			t.Fatalf("ключ без токена: %s", raw)
		}
		return token
	}
	get := func(token, path string) int {
		req, err := http.NewRequest("GET", a.server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := a.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	boards := tokenFor("только доски", "boards:read")
	structure := tokenFor("структура", "structure:read")

	for _, path := range []string{"/api/team", "/api/observers", "/api/team-admins"} {
		for name, token := range map[string]string{"boards:read": boards, "structure:read": structure} {
			if code := get(token, path); code != http.StatusForbidden {
				t.Errorf("ключ %s получил %s: код %d, ожидался 403", name, path, code)
			}
		}
	}
	// Дерево подразделений ключу открыто — но своим разрешением
	// и без единой почты.
	if code := get(structure, "/api/teams"); code != http.StatusOK {
		t.Errorf("ключ со structure:read не получил дерево: код %d", code)
	}
	if code := get(boards, "/api/teams"); code != http.StatusForbidden {
		t.Errorf("ключ без structure:read получил дерево: код %d", code)
	}

	// Человеку разрешений не выдают: он ограничен ролью, и состав
	// команды видит любой участник.
	owner.mustDo("GET", "/api/team", nil, http.StatusOK)
}
