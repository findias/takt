package board

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Путь до корня (этап 32.2): от корня к родителю, недоступное звено
// названо, а не пропало, глубина ограничена.

func TestCardPathGoesFromRootToParent(t *testing.T) {
	f := newFixture(t)
	epic := f.createCard("Эпик", f.columnA)
	feature := f.sub(epic, "Фича")
	task := f.sub(feature, "Задача")

	path, err := f.svc.CardPath(f.ctx, f.orgID, f.actorID, task)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 2 || path[0].ID != epic || path[1].ID != feature {
		t.Fatalf("путь %+v, ожидалось Эпик › Фича", path)
	}
	if !path[0].Visible || path[0].Title != "Эпик" || path[0].BoardID != f.boardID || path[0].Number == "" {
		t.Errorf("звено корня без названия, номера или доски: %+v", path[0])
	}
	if root, err := f.svc.CardPath(f.ctx, f.orgID, f.actorID, epic); err != nil || len(root) != 0 {
		t.Errorf("у корня путь %+v, %v; ожидался пустой", root, err)
	}
}

// Родитель на закрытой доске: звено есть и названо недоступным, его
// содержимое не раскрыто, и выше него подъём не идёт.
func TestCardPathNamesTheHiddenLink(t *testing.T) {
	f := newFixture(t)
	secret := f.boardOfTeam("Закрытая", nil)
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `insert into board_members (org_id, board_id, user_id) values ($1, $2, $3)`,
			f.orgID, secret, f.actorID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(f.ctx, `update boards set visibility = 'private' where id = $1`, secret)
		return err
	})
	var secretCol string
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `select id from board_columns where board_id = $1 order by position limit 1`,
			secret).Scan(&secretCol)
	})
	top := f.applyTo(secret, "CREATE_CARD", map[string]any{"columnId": secretCol, "title": "Тайный эпик"}).Patch.Cards[0].ID
	hidden := f.applyTo(secret, "CREATE_SUBTASK", map[string]any{"parentCardId": top, "title": "Тайная фича"})
	var hiddenID string
	for _, c := range hidden.Patch.Cards {
		if c.ID != top {
			hiddenID = c.ID
		}
	}
	task := f.applyTo(secret, "CREATE_SUBTASK", map[string]any{
		"parentCardId": hiddenID, "title": "Открытая задача", "boardId": f.boardID})
	var taskID string
	for _, c := range task.Patch.Cards {
		if c.Title == "Открытая задача" {
			taskID = c.ID
		}
	}
	if taskID == "" {
		f.inTenant(func(tx pgx.Tx) error {
			return tx.QueryRow(f.ctx, `select id from cards where title = 'Открытая задача' and board_id = $1`,
				f.boardID).Scan(&taskID)
		})
	}

	outsider := f.inviteMember("Посторонний")
	path, err := f.svc.CardPath(f.ctx, f.orgID, outsider, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 1 || path[0].ID != hiddenID || path[0].Visible || path[0].Title != "" || path[0].BoardName != "" {
		t.Fatalf("путь постороннего %+v: ожидалось одно недоступное звено без названия и доски", path)
	}
	// Владельцу закрытой доски путь виден целиком.
	if full, _ := f.svc.CardPath(f.ctx, f.orgID, f.actorID, taskID); len(full) != 2 || !full[0].Visible {
		t.Errorf("путь участника закрытой доски %+v, ожидалось два видимых звена", full)
	}
	// Карточка закрытой доски для постороннего не существует.
	if _, err := f.svc.CardPath(f.ctx, f.orgID, outsider, hiddenID); !errors.Is(err, ErrNotFound) {
		t.Errorf("путь невидимой карточки: %v, ожидалось «не найдена»", err)
	}
	if _, err := f.svc.CardPath(f.ctx, f.orgID, outsider, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Errorf("путь несуществующей карточки: %v", err)
	}
}
