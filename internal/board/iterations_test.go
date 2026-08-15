package board

import (
	"errors"
	"testing"
	"time"
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
