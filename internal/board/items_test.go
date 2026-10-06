package board

import (
	"errors"
	"testing"
)

// Пункт родителя — подзадача с той же доски (IsItem). Своя колонка
// у него в базе есть, но ничего не значит: лимит колонки, «мои задачи»
// и метрики его не считают. Иначе отмеченный сделанным пункт оставался
// в «В работе», занимал её лимит, а вынести его оттуда было нечем —
// на доске он показан внутри родителя.

func (f *fixture) subtask(parent, title string) string {
	f.t.Helper()
	f.mustApply("CREATE_SUBTASK", map[string]any{"parentCardId": parent, "title": title})
	for _, c := range f.snapshot().Cards {
		if c.Title == title {
			return c.ID
		}
	}
	f.t.Fatalf("подзадача %q не появилась", title)
	return ""
}

func TestItemsDoNotTakeTheColumnLimit(t *testing.T) {
	f := newFixture(t)
	queue := f.columns()[0].ID
	parent := f.createCard("Выпустить релиз", queue)
	// Пункты ложатся в первую колонку — ту же, где родитель.
	f.subtask(parent, "Прогнать нагрузочные")
	f.subtask(parent, "Собрать сборку")

	f.mustApply("UPDATE_COLUMN", map[string]any{
		"columnId": queue, "wipLimit": 2, "wipLimitHard": true})

	// В колонке одна работа — родитель; место под вторую есть.
	if _, err := f.apply("CREATE_CARD", map[string]any{"columnId": queue, "title": "Вторая"}); err != nil {
		t.Fatalf("пункты родителя заняли лимит колонки: %v", err)
	}
	// А третьей работе места уже нет: лимит по-прежнему держит.
	_, err := f.apply("CREATE_CARD", map[string]any{"columnId": queue, "title": "Третья"})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("жёсткий лимит перестал держать обычные карточки: %v", err)
	}
	// И новый пункт в полную колонку заводится: в неё он не входит.
	f.subtask(parent, "Согласовать письмо")
}

func TestItemInMyTasksSaysWhereItLives(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	parent := f.createCard("Выпустить релиз", cols[0].ID)
	f.mustApply("MOVE_CARD", map[string]any{"cardId": parent, "toColumnId": cols[1].ID, "place": "end"})
	item := f.subtask(parent, "Прогнать нагрузочные")
	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": item, "userId": f.actorID})

	list, err := f.svc.Tasks(f.ctx, f.orgID, f.actorID, f.actorID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tasks) != 1 {
		t.Fatalf("в списке %d задач, ожидался один пункт", len(list.Tasks))
	}
	got := list.Tasks[0]
	if got.Parent == nil || *got.Parent != f.card(parent).Number {
		t.Errorf("у пункта не назван родитель: %v", got.Parent)
	}
	// Колонка — родителя: своя, первая, у пункта ничего не значит.
	if got.Column != cols[1].Name {
		t.Errorf("пункт показан в колонке %q, а родитель — в %q", got.Column, cols[1].Name)
	}

	// Отмеченный пункт — законченная работа: финиша у него нет, и иначе
	// он висел бы в «моих» вечно.
	f.mustApply("SET_CARD_DONE", map[string]any{"cardId": item, "done": true})
	list, err = f.svc.Tasks(f.ctx, f.orgID, f.actorID, f.actorID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tasks) != 0 {
		t.Errorf("отмеченный пункт остался среди незаконченных: %+v", list.Tasks)
	}
	list, err = f.svc.Tasks(f.ctx, f.orgID, f.actorID, f.actorID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tasks) != 1 {
		t.Fatalf("с законченными пункт должен быть виден: %+v", list.Tasks)
	}
	// И отбор «Сделаны» его находит, хотя родитель ещё в работе.
	if got := list.Tasks[0]; !got.Done || got.ColumnKind != "done" {
		t.Errorf("отмеченный пункт не читается сделанным: done=%v, вид %q", got.Done, got.ColumnKind)
	}
}
