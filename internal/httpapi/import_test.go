package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Импорт из таблицы (ROADMAP 23.1–23.2). Проверяются обещания этапа:
// предпросмотр ничего не пишет и показывает то же, что потом случится;
// ненайденный человек не выдумывается; повтор не плодит двойников;
// читатель не переносит.

type importAnswer struct {
	Headers      []string `json:"headers"`
	Mapping      []string `json:"mapping"`
	MappingError string   `json:"mappingError"`
	Report       *struct {
		Applied   bool   `json:"applied"`
		BoardID   string `json:"boardId"`
		BoardName string `json:"boardName"`
		NewBoard  bool   `json:"newBoard"`
		Rows      int    `json:"rows"`
		Created   int    `json:"created"`
		Skipped   []struct {
			Row    int
			Number string
		} `json:"skipped"`
		NewColumns []struct{ Name, Kind string } `json:"newColumns"`
		NewLabels  []string                      `json:"newLabels"`
		Missing    []struct {
			Email string `json:"email"`
			Cards int    `json:"cards"`
		} `json:"missingPeople"`
		Problems []struct {
			Row     int    `json:"row"`
			Message string `json:"message"`
		} `json:"problems"`
	} `json:"report"`
}

func (s *session) importTable(body map[string]any, want int) importAnswer {
	s.api.t.Helper()
	var a importAnswer
	raw := s.mustDo("POST", "/api/import/table", body, want)
	if want == http.StatusOK {
		if err := json.Unmarshal(raw, &a); err != nil {
			s.api.t.Fatal(err)
		}
	}
	return a
}

func boardCards(t *testing.T, s *session, boardID string) []map[string]any {
	t.Helper()
	var snap struct {
		Cards     []map[string]any    `json:"cards"`
		Assignees map[string][]string `json:"assignees"`
	}
	if err := json.Unmarshal(s.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap); err != nil {
		t.Fatal(err)
	}
	return snap.Cards
}

func TestTableImportPreviewsThenAppliesThenSkipsRepeats(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Переезд")
	boris := a.member(owner, "member")

	file := "Заголовок;Колонка;Исполнитель;Метки;Оценка;Завершена;Ключ\n" +
		"Сверить остатки;В работе;" + strings.ToUpper(boris.email) + ";склад;3;;OLD-1\n" +
		"Заказать тару;Очередь;nikto@example.test;склад, закупки;;;OLD-2\n" +
		"Отчёт за август;Готово;;;1;05.09.2026;OLD-3\n" +
		";Очередь;;;;;OLD-4\n"
	body := map[string]any{"file": []byte(file), "newBoardName": "Склад"}

	// Предпросмотр: сопоставление предложено само, отчёт полный,
	// а на деле ничего нет — ни доски, ни карточек.
	preview := owner.importTable(body, http.StatusOK)
	if preview.Report == nil || preview.Report.Applied {
		t.Fatalf("предпросмотр: %+v", preview)
	}
	if strings.Join(preview.Mapping, ",") != "title,column,assignees,labels,estimate,done,external" {
		t.Fatalf("предложено сопоставление %q", preview.Mapping)
	}
	r := preview.Report
	if r.Created != 3 || r.Rows != 4 || len(r.Problems) != 1 || r.Problems[0].Row != 5 {
		t.Fatalf("предпросмотр: создано %d из %d, претензии %+v", r.Created, r.Rows, r.Problems)
	}
	if len(r.Missing) != 1 || r.Missing[0].Email != "nikto@example.test" {
		t.Fatalf("ненайденные люди: %+v", r.Missing)
	}
	if strings.Join(r.NewLabels, ",") != "закупки,склад" {
		t.Fatalf("новые метки: %q", r.NewLabels)
	}
	var boards []struct{ Name string }
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards", nil, http.StatusOK), &boards)
	for _, b := range boards {
		if b.Name == "Склад" {
			t.Fatal("предпросмотр завёл доску")
		}
	}

	// Перенос: то же самое, но записано.
	body["apply"] = true
	done := owner.importTable(body, http.StatusOK)
	if !done.Report.Applied || done.Report.Created != 3 || done.Report.BoardID == "" {
		t.Fatalf("перенос: %+v", done.Report)
	}
	boardID := done.Report.BoardID
	kinds := map[string]string{}
	order := []string{}
	for _, c := range done.Report.NewColumns {
		kinds[c.Name] = c.Kind
		order = append(order, c.Name)
	}
	// Колонки встают по смыслу, а не по первой строке файла: там первой
	// шла задача «В работе», но работа не бывает левее очереди.
	if strings.Join(order, ",") != "Очередь,В работе,Готово" {
		t.Fatalf("колонки новой доски в порядке %q", order)
	}
	if kinds["В работе"] != "in_progress" || kinds["Готово"] != "done" || kinds["Очередь"] != "queue" {
		t.Fatalf("колонки новой доски размечены так: %+v", done.Report.NewColumns)
	}
	cards := boardCards(t, owner, boardID)
	if len(cards) != 3 {
		t.Fatalf("на доске %d карточек, ожидалось 3", len(cards))
	}
	for _, c := range cards {
		if c["title"] == "Отчёт за август" {
			if c["outcome"] != "done" || !strings.HasPrefix(c["finishedAt"].(string), "2026-09-05") {
				t.Fatalf("сделанная карточка: %+v", c)
			}
		}
	}
	// Назначение через импорт молчит: Борис не получает известий
	// о задаче, которую вёл и вчера.
	if n := boris.notifications(); len(n.Items) != 0 {
		t.Fatalf("импорт разослал уведомления: %+v", n.Items)
	}

	// Повтор того же файла в ту же доску ничего не заводит.
	delete(body, "newBoardName")
	body["boardId"] = boardID
	again := owner.importTable(body, http.StatusOK)
	if again.Report.Created != 0 || len(again.Report.Skipped) != 3 || again.Report.Skipped[0].Number == "" {
		t.Fatalf("повтор: создано %d, пропущено %+v", again.Report.Created, again.Report.Skipped)
	}
	if len(boardCards(t, owner, boardID)) != 3 {
		t.Fatal("повтор завёл двойников")
	}
}

func TestTableImportIntoExistingBoardAddsMissingColumnsBeforeDone(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Колонки")
	boardID := owner.board("Разработка")
	file := "Title,Status\nWrite spec,Review\n"
	rep := owner.importTable(map[string]any{"file": []byte(file), "boardId": boardID, "apply": true}, http.StatusOK)
	if len(rep.Report.NewColumns) != 1 || rep.Report.NewColumns[0].Name != "Review" {
		t.Fatalf("новые колонки: %+v", rep.Report.NewColumns)
	}
	var snap struct {
		Columns []struct{ Name string } `json:"columns"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap)
	names := []string{}
	for _, c := range snap.Columns {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "Очередь,В работе,Review,Готово" {
		t.Fatalf("колонки доски: %q — новая стадия встаёт перед готовым", names)
	}
}

func TestViewerCannotImportAndMappingErrorExplains(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Читатели")
	gleb := a.member(owner, "viewer")
	gleb.importTable(map[string]any{"file": []byte("Title\nX\n"), "newBoardName": "Z"}, http.StatusForbidden)

	// Нет колонки заголовка — предпросмотр не отказывает, а говорит,
	// чего не хватает, на языке запроса.
	req := a.request("POST", "/api/import/table", map[string]any{"file": []byte("Foo,Bar\n1,2\n"), "newBoardName": "Z"})
	req.Header.Set("Accept-Language", "en")
	resp, err := owner.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var ans importAnswer
	_ = json.NewDecoder(resp.Body).Decode(&ans)
	if ans.Report != nil || !strings.Contains(ans.MappingError, "title") {
		t.Fatalf("без заголовка: %+v", ans)
	}
}
