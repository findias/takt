package httpapi

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Видимость на уровне HTTP.
//
// Те же правила проверены на уровне сервиса, и повторение здесь
// не лишнее: между сервисом и проводом лежат маршруты, обёртки доступа
// и разбор путей — то есть ровно те места, где закрытую доску забывают
// закрыть в одной ручке из десяти. Ошибка стоит дороже всего именно
// здесь: сервис можно вызвать неправильно только из нашего кода,
// а ручку — из чужого браузера.

// boardRoutes — всё, что привязано к конкретной доске и обязано отвечать
// «не найдена». Список нарочно собран целиком: смысл проверки в том, что
// недоступная доска одинаково невидима во всех ручках, а не только
// в снимке.
//
// Двух ручек здесь нет, и это не упущение. Ленты — событий доски
// и обсуждения карточки — на недоступной доске отвечают пустотой,
// а не отказом; так решено там же, где они написаны. Прячут обе
// одинаково: «пусто» и «нет такой» равно ничего не говорят о том,
// существует ли доска. Пустота проверяется отдельно — важно, что
// в ней действительно пусто.
func boardRoutes(boardID, cardID, userID string) []struct {
	method, path string
	body         any
} {
	return []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/boards/" + boardID, nil},
		{"POST", "/api/boards/" + boardID + "/operations", map[string]any{
			"operationId": uuid.NewString(),
			"type":        "CREATE_CARD",
			"payload":     map[string]any{"columnId": uuid.NewString(), "title": "Чужое"},
		}},
		{"GET", "/api/boards/" + boardID + "/access", nil},
		{"PUT", "/api/boards/" + boardID + "/access", map[string]any{"visibility": "org"}},
		{"PUT", "/api/boards/" + boardID + "/members/" + userID, nil},
		{"DELETE", "/api/boards/" + boardID + "/members/" + userID, nil},
		{"DELETE", "/api/boards/" + boardID, nil},
		{"POST", "/api/boards/" + boardID + "/restore", nil},
		{"GET", "/api/boards/" + boardID + "/metrics", nil},
		{"POST", "/api/boards/" + boardID + "/cards/" + cardID + "/comments",
			map[string]any{"body": "Подсмотрел"}},
		{"POST", "/api/boards/" + boardID + "/iterations",
			map[string]any{"name": "Чужая итерация", "startsOn": "2026-08-17", "endsOn": "2026-08-31"}},
	}
}

// Закрытая доска не видна никому, кроме поимённо вписанных, — включая
// владельца организации. Это самое сильное утверждение всей системы
// видимости, и проверять его только на уровне сервиса мало.
func TestPrivateBoardIsInvisibleInEveryRoute(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	keeper := owner.join("member")
	outsider := owner.join("member")

	// Доску заводит будущий хранитель и закрывает её вокруг себя:
	// закрыть доску вокруг другого нельзя.
	boardID := keeper.board("Личное")
	snap := keeper.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)
	created := keeper.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Секрет"},
	}, http.StatusOK)
	cardID := field(t, created, "patch", "cards").([]any)[0].(map[string]any)["id"].(string)

	// Состав закрытой доски раздаёт владелец организации, а закрывает
	// её тот, кто в составе остаётся: закрыть доску вокруг другого нельзя.
	// Отсюда два шага и два действующих лица.
	owner.mustDo("PUT", "/api/boards/"+boardID+"/members/"+keeper.userID, nil, http.StatusNoContent)
	keeper.mustDo("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "private"}, http.StatusNoContent)

	// Ни в одной ручке закрытая доска не должна отличаться
	// от несуществующей — ни для постороннего, ни для владельца
	// организации, который в список себя не вписывал.
	for _, who := range []struct {
		name string
		s    *session
	}{{"посторонний", outsider}, {"владелец организации", owner}} {
		for _, route := range boardRoutes(boardID, cardID, outsider.userID) {
			code, body := who.s.do(route.method, route.path, route.body)
			if code != http.StatusNotFound {
				t.Errorf("%s: %s %s → %d, ожидался 404; тело: %s",
					who.name, route.method, route.path, code, body)
			}
		}
		list := who.s.mustDo("GET", "/api/boards", nil, http.StatusOK)
		if bytes.Contains(list, []byte(boardID)) {
			t.Errorf("%s видит закрытую доску в списке: %s", who.name, list)
		}
		assertEmptyFeeds(t, who.s, who.name, boardID, cardID)
	}

	// А вписанный работает как обычно.
	keeper.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
}

// Командная доска — то же самое, но границей служит команда. Отдельная
// проверка нужна потому, что путь в политике другой: у закрытой доски
// это поимённый список, у командной — дерево команд.
func TestTeamBoardIsInvisibleOutsideTheTeamInEveryRoute(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	insider := owner.join("member")
	outsider := owner.join("member")

	teamID := owner.team("Разработка", nil)
	owner.mustDo("PUT", "/api/teams/"+teamID+"/members/"+insider.userID, nil, http.StatusNoContent)

	boardID := owner.board("Планы разработки")
	snap := owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)
	created := owner.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "План"},
	}, http.StatusOK)
	cardID := field(t, created, "patch", "cards").([]any)[0].(map[string]any)["id"].(string)

	owner.mustDo("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "team", "teamId": teamID}, http.StatusNoContent)

	for _, route := range boardRoutes(boardID, cardID, outsider.userID) {
		code, body := outsider.do(route.method, route.path, route.body)
		if code != http.StatusNotFound {
			t.Errorf("посторонний: %s %s → %d, ожидался 404; тело: %s",
				route.method, route.path, code, body)
		}
	}
	if list := outsider.mustDo("GET", "/api/boards", nil, http.StatusOK); bytes.Contains(list, []byte(boardID)) {
		t.Errorf("командная доска видна постороннему в списке: %s", list)
	}
	assertEmptyFeeds(t, outsider, "посторонний", boardID, cardID)

	// Состоящий в команде читает доску и её ленты.
	insider.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	insider.mustDo("GET", "/api/boards/"+boardID+"/events", nil, http.StatusOK)
	insider.mustDo("GET", "/api/boards/"+boardID+"/metrics", nil, http.StatusOK)
}

// Наблюдение за поддеревом даёт чтение всего, что под ним, и ничего
// сверх того. Проверка через HTTP важна отдельно: наблюдатель — частый
// повод для «а покажем-ка ему заодно и это».
func TestObserverSeesOnlyTheirSubtreeOverHTTP(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	watcher := owner.join("member")

	company := owner.team("Компания", nil)
	dev := owner.team("Разработка", company)
	sales := owner.team("Продажи", company)

	devBoard := owner.board("Доска разработки")
	owner.mustDo("PUT", "/api/boards/"+devBoard+"/access",
		map[string]any{"visibility": "team", "teamId": dev}, http.StatusNoContent)
	salesBoard := owner.board("Доска продаж")
	owner.mustDo("PUT", "/api/boards/"+salesBoard+"/access",
		map[string]any{"visibility": "team", "teamId": sales}, http.StatusNoContent)

	// До наблюдения не видно ни той, ни другой.
	if code, _ := watcher.do("GET", "/api/boards/"+devBoard, nil); code != http.StatusNotFound {
		t.Fatalf("доска команды видна постороннему: код %d", code)
	}

	owner.mustDo("POST", "/api/observers",
		map[string]any{"userId": watcher.userID, "teamId": dev}, http.StatusCreated)

	watcher.mustDo("GET", "/api/boards/"+devBoard, nil, http.StatusOK)
	if code, _ := watcher.do("GET", "/api/boards/"+salesBoard, nil); code != http.StatusNotFound {
		t.Errorf("наблюдатель разработки видит доску продаж: код %d", code)
	}

	// Наблюдение — это чтение. Писать наблюдателю нельзя, и отказ здесь
	// уже не 404: доску он видит, и притворяться, что её нет, поздно.
	snap := watcher.mustDo("GET", "/api/boards/"+devBoard, nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)
	code, body := watcher.do("POST", "/api/boards/"+devBoard+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Не должно записаться"},
	})
	if code != http.StatusForbidden {
		t.Errorf("запись наблюдателем: код %d, ожидался 403; тело: %s", code, body)
	}
}

// Чужая организация — не «нет доступа», а «нет такой доски». Разница
// не косметическая: 403 подтверждает, что доска существует, и это уже
// сведения о чужой компании.
func TestForeignOrgBoardIsIndistinguishableFromMissingInEveryRoute(t *testing.T) {
	a := newAPI(t)
	alice := a.registerOrg("Компания А")
	bob := a.registerOrg("Компания Б")

	boardID := alice.board("Своя")
	snap := alice.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK)
	columnID := field(t, snap, "columns").([]any)[0].(map[string]any)["id"].(string)
	created := alice.mustDo("POST", "/api/boards/"+boardID+"/operations", map[string]any{
		"operationId": uuid.NewString(),
		"type":        "CREATE_CARD",
		"payload":     map[string]any{"columnId": columnID, "title": "Своя карточка"},
	}, http.StatusOK)
	cardID := field(t, created, "patch", "cards").([]any)[0].(map[string]any)["id"].(string)

	for _, route := range boardRoutes(boardID, cardID, bob.userID) {
		code, body := bob.do(route.method, route.path, route.body)
		if code != http.StatusNotFound {
			t.Errorf("чужая организация: %s %s → %d, ожидался 404; тело: %s",
				route.method, route.path, code, body)
		}
	}
	assertEmptyFeeds(t, bob, "чужая организация", boardID, cardID)
}

// assertEmptyFeeds проверяет вторую половину правила: ленты недоступной
// доски отвечают согласием, но в них ничего нет. Ответ «пусто» скрывает
// доску не хуже, чем «не найдена», — а вот непустая лента была бы утечкой
// и содержимого, и самого факта существования доски.
func assertEmptyFeeds(t *testing.T, who *session, name, boardID, cardID string) {
	t.Helper()
	feed := who.mustDo("GET", "/api/boards/"+boardID+"/events", nil, http.StatusOK)
	if events, _ := field(t, feed, "events").([]any); len(events) != 0 {
		t.Errorf("%s видит %d событий недоступной доски: %s", name, len(events), feed)
	}
	talk := who.mustDo("GET", "/api/boards/"+boardID+"/cards/"+cardID+"/comments",
		nil, http.StatusOK)
	if comments, _ := field(t, talk, "comments").([]any); len(comments) != 0 {
		t.Errorf("%s видит %d комментариев недоступной карточки: %s", name, len(comments), talk)
	}
}

// Поток изменений — такая же ручка доски, как остальные, но проверяется
// отдельно: он отвечает не JSON, а потоком, и «забыли закрыть» здесь
// выглядит как молчащее соединение, а не как ошибка.
func TestStreamOfInvisibleBoardIsRefused(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания")
	outsider := owner.join("member")

	boardID := owner.board("Закрытая")
	owner.mustDo("PUT", "/api/boards/"+boardID+"/members/"+owner.userID, nil, http.StatusNoContent)
	owner.mustDo("PUT", "/api/boards/"+boardID+"/access",
		map[string]any{"visibility": "private"}, http.StatusNoContent)

	resp, body := outsider.raw("/api/boards/" + boardID + "/stream")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("поток закрытой доски: код %d, ожидался 404; тело: %.200s", resp.StatusCode, body)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Error("посторонний получил поток событий закрытой доски")
	}
}
