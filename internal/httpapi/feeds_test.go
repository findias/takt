package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Ленты через HTTP: кто их читает и что в них видно.

func TestBoardFeedShowsWhatHappened(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	boardID := owner.board("Найм")

	raw := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	var snap struct {
		Columns []struct{ ID string } `json:"columns"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}

	owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": snap.Columns[0].ID, "title": "Задача"},
	}, http.StatusOK)

	raw = owner.mustDo("GET", "/api/boards/"+boardID+"/events", nil, http.StatusOK)
	var feed struct {
		Events []struct {
			Type      string  `json:"type"`
			CardTitle string  `json:"cardTitle"`
			Actor     *string `json:"actor"`
		} `json:"events"`
		Next *int64 `json:"next"`
	}
	if err := json.Unmarshal(raw, &feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Events) != 1 || feed.Events[0].Type != "created" {
		t.Fatalf("лента доски: %+v", feed.Events)
	}
	if feed.Events[0].CardTitle != "Задача" || feed.Events[0].Actor == nil {
		t.Errorf("событие без названия карточки или без автора: %+v", feed.Events[0])
	}
	if feed.Next != nil {
		t.Error("на одном событии предложено продолжение")
	}
}

// Журнал административных действий читают владелец и наблюдатель всей
// организации. Рядовой участник не читает: по ленте видно, кто кого куда
// переводил.
func TestAuditIsReadableOnlyByOwnerAndOrgObserver(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")
	watcher := owner.join("member")
	owner.mustDo("POST", "/api/observers",
		map[string]any{"userId": watcher.userID}, http.StatusCreated)

	teamID := owner.team("Разработка", nil)

	count := func(s *session) int {
		t.Helper()
		raw := s.mustDo("GET", "/api/audit", nil, http.StatusOK)
		var page struct {
			Entries []struct {
				Action    string  `json:"action"`
				Subject   string  `json:"subject"`
				SubjectID *string `json:"subjectId"`
				Actor     *string `json:"actor"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Entries {
			if e.Subject == "teams" && e.SubjectID != nil && *e.SubjectID == teamID {
				if e.Action != "insert" || e.Actor == nil {
					t.Errorf("создание команды записано неверно: %+v", e)
				}
			}
		}
		return len(page.Entries)
	}

	if count(owner) == 0 {
		t.Error("владелец не видит журнала")
	}
	if count(watcher) == 0 {
		t.Error("наблюдатель всей организации не видит журнала")
	}
	// Недоступность выглядит как пустая лента, а не как отказ.
	if got := count(member); got != 0 {
		t.Errorf("рядовой участник видит %d записей журнала", got)
	}
}

func TestAuditOfAnotherOrgIsInvisible(t *testing.T) {
	a := newAPI(t)
	first := a.registerOrg("Первая")
	first.team("Разработка", nil)
	second := a.registerOrg("Вторая")

	raw := second.mustDo("GET", "/api/audit", nil, http.StatusOK)
	var page struct {
		Entries []struct {
			Subject string `json:"subject"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Entries {
		if e.Subject == "teams" {
			t.Fatalf("в журнале чужой организации видны команды: %+v", page.Entries)
		}
	}
}

// Испорченный курсор — не ошибка запроса, а просьба показать сначала:
// разбирать нечего, но и отказывать в ленте из-за битой ссылки незачем.
func TestBrokenCursorShowsTheBeginning(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	owner.team("Разработка", nil)

	for _, bad := range []string{"мусор", "-5", "0", ""} {
		raw := owner.mustDo("GET", "/api/audit?before="+bad, nil, http.StatusOK)
		var page struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) == 0 {
			t.Errorf("курсор %q отдал пустую ленту вместо начала", bad)
		}
	}
}
