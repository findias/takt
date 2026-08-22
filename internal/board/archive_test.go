package board

import (
	"errors"
	"testing"
)

// Архив доски: кто его видит и кому что отвечают.
//
// Отложено это было в 19.2 сознательно — там `explainMissingBoard`
// спрашивает про живую доску, и возврат из архива он объявил бы
// «не найдено» на доску, которую человек видит в списке убранных.
// Ответ нашёлся, когда прогон показал, кто вообще этот список видит:
// наблюдатель видит убранную доску ровно так же, как видел живую,
// и «доска не найдена» на попытку её вернуть отправляла его искать
// несуществующую поломку.
//
// Три ответа вместо двух, и различает их не роль, а `app_writable_boards`
// — та же функция, по которой решают политики. Догадка по роли разошлась
// бы с политикой при первой же правке любой из них.
func TestBoardArchiveAnswersEveryoneTruthfully(t *testing.T) {
	f := newFixture(t)
	company := f.team("Компания", nil)
	dev := f.team("Разработка", &company)
	f.assignBoard(dev)

	insider := addMember(t, f.svc.db, f.orgID, "member")
	f.joins(insider, dev)
	outsider := addMember(t, f.svc.db, f.orgID, "member")
	watcher := addMember(t, f.svc.db, f.orgID, "viewer")
	f.observes(watcher, &company)

	// Наблюдатель доску видит — значит, «не найдена» ему говорить нельзя.
	if !f.sees(watcher) {
		t.Fatal("наблюдатель не видит доску — проверять нечего")
	}
	if err := f.svc.Archive(f.ctx, f.orgID, watcher, f.boardID); !errors.Is(err, ErrReadOnlyBoard) {
		t.Errorf("архивация наблюдателем: %v, ожидалось «только для чтения»", err)
	}
	// Посторонний доски не видит — ему «не найдена» правда.
	if err := f.svc.Archive(f.ctx, f.orgID, outsider, f.boardID); !errors.Is(err, ErrNotFound) {
		t.Errorf("архивация посторонним: %v, ожидалось «не найдена»", err)
	}
	if err := f.svc.Archive(f.ctx, f.orgID, insider, f.boardID); err != nil {
		t.Fatalf("участник команды не убрал доску: %v", err)
	}

	// В архиве доску видят те же, кто видел её живой.
	for _, c := range []struct {
		who  string
		name string
		want int
	}{
		{f.actorID, "владелец", 1},
		{insider, "участник команды", 1},
		{watcher, "наблюдатель", 1},
		{outsider, "посторонний", 0},
	} {
		list, err := f.svc.Archived(f.ctx, f.orgID, c.who)
		if err != nil || len(list) != c.want {
			t.Errorf("в архиве у %s: %d досок (err=%v), ожидалось %d",
				c.name, len(list), err, c.want)
		}
	}

	// И возврат отвечает по тому же правилу — это и есть то, ради чего
	// разбор был отложен: на убранной доске прежний помощник ответил бы
	// «не найдена» даже тому, кто её видит.
	if err := f.svc.Restore(f.ctx, f.orgID, watcher, f.boardID); !errors.Is(err, ErrReadOnlyBoard) {
		t.Errorf("возврат наблюдателем: %v, ожидалось «только для чтения»", err)
	}
	if err := f.svc.Restore(f.ctx, f.orgID, outsider, f.boardID); !errors.Is(err, ErrNotFound) {
		t.Errorf("возврат посторонним: %v, ожидалось «не найдена»", err)
	}
	if err := f.svc.Restore(f.ctx, f.orgID, insider, f.boardID); err != nil {
		t.Errorf("участник команды не вернул доску: %v", err)
	}

	// Стирание насовсем — только владелец, и отказ называет того, кто может.
	if err := f.svc.Archive(f.ctx, f.orgID, f.actorID, f.boardID); err != nil {
		t.Fatalf("архивация владельцем: %v", err)
	}
	for _, who := range []string{watcher, insider} {
		if err := f.svc.Delete(f.ctx, f.orgID, who, f.boardID, "Доска"); err == nil {
			t.Error("доску стёрли насовсем не владельцем")
		}
	}
}
