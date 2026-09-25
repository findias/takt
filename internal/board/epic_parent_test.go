package board

import (
	"errors"
	"strings"
	"testing"
)

// Эпик всегда родитель (замечено владельцем 25.09.2026): с доски
// команды можно было завести подзадачу на портфеле или связать эпик
// подзадачей задачи — иерархия шла наоборот.
func TestEpicCannotBecomeASubtaskOfATask(t *testing.T) {
	f := newFixture(t)
	pf, epic := f.portfolio()
	task := f.createCard("Задача команды", f.columnA)

	refused := func(what string, err error) {
		t.Helper()
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("%s: ожидался отказ, получено %v", what, err)
		}
		if !strings.Contains(conflict.Message, "«Родитель»") {
			t.Errorf("%s: отказ не говорит, как надо: %q", what, conflict.Message)
		}
	}

	_, err := f.apply("CREATE_SUBTASK", map[string]any{
		"parentCardId": task, "title": "Эпик под задачей", "boardId": pf})
	refused("подзадача задачи на портфеле", err)
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, pf)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range snap.Cards {
		if c.Title == "Эпик под задачей" {
			t.Fatal("отказ оставил на портфеле карточку")
		}
	}

	_, err = f.apply("LINK_CARDS", map[string]any{"fromCard": task, "toCard": epic, "kind": "subtask"})
	refused("эпик подзадачей задачи", err)

	// Задача под эпиком — как и должно быть.
	if _, err := f.apply("LINK_CARDS", map[string]any{"fromCard": epic, "toCard": task, "kind": "subtask"}); err != nil {
		t.Fatalf("задача под эпиком: %v", err)
	}
	// Эпик под эпиком — можно: большой эпик делят на меньшие.
	f.applyTo(pf, "CREATE_SUBTASK", map[string]any{"parentCardId": epic, "title": "Эпик поменьше"})
}
