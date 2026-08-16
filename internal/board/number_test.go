package board

import (
	"strings"
	"testing"
)

// Номер выдаётся при создании, идёт подряд и несёт ключ своей доски.
func TestCardNumberIsIssuedOnCreate(t *testing.T) {
	f := newFixture(t)

	var got []string
	for _, title := range []string{"Первая", "Вторая", "Третья"} {
		res := f.mustApply("CREATE_CARD",
			map[string]any{"columnId": f.columnA, "title": title})
		got = append(got, res.Patch.Cards[0].Number)
	}

	want := []string{"ДОСК-1", "ДОСК-2", "ДОСК-3"}
	if !equal(got, want) {
		t.Fatalf("номера %v, ожидались %v", got, want)
	}

	// Номер виден и в снимке доски, а не только в ответе операции:
	// человек, открывший доску ссылкой, должен увидеть то же самое.
	for _, c := range f.snapshot().Cards {
		if !strings.HasPrefix(c.Number, "ДОСК-") {
			t.Fatalf("в снимке карточка с номером %q", c.Number)
		}
	}
}

// Выданный номер не возвращается в оборот. Иначе после архивации
// последней карточки следующая получила бы её имя, и две разные задачи
// оказались бы одной — включая ту, что уже упомянута в переписке.
func TestCardNumberIsNotReused(t *testing.T) {
	f := newFixture(t)

	first := f.mustApply("CREATE_CARD",
		map[string]any{"columnId": f.columnA, "title": "Первая"})
	id := first.Patch.Cards[0].ID
	if first.Patch.Cards[0].Number != "ДОСК-1" {
		t.Fatalf("номер %q, ожидался ДОСК-1", first.Patch.Cards[0].Number)
	}

	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": id})

	second := f.mustApply("CREATE_CARD",
		map[string]any{"columnId": f.columnA, "title": "Вторая"})
	if second.Patch.Cards[0].Number != "ДОСК-2" {
		t.Fatalf("номер после архивации %q, ожидался ДОСК-2",
			second.Patch.Cards[0].Number)
	}
}

// Номер неизменен: операции, правящие карточку, его не трогают.
func TestCardNumberSurvivesEdits(t *testing.T) {
	f := newFixture(t)

	created := f.mustApply("CREATE_CARD",
		map[string]any{"columnId": f.columnA, "title": "Смета"})
	id := created.Patch.Cards[0].ID
	number := created.Patch.Cards[0].Number

	f.mustApply("UPDATE_CARD", map[string]any{"cardId": id, "title": "Смета на второй цех"})
	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": f.columnB})

	for _, c := range f.snapshot().Cards {
		if c.ID == id && c.Number != number {
			t.Fatalf("номер сменился с %q на %q", number, c.Number)
		}
	}
}

// Подзадача — обычная карточка, и номер у неё свой, по её доске.
// Подзадача соседней команды заводится операцией на доске той команды
// и получает её номер: номер отвечает на вопрос «чья работа», а делает
// её та команда, на чьей доске карточка лежит. Связь с родителем
// с чужой доски номер не переписывает.
func TestSubtaskKeepsNumberOfItsOwnBoard(t *testing.T) {
	f := newFixture(t)

	neighbour, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Соседняя команда", "")
	if err != nil {
		t.Fatal(err)
	}
	parent := f.createCard("Основная", f.columnA)

	there, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, neighbour.ID)
	if err != nil {
		t.Fatal(err)
	}
	sub := f.applyTo(neighbour.ID, "CREATE_CARD", map[string]any{
		"columnId": there.Columns[0].ID, "title": "Часть работы",
	})
	if sub.Patch.Cards[0].Number != "СОСЕ-1" {
		t.Fatalf("номер на соседней доске %q, ожидался СОСЕ-1", sub.Patch.Cards[0].Number)
	}

	f.mustApply("LINK_CARDS", map[string]any{
		"fromCard": parent,
		"toCard":   sub.Patch.Cards[0].ID,
		"kind":     "subtask",
	})

	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, neighbour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Cards[0].Number != "СОСЕ-1" {
		t.Fatalf("после связывания номер стал %q, а он неизменен", snap.Cards[0].Number)
	}
}
