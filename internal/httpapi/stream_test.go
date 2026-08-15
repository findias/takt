package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Поток изменений доски. Проверяется то, ради чего он заведён: изменение,
// сделанное одним, доходит до другого без перезагрузки.

func TestBoardChangeReachesTheOtherClient(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")
	boardID := owner.board("Найм")

	raw := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}

	// Второй клиент слушает поток.
	req, err := http.NewRequest("GET", a.server.URL+"/api/boards/"+boardID+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := member.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("поток не открылся: код %d", resp.StatusCode)
	}
	if got := resp.Header.Get("content-type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("тип ответа %q", got)
	}

	events := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
				events <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Первый клиент двигает карточку.
	owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": snap.Columns[0].ID, "title": "Задача"},
	}, http.StatusOK)

	select {
	case body := <-events:
		var change struct {
			BoardID string `json:"boardId"`
			Version int64  `json:"version"`
			ActorID string `json:"actorId"`
		}
		if err := json.Unmarshal([]byte(body), &change); err != nil {
			t.Fatalf("уведомление не разбирается: %s", body)
		}
		if change.BoardID != boardID || change.Version < 2 {
			t.Errorf("уведомление о чужой доске или без версии: %+v", change)
		}
		// Автор нужен затем, чтобы клиент не дёргался на собственное
		// изменение: оно придёт ответом на операцию.
		if change.ActorID != owner.userID {
			t.Errorf("автор изменения %q, ожидался %q", change.ActorID, owner.userID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("изменение не дошло до слушателя за пять секунд")
	}
}

// Поток недоступной доски не открывается: подписка — это чтение,
// и правило здесь то же, что и везде.
func TestStreamOfForeignBoardIsNotFound(t *testing.T) {
	a := newAPI(t)
	first := a.registerOrg("Первая")
	second := a.registerOrg("Вторая")
	boardID := first.board("Найм")

	if code, _ := second.do("GET", "/api/boards/"+boardID+"/stream", nil); code != http.StatusNotFound {
		t.Errorf("чужой поток открылся: код %d, ожидался 404", code)
	}
}
