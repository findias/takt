package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Метки по HTTP: отказы различимы кодом, а не текстом, и каждый говорит,
// что делать, — клиент ведёт по коду, человек читает текст.
func TestLabelsOverHTTP(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Метки по областям")
	created := owner.mustDo("POST", "/api/boards", map[string]any{"name": "Склад"}, http.StatusCreated)
	boardID := field(t, created, "id").(string)

	// Где можно завести: организация и доска — подразделений ещё нет.
	var list struct {
		Labels []struct {
			ID        string `json:"id"`
			Scope     string `json:"scope"`
			ScopeName string `json:"scopeName"`
			Archived  bool   `json:"archived"`
			CanManage bool   `json:"canManage"`
		} `json:"labels"`
		Places []struct {
			Scope string `json:"scope"`
			ID    string `json:"id"`
		} `json:"places"`
	}
	raw := owner.mustDo("GET", "/api/labels", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	var scopes []string
	for _, p := range list.Places {
		scopes = append(scopes, p.Scope)
	}
	if strings.Join(scopes, ",") != "org,board" {
		t.Errorf("места для меток: %s", raw)
	}

	own := owner.mustDo("POST", "/api/labels",
		map[string]any{"name": "Приёмка", "tone": "teal", "boardId": boardID}, http.StatusCreated)
	ownID := field(t, own, "id").(string)
	if field(t, own, "scope") != "board" || field(t, own, "scopeName") != "Склад" {
		t.Errorf("метка доски без происхождения: %s", own)
	}

	// Одноимённая шире — конфликт, и текст называет, где уже есть.
	code, body := owner.do("POST", "/api/labels", map[string]any{"name": "приёмка"})
	if code != http.StatusConflict || !strings.Contains(string(body), "Склад") {
		t.Errorf("одноимённая метка организации: код %d, %s", code, body)
	}

	// Убрать, вернуть — и пропавшая метка называется меткой, а не доской.
	owner.mustDo("DELETE", "/api/labels/"+ownID, nil, http.StatusNoContent)
	raw = owner.mustDo("GET", "/api/labels", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Labels) != 1 || !list.Labels[0].Archived || !list.Labels[0].CanManage {
		t.Errorf("убранная метка в списке: %s", raw)
	}
	owner.mustDo("POST", "/api/labels/"+ownID+"/restore", nil, http.StatusNoContent)
	code, body = owner.do("POST", "/api/labels/"+uuid.NewString()+"/restore", nil)
	if code != http.StatusNotFound || !strings.Contains(string(body), "метка") {
		t.Errorf("возврат несуществующей: код %d, %s", code, body)
	}

	// Метку чужой доски на эту не повесить, и отказ это объясняет.
	other := owner.mustDo("POST", "/api/boards", map[string]any{"name": "Соседи"}, http.StatusCreated)
	snap := owner.mustDo("GET", "/api/boards/"+field(t, other, "id").(string), nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)
	made := owner.mustDo("POST", "/api/boards/"+field(t, other, "id").(string)+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Чужая", "place": "end"},
	}, http.StatusOK)
	cardID := field(t, made, "patch", "cards").([]any)[0].(map[string]any)["id"].(string)
	code, body = owner.do("POST", "/api/boards/"+field(t, other, "id").(string)+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "LABEL_CARD",
		"payload":     map[string]any{"cardId": cardID, "labelId": ownID},
	})
	if code != http.StatusConflict || !strings.Contains(string(body), "не действует") {
		t.Errorf("метка чужой доски: код %d, %s", code, body)
	}
}
