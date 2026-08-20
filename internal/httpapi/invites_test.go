package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Приглашение адресное — и для вошедшего тоже.
//
// Ссылку пересылают: в переписке, в чате, «глянь, куда меня зовут».
// Пока адрес не сверялся, первый открывший её вошедший становился
// участником чужой организации, а владелец видел в списке приглашённых
// не того, кто пришёл.

// invite выписывает приглашение и возвращает его токен.
func (s *session) invite(email, role string) string {
	s.api.t.Helper()
	raw := s.mustDo("POST", "/api/invites",
		map[string]any{"email": email, "role": role}, http.StatusCreated)
	link, _ := field(s.api.t, raw, "link").(string)
	if link == "" {
		s.api.t.Fatalf("приглашение без ссылки: %s", raw)
	}
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")
	return parts[len(parts)-1]
}

func TestInviteBelongsToTheAddressItNames(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Куда зовут")
	guest := "gost-" + uuid.NewString()[:8] + "@example.test"
	token := owner.invite(guest, "member")

	// Ссылку переслали, и открыл её другой человек — со своей учётной
	// записью и своей организацией.
	stranger := a.registerOrg("Своя контора")
	code, raw := stranger.do("POST", "/api/invites/"+token+"/accept", nil)
	if code != http.StatusForbidden {
		t.Fatalf("чужое приглашение принято: код %d, тело: %s", code, raw)
	}
	// Клиент различает этот случай по коду: ему есть что предложить —
	// выйти и войти под тем адресом.
	if got, _ := field(t, raw, "code").(string); got != "invite_other_email" {
		t.Errorf("код отказа %q, ожидался invite_other_email; тело: %s", got, raw)
	}
	// Отказ называет оба адреса: без них непонятно, что именно не сошлось.
	text, _ := field(t, raw, "error").(string)
	if !strings.Contains(text, guest) || !strings.Contains(text, stranger.email) {
		t.Errorf("отказ не называет адреса: %s", text)
	}

	// И главное: он не оказался в чужой организации.
	me := stranger.mustDo("GET", "/api/me", nil, http.StatusOK)
	if name, _ := field(t, me, "orgName").(string); name != "Моя команда" {
		t.Errorf("посторонний сменил организацию на %q", name)
	}
	orgs := stranger.mustDo("GET", "/api/orgs", nil, http.StatusOK)
	if list, _ := field(t, orgs, "orgs").([]any); len(list) != 1 {
		t.Errorf("у постороннего стало %d организаций, ожидалась одна", len(list))
	}

	// Приглашение при этом цело: тот, кому оно выписано, принимает его.
	invited := a.session()
	invited.mustDo("POST", "/api/invites/"+token+"/accept",
		map[string]any{"name": "Гость", "password": "parol12345"}, http.StatusOK)
}

// Второй путь того же правила: аккаунт уже есть, человек вошёл под ним,
// и почта сходится — приглашение принимается без заведения второго
// аккаунта.
func TestInviteAcceptedByTheSamePersonSignedIn(t *testing.T) {
	a := newAPI(t)
	first := a.registerOrg("Первая")
	second := a.registerOrg("Вторая")

	token := first.invite(second.email, "member")
	second.mustDo("POST", "/api/invites/"+token+"/accept", nil, http.StatusOK)

	var me struct{ OrgName string }
	_ = json.Unmarshal(second.mustDo("GET", "/api/me", nil, http.StatusOK), &me)
	orgs := second.mustDo("GET", "/api/orgs", nil, http.StatusOK)
	if list, _ := field(t, orgs, "orgs").([]any); len(list) != 2 {
		t.Errorf("организаций стало %d, ожидалось две; тело: %s", len(list), orgs)
	}
}
