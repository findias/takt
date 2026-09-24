package board

import (
	"errors"
	"testing"
	"time"
)

// Шаблон доски и «Работаем итерациями» (этап 32.4).

func TestTemplatesSetOnlyTheStart(t *testing.T) {
	f := newFixture(t)

	kanban, err := f.svc.CreateFrom(f.ctx, f.orgID, f.actorID, "Канбан", "", TemplateKanban)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, kanban.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Board.IterationsEnabled || kanban.IterationsEnabled {
		t.Error("у канбан-доски итерации включены")
	}
	var limited []string
	for _, c := range snap.Columns {
		if c.WIPLimit != nil {
			limited = append(limited, c.Kind)
		}
	}
	if len(limited) != 1 || limited[0] != KindInProgress {
		t.Errorf("лимит стоит на %v, ожидался только на работе", limited)
	}

	scrum, err := f.svc.CreateFrom(f.ctx, f.orgID, f.actorID, "Скрам", "", TemplateScrum)
	if err != nil {
		t.Fatal(err)
	}
	snap, err = f.svc.Snapshot(f.ctx, f.orgID, f.actorID, scrum.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Board.IterationsEnabled || len(snap.Iterations) != 1 {
		t.Fatalf("скрам-доска: итерации %v, заведено %d", snap.Board.IterationsEnabled, len(snap.Iterations))
	}
	it := snap.Iterations[0]
	start, _ := time.Parse(time.DateOnly, it.StartsOn)
	end, _ := time.Parse(time.DateOnly, it.EndsOn)
	if days := int(end.Sub(start).Hours()/24) + 1; days != 14 {
		t.Errorf("первая итерация на %d дней, ожидалось две недели", days)
	}

	// Пустая — как было всегда: итерации включены, лимитов нет.
	empty := f.snapshot()
	if !empty.Board.IterationsEnabled || len(empty.Iterations) != 0 {
		t.Errorf("пустая доска изменилась: итерации %v, заведено %d",
			empty.Board.IterationsEnabled, len(empty.Iterations))
	}
	if _, err := f.svc.CreateFrom(f.ctx, f.orgID, f.actorID, "Иное", "", "waterfall"); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("незнакомый шаблон: %v", err)
	}
}

// Выключение ничего не удаляет: включили обратно — итерации и состав
// на месте.
func TestIterationsSwitchKeepsHistory(t *testing.T) {
	f := newFixture(t)
	it, err := f.svc.CreateIteration(f.ctx, f.orgID, f.actorID, f.boardID, "Неделя 1", "",
		time.Now().Format(time.DateOnly), time.Now().AddDate(0, 0, 6).Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	card := f.createCard("В итерации", f.columnA)
	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": card, "iterationId": it.ID})

	if err := f.svc.SetIterations(f.ctx, f.orgID, f.actorID, f.boardID, false); err != nil {
		t.Fatal(err)
	}
	if f.snapshot().Board.IterationsEnabled {
		t.Fatal("итерации не выключились")
	}
	if err := f.svc.SetIterations(f.ctx, f.orgID, f.actorID, f.boardID, true); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot()
	if !snap.Board.IterationsEnabled || len(snap.Iterations) != 1 || snap.CardIterations[card] != it.ID {
		t.Errorf("после включения: итерации %v, заведено %d, карточка в %q",
			snap.Board.IterationsEnabled, len(snap.Iterations), snap.CardIterations[card])
	}
}
