package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/findias/takt/internal/importer/pack"
)

// Пакет переноса через экран (docs/import-package.md): то, что таблица
// теряет, — подзадачи, связи, обсуждение — переезжает; автор, которого
// в организации нет, назван в реплике, а не выдуман; испорченный пакет
// отвергается кодом.

func strp(s string) *string { return &s }

func samplePackage(t *testing.T, ownerEmail string) []byte {
	t.Helper()
	done := "done"
	b := pack.Board{
		ExternalID: "b-1", Title: "Склад",
		Columns: []pack.Column{{ExternalID: "c-1", Title: "Нужно сделать"}, {ExternalID: "c-2", Title: "Сделано", Kind: &done}},
		People: []pack.Person{
			{ExternalID: "u-1", Email: strp(strings.ToUpper(ownerEmail)), Name: "Владелец"},
			{ExternalID: "u-2", Name: "Иван Петров"},
		},
		Cards: []pack.Card{
			{ExternalID: "t-1", Title: "Сверить остатки", Column: "c-1", Assignees: []string{"u-1", "u-2"},
				Links: []pack.Link{{Kind: "relates", To: "t-3"}},
				Comments: []pack.Comment{
					{Author: strp("u-1"), At: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC), Text: "Беру"},
					{Author: strp("u-2"), At: time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC), Text: "Ряд первый сверен"},
				}},
			{ExternalID: "t-2", Title: "Выгрузить остатки", Column: "c-1", Parent: strp("t-1")},
			{ExternalID: "t-3", Title: "Отчёт", Column: "c-2", FinishedAt: ptrTime(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))},
		},
	}
	var buf bytes.Buffer
	if err := pack.Write(&buf, pack.Manifest{CreatedBy: "проверка", Source: pack.Source{System: "yougile"}}, []pack.Board{b, {Title: "Пустая"}}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestPackageMovesInPartsLinksAndDiscussion(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Из пакета")
	raw := samplePackage(t, owner.email)

	type answer struct {
		Package struct {
			Source string `json:"source"`
			Boards []struct {
				Title string `json:"title"`
				Cards int    `json:"cards"`
			} `json:"boards"`
		} `json:"package"`
		Report struct {
			Applied  bool   `json:"applied"`
			BoardID  string `json:"boardId"`
			Created  int    `json:"created"`
			Parts    int    `json:"parts"`
			Links    int    `json:"links"`
			Comments int    `json:"comments"`
			Skipped  []any  `json:"skipped"`
			Missing  []struct {
				Email string `json:"email"`
				Name  string `json:"name"`
			} `json:"missingPeople"`
			NewColumns []struct{ Name, Kind string } `json:"newColumns"`
		} `json:"report"`
	}
	call := func(body map[string]any) answer {
		var ans answer
		if err := json.Unmarshal(owner.mustDo("POST", "/api/import/package", body, http.StatusOK), &ans); err != nil {
			t.Fatal(err)
		}
		return ans
	}

	body := map[string]any{"file": raw, "newBoardName": "Склад"}
	preview := call(body)
	if preview.Package.Source != "YouGile" || len(preview.Package.Boards) != 2 {
		t.Fatalf("пакет: %+v", preview.Package)
	}
	r := preview.Report
	if r.Applied || r.Created != 3 || r.Parts != 1 || r.Links != 1 || r.Comments != 2 {
		t.Fatalf("предпросмотр: %+v", r)
	}
	if len(r.Missing) != 1 || r.Missing[0].Name != "Иван Петров" {
		t.Fatalf("человек без почты назван по имени: %+v", r.Missing)
	}
	// Колонки — в порядке источника и с его разметкой: «Сделано» не
	// в словаре, но источник назвал её готовой.
	if len(r.NewColumns) != 2 || r.NewColumns[1].Kind != "done" {
		t.Fatalf("колонки: %+v", r.NewColumns)
	}

	body["apply"] = true
	done := call(body).Report
	var snap struct {
		Cards []struct{ ID, Title string } `json:"cards"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+done.BoardID, nil, http.StatusOK), &snap)
	ids := map[string]string{}
	for _, c := range snap.Cards {
		ids[c.Title] = c.ID
	}
	var detail struct {
		Links []struct{ FromCard, ToCard, Kind string } `json:"links"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+done.BoardID+"/cards/"+ids["Сверить остатки"], nil, http.StatusOK), &detail)
	kinds := map[string]string{}
	for _, l := range detail.Links {
		kinds[l.Kind] = l.ToCard
	}
	if kinds["subtask"] != ids["Выгрузить остатки"] || kinds["relates"] != ids["Отчёт"] {
		t.Fatalf("связи: %+v, карточки %v", detail.Links, ids)
	}
	var comments struct {
		Comments []struct {
			Body string `json:"body"`
		} `json:"comments"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+done.BoardID+"/cards/"+ids["Сверить остатки"]+"/comments", nil, http.StatusOK), &comments)
	bodies := []string{}
	for _, c := range comments.Comments {
		bodies = append(bodies, c.Body)
	}
	joined := strings.Join(bodies, " | ")
	if !strings.Contains(joined, "Беру") || !strings.Contains(joined, "из YouGile: Иван Петров\n\nРяд первый сверен") {
		t.Fatalf("обсуждение: %q", joined)
	}

	// Повтор: всё уже здесь, связи и реплики не удваиваются.
	delete(body, "newBoardName")
	body["boardId"] = done.BoardID
	again := call(body).Report
	if again.Created != 0 || len(again.Skipped) != 3 || again.Comments != 0 {
		t.Fatalf("повтор: %+v", again)
	}

	// Не пакет — отказ кодом, с объяснением. Подмену части внутри
	// пакета ловит сумма; это проверено в internal/importer/pack.
	answerRaw := owner.mustDo("POST", "/api/import/package", map[string]any{"file": []byte("не пакет"), "newBoardName": "Z"}, http.StatusBadRequest)
	if field(t, answerRaw, "code") != "import_package" {
		t.Fatalf("испорченный пакет: %s", answerRaw)
	}
}
