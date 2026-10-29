package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type snapCard struct {
	ID       string
	Percent  *int
	Progress *struct{ Done, Total float64 }
}

func (s *session) snapCards(boardID string) map[string]snapCard {
	s.api.t.Helper()
	var snap struct{ Cards []snapCard }
	if err := json.Unmarshal(s.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap); err != nil {
		s.api.t.Fatal(err)
	}
	out := map[string]snapCard{}
	for _, c := range snap.Cards {
		out[c.ID] = c
	}
	return out
}

// Процент выполнения руками (ROADMAP 38): ставится, снимается, входит
// в счёт родителя долей и остаётся в истории числом.
func TestCardPercentIsSetByHandAndCountsForTheParent(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Процент")
	boardID := owner.board("Поставки")
	parent := owner.cardOn(boardID, "Выпустить релиз")
	var parts []string
	for _, title := range []string{"Смета", "Договор"} {
		raw := owner.op(boardID, uuid.NewString(), "CREATE_SUBTASK", map[string]any{"parentCardId": parent, "title": title})
		var res struct {
			Patch struct{ Cards []struct{ ID, Title string } }
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Patch.Cards {
			if c.Title == title {
				parts = append(parts, c.ID)
			}
		}
	}
	if len(parts) != 2 {
		t.Fatalf("подзадачи не завелись: %v", parts)
	}

	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": parts[0], "percent": 40})
	cards := owner.snapCards(boardID)
	if p := cards[parts[0]].Percent; p == nil || *p != 40 {
		t.Fatalf("процент части не записан: %+v", cards[parts[0]])
	}
	// Часть на 40% — 0,4 части родителю, а не ноль.
	if pr := cards[parent].Progress; pr == nil || pr.Total != 2 || pr.Done < 0.39 || pr.Done > 0.41 {
		t.Errorf("счёт родителя не учёл процент части: %+v", cards[parent].Progress)
	}

	// Ручной процент у родителя — свой, счёт по частям при этом прежний.
	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": parent, "percent": 75})
	if p := owner.snapCards(boardID)[parent].Percent; p == nil || *p != 75 {
		t.Errorf("ручной процент родителя не записан")
	}

	// null — снова «считать само».
	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": parts[0], "percent": nil})
	cards = owner.snapCards(boardID)
	if cards[parts[0]].Percent != nil || cards[parent].Progress.Done != 0 {
		t.Errorf("сброс процента не вернул счёт: часть %+v, родитель %+v", cards[parts[0]], cards[parent].Progress)
	}

	// За пределами 0–100 — отказ с объяснением.
	code, body := owner.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(), "type": "SET_CARD_PERCENT",
		"payload": map[string]any{"cardId": parts[1], "percent": 120}})
	if code != http.StatusBadRequest || !strings.Contains(string(body), "от 0 до 100") {
		t.Errorf("процент 120: код %d, %s", code, body)
	}

	// В истории — числом.
	events := owner.mustDo("GET", "/api/boards/"+boardID+"/events?cardId="+parts[0], nil, http.StatusOK)
	if !strings.Contains(string(events), `"percent_set"`) || !strings.Contains(string(events), `"percent":40`) {
		t.Errorf("в истории нет процента числом: %s", events)
	}
}

// Единица оценки доски (ROADMAP 38): своя или, с null, как у организации.
func TestBoardHasItsOwnEstimateUnit(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Единицы")
	viewer := owner.join("viewer")
	boardID := owner.board("Сопровождение")
	unit := func() (string, *string) {
		t.Helper()
		var snap struct {
			Board struct {
				EstimateUnit    string
				OwnEstimateUnit *string
			}
		}
		if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap); err != nil {
			t.Fatal(err)
		}
		return snap.Board.EstimateUnit, snap.Board.OwnEstimateUnit
	}

	if eff, own := unit(); eff != "points" || own != nil {
		t.Fatalf("новая доска: %q / %v, ожидалась единица организации без своей", eff, own)
	}
	owner.mustDo("PUT", "/api/boards/"+boardID+"/estimate-unit", map[string]any{"unit": "hours"}, http.StatusNoContent)
	if eff, own := unit(); eff != "hours" || own == nil || *own != "hours" {
		t.Errorf("своя единица не встала: %q / %v", eff, own)
	}
	// Организация сменила свою — доска со своей единицей её не меняет.
	owner.mustDo("PUT", "/api/org/estimate-unit", map[string]any{"unit": "days"}, http.StatusOK)
	if eff, _ := unit(); eff != "hours" {
		t.Errorf("своя единица доски сменилась вслед за организацией: %q", eff)
	}
	// null — снова как у организации.
	owner.mustDo("PUT", "/api/boards/"+boardID+"/estimate-unit", map[string]any{"unit": nil}, http.StatusNoContent)
	if eff, own := unit(); eff != "days" || own != nil {
		t.Errorf("сброс не вернул единицу организации: %q / %v", eff, own)
	}

	owner.mustDo("PUT", "/api/boards/"+boardID+"/estimate-unit", map[string]any{"unit": "parrots"}, http.StatusBadRequest)
	viewer.mustDo("PUT", "/api/boards/"+boardID+"/estimate-unit", map[string]any{"unit": "hours"}, http.StatusForbidden)
}

// Сто процентов и отметка «сделано» — одно и то же (решение владельца
// 29.10.2026): сто руками — отметка; последняя часть сделана — родитель
// отмечен; меньше ста у отмеченной — отметка снята; прошедшая финиш
// выполнена на сто, и другой процент у неё не принимается.
func TestHundredPercentMarksDoneAndDoneIsHundred(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Сто процентов")
	boardID := owner.board("Поставки")
	card := func(id string) struct {
		DoneAt  *string
		Percent *int
		Outcome *string
	} {
		t.Helper()
		var snap struct {
			Cards []struct {
				ID      string
				DoneAt  *string
				Percent *int
				Outcome *string
			}
		}
		if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap); err != nil {
			t.Fatal(err)
		}
		for _, c := range snap.Cards {
			if c.ID == id {
				return struct {
					DoneAt  *string
					Percent *int
					Outcome *string
				}{c.DoneAt, c.Percent, c.Outcome}
			}
		}
		t.Fatalf("карточки %s нет на снимке", id)
		return struct {
			DoneAt  *string
			Percent *int
			Outcome *string
		}{}
	}

	// Сто руками — отмечена сделанной, в истории — с причиной.
	single := owner.cardOn(boardID, "Смета")
	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": single, "percent": 100})
	if card(single).DoneAt == nil {
		t.Fatalf("сто процентов не отметили карточку сделанной")
	}
	events := owner.mustDo("GET", "/api/boards/"+boardID+"/events?cardId="+single, nil, http.StatusOK)
	if !strings.Contains(string(events), `"done"`) || !strings.Contains(string(events), `"percent":100`) {
		t.Errorf("в истории нет отметки со ста процентами: %s", events)
	}
	// Меньше ста у отмеченной — отметка снята.
	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": single, "percent": 60})
	if card(single).DoneAt != nil {
		t.Errorf("процент ниже ста не снял отметку")
	}

	// Последняя часть сделана — родитель отмечен; снимок родителя
	// показывает сто, что бы ни стояло руками.
	parent := owner.cardOn(boardID, "Выпустить релиз")
	var parts []string
	for _, title := range []string{"Договор", "Акт"} {
		raw := owner.op(boardID, uuid.NewString(), "CREATE_SUBTASK", map[string]any{"parentCardId": parent, "title": title})
		var res struct {
			Patch struct{ Cards []struct{ ID, Title string } }
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Patch.Cards {
			if c.Title == title {
				parts = append(parts, c.ID)
			}
		}
	}
	owner.op(boardID, uuid.NewString(), "SET_CARD_DONE", map[string]any{"cardId": parts[0], "done": true})
	if card(parent).DoneAt != nil {
		t.Fatalf("родитель отмечен при одной части из двух")
	}
	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": parts[1], "percent": 100})
	if card(parent).DoneAt == nil {
		t.Errorf("последняя часть на ста не отметила родителя")
	}

	// Прошедшая финиш — сто, и другой процент у неё не принимается.
	finished := owner.cardOn(boardID, "Переезд")
	owner.op(boardID, uuid.NewString(), "SET_CARD_PERCENT", map[string]any{"cardId": finished, "percent": 40})
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap); err != nil {
		t.Fatal(err)
	}
	owner.op(boardID, uuid.NewString(), "MOVE_CARD", map[string]any{
		"cardId": finished, "toColumnId": snap.Columns[len(snap.Columns)-1].ID, "place": "end"})
	code, body := owner.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(), "type": "SET_CARD_PERCENT",
		"payload": map[string]any{"cardId": finished, "percent": 50}})
	if code != http.StatusConflict || !strings.Contains(string(body), "уже сделана") {
		t.Errorf("процент у сделанной: код %d, %s", code, body)
	}
	// В выгрузке у неё 100, а не 40.
	var export struct {
		Cards []struct {
			Title   string `json:"title"`
			Percent *int   `json:"percent"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(owner.mustDo("GET", "/api/reports/cards?format=json&boards="+boardID+
		"&from="+time.Now().AddDate(0, 0, -7).Format(time.DateOnly)+"&to="+time.Now().AddDate(0, 0, 1).Format(time.DateOnly), nil, http.StatusOK), &export); err != nil {
		t.Fatal(err)
	}
	if len(export.Cards) == 0 {
		t.Fatalf("выгрузка пуста")
	}
	for _, r := range export.Cards {
		if r.Title == "Переезд" && (r.Percent == nil || *r.Percent != 100) {
			t.Errorf("в выгрузке у сделанной не 100: %+v", r.Percent)
		}
	}
}
