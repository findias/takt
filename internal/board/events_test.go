package board

import (
	"strings"
	"testing"
)

// Лента событий доски.

func TestEventsComeNewestFirstAndCarryTheirAuthor(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Задача", f.columnA)
	f.mustApply("MOVE_CARD", map[string]any{
		"cardId": id, "toColumnId": f.columnB, "place": "end"})
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": id, "title": "Задача, но точнее"})

	feed, err := f.svc.Events(f.ctx, f.orgID, f.actorID, f.boardID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Events) != 3 {
		t.Fatalf("событий %d, ожидались создание, перемещение и переименование: %+v",
			len(feed.Events), feed.Events)
	}
	if feed.Events[0].Type != "renamed" {
		t.Errorf("лента идёт не от свежих: %+v", feed.Events[0].Type)
	}
	// Название берётся текущее, а не то, что было на момент события:
	// иначе в ленте доски не понять, о какой карточке речь.
	if feed.Events[0].CardTitle != "Задача, но точнее" {
		t.Errorf("в ленте старое название карточки: %q", feed.Events[0].CardTitle)
	}
	if feed.Events[0].Actor == nil {
		t.Error("событие пришло без автора")
	}
	if !strings.Contains(string(feed.Events[1].Payload), "crossedStart") {
		t.Errorf("перемещение пришло без смысла перехода: %s", feed.Events[1].Payload)
	}
	if feed.Next != nil {
		t.Errorf("на трёх событиях предложено продолжение: %v", *feed.Next)
	}
}

func TestEventsOfOneCardAreSeparable(t *testing.T) {
	f := newFixture(t)
	first := f.createCard("Первая", f.columnA)
	f.createCard("Вторая", f.columnA)
	f.mustApply("MOVE_CARD", map[string]any{
		"cardId": first, "toColumnId": f.columnB, "place": "end"})

	feed, err := f.svc.Events(f.ctx, f.orgID, f.actorID, f.boardID, first, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Events) != 2 {
		t.Fatalf("событий карточки %d, ожидалось два: %+v", len(feed.Events), feed.Events)
	}
	for _, e := range feed.Events {
		if e.CardID != first {
			t.Errorf("в ленте карточки чужое событие: %+v", e)
		}
	}
}

// Лента растёт неограниченно, поэтому листается курсором: смещение по
// номеру страницы на растущей ленте показывает одно и то же дважды.
func TestFeedIsPagedByCursorWithoutGapsOrRepeats(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < FeedLimit+5; i++ {
		f.createCard("Карточка", f.columnA)
	}

	first, err := f.svc.Events(f.ctx, f.orgID, f.actorID, f.boardID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != FeedLimit {
		t.Fatalf("на первой странице %d событий, ожидалось %d", len(first.Events), FeedLimit)
	}
	if first.Next == nil {
		t.Fatal("продолжение не предложено, хотя события остались")
	}

	second, err := f.svc.Events(f.ctx, f.orgID, f.actorID, f.boardID, "", first.Next)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 5 {
		t.Fatalf("на второй странице %d событий, ожидалось 5", len(second.Events))
	}
	if second.Next != nil {
		t.Error("после последней страницы предложено продолжение")
	}

	seen := map[int64]bool{}
	for _, e := range append(first.Events, second.Events...) {
		if seen[e.ID] {
			t.Fatalf("событие %d показано дважды", e.ID)
		}
		seen[e.ID] = true
	}
	if len(seen) != FeedLimit+5 {
		t.Errorf("всего показано %d событий, заведено %d", len(seen), FeedLimit+5)
	}
}

// Недоступная доска отдаёт пустую ленту, а не отказ: подтверждать
// существование того, чего не видно, незачем.
func TestFeedOfForeignBoardIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.createCard("Секрет", f.columnA)
	other := newFixture(t)

	feed, err := f.svc.Events(f.ctx, other.orgID, other.actorID, f.boardID, "", nil)
	if err != nil {
		t.Fatalf("чужая лента вернула ошибку вместо пустоты: %v", err)
	}
	if len(feed.Events) != 0 {
		t.Errorf("из чужой организации видно %d событий", len(feed.Events))
	}
}
