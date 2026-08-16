package board

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Удаление насовсем (этап 11.5).
//
// Главное свойство, которое здесь проверяется: после удаления нельзя
// узнать, как шла работа, но всегда можно узнать, кто её убрал и что
// именно. Первое — журнал потока, он стирается; второе — журнал
// действий, он не стирается никогда.

func (f *fixture) countRows(query string, args ...any) int {
	f.t.Helper()
	var n int
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, query, args...).Scan(&n)
	})
	return n
}

func TestDeleteCardErasesFlowAndKeepsTheLedger(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	doomed := f.createCard("Дубль сметы", cols[0].ID)
	kept := f.createCard("Настоящая смета", cols[0].ID)

	// У карточки набирается история, метка и подзадача — всё, что
	// каскадом должно уехать вместе с ней.
	f.mustApply("MOVE_CARD", map[string]any{
		"cardId": doomed, "toColumnId": cols[1].ID, "place": "end"})
	f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": doomed, "title": "Кусок дубля"})

	before := f.countRows(`select count(*) from card_events where card_id = $1`, doomed)
	if before == 0 {
		t.Fatal("история карточки не набралась — проверять нечего")
	}

	f.mustApply("DELETE_CARD", map[string]any{"cardId": doomed})

	// Карточки нет, её потока нет, её связей нет.
	if n := f.countRows(`select count(*) from cards where id = $1`, doomed); n != 0 {
		t.Errorf("карточка осталась в базе")
	}
	if n := f.countRows(`select count(*) from card_events where card_id = $1`, doomed); n != 0 {
		t.Errorf("событий потока осталось %d, ожидался ноль", n)
	}
	if n := f.countRows(`select count(*) from card_links where from_card = $1`, doomed); n != 0 {
		t.Errorf("связей осталось %d, ожидался ноль", n)
	}

	// Соседняя карточка не пострадала: удаление точечное.
	if n := f.countRows(`select count(*) from card_events where card_id = $1`, kept); n == 0 {
		t.Error("вместе с чужой историей стёрлась своя")
	}

	// А в журнале действий осталось, кто убрал и что именно.
	var title string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select payload -> 'new' ->> 'title' from audit_events
			 where subject = 'cards' and subject_id = $1 and action = 'delete'`,
			doomed).Scan(&title)
	})
	if title != "Дубль сметы" {
		t.Errorf("в журнале действий записано %q, ожидалось название удалённой карточки", title)
	}
}

// Подзадача удалённой карточки не удаляется вместе с ней: это отдельная
// работа, у неё может быть свой исполнитель и своя доска.
func TestDeleteCardLeavesItsSubtaskAlone(t *testing.T) {
	f := newFixture(t)
	parent := f.createCard("Выпустить релиз", f.columns()[0].ID)
	f.mustApply("CREATE_SUBTASK", map[string]any{
		"parentCardId": parent, "title": "Прогнать тесты"})

	var child string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`select to_card from card_links where from_card = $1`, parent).Scan(&child)
	})

	f.mustApply("DELETE_CARD", map[string]any{"cardId": parent})

	if n := f.countRows(`select count(*) from cards where id = $1`, child); n != 1 {
		t.Error("подзадача уехала вместе с родителем")
	}
}

func TestDeleteCardIsOwnerOnly(t *testing.T) {
	f := newFixture(t)
	id := f.createCard("Не трогать", f.columns()[0].ID)
	member := addMember(t, f.svc.db, f.orgID, "member")

	_, err := f.svc.Apply(f.ctx, f.orgID, member, f.boardID, Request{
		OperationID: uuid.NewString(),
		Type:        "DELETE_CARD",
		Payload:     mustJSON(t, map[string]any{"cardId": id}),
	})
	if !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("ожидался отказ по праву, получено %v", err)
	}
	if n := f.countRows(`select count(*) from cards where id = $1`, id); n != 1 {
		t.Error("карточка удалилась вопреки отказу")
	}
}

// --- доска ---

func TestDeleteBoardRequiresArchiveOwnerAndName(t *testing.T) {
	f := newFixture(t)
	f.createCard("Работа", f.columns()[0].ID)

	var name string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select name from boards where id = $1`, f.boardID).Scan(&name)
	})

	// Живую доску не удаляют: сперва архив, и он обратим.
	if err := f.svc.Delete(f.ctx, f.orgID, f.actorID, f.boardID, name); !errors.Is(err, ErrBoardNotArchived) {
		t.Fatalf("живая доска удалилась или ответила не тем: %v", err)
	}
	if err := f.svc.Archive(f.ctx, f.orgID, f.actorID, f.boardID); err != nil {
		t.Fatal(err)
	}

	// Не тот человек.
	member := addMember(t, f.svc.db, f.orgID, "member")
	if err := f.svc.Delete(f.ctx, f.orgID, member, f.boardID, name); !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("удалить смог не владелец: %v", err)
	}

	// Не то название: подтверждение проверяется на сервере, а не только
	// в диалоге.
	if err := f.svc.Delete(f.ctx, f.orgID, f.actorID, f.boardID, "не та доска"); !errors.Is(err, ErrNameMismatch) {
		t.Fatalf("подтверждение не проверено: %v", err)
	}
	if n := f.countRows(`select count(*) from boards where id = $1`, f.boardID); n != 1 {
		t.Fatal("доска исчезла после неудачных попыток")
	}
}

func TestDeleteBoardErasesFlowAndKeepsTheLedger(t *testing.T) {
	f := newFixture(t)
	cols := f.columns()
	id := f.createCard("Работа", cols[0].ID)
	f.mustApply("MOVE_CARD", map[string]any{
		"cardId": id, "toColumnId": cols[1].ID, "place": "end"})

	var name string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select name from boards where id = $1`, f.boardID).Scan(&name)
	})
	if err := f.svc.Archive(f.ctx, f.orgID, f.actorID, f.boardID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(f.ctx, f.orgID, f.actorID, f.boardID, name); err != nil {
		t.Fatalf("удаление доски: %v", err)
	}

	for _, q := range []string{
		`select count(*) from boards where id = $1`,
		`select count(*) from cards where board_id = $1`,
		`select count(*) from board_columns where board_id = $1`,
		`select count(*) from card_events where board_id = $1`,
	} {
		if n := f.countRows(q, f.boardID); n != 0 {
			t.Errorf("после удаления осталось %d строк: %s", n, q)
		}
	}

	// Журнал действий остался и знает, какую доску убрали.
	var subject, deletedName string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `
			select subject, payload -> 'new' ->> 'name' from audit_events
			 where subject_id = $1 and action = 'delete'`, f.boardID).Scan(&subject, &deletedName)
	})
	if subject != "boards" || deletedName != name {
		t.Errorf("в журнале %q/%q, ожидалась запись об удалении доски %q", subject, deletedName, name)
	}

	// А карточки доски отдельными записями ленту не топят: их унёс
	// каскад, и доска записана целиком.
	if n := f.countRows(`
		select count(*) from audit_events
		 where subject = 'cards' and action = 'delete'
		   and payload -> 'new' ->> 'board_id' = $1::text`, f.boardID); n != 0 {
		t.Errorf("карточки доски получили %d отдельных записей в журнале", n)
	}
}
