package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type decisionAnswer struct {
	Kind   string  `json:"kind"`
	Note   string  `json:"note"`
	Author *string `json:"author"`
	Card   *struct {
		ID      string `json:"id"`
		BoardID string `json:"boardId"`
		Number  string `json:"number"`
		Column  string `json:"column"`
	} `json:"card"`
	Into *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"into"`
}

type decidedIdea struct {
	ideaAnswer
	Decision  *decisionAnswer `json:"decision"`
	CanDecide bool            `json:"canDecide"`
	CanMove   bool            `json:"canMove"`
}

func (s *session) idea(title string, teamID any) string {
	s.api.t.Helper()
	body := map[string]any{"title": title}
	if teamID != nil {
		body["teamId"] = teamID
	}
	raw := s.mustDo("POST", "/api/ideas", body, http.StatusCreated)
	id, _ := field(s.api.t, raw, "id").(string)
	return id
}

func (s *session) decide(ideaID string, body map[string]any, want int) decidedIdea {
	s.api.t.Helper()
	return s.decideLike("POST", "/api/ideas/"+ideaID+"/decisions", body, want)
}

// decideLike — вызов, отвечающий идеей целиком при удаче и сырым
// отказом (в ID) иначе: так проверка читает код отказа без второго типа.
func (s *session) decideLike(method, path string, body map[string]any, want int) decidedIdea {
	s.api.t.Helper()
	var out decidedIdea
	raw := s.mustDo(method, path, body, want)
	if want < http.StatusBadRequest {
		if err := json.Unmarshal(raw, &out); err != nil {
			s.api.t.Fatal(err)
		}
	} else {
		out.ID = string(raw)
	}
	return out
}

func (s *session) decidedIdeas() map[string]decidedIdea {
	s.api.t.Helper()
	var list struct {
		Ideas []decidedIdea `json:"ideas"`
	}
	if err := json.Unmarshal(s.mustDo("GET", "/api/ideas", nil, http.StatusOK), &list); err != nil {
		s.api.t.Fatal(err)
	}
	out := map[string]decidedIdea{}
	for _, i := range list.Ideas {
		out[i.ID] = i
	}
	return out
}

// Решение по идее (ROADMAP 43, срез 2): решает владелец организации
// и владелец подразделения в своём поддереве; «не берём» требует причину;
// «берём» заводит карточку на доске, куда решающий пишет; «уже есть»
// складывает голоса в названную идею; записи копятся историей.
func TestIdeasAreDecidedByOwnersWithWordsCardsAndMergedVotes(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Решения по идеям")
	viewer := owner.join("viewer")
	boris := owner.join("member")
	platform := owner.team("Платформа", nil)
	sales := owner.team("Продажи", nil)
	owner.mustDo("POST", "/api/team-admins", map[string]any{"userId": boris.userID, "teamId": platform}, http.StatusCreated)

	toPlatform := viewer.idea("Чат с дежурным", platform)
	orgWide := boris.idea("Шаблон карточки", nil)
	toSales := viewer.idea("Шаблоны карточек", sales)
	viewer.mustDo("PUT", "/api/ideas/"+toSales+"/vote", nil, http.StatusOK)
	boris.mustDo("PUT", "/api/ideas/"+orgWide+"/vote", nil, http.StatusOK)

	// Кто вправе: наблюдатель — никто; Борис — только адресованное
	// «Платформе»; владелец — всё. Право видно и в списке.
	viewer.decide(toPlatform, map[string]any{"decision": "later"}, http.StatusForbidden)
	boris.decide(orgWide, map[string]any{"decision": "later"}, http.StatusForbidden)
	boris.decide(toSales, map[string]any{"decision": "later"}, http.StatusForbidden)
	seen := boris.decidedIdeas()
	if !seen[toPlatform].CanDecide || seen[orgWide].CanDecide || seen[toSales].CanDecide {
		t.Errorf("право решать глазами Бориса: платформа %v, общая %v, продажи %v",
			seen[toPlatform].CanDecide, seen[orgWide].CanDecide, seen[toSales].CanDecide)
	}
	later := boris.decide(toPlatform, map[string]any{"decision": "later", "note": "после переезда"}, http.StatusCreated)
	if later.Decision == nil || later.Decision.Kind != "later" || later.Decision.Note != "после переезда" ||
		later.Decision.Author == nil || *later.Decision.Author != "Гость" {
		t.Errorf("«позже» от Бориса: %+v", later.Decision)
	}

	// «Не берём» — только с причиной.
	if raw := owner.decide(orgWide, map[string]any{"decision": "declined"}, http.StatusBadRequest); !strings.Contains(raw.ID, `"idea_reason_required"`) {
		t.Errorf("отказ без причины: %s", raw.ID)
	}
	declined := owner.decide(orgWide, map[string]any{"decision": "declined", "note": "уже есть шаблон доски"}, http.StatusCreated)
	if declined.Decision == nil || declined.Decision.Kind != "declined" {
		t.Errorf("«не берём»: %+v", declined.Decision)
	}

	// «Берём»: нужна доска, в которую решающий пишет; появляется карточка
	// с описанием, откуда она.
	if raw := owner.decide(toPlatform, map[string]any{"decision": "taken"}, http.StatusBadRequest); !strings.Contains(raw.ID, `"idea_board_required"`) {
		t.Errorf("«берём» без доски: %s", raw.ID)
	}
	secret := owner.board("Тайная")
	owner.mustDo("PUT", "/api/boards/"+secret+"/access", map[string]any{"visibility": "private"}, http.StatusNoContent)
	if raw := boris.decide(toPlatform, map[string]any{"decision": "taken", "boardId": secret}, http.StatusBadRequest); !strings.Contains(raw.ID, `"idea_board_readonly"`) {
		t.Errorf("«берём» на чужую доску: %s", raw.ID)
	}
	work := owner.board("Работа")
	taken := owner.decide(toPlatform, map[string]any{"decision": "taken", "boardId": work}, http.StatusCreated)
	if taken.Decision == nil || taken.Decision.Kind != "taken" || taken.Decision.Card == nil ||
		taken.Decision.Card.BoardID != work || taken.Decision.Card.Number == "" {
		t.Fatalf("«берём»: %+v", taken.Decision)
	}
	var snap struct {
		Cards []struct {
			ID, Title, Description string
		} `json:"cards"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+work, nil, http.StatusOK), &snap); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range snap.Cards {
		if c.ID == taken.Decision.Card.ID {
			found = true
			if c.Title != "Чат с дежурным" || !strings.Contains(c.Description, "Из идеи «Чат с дежурным»") {
				t.Errorf("карточка из идеи: %+v", c)
			}
		}
	}
	if !found {
		t.Errorf("карточки из идеи нет на доске: %+v", snap.Cards)
	}

	// «Уже есть»: цель обязательна и не сама идея; голоса складываются.
	if raw := owner.decide(toSales, map[string]any{"decision": "duplicate"}, http.StatusBadRequest); !strings.Contains(raw.ID, `"idea_target_required"`) {
		t.Errorf("двойник без цели: %s", raw.ID)
	}
	if raw := owner.decide(toSales, map[string]any{"decision": "duplicate", "intoIdeaId": toSales}, http.StatusBadRequest); !strings.Contains(raw.ID, `"idea_target_self"`) {
		t.Errorf("двойник самой себя: %s", raw.ID)
	}
	dup := owner.decide(toSales, map[string]any{"decision": "duplicate", "intoIdeaId": orgWide}, http.StatusCreated)
	if dup.Decision == nil || dup.Decision.Into == nil || dup.Decision.Into.Title != "Шаблон карточки" {
		t.Errorf("«уже есть»: %+v", dup.Decision)
	}
	merged := viewer.decidedIdeas()[orgWide]
	if merged.Votes != 2 || !merged.Voted || len(merged.Voters) != 2 {
		t.Errorf("голоса после склейки: %+v", merged.ideaAnswer)
	}

	// История — от нового к старому.
	var history struct {
		Decisions []decisionAnswer `json:"decisions"`
	}
	if err := json.Unmarshal(viewer.mustDo("GET", "/api/ideas/"+toPlatform+"/decisions", nil, http.StatusOK), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Decisions) != 2 || history.Decisions[0].Kind != "taken" || history.Decisions[1].Kind != "later" {
		t.Errorf("история решений: %+v", history.Decisions)
	}
	owner.decide(orgWide, map[string]any{"decision": "maybe"}, http.StatusBadRequest)
	viewer.mustDo("GET", "/api/ideas/00000000-0000-0000-0000-000000000000/decisions", nil, http.StatusNotFound)
}
