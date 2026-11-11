package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/findias/takt/internal/board"
	"github.com/google/uuid"
)

func TestVacationsAndConfirmedCoverage(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отпуска")
	member := owner.join("member")
	viewer := owner.join("viewer")
	b := owner.board("Работа")
	card := owner.cardOn(b, "Поставка")
	owner.op(b, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": card, "userId": owner.userID})
	var initial board.Availability
	json.Unmarshal(owner.mustDo("GET", "/api/availability", nil, 200), &initial)
	start := initial.Today
	d, _ := time.Parse(time.DateOnly, start)
	end := d.AddDate(0, 0, 6).Format(time.DateOnly)
	id := uuid.NewString()
	v := board.Vacation{StartsOn: start, EndsOn: end, Note: "Передача"}
	raw := owner.mustDo("PUT", "/api/vacations/"+id, v, 200)
	json.Unmarshal(raw, &v)
	again := owner.mustDo("PUT", "/api/vacations/"+id, board.Vacation{StartsOn: start, EndsOn: end, Note: "Передача"}, 200)
	if string(raw) != string(again) {
		t.Fatal("повтор изменил отпуск")
	}
	code, body := owner.do("PUT", "/api/vacations/"+uuid.NewString(), board.Vacation{StartsOn: start, EndsOn: end})
	if code != 409 || !strings.Contains(string(body), "vacation_overlap") {
		t.Fatalf("пересечение: %d %s", code, body)
	}
	if code, _ := member.do("PUT", "/api/vacations/"+id, v); code != 403 {
		t.Fatalf("чужой отпуск: %d", code)
	}
	viewer.mustDo("PUT", "/api/vacations/"+uuid.NewString(), board.Vacation{StartsOn: start, EndsOn: end}, 200)
	stranger := a.registerOrg("Другая")
	var hidden board.Availability
	json.Unmarshal(stranger.mustDo("GET", "/api/availability", nil, 200), &hidden)
	if len(hidden.Vacations) != 0 || len(hidden.Coverages) != 0 {
		t.Fatal("отпуска другой организации видны")
	}
	cv := board.Coverage{VacationID: id, VacationVersion: v.Version, CardID: card, SubstituteID: member.userID, StartsOn: start, EndsOn: end}
	cid := uuid.NewString()
	raw = owner.mustDo("PUT", "/api/coverages/"+cid, cv, 200)
	json.Unmarshal(raw, &cv)
	owner.mustDo("PUT", "/api/coverages/"+cid, cv, 200)
	if cv.Status != "proposed" || !cv.Valid {
		t.Fatalf("предложение: %+v", cv)
	}
	if code, _ := owner.do("PATCH", "/api/coverages/"+cid, map[string]any{"status": "accepted", "version": 1}); code != 403 {
		t.Fatalf("принял за другого: %d", code)
	}
	member.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "accepted", "version": 1}, 200)
	owner.op(b, uuid.NewString(), "SET_CARD_PLAN", map[string]any{"cardId": card, "plannedStartOn": start, "plannedEndOn": end, "expected": map[string]any{"plannedStartOn": nil, "plannedEndOn": nil}})
	// Подтверждение не меняет основных исполнителей, колонку и факты.
	snap := owner.mustDo("GET", "/api/boards/"+b, nil, 200)
	if !strings.Contains(string(snap), owner.userID) {
		t.Fatal("исполнитель потерян")
	}
	var list board.TaskList
	json.Unmarshal(member.mustDo("GET", "/api/tasks?covering=1", nil, 200), &list)
	if len(list.Tasks) != 1 || list.Tasks[0].ID != card {
		t.Fatalf("замещаемые: %+v", list)
	}
	task := list.Tasks[0]
	if task.PlannedStartOn == nil || *task.PlannedStartOn != start || task.PlannedEndOn == nil || *task.PlannedEndOn != end || len(task.AssigneeIDs) != 1 || task.AssigneeIDs[0] != owner.userID {
		t.Fatalf("нет дат или исполнителей для предупреждения об отпуске: %+v", task)
	}
	member.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "accepted", "version": 1}, 200)
	// Отпуск заместителя отменяет обещание доступности без смены задачи.
	subVacation := uuid.NewString()
	member.mustDo("PUT", "/api/vacations/"+subVacation, board.Vacation{StartsOn: start, EndsOn: start}, 200)
	var data board.Availability
	json.Unmarshal(owner.mustDo("GET", "/api/availability", nil, 200), &data)
	if len(data.Coverages) != 1 || data.Coverages[0].Valid {
		t.Fatalf("конфликт отпуска заместителя: %+v", data)
	}
	json.Unmarshal(member.mustDo("GET", "/api/tasks?covering=1", nil, 200), &list)
	if len(list.Tasks) != 0 {
		t.Fatal("недействительное замещение в работе")
	}
	// Изменение дат не сохраняет прежнее согласие на новые даты.
	v.EndsOn = d.AddDate(0, 0, 8).Format(time.DateOnly)
	owner.mustDo("PUT", "/api/vacations/"+id, v, 200)
	code, body = owner.do("PUT", "/api/vacations/"+id, board.Vacation{StartsOn: start, EndsOn: end, Version: 1})
	if code != 409 || !strings.Contains(string(body), "availability_changed") {
		t.Fatalf("устаревшая правка: %d %s", code, body)
	}
	json.Unmarshal(owner.mustDo("GET", "/api/availability", nil, 200), &data)
	if data.Coverages[0].Valid {
		t.Fatal("старое согласие действует на новый отпуск")
	}
	invalid := board.Coverage{VacationID: id, VacationVersion: 1, CardID: card, SubstituteID: member.userID, StartsOn: start, EndsOn: end}
	code, body = owner.do("PUT", "/api/coverages/"+uuid.NewString(), invalid)
	if code != 409 || !strings.Contains(string(body), "coverage_invalid") {
		t.Fatalf("устаревшее замещение: %d %s", code, body)
	}
	// Ключи не читают календарь людей.
	owner.mustDo("PUT", "/api/availability/timezone", map[string]string{"timeZone": "Europe/Moscow"}, 204)
	if code, _ := member.do("PUT", "/api/availability/timezone", map[string]string{"timeZone": "UTC"}); code != http.StatusForbidden {
		t.Fatalf("часовой пояс: %d", code)
	}
	_, token := owner.apiClient("Интеграция", "boards:read", "boards:write")
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/availability"},
		{"PUT", "/api/availability/timezone"},
		{"PUT", "/api/vacations/" + id},
		{"PUT", "/api/coverages/" + cid},
		{"PATCH", "/api/coverages/" + cid},
	} {
		if code, body := a.withToken(token, route.method, route.path, map[string]any{}); code != http.StatusForbidden {
			t.Fatalf("личный маршрут ключом %s %s: %d %s", route.method, route.path, code, body)
		}
	}
}

func TestFutureAndCancelledCoverageDoNotEnterCurrentTasks(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Будущая передача")
	sub := owner.join("member")
	b := owner.board("План")
	card := owner.cardOn(b, "Передать документы")
	owner.op(b, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": card, "userId": owner.userID})
	var clock board.Availability
	json.Unmarshal(owner.mustDo("GET", "/api/availability", nil, 200), &clock)
	today, _ := time.Parse(time.DateOnly, clock.Today)
	start, end := today.AddDate(0, 0, 1).Format(time.DateOnly), today.AddDate(0, 0, 7).Format(time.DateOnly)
	vid := uuid.NewString()
	var vacation board.Vacation
	json.Unmarshal(owner.mustDo("PUT", "/api/vacations/"+vid, board.Vacation{StartsOn: start, EndsOn: end}, 200), &vacation)
	cid := uuid.NewString()
	owner.mustDo("PUT", "/api/coverages/"+cid, board.Coverage{VacationID: vid, VacationVersion: vacation.Version, CardID: card, SubstituteID: sub.userID, StartsOn: start, EndsOn: end}, 200)
	sub.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "accepted", "version": 1}, 200)
	var tasks board.TaskList
	json.Unmarshal(sub.mustDo("GET", "/api/tasks?covering=1", nil, 200), &tasks)
	if len(tasks.Tasks) != 0 {
		t.Fatal("будущее замещение уже в текущей работе")
	}
	var data board.Availability
	json.Unmarshal(sub.mustDo("GET", "/api/availability", nil, 200), &data)
	if len(data.Coverages) != 1 || !data.Coverages[0].Valid {
		t.Fatal("будущая договорённость потеряна")
	}
	// Отмена и возврат отпуска требуют нового согласия, прежнее не оживает.
	vacation.Cancelled = true
	json.Unmarshal(owner.mustDo("PUT", "/api/vacations/"+vid, vacation, 200), &vacation)
	vacation.Cancelled = false
	owner.mustDo("PUT", "/api/vacations/"+vid, vacation, 200)
	json.Unmarshal(sub.mustDo("GET", "/api/availability", nil, 200), &data)
	if len(data.Coverages) != 1 || data.Coverages[0].Valid {
		t.Fatal("возврат отпуска восстановил старое согласие")
	}
	// Согласившийся вправе отозвать договорённость; устаревшая отмена
	// не должна перезаписать ответ другой вкладки.
	sub.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "cancelled", "version": 1}, 409)
	sub.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "cancelled", "version": 2}, 200)
	sub.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "accepted", "version": 3}, 409)
}

func TestCoverageRespectsPrivateBoardsAndCurrentAccess(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Закрытая работа")
	sub := owner.join("member")
	other := owner.join("member")
	viewer := owner.join("viewer")
	b := owner.board("Секретная доска")
	card := owner.cardOn(b, "Закрытая поставка")
	owner.op(b, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": card, "userId": owner.userID})
	owner.mustDo("PUT", "/api/boards/"+b+"/access", map[string]any{"visibility": "private"}, 204)
	owner.mustDo("PUT", "/api/boards/"+b+"/members/"+sub.userID, nil, 204)
	var clock board.Availability
	json.Unmarshal(owner.mustDo("GET", "/api/availability", nil, 200), &clock)
	v := board.Vacation{StartsOn: clock.Today, EndsOn: clock.Today}
	var saved board.Vacation
	vid := uuid.NewString()
	json.Unmarshal(owner.mustDo("PUT", "/api/vacations/"+vid, v, 200), &saved)
	coverage := board.Coverage{VacationID: vid, VacationVersion: saved.Version, CardID: card, SubstituteID: sub.userID, StartsOn: clock.Today, EndsOn: clock.Today}
	cid := uuid.NewString()
	owner.mustDo("PUT", "/api/coverages/"+cid, coverage, 200)
	var data board.Availability
	raw := other.mustDo("GET", "/api/availability", nil, 200)
	json.Unmarshal(raw, &data)
	if len(data.Coverages) != 0 || strings.Contains(string(raw), "Закрытая поставка") {
		t.Fatal("название закрытой задачи видно постороннему")
	}
	if len(data.Vacations) != 1 {
		t.Fatal("календарь отпусков должен быть виден коллегам")
	}
	sub.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "accepted", "version": 1}, 200)
	// Повтор не создаёт второго уведомления.
	before := owner.mustDo("GET", "/api/notifications", nil, 200)
	sub.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "accepted", "version": 1}, 200)
	after := owner.mustDo("GET", "/api/notifications", nil, 200)
	if string(before) != string(after) {
		t.Fatal("повтор согласия продублировал уведомления")
	}
	owner.mustDo("DELETE", "/api/boards/"+b+"/members/"+sub.userID, nil, 204)
	json.Unmarshal(owner.mustDo("GET", "/api/availability", nil, 200), &data)
	if len(data.Coverages) != 1 || data.Coverages[0].Valid {
		t.Fatal("потерявший доступ всё ещё замещает")
	}
	json.Unmarshal(sub.mustDo("GET", "/api/availability", nil, 200), &data)
	if len(data.Coverages) != 0 {
		t.Fatal("после потери доступа остались сведения о задаче")
	}
	// Наблюдатель может сообщать свой отпуск, но запись ему не выдаётся
	// ни предложением замещения, ни прямым ответом по известному UUID.
	coverage.SubstituteID = viewer.userID
	code, body := owner.do("PUT", "/api/coverages/"+uuid.NewString(), coverage)
	if code != 409 || !strings.Contains(string(body), "coverage_invalid") {
		t.Fatalf("наблюдатель-заместитель: %d %s", code, body)
	}
	owner.mustDo("PATCH", "/api/coverages/"+cid, map[string]any{"status": "cancelled", "version": 2}, 200)
}

func TestSimultaneousVacationsCannotOverlap(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Одновременные даты")
	result := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			code, _ := owner.do("PUT", "/api/vacations/"+uuid.NewString(), board.Vacation{StartsOn: "2026-12-01", EndsOn: "2026-12-10"})
			result <- code
		}()
	}
	one, two := <-result, <-result
	if !((one == 200 && two == 409) || (one == 409 && two == 200)) {
		t.Fatalf("одновременные отпуска: %d %d", one, two)
	}
}
