package board

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// Метка эпика (этап 33.3): эпик карточки — ближайший предок на доске-
// портфеле, через доски и уровни; снимок и патч отвечают одно и то же.

func (f *fixture) portfolio() (boardID, epicID string) {
	f.t.Helper()
	p, err := f.svc.CreateFrom(f.ctx, f.orgID, f.actorID, "Портфель", "", TemplatePortfolio)
	if err != nil {
		f.t.Fatal(err)
	}
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, p.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	res := f.applyTo(p.ID, "CREATE_CARD", map[string]any{"columnId": snap.Columns[0].ID, "title": "Эпик"})
	return p.ID, res.Patch.Cards[0].ID
}

func TestEpicReachesTasksThroughBoards(t *testing.T) {
	f := newFixture(t)
	pf, epic := f.portfolio()
	// Фича — на доске команды, частью эпика с портфеля.
	res := f.applyTo(pf, "CREATE_SUBTASK", map[string]any{"parentCardId": epic, "title": "Фича", "boardId": f.boardID})
	_ = res
	var feature string
	for _, c := range f.snapshot().Cards {
		if c.Title == "Фича" {
			feature = c.ID
		}
	}
	task := f.sub(feature, "Задача")

	for _, id := range []string{feature, task} {
		e := f.card(id).Epic
		if e == nil || e.ID != epic || e.Title != "Эпик" || e.BoardID != pf {
			t.Errorf("у %s эпик %+v, ожидался «Эпик» с портфеля", f.card(id).Title, e)
		}
	}
	// Карточке самого портфеля эпик не назначается.
	snap, _ := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, pf)
	for _, c := range snap.Cards {
		if c.Epic != nil {
			t.Errorf("карточке портфеля назначен эпик: %+v", c.Epic)
		}
	}
}

// Привязали фичу с задачами к эпику — метку получают и задачи, тем же
// патчем.
func TestLinkingFeatureBringsEpicToItsTasks(t *testing.T) {
	f := newFixture(t)
	pf, epic := f.portfolio()
	feature := f.createCard("Фича", f.columnA)
	task := f.sub(feature, "Задача")

	res := f.applyTo(f.boardID, "LINK_CARDS", map[string]any{"fromCard": epic, "toCard": feature, "kind": "subtask"})
	got := map[string]*EpicRef{}
	for _, c := range res.Patch.Cards {
		got[c.ID] = c.Epic
	}
	for _, id := range []string{feature, task} {
		if e, ok := got[id]; !ok || e == nil || e.ID != epic {
			t.Errorf("патч связывания: у %s эпик %+v (в патче: %v)", id, e, ok)
		}
	}
	_ = pf
}

// Портфель, которого человеку не видно, метки не даёт.
func TestHiddenEpicGivesNoMark(t *testing.T) {
	f := newFixture(t)
	pf, epic := f.portfolio()
	f.applyTo(pf, "CREATE_SUBTASK", map[string]any{"parentCardId": epic, "title": "Фича", "boardId": f.boardID})
	f.inTenant(func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `insert into board_members (org_id, board_id, user_id) values ($1, $2, $3)`,
			f.orgID, pf, f.actorID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `update boards set visibility = 'private' where id = $1`, pf)
		return err
	})
	outsider := f.inviteMember("Посторонний")
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, outsider, f.boardID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range snap.Cards {
		if c.Epic != nil {
			t.Errorf("постороннему видна метка скрытого эпика: %+v", c.Epic)
		}
	}
}
