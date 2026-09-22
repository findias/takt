package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Уведомления внутри приложения (ROADMAP, этап 29). Проверяются
// обещания, ради которых этап и расписан: видит только получатель;
// ничего не утекает после отзыва доступа; себе — никогда; повтор
// операции двойника не даёт; снятие по сроку доходит до исполнителя.

type notificationsPage struct {
	Unread int `json:"unread"`
	Items  []struct {
		ID        string  `json:"id"`
		Reason    string  `json:"reason"`
		CardID    string  `json:"cardId"`
		CardTitle string  `json:"cardTitle"`
		ActorName *string `json:"actorName"`
		Read      bool    `json:"read"`
	} `json:"items"`
}

func (s *session) notifications() notificationsPage {
	s.api.t.Helper()
	var page notificationsPage
	if err := json.Unmarshal(s.mustDo("GET", "/api/notifications", nil, http.StatusOK), &page); err != nil {
		s.api.t.Fatal(err)
	}
	return page
}

// op выполняет операцию над доской с заданным идентификатором: тот же
// идентификатор дважды — это повтор, а не второе действие.
func (s *session) op(boardID, operationID, kind string, payload map[string]any) []byte {
	s.api.t.Helper()
	return s.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": operationID, "type": kind, "payload": payload,
	}, http.StatusOK)
}

// cardOn заводит карточку в первой колонке доски.
func (s *session) cardOn(boardID, title string) string {
	s.api.t.Helper()
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(s.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap); err != nil {
		s.api.t.Fatal(err)
	}
	raw := s.op(boardID, uuid.NewString(), "CREATE_CARD",
		map[string]any{"columnId": snap.Columns[0].ID, "title": title})
	var result struct {
		Patch struct {
			Cards []struct{ ID string } `json:"cards"`
		} `json:"patch"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || len(result.Patch.Cards) == 0 {
		s.api.t.Fatalf("карточка не завелась: %s", raw)
	}
	return result.Patch.Cards[0].ID
}

func TestNotificationsReachTheirRecipientOnly(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Уведомления")
	boris := a.member(owner, "member")
	vera := a.member(owner, "member")
	boardID := owner.board("Поставки")
	cardID := owner.cardOn(boardID, "Согласовать смету")

	// Назначили Бориса и упомянули его в обсуждении.
	owner.op(boardID, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": boris.userID})
	owner.mustDo("POST", "/api/boards/"+boardID+"/cards/"+cardID+"/comments",
		map[string]any{"body": "Борис, глянь", "mentions": []string{boris.userID}}, http.StatusCreated)

	got := boris.notifications()
	if got.Unread != 2 || len(got.Items) != 2 {
		t.Fatalf("Борису: непрочитанных %d, всего %d, ожидалось по 2: %+v", got.Unread, len(got.Items), got.Items)
	}
	reasons := map[string]bool{}
	for _, n := range got.Items {
		reasons[n.Reason] = true
		if n.CardID != cardID || n.CardTitle != "Согласовать смету" || n.ActorName == nil {
			t.Errorf("уведомление без карточки или автора: %+v", n)
		}
	}
	if !reasons["assigned"] || !reasons["mentioned"] {
		t.Errorf("поводы: %v, ожидались assigned и mentioned", reasons)
	}

	// Чужие не видны: у Веры пусто, у владельца — тоже (сам всё и сделал).
	if n := vera.notifications(); n.Unread != 0 || len(n.Items) != 0 {
		t.Errorf("Вере пришло чужое: %+v", n)
	}
	if n := owner.notifications(); len(n.Items) != 0 {
		t.Errorf("владельцу пришло о собственных действиях: %+v", n)
	}

	// Вера не может отметить прочитанным чужое: политика не даёт.
	vera.mustDo("POST", "/api/notifications/read",
		map[string]any{"ids": []string{got.Items[0].ID}}, http.StatusNoContent)
	if n := boris.notifications(); n.Unread != 2 {
		t.Errorf("чужая отметка «прочитано» сработала: у Бориса непрочитанных %d", n.Unread)
	}

	// Своё — отмечается по одному и разом.
	boris.mustDo("POST", "/api/notifications/read",
		map[string]any{"ids": []string{got.Items[0].ID}}, http.StatusNoContent)
	if n := boris.notifications(); n.Unread != 1 {
		t.Errorf("после одной отметки непрочитанных %d, ожидалось 1", n.Unread)
	}
	boris.mustDo("POST", "/api/notifications/read", map[string]any{}, http.StatusNoContent)
	if n := boris.notifications(); n.Unread != 0 || len(n.Items) != 2 {
		t.Errorf("после «прочитать всё»: непрочитанных %d, всего %d", n.Unread, len(n.Items))
	}
}

func TestNoNotificationForYourOwnActionAndNoTwinOnReplay(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Себе и дважды")
	boris := a.member(owner, "member")
	boardID := owner.board("Доска")
	cardID := owner.cardOn(boardID, "Задача")

	// Себя назначил и себя упомянул — известий нет: кто сделал, тот знает.
	owner.op(boardID, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": owner.userID})
	owner.mustDo("POST", "/api/boards/"+boardID+"/cards/"+cardID+"/comments",
		map[string]any{"body": "себе", "mentions": []string{owner.userID}}, http.StatusCreated)
	if n := owner.notifications(); len(n.Items) != 0 {
		t.Errorf("уведомление о собственном действии: %+v", n.Items)
	}

	// Повтор той же операции — одно назначение и одно уведомление.
	opID := uuid.NewString()
	owner.op(boardID, opID, "ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": boris.userID})
	owner.op(boardID, opID, "ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": boris.userID})
	if n := boris.notifications(); len(n.Items) != 1 {
		t.Errorf("повтор операции: уведомлений %d, ожидалось 1", len(n.Items))
	}
}

// Самое опасное место этапа: отняли доступ к закрытой доске —
// прежнее уведомление не раскрывает названия карточки.
func TestNotificationDisappearsWhenBoardAccessIsGone(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Закрытая доска")
	boris := owner.join("member")
	boardID := owner.board("Найм")
	teamID := owner.team("Кадры", nil)
	owner.mustDo("PUT", "/api/teams/"+teamID+"/members/"+boris.userID, nil, http.StatusNoContent)
	owner.mustDo("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "team", "teamId": teamID}, http.StatusNoContent)
	cardID := owner.cardOn(boardID, "Секретная вакансия")
	owner.op(boardID, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": boris.userID})

	if n := boris.notifications(); n.Unread != 1 {
		t.Fatalf("пока доступ есть: непрочитанных %d, ожидалось 1", n.Unread)
	}

	// Бориса вывели из подразделения — доска для него закрыта.
	owner.mustDo("DELETE", "/api/teams/"+teamID+"/members/"+boris.userID, nil, http.StatusNoContent)
	n := boris.notifications()
	if n.Unread != 0 || len(n.Items) != 0 {
		t.Errorf("после отзыва доступа уведомление видно: %+v", n)
	}
	for _, item := range n.Items {
		if item.CardTitle == "Секретная вакансия" {
			t.Error("название карточки закрытой доски утекло через уведомление")
		}
	}
}

func TestBlockAndItsExpiryReachTheAssignee(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Блокировки")
	boris := a.member(owner, "member")
	boardID := owner.board("Доска")
	cardID := owner.cardOn(boardID, "Отгрузка")
	owner.op(boardID, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": cardID, "userId": boris.userID})

	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	owner.op(boardID, uuid.NewString(), "BLOCK_CARD",
		map[string]any{"cardId": cardID, "reason": "ждём склад", "until": until})

	// Срок переносится в прошлое мимо сервера — ждать настоящего
	// истечения в проверке незачем, — и проходит задача снятия.
	var orgID string
	if err := a.impl.db.Pool.QueryRow(context.Background(),
		`select org_id from memberships where user_id = $1`, owner.userID).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := a.impl.db.InTenant(context.Background(), orgID, owner.userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
			update card_blocks set blocked_until = now() - interval '1 minute'
			 where card_id = $1 and unblocked_at is null`, cardID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.impl.boards.ExpireBlocks(context.Background()); err != nil {
		t.Fatalf("проход по срокам: %v", err)
	}

	reasons := map[string]*string{}
	for _, n := range boris.notifications().Items {
		reasons[n.Reason] = n.ActorName
	}
	if _, ok := reasons["blocked"]; !ok {
		t.Error("исполнителю не пришло, что его карточку заблокировали")
	}
	actor, ok := reasons["block_expired"]
	if !ok {
		t.Fatal("исполнителю не пришло, что блокировка снялась по сроку")
	}
	if actor != nil {
		t.Errorf("у снятия по сроку автор %q, а снимала служебная задача", *actor)
	}
}
