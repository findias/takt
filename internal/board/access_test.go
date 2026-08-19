package board

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Доступ к доске: команда, видимость, поимённый состав.

func TestBoardStartsOpenToTheWholeOrganisation(t *testing.T) {
	f := newFixture(t)
	access, err := f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if access.Visibility != VisibilityOrg || access.TeamID != nil {
		t.Errorf("новая доска: %+v, ожидалась открытая всей организации", access)
	}
	if len(access.Members) != 0 {
		t.Errorf("в новой доске уже кто-то вписан: %+v", access.Members)
	}
}

func TestBoardIsHandedToTeamAndBack(t *testing.T) {
	f := newFixture(t)
	dev := f.team("Разработка", nil)
	outsider := addMember(t, f.svc.db, f.orgID, "member")

	// Владелец не состоит в команде, но видит все командные доски
	// по должности — значит перевод ему доступен.
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityTeam, &dev); err != nil {
		t.Fatalf("перевод доски команде: %v", err)
	}
	access, err := f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if access.Visibility != VisibilityTeam || access.TeamName == nil || *access.TeamName != "Разработка" {
		t.Fatalf("доска не стала командной: %+v", access)
	}
	if f.sees(outsider) {
		t.Error("командная доска осталась видна постороннему")
	}

	// Регрессия: список досок, доступных на запись, считался только по
	// членству, и владелец, отдавший доску команде без своего участия,
	// терял право её вернуть. Починено миграцией 0011 — владелец
	// управляет всем, что видит, кроме закрытых досок.
	//
	// Обратно в общую — а подразделение остаётся: «кому видно» и «чья
	// доска» разные вопросы, и дерево структуры отвечает на второй.
	// Прежде отметка здесь снималась, и довод был записан — без
	// командной видимости она ни на что не влияет; работа у неё
	// появилась вместе с досками узла в структуре.
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityOrg, nil); err != nil {
		t.Fatalf("возврат доски организации: %v", err)
	}
	access, err = f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if access.TeamID == nil || *access.TeamID != dev {
		t.Errorf("подразделение потерялось при возврате доски организации: %+v", access)
	}
	if !f.sees(outsider) {
		t.Error("общая доска не вернулась постороннему")
	}

	// Снять отметку можно, и это отдельная просьба: пустая команда
	// значит «убрать», а не присланная — «не трогать».
	none := ""
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityOrg, &none); err != nil {
		t.Fatalf("снятие подразделения: %v", err)
	}
	if access, _ = f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID); access.TeamID != nil {
		t.Errorf("подразделение осталось после снятия: %+v", access)
	}
}

// Командная доска без команды не бывает: проверка стоит и в коде, и в базе.
func TestTeamBoardRequiresTeam(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityTeam, nil); !errors.Is(err, ErrTeamRequired) {
		t.Errorf("командная доска без команды: %v", err)
	}
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, "секретная", nil); !errors.Is(err, ErrBadRequest) {
		t.Errorf("неизвестная видимость: %v", err)
	}
}

// Главное правило видимости, и единственное, которое приходится объяснять
// прикладным кодом: база отказывает в нём голым нарушением политики.
// Закрыть доску можно только вокруг себя — это следует из политик,
// и раньше следовало отказом: «впишите себя в состав». Порядок,
// известный только из отказа, — не порядок, а загадка, поэтому закрытие
// вписывает закрывающего само, в той же транзакции.
func TestClosingABoardInscribesTheOneWhoClosesIt(t *testing.T) {
	f := newFixture(t)
	named := addMember(t, f.svc.db, f.orgID, "member")

	if err := f.svc.AddMember(f.ctx, f.orgID, f.actorID, f.boardID, named); err != nil {
		t.Fatalf("добавление в доску: %v", err)
	}

	// Одно действие, без предварительного «впиши себя».
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityPrivate, nil); err != nil {
		t.Fatalf("закрытие доски: %v", err)
	}
	if !f.sees(named) || !f.sees(f.actorID) {
		t.Error("закрытая доска не видна тем, кто в неё вписан")
	}

	access, err := f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if access.Visibility != VisibilityPrivate {
		t.Errorf("видимость доски %q, ожидалась закрытая", access.Visibility)
	}
	inside := map[string]bool{}
	for _, m := range access.Members {
		inside[m.UserID] = true
	}
	if !inside[f.actorID] {
		t.Error("закрывающий не вписан в состав — доска стала бы неисправимой")
	}
	if !inside[named] {
		t.Error("прежний состав потерялся при закрытии")
	}

	// Повторное закрытие ничего не удваивает и не отказывает.
	if err := f.svc.SetAccess(f.ctx, f.orgID, f.actorID, f.boardID, VisibilityPrivate, nil); err != nil {
		t.Fatalf("повторное закрытие: %v", err)
	}
	again, err := f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Members) != len(access.Members) {
		t.Errorf("состав после повторного закрытия: %+v", again.Members)
	}
}

// Состав закрытой доски раздаёт только владелец организации (4.1),
// а закрытие вписывает закрывающего — значит, участнику закрыть доску
// нечем. Отказ обязан называть того, кто может, а не предлагать
// участнику вписать себя: этого он как раз не умеет.
func TestClosingABoardByAMemberSaysWhoCan(t *testing.T) {
	f := newFixture(t)
	member := addMember(t, f.svc.db, f.orgID, "member")

	err := f.svc.SetAccess(f.ctx, f.orgID, member, f.boardID, VisibilityPrivate, nil)
	if !errors.Is(err, ErrCloseNeedsRoster) {
		t.Fatalf("закрытие доски участником: %v", err)
	}
	access, err := f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if access.Visibility == VisibilityPrivate {
		t.Error("отвергнутое закрытие всё-таки применилось")
	}
	if len(access.Members) != 0 {
		t.Errorf("отвергнутое закрытие оставило состав: %+v", access.Members)
	}
}

func TestBoardRosterIsManaged(t *testing.T) {
	f := newFixture(t)
	inside := addMember(t, f.svc.db, f.orgID, "member")

	if err := f.svc.AddMember(f.ctx, f.orgID, f.actorID, f.boardID, inside); err != nil {
		t.Fatal(err)
	}
	// Повтор не ломается: вписать дважды — то же самое, что вписать.
	if err := f.svc.AddMember(f.ctx, f.orgID, f.actorID, f.boardID, inside); err != nil {
		t.Fatalf("повторное добавление: %v", err)
	}
	access, err := f.svc.Access(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	if len(access.Members) != 1 || access.Members[0].UserID != inside {
		t.Fatalf("состав доски %+v", access.Members)
	}

	// Постороннего в доску не вписать: доска — структура внутри
	// арендатора, а не способ пригласить снаружи.
	var stranger string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			insert into users (email, name, password_hash)
			values ('чужой-' || gen_random_uuid() || '@example.test', 'Ч', 'x')
			returning id`).Scan(&stranger)
	})
	if err := f.svc.AddMember(f.ctx, f.orgID, f.actorID, f.boardID, stranger); !errors.Is(err, ErrNotFound) {
		t.Errorf("посторонний вписан в доску: %v", err)
	}

	if err := f.svc.RemoveMember(f.ctx, f.orgID, f.actorID, f.boardID, inside); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveMember(f.ctx, f.orgID, f.actorID, f.boardID, inside); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное исключение: ожидалось ErrNotFound, получено %v", err)
	}
}

// Чужая доска неотличима от несуществующей — и при чтении доступа,
// и при попытке его изменить.
func TestAccessToForeignBoardIsNotFound(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)

	if _, err := f.svc.Access(f.ctx, other.orgID, other.actorID, f.boardID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чтение доступа к чужой доске: %v", err)
	}
	if err := f.svc.SetAccess(f.ctx, other.orgID, other.actorID, f.boardID, VisibilityOrg, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("смена видимости чужой доски: %v", err)
	}
	if err := f.svc.AddMember(f.ctx, other.orgID, other.actorID, f.boardID, other.actorID); !errors.Is(err, ErrNotFound) {
		t.Errorf("добавление в чужую доску: %v", err)
	}
}

// Убранная доска не удаляется: карточки и журнал остаются, по ним
// считается поток. Значит, у архивации обязано быть обратное действие —
// иначе это просто удаление с лишним шагом.
func TestBoardIsArchivedAndBroughtBack(t *testing.T) {
	f := newFixture(t)
	f.createCard("Задача", f.columnA)

	if err := f.svc.Archive(f.ctx, f.orgID, f.actorID, f.boardID); err != nil {
		t.Fatalf("архивация: %v", err)
	}

	list, err := f.svc.List(f.ctx, f.orgID, f.actorID)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range list {
		if b.ID == f.boardID {
			t.Error("архивная доска осталась в обычном списке")
		}
	}
	// Архивная доска называется архивной, а не «не найденной»: это
	// разные положения дел и разные следующие шаги — см.
	// TestArchivedBoardIsNamedArchived.
	if _, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, f.boardID); !errors.Is(err, ErrArchivedBoard) {
		t.Errorf("снимок архивной доски: %v", err)
	}

	// Операции по архивной доске не проходят — это единственное место,
	// где такая защита теперь и живёт.
	if _, err := f.apply("CREATE_CARD", map[string]any{
		"columnId": f.columnA, "title": "Поздно"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("операция по архивной доске: %v", err)
	}

	archived, err := f.svc.Archived(f.ctx, f.orgID, f.actorID)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].ID != f.boardID {
		t.Fatalf("архив: %+v", archived)
	}
	// Строка архива говорит о доске то же, что и строка списка: из архива
	// выбирают, какую вернуть и какую стереть насовсем, а по одному
	// названию этот выбор делается вслепую.
	if got := archived[0]; got.Key == "" || got.Visibility == nil || got.Cards == nil {
		t.Errorf("архив без ключа, видимости или объёма: %+v", got)
	}

	if err := f.svc.Restore(f.ctx, f.orgID, f.actorID, f.boardID); err != nil {
		t.Fatalf("возврат из архива: %v", err)
	}
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, f.boardID)
	if err != nil {
		t.Fatalf("снимок возвращённой доски: %v", err)
	}
	if len(snap.Cards) != 1 {
		t.Errorf("карточки не пережили архивацию: %+v", snap.Cards)
	}

	// Повтор ничего не меняет и говорит об этом.
	if err := f.svc.Restore(f.ctx, f.orgID, f.actorID, f.boardID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторный возврат: %v", err)
	}
}

func TestArchivingForeignBoardIsNotFound(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	if err := f.svc.Archive(f.ctx, other.orgID, other.actorID, f.boardID); !errors.Is(err, ErrNotFound) {
		t.Errorf("архивация чужой доски: %v", err)
	}
	if archived, _ := f.svc.Archived(f.ctx, other.orgID, other.actorID); len(archived) != 0 {
		t.Errorf("в чужом архиве видно %d досок", len(archived))
	}
}
