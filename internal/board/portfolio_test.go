package board

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Карточка эпика на портфеле (этап 33.5): где лежит его работа и где
// стоит — по доскам, и ветка через доски для вида «Дерево».

// partOn заводит часть карточки портфеля на доске команды и находит её
// в снимке той доски: патч портфеля чужих карточек не несёт.
func (f *fixture) partOn(pf, parent, title, boardID string) string {
	f.t.Helper()
	f.applyTo(pf, "CREATE_SUBTASK", map[string]any{"parentCardId": parent, "title": title, "boardId": boardID})
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, f.actorID, boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, c := range snap.Cards {
		if c.Title == title {
			return c.ID
		}
	}
	f.t.Fatalf("часть %q не появилась на доске", title)
	return ""
}

// epicWithTwoTeams — эпик с фичей на доске фикстуры (две задачи: одна
// сделана, другая стоит) и фичей-листом на соседней доске.
func (f *fixture) epicWithTwoTeams() (pf, epic, other, feature, done, stuck, leaf string) {
	f.t.Helper()
	pf, epic = f.portfolio()
	other = f.boardOfTeam("Соседи", nil)
	feature = f.partOn(pf, epic, "Фича", f.boardID)
	done = f.sub(feature, "Сделанная")
	stuck = f.sub(feature, "Стоящая")
	f.done(done)
	f.mustApply("BLOCK_CARD", map[string]any{"cardId": stuck, "reason": "ждём доступ"})
	leaf = f.partOn(pf, epic, "Фича соседей", other)
	return
}

func (f *fixture) teamsOf(boardID, cardID string, userID string) []TeamShare {
	f.t.Helper()
	snap, err := f.svc.Snapshot(f.ctx, f.orgID, userID, boardID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, c := range snap.Cards {
		if c.ID == cardID {
			return c.Teams
		}
	}
	f.t.Fatalf("карточки %s нет в снимке", cardID)
	return nil
}

func TestEpicCardSaysWhereItsWorkIs(t *testing.T) {
	f := newFixture(t)
	pf, epic, other, _, _, _, _ := f.epicWithTwoTeams()

	got := map[string]TeamShare{}
	for _, s := range f.teamsOf(pf, epic, f.actorID) {
		got[s.BoardID] = s
	}
	if len(got) != 2 {
		t.Fatalf("досок %d, ожидалось две: %+v", len(got), got)
	}
	// Листья фичи — её задачи, а не она сама; стоящая задача видна на эпике.
	if s := got[f.boardID]; s.Total != 2 || s.Done != 1 || s.Blocked != 1 {
		t.Errorf("доска фикстуры: %+v, ожидалось 1 из 2 и одна стоит", s)
	}
	if s := got[other]; s.Total != 1 || s.Done != 0 || s.Blocked != 0 || s.BoardName != "Соседи" {
		t.Errorf("соседи: %+v, ожидалось 0 из 1", s)
	}

	// Патч несёт тот же счёт, что снимок, — иначе значки прыгают
	// после каждой правки.
	f.inTenant(func(tx pgx.Tx) error {
		c, err := readCard(f.ctx, tx, pf, epic)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(c.Teams, f.teamsOf(pf, epic, f.actorID)) {
			t.Errorf("патч %+v расходится со снимком", c.Teams)
		}
		return nil
	})

	// Карточкам досок команд разбивка не нужна: их работа на своей доске.
	snap := f.snapshot()
	for _, c := range snap.Cards {
		if c.Teams != nil {
			t.Errorf("у карточки команды %q разбивка по доскам: %+v", c.Title, c.Teams)
		}
	}
}

func TestCardTreeCrossesBoards(t *testing.T) {
	f := newFixture(t)
	_, epic, other, feature, done, stuck, leaf := f.epicWithTwoTeams()

	nodes, err := f.svc.CardTree(f.ctx, f.orgID, f.actorID, epic)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]TreeNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	if len(nodes) != 5 || nodes[0].ID != epic || nodes[0].ParentID != "" {
		t.Fatalf("ветка %+v, ожидалось пять узлов корнем вперёд", nodes)
	}
	want := map[string]string{feature: epic, leaf: epic, done: feature, stuck: feature}
	for id, parent := range want {
		if n := byID[id]; !n.Visible || n.ParentID != parent {
			t.Errorf("узел %s: %+v, ожидался родитель %s", n.Title, n, parent)
		}
	}
	if !byID[done].Done || !byID[stuck].Blocked || byID[leaf].BoardID != other || byID[stuck].ColumnName == "" {
		t.Errorf("состояние узлов не дошло: %+v", nodes)
	}
}

// Закрытая доска: её значка на эпике нет, в ветке её карточка названа
// недоступной, а спросить ветку закрытой карточки нельзя вовсе.
func TestCardTreeRespectsVisibility(t *testing.T) {
	f := newFixture(t)
	pf, epic, other, _, _, _, leaf := f.epicWithTwoTeams()
	f.inTenant(func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `insert into board_members (org_id, board_id, user_id) values ($1, $2, $3)`,
			f.orgID, other, f.actorID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `update boards set visibility = 'private' where id = $1`, other)
		return err
	})
	outsider := f.inviteMember("Посторонний")

	for _, s := range f.teamsOf(pf, epic, outsider) {
		if s.BoardID == other {
			t.Errorf("постороннему виден значок закрытой доски: %+v", s)
		}
	}
	nodes, err := f.svc.CardTree(f.ctx, f.orgID, outsider, epic)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.ID == leaf && (n.Visible || n.Title != "" || n.BoardName != "") {
			t.Errorf("закрытая карточка раскрыта: %+v", n)
		}
	}
	if _, err := f.svc.CardTree(f.ctx, f.orgID, outsider, leaf); !errors.Is(err, ErrNotFound) {
		t.Errorf("ветка закрытой карточки: %v, ожидалось ErrNotFound", err)
	}
}
