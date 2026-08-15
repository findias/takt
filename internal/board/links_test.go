package board

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Тесты иерархии, прогресса, блокировок и исхода работы.

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// secondBoard заводит вторую доску в той же организации — так проверяется
// главное свойство связей: подзадача может лежать на доске другой команды.
func (f *fixture) secondBoard() (boardID, queueID, doneID string) {
	f.t.Helper()
	b, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Соседняя команда")
	if err != nil {
		f.t.Fatalf("создание второй доски: %v", err)
	}
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, b.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return b.ID, snap.Columns[0].ID, snap.Columns[2].ID
}

func (f *fixture) cardOn(boardID, columnID, title string) string {
	f.t.Helper()
	raw := mustJSON(f.t, map[string]any{"columnId": columnID, "title": title, "place": "end"})
	res, err := f.svc.Apply(f.ctx, f.orgID, f.actorID, boardID,
		Request{OperationID: uuid.NewString(), Type: "CREATE_CARD", Payload: raw})
	if err != nil {
		f.t.Fatalf("создание карточки на доске %s: %v", boardID, err)
	}
	return res.Patch.Cards[0].ID
}

func (f *fixture) moveOn(boardID, cardID, columnID string) {
	f.t.Helper()
	raw := mustJSON(f.t, map[string]any{"cardId": cardID, "toColumnId": columnID, "place": "end"})
	if _, err := f.svc.Apply(f.ctx, f.orgID, f.actorID, boardID,
		Request{OperationID: uuid.NewString(), Type: "MOVE_CARD", Payload: raw}); err != nil {
		f.t.Fatalf("перемещение карточки: %v", err)
	}
}

func TestSubtaskOnAnotherTeamBoardCountsInProgress(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	parent := f.createCard("Выпустить релиз", cols[0].ID)

	otherBoard, otherQueue, otherDone := f.secondBoard()
	first := f.cardOn(otherBoard, otherQueue, "Собрать сборку")
	second := f.cardOn(otherBoard, otherQueue, "Прогнать тесты")

	for _, child := range []string{first, second} {
		f.mustApply("LINK_CARDS", map[string]any{
			"fromCard": parent, "toCard": child, "kind": "subtask"})
	}

	if p := f.card(parent).Progress; p == nil || p.Total != 2 || p.Done != 0 {
		t.Fatalf("прогресс родителя: %+v, ожидалось 0 из 2", p)
	}

	// Подзадачу доводит до конца другая команда — на своей доске.
	f.moveOn(otherBoard, first, otherDone)

	p := f.card(parent).Progress
	if p == nil || p.Done != 1 || p.Total != 2 {
		t.Fatalf("после завершения подзадачи прогресс: %+v, ожидалось 1 из 2", p)
	}

	// Связи видны с доски родителя, включая ссылки на чужие карточки.
	if got := len(f.snapshot().Links); got != 2 {
		t.Errorf("на доске родителя %d связей, ожидалось 2", got)
	}
}

func TestSubtaskTreeRejectsCyclesAndDepth(t *testing.T) {
	f := newFixture(t)
	queue := f.columns()[0].ID

	ids := make([]string, 0, MaxSubtaskDepth+1)
	for i := 0; i <= MaxSubtaskDepth; i++ {
		ids = append(ids, f.createCard(string(rune('А'+i)), queue))
	}

	// Цепочка ровно в MaxSubtaskDepth уровней строится без возражений.
	for i := 0; i+1 < MaxSubtaskDepth; i++ {
		f.mustApply("LINK_CARDS", map[string]any{"fromCard": ids[i], "toCard": ids[i+1]})
	}

	// Следующий уровень — уже за пределом.
	_, err := f.apply("LINK_CARDS", map[string]any{
		"fromCard": ids[MaxSubtaskDepth-1], "toCard": ids[MaxSubtaskDepth]})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Errorf("предел глубины не сработал: %v", err)
	}

	// Цикл: сделать корень подзадачей собственного потомка.
	_, err = f.apply("LINK_CARDS", map[string]any{"fromCard": ids[2], "toCard": ids[0]})
	if !errors.As(err, &conflict) {
		t.Errorf("цикл не пойман: %v", err)
	}

	// Второй родитель у той же подзадачи — дерево, а не граф.
	other := f.createCard("Чужой родитель", queue)
	_, err = f.apply("LINK_CARDS", map[string]any{"fromCard": other, "toCard": ids[1]})
	if !errors.As(err, &conflict) {
		t.Errorf("второй родитель принят: %v", err)
	}

	// Своя же связь наоборот — тоже цикл, но самый короткий.
	if _, err := f.apply("LINK_CARDS", map[string]any{
		"fromCard": ids[0], "toCard": ids[0]}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("связь карточки с собой: ожидалась ErrBadRequest, получено %v", err)
	}
}

func TestOutcomeSeparatesDoneFromDiscarded(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	queue, done := cols[0].ID, cols[2].ID

	finished := f.createCard("Сделано", queue)
	f.mustApply("MOVE_CARD", map[string]any{"cardId": finished, "toColumnId": done, "place": "end"})
	if got := f.card(finished).Outcome; got == nil || *got != "done" {
		t.Fatalf("исход завершённой карточки: %v, ожидалось done", got)
	}

	// Возврат в работу снимает объявление о завершении.
	f.mustApply("MOVE_CARD", map[string]any{"cardId": finished, "toColumnId": queue, "place": "end"})
	if got := f.card(finished).Outcome; got != nil {
		t.Errorf("карточка вернулась в работу, но исход остался %q", *got)
	}

	// Карточка, убранная с доски незавершённой, — это отказ, а не работа.
	dropped := f.createCard("Передумали", queue)
	f.mustApply("ARCHIVE_CARD", map[string]any{"cardId": dropped})

	var outcome *string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select outcome from cards where id = $1`, dropped).Scan(&outcome)
	})
	if outcome == nil || *outcome != "discarded" {
		t.Errorf("исход выброшенной карточки: %v, ожидалось discarded", outcome)
	}
}

func TestBlockIsAnIntervalNotAFlag(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Ждём смежников", f.columns()[0].ID)

	res := f.mustApply("BLOCK_CARD", map[string]any{"cardId": id, "reason": "нет доступа к стенду"})
	if b := res.Patch.Cards[0].Blocked; b == nil || b.Reason != "нет доступа к стенду" {
		t.Fatalf("блокировка не отражена в патче: %+v", res.Patch.Cards[0])
	}

	// Вторая блокировка поверх открытой — конфликт: иначе время в блоке
	// посчиталось бы дважды.
	_, err := f.apply("BLOCK_CARD", map[string]any{"cardId": id, "reason": "ещё одна"})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Errorf("повторная блокировка принята: %v", err)
	}

	res = f.mustApply("UNBLOCK_CARD", map[string]any{"cardId": id})
	if res.Patch.Cards[0].Blocked != nil {
		t.Error("после снятия блокировка осталась в патче")
	}

	// Интервал должен сохраниться: именно из него считается время
	// разрешения блокировок.
	var closed int
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select count(*) from card_blocks
			 where card_id = $1 and unblocked_at is not null`, id).Scan(&closed)
	})
	if closed != 1 {
		t.Errorf("закрытых интервалов блокировки: %d, ожидался 1", closed)
	}

	if _, err := f.apply("UNBLOCK_CARD", map[string]any{"cardId": id}); !errors.As(err, &conflict) {
		t.Errorf("снятие несуществующей блокировки прошло: %v", err)
	}
}

func TestMoveEventCarriesFlowMeaning(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	id := f.createCard("Задача", cols[0].ID)
	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": cols[1].ID, "place": "end"})

	var raw []byte
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select payload from card_events
			 where card_id = $1 and type = 'moved' order by id desc limit 1`, id).Scan(&raw)
	})

	var payload struct {
		CrossedStart  bool `json:"crossedStart"`
		CrossedFinish bool `json:"crossedFinish"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.CrossedStart || payload.CrossedFinish {
		t.Errorf("смысл перехода в журнале: старт %v, финиш %v; ожидалось true/false",
			payload.CrossedStart, payload.CrossedFinish)
	}
}

// Связь без названия карточки бесполезна: на экране родителя подзадача
// с соседней доски должна называться и показывать, чья это команда.
func TestSnapshotResolvesCardsFromOtherBoards(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columnA)

	otherBoard, otherQueue, otherDone := f.secondBoard()
	child := f.cardOn(otherBoard, otherQueue, "Собрать сборку")
	f.mustApply("LINK_CARDS", map[string]any{
		"fromCard": parent, "toCard": child, "kind": "subtask"})

	snap := f.snapshot()
	if len(snap.Linked) != 1 {
		t.Fatalf("карточек с других досок %d, ожидалась одна: %+v", len(snap.Linked), snap.Linked)
	}
	got := snap.Linked[0]
	if got.ID != child || got.Title != "Собрать сборку" {
		t.Errorf("подзадача пришла неузнаваемой: %+v", got)
	}
	if got.BoardName != "Соседняя команда" {
		t.Errorf("не видно, на какой доске идёт подзадача: %+v", got)
	}
	if got.Outcome != nil {
		t.Errorf("незавершённая подзадача пришла с исходом %v", *got.Outcome)
	}

	// Своих карточек здесь быть не должно: они уже в Cards, и дублировать
	// их значит заставить клиент решать, какой копии верить.
	for _, c := range snap.Linked {
		if c.BoardID == f.boardID {
			t.Errorf("своя карточка попала в список чужих: %+v", c)
		}
	}

	f.moveOn(otherBoard, child, otherDone)
	if snap = f.snapshot(); snap.Linked[0].Outcome == nil || *snap.Linked[0].Outcome != "done" {
		t.Errorf("завершение подзадачи не отражено: %+v", snap.Linked[0])
	}
}
