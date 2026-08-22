package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Структура организации на уровне HTTP. Проверяются границы: кто читает
// дерево, кто его меняет, и во что превращается отказ базы.

// join добавляет в организацию владельца ещё одного человека с заданной
// ролью — через приглашение, потому что другого пути внутрь нет.
func (s *session) join(role string) *session {
	s.api.t.Helper()
	guest := s.api.session()
	guest.email = "join-" + uuid.NewString()[:8] + "@example.test"

	inv := s.mustDo("POST", "/api/invites", map[string]any{
		"email": guest.email, "role": role}, http.StatusCreated)
	link, _ := field(s.api.t, inv, "link").(string)
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")

	guest.mustDo("POST", "/api/invites/accept", map[string]any{
		"token": parts[len(parts)-1], "name": "Гость", "password": "parol12345"}, http.StatusOK)

	var me struct{ ID string }
	body := guest.mustDo("GET", "/api/me", nil, http.StatusOK)
	if err := json.Unmarshal(body, &me); err != nil {
		s.api.t.Fatal(err)
	}
	guest.userID = me.ID
	return guest
}

// team заводит подразделение и возвращает его идентификатор.
func (s *session) team(name string, parent any) string {
	s.api.t.Helper()
	body := map[string]any{"name": name}
	if parent != nil {
		body["parentId"] = parent
	}
	raw := s.mustDo("POST", "/api/teams", body, http.StatusCreated)
	id, _ := field(s.api.t, raw, "id").(string)
	if id == "" {
		s.api.t.Fatalf("создание команды не вернуло идентификатор: %s", raw)
	}
	return id
}

// Раскрытый узел структуры отвечал только «кто здесь», хотя доски
// подразделения обещаны были ещё этапом 2.1. Список тот же, что дерево
// считает числом: политики решают, кому какие доски видны, и одно и то
// же подразделение честно покажет разным людям разное.
func TestTeamBoardsAreListedAsThePolicyAllows(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	outsider := owner.join("member")

	dev := owner.team("Разработка", nil)
	other := owner.team("Продажи", nil)

	open := owner.board("Поставки")
	closed := owner.board("Найм")
	owner.mustDo("PUT", "/api/boards/"+open+"/access",
		map[string]any{"visibility": "org", "teamId": dev}, http.StatusNoContent)
	owner.mustDo("PUT", "/api/boards/"+closed+"/access",
		map[string]any{"visibility": "team", "teamId": dev}, http.StatusNoContent)

	// Владелец видит обе доски узла — и та, что видна всем, тоже его:
	// команда у доски отметка о принадлежности, а не только правило
	// доступа.
	if got := boardNames(t, owner, dev); len(got) != 2 {
		t.Errorf("доски узла у владельца: %v, ожидались обе", got)
	}
	// Посторонний участник видит только открытую: командную доску чужого
	// подразделения ему не видно, и число в дереве это тоже покажет.
	if got := boardNames(t, outsider, dev); len(got) != 1 || got[0] != "Поставки" {
		t.Errorf("доски узла у постороннего: %v, ожидалась одна открытая", got)
	}
	// Чужой узел не забирает себе досок соседа.
	if got := boardNames(t, owner, other); len(got) != 0 {
		t.Errorf("доски пустого узла: %v", got)
	}
}

func boardNames(t *testing.T, s *session, teamID string) []string {
	t.Helper()
	raw := s.mustDo("GET", "/api/teams/"+teamID+"/boards", nil, http.StatusOK)
	var list struct {
		Boards []struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"boards"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, b := range list.Boards {
		if b.Key == "" {
			t.Errorf("доска без ключа: %+v", b)
		}
		out = append(out, b.Name)
	}
	return out
}

func TestTeamTreeIsReadableByEveryoneAndChangeableByOwner(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")

	company := owner.team("Компания", nil)
	dev := owner.team("Разработка", company)

	raw := member.mustDo("GET", "/api/teams", nil, http.StatusOK)
	teams, _ := field(t, raw, "teams").([]any)
	if len(teams) != 2 {
		t.Fatalf("участник видит %d команд, ожидалось две; тело: %s", len(teams), raw)
	}

	// Менять структуру рядовому участнику нельзя — ни завести, ни перенести.
	if code, _ := member.do("POST", "/api/teams", map[string]any{"name": "Своя"}); code != http.StatusForbidden {
		t.Errorf("создание команды участником: код %d, ожидался 403", code)
	}
	if code, _ := member.do("PATCH", "/api/teams/"+dev, map[string]any{"root": true}); code != http.StatusForbidden {
		t.Errorf("перенос команды участником: код %d, ожидался 403", code)
	}

	// Владелец переименовывает и переносит одним запросом.
	owner.mustDo("PATCH", "/api/teams/"+dev,
		map[string]any{"name": "Продукт", "root": true}, http.StatusNoContent)

	raw = owner.mustDo("GET", "/api/teams", nil, http.StatusOK)
	var list struct {
		Teams []struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			ParentID *string `json:"parentId"`
			Depth    int     `json:"depth"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Teams {
		if item.ID != dev {
			continue
		}
		if item.Name != "Продукт" {
			t.Errorf("команда не переименована: %+v", item)
		}
		if item.ParentID != nil || item.Depth != 1 {
			t.Errorf("команда не стала корневой: %+v", item)
		}
	}
}

// Отказ ограничений дерева — это конфликт с человеческим объяснением,
// а не пятисотка: текст написан в миграции для того, кто его увидит.
func TestTreeLimitsAnswerWithConflict(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")

	var parent any
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		id := owner.team("Уровень", parent)
		ids = append(ids, id)
		parent = id
	}

	code, raw := owner.do("POST", "/api/teams", map[string]any{
		"name": "Шестой", "parentId": parent})
	if code != http.StatusConflict {
		t.Fatalf("шестой уровень: код %d, ожидался 409; тело: %s", code, raw)
	}
	if msg, _ := field(t, raw, "error").(string); !strings.Contains(msg, "глубина") {
		t.Errorf("отказ не объясняет причину: %s", raw)
	}

	// Цикл: корень под собственного потомка.
	code, raw = owner.do("PATCH", "/api/teams/"+ids[0], map[string]any{"parentId": ids[2]})
	if code != http.StatusConflict {
		t.Errorf("цикл: код %d, ожидался 409; тело: %s", code, raw)
	}
}

func TestTeamMembershipAndObservationOverHTTP(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	worker := owner.join("member")
	watcher := owner.join("member")

	dev := owner.team("Разработка", nil)

	owner.mustDo("PUT", "/api/teams/"+dev+"/members/"+worker.userID,
		map[string]any{"role": "lead"}, http.StatusNoContent)
	raw := worker.mustDo("GET", "/api/teams/"+dev+"/members", nil, http.StatusOK)
	members, _ := field(t, raw, "members").([]any)
	if len(members) != 1 {
		t.Fatalf("в составе %d человек, ожидался один; тело: %s", len(members), raw)
	}

	// Наблюдение выдаётся за поддеревом и видно в списке.
	obs := owner.mustDo("POST", "/api/observers", map[string]any{
		"userId": watcher.userID, "teamId": dev}, http.StatusCreated)
	obsID, _ := field(t, obs, "id").(string)
	if name, _ := field(t, obs, "teamName").(string); name != "Разработка" {
		t.Errorf("наблюдение выдано без подразделения: %s", obs)
	}

	// Повторная выдача — конфликт, а не второе такое же наблюдение.
	if code, _ := owner.do("POST", "/api/observers", map[string]any{
		"userId": watcher.userID, "teamId": dev}); code != http.StatusConflict {
		t.Errorf("повторная выдача наблюдения: код %d, ожидался 409", code)
	}

	// Раздавать наблюдение может только владелец.
	if code, _ := worker.do("POST", "/api/observers", map[string]any{
		"userId": worker.userID}); code != http.StatusForbidden {
		t.Errorf("участник выдал наблюдение: код %d, ожидался 403", code)
	}

	owner.mustDo("DELETE", "/api/observers/"+obsID, nil, http.StatusNoContent)
	if code, _ := owner.do("DELETE", "/api/observers/"+obsID, nil); code != http.StatusNotFound {
		t.Errorf("повторный отзыв: код %d, ожидался 404", code)
	}

	owner.mustDo("DELETE", "/api/teams/"+dev+"/members/"+worker.userID, nil, http.StatusNoContent)
}

// Структура чужой организации неотличима от несуществующей — то же
// правило, что и для досок.
func TestTeamsOfAnotherOrgAreInvisible(t *testing.T) {
	a := newAPI(t)
	first := a.registerOrg("Первая")
	second := a.registerOrg("Вторая")

	teamID := first.team("Разработка", nil)

	raw := second.mustDo("GET", "/api/teams", nil, http.StatusOK)
	if teams, _ := field(t, raw, "teams").([]any); len(teams) != 0 {
		t.Errorf("чужая организация видит %d команд", len(teams))
	}
	if code, _ := second.do("PATCH", "/api/teams/"+teamID,
		map[string]any{"name": "Захвачено"}); code != http.StatusNotFound {
		t.Errorf("переименование чужой команды: код %d, ожидался 404", code)
	}
	if code, _ := second.do("DELETE", "/api/teams/"+teamID, nil); code != http.StatusNotFound {
		t.Errorf("архивация чужой команды: код %d, ожидался 404", code)
	}
}

// Единицу оценки меняет владелец, и меняется она у всей организации.
//
// Через HTTP это проверяется отдельно от службы: путь закрыт обёрткой
// owner, а «кто я» после смены обязан отдавать новую единицу — иначе
// клиент показывает старую подпись до перезагрузки.
func TestEstimateUnitIsOwnerOnlyAndComesBackInWhoAmI(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Часы")
	member := owner.join("member")

	member.mustDo("PUT", "/api/org/estimate-unit", map[string]any{"unit": "hours"},
		http.StatusForbidden)

	me := owner.mustDo("PUT", "/api/org/estimate-unit", map[string]any{"unit": "hours"},
		http.StatusOK)
	if got := field(t, me, "estimateUnit"); got != "hours" {
		t.Errorf("после смены «кто я» отдаёт %v, ожидались часы", got)
	}
	who := owner.mustDo("GET", "/api/me", nil, http.StatusOK)
	if got := field(t, who, "estimateUnit"); got != "hours" {
		t.Errorf("«кто я» отдаёт %v, ожидались часы", got)
	}

	// Незнакомая единица — отказ с объяснением, а не пятисотая.
	owner.mustDo("PUT", "/api/org/estimate-unit", map[string]any{"unit": "story-points"},
		http.StatusBadRequest)
}

// Отключение последнего человека не трогает досок подразделения.
//
// Прогон девятого захода: каталог шлёт `active = false` единственному
// участнику группы. Состав пустеет — и вопрос, который до сих пор никто
// не задавал: что становится с досками этого узла. Ответ должен быть
// «ничего»: доска принадлежит подразделению, а не тому, кто в нём
// состоял, и увольнение — не повод её потерять.
//
// Второе, что здесь закреплено: видимость командной доски считается
// по дереву, а не по составу одного узла. Участник подразделения над
// опустевшим видит его доску, посторонний — нет; ровно это обещает
// экран «Структура», и до сих пор обещание держалось само.
func TestDeactivatingTheLastMemberLeavesTheTeamBoards(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отдел, который опустел")
	dir := a.withSCIMKey(t, owner)

	upper := owner.team("Надотдел", nil)
	empty := owner.team("Отдел одного", upper)

	person := dir.must("POST", "/scim/v2/Users",
		newUserPayload("odin-"+uuid.NewString()[:8]+"@example.test"), http.StatusCreated)
	personID := field(t, person, "id").(string)
	owner.mustDo("PUT", "/api/teams/"+empty+"/members/"+personID, nil, http.StatusNoContent)

	board := owner.mustDo("POST", "/api/boards", map[string]any{"name": "Доска отдела"},
		http.StatusCreated)
	boardID := field(t, board, "id").(string)
	owner.mustDo("PUT", "/api/boards/"+boardID+"/access", map[string]any{
		"visibility": "team", "teamId": empty,
	}, http.StatusNoContent)

	dir.must("PATCH", "/scim/v2/Users/"+personID, map[string]any{
		"schemas": []string{scimPatchSchema},
		"Operations": []map[string]any{
			{"op": "replace", "path": "active", "value": false},
		},
	}, http.StatusOK)

	// Состав пуст — это и было действие.
	if members, _ := field(t, owner.mustDo("GET", "/api/teams/"+empty+"/members", nil,
		http.StatusOK), "members").([]any); len(members) != 0 {
		t.Errorf("после отключения в составе %d человек", len(members))
	}
	// А доска на месте.
	boards, _ := field(t, owner.mustDo("GET", "/api/teams/"+empty+"/boards", nil,
		http.StatusOK), "boards").([]any)
	if len(boards) != 1 {
		t.Fatalf("у опустевшего подразделения %d досок, ожидалась одна", len(boards))
	}

	// Видимость считается по дереву: свой сверху видит, посторонний нет.
	above := owner.join("member")
	owner.mustDo("PUT", "/api/teams/"+upper+"/members/"+above.userID, nil, http.StatusNoContent)
	if code, body := above.do("GET", "/api/boards/"+boardID, nil); code != http.StatusOK {
		t.Errorf("участник надотдела не видит доски пустого отдела: %d %s", code, body)
	}
	if code, _ := owner.join("member").do("GET", "/api/boards/"+boardID, nil); code != http.StatusNotFound {
		t.Errorf("посторонний видит командную доску: код %d, ожидался 404", code)
	}

	// И третье: доступ, данный поверх состава, опустением не теряется.
	// Это ровно то, что говорит теперь экран, и говорить он это может
	// только если так и есть.
	admin := owner.join("member")
	owner.mustDo("POST", "/api/team-admins",
		map[string]any{"userId": admin.userID, "teamId": empty}, http.StatusCreated)
	if code, body := admin.do("GET", "/api/boards/"+boardID, nil); code != http.StatusOK {
		t.Errorf("администратор области не видит доски пустого подразделения: %d %s", code, body)
	}

	watcher := owner.join("viewer")
	owner.mustDo("POST", "/api/observers",
		map[string]any{"userId": watcher.userID, "teamId": empty}, http.StatusCreated)
	if code, body := watcher.do("GET", "/api/boards/"+boardID, nil); code != http.StatusOK {
		t.Errorf("наблюдатель не видит доски пустого подразделения: %d %s", code, body)
	}
}
