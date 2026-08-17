package board

import (
	"errors"
	"testing"
)

// Обсуждение карточки. Проверяется не переписка, а то, что в ней
// необратимо: ветка, прежний текст правки и упоминание ссылкой.

func TestCommentsFormOneLevelThreads(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)

	root, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card,
		"Кто этим занимается?", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card,
		"Я, начну завтра", &root.ID, nil)
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}

	// Ответ на ответ читать невозможно, и все, кто пробовал, к этому пришли.
	if _, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card,
		"А я послезавтра", &reply.ID, nil); !errors.Is(err, ErrTooDeep) {
		t.Errorf("ответ на ответ принят: %v", err)
	}

	// Отвечать можно только на комментарий той же карточки.
	other := f.createCard("Другая", f.columnA)
	if _, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, other,
		"Не туда", &root.ID, nil); err == nil {
		t.Error("ответ на комментарий чужой карточки принят")
	}

	list, err := f.svc.Comments(f.ctx, f.orgID, f.actorID, card)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[1].ParentID == nil || *list[1].ParentID != root.ID {
		t.Fatalf("обсуждение: %+v", list)
	}
}

// Число реплик приезжает со снимком доски: у подзадачи своё обсуждение,
// и без счётчика в строке о нём узнают, только зайдя внутрь. Удалённые
// не считаются — обсуждение это то, что в нём осталось.
func TestSnapshotCarriesCommentCount(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	quiet := f.createCard("Без разговора", f.columnA)

	root, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card, "Раз", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card,
		"Два", &root.ID, nil); err != nil {
		t.Fatal(err)
	}
	gone, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card, "Лишнее", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteComment(f.ctx, f.orgID, f.actorID, gone.ID); err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{}
	for _, c := range f.snapshot().Cards {
		counts[c.ID] = c.Comments
	}
	if counts[card] != 2 {
		t.Errorf("реплик у карточки: %d, ожидалось 2", counts[card])
	}
	if counts[quiet] != 0 {
		t.Errorf("реплик у карточки без обсуждения: %d", counts[quiet])
	}
}

// «Изменено» без прежнего текста бесполезно: спрашивают не «правил ли он»,
// а «что там было написано до».
func TestEditKeepsThePreviousText(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	c, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card, "Первый вариант", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.EditComment(f.ctx, f.orgID, f.actorID, c.ID, "Второй вариант"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.EditComment(f.ctx, f.orgID, f.actorID, c.ID, "Третий вариант"); err != nil {
		t.Fatal(err)
	}

	list, _ := f.svc.Comments(f.ctx, f.orgID, f.actorID, card)
	if list[0].Body != "Третий вариант" || list[0].EditedAt == nil {
		t.Fatalf("после правки: %+v", list[0])
	}

	was, err := f.svc.Revisions(f.ctx, f.orgID, f.actorID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(was) != 2 || was[0] != "Второй вариант" || was[1] != "Первый вариант" {
		t.Errorf("прежние версии: %v", was)
	}

	// Удаление — не правка, и версий не плодит.
	if err := f.svc.DeleteComment(f.ctx, f.orgID, f.actorID, c.ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.svc.Revisions(f.ctx, f.orgID, f.actorID, c.ID); len(again) != 2 {
		t.Errorf("удаление добавило версий: %v", again)
	}
}

// Строка остаётся: на неё ссылаются ответы, и вырезав её, мы разорвали бы
// ветку, в которой отвечали живым людям.
func TestDeletedCommentKeepsTheThread(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	root, _ := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card, "Вопрос", nil, nil)
	f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card, "Ответ", &root.ID, nil)

	if err := f.svc.DeleteComment(f.ctx, f.orgID, f.actorID, root.ID); err != nil {
		t.Fatal(err)
	}

	list, _ := f.svc.Comments(f.ctx, f.orgID, f.actorID, card)
	if len(list) != 2 {
		t.Fatalf("после удаления в обсуждении %d реплик, ожидалось две", len(list))
	}
	if !list[0].Deleted || list[0].Body != "" {
		t.Errorf("удалённый комментарий отдаётся с текстом: %+v", list[0])
	}
	if list[1].Body != "Ответ" {
		t.Errorf("ответ пострадал: %+v", list[1])
	}

	// Повтор ничего не меняет.
	if err := f.svc.DeleteComment(f.ctx, f.orgID, f.actorID, root.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
}

// Чужой текст под чужим именем — это подлог, а не редактирование.
func TestOnlyAuthorEditsAndDeletes(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	c, _ := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card, "Моё", nil, nil)

	stranger := addMember(t, f.svc.db, f.orgID, "member")
	if err := f.svc.EditComment(f.ctx, f.orgID, stranger, c.ID, "Не моё"); !errors.Is(err, ErrNotAuthor) {
		t.Errorf("чужой комментарий отредактирован: %v", err)
	}
	if err := f.svc.DeleteComment(f.ctx, f.orgID, stranger, c.ID); !errors.Is(err, ErrNotAuthor) {
		t.Errorf("чужой комментарий удалён: %v", err)
	}

	list, _ := f.svc.Comments(f.ctx, f.orgID, f.actorID, card)
	if list[0].Body != "Моё" || list[0].Deleted {
		t.Errorf("комментарий всё-таки пострадал: %+v", list[0])
	}
}

// Упоминание хранится ссылкой: имя сменится, текст перепишется, а факт
// «его позвали в это обсуждение» обязан пережить и то, и другое.
func TestMentionsAreStoredAsPeopleNotText(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	colleague := addMember(t, f.svc.db, f.orgID, "member")

	other := newFixture(t)
	c, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card,
		"@Коллега, посмотри", nil, []string{colleague, other.actorID})
	if err != nil {
		t.Fatal(err)
	}

	// Позвать можно только того, кто в организации есть.
	if len(c.Mentions) != 1 || c.Mentions[0] != colleague {
		t.Fatalf("упоминания: %v", c.Mentions)
	}

	// Правка текста не трогает того, кого уже позвали.
	if err := f.svc.EditComment(f.ctx, f.orgID, f.actorID, c.ID, "уже неважно"); err != nil {
		t.Fatal(err)
	}
	list, _ := f.svc.Comments(f.ctx, f.orgID, f.actorID, card)
	if len(list[0].Mentions) != 1 {
		t.Errorf("правка потеряла упоминание: %+v", list[0])
	}
}

func TestCommentsOfForeignBoardAreInvisible(t *testing.T) {
	f := newFixture(t)
	card := f.createCard("Задача", f.columnA)
	f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, card, "Секрет", nil, nil)
	other := newFixture(t)

	list, err := f.svc.Comments(f.ctx, other.orgID, other.actorID, card)
	if err != nil {
		t.Fatalf("чужое обсуждение вернуло ошибку вместо пустоты: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("из чужой организации видно %d реплик", len(list))
	}
}
