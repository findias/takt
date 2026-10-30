package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type ideaAnswer struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Author *string  `json:"author"`
	TeamID *string  `json:"teamId"`
	Team   *string  `json:"team"`
	Votes  int      `json:"votes"`
	Voters []string `json:"voters"`
	Voted  bool     `json:"voted"`
}

func (s *session) ideas() []ideaAnswer {
	s.api.t.Helper()
	var list struct {
		Ideas []ideaAnswer `json:"ideas"`
	}
	if err := json.Unmarshal(s.mustDo("GET", "/api/ideas", nil, http.StatusOK), &list); err != nil {
		s.api.t.Fatal(err)
	}
	return list.Ideas
}

// Идеи (ROADMAP 43, срез 1): подаёт каждый в организации, в том числе
// наблюдатель; голос один на человека и снимается; чужая организация
// списка не видит.
func TestIdeasAreSubmittedAndSupportedByAnyoneInTheOrganisation(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Идеи")
	viewer := owner.join("viewer")
	member := owner.join("member")
	team := owner.team("Платформа", nil)

	// Наблюдатель подаёт идею — единственное, что он пишет.
	var mine ideaAnswer
	if err := json.Unmarshal(viewer.mustDo("POST", "/api/ideas", map[string]any{
		"title": "Чат с дежурным смежников", "body": "Почта идёт сутки.", "teamId": team,
	}, http.StatusCreated), &mine); err != nil {
		t.Fatal(err)
	}
	if mine.Author == nil || *mine.Author != "Гость" || mine.Team == nil || *mine.Team != "Платформа" {
		t.Errorf("идея наблюдателя: %+v", mine)
	}
	if mine.Votes != 0 || mine.Voted {
		t.Errorf("новая идея уже с голосами: %+v", mine)
	}

	// Голос: ставится один раз, повтор не удваивает, снимается — и снятие
	// без голоса не ошибка.
	for range 2 {
		member.mustDo("PUT", "/api/ideas/"+mine.ID+"/vote", nil, http.StatusOK)
	}
	var voted ideaAnswer
	if err := json.Unmarshal(viewer.mustDo("PUT", "/api/ideas/"+mine.ID+"/vote", nil, http.StatusOK), &voted); err != nil {
		t.Fatal(err)
	}
	if voted.Votes != 2 || !voted.Voted || len(voted.Voters) != 2 {
		t.Errorf("после двух голосов (участник дважды, наблюдатель): %+v", voted)
	}
	var retracted ideaAnswer
	if err := json.Unmarshal(member.mustDo("DELETE", "/api/ideas/"+mine.ID+"/vote", nil, http.StatusOK), &retracted); err != nil {
		t.Fatal(err)
	}
	if retracted.Votes != 1 || retracted.Voted {
		t.Errorf("после снятия голоса участника: %+v", retracted)
	}
	member.mustDo("DELETE", "/api/ideas/"+mine.ID+"/vote", nil, http.StatusOK)

	// Список — у всех один и тот же, «мой голос» — у каждого свой.
	if list := owner.ideas(); len(list) != 1 || list[0].Votes != 1 || list[0].Voted {
		t.Errorf("список глазами владельца: %+v", list)
	}
	if list := viewer.ideas(); len(list) != 1 || !list[0].Voted {
		t.Errorf("список глазами наблюдателя: %+v", list)
	}

	// Отказы — кодом, по которому форма находит поле.
	body := owner.mustDo("POST", "/api/ideas", map[string]any{"title": "   "}, http.StatusBadRequest)
	if !strings.Contains(string(body), `"idea_title_empty"`) {
		t.Errorf("пустое название: %s", body)
	}
	body = owner.mustDo("POST", "/api/ideas", map[string]any{"title": strings.Repeat("а", 201)}, http.StatusBadRequest)
	if !strings.Contains(string(body), `"idea_title_long"`) {
		t.Errorf("длинное название: %s", body)
	}
	body = owner.mustDo("POST", "/api/ideas", map[string]any{
		"title": "Длинно", "body": strings.Repeat("б", 5001)}, http.StatusBadRequest)
	if !strings.Contains(string(body), `"idea_body_long"`) {
		t.Errorf("длинное описание: %s", body)
	}
	body = owner.mustDo("POST", "/api/ideas", map[string]any{
		"title": "Куда-то", "teamId": "00000000-0000-0000-0000-000000000000"}, http.StatusBadRequest)
	if !strings.Contains(string(body), `"idea_team_unknown"`) {
		t.Errorf("неизвестное подразделение: %s", body)
	}

	// Чужая организация: ни списка, ни голоса.
	stranger := a.registerOrg("Чужие")
	if list := stranger.ideas(); len(list) != 0 {
		t.Errorf("посторонний видит идеи: %+v", list)
	}
	stranger.mustDo("PUT", "/api/ideas/"+mine.ID+"/vote", nil, http.StatusNotFound)
	// Ключ без разрешения на запись не голосует: голос — действие человека.
	key := owner.serviceKey("boards:read")
	if status, _ := a.withToken(key, "PUT", "/api/ideas/"+mine.ID+"/vote", nil); status != http.StatusForbidden {
		t.Errorf("ключ проголосовал: %d", status)
	}
}

// Идеи в контракте (ROADMAP 43, срез 4): ключ с boards:read читает
// список, с boards:write подаёт — автором становится ключ; голос
// и решение ключу закрыты. Подача и решение уходят подписчику
// событиями idea.created и idea.decided.
func TestIdeasAreOpenToKeysAndSendEvents(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Идеи ключом")
	target := newReceiver(t)
	owner.mustDo("POST", "/api/webhooks", map[string]any{
		"name": "Чат", "url": target.server.URL, "events": []string{"idea.created", "idea.decided"},
	}, http.StatusCreated)

	reader := owner.serviceKey("boards:read")
	writer := owner.serviceKey("boards:read", "boards:write")
	if status, body := a.withToken(reader, "POST", "/api/v1/ideas", map[string]any{"title": "Из чата"}); status != http.StatusForbidden {
		t.Errorf("ключ без boards:write подал идею: %d %s", status, body)
	}
	status, body := a.withToken(writer, "POST", "/api/v1/ideas", map[string]any{"title": "Из чата", "body": "Принёс бот."})
	if status != http.StatusCreated {
		t.Fatalf("подача ключом: %d %s", status, body)
	}
	id, _ := field(t, body, "id").(string)
	if author, _ := field(t, body, "author").(string); author == "" {
		t.Errorf("у идеи от ключа нет автора: %s", body)
	}
	status, body = a.withToken(reader, "GET", "/api/v1/ideas", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"Из чата"`) {
		t.Errorf("список ключом: %d %s", status, body)
	}
	if status, _ := a.withToken(writer, "PUT", "/api/v1/ideas/"+id+"/vote", nil); status != http.StatusForbidden {
		t.Errorf("ключ проголосовал: %d", status)
	}
	if status, _ := a.withToken(writer, "POST", "/api/v1/ideas/"+id+"/decisions", map[string]any{"decision": "later"}); status != http.StatusForbidden {
		t.Errorf("ключ решил: %d", status)
	}

	owner.decide(id, map[string]any{"decision": "later", "note": "после чата"}, http.StatusCreated)
	await(t, "события по идее не доехали", func() bool { return len(target.received()) >= 2 })
	events := map[string]bool{}
	for _, r := range target.received() {
		events[r.event] = true
		if !strings.Contains(string(r.body), `"Из чата"`) {
			t.Errorf("в событии %s нет идеи: %s", r.event, r.body)
		}
	}
	if !events["idea.created"] || !events["idea.decided"] {
		t.Errorf("события: %v", events)
	}
}
