package board

import "testing"

// Прогресс по всему поддереву (этап 32.3): у родителя с внуками —
// второй счёт, по листьям; промежуточный узел в него не входит.

// sub заводит подзадачу и возвращает её идентификатор.
func (f *fixture) sub(parent, title string) string {
	f.t.Helper()
	res := f.mustApply("CREATE_SUBTASK", map[string]any{"parentCardId": parent, "title": title})
	for _, c := range res.Patch.Cards {
		if c.Title == title {
			return c.ID
		}
	}
	f.t.Fatalf("подзадача %q не вернулась в патче", title)
	return ""
}

func (f *fixture) done(cardID string) Result {
	f.t.Helper()
	return f.mustApply("SET_CARD_DONE", map[string]any{"cardId": cardID, "done": true})
}

func TestSubtreeCountsLeavesNotMiddle(t *testing.T) {
	f := newFixture(t)
	epic := f.createCard("Эпик", f.columnA)
	feature := f.sub(epic, "Фича")
	other := f.sub(epic, "Вторая фича")
	a := f.sub(feature, "Задача А")
	f.sub(feature, "Задача Б")
	f.done(a)
	f.done(other) // лист на втором уровне: у фичи без задач она и есть лист

	c := f.card(epic)
	if c.Progress == nil || c.Progress.Total != 2 || c.Progress.Done != 1 {
		t.Errorf("прямые части %+v, ожидалось 1 из 2 фич", c.Progress)
	}
	// Листья: задача А (сделана), задача Б, вторая фича (сделана) — фича
	// с задачами в счёт не входит, иначе посчиталась бы дважды.
	if c.Subtree == nil || c.Subtree.Total != 3 || c.Subtree.Done != 2 {
		t.Fatalf("поддерево %+v, ожидалось 2 из 3 листьев", c.Subtree)
	}
	// У фичи внуков нет — второго счёта нет, всё как прежде.
	if s := f.card(feature).Subtree; s != nil {
		t.Errorf("у карточки без внуков появился второй счёт: %+v", s)
	}
}

func TestSubtreeWeighsByLeafEstimates(t *testing.T) {
	f := newFixture(t)
	epic := f.createCard("Эпик", f.columnA)
	feature := f.sub(epic, "Фича")
	a := f.sub(feature, "А")
	b := f.sub(feature, "Б")
	// Оценка у фичи нарочно: промежуточный узел в вес не входит.
	for id, est := range map[string]float64{feature: 100, a: 3, b: 5} {
		f.mustApply("UPDATE_CARD", map[string]any{"cardId": id, "estimate": est})
	}
	f.done(b)
	s := f.card(epic).Subtree
	if s == nil || !s.ByWeight || s.Total != 8 || s.Done != 5 {
		t.Errorf("вес поддерева %+v, ожидалось 5 из 8 по оценкам листьев", s)
	}
}

// Упёршаяся часть на любой глубине останавливает корень — и корень
// узнаёт об этом из патча, а не после перезагрузки.
func TestBlockedGrandchildReachesRoot(t *testing.T) {
	f := newFixture(t)
	epic := f.createCard("Эпик", f.columnA)
	feature := f.sub(epic, "Фича")
	task := f.sub(feature, "Задача")

	res := f.mustApply("BLOCK_CARD", map[string]any{"cardId": task, "reason": "ждём доступ"})
	var root *Card
	for i, c := range res.Patch.Cards {
		if c.ID == epic {
			root = &res.Patch.Cards[i]
		}
	}
	if root == nil {
		t.Fatalf("корень не приехал с патчем блокировки: %d карточек", len(res.Patch.Cards))
	}
	if root.Subtree == nil || root.Subtree.Stuck != 1 || root.Subtree.StuckReason != "ждём доступ" {
		t.Errorf("в патче корень не знает о застрявшем внуке: %+v", root.Subtree)
	}
	if s := f.card(epic).Subtree; s == nil || s.Stuck != 1 || s.StuckTitle != "Задача" {
		t.Errorf("в снимке корень не знает о застрявшем внуке: %+v", s)
	}
	// Прямая часть в Stuck не считается: о ней клиент знает из связей.
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": feature, "reason": "сама"})
	if s := f.card(epic).Subtree; s.Stuck != 1 {
		t.Errorf("прямая часть попала в счёт застрявшего глубже: %d", s.Stuck)
	}
	// Заблокированный корень частей не останавливает: им это не видно.
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": epic, "reason": "корень"})
	if s := f.card(feature).Subtree; s != nil {
		t.Errorf("у фичи без внуков появился счёт поддерева: %+v", s)
	}

	res = f.mustApply("UNBLOCK_CARD", map[string]any{"cardId": task})
	for _, c := range res.Patch.Cards {
		if c.ID == epic && (c.Subtree == nil || c.Subtree.Stuck != 0) {
			t.Errorf("снятие блокировки не дошло до корня: %+v", c.Subtree)
		}
	}
}

// Отметка внука сделанным доносит новый счёт до корня тем же патчем.
func TestDoneGrandchildPatchesRoot(t *testing.T) {
	f := newFixture(t)
	epic := f.createCard("Эпик", f.columnA)
	feature := f.sub(epic, "Фича")
	task := f.sub(feature, "Задача")
	f.sub(feature, "Ещё задача")
	for _, c := range f.done(task).Patch.Cards {
		if c.ID == epic {
			if c.Subtree == nil || c.Subtree.Done != 1 || c.Subtree.Total != 2 {
				t.Errorf("корень в патче: %+v", c.Subtree)
			}
			return
		}
	}
	t.Error("корень не приехал с патчем отметки внука")
}
