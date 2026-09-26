package board

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Владелец подразделения удаляет насовсем доски и карточки своего
// поддерева (этап 31, app_runs_board, 0074) — и только их.
func TestSubdivisionOwnerPurgesOnlyItsOwnBoards(t *testing.T) {
	f := newFixture(t)
	dev := f.team("Разработка", nil)
	sales := f.team("Продажи", nil)
	head := addMember(t, f.svc.db, f.orgID, "member")
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx,
			`insert into team_admins (org_id, user_id, team_id) values ($1, $2, $3)`,
			f.orgID, head, dev)
		return err
	})

	mine := f.boardOfTeam("Своя", &dev)
	theirs := f.boardOfTeam("Соседская", &sales)

	firstColumn := func(boardID string) string {
		snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID)
		if err != nil {
			t.Fatal(err)
		}
		return snap.Columns[0].ID
	}
	deleteCard := func(boardID, cardID string) error {
		_, err := f.svc.Apply(f.ctx, f.orgID, head, boardID, Request{
			OperationID: uuid.NewString(),
			Type:        "DELETE_CARD",
			Payload:     mustJSON(t, map[string]any{"cardId": cardID}),
		})
		return err
	}
	canPurge := func(boardID string) bool {
		snap, err := f.svc.Snapshot(f.ctx, f.orgID, head, boardID)
		if err != nil {
			t.Fatal(err)
		}
		return snap.Board.CanPurge != nil && *snap.Board.CanPurge
	}

	if !canPurge(mine) || canPurge(theirs) {
		t.Errorf("снимок врёт о праве: своя %v, соседская %v", canPurge(mine), canPurge(theirs))
	}

	own := f.cardOn(mine, firstColumn(mine), "Своя карточка")
	other := f.cardOn(theirs, firstColumn(theirs), "Чужая карточка")
	if err := deleteCard(mine, own); err != nil {
		t.Errorf("владелец подразделения не удалил карточку своей доски: %v", err)
	}
	if err := deleteCard(theirs, other); !errors.Is(err, ErrPurgeNotYours) {
		t.Errorf("карточка соседа: ждали ErrPurgeNotYours, получили %v", err)
	}
	if n := f.countRows(`select count(*) from cards where id = $1`, other); n != 1 {
		t.Error("чужая карточка удалилась вопреки отказу")
	}

	for _, id := range []string{mine, theirs} {
		if err := f.svc.Archive(f.ctx, f.orgID, f.actorID, id); err != nil {
			t.Fatal(err)
		}
	}
	archived, err := f.svc.Archived(f.ctx, f.orgID, head)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range archived {
		want := b.ID == mine
		if got := b.CanPurge != nil && *b.CanPurge; got != want {
			t.Errorf("архив: у «%s» canPurge %v, ждали %v", b.Name, got, want)
		}
	}

	if err := f.svc.Delete(f.ctx, f.orgID, head, theirs, "Соседская"); !errors.Is(err, ErrPurgeNotYours) {
		t.Errorf("доска соседа: ждали ErrPurgeNotYours, получили %v", err)
	}
	if err := f.svc.Delete(f.ctx, f.orgID, head, mine, "Своя"); err != nil {
		t.Errorf("владелец подразделения не удалил свою доску из архива: %v", err)
	}
}
