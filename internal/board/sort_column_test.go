package board

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Колонка упорядочивается по итерации одной операцией (ROADMAP 34.11):
// итерации по порядку начала, без итерации — в конце, а внутри одной
// итерации остаётся ручной порядок.
func TestColumnIsOrderedByIteration(t *testing.T) {
	f := newFixture(t)
	first := f.iteration("Спринт 1")
	second := f.iteration("Спринт 2")
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `update iterations set starts_on = starts_on + 14, ends_on = ends_on + 14
		                           where id = $1`, second.ID)
		return err
	})
	ids := map[string]string{}
	for _, c := range []struct{ title, iteration string }{
		{"Без итерации", ""},
		{"Во втором", second.ID},
		{"В первом, раньше", first.ID},
		{"В первом, позже", first.ID},
	} {
		id := f.createCard(c.title, f.columnA)
		ids[c.title] = id
		if c.iteration != "" {
			f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": id, "iterationId": c.iteration})
		}
	}

	res := f.mustApply("SORT_COLUMN", map[string]any{"columnId": f.columnA, "by": "iteration"})

	want := []string{"В первом, раньше", "В первом, позже", "Во втором", "Без итерации"}
	if got := f.titles(f.columnA); !slices.Equal(got, want) {
		t.Errorf("порядок после упорядочивания: %v, ждали %v", got, want)
	}
	if len(res.Patch.Cards) != len(want) {
		t.Errorf("патч несёт %d карточек, ждали %d", len(res.Patch.Cards), len(want))
	}
	// Порядок дальше ручной: перестановка после упорядочивания работает.
	f.mustApply("MOVE_CARD", map[string]any{"cardId": ids["Без итерации"],
		"toColumnId": f.columnA, "place": "start"})
	if got := f.titles(f.columnA)[0]; got != "Без итерации" {
		t.Errorf("после ручной перестановки первой стоит %q", got)
	}

	if _, err := f.apply("SORT_COLUMN", map[string]any{"columnId": f.columnA, "by": "title"}); err == nil {
		t.Error("незнакомый способ упорядочить принят молча")
	}
}
