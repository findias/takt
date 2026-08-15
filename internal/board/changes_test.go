package board

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// Догон изменений.
//
// Проверяется свойство, ради которого всё это заведено: клиент, отставший
// на несколько операций, получает ровно те патчи, которые пропустил, —
// и, применив их, приходит в то же состояние, что и полный снимок.
// Там, где догнать нельзя, ответ говорит об этом прямо, а не отдаёт
// неполный список.

func TestCatchupGivesExactlyTheMissedPatches(t *testing.T) {
	f := newFixture(t)
	before := f.snapshot().Board.Version

	first := f.createCard("Первая", f.columnA)
	f.createCard("Вторая", f.columnA)

	catchup, err := f.svc.Changes(f.ctx, f.orgID, f.actorID, f.boardID, before)
	if err != nil {
		t.Fatal(err)
	}
	if catchup.Full {
		t.Fatal("догнать двумя операциями не удалось")
	}
	if len(catchup.Results) != 2 {
		t.Fatalf("патчей %d, операций было 2", len(catchup.Results))
	}
	if catchup.Version != before+2 {
		t.Errorf("версия в ответе %d, ожидалась %d", catchup.Version, before+2)
	}

	// Патчи идут по порядку, несут то самое, что случилось, и каждый
	// назван своей версией — выводить её из порядка на той стороне
	// значило бы хранить одно и то же в двух местах.
	if len(catchup.Results[0].Patch.Cards) == 0 || catchup.Results[0].Patch.Cards[0].ID != first {
		t.Errorf("первый патч не про первую карточку: %+v", catchup.Results[0])
	}
	if catchup.Results[0].Version != before+1 || catchup.Results[1].Version != before+2 {
		t.Errorf("версии патчей %d и %d при исходной %d",
			catchup.Results[0].Version, catchup.Results[1].Version, before)
	}
}

// Тот, кто ничего не пропустил, не должен получать работы.
func TestCatchupOfCurrentVersionIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.createCard("Единственная", f.columnA)
	now := f.snapshot().Board.Version

	catchup, err := f.svc.Changes(f.ctx, f.orgID, f.actorID, f.boardID, now)
	if err != nil {
		t.Fatal(err)
	}
	if catchup.Full || len(catchup.Results) != 0 {
		t.Errorf("нечего догонять, а ответ: full=%v, патчей %d", catchup.Full, len(catchup.Results))
	}
	if catchup.Version != now {
		t.Errorf("версия %d вместо %d", catchup.Version, now)
	}
}

// Операции, применённые до появления самой возможности догонять, версии
// не имеют. Молча отдать неполный список нельзя: расхождение всплывёт
// позже и объяснить его будет нечем.
func TestCatchupSaysWhenItCannotCatchUp(t *testing.T) {
	f := newFixture(t)
	before := f.snapshot().Board.Version
	f.createCard("С пропуском", f.columnA)

	// Подделываем прошлое: у операции пропадает версия — ровно так
	// выглядят операции, сделанные до миграции.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `update operations set board_version = null where board_id = $1`, f.boardID)
		return err
	})

	catchup, err := f.svc.Changes(f.ctx, f.orgID, f.actorID, f.boardID, before)
	if err != nil {
		t.Fatal(err)
	}
	if !catchup.Full {
		t.Error("догон не сообщил, что догнать нечем")
	}
	if len(catchup.Results) != 0 {
		t.Errorf("вместе с отказом пришли патчи: %d", len(catchup.Results))
	}
}

func TestCatchupOfForeignBoardIsNotFound(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	f.createCard("Своя", f.columnA)

	if _, err := f.svc.Changes(f.ctx, other.orgID, other.actorID, f.boardID, 0); err == nil {
		t.Error("чужая доска отдала свои изменения")
	}
}
