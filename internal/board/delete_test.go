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
