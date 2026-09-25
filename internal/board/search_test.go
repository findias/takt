package board

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// Поиск по организации — чтобы связать задачу команды с эпиком
// портфеля: прежде выбрать было можно только карточку своей доски.

func TestSearchFindsCardsOnOtherBoards(t *testing.T) {
	f := newFixture(t)
	other := f.boardOfTeam("Портфель", nil)
	var otherCol string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select id from board_columns where board_id = $1 order by position limit 1`,
			other).Scan(&otherCol)
	})
	epic := f.applyTo(other, "CREATE_CARD", map[string]any{"columnId": otherCol, "title": "Переезд на новый склад"}).Patch.Cards[0]
	f.createCard("Склад по умолчанию", f.columnA)

	found, err := f.svc.SearchCards(f.ctx, f.orgID, f.actorID, "склад")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("найдено %d, ожидались обе карточки со «склад»: %+v", len(found), found)
	}
	var hit *FoundCard
	for i := range found {
		if found[i].ID == epic.ID {
			hit = &found[i]
		}
	}
	if hit == nil || hit.BoardName != "Портфель" || hit.BoardID != other || hit.Number == "" {
		t.Fatalf("карточка соседней доски не найдена или без доски: %+v", found)
	}

	byNumber, err := f.svc.SearchCards(f.ctx, f.orgID, f.actorID, epic.Number)
	if err != nil {
		t.Fatal(err)
	}
	if len(byNumber) == 0 || byNumber[0].ID != epic.ID {
		t.Errorf("номер целиком не первым: %+v", byNumber)
	}

	if none, err := f.svc.SearchCards(f.ctx, f.orgID, f.actorID, "   "); err != nil || len(none) != 0 {
		t.Errorf("пустой запрос вернул %+v, %v", none, err)
	}
}

// Закрытая доска постороннему в поиск не попадает, архивная карточка —
// никому: связывать с ней нечего.
func TestSearchKeepsClosedBoardsAndArchiveOut(t *testing.T) {
	f := newFixture(t)
	secret := f.boardOfTeam("Закрытая", nil)
	f.inTenant(func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `insert into board_members (org_id, board_id, user_id) values ($1, $2, $3)`,
			f.orgID, secret, f.actorID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `update boards set visibility = 'private' where id = $1`, secret)
		return err
	})
	var secretCol string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select id from board_columns where board_id = $1 order by position limit 1`,
			secret).Scan(&secretCol)
	})
	f.applyTo(secret, "CREATE_CARD", map[string]any{"columnId": secretCol, "title": "Тайный эпик"})
	gone := f.createCard("Убранный эпик", f.columnA)
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `update cards set archived_at = now() where id = $1`, gone)
		return err
	})

	outsider := f.inviteMember("Посторонний")
	found, err := f.svc.SearchCards(f.ctx, f.orgID, outsider, "эпик")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("посторонний нашёл %+v — закрытое или архивное", found)
	}
	mine, err := f.svc.SearchCards(f.ctx, f.orgID, f.actorID, "эпик")
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].Title != "Тайный эпик" {
		t.Errorf("участник закрытой доски нашёл %+v, ожидался один «Тайный эпик»", mine)
	}
}
