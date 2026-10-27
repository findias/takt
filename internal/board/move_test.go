package board

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Перенос карточки на другую доску (ROADMAP 40): переносится одна
// и та же карточка — с обсуждением, историей и пунктами, — а то, что
// на новой доске не действует, снимается и называется.

func (f *fixture) moveToBoard(cardID, target, columnID string) (Result, error) {
	f.t.Helper()
	raw := mustJSON(f.t, map[string]any{"cardId": cardID, "boardId": target, "toColumnId": columnID})
	return f.svc.Apply(f.ctx, f.orgID, f.actorID, f.boardID,
		Request{OperationID: uuid.NewString(), Type: "MOVE_CARD_TO_BOARD", Payload: raw})
}

func (f *fixture) boardOf(cardID string) (boardID, number string) {
	f.t.Helper()
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select board_id::text, number from cards where id = $1`, cardID).
			Scan(&boardID, &number)
	})
	return boardID, number
}

func TestCardMovesToAnotherBoardWithItsHistory(t *testing.T) {
	f := newFixture(t)
	target, queue, done := f.secondBoard()
	cardID := f.createCard("Согласовать смету", f.columnA)
	_, oldNumber := f.boardOf(cardID)
	if _, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, cardID,
		"Смета у юристов", nil, nil); err != nil {
		t.Fatal(err)
	}

	// Метка организации действует везде и остаётся; метка своей доски
	// на чужой не действует и снимается.
	everywhere := f.label("Срочно")
	own := f.scopedLabel("Наша", "", f.boardID)
	for _, l := range []string{everywhere.ID, own.ID} {
		f.mustApply("LABEL_CARD", map[string]any{"cardId": cardID, "labelId": l})
	}
	it := f.iteration("Неделя 44")
	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": cardID, "iterationId": it.ID})

	preview, err := f.svc.PreviewMove(f.ctx, f.orgID, f.actorID, f.boardID, cardID, target)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(preview.DroppedLabels, []string{"Наша"}) || preview.Iteration != "Неделя 44" ||
		len(preview.Unseen) != 0 || preview.OtherLevel || len(preview.Columns) < 3 {
		t.Fatalf("предпросмотр не называет снимаемое: %+v", preview)
	}

	res, err := f.moveToBoard(cardID, target, done)
	if err != nil {
		t.Fatalf("перенос не выполнился: %v", err)
	}
	if !slices.Contains(res.Patch.RemovedCardIDs, cardID) {
		t.Errorf("своя доска не узнала, что карточка ушла: %+v", res.Patch)
	}
	for _, c := range f.snapshot().Cards {
		if c.ID == cardID {
			t.Fatal("карточка осталась на прежней доске")
		}
	}

	board, number := f.boardOf(cardID)
	if board != target {
		t.Fatalf("карточка не на доске назначения: %s", board)
	}
	var key string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select key from boards where id = $1`, target).Scan(&key)
	})
	if !strings.HasPrefix(number, key+"-") || number == oldNumber {
		t.Errorf("номер не выдан доской назначения: было %s, стало %s (ключ %s)", oldNumber, number, key)
	}

	moved := f.cardOnBoard(target, cardID)
	if moved.ColumnID != done || moved.FinishedAt == nil || moved.Outcome == nil || *moved.Outcome != "done" {
		t.Errorf("в колонке финиша карточка не стала сделанной: %+v", moved)
	}
	if f.cardOnBoard(target, cardID).ColumnID == queue {
		t.Error("выбранная колонка проигнорирована")
	}
	if got := f.labelsOn(target, cardID); !slices.Equal(got, []string{"Срочно"}) {
		t.Errorf("метки после переноса: %v, ожидалась только «Срочно»", got)
	}
	var open int
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`select count(*) from iteration_cards where card_id = $1 and removed_at is null`, cardID).Scan(&open)
	})
	if open != 0 {
		t.Error("карточка осталась в итерации прежней доски")
	}

	// История одна на всю жизнь карточки: открытая на новой доске, она
	// показывает и заведение на прежней.
	feed, err := f.svc.Events(f.ctx, f.orgID, f.actorID, target, cardID, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, e := range feed.Events {
		kinds = append(kinds, e.Type)
	}
	for _, want := range []string{"created", "moved_out", "moved_in"} {
		if !slices.Contains(kinds, want) {
			t.Errorf("в истории карточки нет %s: %v", want, kinds)
		}
	}

	// По старому номеру карточку находят: на него ссылается переписка.
	found, err := f.svc.SearchCards(f.ctx, f.orgID, f.actorID, oldNumber)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 || found[0].ID != cardID || found[0].Number != number {
		t.Errorf("поиск по прежнему номеру %s не нашёл карточку: %+v", oldNumber, found)
	}
}

// cardOnBoard — карточка из снимка названной доски.
func (f *fixture) cardOnBoard(boardID, cardID string) Card {
	f.t.Helper()
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, c := range snap.Cards {
		if c.ID == cardID {
			return c
		}
	}
	f.t.Fatalf("карточки нет на доске %s", boardID)
	return Card{}
}

func (f *fixture) labelsOn(boardID, cardID string) []string {
	f.t.Helper()
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	names := []string{}
	for _, id := range snap.CardLabels[cardID] {
		if l := boardLabelIn(snap, id); l != nil {
			names = append(names, l.Name)
		}
	}
	slices.Sort(names)
	return names
}

// Видящий только новую доску видит перенесённое целиком: карточку,
// обсуждение и историю с момента переноса.
func TestMovedCardIsWholeForThoseWhoSeeOnlyTheNewBoard(t *testing.T) {
	f := newFixture(t)
	ours := f.team("Ядро", nil)
	theirs := f.team("Поставки", nil)
	f.assignBoard(ours)
	target := f.boardOfTeam("Поставки", &theirs)
	reader := f.inviteMember("Вера")
	f.joins(reader, theirs)

	cardID := f.createCard("Перевезти стенд", f.columnA)
	if _, err := f.svc.AddComment(f.ctx, f.orgID, f.actorID, f.boardID, cardID,
		"Стенд в серверной", nil, nil); err != nil {
		t.Fatal(err)
	}
	if comments, _ := f.svc.Comments(f.ctx, f.orgID, reader, cardID); len(comments) != 0 {
		t.Fatal("до переноса чужое обсуждение уже видно — проверять нечего")
	}

	if _, err := f.moveToBoard(cardID, target, ""); err != nil {
		t.Fatalf("перенос не выполнился: %v", err)
	}
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, reader, target)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(snap.Cards, func(c Card) bool { return c.ID == cardID }) {
		t.Fatal("на новой доске карточки не видно")
	}
	if snap.Cards[0].ColumnID != snap.Columns[0].ID {
		t.Error("без выбранной колонки карточка легла не в первую")
	}
	comments, err := f.svc.Comments(f.ctx, f.orgID, reader, cardID)
	if err != nil || len(comments) != 1 {
		t.Errorf("обсуждение осталось на прежней доске: %v, %v", comments, err)
	}
}

func TestMoveRefusesWhileAssigneeCannotSeeTheNewBoard(t *testing.T) {
	f := newFixture(t)
	ours := f.team("Ядро", nil)
	theirs := f.team("Поставки", nil)
	f.assignBoard(ours)
	target := f.boardOfTeam("Поставки", &theirs)
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, target, VisibilityTeam, &theirs); err != nil {
		t.Fatal(err)
	}
	boris := f.inviteMember("Борис")
	f.joins(boris, ours)

	cardID := f.createCard("Починить сборку", f.columnA)
	f.mustApply("ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": boris})

	preview, err := f.svc.PreviewMove(f.ctx, f.orgID, f.actorID, f.boardID, cardID, target)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(preview.Unseen, []string{"Борис"}) {
		t.Errorf("предпросмотр не называет исполнителя без доступа: %v", preview.Unseen)
	}

	_, err = f.moveToBoard(cardID, target, "")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || !strings.Contains(err.Error(), "Борис") ||
		!strings.Contains(err.Error(), "снимите") {
		t.Fatalf("отказ не называет исполнителя и что делать: %v", err)
	}
	if board, _ := f.boardOf(cardID); board != f.boardID {
		t.Error("после отказа карточка всё равно переехала")
	}

	// Открыли доску — переносится.
	f.joins(boris, theirs)
	if _, err := f.moveToBoard(cardID, target, ""); err != nil {
		t.Fatalf("исполнитель видит доску, а перенос отказал: %v", err)
	}
	// Известие о назначении написано прежней доске, а ведёт туда, где
	// карточка теперь: по старой ссылке она бы не открылась.
	page, err := f.svc.Notifications(f.ctx, f.orgID, boris)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].BoardID != target || page.Unread != 1 {
		t.Errorf("известие о назначении не ведёт на новую доску: %+v", page)
	}
}

func TestItemsTravelWithTheCardAndForeignPartsStay(t *testing.T) {
	f := newFixture(t)
	target, _, _ := f.secondBoard()
	third, thirdQueue, _ := f.secondBoard()

	parent := f.createCard("Выпустить релиз", f.columnA)
	item := f.subtask(parent, "Прогнать нагрузочные")
	f.mustApply("SET_CARD_DONE", map[string]any{"cardId": item, "done": true})
	f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Обновить стенд", "boardId": third, "columnId": thirdQueue})
	var part string
	for _, c := range f.cardsOn(third) {
		if c.Title == "Обновить стенд" {
			part = c.ID
		}
	}

	res, err := f.moveToBoard(parent, target, "")
	if err != nil {
		t.Fatalf("перенос не выполнился: %v", err)
	}
	if !slices.Contains(res.Patch.RemovedCardIDs, item) {
		t.Error("своя доска не узнала, что пункт уехал с родителем")
	}
	if board, _ := f.boardOf(item); board != target {
		t.Error("пункт остался на прежней доске отдельной карточкой")
	}
	if f.cardOnBoard(target, item).DoneAt == nil {
		t.Error("сделанный пункт после переноса снова не сделан")
	}
	if board, _ := f.boardOf(part); board != third {
		t.Error("часть на доске соседей переехала вслед за родителем")
	}
	if p := f.cardOnBoard(target, parent).Progress; p == nil || p.Total != 2 {
		t.Errorf("родитель потерял части: %+v", p)
	}
}

// Пункт чужого родителя, перенесённый на другую доску, становится работой
// соседей — и родитель на прежней доске узнаёт об этом из патча.
func TestMovedItemBecomesForeignPartOfItsParent(t *testing.T) {
	f := newFixture(t)
	target, _, _ := f.secondBoard()
	parent := f.createCard("Выпустить релиз", f.columnA)
	item := f.subtask(parent, "Прогнать нагрузочные")

	res, err := f.moveToBoard(item, target, "")
	if err != nil {
		t.Fatalf("перенос не выполнился: %v", err)
	}
	if p := patchedProgress(t, res, parent); p == nil || p.Total != 1 {
		t.Errorf("родитель в патче не пересчитан: %+v", p)
	}
	if board, _ := f.boardOf(item); board != target {
		t.Error("пункт не переехал")
	}
}

func TestMoveBetweenPortfolioAndTeamBoardIsRefused(t *testing.T) {
	f := newFixture(t)
	pf, _ := f.portfolio()
	cardID := f.createCard("Задача команды", f.columnA)

	preview, err := f.svc.PreviewMove(f.ctx, f.orgID, f.actorID, f.boardID, cardID, pf)
	if err != nil || !preview.OtherLevel {
		t.Errorf("предпросмотр не предупреждает о другом уровне: %+v, %v", preview, err)
	}
	_, err = f.moveToBoard(cardID, pf, "")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || !strings.Contains(err.Error(), "подзадачу") {
		t.Fatalf("отказ не объясняет, как поставить работу эпику: %v", err)
	}
}

func TestMoveToTheSameBoardIsABadRequest(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Никуда", f.columnA)
	if _, err := f.moveToBoard(cardID, f.boardID, ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("перенос на свою же доску не отвергнут: %v", err)
	}
}

func (f *fixture) cardsOn(boardID string) []Card {
	f.t.Helper()
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	return snap.Cards
}
