package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Каталог не убирает подразделение, за которым числятся доски и отделы.
//
// Правило было записано в одном месте — в `team.Archive`, — а дверей
// к архивации две: экран и удаление группы каталогом. Каталог шёл своим
// запросом и проверок не делал.
//
// Прогон 22 августа 2026: владелец получил на тот же узел 409 «сначала
// перенесите вложенные команды и доски», ключ каталога следом получил
// 204. После этого в дереве оставался живой отдел с `parentId`
// на архивированного старшего — узел, у которого есть старший,
// но старшего нет в дереве, — а доска числилась за архивированным
// подразделением, то есть переставала быть видной всем, кто видел её
// этим подразделением.
//
// Теперь правило держит база (миграция 0045), и обе двери отвечают
// отказом — каждая на своём языке: экран словами приложения, каталог
// в формате SCIM и про группы.
func TestDirectoryDoesNotArchiveWhatTheScreenRefuses(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Каталог и структура")
	dir := a.withSCIMKey(t, owner)

	group := dir.must("POST", "/scim/v2/Groups",
		map[string]any{"displayName": "Разработка"}, http.StatusCreated)
	groupID := idOf(t, group)

	child := owner.mustDo("POST", "/api/teams",
		map[string]any{"name": "Платформа", "parentId": groupID}, http.StatusCreated)
	childID := idOf(t, child)

	board := owner.mustDo("POST", "/api/boards",
		map[string]any{"name": "Поставки", "key": "ПОСТ"}, http.StatusCreated)
	owner.mustDo("PUT", "/api/boards/"+idOf(t, board)+"/access",
		map[string]any{"visibility": "team", "teamId": groupID}, http.StatusNoContent)

	// Обе двери заперты, и обе объясняют.
	if code, body := owner.do("DELETE", "/api/teams/"+groupID, nil); code != http.StatusConflict {
		t.Errorf("экран убрал непустой узел: %d %s", code, body)
	}
	code, body := dir.do("DELETE", "/scim/v2/Groups/"+groupID, nil)
	if code != http.StatusConflict {
		t.Errorf("каталог убрал непустую группу: %d %s", code, body)
	}

	// Дерево цело: это и есть то, ради чего отказ. Отказ можно вернуть
	// и сломав что-нибудь другое — а вот живой отдел под архивированным
	// старшим виден только отсюда.
	var tree struct {
		Teams []struct {
			ID       string  `json:"id"`
			ParentID *string `json:"parentId"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/teams", nil, http.StatusOK), &tree); err != nil {
		t.Fatal(err)
	}
	if len(tree.Teams) != 2 {
		t.Fatalf("в дереве %d узлов, ожидалось 2", len(tree.Teams))
	}
	for _, n := range tree.Teams {
		if n.ID == childID && (n.ParentID == nil || *n.ParentID != groupID) {
			t.Error("отдел потерял старшего")
		}
	}

	var archived struct {
		Teams []struct{} `json:"teams"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/teams/archived", nil, http.StatusOK), &archived); err != nil {
		t.Fatal(err)
	}
	if len(archived.Teams) != 0 {
		t.Errorf("в архиве %d подразделений, ожидалось пусто", len(archived.Teams))
	}

	// А когда убирать нечего — каталог убирает: отказ про непустоту,
	// а не про то, что каталогу вообще нельзя.
	empty := dir.must("POST", "/scim/v2/Groups",
		map[string]any{"displayName": "Курсы"}, http.StatusCreated)
	dir.must("DELETE", "/scim/v2/Groups/"+idOf(t, empty), nil, http.StatusNoContent)
}

func idOf(t *testing.T, body []byte) string {
	t.Helper()
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if v.ID == "" {
		t.Fatalf("в ответе нет id: %s", body)
	}
	return v.ID
}
