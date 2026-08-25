package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"

	"github.com/findias/takt/internal/apiclient"
)

// Описание до сих пор ни разу не исполнялось: его сверяли с кодом
// перечислениями и адресами, но никто не проверял, что сервер отвечает
// именно тем, что там написано. Схема ответа могла обещать поле, которое
// не приходит, или тип, которого не бывает, — и узнал бы об этом первым
// тот, кто сгенерировал по описанию типизированный клиент.
//
// Здесь запросы идут настоящие: снаружи, по /api/v1, ключом сервисного
// клиента — то есть ровно так, как ходит интеграция. Каждый ответ
// проверяется по описанию целиком: путь, параметры, тело запроса, код
// и тело ответа.
func TestContractDescribesWhatServerAnswers(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Контракт")
	key := owner.serviceKey(apiclient.ScopeBoardsRead, apiclient.ScopeBoardsWrite,
		apiclient.ScopeStructureRead, apiclient.ScopeAuditRead)
	c := &contractClient{t: t, api: a, key: key, check: contractValidator(t)}

	c.call("GET", "/me", nil, http.StatusOK)

	board := c.call("POST", "/boards", map[string]any{
		"name": "Поставки", "key": "KNTR",
	}, http.StatusCreated)
	boardID, _ := field(t, board, "id").(string)
	if boardID == "" {
		t.Fatalf("в ответе на создание доски нет id: %s", board)
	}

	c.call("GET", "/boards", nil, http.StatusOK)
	snapshot := c.call("GET", "/boards/"+boardID, nil, http.StatusOK)

	// Операция — главный маршрут контракта: через него делается всё,
	// что вообще меняет доску.
	columnID := firstColumnID(t, snapshot)
	created := c.call("POST", "/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Проверка контракта"},
	}, http.StatusOK)
	cards, ok := field(t, created, "patch", "cards").([]any)
	if !ok || len(cards) == 0 {
		t.Fatalf("в ответе на CREATE_CARD нет карточки: %s", created)
	}
	card, _ := cards[0].(map[string]any)
	cardID, _ := card["id"].(string)
	if cardID == "" {
		t.Fatalf("у созданной карточки нет id: %s", created)
	}

	c.call("GET", "/boards/"+boardID+"/cards/"+cardID, nil, http.StatusOK)
	c.call("GET", "/boards/"+boardID+"/changes?since=0", nil, http.StatusOK)
	c.call("GET", "/teams", nil, http.StatusOK)
	c.call("GET", "/audit", nil, http.StatusOK)

	// Отказы описаны так же, как успехи, и проверяются так же: клиент
	// различает случаи по коду, и схема ошибки — часть обещания.
	c.call("GET", "/boards/"+uuid.NewString(), nil, http.StatusNotFound)
	c.call("GET", "/boards/"+uuid.NewString()+"/metrics", nil, http.StatusNotFound)

	// Доска без единой законченной карточки — не отказ, а ответ:
	// пустые ряды, cycleTime и forecast равные null. Схема отчёта
	// проверяется целиком именно здесь.
	c.call("GET", "/boards/"+boardID+"/metrics?days=30", nil, http.StatusOK)
}

// contractClient ходит снаружи: по /api/v1 и ключом, а не сессией.
type contractClient struct {
	t     *testing.T
	api   *api
	key   string
	check validator.Validator
}

func (c *contractClient) call(method, path string, body any, want int) []byte {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.api.server.URL+"/api/v1"+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := c.api.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: код %d, ожидался %d; тело: %s", method, path, resp.StatusCode, want, raw)
	}

	// Тело прочитано ради сообщения об ошибке — вернуть его на место
	// обязательно, иначе проверке достанется пустой ответ и она
	// молча согласится с чем угодно.
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(mustJSON(c.t, body)))
	}
	valid, problems := c.check.ValidateHttpRequestResponse(req, resp)
	if !valid {
		for _, problem := range problems {
			c.t.Errorf("%s %s расходится с описанием: %s — %s",
				method, path, problem.Message, problem.Reason)
			for _, item := range problem.SchemaValidationErrors {
				c.t.Errorf("  %s: %s", item.FieldPath, item.Reason)
			}
		}
	}
	return raw
}

func contractValidator(t *testing.T) validator.Validator {
	t.Helper()
	document, err := libopenapi.NewDocument(openapiDocument)
	if err != nil {
		t.Fatalf("описание не разбирается: %v", err)
	}
	check, errs := validator.NewValidator(document)
	if len(errs) > 0 {
		t.Fatalf("проверку по описанию не удалось построить: %v", errs)
	}
	return check
}

// serviceKey заводит ключ так же, как его заводит владелец на экране
// «Команда»: другого способа получить ключ нет и быть не должно.
func (s *session) serviceKey(scopes ...string) string {
	s.api.t.Helper()
	raw := s.mustDo("POST", "/api/clients", map[string]any{
		"name": "Проверка контракта " + uuid8(), "scopes": scopes,
	}, http.StatusCreated)
	token, _ := field(s.api.t, raw, "token").(string)
	if token == "" {
		s.api.t.Fatalf("ключ не выдан: %s", raw)
	}
	return token
}

func firstColumnID(t *testing.T, snapshot []byte) string {
	t.Helper()
	columns, ok := field(t, snapshot, "columns").([]any)
	if !ok || len(columns) == 0 {
		t.Fatalf("у новой доски нет колонок: %s", snapshot)
	}
	first, _ := columns[0].(map[string]any)
	id, _ := first["id"].(string)
	if id == "" {
		t.Fatalf("у колонки нет id: %s", snapshot)
	}
	return id
}

func mustJSON(t *testing.T, body any) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func uuid8() string { return uuid.NewString()[:8] }
