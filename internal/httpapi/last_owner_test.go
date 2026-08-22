package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Владельцем, которого считают, бывает только человек.
//
// «Последнего владельца снять нельзя» — правило старое и записанное
// как «цело» в разборе входа. Оно и было цело ровно до тех пор, пока
// в организации не появлялся ключ каталога: у него роль владельца
// организации — её требуют политики на подразделениях и их составе, —
// и его служебная личность стоит в составе рядом со всеми. Оба сторожа
// считали владельцев одинаково — `count(*) … where role = 'owner'`, —
// и в такой организации «другой владелец» находился всегда.
//
// Прогон 22 августа 2026: человек снял сам себя, получил 204,
// и следующий запрос ответил «нужно войти». Организация осталась
// с владельцем, который не умеет входить: войти паролем служебной
// личности нельзя, а других владельцев нет. Чинится руками в базе —
// ровно то, ради чего правило и писали.
//
// Дверей четыре, и проверяются все: снять участника, разжаловать его,
// отключить каталогом, удалить каталогом. Считает их теперь одна
// функция базы (миграция 0047) — «кто считается владельцем» определено
// один раз, и разойтись двум ответам больше негде.
func TestLastOwnerMeansTheLastPerson(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Один владелец")
	dir := a.withSCIMKey(t, owner)

	var who struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/me", nil, http.StatusOK), &who); err != nil {
		t.Fatal(err)
	}

	// В организации двое, и один из них — не человек. Без этого
	// проверять нечего: она и написана про такую организацию.
	var team struct {
		Members []struct {
			Role string `json:"role"`
			Kind string `json:"kind"`
		} `json:"members"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/team", nil, http.StatusOK), &team); err != nil {
		t.Fatal(err)
	}
	service := 0
	for _, m := range team.Members {
		if m.Kind == "service" && m.Role == "owner" {
			service++
		}
	}
	if service != 1 {
		t.Fatalf("в составе %d служебных владельцев, ожидался один", service)
	}

	// Дверь первая и вторая: снять и разжаловать руками.
	if code, body := owner.do("DELETE", "/api/members/"+who.ID, nil); code != http.StatusConflict {
		t.Errorf("владелец снял сам себя: %d %s", code, body)
	}
	if code, body := owner.do("PUT", "/api/members/"+who.ID+"/role",
		map[string]any{"role": "member"}); code != http.StatusConflict {
		t.Errorf("владелец разжаловал сам себя: %d %s", code, body)
	}

	// Дверь третья и четвёртая: каталогом. Провайдеры шлют и отключение,
	// и удаление — считать их разными действиями значит зависеть
	// от настроек чужой системы, и отвечать на них надо одинаково.
	if code, body := dir.do("PATCH", "/scim/v2/Users/"+who.ID, map[string]any{
		"Operations": []map[string]any{
			{"op": "replace", "path": "active", "value": false},
		},
	}); code != http.StatusConflict {
		t.Errorf("каталог отключил владельца: %d %s", code, body)
	}
	if code, body := dir.do("DELETE", "/scim/v2/Users/"+who.ID, nil); code != http.StatusConflict {
		t.Errorf("каталог удалил владельца: %d %s", code, body)
	}

	// Организация цела, и владелец на месте — это и есть то, ради чего
	// все четыре отказа.
	owner.mustDo("GET", "/api/me", nil, http.StatusOK)

	// А когда владелец-человек не один, всё это разрешено: правило
	// про последнего, а не про владельцев вообще.
	second := dir.must("POST", "/scim/v2/Users",
		newUserPayload("second@example.test"), http.StatusCreated)
	secondID := idOf(t, second)
	owner.mustDo("PUT", "/api/members/"+secondID+"/role",
		map[string]any{"role": "owner"}, http.StatusNoContent)
	owner.mustDo("DELETE", "/api/members/"+who.ID, nil, http.StatusNoContent)
}
