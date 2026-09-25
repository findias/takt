package board

import (
	"errors"
	"slices"
	"testing"
)

// Карточка, заведённая при отборе по итерации, заводится в эту итерацию
// одной операцией (замечено владельцем 25.09.2026): прежде она заводилась
// вне итерации и тут же пропадала из отобранной доски.
func TestCardIsCreatedStraightIntoAnIteration(t *testing.T) {
	f := newFixture(t)
	it := f.iteration("Спринт 7")

	res := f.mustApply("CREATE_CARD", map[string]any{
		"columnId": f.columnA, "title": "Прямо в спринт", "place": "end", "iterationId": it.ID})
	id := res.Patch.Cards[0].ID
	in, err := f.svc.CardsAt(f.ctx, f.orgID, f.actorID, it.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(in, id) {
		t.Fatalf("карточка не в итерации: %v", in)
	}

	// Закрытая итерация карточку не принимает — и карточки нет вовсе:
	// одна транзакция, а не две операции подряд.
	if err := f.svc.CloseIteration(f.ctx, f.orgID, f.actorID, f.boardID, it.ID); err != nil {
		t.Fatal(err)
	}
	_, err = f.apply("CREATE_CARD", map[string]any{
		"columnId": f.columnA, "title": "В закрытый спринт", "place": "end", "iterationId": it.ID})
	if !errors.Is(err, ErrIterationClosed) {
		t.Fatalf("в закрытую итерацию: ожидался отказ «итерация закрыта», получено %v", err)
	}
	for _, title := range f.titles(f.columnA) {
		if title == "В закрытый спринт" {
			t.Fatal("отказ оставил карточку на доске")
		}
	}
}
