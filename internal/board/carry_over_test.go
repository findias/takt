package board

import (
	"slices"
	"testing"
)

// Незакрытую карточку закрытой итерации переносят в открытую
// (владелец 25.09.2026): закрытая итерация больше «не отпускала»
// карточки, и хвост спринта застревал в нём. Отчёт закрытой при этом
// не меняется — он на момент закрытия.
func TestOpenCardMovesFromAClosedIterationToAnOpenOne(t *testing.T) {
	f := newFixture(t)
	old := f.iteration("Спринт 8")
	id := f.createCard("Хвост спринта", f.columnA)
	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": id, "iterationId": old.ID})
	if err := f.svc.CloseIteration(f.ctx, f.orgID, f.actorID, f.boardID, old.ID); err != nil {
		t.Fatal(err)
	}
	before, err := f.svc.IterationReport(f.ctx, f.orgID, f.actorID, f.boardID, old.ID)
	if err != nil {
		t.Fatal(err)
	}

	next := f.iteration("Спринт 9")
	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": id, "iterationId": next.ID})

	in, err := f.svc.CardsAt(f.ctx, f.orgID, f.actorID, next.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(in, id) {
		t.Fatalf("карточка не перешла в открытую итерацию: %v", in)
	}
	after, err := f.svc.IterationReport(f.ctx, f.orgID, f.actorID, f.boardID, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Cards) != len(before.Cards) || after.Totals != before.Totals {
		t.Errorf("отчёт закрытой итерации изменился: было %+v, стало %+v", before.Totals, after.Totals)
	}
}
