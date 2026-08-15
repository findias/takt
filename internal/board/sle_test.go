package board

import (
	"errors"
	"testing"
)

// Обещание доски.
//
// Проверяется не «поле сохранилось», а то, ради чего поле заведено:
// обещание неподвижно между пересмотрами и потому годится как мерило.
// Всё остальное — границы, за которыми обещание перестаёт быть прогнозом.

func TestBoardHasNoPromiseUntilItIsGiven(t *testing.T) {
	f := newFixture(t)

	// Доска без истории обещать не может, и подставлять ей выдуманный
	// срок значило бы начинать с неправды.
	if got := f.snapshot().Board.SLEDays; got != nil {
		t.Errorf("новая доска уже что-то обещает: %v дн.", *got)
	}
	// Вероятность при этом названа: она нужна, как только появится срок,
	// и та же, о которой говорит само руководство в своём примере.
	if got := f.snapshot().Board.SLEProbability; got != 85 {
		t.Errorf("вероятность по умолчанию %d, ожидалось 85", got)
	}
}

func TestPromiseIsKeptAndCanBeWithdrawn(t *testing.T) {
	f := newFixture(t)
	days := 8

	if err := f.svc.SetSLE(f.ctx, f.orgID, f.actorID, f.boardID, &days, 90); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot()
	if snap.Board.SLEDays == nil || *snap.Board.SLEDays != 8 {
		t.Fatalf("срок обещания: %v", snap.Board.SLEDays)
	}
	if snap.Board.SLEProbability != 90 {
		t.Errorf("вероятность: %d", snap.Board.SLEProbability)
	}

	// Снять обещание можно: команда, потерявшая опору для прогноза,
	// не обязана продолжать обещать.
	if err := f.svc.SetSLE(f.ctx, f.orgID, f.actorID, f.boardID, nil, 85); err != nil {
		t.Fatal(err)
	}
	if got := f.snapshot().Board.SLEDays; got != nil {
		t.Errorf("обещание не снялось: %v", *got)
	}
}

// Границы: прогноз с вероятностью в десять процентов — не прогноз,
// а гадание, и срок в ноль дней не бывает.
func TestPromiseRefusesNonsense(t *testing.T) {
	f := newFixture(t)
	zero := 0
	valid := 5

	for _, c := range []struct {
		name        string
		days        *int
		probability int
	}{
		{"ноль дней", &zero, 85},
		{"вероятность десять процентов", &valid, 10},
		{"вероятность сто процентов", &valid, 100},
	} {
		err := f.svc.SetSLE(f.ctx, f.orgID, f.actorID, f.boardID, c.days, c.probability)
		if !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: принято, ошибка %v", c.name, err)
		}
	}
}

// Обещание чужой доски недоступно так же, как сама доска.
func TestPromiseOfForeignBoardIsNotFound(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	days := 7

	err := f.svc.SetSLE(f.ctx, other.orgID, other.actorID, f.boardID, &days, 85)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("обещание чужой доски: %v, ожидалось «не найдена»", err)
	}
	if got := f.snapshot().Board.SLEDays; got != nil {
		t.Errorf("чужая рука поставила обещание: %v", *got)
	}
}
