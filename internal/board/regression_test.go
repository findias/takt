package board

import (
	"errors"
	"testing"
)

// Регрессия: архивная карточка занимала ключ порядка навсегда.
//
// Уникальный индекс на (column_id, position) в первой миграции не был
// частичным, а архивация оставляла карточке и колонку, и позицию. Поиск
// соседей при этом смотрит только на живые карточки — значит вычисленный
// ключ мог совпасть с ключом давно убранной карточки. Результат: операция
// падала нарушением уникальности, то есть внутренней ошибкой, вместо того
// чтобы просто выполниться.
//
// Сценарий ниже воспроизводит это детерминированно: третья карточка
// вычисляет ровно тот же ключ, что был у второй, потому что вторая
// исключена из поиска соседей, а её ключ из индекса никуда не делся.
func TestArchivedCardDoesNotHoldItsOrderKeyForever(t *testing.T) {
	f := newFixture(t)
	column := f.columns()[0].ID

	f.createCard("Первая", column)
	second := f.createCard("Вторая", column)
	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": second})

	if _, err := f.apply("CREATE_CARD", map[string]any{
		"columnId": column, "title": "Третья", "place": "end"}); err != nil {
		t.Fatalf("создание карточки после архивации соседней: %v", err)
	}

	if got := f.titles(column); len(got) != 2 || got[1] != "Третья" {
		t.Errorf("состав колонки: %v, ожидались «Первая» и «Третья»", got)
	}

	// Та же ловушка при перемещении: позиция вычисляется тем же кодом.
	fourth := f.createCard("Четвёртая", f.columns()[1].ID)
	if _, err := f.apply("MOVE_CARD", map[string]any{
		"cardId": fourth, "toColumnId": column, "place": "end"}); err != nil {
		t.Errorf("перемещение в колонку с архивной карточкой: %v", err)
	}
}

// Убранная в архив доска называется архивной, а не «не найденной».
//
// Разные положения дел требуют разных следующих шагов: несуществующую
// доску искать бесполезно, архивную — достаточно вернуть одной кнопкой.
// Пока их не различали, ссылка на убранную доску отвечала «не найдена»,
// и человек шёл искать поломку там, где её нет.
func TestArchivedBoardIsNamedArchived(t *testing.T) {
	f := newFixture(t)

	if err := f.svc.Archive(f.ctx, f.orgID, f.actorID, f.boardID); err != nil {
		t.Fatalf("архивация доски: %v", err)
	}

	_, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, f.boardID)
	if !errors.Is(err, ErrArchivedBoard) {
		t.Fatalf("снимок архивной доски вернул %v, ожидалось «доска в архиве»", err)
	}

	// А чужая доска по-прежнему неотличима от несуществующей: различать
	// можно только то, что человеку и так видно.
	other := newFixture(t)
	_, err = f.svc.Snapshot(f.ctx, f.orgID, f.actorID, other.boardID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("чужая доска вернула %v, ожидалось «не найдена»", err)
	}
}
