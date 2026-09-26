package board

import (
	"os"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Повтор каждой операции с тем же ключом ничего не меняет
// (PROMPT-TESTING.md, уровень 3).
//
// Повтор держит общий механизм — ключ операции резервируется до её
// выполнения, — и проверен он был на одной CREATE_CARD. Операция,
// которая пишет мимо общего пути (своё событие, своё уведомление,
// своя версия), на повторе сделала бы это второй раз: клиент повторяет
// запрос после обрыва связи, и у человека задвоилось бы действие.
//
// Здесь все операции по очереди, каждая — дважды под одним ключом:
// повтор не падает и не двигает версию доски. Сторож в конце требует,
// чтобы каждая операция из ops.go была в списке: новая операция без
// строки здесь роняет тест.
func TestEveryOperationReplaysWithoutEffect(t *testing.T) {
	f := newFixture(t)
	first := f.createCard("Первая", f.columnA)
	second := f.createCard("Вторая", f.columnA)
	doomed := f.createCard("На удаление", f.columnA)
	label := f.label("Проба повтора")
	field, err := f.svc.CreateField(f.ctx, f.orgID, f.actorID, "Проба повтора", FieldText, nil)
	if err != nil {
		t.Fatal(err)
	}
	it := f.iteration("Итерация повтора")
	later := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)

	refID := func() any {
		snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, f.boardID)
		if err != nil {
			t.Fatal(err)
		}
		refs := snap.CardRefs[first]
		if len(refs) == 0 {
			t.Fatal("ссылка не завелась — нечего убирать")
		}
		return map[string]any{"cardId": first, "refId": refs[0].ID}
	}
	fixed := func(p map[string]any) func() any { return func() any { return p } }
	link := map[string]any{"fromCard": first, "toCard": second, "kind": "relates"}

	steps := []struct {
		op      string
		payload func() any
	}{
		{"CREATE_CARD", fixed(map[string]any{"columnId": f.columnA, "title": "Созданная повтором"})},
		{"UPDATE_CARD", fixed(map[string]any{"cardId": first, "title": "Переименованная"})},
		{"MOVE_CARD", fixed(map[string]any{"cardId": first, "toColumnId": f.columnB, "place": "end"})},
		{"ASSIGN_CARD", fixed(map[string]any{"cardId": first, "userId": f.actorID})},
		{"UNASSIGN_CARD", fixed(map[string]any{"cardId": first, "userId": f.actorID})},
		{"LABEL_CARD", fixed(map[string]any{"cardId": first, "labelId": label.ID})},
		{"UNLABEL_CARD", fixed(map[string]any{"cardId": first, "labelId": label.ID})},
		{"SET_CARD_FIELD", fixed(map[string]any{"cardId": first, "fieldId": field.ID, "value": "Значение"})},
		{"ADD_CARD_REF", fixed(map[string]any{"cardId": first, "kind": RefZNO, "ref": "ЗНО-1"})},
		{"REMOVE_CARD_REF", refID},
		{"ADD_TO_ITERATION", fixed(map[string]any{"cardId": first, "iterationId": it.ID})},
		{"REMOVE_FROM_ITERATION", fixed(map[string]any{"cardId": first, "iterationId": it.ID})},
		{"LINK_CARDS", fixed(link)},
		{"UNLINK_CARDS", fixed(link)},
		{"CREATE_SUBTASK", fixed(map[string]any{"parentCardId": first, "title": "Подзадача повтором"})},
		{"BLOCK_CARD", fixed(map[string]any{"cardId": second, "reason": "ждём"})},
		{"SET_BLOCK_REASON", fixed(map[string]any{"cardId": second, "reason": "ждём иначе"})},
		{"SET_BLOCK_UNTIL", fixed(map[string]any{"cardId": second, "until": later})},
		{"UNBLOCK_CARD", fixed(map[string]any{"cardId": second})},
		{"SET_CARD_DONE", fixed(map[string]any{"cardId": first, "done": true})},
		{"ARCHIVE_CARD", fixed(map[string]any{"cardId": second})},
		{"RESTORE_CARD", fixed(map[string]any{"cardId": second})},
		{"DELETE_CARD", fixed(map[string]any{"cardId": doomed})},
		{"CREATE_COLUMN", fixed(map[string]any{"name": "Колонка повтором"})},
		{"RENAME_COLUMN", fixed(map[string]any{"columnId": f.columnB, "name": "Переименована"})},
		{"UPDATE_COLUMN", fixed(map[string]any{"columnId": f.columnA, "wipLimit": 10})},
		{"MOVE_COLUMN", fixed(map[string]any{"columnId": f.columnA, "place": "after", "afterColumnId": f.columnB})},
		{"SORT_COLUMN", fixed(map[string]any{"columnId": f.columnA, "by": "iteration"})},
	}

	covered := map[string]bool{}
	for _, s := range steps {
		covered[s.op] = true
		payload := s.payload()
		opID := uuid.NewString()
		res, err := f.applyWithID(opID, s.op, payload)
		if err != nil {
			t.Errorf("%s: первая попытка: %v", s.op, err)
			continue
		}
		again, err := f.applyWithID(opID, s.op, payload)
		if err != nil {
			t.Errorf("%s: повтор упал: %v", s.op, err)
			continue
		}
		if again.Version != res.Version {
			t.Errorf("%s: повтор ответил версией %d, первый раз — %d", s.op, again.Version, res.Version)
		}
		snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, f.boardID)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Board.Version != res.Version {
			t.Errorf("%s: после повтора доска на версии %d, а операция оставила %d — повтор что-то сделал",
				s.op, snap.Board.Version, res.Version)
		}
	}

	src, err := os.ReadFile("ops.go")
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, m := range regexp.MustCompile(`case "([A-Z_]+)"`).FindAllSubmatch(src, -1) {
		if !covered[string(m[1])] {
			missing = append(missing, string(m[1]))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("операции без проверки повтора: %v — добавьте их в список", missing)
	}
}
