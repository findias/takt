package board

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Закрытую доску нельзя отнять у самого себя.
//
// Закрытая доска открывается только поимённо, и вписывает в неё один
// владелец организации (4.1). Значит, выписавший себя теряет её без
// возврата — а если это владелец, доску теряет вся организация: после
// него на закрытой доске не остаётся никого, кто может её открыть,
// переназначить или убрать.
//
// До 22 августа 2026 так и было, и прогон это показал прямо: владелец
// выписал себя, перестал видеть доску и не смог открыть её обратно —
// «доска не найдена» на собственную доску собственной организации.
//
// Правило то же, что у смены видимости: собственный доступ не теряют
// по неосторожности. Там его держит политика — новый ряд обязан
// остаться видимым автору; здесь политика не поможет, удалить свою
// строку она разрешает, а расплата приходит следующим запросом.
func TestClosedBoardCannotBeTakenFromYourself(t *testing.T) {
	f := newFixture(t)
	inside := addMember(t, f.svc.db, f.orgID, "member")

	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityPrivate, nil); err != nil {
		t.Fatalf("закрытие доски: %v", err)
	}
	if err := f.svc.AddMember(f.ctx, f.orgID, f.actorID, f.boardID, inside); err != nil {
		t.Fatalf("вписывание: %v", err)
	}

	if err := f.svc.RemoveMember(f.ctx, f.orgID, f.actorID, f.boardID, f.actorID); !errors.Is(err, ErrLastWayIn) {
		t.Errorf("владелец выписал себя: %v", err)
	}
	if err := f.svc.RemoveMember(f.ctx, f.orgID, inside, f.boardID, inside); !errors.Is(err, ErrLastWayIn) {
		t.Errorf("вписанный выписал себя: %v", err)
	}

	// Доска цела и остаётся управляемой: это и есть то, ради чего отказ.
	if !f.sees(f.actorID) {
		t.Fatal("владелец потерял доску")
	}
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityOrg, nil); err != nil {
		t.Errorf("владелец не смог открыть доску обратно: %v", err)
	}

	// На открытой доске строка состава ничего не решает, и запрещать
	// там нечего: доступ даёт видимость, а не список.
	if err := f.svc.RemoveMember(f.ctx, f.orgID, f.actorID, f.boardID, f.actorID); err != nil {
		t.Errorf("выписаться из открытой доски не дали: %v", err)
	}
}

// Отказ по составу закрытой доски называет состав, а не команду и не
// пропавшую доску.
//
// Оба текста раньше приходили из чужих случаев: попытка вписать
// отвечала «доска станет вам не видна: выберите команду, в которой
// состоите» — общим разбором отказа политики, — а попытка выписать
// чужого отвечала «доска не найдена», хотя доску человек видит. Отказ,
// говорящий не о том, отправляет чинить не то.
func TestClosedBoardRosterRefusalNamesTheRoster(t *testing.T) {
	f := newFixture(t)
	inside := addMember(t, f.svc.db, f.orgID, "member")
	other := addMember(t, f.svc.db, f.orgID, "member")

	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityPrivate, nil); err != nil {
		t.Fatalf("закрытие доски: %v", err)
	}
	for _, u := range []string{inside, other} {
		if err := f.svc.AddMember(f.ctx, f.orgID, f.actorID, f.boardID, u); err != nil {
			t.Fatalf("вписывание: %v", err)
		}
	}

	third := addMember(t, f.svc.db, f.orgID, "member")
	if err := f.svc.AddMember(f.ctx, f.orgID, inside, f.boardID, third); !errors.Is(err, ErrRosterNotYours) {
		t.Errorf("вписывание не владельцем: %v, ожидалось «состав не ваш»", err)
	}
	if err := f.svc.RemoveMember(f.ctx, f.orgID, inside, f.boardID, other); !errors.Is(err, ErrRosterNotYours) {
		t.Errorf("выписывание не владельцем: %v, ожидалось «состав не ваш»", err)
	}

	// Состав закрытой доски вписанному виден из одного себя — так решено
	// в миграции 0006 и иначе не выражается: условие «строка доски,
	// на которой я состою» упирается в рекурсию политики. Но неполноту
	// теперь называют вслух: без этого признака экран показывал список
	// из одного человека как весь состав, и человек на доске втроём
	// видел, что он один.
	acc, err := f.svc.Access(f.ctx, f.orgID, inside, f.boardID)
	if err != nil {
		t.Fatalf("состав глазами вписанного: %v", err)
	}
	if len(acc.Members) != 1 || acc.Members[0].UserID != inside {
		t.Errorf("вписанный видит состав из %d человек", len(acc.Members))
	}
	if acc.RosterComplete {
		t.Error("неполный состав назван полным")
	}
	own, err := f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil || len(own.Members) != 3 {
		t.Errorf("владелец видит состав из %d человек, ожидалось 3", len(own.Members))
	}
	if !own.RosterComplete {
		t.Error("владельцу полный состав назван неполным")
	}

	// А вот когда убирать и правда нечего — «не найдено» правда.
	if err := f.svc.RemoveMember(f.ctx, f.orgID, f.actorID, f.boardID, third); !errors.Is(err, ErrNotFound) {
		t.Errorf("выписывание того, кого в составе нет: %v", err)
	}

	// Закрытая доска не видна никому, кроме вписанных: ни постороннему,
	// ни наблюдателю — это обещано на самом экране наблюдения.
	watcher := addMember(t, f.svc.db, f.orgID, "viewer")
	f.observes(watcher, nil)
	if f.sees(watcher) {
		t.Error("наблюдатель видит закрытую доску")
	}
	if f.sees(third) {
		t.Error("посторонний видит закрытую доску")
	}
	f.inTenant(func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(f.ctx,
			`select count(*) from board_members where board_id = $1`, f.boardID).Scan(&n); err != nil {
			return err
		}
		if n != 3 {
			t.Errorf("в составе %d человек, ожидалось 3", n)
		}
		return nil
	})
}
