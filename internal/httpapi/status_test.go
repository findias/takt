package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Статус эпика словами (ROADMAP 42.3, срез 10): ставится на портфеле,
// виден на карточке и в истории, не ставится задаче команды и
// наблюдателю; отказы — своими кодами.
func TestEpicStatusIsSetAndKeptAsHistory(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Статусы")
	raw := owner.mustDo("POST", "/api/boards", map[string]any{"name": "Портфель", "template": "portfolio"},
		http.StatusCreated)
	portfolio, _ := field(t, raw, "id").(string)
	epic := owner.cardOn(portfolio, "Переезд")
	path := "/api/boards/" + portfolio + "/cards/" + epic + "/statuses"

	owner.mustDo("POST", path, map[string]any{"status": "on_track"}, http.StatusCreated)
	raw = owner.mustDo("POST", path, map[string]any{"status": "at_risk", "note": "смежники молчат"},
		http.StatusCreated)
	if got, _ := field(t, raw, "author").(string); got == "" {
		t.Errorf("у статуса нет автора: %s", raw)
	}

	// На карточке портфеля — последний статус; история — от нового к старому.
	var snap struct {
		Cards []struct {
			ID     string
			Status *struct{ Status, Note string }
		}
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+portfolio, nil, http.StatusOK), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Cards) != 1 || snap.Cards[0].Status == nil || snap.Cards[0].Status.Status != "at_risk" ||
		snap.Cards[0].Status.Note != "смежники молчат" {
		t.Fatalf("на снимке портфеля не последний статус: %+v", snap.Cards)
	}
	var history struct {
		Statuses []struct{ Status string }
		Kinds    []string
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/cards/"+epic+"/statuses", nil, http.StatusOK), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Statuses) != 2 || history.Statuses[0].Status != "at_risk" || history.Statuses[1].Status != "on_track" {
		t.Errorf("история статусов: %+v, ожидалось at_risk, on_track", history.Statuses)
	}
	if len(history.Kinds) != 3 {
		t.Errorf("список статусов: %v, ожидалось три", history.Kinds)
	}

	// В ленте карточки — событие со словом и пояснением.
	var feed struct {
		Events []struct {
			Type    string
			Payload map[string]any
		}
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+portfolio+"/events?cardId="+epic, nil,
		http.StatusOK), &feed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range feed.Events {
		if e.Type == "status_set" && e.Payload["status"] == "at_risk" && e.Payload["note"] == "смежники молчат" {
			found = true
		}
	}
	if !found {
		t.Errorf("в ленте нет status_set с пояснением: %+v", feed.Events)
	}

	// Слово не из трёх и задача команды — отказы своими кодами.
	refused(t, owner, "POST", path, map[string]any{"status": "fine"}, http.StatusBadRequest, "status_unknown")
	team := owner.board("Команда")
	task := owner.cardOn(team, "Задача")
	refused(t, owner, "POST", "/api/boards/"+team+"/cards/"+task+"/statuses",
		map[string]any{"status": "on_track"}, http.StatusConflict, "status_not_epic")

	// Участник пишет статус чужого эпика: портфель ему открыт на запись.
	member := owner.join("member")
	member.mustDo("POST", path, map[string]any{"status": "off_track"}, http.StatusCreated)
	// Наблюдателю — нет, и история ему видна.
	viewer := owner.join("viewer")
	if code, _ := viewer.do("POST", path, map[string]any{"status": "on_track"}); code != http.StatusForbidden {
		t.Errorf("наблюдатель поставил статус: код %d", code)
	}
	viewer.mustDo("GET", "/api/cards/"+epic+"/statuses", nil, http.StatusOK)
}
