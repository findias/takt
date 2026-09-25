package board

import "testing"

// Колонка переставляется, и карточки едут с ней (ROADMAP 34.12).
func TestColumnMovesWithItsCards(t *testing.T) {
	f := newFixture(t)
	order := func() []string {
		out := []string{}
		for _, c := range f.columns() {
			out = append(out, c.ID)
		}
		return out
	}
	start := order()
	if len(start) < 3 {
		t.Fatalf("у доски %d колонок, для проверки нужно три", len(start))
	}
	a, b, c := start[0], start[1], start[2]
	card := f.createCard("Едет с колонкой", a)

	f.mustApply("MOVE_COLUMN", map[string]any{"columnId": a, "place": "after", "afterColumnId": b})
	if got := order(); got[0] != b || got[1] != a || got[2] != c {
		t.Errorf("после переноса за вторую: %v", got)
	}
	if got := f.card(card).ColumnID; got != a {
		t.Errorf("карточка уехала из своей колонки: %s", got)
	}

	f.mustApply("MOVE_COLUMN", map[string]any{"columnId": c, "place": "start"})
	if got := order(); got[0] != c {
		t.Errorf("после переноса в начало первой стоит %s", got[0])
	}
	f.mustApply("MOVE_COLUMN", map[string]any{"columnId": c, "place": "end"})
	if got := order(); got[len(got)-1] != c {
		t.Errorf("после переноса в конец последней стоит %s", got[len(got)-1])
	}

	if _, err := f.apply("MOVE_COLUMN", map[string]any{"columnId": a, "place": "after", "afterColumnId": a}); err == nil {
		t.Error("колонка встала за саму себя")
	}
	if _, err := f.apply("MOVE_COLUMN", map[string]any{"columnId": a, "place": "middle"}); err == nil {
		t.Error("незнакомое место принято молча")
	}
}
