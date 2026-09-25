package board

import "testing"

// Блокировка, которую держит другая карточка, снимается, когда ту
// сделали (замечено владельцем 25.09.2026): прежде карточка оставалась
// «Заблокирована: ждёт задачу 2» и после того, как задача 2 была
// сделана, — и переносом в «Готово», и отметкой «Сделана».
func TestDoneCardReleasesTheBlockItHeld(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	queue, done := cols[0].ID, cols[2].ID

	for _, how := range []string{"перенос в готово", "отметка сделана"} {
		waiting := f.createCard("Ждёт "+how, queue)
		holder := f.createCard("Держит "+how, queue)
		f.mustApply("BLOCK_CARD", map[string]any{"cardId": waiting, "reason": "ждём вторую", "blockingCard": holder})

		var res Result
		if how == "перенос в готово" {
			res = f.mustApply("MOVE_CARD", map[string]any{"cardId": holder, "toColumnId": done, "place": "end"})
		} else {
			res = f.done(holder)
		}
		if c := f.card(waiting); c.Blocked != nil {
			t.Errorf("%s: блокировка осталась: %+v", how, c.Blocked)
		}
		released := false
		for _, c := range res.Patch.Cards {
			if c.ID == waiting && c.Blocked == nil {
				released = true
			}
		}
		if !released {
			t.Errorf("%s: освобождённой карточки нет в патче — на экране она осталась бы заблокированной", how)
		}
	}

	// Блокировку без держащей карточки сделанная чужая не трогает.
	own := f.createCard("Своя блокировка", queue)
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": own, "reason": "ждём доступ"})
	f.done(f.createCard("Посторонняя", queue))
	if f.card(own).Blocked == nil {
		t.Error("сделанная карточка сняла чужую блокировку")
	}
}
