package board

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
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

// Убранный пункт пропадает из снимка вместе со связью и возвращается
// вместе с ней. Пока связь приходила одна, без карточки, клиент
// показывал на её месте «карточка недоступна, подразделение вам
// не видно» — про часть, которую только что убрали в архив.
func TestArchivedItemLeavesNoDanglingLink(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	item := f.subtask(parent, "Прогнать нагрузочные")

	linked := func() bool {
		for _, l := range f.snapshot().Links {
			if l.FromCard == parent && l.ToCard == item {
				return true
			}
		}
		return false
	}

	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": item})
	if linked() {
		t.Error("связь с убранным пунктом осталась в снимке без самой карточки")
	}
	if n := f.countRows(`select count(*) from card_links where to_card = $1`, item); n != 1 {
		t.Fatalf("архив снял связь в базе: %d", n)
	}

	f.mustApply("RESTORE_CARD", map[string]any{"cardId": item})
	if !linked() {
		t.Error("пункт вернулся из архива без родителя")
	}
}

// Пункты уходят в архив вместе с родителем и вместе с ним возвращаются
// (решение владельца 26.10.2026). Прежде убранный родитель выпускал их
// в колонку отдельными карточками: убрали одну, а на доске появилось
// столько, сколько было пунктов.
func TestItemsGoToArchiveWithTheirParent(t *testing.T) {
	f := newFixture(t)
	queue := f.columns()[0].ID
	parent := f.createCard("Выпустить релиз", queue)
	open := f.subtask(parent, "Прогнать нагрузочные")
	deep := f.subtask(open, "Поднять стенд")
	done := f.subtask(parent, "Собрать сборку")
	f.mustApply("SET_CARD_DONE", map[string]any{"cardId": done, "done": true})
	// Убранный раньше родителя пункт — отдельное решение, и возврат
	// родителя его не отменяет.
	earlier := f.subtask(parent, "Черновик письма")
	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": earlier})
	// Часть на чужой доске — работа соседей: с родителем она не уходит.
	other := f.boardOfTeam("Соседи", nil)
	far := f.partOn(f.boardID, parent, "Поправить их сервис", other)

	res := f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": parent})
	removed := map[string]bool{}
	for _, id := range res.Patch.RemovedCardIDs {
		removed[id] = true
	}
	for _, id := range []string{parent, open, deep, done} {
		if !removed[id] {
			t.Errorf("карточка %s не убрана патчем вместе с родителем", f.titleOf(id))
		}
	}
	for _, c := range f.snapshot().Cards {
		if c.ID == open || c.ID == deep || c.ID == done {
			t.Errorf("пункт %q остался на доске без родителя", c.Title)
		}
	}
	if n := f.countRows(`select count(*) from cards where id = $1 and archived_at is null`, far); n != 1 {
		t.Error("часть на чужой доске ушла в архив вместе с родителем")
	}
	// Сделанная часть отказом не становится, брошенная — становится.
	if n := f.countRows(`select count(*) from cards where id = $1 and outcome is null`, done); n != 1 {
		t.Error("сделанная часть записана отброшенной")
	}
	// И метрики считают ушедшие с родителем пунктами, а не отказами.
	if n := f.countRows(`select count(*) from cards c where c.id = any($1) and `+IsItem("c"),
		[]string{open, deep, done}); n != 3 {
		t.Errorf("пунктами остались %d из 3 ушедших с родителем", n)
	}

	// В архиве — одна строка за одно действие, с числом частей.
	list, err := f.svc.ArchivedCards(f.ctx, f.orgID, f.actorID, f.boardID, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	for _, c := range list {
		rows[c.ID] = c.Parts
	}
	if len(rows) != 2 || rows[parent] != 3 {
		t.Errorf("в архиве ждали родителя с тремя частями и отдельно убранный пункт, вышло %+v", list)
	}
	if _, ok := rows[earlier]; !ok {
		t.Error("отдельно убранный пункт пропал из архива")
	}

	res = f.mustApply("RESTORE_CARD", map[string]any{"cardId": parent})
	back := map[string]bool{}
	for _, c := range res.Patch.Cards {
		back[c.ID] = true
	}
	for _, id := range []string{open, deep, done} {
		if !back[id] {
			t.Errorf("пункт %s не вернулся патчем вместе с родителем", f.titleOf(id))
		}
	}
	live := map[string]bool{}
	for _, c := range f.snapshot().Cards {
		live[c.ID] = true
	}
	for _, id := range []string{parent, open, deep, done} {
		if !live[id] {
			t.Errorf("карточка %s не вернулась на доску", f.titleOf(id))
		}
	}
	if live[earlier] {
		t.Error("возврат родителя вернул и пункт, убранный раньше него")
	}
	if n := f.countRows(`select count(*) from cards where id = any($1) and outcome is not null`,
		[]string{open, deep, done}); n != 0 {
		t.Errorf("у %d вернувшихся пунктов остался исход", n)
	}
}

func (f *fixture) titleOf(id string) string {
	f.t.Helper()
	title := id
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select title from cards where id = $1`, id).Scan(&title)
	})
	return title
}
