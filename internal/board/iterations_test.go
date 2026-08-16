package board

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Итерации. Проверяется главное свойство модели: вхождение — интервал,
// поэтому состав восстанавливается на любой момент, а не только «сейчас».

func (f *fixture) iteration(name string) Iteration {
	f.t.Helper()
	it, err := f.svc.CreateIteration(f.ctx, f.orgID, f.actorID, f.boardID,
		name, "Довести до стенда", "2026-08-01", "2026-08-14")
	if err != nil {
		f.t.Fatalf("создание итерации %q: %v", name, err)
	}
	return it
}

func TestIterationKeepsWhatWasInItAtEveryMoment(t *testing.T) {
	f := newFixture(t)
	it := f.iteration("Спринт 1")

	planned := f.createCard("Запланировано", f.columnA)
	f.mustApply("ADD_TO_ITERATION", map[string]any{
		"cardId": planned, "iterationId": it.ID})

	afterStart := time.Now()
	time.Sleep(20 * time.Millisecond)

	// Добавили после старта — обычное дело, и именно его поле на карточке
	// не отличило бы от запланированного.
	added := f.createCard("Прилетело по дороге", f.columnA)
	f.mustApply("ADD_TO_ITERATION", map[string]any{
		"cardId": added, "iterationId": it.ID})
	// И выкинули одну — тоже обычное дело.
	f.mustApply("REMOVE_FROM_ITERATION", map[string]any{
		"cardId": planned, "iterationId": it.ID})

	now, err := f.svc.CardsAt(f.ctx, f.orgID, f.actorID, it.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(now) != 1 || now[0] != added {
		t.Fatalf("сейчас в итерации %v, ожидалась только прилетевшая карточка", now)
	}

	then, err := f.svc.CardsAt(f.ctx, f.orgID, f.actorID, it.ID, &afterStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(then) != 1 || then[0] != planned {
		t.Fatalf("на момент старта в итерации %v, ожидалась запланированная", then)
	}
}

// Закрытие замораживает состав: после него итерация — утверждение о том,
// что было сделано, а не черновик.
func TestClosedIterationFreezesItsContent(t *testing.T) {
	f := newFixture(t)
	it := f.iteration("Спринт 1")
	inside := f.createCard("Внутри", f.columnA)
	f.mustApply("ADD_TO_ITERATION", map[string]any{
		"cardId": inside, "iterationId": it.ID})

	if err := f.svc.CloseIteration(f.ctx, f.orgID, f.actorID, f.boardID, it.ID); err != nil {
		t.Fatalf("закрытие: %v", err)
	}

	late := f.createCard("Опоздавшая", f.columnA)
	if _, err := f.apply("ADD_TO_ITERATION", map[string]any{
		"cardId": late, "iterationId": it.ID}); !errors.Is(err, ErrIterationClosed) {
		t.Errorf("в закрытую итерацию добавили карточку: %v", err)
	}
	if _, err := f.apply("REMOVE_FROM_ITERATION", map[string]any{
		"cardId": inside, "iterationId": it.ID}); !errors.Is(err, ErrIterationClosed) {
		t.Errorf("из закрытой итерации убрали карточку: %v", err)
	}

	// Состав закрытой итерации читается на момент закрытия — без него
	// «сейчас» и «тогда» разъехались бы при любом дальнейшем движении.
	cards, err := f.svc.CardsAt(f.ctx, f.orgID, f.actorID, it.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0] != inside {
		t.Errorf("состав закрытой итерации %v", cards)
	}

	// Повторное закрытие ничего не меняет и говорит об этом.
	if err := f.svc.CloseIteration(f.ctx, f.orgID, f.actorID, f.boardID, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное закрытие: %v", err)
	}
}

// Две итерации сразу означали бы, что сделанная работа посчитана дважды.
func TestCardGoesIntoOnlyOneIterationAtATime(t *testing.T) {
	f := newFixture(t)
	first := f.iteration("Спринт 1")
	second := f.iteration("Спринт 2")
	card := f.createCard("Задача", f.columnA)

	f.mustApply("ADD_TO_ITERATION", map[string]any{
		"cardId": card, "iterationId": first.ID})

	if _, err := f.apply("ADD_TO_ITERATION", map[string]any{
		"cardId": card, "iterationId": second.ID}); !errors.Is(err, ErrCardInAnotherIteration) {
		t.Fatalf("карточка попала в две итерации сразу: %v", err)
	}

	// Перенос в следующий спринт — это выход из одного и вход в другой,
	// и оба факта остаются в истории.
	f.mustApply("REMOVE_FROM_ITERATION", map[string]any{
		"cardId": card, "iterationId": first.ID})
	f.mustApply("ADD_TO_ITERATION", map[string]any{
		"cardId": card, "iterationId": second.ID})

	was, err := f.svc.CardsAt(f.ctx, f.orgID, f.actorID, first.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(was) != 0 {
		t.Errorf("карточка осталась в первом спринте: %v", was)
	}
}

func TestIterationDatesAreChecked(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.CreateIteration(f.ctx, f.orgID, f.actorID, f.boardID,
		"Задом наперёд", "", "2026-08-14", "2026-08-01")
	if !errors.Is(err, ErrBadRequest) {
		t.Errorf("итерация с концом раньше начала: %v", err)
	}
	if _, err := f.svc.CreateIteration(f.ctx, f.orgID, f.actorID, f.boardID,
		"  ", "", "2026-08-01", "2026-08-14"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("итерация без названия: %v", err)
	}
}

// Добавление и удаление попадают в историю карточки: по ней потом
// объясняют, почему спринт не сошёлся.
func TestIterationChangesLandInCardHistory(t *testing.T) {
	f := newFixture(t)
	it := f.iteration("Спринт 1")
	card := f.createCard("Задача", f.columnA)

	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": card, "iterationId": it.ID})
	f.mustApply("REMOVE_FROM_ITERATION", map[string]any{"cardId": card, "iterationId": it.ID})

	feed, err := f.svc.Events(f.ctx, f.orgID, f.actorID, f.boardID, card, nil)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range feed.Events {
		kinds[e.Type] = true
	}
	if !kinds["iteration_added"] || !kinds["iteration_removed"] {
		t.Errorf("в истории карточки нет следов итерации: %+v", feed.Events)
	}

	// Повтор безобиден и не плодит записей.
	before := len(feed.Events)
	f.mustApply("REMOVE_FROM_ITERATION", map[string]any{"cardId": card, "iterationId": it.ID})
	feed, _ = f.svc.Events(f.ctx, f.orgID, f.actorID, f.boardID, card, nil)
	if len(feed.Events) != before {
		t.Errorf("повтор удаления добавил записей: было %d, стало %d", before, len(feed.Events))
	}
}

func TestIterationsOfForeignBoardAreInvisible(t *testing.T) {
	f := newFixture(t)
	f.iteration("Спринт 1")
	other := newFixture(t)

	list, err := f.svc.Iterations(f.ctx, other.orgID, other.actorID, f.boardID)
	if err != nil {
		t.Fatalf("чужие итерации вернули ошибку вместо пустоты: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("из чужой организации видно %d итераций", len(list))
	}
}

// --- отчёт по итерации (этап 11.6) ---

// Отчёт отвечает на вопрос, ради которого вхождение сделано интервалом:
// что было в составе на момент закрытия, что из этого доведено до конца,
// что прилетело после начала и что выкинули по дороге.
func TestIterationReportAnswersWhatWasInTheSprint(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	done := cols[2].ID
	it := f.iteration("Спринт 1")

	// Итерация начинается в прошлом: иначе «добавлено после начала»
	// неотличимо — всё, что заводит тест, заводится сегодня.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`update iterations set starts_on = current_date - 7 where id = $1`, it.ID)
		return err
	})

	planned := f.createCard("Запланировано и сделано", f.columnA)
	kept := f.createCard("Запланировано и не сделано", f.columnA)
	late := f.createCard("Прилетело по дороге", f.columnA)
	dropped := f.createCard("Выкинуто по дороге", f.columnA)
	for _, id := range []string{planned, kept, late, dropped} {
		f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": id, "iterationId": it.ID})
	}
	// Запланированное вошло в день начала, прилетевшее — сегодня.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			update iteration_cards set added_at = (current_date - 7)::timestamptz
			 where iteration_id = $1 and card_id <> $2`, it.ID, late)
		return err
	})

	f.mustApply("UPDATE_CARD", map[string]any{"cardId": planned, "estimate": 3})
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": kept, "estimate": 5})
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": late, "estimate": 2})
	f.mustApply("MOVE_CARD", map[string]any{"cardId": planned, "toColumnId": done, "place": "end"})
	f.mustApply("REMOVE_FROM_ITERATION", map[string]any{"cardId": dropped, "iterationId": it.ID})

	if err := f.svc.CloseIteration(f.ctx, f.orgID, f.actorID, f.boardID, it.ID); err != nil {
		t.Fatal(err)
	}

	rep, err := f.svc.IterationReport(f.ctx, f.orgID, f.actorID, f.boardID, it.ID)
	if err != nil {
		t.Fatalf("отчёт: %v", err)
	}
	if rep.Iteration.ClosedAt == nil {
		t.Fatal("отчёт по закрытой итерации не знает о закрытии")
	}
	if rep.Totals.Committed != 3 || rep.Totals.Done != 1 || rep.Totals.Dropped != 1 {
		t.Errorf("сводка %+v, ожидалось 3 в составе, 1 сделана, 1 выкинута", rep.Totals)
	}
	// Все три карточки состава оценены — весу можно верить.
	if !rep.Totals.ByWeight || rep.Totals.CommittedWeight != 10 || rep.Totals.DoneWeight != 3 {
		t.Errorf("вес %+v, ожидалось 3 из 10", rep.Totals)
	}

	byTitle := map[string]IterationCard{}
	for _, c := range rep.Cards {
		byTitle[c.Title] = c
	}
	if !byTitle["Запланировано и сделано"].Done {
		t.Error("сделанная карточка не отмечена сделанной")
	}
	if byTitle["Запланировано и не сделано"].Done {
		t.Error("несделанная карточка отмечена сделанной")
	}
	if !byTitle["Прилетело по дороге"].LateAdd {
		t.Error("добавленная после начала не отмечена")
	}
	if byTitle["Запланировано и сделано"].LateAdd {
		t.Error("запланированная отмечена как добавленная после начала")
	}
	if !byTitle["Выкинуто по дороге"].Dropped {
		t.Error("выкинутая не отмечена выкинутой")
	}
	// Выкинутая осталась в отчёте: о ней и спрашивают на разборе.
	if len(rep.Cards) != 4 {
		t.Errorf("в отчёте %d карточек, ожидалось 4 вместе с выкинутой", len(rep.Cards))
	}
}

// Состав застыл в момент закрытия: работа, законченная позже, в этой
// итерации не сделана. Отчёт, считающий иначе, льстит.
func TestIterationReportCountsOnlyWorkFinishedBeforeClosing(t *testing.T) {
	f := newFixture(t)
	done := f.columns()[2].ID
	it := f.iteration("Спринт 2")
	id := f.createCard("Доделано после закрытия", f.columnA)
	f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": id, "iterationId": it.ID})
	if err := f.svc.CloseIteration(f.ctx, f.orgID, f.actorID, f.boardID, it.ID); err != nil {
		t.Fatal(err)
	}
	f.mustApply("MOVE_CARD", map[string]any{"cardId": id, "toColumnId": done, "place": "end"})

	rep, err := f.svc.IterationReport(f.ctx, f.orgID, f.actorID, f.boardID, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Committed != 1 || rep.Totals.Done != 0 {
		t.Errorf("сводка %+v, ожидалось 1 в составе и ни одной сделанной", rep.Totals)
	}
}

// Неоценённая карточка в составе делает вес недостоверным целиком:
// сумма без неё врёт в меньшую сторону.
func TestIterationReportDoesNotTrustPartialWeight(t *testing.T) {
	f := newFixture(t)
	it := f.iteration("Спринт 3")
	a := f.createCard("Оценена", f.columnA)
	b := f.createCard("Не оценена", f.columnA)
	f.mustApply("UPDATE_CARD", map[string]any{"cardId": a, "estimate": 4})
	for _, id := range []string{a, b} {
		f.mustApply("ADD_TO_ITERATION", map[string]any{"cardId": id, "iterationId": it.ID})
	}

	rep, err := f.svc.IterationReport(f.ctx, f.orgID, f.actorID, f.boardID, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.ByWeight {
		t.Error("вес объявлен достоверным, хотя одна карточка состава не оценена")
	}
	if rep.Iteration.ClosedAt != nil {
		t.Error("открытая итерация показана закрытой")
	}
}
