package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Доступ к доске через HTTP: команда, видимость, поимённый состав.

func (s *session) board(name string) string {
	s.api.t.Helper()
	raw := s.mustDo("POST", "/api/boards", map[string]any{"name": name}, http.StatusCreated)
	id, _ := field(s.api.t, raw, "id").(string)
	if id == "" {
		s.api.t.Fatalf("создание доски не вернуло идентификатор: %s", raw)
	}
	return id
}

func TestBoardVisibilityIsChangedOverHTTP(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")

	boardID := owner.board("Найм")
	teamID := owner.team("Разработка", nil)

	raw := owner.mustDo("GET", "/api/boards/"+boardID+"/access", nil, http.StatusOK)
	if v, _ := field(t, raw, "visibility").(string); v != "org" {
		t.Fatalf("новая доска: видимость %q, ожидалась org", v)
	}

	// Пока доска общая — её видит любой участник организации.
	member.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)

	owner.mustDo("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "team", "teamId": teamID}, http.StatusNoContent)

	if code, _ := member.do("GET", "/api/boards/"+boardID, nil); code != http.StatusNotFound {
		t.Errorf("командная доска видна постороннему: код %d, ожидался 404", code)
	}

	// Вписали в команду — доска появилась.
	owner.mustDo("PUT", "/api/teams/"+teamID+"/members/"+member.userID, nil, http.StatusNoContent)
	member.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)

	// Командная доска без команды — ошибка клиента, а не сбой.
	code, body := owner.do("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "team"})
	if code != http.StatusBadRequest {
		t.Errorf("командная доска без команды: код %d, ожидался 400; тело: %s", code, body)
	}
}

// Закрыть доску можно только вокруг себя. Отказ обязан объяснять, что
// делать, — база отвечает на это голым нарушением политики.
func TestClosingBoardAroundSomeoneElseAnswersWithExplanation(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")
	boardID := owner.board("Найм")

	owner.mustDo("PUT", "/api/boards/"+boardID+"/members/"+member.userID, nil, http.StatusNoContent)

	code, raw := owner.do("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "private"})
	if code != http.StatusConflict {
		t.Fatalf("закрытие вокруг чужого: код %d, ожидался 409; тело: %s", code, raw)
	}
	if msg, _ := field(t, raw, "error").(string); msg == "" {
		t.Error("отказ пришёл без объяснения")
	}

	// Вписали себя — то же действие проходит.
	owner.mustDo("PUT", "/api/boards/"+boardID+"/members/"+owner.userID, nil, http.StatusNoContent)
	owner.mustDo("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "private"}, http.StatusNoContent)

	raw = owner.mustDo("GET", "/api/boards/"+boardID+"/access", nil, http.StatusOK)
	var access struct {
		Visibility string `json:"visibility"`
		Members    []struct {
			UserID string `json:"userId"`
		} `json:"members"`
	}
	if err := json.Unmarshal(raw, &access); err != nil {
		t.Fatal(err)
	}
	if access.Visibility != "private" || len(access.Members) != 2 {
		t.Errorf("доступ к закрытой доске: %+v", access)
	}
}

// Наблюдатель видит доску, но не управляет ею: «видит всё» и «может всё» —
// разные вопросы.
func TestObserverReadsBoardButDoesNotChangeItsAccess(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	watcher := owner.join("member")
	boardID := owner.board("Найм")
	teamID := owner.team("Разработка", nil)

	owner.mustDo("POST", "/api/observers",
		map[string]any{"userId": watcher.userID}, http.StatusCreated)
	owner.mustDo("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "team", "teamId": teamID}, http.StatusNoContent)

	watcher.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)

	code, raw := watcher.do("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "org"})
	if code == http.StatusNoContent {
		t.Fatalf("наблюдатель сменил видимость доски; тело: %s", raw)
	}
}

func TestBoardAccessOfAnotherOrgIsNotFound(t *testing.T) {
	a := newAPI(t)
	first := a.registerOrg("Первая")
	second := a.registerOrg("Вторая")
	boardID := first.board("Найм")

	if code, _ := second.do("GET", "/api/boards/"+boardID+"/access", nil); code != http.StatusNotFound {
		t.Errorf("чтение чужого доступа: код %d, ожидался 404", code)
	}
	if code, _ := second.do("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "org"}); code != http.StatusNotFound {
		t.Errorf("смена чужой видимости: код %d, ожидался 404", code)
	}
	if code, _ := second.do("PUT", "/api/boards/"+boardID+"/members/"+second.userID, nil); code != http.StatusNotFound {
		t.Errorf("добавление в чужую доску: код %d, ожидался 404", code)
	}
}

// «Убрать» — не «удалить»: карточки и журнал остаются, и доску можно
// вернуть. Без обратного действия архивация была бы удалением с лишним
// шагом.
func TestBoardIsArchivedAndRestoredOverHTTP(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	viewer := owner.join("viewer")
	boardID := owner.board("Найм")

	// Наблюдателю доска видна, но убрать её он не может.
	viewer.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	if code, _ := viewer.do("DELETE", "/api/boards/"+boardID, nil); code != http.StatusForbidden {
		t.Errorf("наблюдатель убрал доску: код %d, ожидался 403", code)
	}

	owner.mustDo("DELETE", "/api/boards/"+boardID, nil, http.StatusNoContent)
	if code, _ := owner.do("GET", "/api/boards/"+boardID, nil); code != http.StatusNotFound {
		t.Errorf("убранная доска открывается: код %d, ожидался 404", code)
	}

	raw := owner.mustDo("GET", "/api/boards/archived", nil, http.StatusOK)
	if boards, _ := field(t, raw, "boards").([]any); len(boards) != 1 {
		t.Fatalf("в архиве %d досок, ожидалась одна; тело: %s", len(boards), raw)
	}

	owner.mustDo("POST", "/api/boards/"+boardID+"/restore", nil, http.StatusNoContent)
	owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)

	// Повтор ничего не меняет и говорит об этом.
	if code, _ := owner.do("POST", "/api/boards/"+boardID+"/restore", nil); code != http.StatusNotFound {
		t.Errorf("повторный возврат: код %d, ожидался 404", code)
	}
}
