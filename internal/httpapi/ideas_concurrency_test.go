package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/idea"
)

type pausingIdeaCardMaker struct {
	base    idea.Cards
	ready   chan uint32
	proceed <-chan struct{}
}

func (m pausingIdeaCardMaker) CreateCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID, columnID, title, description string) (string, error) {
	id, err := m.base.CreateCard(ctx, tx, orgID, actorID, boardID, columnID, title, description)
	if err != nil {
		return "", err
	}
	m.ready <- tx.Conn().PgConn().PID()
	select {
	case <-m.proceed:
		return id, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Проверка ждёт настоящего ожидания замка в PostgreSQL, а не надеется,
// что за выбранную паузу горутина уже добралась до запроса.
func waitForIdeaLock(t *testing.T, a *api, ctx context.Context, blocker uint32) {
	t.Helper()
	for {
		var waiting bool
		err := a.impl.db.Pool.QueryRow(ctx, `
			select exists (select 1 from pg_stat_activity
			                where $1::int = any (pg_blocking_pids(pid)))`, int64(blocker)).Scan(&waiting)
		if err != nil {
			t.Fatalf("ожидание замка идеи: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("запрос не дождался замка идеи")
		}
	}
}

func TestIdeaMoveRechecksTheDecisionAfterWaiting(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Перенос ждёт решения")
	team := owner.team("Платформа", nil)
	id := owner.idea("Сначала решение", team)
	boardID := owner.board("Работа")
	orgID := field(t, owner.mustDo("GET", "/api/me", nil, http.StatusOK), "orgId").(string)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := make(chan uint32, 1)
	proceed := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(proceed) })
	defer unblock()
	decided := make(chan error, 1)
	go func() {
		_, err := a.impl.ideas.Decide(ctx, orgID, owner.userID, id,
			idea.DecideRequest{Kind: idea.Taken, BoardID: boardID},
			pausingIdeaCardMaker{idea.Cards{Boards: a.impl.boards}, ready, proceed})
		decided <- err
	}()
	var pid uint32
	select {
	case pid = <-ready:
	case err := <-decided:
		t.Fatalf("решение остановилось до карточки: %v", err)
	case <-ctx.Done():
		t.Fatal("решение не дошло до карточки")
	}
	moved := make(chan error, 1)
	go func() {
		_, err := a.impl.ideas.Move(ctx, orgID, owner.userID, id, nil)
		moved <- err
	}()
	waitForIdeaLock(t, a, ctx, pid)
	unblock()
	if err := <-decided; err != nil {
		t.Fatalf("решение: %v", err)
	}
	if err := <-moved; !errors.Is(err, idea.ErrSettled) {
		t.Fatalf("перенос после решения: %v вместо отказа", err)
	}
	if got := owner.decidedIdeas()[id]; got.TeamID == nil || *got.TeamID != team {
		t.Fatalf("решённая идея сменила подразделение: %+v", got)
	}
}

func TestIdeaDecisionRechecksItsRightsAfterWaitingForMove(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Решение ждёт переноса")
	boris := owner.join("member")
	platform := owner.team("Платформа", nil)
	sales := owner.team("Продажи", nil)
	owner.mustDo("POST", "/api/team-admins", map[string]any{"userId": boris.userID, "teamId": platform}, http.StatusCreated)
	id := owner.idea("Сначала перенос", platform)
	boardID := owner.board("Работа")
	orgID := field(t, owner.mustDo("GET", "/api/me", nil, http.StatusOK), "orgId").(string)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := a.impl.db.BeginTenant(ctx, orgID, owner.userID)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	// Тот же UPDATE, что у Move, удержан до попытки решения Бориса.
	if _, err := tx.Exec(ctx, `update ideas set team_id = $2 where id = $1`, id, sales); err != nil {
		t.Fatal(err)
	}
	decided := make(chan error, 1)
	go func() {
		_, err := a.impl.ideas.Decide(ctx, orgID, boris.userID, id,
			idea.DecideRequest{Kind: idea.Taken, BoardID: boardID}, idea.Cards{Boards: a.impl.boards})
		decided <- err
	}()
	waitForIdeaLock(t, a, ctx, tx.Conn().PgConn().PID())
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-decided; !errors.Is(err, idea.ErrCannotDecide) {
		t.Fatalf("решение после переноса в чужую область: %v", err)
	}
	if cards := ideaBoardCards(t, owner, boardID); len(cards) != 0 {
		t.Fatalf("отказ оставил %d карточек", len(cards))
	}
}
