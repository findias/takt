package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Владелец подразделения читает журнал своей ветки (этап 31, 0075):
// события узлов, состава, наблюдения, приглашений, назначений и досок
// своего поддерева и то, что сделал сам, — но не соседнюю ветку и не
// записи уровня организации.
func TestSubdivisionOwnerReadsItsSubtreeAudit(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	dev := owner.team("Разработка", nil)
	sales := owner.team("Продажи", nil)
	head := owner.join("member")
	owner.mustDo("POST", "/api/team-admins",
		map[string]any{"userId": head.userID, "teamId": dev}, http.StatusCreated)

	// Чужими руками в своей ветке и в соседней.
	core := owner.team("Ядро", dev)
	worker := owner.join("member")
	owner.mustDo("PUT", "/api/teams/"+core+"/members/"+worker.userID, nil, http.StatusNoContent)
	owner.mustDo("PUT", "/api/teams/"+sales+"/members/"+worker.userID, nil, http.StatusNoContent)
	owner.mustDo("PATCH", "/api/teams/"+sales, map[string]any{"name": "Продажи и сбыт"}, http.StatusNoContent)
	board := owner.board("Доска ядра")
	owner.mustDo("PUT", "/api/boards/"+board+"/access",
		map[string]any{"visibility": "team", "teamId": core}, http.StatusNoContent)
	// Своими — отдел в своей ветке.
	head.team("Своя группа", dev)

	type entry struct {
		Subject   string          `json:"subject"`
		SubjectID *string         `json:"subjectId"`
		Payload   json.RawMessage `json:"payload"`
	}
	read := func(s *session) []entry {
		t.Helper()
		var page struct {
			Entries []entry `json:"entries"`
		}
		if err := json.Unmarshal(s.mustDo("GET", "/api/audit", nil, http.StatusOK), &page); err != nil {
			t.Fatal(err)
		}
		return page.Entries
	}
	// Узел записи — тот же, по которому решает политика.
	teamOf := func(e entry) map[string]bool {
		var p struct {
			New, Old map[string]any
		}
		_ = json.Unmarshal(e.Payload, &p)
		out := map[string]bool{}
		for _, row := range []map[string]any{p.New, p.Old} {
			for _, key := range []string{"team_id", "id"} {
				if v, ok := row[key].(string); ok && (key == "team_id" || e.Subject == "teams") {
					out[v] = true
				}
			}
		}
		return out
	}

	seen := map[string]bool{}
	for _, e := range read(head) {
		seen[e.Subject] = true
		if teamOf(e)[sales] {
			t.Errorf("владелец «Разработки» видит соседнюю ветку: %s %s", e.Subject, e.Payload)
		}
		if e.Subject == "memberships" || e.Subject == "users" {
			// Своё — видно: так он вошёл в организацию. Чужое — нет.
			var p struct{ New, Old map[string]any }
			_ = json.Unmarshal(e.Payload, &p)
			if p.New["user_id"] != head.userID && p.Old["user_id"] != head.userID {
				t.Errorf("владелец подразделения видит запись уровня организации: %s", e.Payload)
			}
		}
	}
	for _, want := range []string{"teams", "team_members", "team_admins", "boards"} {
		if !seen[want] {
			t.Errorf("в журнале ветки нет записей %q: %v", want, seen)
		}
	}

	// Рядовой участник по-прежнему не видит ничего.
	if got := len(read(worker)); got != 0 {
		t.Errorf("рядовой участник видит %d записей журнала", got)
	}
}
