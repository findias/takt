package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Поздний повтор не должен отменять новое решение и заводить ещё одну
// карточку: тело осталось прежним, но между запросами ответ изменили.
func TestIdeaDecisionRequestIDSurvivesInterveningDecisions(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Поздний повтор решения")
	author := owner.join("viewer")
	id := author.idea("Приняли, затем отложили", nil)
	boardID := owner.board("Работа")
	takenID, laterID := uuid.NewString(), uuid.NewString()
	taken := map[string]any{"decision": "taken", "boardId": boardID, "requestId": takenID}
	later := map[string]any{"decision": "later", "note": "сначала согласование", "requestId": laterID}
	first := owner.decide(id, taken, http.StatusCreated)
	owner.decide(id, later, http.StatusCreated)
	replayed := owner.decide(id, taken, http.StatusCreated)
	if replayed.Decision == nil || replayed.Decision.Kind != "later" {
		t.Errorf("поздний повтор подменил новое решение: %+v", replayed.Decision)
	}
	if n := len(ideaDecisionHistory(t, owner, id)); n != 2 {
		t.Errorf("повтор записал новую историю: %d вместо двух", n)
	}
	if cards := ideaBoardCards(t, owner, boardID); len(cards) != 1 || cards[0].ID != first.Decision.Card.ID {
		t.Errorf("повтор завёл вторую карточку: %+v", cards)
	}
	if n := len(author.notifications().Items); n != 2 {
		t.Errorf("повтор прислал уведомление: %d вместо двух", n)
	}
	owner.decide(id, map[string]any{
		"decision": "taken", "boardId": boardID, "note": "другая команда", "requestId": takenID,
	}, http.StatusConflict)
	owner.decide(id, map[string]any{"decision": "later", "requestId": "не UUID"}, http.StatusBadRequest)
	// Новое намерение с тем же содержимым получает новый идентификатор.
	taken["requestId"] = uuid.NewString()
	owner.decide(id, taken, http.StatusCreated)
	if current := owner.decide(id, later, http.StatusCreated); current.Decision == nil || current.Decision.Kind != "taken" {
		t.Errorf("старое «позже» подменило нынешнее «берём»: %+v", current.Decision)
	}
	if n := len(ideaDecisionHistory(t, owner, id)); n != 3 {
		t.Errorf("новое намерение или повтор: в истории %d вместо трёх", n)
	}
	// Тот же UUID в другой организации не открывает чужой ответ
	// и не мешает её собственной команде.
	stranger := a.registerOrg("Чужая организация")
	other := stranger.idea("Другая идея", nil)
	stranger.decide(other, map[string]any{"decision": "later", "requestId": laterID}, http.StatusCreated)
	stranger.decide(id, later, http.StatusNotFound)
}
