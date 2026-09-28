package board

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Исполнитель — только тот, кто видит доску (0076).
//
// Назначить можно было любого участника организации, и работа числилась
// за человеком, который её карточки не видит и известия о ней не
// получает. Проверяется снимок (кого предлагать) и сама операция (кого
// принять) — одним правилом, политикой видимости доски.

// assignAs назначает от имени названного человека: права спрашивающего
// участвуют в ответе, и владелец организации фикстуры их бы заслонил.
func (f *fixture) assignAs(actorID, cardID, userID string) error {
	f.t.Helper()
	raw, _ := json.Marshal(map[string]any{"cardId": cardID, "userId": userID})
	_, err := f.svc.Apply(f.ctx, f.orgID, actorID, f.boardID,
		Request{OperationID: uuid.NewString(), Type: "ASSIGN_CARD", Payload: raw})
	return err
}

// assignableAs — кого снимок предлагает назначить, глазами названного.
func (f *fixture) assignableAs(viewerID string) map[string]bool {
	f.t.Helper()
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, viewerID, f.boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]bool{}
	for _, p := range snap.People {
		out[p.UserID] = p.Assignable
	}
	return out
}

func refusedForAccess(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("назначили человека, который доску не видит")
	}
	if !errors.Is(err, ErrBadRequest) || !strings.Contains(err.Error(), "не видит доску") {
		t.Fatalf("отказ не объясняет, в чём дело: %v", err)
	}
}

func TestAssigneeOfTeamBoardComesFromItsTeam(t *testing.T) {
	f := newFixture(t)
	core := f.team("Ядро", nil)
	inner := f.team("Ядро: сервер", &core)
	other := f.team("Поставки", nil)
	f.assignBoard(core)

	ours := addMember(t, f.svc.db, f.orgID, "member")
	f.joins(ours, core)
	below := addMember(t, f.svc.db, f.orgID, "member")
	f.joins(below, inner)
	stranger := addMember(t, f.svc.db, f.orgID, "member")
	f.joins(stranger, other)
	nowhere := addMember(t, f.svc.db, f.orgID, "member")

	card := f.createCard("Кому делать", f.columnA)

	// Доска узла видна его ветке сверху вниз: состоящий в «Ядре» её
	// видит, состоящий только ниже — нет (app_member_teams).
	offered := f.assignableAs(ours)
	for who, want := range map[string]bool{ours: true, stranger: false, nowhere: false} {
		if offered[who] != want {
			t.Errorf("снимок: %s предложен=%v, ждали %v", who, offered[who], want)
		}
	}
	if err := f.assignAs(ours, card, ours); err != nil {
		t.Fatalf("своего подразделения не назначить: %v", err)
	}
	refusedForAccess(t, f.assignAs(ours, card, stranger))
	refusedForAccess(t, f.assignAs(ours, card, nowhere))
	if offered[below] != f.sees(below) {
		t.Errorf("снимок и видимость расходятся для нижнего узла: предложен=%v, видит=%v",
			offered[below], f.sees(below))
	}

	// Список людей не сужается: по нему подписаны прежние исполнители.
	if _, ok := offered[stranger]; !ok {
		t.Error("человек без доступа пропал из списка людей — прежний исполнитель остался бы без имени")
	}
}

func TestAssigneeOfPrivateBoardIsOneOfItsMembers(t *testing.T) {
	f := newFixture(t)
	named := addMember(t, f.svc.db, f.orgID, "member")
	outside := addMember(t, f.svc.db, f.orgID, "member")
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			insert into board_members (org_id, board_id, user_id)
			values ($1, $2, $3), ($1, $2, $4)`,
			f.orgID, f.boardID, named, f.actorID)
		return err
	})
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `update boards set visibility = 'private' where id = $1`, f.boardID)
		return err
	})
	card := f.createCard("Закрытая работа", f.columnA)

	// Спрашивает рядовой участник: состав закрытой доски ему под ролью
	// приложения не виден (0006), и ответ обязан прийти от политики.
	offered := f.assignableAs(named)
	if !offered[f.actorID] || !offered[named] || offered[outside] {
		t.Errorf("снимок закрытой доски предлагает не тех: %v (вписаны %s и %s, не вписан %s)",
			offered, f.actorID, named, outside)
	}
	if err := f.assignAs(named, card, f.actorID); err != nil {
		t.Fatalf("участника закрытой доски не назначить: %v", err)
	}
	refusedForAccess(t, f.assignAs(named, card, outside))
}

// Функция подставляет назначаемого на время одного запроса. Она не
// отвечает о доске, которой не видит сам спрашивающий, и возвращает его
// самого: после вопроса транзакция идёт от прежнего имени.
func TestSeesBoardAsAnswersOnlyAboutOwnBoardsAndRestoresTheActor(t *testing.T) {
	f := newFixture(t)
	other := addMember(t, f.svc.db, f.orgID, "member")
	// Закрыть доску можно только вписав себя (TestBoardCannotBeClosedAroundSomeoneElse),
	// поэтому владелец вписывается и выходит уже из закрытой.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `
			insert into board_members (org_id, board_id, user_id)
			values ($1, $2, $3), ($1, $2, $4)`,
			f.orgID, f.boardID, other, f.actorID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(f.ctx, `update boards set visibility = 'private' where id = $1`, f.boardID)
		return err
	})
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `delete from board_members where board_id = $1 and user_id = $2`,
			f.boardID, f.actorID)
		return err
	})

	f.inTenant(func(tx pgx.Tx) error {
		var sees bool
		var me string
		if err := tx.QueryRow(f.ctx,
			`select app_sees_board_as($1, $2), app_current_user()::text`,
			other, f.boardID).Scan(&sees, &me); err != nil {
			return err
		}
		if sees {
			t.Error("функция ответила о закрытой доске, которой спрашивающий не видит")
		}
		if me != f.actorID {
			t.Errorf("после вопроса текущий пользователь %s, а не %s", me, f.actorID)
		}
		return nil
	})
}
