package board

import (
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
