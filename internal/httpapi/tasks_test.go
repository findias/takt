package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// Задачи человека со всех досок (вкладка «Задачи»): видно то, что
// спрашивающему видно и на самих досках, — и не больше.
func TestTasksOfAPersonAcrossBoards(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Задачи по людям")
	boris := owner.join("member")
	vera := owner.join("member")

	assign := func(boardID, title, userID string) {
		id := owner.cardOn(boardID, title)
		owner.op(boardID, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": id, "userId": userID})
	}
	supply, hiring, secret := owner.board("Поставки"), owner.board("Найм"), owner.board("Тайная")
	assign(supply, "Сверить остатки", boris.userID)
	assign(hiring, "Позвать кандидата", boris.userID)
	assign(secret, "Тайное дело", boris.userID)
	assign(supply, "Не Бориса", vera.userID)
	owner.mustDo("PUT", "/api/boards/"+secret+"/access",
		map[string]any{"visibility": "private"}, http.StatusNoContent)

	titles := func(s *session, query string) []string {
		var list struct {
			Tasks []struct {
				Title, BoardName string
			} `json:"tasks"`
		}
		if err := json.Unmarshal(s.mustDo("GET", "/api/tasks"+query, nil, http.StatusOK), &list); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, task := range list.Tasks {
			out = append(out, task.Title)
		}
		slices.Sort(out)
		return out
	}

	// Вера смотрит задачи Бориса: две доски, а закрытой ей не видно —
	// и в «Задачах» её нет.
	if got := titles(vera, "?user="+boris.userID); !slices.Equal(got, []string{"Позвать кандидата", "Сверить остатки"}) {
		t.Errorf("задачи Бориса глазами Веры: %q", got)
	}
	// Владелец видит и закрытую.
	if got := titles(owner, "?user="+boris.userID); len(got) != 3 {
		t.Errorf("задачи Бориса глазами владельца: %q", got)
	}
	// Без ?user= — свои.
	if got := titles(vera, ""); !slices.Equal(got, []string{"Не Бориса"}) {
		t.Errorf("свои задачи Веры: %q", got)
	}
	// Чужой организации человек — «не найден», а не пустой список.
	stranger := a.registerOrg("Чужие")
	vera.mustDo("GET", "/api/tasks?user="+stranger.userID, nil, http.StatusNotFound)
}
