//go:build load

package board

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Поведение под нагрузкой.
//
// Отделено тегом сборки: эти проверки идут минуты, а не секунды, и в общей
// проверке им не место — общую проверку запускают перед каждым коммитом,
// и всё, что делает её долгой, приводит к тому, что её перестают
// запускать. Запуск: make load.
//
// Здесь нарочно нет порогов вида «снимок за 50 мс». Такой порог измеряет
// машину, на которой его написали, и краснеет у всех остальных. Измеряется
// другое — форма зависимости: как растёт время, когда данных становится
// больше. Линейный рост нормален, квадратичный означает, что где-то
// появился запрос на каждую карточку, и заметить это надо до того,
// как у заказчика соберётся доска на пятьсот карточек.
//
// Масштаб взят из README: сто тридцать человек, доска в сотни карточек.
// Проверять миллионы значило бы проверять чужой продукт.

// Сколько карточек считать «нормальной» и «большой» доской. Разница
// в пять раз: её достаточно, чтобы квадратичный рост стал очевиден,
// и мало, чтобы проверка укладывалась в минуты.
const (
	normalBoard = 100
	largeBoard  = 500
)

func fillBoard(t *testing.T, f *fixture, cards int) {
	t.Helper()
	for i := 0; i < cards; i++ {
		f.createCard(fmt.Sprintf("Карточка %d", i), f.columnA)
	}
}

func timeSnapshot(t *testing.T, f *fixture, times int) time.Duration {
	t.Helper()
	// Один прогрев: первый запрос платит за планы и прогрев кеша базы,
	// и мерить его вместе с остальными значит мерить холодный старт.
	f.snapshot()

	start := time.Now()
	for i := 0; i < times; i++ {
		f.snapshot()
	}
	return time.Since(start) / time.Duration(times)
}

// Снимок доски отдаётся одним ответом — так решено сознательно, и это
// решение держится ровно до тех пор, пока время снимка растёт линейно.
func TestSnapshotScalesLinearlyWithCards(t *testing.T) {
	small := newFixture(t)
	fillBoard(t, small, normalBoard)
	perSmall := timeSnapshot(t, small, 20)

	big := newFixture(t)
	fillBoard(t, big, largeBoard)
	perBig := timeSnapshot(t, big, 20)

	ratio := float64(perBig) / float64(perSmall)
	t.Logf("снимок: %d карточек — %v, %d карточек — %v, отношение %.1f×",
		normalBoard, perSmall, largeBoard, perBig, ratio)

	// Данных в пять раз больше. Замеры показывают, что время почти
	// не меняется: на нашем масштабе всё съедают обращения к базе,
	// а не карточки. Порог поэтому поставлен туда, где начинается беда,
	// а не туда, где кончается идеал: втрое при пятикратном объёме — это
	// ещё лучше линейного, а вот больше означает запрос на каждую
	// карточку.
	if ratio > 3 {
		t.Errorf("время снимка растёт быстрее, чем данные: в %.1f× при пятикратном объёме", ratio)
	}
}

// Все операции по одной доске сериализуются блокировкой её строки.
// Вопрос не в том, сколько операций в секунду выходит — это упирается
// в скорость самой базы, — а в том, не портит ли толпа то, что и так
// идёт по очереди. Замер сравнивает толпу с последовательной работой:
// при честной сериализации они должны быть примерно равны, а разъехаться
// они могут только на заторе — когда участники начинают мешать друг
// другу, а не просто ждать.
func TestCrowdIsNoWorseThanQueue(t *testing.T) {
	const crowd = 30
	const each = 5
	const total = crowd * each

	// Сначала — то же число операций подряд, чтобы знать цену одной.
	alone := newFixture(t)
	sequentialStart := time.Now()
	for i := 0; i < total; i++ {
		alone.createCard(fmt.Sprintf("Подряд %d", i), alone.columnA)
	}
	sequential := time.Since(sequentialStart)

	f := newFixture(t)
	start := time.Now()

	var wg sync.WaitGroup
	errs := make([]error, crowd)
	wg.Add(crowd)
	for i := 0; i < crowd; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				if _, err := f.apply("CREATE_CARD", map[string]any{
					"columnId": f.columnA,
					"title":    fmt.Sprintf("Карточка %d-%d", i, j),
				}); err != nil {
					errs[i] = err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("поток %d сломался под нагрузкой: %v", i, err)
		}
	}

	t.Logf("%d операций: подряд — %v (%.0f в секунду), толпой в %d потоков — %v (%.0f в секунду)",
		total, sequential, float64(total)/sequential.Seconds(),
		crowd, elapsed, float64(total)/elapsed.Seconds())

	// Главное — не скорость, а сохранность: ни одна операция не потерялась
	// и ни одна не применилась дважды.
	if got := len(f.titles(f.columnA)); got != total {
		t.Errorf("создано %d карточек из %d", got, total)
	}
	snap := f.snapshot()
	if snap.Board.Version < int64(total) {
		t.Errorf("версия доски %d при %d операциях", snap.Board.Version, total)
	}

	// Толпа не должна оказаться заметно хуже очереди. Двойной запас —
	// на планировщик и на разогрев соединений в пуле; всё, что хуже,
	// означает, что участники мешают друг другу, а не ждут своей очереди.
	if elapsed > 2*sequential {
		t.Errorf("толпа медленнее очереди в %.1f× — похоже на затор, а не на ожидание",
			float64(elapsed)/float64(sequential))
	}
}

// Пустая доска в организации, где рядом лежит набитая, должна отдаваться
// так же быстро, как пустая доска в пустой организации.
//
// Проверяется этим не скорость, а область видимости запросов: политики
// выполняются на каждое обращение, и запрос, заглядывающий во все
// карточки организации, а не только своей доски, здесь и обнаружится.
// Такой запрос незаметен, пока организация маленькая, и становится
// заметен ровно у самого крупного заказчика.
func TestNeighbourOfBusyBoardIsNotSlowedDown(t *testing.T) {
	const times = 20

	// Пустая доска в пустой организации — образец для сравнения.
	quiet := newFixture(t)
	quietEmpty := emptyBoardOf(t, quiet)
	perQuiet := timeBoard(t, quiet, quietEmpty, times)

	// Такая же пустая доска, но по соседству лежит набитая.
	busy := newFixture(t)
	fillBoard(t, busy, largeBoard)
	busyEmpty := emptyBoardOf(t, busy)
	perBusy := timeBoard(t, busy, busyEmpty, times)

	t.Logf("пустая доска: в пустой организации — %v, рядом с доской на %d карточек — %v",
		perQuiet, largeBoard, perBusy)

	if perBusy > 2*perQuiet {
		t.Errorf("соседство с набитой доской замедлило пустую в %.1f×",
			float64(perBusy)/float64(perQuiet))
	}
}

// emptyBoardOf заводит в той же организации отдельную пустую доску.
func emptyBoardOf(t *testing.T, f *fixture) string {
	t.Helper()
	b, err := f.svc.Create(f.ctx, f.orgID, f.actorID, "Соседняя", "")
	if err != nil {
		t.Fatal(err)
	}
	return b.ID
}

func timeBoard(t *testing.T, f *fixture, boardID string, times int) time.Duration {
	t.Helper()
	if _, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < times; i++ {
		if _, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID); err != nil {
			t.Fatal(err)
		}
	}
	return time.Since(start) / time.Duration(times)
}
