package board

import (
	"encoding/json"
	"errors"
	"strings"
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
	b, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Соседняя команда", "")
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

// Отметка «сделано» отвечает на вопрос о готовности части работы,
// не двигая карточку по доске и не подменяя собой поток.
func TestDoneMarkCountsInProgressWithoutTouchingFlow(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	parent := f.createCard("Выпустить рассылку", cols[0].ID)
	child := f.createCard("Согласовать с юристами", cols[0].ID)
	f.mustApply("LINK_CARDS", map[string]any{
		"fromCard": parent, "toCard": child, "kind": "subtask"})

	res := f.mustApply("SET_CARD_DONE", map[string]any{"cardId": child, "done": true})
	if res.Patch.Cards[0].DoneAt == nil {
		t.Fatal("отметка не отражена в патче")
	}
	// Родитель приезжает тем же патчем: его доля разбиения изменилась,
	// а узнать об этом ему больше неоткуда.
	if len(res.Patch.Cards) != 2 {
		t.Fatalf("в патче %d карточек, ожидались подзадача и родитель", len(res.Patch.Cards))
	}
	if p := res.Patch.Cards[1].Progress; p == nil || p.Done != 1 || p.Total != 1 {
		t.Fatalf("прогресс родителя в патче: %+v, ожидалось 1 из 1", p)
	}
	if p := f.card(parent).Progress; p == nil || p.Done != 1 || p.Total != 1 {
		t.Fatalf("прогресс родителя в снимке: %+v, ожидалось 1 из 1", p)
	}

	// Поток не тронут: отмеченная карточка стоит там же, где стояла,
	// и завершённой для метрик не считается — иначе появилась бы вторая
	// пропускная способность, которую никто не мерил.
	marked := f.card(child)
	if marked.ColumnID != cols[0].ID {
		t.Errorf("карточка переехала в %s, а отметка колонок не касается", marked.ColumnID)
	}
	if marked.FinishedAt != nil || marked.Outcome != nil {
		t.Errorf("отметка объявила работу законченной для потока: finishedAt=%v outcome=%v",
			marked.FinishedAt, marked.Outcome)
	}

	// Повтор не сдвигает момент: «отмечена третьего дня» не должна
	// превращаться в «отмечена только что» от лишнего нажатия.
	was := *marked.DoneAt
	again := f.mustApply("SET_CARD_DONE", map[string]any{"cardId": child, "done": true})
	if !again.Patch.Cards[0].DoneAt.Equal(was) {
		t.Errorf("повторная отметка сдвинула момент: было %v, стало %v",
			was, *again.Patch.Cards[0].DoneAt)
	}

	off := f.mustApply("SET_CARD_DONE", map[string]any{"cardId": child, "done": false})
	if off.Patch.Cards[0].DoneAt != nil {
		t.Error("отметка не снялась")
	}
	if p := f.card(parent).Progress; p == nil || p.Done != 0 {
		t.Errorf("после снятия отметки прогресс: %+v, ожидалось 0 из 1", p)
	}

	// Без явного «отметить или снять» операция не выполняется: молчание
	// клиента нельзя толковать переключением — два одновременных нажатия
	// вернули бы отметку туда, откуда начали.
	if _, err := f.apply("SET_CARD_DONE", map[string]any{"cardId": child}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("отметка без указания: ожидалась ErrBadRequest, получено %v", err)
	}
}

// Отмеченная подзадача считается сделанной и у соседей: доску они ведут
// свою, а доля разбиения родителя обязана это учитывать.
func TestDoneMarkOnNeighbourBoardCountsForParent(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)

	otherBoard, otherQueue, _ := f.secondBoard()
	child := f.cardOn(otherBoard, otherQueue, "Обновить документацию")
	f.mustApply("LINK_CARDS", map[string]any{
		"fromCard": parent, "toCard": child, "kind": "subtask"})

	raw := mustJSON(t, map[string]any{"cardId": child, "done": true})
	if _, err := f.svc.Apply(f.ctx, f.orgID, f.actorID, otherBoard,
		Request{OperationID: uuid.NewString(), Type: "SET_CARD_DONE", Payload: raw}); err != nil {
		t.Fatalf("отметка на чужой доске: %v", err)
	}

	if p := f.card(parent).Progress; p == nil || p.Done != 1 || p.Total != 1 {
		t.Fatalf("прогресс родителя: %+v, ожидалось 1 из 1", p)
	}
	// Чужая карточка приезжает со снимком отдельным составом — там
	// готовность тоже должна быть видна, иначе строка подзадачи скажет
	// «не сделана» рядом с полным прогрессом.
	linked := f.snapshot().Linked
	if len(linked) != 1 || !linked[0].Done {
		t.Errorf("готовность чужой подзадачи в снимке: %+v", linked)
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

// Прогресс по числу подзадач — ложь в любой команде, где задачи разного
// размера. Три мелкие правки из пяти задач не означают, что работа
// сделана на шестьдесят процентов.
func TestProgressCountsWeightWhenEverythingIsEstimated(t *testing.T) {
	f := newFixture(t)
	queue := f.columns()[0].ID
	done := f.columns()[2].ID
	parent := f.createCard("Выпустить релиз", queue)

	small := f.createCard("Мелочь", queue)
	big := f.createCard("Большая часть", queue)
	for _, child := range []string{small, big} {
		f.mustApply("LINK_CARDS", map[string]any{
			"fromCard": parent, "toCard": child, "kind": "subtask"})
	}

	// Пока не оценено ничего — считаем штуками.
	f.mustApply("MOVE_CARD", map[string]any{"cardId": small, "toColumnId": done, "place": "end"})
	p := f.card(parent).Progress
	if p == nil || p.ByWeight || p.Done != 1 || p.Total != 2 {
		t.Fatalf("без оценок прогресс: %+v, ожидалось 1 из 2 штуками", p)
	}

	// Оценена половина — по-прежнему штуками: неоценённая подзадача
	// с весом ноль исчезла бы из знаменателя, и прогресс показал бы
	// больше сделанного, чем есть.
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": small, "estimate": 1})
	p = f.card(parent).Progress
	if p == nil || p.ByWeight {
		t.Fatalf("при частичной оценке прогресс посчитан весом: %+v", p)
	}

	// Оценено всё — считаем весом, и картина меняется на честную.
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": big, "estimate": 9})
	p = f.card(parent).Progress
	if p == nil || !p.ByWeight || p.Done != 1 || p.Total != 10 {
		t.Fatalf("с оценками прогресс: %+v, ожидалось 1 из 10 весом", p)
	}
}

func TestEstimateIsSetClearedAndValidated(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Задача", f.columns()[0].ID)

	if got := f.card(id).Estimate; got != nil {
		t.Errorf("новая карточка уже оценена: %v", *got)
	}

	f.mustApply("UPDATE_CARD", map[string]any{"cardId": id, "estimate": 2.5})
	if got := f.card(id).Estimate; got == nil || *got != 2.5 {
		t.Fatalf("оценка не сохранилась: %v", got)
	}

	// Не присланное поле ничего не меняет — иначе переименование
	// стирало бы оценку.
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": id, "title": "Другое имя"})
	if got := f.card(id).Estimate; got == nil || *got != 2.5 {
		t.Errorf("переименование сбросило оценку: %v", got)
	}

	// Снять оценку можно только явным null.
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": id, "estimate": nil})
	if got := f.card(id).Estimate; got != nil {
		t.Errorf("оценка не снялась: %v", *got)
	}

	for _, bad := range []any{0, -3} {
		if _, err := f.apply("UPDATE_CARD", map[string]any{
			"cardId": id, "estimate": bad}); !errors.Is(err, ErrBadRequest) {
			t.Errorf("оценка %v принята: %v", bad, err)
		}
	}
}

// Подзадача заводится одной операцией: карточка и связь появляются вместе
// или не появляются вовсе. Двумя вызовами с клиента это делалось бы
// в два шага, и оборванный второй оставлял бы карточку без родителя —
// то есть работу, о которой никто не просил и которую никто не найдёт.
func TestCreateSubtaskMakesCardAndLinkAtOnce(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	parent := f.createCard("Выпустить релиз", cols[0].ID)

	res := f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Прогнать тесты"})

	// В ответе обе карточки: у родителя изменился прогресс, подзадача
	// появилась — клиенту нужно и то, и другое.
	var child Card
	for _, c := range res.Patch.Cards {
		if c.ID != parent {
			child = c
		}
	}
	if child.ID == "" {
		t.Fatalf("в патче нет новой карточки: %+v", res.Patch.Cards)
	}
	if child.Title != "Прогнать тесты" {
		t.Errorf("название подзадачи %q", child.Title)
	}
	// Колонка не названа — значит первая на доске: подзадачу заводят
	// из панели родителя, где колонку не выбирают.
	if child.ColumnID != cols[0].ID {
		t.Errorf("подзадача легла в колонку %s, ожидалась первая %s", child.ColumnID, cols[0].ID)
	}

	if p := f.card(parent).Progress; p == nil || p.Total != 1 || p.Done != 0 {
		t.Fatalf("прогресс родителя: %+v, ожидалось 0 из 1", p)
	}
	if got := len(f.snapshot().Links); got != 1 {
		t.Errorf("связей на доске %d, ожидалась одна", got)
	}
}

// Колонку можно назвать: подзадачу, которую уже делают, заводят сразу
// в работе, и лишний перенос после создания — это лишний шаг.
func TestCreateSubtaskRespectsNamedColumn(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	parent := f.createCard("Выпустить релиз", cols[0].ID)

	res := f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Уже делаем", "columnId": cols[1].ID})

	for _, c := range res.Patch.Cards {
		if c.ID != parent && c.ColumnID != cols[1].ID {
			t.Fatalf("подзадача легла в %s, ожидалось %s", c.ColumnID, cols[1].ID)
		}
	}
}

// Название пустое или родителя нет — карточка не заводится. Проверяется
// именно отсутствие следов: операция, оборвавшаяся после создания
// карточки, оставила бы её на доске.
func TestCreateSubtaskRefusesWithoutTitleOrParent(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)

	if _, err := f.apply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "   "}); err == nil {
		t.Fatal("пустое название принято")
	}
	if _, err := f.apply("CREATE_SUBTASK", map[string]any{
		"parentCardId": uuid.NewString(), "title": "Сирота"}); err == nil {
		t.Fatal("подзадача заведена без родителя")
	}

	snap := f.snapshot()
	if len(snap.Cards) != 1 {
		t.Fatalf("на доске %d карточек, ожидалась одна — родитель", len(snap.Cards))
	}
	if len(snap.Links) != 0 {
		t.Errorf("связей %d, ожидалось ни одной", len(snap.Links))
	}
}

// --- постановка работы на доску соседей (этап 11.4) ---

// boardVersion читает версию доски напрямую: снимок отдаёт её только для
// той доски, которую спрашивают, а здесь нужны обе.
func (f *fixture) boardVersion(boardID string) int64 {
	f.t.Helper()
	var v int64
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select version from boards where id = $1`, boardID).Scan(&v)
	})
	return v
}

// Постановка задачи соседям — это карточка на их доске, а не заявка рядом
// с ней. Отсюда и проверки: работа лежит у них, связь видна с обеих сторон,
// прогресс родителя её считает.
func TestSubtaskLandsOnNeighbourBoard(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	neighbour, neighbourQueue, _ := f.secondBoard()
	before := f.boardVersion(neighbour)

	f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Проверить нагрузку", "boardId": neighbour})

	// Карточка лежит у соседей, в их первой колонке, с их номером.
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, neighbour)
	if err != nil {
		t.Fatal(err)
	}
	var placed *Card
	for i, c := range snap.Cards {
		if c.Title == "Проверить нагрузку" {
			placed = &snap.Cards[i]
		}
	}
	if placed == nil {
		t.Fatal("подзадача не появилась на доске соседей")
	}
	if placed.ColumnID != neighbourQueue {
		t.Errorf("подзадача легла в колонку %s, ожидалась первая колонка соседей", placed.ColumnID)
	}
	if !strings.HasPrefix(placed.Number, snap.Board.Key+"-") {
		t.Errorf("номер %q не из пространства доски соседей (%s)", placed.Number, snap.Board.Key)
	}

	// На своей доске карточки нет, а прогресс родителя её считает: работа
	// принадлежит исполнителю, разбиение — заказчику.
	for _, c := range f.snapshot().Cards {
		if c.ID == placed.ID {
			t.Error("подзадача оказалась и на доске заказчика")
		}
	}
	if p := f.card(parent).Progress; p == nil || p.Total != 1 || p.Done != 0 {
		t.Fatalf("прогресс родителя: %+v, ожидалось 0 из 1", p)
	}

	// Версия доски соседей сдвинулась: иначе работа приехала бы к ним молча.
	if after := f.boardVersion(neighbour); after <= before {
		t.Errorf("версия доски соседей %d, была %d — оповестить их нечем", after, before)
	}
}

// Событие принадлежит доске своей карточки. Иначе лента доски заказчика
// показывала бы событие о карточке, которой на ней нет, а у исполнителя
// появление работы не отражалось бы вовсе.
func TestSubtaskEventsBelongToOwnBoards(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	neighbour, _, _ := f.secondBoard()

	f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Проверить нагрузку", "boardId": neighbour})

	var childBoard, parentBoard string
	f.inTenant(func(tx pgx.Tx) error {
		if err := tx.QueryRow(f.ctx, `
			select board_id from card_events
			 where type = 'linked' and card_id <> $1`, parent).Scan(&childBoard); err != nil {
			return err
		}
		return tx.QueryRow(f.ctx, `
			select board_id from card_events
			 where type = 'linked' and card_id = $1`, parent).Scan(&parentBoard)
	})
	if childBoard != neighbour {
		t.Errorf("событие подзадачи записано доске %s, ожидалась доска соседей", childBoard)
	}
	if parentBoard != f.boardID {
		t.Errorf("событие родителя записано доске %s, ожидалась доска заказчика", parentBoard)
	}
}

// Заказ не обходит правил доски-получателя: жёсткий лимит их колонки
// отвечает тем же конфликтом, что и своей.
func TestSubtaskOnNeighbourBoardObeysTheirLimit(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	neighbour, neighbourQueue, _ := f.secondBoard()

	f.cardOn(neighbour, neighbourQueue, "Своя работа")
	f.applyTo(neighbour, "UPDATE_COLUMN", map[string]any{
		"columnId": neighbourQueue, "wipLimit": 1, "wipLimitHard": true})

	_, err := f.apply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Проверить нагрузку", "boardId": neighbour})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ожидался конфликт по лимиту соседей, получено %v", err)
	}
}

// Закрытая доска чужой команды заказов не принимает — и отвечает тем же,
// чем ответила бы на любую попытку в неё написать.
func TestSubtaskOnUnreachableBoardRefused(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	neighbour, _, _ := f.secondBoard()

	// Доска уезжает в команду. Спрашивает не владелец организации — ему
	// подвластно всё, что он видит (0011), — а обычный участник со стороны.
	teamID := f.team("Соседи", nil)
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update boards set team_id = $2, visibility = 'team' where id = $1`,
			neighbour, teamID)
		return err
	})
	outsider := addMember(t, f.svc.db, f.orgID, "member")

	_, err := f.svc.Apply(f.ctx, f.orgID, outsider, f.boardID, Request{
		OperationID: uuid.NewString(),
		Type:        "CREATE_SUBTASK",
		Payload: mustJSON(t, map[string]any{
			"parentCardId": parent, "title": "Проверить нагрузку", "boardId": neighbour}),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ожидалось «доска не найдена», получено %v", err)
	}

	// И ничего не завелось по дороге: отказ целен, как и всякая операция.
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, neighbour)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Cards) != 0 {
		t.Errorf("на доске соседей %d карточек, ожидалось ноль", len(snap.Cards))
	}
}

// Отказ соседей — это архивация карточки. Прежде архивная чужая карточка
// просто выпадала из снимка, и у клиента оставалась одна ветка: «доски
// вам не видно». Отказ читался как отсутствие доступа.
func TestRefusedSubtaskComesBackAsArchivedNotMissing(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	neighbour, _, _ := f.secondBoard()

	res := f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Поднять квоту", "boardId": neighbour})
	_ = res

	var childID string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`select to_card from card_links where from_card = $1 and kind = 'subtask'`,
			parent).Scan(&childID)
	})

	// Соседи работу не взяли.
	f.applyTo(neighbour, "ARCHIVE_CARD", map[string]any{"cardId": childID})

	snap := f.snapshot()
	var seen *LinkedCard
	for i, c := range snap.Linked {
		if c.ID == childID {
			seen = &snap.Linked[i]
		}
	}
	if seen == nil {
		t.Fatal("отказавшая карточка пропала из снимка — связь осталась без второй стороны")
	}
	if !seen.Archived {
		t.Error("карточка вернулась без признака архива")
	}
	if seen.BoardName == "" || seen.ColumnName == "" {
		t.Errorf("о карточке нечего сказать: %+v", *seen)
	}

	// Разбиение её больше не считает: отказ — не сделанная работа
	// и не оставшаяся, а выбывшая.
	if p := f.card(parent).Progress; p != nil {
		t.Errorf("прогресс родителя %+v, ожидалось, что подзадач не осталось", p)
	}
}

// О чужой карточке видно, где она стоит и когда её ждать: одного
// «сделана или нет» мало — «третью неделю в очереди» и «делают со вчера»
// выглядели одинаково.
func TestLinkedCardCarriesColumnAndPromise(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	neighbour, _, neighbourDone := f.secondBoard()

	f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Поднять квоту", "boardId": neighbour})
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update boards set sle_days = 8, sle_probability = 85 where id = $1`, neighbour)
		return err
	})

	linked := f.snapshot().Linked
	if len(linked) != 1 {
		t.Fatalf("связанных карточек %d, ожидалась одна", len(linked))
	}
	if linked[0].ColumnKind != KindQueue {
		t.Errorf("вид колонки %q, ожидалась очередь", linked[0].ColumnKind)
	}
	if linked[0].SLEDays == nil || *linked[0].SLEDays != 8 || linked[0].SLEProbability != 85 {
		t.Errorf("обещание доски исполнителя не доехало: %+v", linked[0])
	}

	// Соседи довели работу до конца — колонка меняется вместе с ней.
	var childID string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`select to_card from card_links where from_card = $1 and kind = 'subtask'`,
			parent).Scan(&childID)
	})
	f.moveOn(neighbour, childID, neighbourDone)
	if got := f.snapshot().Linked[0].ColumnKind; got != KindDone {
		t.Errorf("после переноса вид колонки %q, ожидалось завершение", got)
	}
}

// Карточка, заведённая сразу за финишем, объявляется сделанной.
//
// Отметка времени у неё ставилась и раньше, а исход — нет: два поля
// об одном расходились молча, и такая карточка не попадала ни
// в пропускную способность, ни во время цикла.
func TestCardCreatedPastTheFinishIsDone(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	id := f.createCard("Уже сделано", cols[2].ID)

	c := f.card(id)
	if c.FinishedAt == nil {
		t.Fatal("отметка завершения не поставлена")
	}
	if c.Outcome == nil || *c.Outcome != "done" {
		t.Errorf("исход %v, ожидалось done", c.Outcome)
	}

	// А заведённая в очереди — ни то, ни другое: работа не начиналась.
	fresh := f.card(f.createCard("Ещё не начато", cols[0].ID))
	if fresh.FinishedAt != nil || fresh.Outcome != nil {
		t.Errorf("карточка в очереди объявлена завершённой: %+v", fresh)
	}
}
