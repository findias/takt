package board

import "testing"

// Карточка в патче переноса и правки — целиком: с прогрессом подзадач,
// блокировкой и эпиком. Клиент заменяет карточку тем, что пришло,
// и голая выборка стирала блок подзадач и блокировку до перезагрузки
// страницы (замечено владельцем 25.09.2026).
func TestPatchedCardKeepsProgressBlockAndEpic(t *testing.T) {
	f := newFixture(t)
	pf, epic := f.portfolio()
	parent := f.partOn(pf, epic, "Фича с задачами", f.boardID)
	f.sub(parent, "Первая")
	f.sub(parent, "Вторая")
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": parent, "reason": "ждём доступ"})

	check := func(what string, res Result) {
		t.Helper()
		for _, c := range res.Patch.Cards {
			if c.ID != parent {
				continue
			}
			if c.Progress == nil || c.Progress.Total != 2 {
				t.Errorf("%s: прогресс подзадач пропал: %+v", what, c.Progress)
			}
			if c.Blocked == nil || c.Blocked.Reason != "ждём доступ" {
				t.Errorf("%s: блокировка пропала: %+v", what, c.Blocked)
			}
			if c.Epic == nil {
				t.Errorf("%s: эпик пропал", what)
			}
			return
		}
		t.Fatalf("%s: карточки нет в патче", what)
	}

	check("перенос", f.mustApply("MOVE_CARD", map[string]any{"cardId": parent, "toColumnId": f.columnB, "place": "end"}))
	check("правка", f.mustApply("UPDATE_CARD", map[string]any{"cardId": parent, "title": "Фича с задачами, переименована"}))
}
