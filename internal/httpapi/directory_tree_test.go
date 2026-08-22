package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Каталог шлёт плоские группы, а подразделения у нас — дерево.
//
// Проверяется то, о чём никто не спрашивал: что делает синхронизация
// с узлом, который человек вложил руками и в который вписал своего.
//
// Хорошая новость держится: вложенность синхронизацию переживает.
// Плохая — состав нет: провайдеры шлют полную замену, и вписанный
// руками исчезает молча. Выбирает это не наша сторона, спорить
// с протоколом мы не будем; но узел из каталога теперь назван таковым,
// и экран говорит, чей это состав, до того, как человек его поправит,
// а не после.
func TestDirectoryGroupsLiveInsideTheTree(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Каталог и дерево")
	dir := a.withSCIMKey(t, owner)

	manual := owner.mustDo("POST", "/api/teams",
		map[string]any{"name": "Продажи", "parentId": nil}, http.StatusCreated)
	manualID := idOf(t, manual)

	group := dir.must("POST", "/scim/v2/Groups",
		map[string]any{"displayName": "Разработка", "externalId": "ext-1"}, http.StatusCreated)
	groupID := idOf(t, group)

	// Узел из каталога назван таковым, заведённый руками — нет.
	byID := teamsByID(t, owner)
	if !byID[groupID].FromDirectory {
		t.Error("узел из каталога не назван таковым")
	}
	if byID[manualID].FromDirectory {
		t.Error("узел, заведённый руками, назван пришедшим из каталога")
	}

	// Человек вкладывает группу каталога в своё дерево и вписывает
	// в неё участника.
	owner.mustDo("PATCH", "/api/teams/"+groupID,
		map[string]any{"parentId": manualID}, http.StatusNoContent)
	person := dir.must("POST", "/scim/v2/Users",
		newUserPayload("boris@example.test"), http.StatusCreated)
	owner.mustDo("PUT", "/api/teams/"+groupID+"/members/"+idOf(t, person), nil,
		http.StatusNoContent)

	// Каталог синхронизирует состав полной заменой.
	dir.must("PATCH", "/scim/v2/Groups/"+groupID, map[string]any{
		"Operations": []map[string]any{
			{"op": "replace", "path": "members", "value": []map[string]any{}},
		},
	}, http.StatusOK)

	// Вложенность цела — дерево каталог не трогает.
	byID = teamsByID(t, owner)
	if p := byID[groupID].ParentID; p == nil || *p != manualID {
		t.Error("синхронизация вынесла группу из дерева")
	}

	// А состав каталог ведёт сам, и вписанный руками исчез. Закреплено
	// как есть: спорить с протоколом мы не будем, но и делать вид, что
	// состав здесь наш, тоже.
	var members struct {
		Members []struct{} `json:"members"`
	}
	if err := json.Unmarshal(
		owner.mustDo("GET", "/api/teams/"+groupID+"/members", nil, http.StatusOK),
		&members); err != nil {
		t.Fatal(err)
	}
	if len(members.Members) != 0 {
		t.Errorf("после синхронизации в составе %d человек", len(members.Members))
	}
}

type teamRow struct {
	ID            string  `json:"id"`
	ParentID      *string `json:"parentId"`
	FromDirectory bool    `json:"fromDirectory"`
}

func teamsByID(t *testing.T, owner *session) map[string]teamRow {
	t.Helper()
	var tree struct {
		Teams []teamRow `json:"teams"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/teams", nil, http.StatusOK), &tree); err != nil {
		t.Fatalf("разбор дерева: %v", err)
	}
	out := map[string]teamRow{}
	for _, n := range tree.Teams {
		out[n.ID] = n
	}
	return out
}
