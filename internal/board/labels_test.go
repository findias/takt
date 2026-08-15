package board

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Метки.
//
// Проверяется то, из-за чего они устроены именно так: определение живёт
// в организации (иначе фильтр собирать не из чего), убранная метка
// не исчезает с карточек (иначе история становится неверной), и повтор
// действия безобиден — метку вешают и снимают чаще, чем что-либо ещё.

func (f *fixture) label(name string) Label {
	f.t.Helper()
	l, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID, name, "green")
	if err != nil {
		f.t.Fatalf("создание метки: %v", err)
	}
	return l
}

func (f *fixture) labelsOf(cardID string) []string {
	f.t.Helper()
	return f.snapshot().CardLabels[cardID]
}

func TestLabelIsHungAndRemoved(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Помеченная", f.columnA)
	urgent := f.label("Срочно")

	f.mustApply("LABEL_CARD", map[string]any{"cardId": cardID, "labelId": urgent.ID})
	if got := f.labelsOf(cardID); len(got) != 1 || got[0] != urgent.ID {
		t.Fatalf("метка не повесилась: %v", got)
	}

	// Повтор безобиден: метку вешают из меню, и нажать дважды — обычное
	// дело.
	f.mustApply("LABEL_CARD", map[string]any{"cardId": cardID, "labelId": urgent.ID})
	if got := f.labelsOf(cardID); len(got) != 1 {
		t.Errorf("повтор задвоил метку: %v", got)
	}

	f.mustApply("UNLABEL_CARD", map[string]any{"cardId": cardID, "labelId": urgent.ID})
	if got := f.labelsOf(cardID); len(got) != 0 {
		t.Errorf("метка не снялась: %v", got)
	}
	// И снятие повторяется без последствий.
	f.mustApply("UNLABEL_CARD", map[string]any{"cardId": cardID, "labelId": urgent.ID})
}

func TestLabelNamesAreUniqueInTheOrganisation(t *testing.T) {
	f := newFixture(t)
	f.label("Срочно")

	_, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID, "срочно", "rose")
	if !errors.Is(err, ErrLabelExists) {
		t.Errorf("вторая «срочно» заведена, ошибка: %v", err)
	}

	// Пустое название — не метка.
	if _, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID, "   ", "green"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("метка без названия принята, ошибка: %v", err)
	}
	// Оттенок берётся из закрытого набора: сырой цвет в тёмной теме
	// начал бы светиться.
	if _, err := f.svc.CreateLabel(f.ctx, f.orgID, f.actorID, "Своя", "#ff0000"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("выдуманный оттенок принят, ошибка: %v", err)
	}
}

// Убранная метка перестаёт предлагаться, но остаётся там, где уже висит:
// карточка, помеченная полгода назад, объясняет этим своё время
// в очереди.
func TestArchivedLabelStaysOnCards(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Со старой меткой", f.columnA)
	old := f.label("Устаревшая")
	f.mustApply("LABEL_CARD", map[string]any{"cardId": cardID, "labelId": old.ID})

	if err := f.svc.ArchiveLabel(f.ctx, f.orgID, f.actorID, old.ID); err != nil {
		t.Fatal(err)
	}

	// В словаре её больше нет — вешать нечего.
	labels, err := f.svc.Labels(f.ctx, f.orgID, f.actorID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range labels {
		if l.ID == old.ID {
			t.Error("убранная метка всё ещё предлагается")
		}
	}
	// А на карточке — осталась.
	if got := f.labelsOf(cardID); len(got) != 1 || got[0] != old.ID {
		t.Errorf("история переписана: метка исчезла с карточки, %v", got)
	}
	// И повесить её заново уже нельзя.
	if _, err := f.apply("LABEL_CARD", map[string]any{
		"cardId": f.createCard("Новая", f.columnA), "labelId": old.ID,
	}); err == nil {
		t.Error("убранная метка повесилась на новую карточку")
	}
}

func TestLabelOfAnotherOrganisationIsNotFound(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	cardID := f.createCard("Своя", f.columnA)
	foreign := other.label("Чужая")

	if _, err := f.apply("LABEL_CARD", map[string]any{
		"cardId": cardID, "labelId": foreign.ID,
	}); err == nil {
		t.Error("повесилась метка чужой организации")
	}
	if got := f.labelsOf(cardID); len(got) != 0 {
		t.Errorf("на карточке чужая метка: %v", got)
	}

	// И несуществующая метка тоже не вешается.
	if _, err := f.apply("LABEL_CARD", map[string]any{
		"cardId": cardID, "labelId": uuid.NewString(),
	}); err == nil {
		t.Error("повесилась несуществующая метка")
	}
}

// Словарь меток приезжает со снимком: иначе метка на карточке осталась бы
// идентификатором.
func TestSnapshotCarriesLabelDictionary(t *testing.T) {
	f := newFixture(t)
	urgent := f.label("Срочно")

	snap := f.snapshot()
	found := false
	for _, l := range snap.Labels {
		if l.ID == urgent.ID {
			found = true
			if l.Name != "Срочно" || l.Tone != "green" {
				t.Errorf("метка приехала испорченной: %+v", l)
			}
		}
	}
	if !found {
		t.Error("метки нет в снимке — показывать нечего")
	}
}

// Метка обязана доезжать до соседа догоном, а не только перезагрузкой:
// патч без неё означает, что второй открытый браузер узнаёт о метке
// случайно.
func TestLabelChangeTravelsInThePatch(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Помечу", f.columnA)
	urgent := f.label("Срочно")

	res := f.mustApply("LABEL_CARD", map[string]any{"cardId": cardID, "labelId": urgent.ID})
	if got := res.Patch.CardLabels[cardID]; len(got) != 1 || got[0] != urgent.ID {
		t.Fatalf("патч без метки: %+v", res.Patch)
	}

	// Снятие — тоже событие, и в патче оно выглядит как «теперь пусто»,
	// а не как «удалили такую-то»: такой патч можно применить дважды.
	res = f.mustApply("UNLABEL_CARD", map[string]any{"cardId": cardID, "labelId": urgent.ID})
	if got, ok := res.Patch.CardLabels[cardID]; !ok || len(got) != 0 {
		t.Errorf("патч снятия: %+v", res.Patch)
	}
}
