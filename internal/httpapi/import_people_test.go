package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/importer/pack"
)

// Выбор по людям в предпросмотре переноса (ROADMAP 23.6): сопоставить,
// завести, не переносить. Проверяется, что выбор исполняется и там, где
// у человека нет почты; что заведённый входит по ссылке; что выбор
// помнится на повторе; что заводить людей может только владелец.

type personRow struct {
	Key, Name, Email, Action, UserID, UserName, Origin, Problem string
	Cards                                                       int
}

type peopleAnswer struct {
	Report struct {
		BoardID       string                                   `json:"boardId"`
		People        []personRow                              `json:"people"`
		Members       []struct{ ID, Name, Email string }       `json:"members"`
		CreatedPeople []struct{ ID, Name, Email, Link string } `json:"createdPeople"`
		Comments      int                                      `json:"comments"`
	} `json:"report"`
}

func peoplePackage(t *testing.T, ownerEmail, stranger string) []byte {
	t.Helper()
	b := pack.Board{
		ExternalID: "b-1", Title: "Склад",
		Columns: []pack.Column{{ExternalID: "c-1", Title: "Нужно сделать"}},
		People: []pack.Person{
			{ExternalID: "u-1", Email: strp(ownerEmail), Name: "Владелец"},
			{ExternalID: "u-2", Name: "Иван Петров"},
			{ExternalID: "u-3", Email: strp(stranger), Name: "Пётр Сидоров"},
		},
		Cards: []pack.Card{
			{ExternalID: "t-1", Title: "Первая", Column: "c-1", Assignees: []string{"u-2"},
				Comments: []pack.Comment{{Author: strp("u-2"), At: time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC), Text: "Сверено"}}},
			{ExternalID: "t-2", Title: "Вторая", Column: "c-1", Assignees: []string{"u-3", "u-1"}},
		},
	}
	var buf bytes.Buffer
	if err := pack.Write(&buf, pack.Manifest{CreatedBy: "проверка", Source: pack.Source{System: "yougile"}}, []pack.Board{b}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (s *session) importPeople(body map[string]any, want int) peopleAnswer {
	s.api.t.Helper()
	var ans peopleAnswer
	raw := s.mustDo("POST", "/api/import/package", body, want)
	if want == http.StatusOK {
		if err := json.Unmarshal(raw, &ans); err != nil {
			s.api.t.Fatal(err)
		}
	}
	return ans
}

func byKey(rows []personRow) map[string]personRow {
	out := map[string]personRow{}
	for _, r := range rows {
		out[r.Key] = r
	}
	return out
}

func TestImportPeopleMatchCreateSkip(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Люди переноса")
	stranger := "petr-" + uuid.NewString()[:8] + "@example.test"
	file := peoplePackage(t, owner.email, stranger)

	// Предпросмотр без выбора: владелец найден по почте, остальные — нет.
	preview := owner.importPeople(map[string]any{"file": file, "newBoardName": "Склад"}, http.StatusOK)
	got := byKey(preview.Report.People)
	if p := got[owner.email]; p.Action != "match" || p.Origin != "auto" || p.UserID != owner.userID {
		t.Fatalf("владелец по почте: %+v", p)
	}
	if p := got["source:u-2"]; p.Action != "skip" || p.Name != "Иван Петров" || p.Cards != 1 {
		t.Fatalf("без почты: %+v", p)
	}
	if p := got[stranger]; p.Action != "skip" || p.Origin != "none" {
		t.Fatalf("почта не найдена: %+v", p)
	}
	if len(preview.Report.Members) != 1 {
		t.Fatalf("из кого выбирать: %+v", preview.Report.Members)
	}

	// Иван — это владелец; Пётр — завести.
	choices := map[string]any{
		"source:u-2": map[string]any{"action": "match", "userId": owner.userID},
		stranger:     map[string]any{"action": "create"},
	}
	done := owner.importPeople(map[string]any{"file": file, "newBoardName": "Склад", "apply": true, "people": choices}, http.StatusOK)
	if len(done.Report.CreatedPeople) != 1 || done.Report.CreatedPeople[0].Email != stranger ||
		!strings.Contains(done.Report.CreatedPeople[0].Link, "/password/") {
		t.Fatalf("заведённый и его ссылка: %+v", done.Report.CreatedPeople)
	}
	created := done.Report.CreatedPeople[0]

	var snap struct {
		Cards     []struct{ ID, Title string } `json:"cards"`
		Assignees map[string][]string          `json:"cardAssignees"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+done.Report.BoardID, nil, http.StatusOK), &snap)
	on := map[string][]string{}
	for _, c := range snap.Cards {
		on[c.Title] = snap.Assignees[c.ID]
	}
	if len(on["Первая"]) != 1 || on["Первая"][0] != owner.userID {
		t.Errorf("человек без почты сопоставлен, а исполнителем не стал: %v", on["Первая"])
	}
	if len(on["Вторая"]) != 2 {
		t.Errorf("заведённый не стал исполнителем: %v", on["Вторая"])
	}

	// Реплика Ивана — от сопоставленного, без приписки «из YouGile».
	var comments struct {
		Comments []struct {
			Body     string `json:"body"`
			AuthorID string `json:"authorId"`
		} `json:"comments"`
	}
	for _, c := range snap.Cards {
		if c.Title == "Первая" {
			_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+done.Report.BoardID+"/cards/"+c.ID+"/comments", nil, http.StatusOK), &comments)
		}
	}
	if len(comments.Comments) != 1 || comments.Comments[0].Body != "Сверено" {
		t.Errorf("реплика сопоставленного: %+v", comments.Comments)
	}

	// Заведённый — в «Команде», ещё не входил, и ссылка его впускает.
	team := owner.mustDo("GET", "/api/team", nil, http.StatusOK)
	if !strings.Contains(string(team), stranger) || !strings.Contains(string(team), `"awaitingPassword":true`) {
		t.Errorf("заведённый в «Команде»: %s", team)
	}
	token := created.Link[strings.LastIndex(created.Link, "/")+1:]
	a.session().mustDo("POST", "/api/password-links/use",
		map[string]any{"token": token, "password": "novyy-parol-12345"}, http.StatusOK)

	// Повтор помнит выбор: никого не заводит заново.
	again := owner.importPeople(map[string]any{"file": file, "boardId": done.Report.BoardID}, http.StatusOK)
	got = byKey(again.Report.People)
	if p := got["source:u-2"]; p.Action != "match" || p.Origin != "saved" {
		t.Errorf("выбор по Ивану не запомнен: %+v", p)
	}
	if p := got[stranger]; p.Action != "match" || p.UserID != created.ID {
		t.Errorf("заведённый на повторе: %+v", p)
	}
	if len(again.Report.CreatedPeople) != 0 {
		t.Errorf("повтор завёл людей: %+v", again.Report.CreatedPeople)
	}
}

func TestImportPeopleRefusals(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Отказы людей")
	member := owner.join("member")
	elsewhere := a.registerOrg("Чужая контора")
	file := peoplePackage(t, owner.email, elsewhere.email)

	// Участник переносит, но людей не заводит.
	raw := member.mustDo("POST", "/api/import/package", map[string]any{
		"file": file, "newBoardName": "Склад",
		"people": map[string]any{"source:u-2": map[string]any{"action": "create", "email": "x@example.test"}},
	}, http.StatusForbidden)
	if code, _ := field(t, raw, "code").(string); code != "import_people_owner" {
		t.Errorf("участник заводит людей: код %q", code)
	}
	// А сопоставлять — может.
	member.importPeople(map[string]any{"file": file, "newBoardName": "Склад",
		"people": map[string]any{"source:u-2": map[string]any{"action": "match", "userId": member.userID}}}, http.StatusOK)

	ans := owner.importPeople(map[string]any{"file": file, "newBoardName": "Склад", "people": map[string]any{
		"source:u-2":    map[string]any{"action": "create"},
		elsewhere.email: map[string]any{"action": "create"},
	}}, http.StatusOK)
	got := byKey(ans.Report.People)
	if p := got["source:u-2"]; p.Action != "skip" || !strings.Contains(p.Problem, "нет почты") {
		t.Errorf("завести без почты: %+v", p)
	}
	if p := got[elsewhere.email]; p.Action != "skip" || !strings.Contains(p.Problem, "вне организации") {
		t.Errorf("завести чужого: %+v", p)
	}
	// Выбор участника не из этой организации — не исполняется.
	ans = owner.importPeople(map[string]any{"file": file, "newBoardName": "Склад", "people": map[string]any{
		"source:u-2": map[string]any{"action": "match", "userId": elsewhere.userID},
	}}, http.StatusOK)
	if p := byKey(ans.Report.People)["source:u-2"]; p.Action != "skip" || p.Problem == "" {
		t.Errorf("сопоставление с чужим: %+v", p)
	}
}
