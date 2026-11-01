package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/findias/takt/internal/idea"
)

func TestTakingAnIdeaSurvivesSequentialAndConcurrentRetries(t *testing.T) {
	for _, mode := range []struct {
		name         string
		concurrent   bool
		useRequestID bool
	}{
		{"по очереди без ID", false, false},
		{"одновременно без ID", true, false},
		{"по очереди с ID", false, true},
		{"одновременно с ID", true, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			concurrent := mode.concurrent
			a := newAPI(t)
			owner := a.registerOrg("Повтор решения")
			author := owner.join("viewer")
			id := author.idea("Одна карточка на идею", nil)
			boardID := owner.board("Работа")
			body := map[string]any{"decision": "taken", "boardId": boardID, "note": "берём"}
			if mode.useRequestID {
				body["requestId"] = uuid.NewString()
			}
			type answer struct {
				status int
				body   []byte
			}
			answers := make(chan answer, 4)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for range 4 {
				send := func() {
					defer wg.Done()
					if concurrent {
						<-start
					}
					status, raw := owner.do("POST", "/api/ideas/"+id+"/decisions", body)
					answers <- answer{status, raw}
				}
				wg.Add(1)
				if concurrent {
					go send()
				} else {
					send()
				}
			}
			close(start)
			wg.Wait()
			close(answers)
			var cardID string
			for got := range answers {
				if got.status != http.StatusCreated {
					t.Fatalf("решение: %d %s", got.status, got.body)
				}
				var i decidedIdea
				if err := json.Unmarshal(got.body, &i); err != nil {
					t.Fatal(err)
				}
				if i.Decision == nil || i.Decision.Card == nil {
					t.Fatalf("нет карточки решения: %s", got.body)
				}
				if cardID != "" && cardID != i.Decision.Card.ID {
					t.Errorf("повтор создал другую карточку: %s вместо %s", i.Decision.Card.ID, cardID)
				}
				cardID = i.Decision.Card.ID
			}
			if n := len(ideaDecisionHistory(t, owner, id)); n != 1 {
				t.Errorf("повтор записал %d решений вместо одного", n)
			}
			if cards := ideaBoardCards(t, owner, boardID); len(cards) != 1 {
				t.Errorf("после повтора на доске %d карточек вместо одной", len(cards))
			}
			if got := len(author.notifications().Items); got != 1 {
				t.Errorf("повтор прислал %d уведомлений вместо одного", got)
			}
			// Изменённый ответ — новая запись, а не подавленный повтор.
			owner.decide(id, map[string]any{"decision": "later", "note": "сначала перенос"}, http.StatusCreated)
			if mode.useRequestID {
				body["requestId"] = uuid.NewString()
			}
			owner.decide(id, body, http.StatusCreated)
			if n := len(ideaDecisionHistory(t, owner, id)); n != 3 {
				t.Errorf("после нового решения история: %d вместо трёх", n)
			}
		})
	}
}

func ideaDecisionHistory(t *testing.T, s *session, id string) []decisionAnswer {
	t.Helper()
	var history struct {
		Decisions []decisionAnswer `json:"decisions"`
	}
	if err := json.Unmarshal(s.mustDo("GET", "/api/ideas/"+id+"/decisions", nil, http.StatusOK), &history); err != nil {
		t.Fatal(err)
	}
	return history.Decisions
}

func ideaBoardCards(t *testing.T, s *session, boardID string) []struct{ ID string } {
	t.Helper()
	var snapshot struct{ Cards []struct{ ID string } }
	if err := json.Unmarshal(s.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot.Cards
}

type failingIdeaCardMaker struct{ base idea.Cards }

var errAfterIdeaCard = errors.New("проверка отката после создания карточки")

func (m failingIdeaCardMaker) CreateCard(ctx context.Context, tx pgx.Tx, orgID, actorID, boardID, columnID, title, description string) (string, error) {
	id, err := m.base.CreateCard(ctx, tx, orgID, actorID, boardID, columnID, title, description)
	if err != nil {
		return "", err
	}
	return id, errAfterIdeaCard
}

func TestFailedIdeaDecisionRollsBackItsCard(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Атомарность решения")
	id := owner.idea("Без карточки после отказа", nil)
	boardID := owner.board("Работа")
	orgID := field(t, owner.mustDo("GET", "/api/me", nil, http.StatusOK), "orgId").(string)
	before := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	_, err := a.impl.ideas.Decide(context.Background(), orgID, owner.userID, id,
		idea.DecideRequest{Kind: idea.Taken, BoardID: boardID}, failingIdeaCardMaker{idea.Cards{Boards: a.impl.boards}})
	if !errors.Is(err, errAfterIdeaCard) {
		t.Fatalf("ожидалась проверочная ошибка, получено %v", err)
	}
	if cards := ideaBoardCards(t, owner, boardID); len(cards) != 0 {
		t.Errorf("после отказа осталось %d карточек", len(cards))
	}
	after := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	if field(t, before, "board", "version") != field(t, after, "board", "version") {
		t.Error("отказ изменил версию доски")
	}
	if len(ideaDecisionHistory(t, owner, id)) != 0 {
		t.Error("после отказа сохранилось решение")
	}
}

func TestIdeaDecisionRetryKeepsItsResultAfterArchivingTheCard(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Повтор после архивации")
	id := owner.idea("Не заводить заново", nil)
	boardID := owner.board("Работа")
	body := map[string]any{"decision": "taken", "boardId": boardID}
	taken := owner.decide(id, body, http.StatusCreated)
	owner.op(boardID, uuid.NewString(), "ARCHIVE_CARD", map[string]any{"cardId": taken.Decision.Card.ID})
	owner.decide(id, body, http.StatusCreated)
	if cards := ideaBoardCards(t, owner, boardID); len(cards) != 0 {
		t.Errorf("повтор завёл %d новых карточек вместо убранной", len(cards))
	}
	if n := len(ideaDecisionHistory(t, owner, id)); n != 1 {
		t.Errorf("после архивации повтор записал %d решений", n)
	}
}

func TestIdeaDecisionWrittenByPreviousVersionCanBeRepeated(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Повтор после обновления")
	id := owner.idea("Уже приняли", nil)
	boardID := owner.board("Работа")
	cardID := owner.cardOn(boardID, "Уже приняли")
	orgID := field(t, owner.mustDo("GET", "/api/me", nil, http.StatusOK), "orgId").(string)
	ctx := context.Background()
	// Старый бинарник не знает про отпечаток, пользуется умолчанием.
	err := a.impl.db.InTenant(ctx, orgID, owner.userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			insert into idea_decisions (org_id, idea_id, author_id, decision, card_id)
			values ($1, $2, $3, 'taken', $4)`, orgID, id, owner.userID, cardID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	repeated := owner.decide(id, map[string]any{"decision": "taken", "boardId": boardID}, http.StatusCreated)
	if repeated.Decision.Card == nil || repeated.Decision.Card.ID != cardID {
		t.Errorf("обновление поменяло карточку: %+v", repeated.Decision)
	}
	if n := len(ideaDecisionHistory(t, owner, id)); n != 1 {
		t.Errorf("повтор старого решения записал %d решений", n)
	}
}
