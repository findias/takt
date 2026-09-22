package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
		NewColumns   []struct{ Name, Kind string } `json:"newColumns"`
		NewLabels    []string                      `json:"newLabels"`
		ColumnValues []importValue                 `json:"columnValues"`
		BoardColumns []struct{ ID, Name string }   `json:"boardColumns"`
		Missing      []struct {
			Email string `json:"email"`
			Cards int    `json:"cards"`
			Label string `json:"label"`
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

// Перенос пачкой, а не по карточке. По запросу на номер, карточку,
// событие и доставку пять тысяч строк шли сорок секунд — и столько же
// предпросмотр, на который человек смотрит, выбирая колонки. Порог
// взят с запасом вчетверо от нынешнего и вдесятеро ниже прежнего:
// сорвётся он, только если вставка снова пойдёт по одной.
func TestLargeTableImportsInSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("перенос двух тысяч строк")
	}
	a := newAPI(t)
	owner := a.registerOrg("Большой файл")
	var b strings.Builder
	b.WriteString("Title,Status,Assignee,Labels,Estimate,Created,Key\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "Задача %d,%s,%s,m%d,%d,2026-08-%02d,K-%d\n",
			i, []string{"To Do", "In Progress", "Done"}[i%3], owner.email, i%10, i%5+1, i%28+1, i)
	}
	start := time.Now()
	r := owner.importTable(map[string]any{"file": []byte(b.String()), "newBoardName": "Большой", "apply": true}, http.StatusOK)
	took := time.Since(start)
	if r.Report.Created != 2000 {
		t.Fatalf("заведено %d из 2000", r.Report.Created)
	}
	if took > 4*time.Second {
		t.Fatalf("перенос двух тысяч строк занял %v — вставка снова идёт по одной карточке?", took)
	}
	var snap struct {
		Cards     []struct{ ID string } `json:"cards"`
		Assignees map[string][]string   `json:"assignees"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+r.Report.BoardID, nil, http.StatusOK), &snap)
	if len(snap.Cards) != 2000 {
		t.Fatalf("на доске %d карточек", len(snap.Cards))
	}
}

// Отчёт отделяет перенесённое от прожитого: даты перенесённых взяты
// из другой системы, а начало работы там неизвестно, и смешивать их
// время цикла со своим молча нельзя.
func TestFlowMetricsTellImportedFromLived(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Метрики переезда")
	boardID := owner.board("Поток")
	var snap struct {
		Columns []struct{ ID, Name string } `json:"columns"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap)
	work, done := snap.Columns[1].ID, snap.Columns[len(snap.Columns)-1].ID
	lived := owner.cardOn(boardID, "Прожитая")
	for _, to := range []string{work, done} {
		owner.op(boardID, uuid.NewString(), "MOVE_CARD", map[string]any{"cardId": lived, "toColumnId": to, "place": "end"})
	}

	file := "Заголовок;Колонка;Создана;Завершена\n" +
		"Старая первая;Готово;01.08.2026;20.08.2026\n" +
		"Старая вторая;Готово;05.08.2026;25.08.2026\n"
	owner.importTable(map[string]any{"file": []byte(file), "boardId": boardID, "apply": true}, http.StatusOK)

	type report struct {
		CycleTime *struct {
			Count int `json:"count"`
		} `json:"cycleTime"`
		Finished []struct {
			Title    string `json:"title"`
			Imported bool   `json:"imported"`
		} `json:"finished"`
		Imported        int  `json:"imported"`
		WithoutImported bool `json:"withoutImported"`
	}
	var all, own report
	_ = json.Unmarshal(owner.mustDo("GET", "/api/v1/boards/"+boardID+"/metrics", nil, http.StatusOK), &all)
	if all.CycleTime == nil || all.CycleTime.Count != 3 || all.Imported != 2 || all.WithoutImported {
		t.Fatalf("всё вместе: %+v", all)
	}
	marked := 0
	for _, f := range all.Finished {
		if f.Imported {
			marked++
		}
	}
	if marked != 2 {
		t.Fatalf("помечены %d точек из двух перенесённых: %+v", marked, all.Finished)
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/v1/boards/"+boardID+"/metrics?withoutImported=true", nil, http.StatusOK), &own)
	if own.CycleTime == nil || own.CycleTime.Count != 1 || own.Imported != 2 || !own.WithoutImported {
		t.Fatalf("без перенесённых: %+v", own)
	}
}

// Значения колонки файла ложатся туда, куда сказал человек: «In Review»
// из Jira и наша «В работе» — одно и то же, но по названию этого
// не угадать. Несопоставленное по-прежнему ищется по названию
// или заводится новой колонкой.
func TestColumnValuesGoWhereThePersonSaid(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Значения колонок")
	boardID := owner.board("Разработка")
	var snap struct {
		Columns []struct{ ID, Name string } `json:"columns"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap)
	work := snap.Columns[1].ID

	file := []byte("Title,Status\nWrite spec,In Review\nFix login,In Review\nPlan Q4,Icebox\nShip it,Готово\n")
	body := map[string]any{"file": file, "boardId": boardID}
	preview := owner.importTable(body, http.StatusOK)
	if len(preview.Report.BoardColumns) != 3 {
		t.Fatalf("колонки доски для выбора: %+v", preview.Report.BoardColumns)
	}
	byValue := map[string]importValue{}
	for _, v := range preview.Report.ColumnValues {
		byValue[v.Value] = v
	}
	if v := byValue["In Review"]; !v.New || v.Cards != 2 {
		t.Fatalf("без выбора незнакомое значение — новая колонка: %+v", v)
	}
	if v := byValue["Готово"]; v.New || v.ColumnID != snap.Columns[2].ID {
		t.Fatalf("знакомое по названию — в свою колонку: %+v", v)
	}

	body["columns"] = map[string]string{"in review": work}
	body["apply"] = true
	done := owner.importTable(body, http.StatusOK)
	for _, c := range done.Report.NewColumns {
		if c.Name == "In Review" {
			t.Fatalf("значение, отданное в «В работе», завело свою колонку: %+v", done.Report.NewColumns)
		}
	}
	var after struct {
		Columns []struct{ ID, Name string } `json:"columns"`
		Cards   []struct {
			Title    string `json:"title"`
			ColumnID string `json:"columnId"`
		} `json:"cards"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &after)
	for _, c := range after.Cards {
		if (c.Title == "Write spec" || c.Title == "Fix login") && c.ColumnID != work {
			t.Fatalf("«%s» не в «В работе»", c.Title)
		}
	}
	if len(after.Columns) != 4 {
		t.Fatalf("колонок %d: ожидалась одна новая — Icebox", len(after.Columns))
	}

	// Выбранную колонку убрали, пока человек смотрел, — отказ называет значение.
	body["columns"] = map[string]string{"icebox": uuid.NewString()}
	raw := owner.mustDo("POST", "/api/import/table", body, http.StatusBadRequest)
	if !strings.Contains(string(raw), "Icebox") {
		t.Fatalf("отказ не называет значение: %s", raw)
	}
}

type importValue struct {
	Value    string `json:"value"`
	Cards    int    `json:"cards"`
	ColumnID string `json:"columnId"`
	New      bool   `json:"new"`
}

// Людей заводит администратор, перенос их только находит по почте.
// Заведённого после первого переноса повтор дописывает исполнителем
// в уже переехавшие карточки — карточки при этом не удваиваются.
func TestPersonAddedAfterImportIsAssignedOnRerun(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Люди потом")
	later := "potom-" + uuid.NewString()[:8] + "@example.test"
	file := []byte("Title,Assignee,Key\nСверить остатки," + later + ",K-1\n")
	first := owner.importTable(map[string]any{"file": file, "newBoardName": "Склад", "apply": true}, http.StatusOK)
	if len(first.Report.Missing) != 1 || first.Report.Missing[0].Email != later {
		t.Fatalf("почта не найдена и названа: %+v", first.Report.Missing)
	}
	// Ненайденный не пропадает с карточки: на ней его метка. Имени
	// таблица не дала — меткой служит почта. Метка техническая: в выборе
	// метки её не предлагают.
	if first.Report.Missing[0].Label != later {
		t.Fatalf("метка человека в отчёте: %+v", first.Report.Missing)
	}
	personLabels := func() (hung int, offered bool) {
		var snap struct {
			Labels []struct {
				ID       string `json:"id"`
				Kind     string `json:"kind"`
				Offered  bool   `json:"offered"`
				Archived bool   `json:"archived"`
			} `json:"labels"`
			CardLabels map[string][]string `json:"cardLabels"`
		}
		_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+first.Report.BoardID, nil, http.StatusOK), &snap)
		for _, l := range snap.Labels {
			if l.Kind != "person" {
				continue
			}
			offered = offered || l.Offered
			for _, ids := range snap.CardLabels {
				for _, id := range ids {
					if id == l.ID {
						hung++
					}
				}
			}
		}
		return hung, offered
	}
	if hung, offered := personLabels(); hung != 1 || offered {
		t.Fatalf("метка человека: висит на %d карточках (ожидалась одна), предлагается: %v", hung, offered)
	}

	// Администратор заводит человека.
	inv := owner.mustDo("POST", "/api/invites", map[string]any{"email": later, "role": "member"}, http.StatusCreated)
	link, _ := field(t, inv, "link").(string)
	parts := strings.Split(strings.TrimSuffix(link, "/"), "/")
	person := a.session()
	person.mustDo("POST", "/api/invites/accept", map[string]any{
		"token": parts[len(parts)-1], "name": "Потом", "password": "parol12345",
	}, http.StatusOK)

	var again struct {
		Report struct {
			Created       int   `json:"created"`
			Skipped       []any `json:"skipped"`
			AssignedLater int   `json:"assignedLater"`
			Unlabeled     int   `json:"unlabeled"`
			Missing       []any `json:"missingPeople"`
		} `json:"report"`
	}
	raw := owner.mustDo("POST", "/api/import/table",
		map[string]any{"file": file, "boardId": first.Report.BoardID, "apply": true}, http.StatusOK)
	_ = json.Unmarshal(raw, &again)
	if again.Report.Created != 0 || len(again.Report.Skipped) != 1 || again.Report.AssignedLater != 1 || len(again.Report.Missing) != 0 {
		t.Fatalf("повтор: %+v", again.Report)
	}
	var snap struct {
		Assignees map[string][]string `json:"cardAssignees"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+first.Report.BoardID, nil, http.StatusOK), &snap)
	total := 0
	for _, ids := range snap.Assignees {
		total += len(ids)
	}
	if total != 1 {
		t.Fatalf("исполнителей на доске %d, ожидался один: %v", total, snap.Assignees)
	}
	// Человек нашёлся — замена больше не нужна.
	if hung, _ := personLabels(); hung != 0 || again.Report.Unlabeled != 1 {
		t.Fatalf("метка человека после того, как он нашёлся: висит на %d, снято %d", hung, again.Report.Unlabeled)
	}
}
