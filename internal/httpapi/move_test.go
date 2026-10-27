package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Перенос карточки на другую доску (ROADMAP 40) через HTTP: предпросмотр
// называет колонки назначения, сама операция идёт обычным путём,
// а отказы различимы — нет карточки и нет доски назначения звучат
// по-разному.
func TestCardMovesToAnotherBoardOverHTTP(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Перенос")
	from := field(t, owner.mustDo("POST", "/api/boards",
		map[string]any{"name": "Ядро"}, http.StatusCreated), "id").(string)
	to := field(t, owner.mustDo("POST", "/api/boards",
		map[string]any{"name": "Поставки"}, http.StatusCreated), "id").(string)
	snap := owner.mustDo("GET", "/api/boards/"+from, nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)
	made := owner.mustDo("POST", "/api/boards/"+from+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Перевезти стенд"},
	}, http.StatusOK)
	cardID := field(t, made, "patch", "cards").([]any)[0].(map[string]any)["id"].(string)

	preview := owner.mustDo("GET",
		"/api/boards/"+from+"/cards/"+cardID+"/move-preview?to="+to, nil, http.StatusOK)
	columns := field(t, preview, "columns").([]any)
	if len(columns) == 0 {
		t.Fatalf("предпросмотр без колонок назначения: %s", preview)
	}
	target := columns[len(columns)-1].(map[string]any)["id"].(string)

	if code, body := owner.do("GET",
		"/api/boards/"+from+"/cards/"+cardID+"/move-preview?to="+uuid.NewString(), nil); code != http.StatusNotFound ||
		!strings.Contains(string(body), "куда переносить") {
		t.Errorf("пропавшая доска назначения: код %d, тело %s", code, body)
	}
	if code, body := owner.do("GET",
		"/api/boards/"+from+"/cards/"+cardID+"/move-preview", nil); code != http.StatusBadRequest {
		t.Errorf("без параметра to: код %d, тело %s", code, body)
	}

	moved := owner.mustDo("POST", "/api/boards/"+from+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "MOVE_CARD_TO_BOARD",
		"payload":     map[string]any{"cardId": cardID, "boardId": to, "toColumnId": target},
	}, http.StatusOK)
	if removed := field(t, moved, "patch", "removedCardIds").([]any); len(removed) != 1 || removed[0] != cardID {
		t.Errorf("патч не убирает карточку со своей доски: %s", moved)
	}
	card := owner.mustDo("GET", "/api/boards/"+to+"/cards/"+cardID, nil, http.StatusOK)
	if field(t, card, "columnId") != target {
		t.Errorf("карточка не в выбранной колонке: %s", card)
	}
	if code, body := owner.do("GET",
		"/api/boards/"+from+"/cards/"+cardID+"/move-preview?to="+to, nil); code != http.StatusNotFound ||
		!strings.Contains(string(body), "карточка не найдена") {
		t.Errorf("карточки на прежней доске уже нет: код %d, тело %s", code, body)
	}
}
