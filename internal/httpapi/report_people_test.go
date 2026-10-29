package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Люди в отчёте (ROADMAP 37.1–37.2): выбрали подразделение или доску —
// список исполнителей сужается до их людей; разрез по людям считает
// карточки каждого, и карточка на двоих — у обоих.
func TestReportPeopleNarrowToChosenTeamAndBoard(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отчёт по людям")
	boris := owner.join("member")
	vera := owner.join("member")
	gleb := owner.join("member")

	dev := owner.team("Разработка", nil)
	core := owner.team("Ядро", dev)
	owner.mustDo("PUT", "/api/teams/"+dev+"/members/"+boris.userID, map[string]any{}, http.StatusNoContent)
	owner.mustDo("PUT", "/api/teams/"+core+"/members/"+vera.userID, map[string]any{}, http.StatusNoContent)

	devBoard := owner.board("Разработка")
	owner.mustDo("PUT", "/api/boards/"+devBoard+"/access",
		map[string]any{"visibility": "team", "teamId": dev}, http.StatusNoContent)
	other := owner.board("Склад")

	pair := owner.cardOn(devBoard, "Вдвоём")
	owner.op(devBoard, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": pair, "userId": boris.userID})
	owner.op(devBoard, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": pair, "userId": owner.userID})
	owner.cardOn(devBoard, "Ничья")
	glebs := owner.cardOn(other, "Глеба")
	owner.op(other, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": glebs, "userId": gleb.userID})

	day := time.Now().Format(time.DateOnly)
	period := "/api/reports/people?from=" + time.Now().AddDate(0, 0, -7).Format(time.DateOnly) + "&to=" + day
	type answer struct {
		Candidates []struct{ ID string }
	}
	get := func(q string) answer {
		t.Helper()
		var out answer
		if err := json.Unmarshal(owner.mustDo("GET", period+q, nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	// Разрез по людям — в сводке: экран берёт её целиком, и ещё раз
	// тот же разрез люди не считают.
	type personRow struct {
		UserID *string
		Cards  int
	}
	rowsOf := func(q string) []personRow {
		t.Helper()
		var out struct{ People []personRow }
		raw := owner.mustDo("GET", "/api/reports/summary?from="+
			time.Now().AddDate(0, 0, -7).Format(time.DateOnly)+"&to="+day+q, nil, http.StatusOK)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out.People
	}
	ids := func(x answer) []string {
		out := []string{}
		for _, c := range x.Candidates {
			out = append(out, c.ID)
		}
		return out
	}

	// Ничего не выбрано — вся организация.
	all := ids(get(""))
	for _, who := range []string{owner.userID, boris.userID, vera.userID, gleb.userID} {
		if !slices.Contains(all, who) {
			t.Errorf("без отбора в списке нет %s: %v", who, all)
		}
	}

	// Подразделение — его люди и люди вложенных, без чужих.
	team := ids(get("&team=" + dev))
	if !slices.Contains(team, boris.userID) || !slices.Contains(team, vera.userID) {
		t.Errorf("в списке подразделения нет его людей (Вера — во вложенном): %v", team)
	}
	if slices.Contains(team, gleb.userID) {
		t.Errorf("в списке подразделения посторонний: %v", team)
	}

	// Доска подразделения — те его люди, кто доску видит: Вера во
	// вложенном её не видит, и назначить её туда нельзя.
	if got := ids(get("&board=" + devBoard)); !slices.Contains(got, boris.userID) || slices.Contains(got, vera.userID) {
		t.Errorf("список доски подразделения: %v — ждали Бориса и не Веру", got)
	}

	// Общая доска — исполнители на ней.
	board := get("&board=" + other)
	if got := ids(board); !slices.Contains(got, gleb.userID) || slices.Contains(got, boris.userID) {
		t.Errorf("список доски «Склад»: %v — ждали Глеба и не Бориса", got)
	}

	// Разрез: карточка на двоих — у каждого, ничья — строкой без имени.
	rows := rowsOf("&board=" + devBoard)
	count := map[string]int{}
	for _, r := range rows {
		key := ""
		if r.UserID != nil {
			key = *r.UserID
		}
		count[key] = r.Cards
	}
	if count[boris.userID] != 1 || count[owner.userID] != 1 || count[""] != 1 {
		t.Errorf("разрез по людям доски: %v", count)
	}

	// В файле тот же разрез — в сводке.
	var file struct {
		Summary struct {
			People []struct{ UserID *string }
		}
	}
	raw := owner.mustDo("GET", "/api/reports/cards?format=json&from="+
		time.Now().AddDate(0, 0, -7).Format(time.DateOnly)+"&to="+day+"&board="+devBoard, nil, http.StatusOK)
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Summary.People) != 3 {
		t.Errorf("в сводке файла %d строк по людям, ожидалось три (Борис, владелец, без исполнителя)", len(file.Summary.People))
	}

	// Выбран исполнитель — строки только его, а в списке он есть,
	// даже если не из выбранного подразделения.
	one := get("&team=" + core + "&assignee=" + boris.userID)
	if !slices.Contains(ids(one), boris.userID) {
		t.Errorf("выбранный исполнитель пропал из списка: %v", ids(one))
	}
	for _, r := range rowsOf("&assignee=" + boris.userID) {
		if r.UserID == nil || *r.UserID != boris.userID {
			t.Errorf("при выбранном Борисе в разрезе чужая строка: %+v", r)
		}
	}
}

// Карточка с исполнителем — одной операцией (ROADMAP 37.4), и с теми же
// проверками, что у назначения: кто не видит доску, назначен не будет,
// и тогда не заводится и сама карточка.
func TestCardIsCreatedWithAssignees(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Сразу с исполнителем")
	boris := owner.join("member")
	boardID := owner.board("Поставки")
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap); err != nil {
		t.Fatal(err)
	}
	raw := owner.op(boardID, uuid.NewString(), "CREATE_CARD", map[string]any{
		"columnId": snap.Columns[0].ID, "title": "Сверить остатки", "assigneeIds": []string{boris.userID}})
	var res struct {
		Patch struct {
			Cards         []struct{ ID string }
			CardAssignees map[string][]string `json:"cardAssignees"`
		}
	}
	if err := json.Unmarshal(raw, &res); err != nil || len(res.Patch.Cards) != 1 {
		t.Fatalf("карточка не завелась: %s", raw)
	}
	id := res.Patch.Cards[0].ID
	if got := res.Patch.CardAssignees[id]; len(got) != 1 || got[0] != boris.userID {
		t.Errorf("патч не несёт исполнителя: %s", raw)
	}

	// Назначенный узнаёт — как при обычном назначении.
	notes := boris.mustDo("GET", "/api/notifications", nil, http.StatusOK)
	if !strings.Contains(string(notes), id) {
		t.Errorf("назначенному при заведении не пришло известие: %s", notes)
	}

	// Посторонний — отказ, и карточки нет.
	stranger := a.registerOrg("Чужие")
	code, body := owner.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(), "type": "CREATE_CARD", "payload": map[string]any{
			"columnId": snap.Columns[0].ID, "title": "Не должна завестись", "assigneeIds": []string{stranger.userID}}})
	if code != http.StatusBadRequest {
		t.Errorf("исполнитель не из организации: код %d, %s", code, body)
	}
	if strings.Contains(string(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)), "Не должна завестись") {
		t.Error("карточка завелась без исполнителя, которому отказали")
	}
}

// Сводка на экране и в файле — одно и то же (ROADMAP 42.3, срез 1):
// ответ /reports/summary побайтово равен полю summary выгрузки JSON
// под тем же отбором. Разойдутся — тимлид покажет руководству одни
// числа, а в присланном файле окажутся другие.
func TestReportSummaryIsTheFilesSummary(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Сводка на экране")
	boris := owner.join("member")
	board := owner.board("Поставки")
	done := owner.cardOn(board, "Сделанная")
	owner.op(board, uuid.NewString(), "ASSIGN_CARD", map[string]any{"cardId": done, "userId": boris.userID})
	owner.cardOn(board, "В очереди")

	day := time.Now().Format(time.DateOnly)
	q := "from=" + time.Now().AddDate(0, 0, -7).Format(time.DateOnly) + "&to=" + day + "&board=" + board
	screen := owner.mustDo("GET", "/api/reports/summary?"+q, nil, http.StatusOK)
	var file struct {
		Summary json.RawMessage `json:"summary"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/reports/cards?format=json&"+q, nil, http.StatusOK), &file); err != nil {
		t.Fatal(err)
	}
	var onScreen, inFile map[string]any
	if err := json.Unmarshal(screen, &onScreen); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(file.Summary, &inFile); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(onScreen, inFile) {
		t.Errorf("сводка экрана и файла разошлись:\nэкран %s\nфайл  %s", screen, file.Summary)
	}
	if onScreen["cards"] != float64(2) || len(onScreen["people"].([]any)) != 2 {
		t.Errorf("сводка не о тех карточках: %s", screen)
	}
}
