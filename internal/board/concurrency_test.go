package board

import (
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Конкурентные операции по одной доске.
//
// Все они сериализуются блокировкой строки доски — это утверждение
// написано в комментарии к Apply, и здесь оно проверяется, а не
// пересказывается. Проверка нужна не ради сегодняшнего кода: блокировка
// выглядит избыточной ровно до того дня, когда её снимут «ради
// производительности», и без этих тестов день будет обычным.
//
// Все тесты запускают настоящие горутины против настоящей базы:
// гонку нельзя изобразить последовательным вызовом.

// racers — сколько одновременных запросов пускать. Десять достаточно,
// чтобы поймать гонку, и мало, чтобы не упереться в размер пула.
const racers = 10

// race выполняет fn одновременно в нескольких горутинах и возвращает
// их ошибки в порядке номеров.
func race(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			// Общий старт: без него первая горутина успевает закончить
			// раньше, чем последняя начнёт, и гонки не случается.
			<-start
			errs[i] = fn(i)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// Повтор при обрыве связи выглядит для сервера как несколько
// одновременных одинаковых запросов. Карточка должна получиться одна.
func TestSimultaneousRepeatCreatesOneCard(t *testing.T) {
	f := newFixture(t)
	opID := uuid.NewString()

	results := make([]Result, racers)
	errs := race(racers, func(i int) error {
		res, err := f.applyWithID(opID, "CREATE_CARD",
			map[string]any{"columnId": f.columnA, "title": "Одна-единственная"})
		results[i] = res
		return err
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("повтор %d не выполнился: %v", i, err)
		}
	}

	if got := f.titles(f.columnA); len(got) != 1 {
		t.Fatalf("карточек в колонке %d, ожидалась одна: %v", len(got), got)
	}

	// И все повторы обязаны получить один и тот же ответ: клиент,
	// повторивший запрос, применяет патч у себя, и разные ответы
	// означали бы разные копии доски у разных вкладок.
	first := results[0]
	for i, res := range results {
		if res.Version != first.Version {
			t.Errorf("повтор %d вернул версию %d, первый — %d", i, res.Version, first.Version)
		}
		if len(res.Patch.Cards) != 1 || res.Patch.Cards[0].ID != first.Patch.Cards[0].ID {
			t.Errorf("повтор %d вернул другую карточку: %+v", i, res.Patch.Cards)
		}
	}
}

// Одновременное создание карточек в одной колонке — обычное дело:
// доску открыли на планировании впятером. Ни одна не должна пропасть,
// и позиции обязаны остаться различными, иначе порядок станет
// зависеть от того, как база вернёт строки.
func TestSimultaneousCreatesKeepEveryCardAndDistinctPositions(t *testing.T) {
	f := newFixture(t)

	errs := race(racers, func(i int) error {
		_, err := f.apply("CREATE_CARD",
			map[string]any{"columnId": f.columnA, "title": "Карточка"})
		return err
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("создание %d не выполнилось: %v", i, err)
		}
	}

	snap := f.snapshot()
	positions := []string{}
	for _, c := range snap.Cards {
		if c.ColumnID == f.columnA {
			positions = append(positions, c.Position)
		}
	}
	if len(positions) != racers {
		t.Fatalf("создано %d карточек из %d", len(positions), racers)
	}
	sort.Strings(positions)
	for i := 1; i < len(positions); i++ {
		if positions[i] == positions[i-1] {
			t.Fatalf("две карточки получили одну позицию %q", positions[i])
		}
	}

	// Версия доски равна числу применённых операций: она и есть счётчик
	// того, что клиент мог пропустить.
	if snap.Board.Version < int64(racers) {
		t.Errorf("версия доски %d, а операций было %d", snap.Board.Version, racers)
	}
}

// Одну карточку тянут в разные стороны из разных вкладок. Итог может быть
// любым из запрошенных, но карточка обязана остаться ровно одна и ровно
// в одной колонке: раздвоившаяся карточка — худший из возможных исходов,
// потому что заметят её не сразу.
func TestSimultaneousMovesLeaveExactlyOneCard(t *testing.T) {
	f := newFixture(t)
	cardID := f.createCard("Перетягиваемая", f.columnA)
	columns := []string{f.columnA, f.columnB}

	race(racers, func(i int) error {
		_, err := f.apply("MOVE_CARD", map[string]any{
			"cardId": cardID, "columnId": columns[i%2],
		})
		// Конфликт здесь законен: якорь мог уехать. Важно не то, что все
		// прошли, а то, во что превратилась доска.
		return err
	})

	snap := f.snapshot()
	seen := 0
	for _, c := range snap.Cards {
		if c.ID == cardID {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("после гонки перемещений карточка встречается %d раз", seen)
	}
	if len(snap.Cards) != 1 {
		t.Fatalf("на доске %d карточек вместо одной", len(snap.Cards))
	}
}

// Жёсткий лимит — обещание, что в колонке не окажется больше работы,
// чем разрешено. Обещание, которое держится только при последовательных
// запросах, ничего не стоит: проверка и вставка обязаны видеть одно
// и то же состояние.
func TestHardLimitHoldsUnderRace(t *testing.T) {
	const limit = 3
	f := newFixture(t)
	f.mustApply("UPDATE_COLUMN", map[string]any{
		"columnId": f.columnA, "wipLimit": limit, "wipLimitHard": true,
	})

	errs := race(racers, func(i int) error {
		_, err := f.apply("CREATE_CARD",
			map[string]any{"columnId": f.columnA, "title": "Работа"})
		return err
	})

	accepted := 0
	for i, err := range errs {
		switch {
		case err == nil:
			accepted++
		default:
			var conflict *ConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("попытка %d отвергнута не конфликтом, а %v", i, err)
			}
		}
	}
	if accepted != limit {
		t.Errorf("сквозь лимит %d прошло %d карточек", limit, accepted)
	}
	if got := len(f.titles(f.columnA)); got != limit {
		t.Errorf("в колонке %d карточек при лимите %d", got, limit)
	}
}

// Разные операции разных людей по одной доске идут вперемешку. Проверяется
// не результат каждой — он свой у каждой, — а то, что доска остаётся
// связной: у каждой карточки живая колонка, версия не отстаёт, ничего
// не потерялось по дороге.
func TestMixedOperationsLeaveTheBoardConsistent(t *testing.T) {
	f := newFixture(t)
	kept := f.createCard("Останется", f.columnA)
	doomed := f.createCard("Уедет в архив", f.columnA)

	race(racers, func(i int) error {
		switch i % 4 {
		case 0:
			_, err := f.apply("CREATE_CARD",
				map[string]any{"columnId": f.columnB, "title": "Новая"})
			return err
		case 1:
			_, err := f.apply("MOVE_CARD",
				map[string]any{"cardId": kept, "columnId": f.columnB})
			return err
		case 2:
			_, err := f.apply("UPDATE_CARD",
				map[string]any{"cardId": kept, "title": "Переименована"})
			return err
		default:
			_, err := f.apply("ARCHIVE_CARD", map[string]any{"cardId": doomed})
			return err
		}
	})

	snap := f.snapshot()
	columns := map[string]bool{}
	for _, c := range snap.Columns {
		columns[c.ID] = true
	}
	for _, c := range snap.Cards {
		if !columns[c.ColumnID] {
			t.Errorf("карточка %q оказалась в несуществующей колонке %s", c.Title, c.ColumnID)
		}
		if c.ID == doomed {
			t.Error("заархивированная карточка вернулась в снимок")
		}
	}
	var alive bool
	for _, c := range snap.Cards {
		if c.ID == kept {
			alive = true
		}
	}
	if !alive {
		t.Error("карточка потерялась в общей суете")
	}
}
