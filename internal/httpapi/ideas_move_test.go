package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func (s *session) move(ideaID string, teamID any, want int) decidedIdea {
	s.api.t.Helper()
	return s.decideLike("PATCH", "/api/ideas/"+ideaID, map[string]any{"teamId": teamID}, want)
}

// Перенос идеи между уровнями (ROADMAP 45): автор — свою, пока по ней
// не решили, куда угодно; владелец подразделения — адресованную его
// поддереву внутрь поддерева или на всю организацию, в чужое — нет;
// владелец организации — любую открытую; решённую насовсем — никто.
func TestIdeasMoveBetweenLevels(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Уровни идей")
	viewer := owner.join("viewer")
	boris := owner.join("member")
	platform := owner.team("Платформа", nil)
	core := owner.team("Ядро", platform)
	sales := owner.team("Продажи", nil)
	owner.mustDo("POST", "/api/team-admins", map[string]any{"userId": boris.userID, "teamId": platform}, http.StatusCreated)

	// Автор переносит свою идею, пока не решили: общую — в «Продажи».
	mine := viewer.idea("Чат с дежурным", nil)
	moved := viewer.move(mine, sales, http.StatusOK)
	if moved.TeamID == nil || *moved.TeamID != sales || !moved.CanMove {
		t.Errorf("автор перенёс свою идею: кому %v, право %v", moved.TeamID, moved.CanMove)
	}
	// Чужую — нет, и неизвестное подразделение называется кодом.
	other := viewer.idea("Шаблон карточки", platform)
	boris.move(mine, nil, http.StatusForbidden)
	if raw := owner.move(other, "00000000-0000-0000-0000-000000000000", http.StatusBadRequest); !strings.Contains(raw.ID, `"idea_team_unknown"`) {
		t.Errorf("перенос в неизвестное подразделение: %s", raw.ID)
	}

	// Борис — владелец «Платформы»: внутрь поддерева и выше можно,
	// в «Продажи» — отказ с кодом; поднятая на организацию идея
	// ему больше не подвластна.
	seen := boris.decidedIdeas()
	if !seen[other].CanMove || seen[mine].CanMove {
		t.Errorf("право переноса глазами Бориса: платформенная %v, продажи %v", seen[other].CanMove, seen[mine].CanMove)
	}
	if got := boris.move(other, core, http.StatusOK); got.TeamID == nil || *got.TeamID != core {
		t.Errorf("перенос в «Ядро»: %v", got.TeamID)
	}
	if raw := boris.move(other, sales, http.StatusForbidden); !strings.Contains(raw.ID, `"idea_move_outside"`) {
		t.Errorf("перенос в чужое подразделение: %s", raw.ID)
	}
	if got := boris.move(other, nil, http.StatusOK); got.TeamID != nil || got.CanMove {
		t.Errorf("перенос на организацию: кому %v, право %v", got.TeamID, got.CanMove)
	}
	boris.move(other, platform, http.StatusForbidden)

	// После «позже» автор уже не переносит, владелец — переносит;
	// решённую насовсем не переносит никто.
	owner.decide(other, map[string]any{"decision": "later"}, http.StatusCreated)
	viewer.move(other, sales, http.StatusForbidden)
	if got := owner.move(other, sales, http.StatusOK); got.TeamID == nil || *got.TeamID != sales {
		t.Errorf("владелец перенёс после «позже»: %v", got.TeamID)
	}
	owner.decide(other, map[string]any{"decision": "declined", "note": "шаблон доски уже есть"}, http.StatusCreated)
	owner.move(other, nil, http.StatusConflict)
	if settled := viewer.decidedIdeas()[other]; settled.CanMove {
		t.Errorf("решённая идея считается переносимой: %+v", settled)
	}
	owner.move("00000000-0000-0000-0000-000000000000", nil, http.StatusNotFound)
}
