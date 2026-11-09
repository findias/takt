package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/findias/takt/internal/board"
	"github.com/google/uuid"
)

func TestVacationWarningDaysAreSharedByBoardAndRespectAccess(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Предупреждения")
	member, viewer := owner.join("member"), owner.join("viewer")
	b := owner.board("Работа")
	other := owner.board("Соседняя")
	card := owner.cardOn(b, "Передать дела")
	owner.op(b, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": card, "userId": owner.userID})
	path := "/api/boards/" + b + "/vacation-warning-days"
	read := func(want *int) {
		t.Helper()
		var snapshot struct{ Board board.Info }
		if err := json.Unmarshal(viewer.mustDo("GET", "/api/boards/"+b, nil, 200), &snapshot); err != nil {
			t.Fatal(err)
		}
		got := snapshot.Board.VacationWarningDays
		if (got == nil) != (want == nil) || got != nil && *got != *want {
			t.Fatalf("порог в снимке: %v, ожидалось %v", got, want)
		}
		var list board.TaskList
		if err := json.Unmarshal(viewer.mustDo("GET", "/api/tasks?user="+owner.userID, nil, 200), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Tasks) != 1 {
			t.Fatalf("задачи: %+v", list)
		}
		got = list.Tasks[0].VacationWarningDays
		if (got == nil) != (want == nil) || got != nil && *got != *want {
			t.Fatalf("порог в задачах: %v, ожидалось %v", got, want)
		}
	}
	read(nil)
	for _, days := range []int{7, 0, 365} {
		member.mustDo("PUT", path, map[string]any{"days": days}, http.StatusNoContent)
		read(&days)
	}
	for _, days := range []any{-1, 366, 1.5, "7"} {
		owner.mustDo("PUT", path, map[string]any{"days": days}, http.StatusBadRequest)
	}
	viewer.mustDo("PUT", path, map[string]any{"days": 1}, http.StatusForbidden)
	stranger := a.registerOrg("Посторонние")
	stranger.mustDo("PUT", path, map[string]any{"days": 1}, http.StatusNotFound)
	// Закрытая доска не становится доступной из-за новой настройки.
	owner.mustDo("PUT", "/api/boards/"+other+"/access", map[string]any{"visibility": "private"}, http.StatusNoContent)
	if code, _ := member.do("PUT", "/api/boards/"+other+"/vacation-warning-days", map[string]any{"days": 1}); code != 403 && code != 404 {
		t.Fatalf("настройка закрытой доски: %d", code)
	}
	var snapshot struct{ Board board.Info }
	json.Unmarshal(owner.mustDo("GET", "/api/boards/"+other, nil, 200), &snapshot)
	if snapshot.Board.VacationWarningDays != nil {
		t.Fatal("порог изменился на другой доске")
	}
	owner.mustDo("PUT", path, map[string]any{"days": nil}, http.StatusNoContent)
	read(nil)
}
