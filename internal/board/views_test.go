package board

import (
	"errors"
	"testing"
)

// Сохранённые виды.
//
// Проверяется главное свойство: вид принадлежит человеку. Список чужих
// сохранённых фильтров рассказывает, кто чем занят, и показывать его
// незачем — даже внутри одной организации.

func TestViewIsSavedAndListed(t *testing.T) {
	f := newFixture(t)

	saved, err := f.svc.SaveView(f.ctx, f.orgID, f.actorID, f.boardID, "Моё на неделе", "?assignee=me&aging=1")
	if err != nil {
		t.Fatal(err)
	}
	// Ведущий вопросительный знак — деталь адреса, а не условия: иначе
	// появились бы два вида, отличающиеся только им.
	if saved.Query != "assignee=me&aging=1" {
		t.Errorf("условие сохранилось как %q", saved.Query)
	}

	views, err := f.svc.Views(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Name != "Моё на неделе" {
		t.Fatalf("список видов: %+v", views)
	}

	if err := f.svc.DeleteView(f.ctx, f.orgID, f.actorID, saved.ID); err != nil {
		t.Fatal(err)
	}
	views, _ = f.svc.Views(f.ctx, f.orgID, f.actorID, f.boardID)
	if len(views) != 0 {
		t.Errorf("вид не удалился: %+v", views)
	}
}

func TestViewNamesAreUniquePerPersonAndBoard(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveView(f.ctx, f.orgID, f.actorID, f.boardID, "Моё", "q=1"); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.SaveView(f.ctx, f.orgID, f.actorID, f.boardID, "моё", "q=2")
	if !errors.Is(err, ErrViewExists) {
		t.Errorf("второй вид с тем же названием заведён, ошибка: %v", err)
	}

	if _, err := f.svc.SaveView(f.ctx, f.orgID, f.actorID, f.boardID, "  ", "q=3"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("вид без названия принят, ошибка: %v", err)
	}
}

// Чужие виды не видны и не удаляются — даже владельцу организации.
func TestViewsAreOwnOnly(t *testing.T) {
	f := newFixture(t)
	mate := addMember(t, f.svc.db, f.orgID, "member")

	mine, err := f.svc.SaveView(f.ctx, f.orgID, f.actorID, f.boardID, "Только моё", "q=1")
	if err != nil {
		t.Fatal(err)
	}

	// Коллега на той же доске своих видов не имеет и чужих не видит.
	views, err := f.svc.Views(f.ctx, f.orgID, mate, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 0 {
		t.Errorf("коллега видит чужие виды: %+v", views)
	}

	if err := f.svc.DeleteView(f.ctx, f.orgID, mate, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("коллега удалил чужой вид, ошибка: %v", err)
	}
	if views, _ := f.svc.Views(f.ctx, f.orgID, f.actorID, f.boardID); len(views) != 1 {
		t.Error("вид всё-таки исчез")
	}
}

func TestViewOfForeignBoardIsNotSaved(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)

	_, err := f.svc.SaveView(other.ctx, other.orgID, other.actorID, f.boardID, "Чужое", "q=1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("вид сохранён на чужую доску, ошибка: %v", err)
	}
}
