package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/findias/takt/internal/board"
	"github.com/google/uuid"
)

func TestCardPlanPreservesFactsAndRejectsStaleWrites(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("План")
	boardID := owner.board("Планирование")
	cardID := owner.cardOn(boardID, "Подготовить поставку")
	owner.op(boardID, uuid.NewString(), "UPDATE_CARD", map[string]any{"cardId": cardID, "dueOn": "2026-12-10"})
	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": cardID, "percent": 40})
	empty := map[string]any{"plannedStartOn": nil, "plannedEndOn": nil}
	payload := map[string]any{"cardId": cardID, "plannedStartOn": "2026-11-05", "plannedEndOn": "2026-11-05", "expected": empty}
	opID := uuid.NewString()
	raw := owner.op(boardID, opID, "SET_CARD_PLAN", payload)
	var result board.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Patch.Cards) != 1 {
		t.Fatalf("патч: %s", raw)
	}
	c := result.Patch.Cards[0]
	if c.PlannedStartOn == nil || *c.PlannedStartOn != "2026-11-05" || c.PlannedEndOn == nil || *c.PlannedEndOn != "2026-11-05" {
		t.Fatalf("план: %+v", c)
	}
	if c.DueOn == nil || *c.DueOn != "2026-12-10" || c.Percent == nil || *c.Percent != 40 || c.StartedAt != nil || c.FinishedAt != nil || c.DoneAt != nil {
		t.Fatalf("план изменил факты: %+v", c)
	}
	// Повтор после записи не проверяет старые даты повторно и не пишет событие.
	repeated := owner.op(boardID, opID, "SET_CARD_PLAN", payload)
	if string(repeated) != string(raw) {
		t.Errorf("повтор изменил ответ")
	}
	code, body := owner.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{"operationId": uuid.NewString(), "type": "SET_CARD_PLAN", "payload": payload})
	if code != http.StatusConflict || !strings.Contains(string(body), `"code":"plan_changed"`) {
		t.Fatalf("устаревший план: %d %s", code, body)
	}
	var snapshot struct{ Cards []board.Card }
	if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Cards) != 1 || snapshot.Cards[0].PlannedStartOn == nil {
		t.Fatalf("снимок потерял план")
	}
	current := map[string]any{"plannedStartOn": "2026-11-05", "plannedEndOn": "2026-11-05"}
	payload["expected"] = current
	payload["plannedEndOn"] = "2026-11-04"
	code, _ = owner.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{"operationId": uuid.NewString(), "type": "SET_CARD_PLAN", "payload": payload})
	if code != http.StatusBadRequest {
		t.Fatalf("обратный период: %d", code)
	}
	payload["plannedEndOn"] = "2026-02-30"
	code, _ = owner.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{"operationId": uuid.NewString(), "type": "SET_CARD_PLAN", "payload": payload})
	if code != http.StatusBadRequest {
		t.Fatalf("невозможная дата: %d", code)
	}
	payload["plannedStartOn"], payload["plannedEndOn"] = nil, nil
	owner.op(boardID, uuid.NewString(), "SET_CARD_PLAN", payload)
	events := owner.mustDo("GET", "/api/boards/"+boardID+"/events?cardId="+cardID, nil, http.StatusOK)
	if strings.Count(string(events), `"type":"planned"`) != 2 {
		t.Errorf("история плана: %s", events)
	}
	// Наблюдатель не может править даже полным запросом напрямую.
	viewer := owner.join("viewer")
	payload["expected"] = empty
	code, _ = viewer.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{"operationId": uuid.NewString(), "type": "SET_CARD_PLAN", "payload": payload})
	if code != http.StatusForbidden {
		t.Fatalf("наблюдатель: %d", code)
	}
	stranger := a.registerOrg("Чужая")
	code, _ = stranger.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{"operationId": uuid.NewString(), "type": "SET_CARD_PLAN", "payload": payload})
	if code != http.StatusNotFound {
		t.Fatalf("другая организация: %d", code)
	}
}
