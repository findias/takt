package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
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
func TestClosingBoardInscribesTheOneWhoClosesIt(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	member := owner.join("member")
	boardID := owner.board("Найм")

	outsider := owner.join("member")
	owner.mustDo("PUT", "/api/boards/"+boardID+"/members/"+member.userID, nil, http.StatusNoContent)

	// Участнику, которого в состав не вписывали, закрыть доску нечем:
	// состав раздаёт владелец организации. Отказ приходит запретом,
	// а не «не найдено», и называет того, кто может.
	code, raw := outsider.do("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "private"})
	if code != http.StatusForbidden {
		t.Fatalf("закрытие доски участником: код %d, ожидался 403; тело: %s", code, raw)
	}
	if msg, _ := field(t, raw, "error").(string); msg == "" {
		t.Error("отказ пришёл без объяснения")
	}

	// А владельцу — одним действием, без предварительного «впиши себя»:
	// раньше здесь приходил отказ, из которого человек и узнавал порядок.
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

// Итерации через HTTP. Проверяется то же, что и на сервисе, но здесь
// важнее форма отказа: правило итерации — конфликт с объяснением,
// а не пятисотка.
func TestIterationOverHTTP(t *testing.T) {
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

	raw = owner.mustDo("POST", "/api/boards/"+boardID+"/iterations", map[string]any{
		"name": "Спринт 1", "goal": "Довести до стенда",
		"startsOn": "2026-08-01", "endsOn": "2026-08-14",
	}, http.StatusCreated)
	iterationID, _ := field(t, raw, "id").(string)
	if iterationID == "" {
		t.Fatalf("итерация не вернула идентификатор: %s", raw)
	}

	// Конец раньше начала — ошибка клиента, а не сбой.
	if code, _ := owner.do("POST", "/api/boards/"+boardID+"/iterations", map[string]any{
		"name": "Задом наперёд", "startsOn": "2026-08-14", "endsOn": "2026-08-01",
	}); code != http.StatusBadRequest {
		t.Errorf("итерация с концом раньше начала: код %d, ожидался 400", code)
	}

	card := owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": snap.Columns[0].ID, "title": "Задача"},
	}, http.StatusOK)
	cardID, _ := field(t, card, "patch", "cards").([]any)[0].(map[string]any)["id"].(string)

	owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "ADD_TO_ITERATION",
		"payload":     map[string]any{"cardId": cardID, "iterationId": iterationID},
	}, http.StatusOK)

	raw = owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	if got := field(t, raw, "cardIterations", cardID); got != iterationID {
		t.Errorf("снимок не показывает вхождение карточки в итерацию: %v", got)
	}

	owner.mustDo("POST", "/api/boards/"+boardID+"/iterations/"+iterationID+"/close",
		nil, http.StatusNoContent)

	code, body := owner.do("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "ADD_TO_ITERATION",
		"payload":     map[string]any{"cardId": cardID, "iterationId": iterationID},
	})
	if code != http.StatusConflict {
		t.Fatalf("добавление в закрытую итерацию: код %d, ожидался 409; тело: %s", code, body)
	}
	if msg, _ := field(t, body, "error").(string); msg == "" {
		t.Error("отказ пришёл без объяснения")
	}
}
