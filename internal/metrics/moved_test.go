package metrics

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/board"
)

// Ушедшая на другую доску карточка (ROADMAP 40) уносит своё время цикла
// туда, а прежняя доска называет, сколько работы ушло: молча пропавшая
// работа выглядела бы работой, которой не было.
func TestMovedOutCardsAreNamedNotCounted(t *testing.T) {
	f := newFixture(t)
	boards := board.New(f.db)
	other, err := boards.Create(f.ctx, f.orgID, f.userID, "Соседи", "")
	if err != nil {
		t.Fatal(err)
	}
	gone := f.card("Ушла к соседям", 5, days(2), done())
	f.card("Осталась", 3, days(1), done())

	raw, _ := json.Marshal(map[string]any{"cardId": gone, "boardId": other.ID})
	if _, err := boards.Apply(f.ctx, f.orgID, f.userID, f.boardID, board.Request{
		OperationID: uuid.NewString(), Type: "MOVE_CARD_TO_BOARD", Payload: raw}); err != nil {
		t.Fatal(err)
	}

	report, err := f.svc.Report(f.ctx, f.orgID, f.userID, f.boardID, 90)
	if err != nil {
		t.Fatal(err)
	}
	if report.MovedOut != 1 {
		t.Errorf("ушедших %d, ожидалась одна", report.MovedOut)
	}
	if report.CycleTime == nil || report.CycleTime.Count != 1 {
		t.Errorf("ушедшая карточка осталась во времени цикла прежней доски: %+v", report.CycleTime)
	}
}
