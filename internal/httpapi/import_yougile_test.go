package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/findias/takt/internal/config"
)

// Перенос из YouGile (ROADMAP 23.3) — против поддельного YouGile,
// который отвечает так, как описан API v2 в двух независимых клиентах
// (см. internal/importer/yougile). Настоящий YouGile в проверках
// недоступен и не нужен: проверяется наше обещание, а не их сервер.

type fakeYougile struct {
	*httptest.Server
	mu      sync.Mutex
	created int // сколько ключей завели
	keys    []map[string]any
	seen    []string
	// Почта, под которой YouGile знает нашего владельца: её сопоставят.
	owner string
	// Колонки отвечают 500: YouGile ответил не так, как ждали.
	brokenColumns bool
	// status — ответить им на всё, кроме входа: 429 даёт «занят»,
	// 404 — «нет такой доски» (contract_codes_test).
	status int
}

const (
	ygKey     = "test-key-1"
	ygBoard   = "b-1"
	ygProject = "p-1"
)

func newFakeYougile(t *testing.T) *fakeYougile {
	t.Helper()
	f := &fakeYougile{}
	ms := func(d time.Duration) int64 { return time.Now().Add(-d).UnixMilli() }
	chatAt := ms(19 * 24 * time.Hour)
	tasks := map[string][]map[string]any{
		"c-todo": {
			{"id": "t-1", "title": "Сверить остатки", "columnId": "c-todo", "timestamp": ms(20 * 24 * time.Hour),
				"idTaskProject": "СКЛ-7", "idTaskCommon": "ID-1207",
				"assigned": []string{"u-1", "u-2"}, "description": "<p>По складу <b>№2</b></p><p>до пятницы</p>",
				"stickers":   map[string]string{"s-prio": "st-high", "s-area": "st-wh"},
				"deadline":   map[string]any{"deadline": ms(-72 * time.Hour)},
				"checklists": []map[string]any{{"title": "Шаги", "items": []map[string]any{{"title": "Выгрузить", "isCompleted": true}, {"title": "Сверить"}}}}},
			{"id": "t-2", "title": "Старое", "columnId": "c-todo", "archived": true},
			{"id": "t-3", "title": "Удалённое", "columnId": "c-todo", "deleted": true},
		},
		"c-done": {
			{"id": "t-4", "title": "Отчёт за август", "columnId": "c-done", "timestamp": ms(40 * 24 * time.Hour),
				"completed": true, "completedTimestamp": ms(30 * 24 * time.Hour), "subtasks": []string{"t-9", "t-broken"}},
		},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/api-v2/")
		f.mu.Lock()
		status := f.status
		f.mu.Unlock()
		if status != 0 && !strings.HasPrefix(path, "auth/") {
			w.WriteHeader(status)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		list := func(items ...map[string]any) {
			// Страницы — по одной записи: так проверяется, что перенос
			// идёт по всем, а не берёт первую.
			off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			out := map[string]any{"paging": map[string]any{"next": off+1 < len(items)}, "content": []any{}}
			if off < len(items) {
				out["content"] = []any{items[off]}
			}
			_ = json.NewEncoder(w).Encode(out)
		}
		if strings.HasPrefix(path, "auth/") {
			if body["login"] != "anna@yougile.test" || body["password"] != "верный" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch path {
			case "auth/companies":
				list(map[string]any{"id": "co-1", "name": "Склад", "isAdmin": true})
			case "auth/keys/get":
				_ = json.NewEncoder(w).Encode(f.keys)
			case "auth/keys":
				f.mu.Lock()
				f.created++
				f.keys = append(f.keys, map[string]any{"key": ygKey, "deleted": false})
				f.mu.Unlock()
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]string{"key": ygKey})
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+ygKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		broken := f.brokenColumns
		f.mu.Unlock()
		switch {
		// Одна подзадача, которую YouGile не отдаёт, не роняет доску.
		case path == "tasks/t-broken", path == "columns" && broken:
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		switch path {
		case "projects":
			list(map[string]any{"id": ygProject, "title": "Логистика"})
		case "boards":
			list(map[string]any{"id": ygBoard, "title": "Склад", "projectId": ygProject},
				map[string]any{"id": "b-gone", "title": "Удалённая", "projectId": ygProject, "deleted": true})
		case "columns":
			// Фильтр по доске этот YouGile «забывает» — перенос обязан
			// отсеять чужие колонки сам.
			list(map[string]any{"id": "c-todo", "title": "Нужно сделать", "boardId": ygBoard},
				map[string]any{"id": "c-done", "title": "Готово", "boardId": ygBoard},
				map[string]any{"id": "c-other", "title": "Чужая", "boardId": "b-other"})
		case "users":
			f.mu.Lock()
			owner := f.owner
			f.mu.Unlock()
			list(map[string]any{"id": "u-1", "email": strings.ToUpper(owner)},
				map[string]any{"id": "u-2", "email": "nikto@yougile.test"})
		case "string-stickers":
			list(map[string]any{"id": "s-prio", "name": "Приоритет", "states": []map[string]any{{"id": "st-high", "name": "Высокий"}}},
				map[string]any{"id": "s-area", "name": "Участок", "states": []map[string]any{{"id": "st-wh", "name": "Склад"}}})
		case "tasks":
			list(tasks[r.URL.Query().Get("columnId")]...)
		// Этот чат YouGile не отдаёт: карточка остаётся ждать, доска
		// и задание не падают.
		case "chats/t-4/messages":
			w.WriteHeader(http.StatusBadRequest)
		case "chats/t-1/messages":
			// Реплика человека и — только с includeSystem — системное
			// сообщение: история задачи в YouGile.
			// Номер сообщения — его время; у YouGile он постоянный, и
			// выборки сравниваются по нему.
			// u-gone в users нет — так YouGile отдаёт убранного из компании.
			// Его слова не должны стать словами переносящего.
			items := []map[string]any{{"id": chatAt, "fromUserId": "u-2", "text": "Начал сверку"},
				{"id": chatAt + 30_000, "fromUserId": "u-gone", "text": "Сверку закончил не я"}}
			if r.URL.Query().Get("includeSystem") == "true" {
				items = append(items, map[string]any{"id": chatAt + 60_000, "fromUserId": "u-1",
					"text": "Задача перемещена в колонку «Нужно сделать»"})
			}
			list(items...)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

type yougileAnswer struct {
	Companies []struct{ ID, Name string }           `json:"companies"`
	Key       string                                `json:"key"`
	Created   bool                                  `json:"created"`
	Boards    []struct{ ID, Title, Project string } `json:"boards"`
	Report    *struct {
		Applied   bool     `json:"applied"`
		BoardID   string   `json:"boardId"`
		Created   int      `json:"created"`
		Rows      int      `json:"rows"`
		Skipped   []any    `json:"skipped"`
		NewLabels []string `json:"newLabels"`
		Lost      []string `json:"lost"`
		Missing   []struct {
			Email string `json:"email"`
		} `json:"missingPeople"`
		NewColumns []struct{ Name, Kind string } `json:"newColumns"`
	} `json:"report"`
}

func (s *session) yougile(path string, body map[string]any, want int) yougileAnswer {
	s.api.t.Helper()
	var a yougileAnswer
	raw := s.mustDo("POST", "/api/import/yougile"+path, body, want)
	if want == http.StatusOK {
		if err := json.Unmarshal(raw, &a); err != nil {
			s.api.t.Fatal(err)
		}
	}
	return a
}

func TestYougileBoardMovesInWithWhatItLoses(t *testing.T) {
	fake := newFakeYougile(t)
	var journal bytes.Buffer
	a := newAPIConfigured(t, &journal, func(c *config.Config) { c.YougileURL = fake.URL })
	owner := a.registerOrg("Из YouGile")
	fake.owner = owner.email

	// Вход: неверный пароль назван кодом, верный даёт компании и ключ.
	raw := owner.mustDo("POST", "/api/import/yougile/companies",
		map[string]any{"login": "anna@yougile.test", "password": "не тот"}, http.StatusBadRequest)
	if field(t, raw, "code") != "yougile_denied" {
		t.Fatalf("неверный пароль: %s", raw)
	}
	auth := map[string]any{"login": "anna@yougile.test", "password": "верный"}
	if c := owner.yougile("/companies", auth, http.StatusOK); len(c.Companies) != 1 || c.Companies[0].ID != "co-1" {
		t.Fatalf("компании: %+v", c.Companies)
	}
	auth["companyId"] = "co-1"
	k := owner.yougile("/key", auth, http.StatusOK)
	if k.Key != ygKey || !k.Created {
		t.Fatalf("ключ: %+v", k)
	}
	// Второй раз ключ берётся существующий, а не заводится ещё один.
	if k2 := owner.yougile("/key", auth, http.StatusOK); k2.Created || fake.created != 1 {
		t.Fatalf("ключ завёлся повторно: %+v, заведено %d", k2, fake.created)
	}

	boards := owner.yougile("/boards", map[string]any{"key": ygKey}, http.StatusOK)
	if len(boards.Boards) != 1 || boards.Boards[0].Project != "Логистика" {
		t.Fatalf("доски: удалённая не нужна, проект назван: %+v", boards.Boards)
	}

	body := map[string]any{"key": ygKey, "board": ygBoard, "newBoardName": "Склад"}
	preview := owner.yougile("", body, http.StatusOK).Report
	if preview == nil || preview.Applied || preview.Created != 2 {
		t.Fatalf("предпросмотр: %+v", preview)
	}
	// Названо то, что не едет: архив, файлы, учёт времени; чаты — нет:
	// их дотягивают фоном после переноса.
	lost := strings.Join(preview.Lost, " | ")
	for _, want := range []string{"архива YouGile: 1", "учёт времени", "YouGile не отдал: 1"} {
		if !strings.Contains(lost, want) {
			t.Fatalf("потери не названы (%q): %q", want, lost)
		}
	}
	if len(preview.Missing) != 1 || preview.Missing[0].Email != "nikto@yougile.test" {
		t.Fatalf("ненайденные: %+v", preview.Missing)
	}
	if strings.Join(preview.NewLabels, ",") != "Участок: Склад" {
		t.Fatalf("метки из стикеров (приоритет — не метка): %q", preview.NewLabels)
	}
	for _, c := range preview.NewColumns {
		if c.Name == "Чужая" {
			t.Fatal("колонка чужой доски приехала: фильтр по доске не перепроверен")
		}
	}

	body["apply"] = true
	done := owner.yougile("", body, http.StatusOK).Report
	var snap struct {
		Cards []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
			DueOn       string `json:"dueOn"`
			Outcome     string `json:"outcome"`
		} `json:"cards"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+done.BoardID, nil, http.StatusOK), &snap)
	if len(snap.Cards) != 2 {
		t.Fatalf("карточек %d", len(snap.Cards))
	}
	for _, c := range snap.Cards {
		switch c.Title {
		case "Сверить остатки":
			// В истории — номер задачи для людей, по которому её найдут
			// в YouGile, а не внутренний идентификатор.
			detail := string(owner.mustDo("GET", "/api/boards/"+done.BoardID+"/events?cardId="+c.ID, nil, http.StatusOK))
			if !strings.Contains(detail, `"externalId":"СКЛ-7"`) || !strings.Contains(detail, `"imported":"yougile"`) {
				t.Errorf("в истории нет номера задачи YouGile: %.600s", detail)
			}
			if c.Priority != "high" || c.DueOn == "" ||
				c.Description != "По складу №2\nдо пятницы\n\nШаги:\n- [x] Выгрузить\n- [ ] Сверить" {
				t.Fatalf("задача перенесена так: %+v", c)
			}
		case "Отчёт за август":
			if c.Outcome != "done" {
				t.Fatalf("сделанная: %+v", c)
			}
		}
	}

	// Повтор — пропуск по идентификатору задачи YouGile.
	delete(body, "newBoardName")
	body["boardId"] = done.BoardID
	again := owner.yougile("", body, http.StatusOK).Report
	if again.Created != 0 || len(again.Skipped) != 2 {
		t.Fatalf("повтор: %+v", again)
	}

	// Ни пароль, ни ключ не попадают в журнал сервера — так обещано
	// в проверке для ИБ.
	for _, secret := range []string{"верный", ygKey} {
		if strings.Contains(journal.String(), secret) {
			t.Fatalf("в журнале сервера есть %q", secret)
		}
	}

	// Пароль в YouGile уходит только на вход: в запросах к данным его нет.
	for _, call := range fake.seen {
		if strings.HasPrefix(call, "POST /api-v2/") && !strings.Contains(call, "/auth/") {
			t.Fatalf("к данным YouGile ходили с паролем: %s", call)
		}
	}
}

func TestYougileUnreachableSaysHowToMoveByFile(t *testing.T) {
	a := newAPIWith(t, func(c *config.Config) { c.YougileURL = "http://127.0.0.1:1" })
	owner := a.registerOrg("Закрытый контур")
	raw := owner.mustDo("POST", "/api/import/yougile/companies",
		map[string]any{"login": "a@b.test", "password": "x"}, http.StatusBadGateway)
	if field(t, raw, "code") != "yougile_unreachable" || !strings.Contains(string(raw), "файлом") {
		t.Fatalf("недоступный YouGile: %s", raw)
	}
	gleb := a.member(owner, "viewer")
	gleb.mustDo("POST", "/api/import/yougile/boards", map[string]any{"key": "k"}, http.StatusForbidden)
}

func TestYougileCanBeSwitchedOff(t *testing.T) {
	a := newAPIWith(t, func(c *config.Config) { c.YougileURL = config.YougileOff })
	owner := a.registerOrg("Без YouGile")
	raw := owner.mustDo("POST", "/api/import/yougile/companies",
		map[string]any{"login": "a@b.test", "password": "x"}, http.StatusForbidden)
	if field(t, raw, "code") != "yougile_off" {
		t.Fatalf("выключенный YouGile: %s", raw)
	}
}

// Неожиданный ответ YouGile — не «внутренняя ошибка, попробуйте ещё раз»:
// отказ называет, что ответил YouGile, и код у него свой.
func TestYougileOddAnswerIsNamed(t *testing.T) {
	fake := newFakeYougile(t)
	a := newAPIWith(t, func(c *config.Config) { c.YougileURL = fake.URL })
	owner := a.registerOrg("Странный YouGile")
	fake.owner = owner.email
	fake.mu.Lock()
	fake.brokenColumns = true
	fake.mu.Unlock()
	raw := owner.mustDo("POST", "/api/import/yougile",
		map[string]any{"key": ygKey, "board": ygBoard, "newBoardName": "Склад"}, http.StatusBadGateway)
	if field(t, raw, "code") != "yougile_failed" || !strings.Contains(string(raw), "500") {
		t.Fatalf("неожиданный ответ YouGile: %s", raw)
	}
}

// История и обсуждение YouGile дотягиваются фоном после переноса по API
// (ROADMAP 23.7): реплика — в обсуждение, системное сообщение —
// в историю «до переноса»; повтор той же доски ничего не дотягивает
// заново.
func TestYougileHistoryArrivesInTheBackground(t *testing.T) {
	fake := newFakeYougile(t)
	a := newAPIWith(t, func(c *config.Config) { c.YougileURL = fake.URL })
	owner := a.registerOrg("История YouGile")
	fake.owner = owner.email

	body := map[string]any{"key": ygKey, "board": ygBoard, "newBoardName": "Склад", "apply": true}
	raw := owner.mustDo("POST", "/api/import/yougile", body, http.StatusOK)
	boardID, _ := field(t, raw, "report", "boardId").(string)
	if pending, _ := field(t, raw, "report", "historyPending").(float64); pending != 2 {
		t.Fatalf("карточек ждут истории: %v", pending)
	}

	var job struct {
		Total, Done, Comments, History, Skipped int
		Finished                                bool
		Failed                                  string
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = json.Unmarshal(owner.mustDo("GET", "/api/import/yougile/history/"+boardID, nil, http.StatusOK), &job)
		if job.Finished || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !job.Finished || job.Failed != "" || job.Done != 1 || job.Skipped != 1 ||
		job.Comments != 2 || job.History != 1 {
		t.Fatalf("задание: %+v", job)
	}

	var snap struct {
		Cards []struct{ ID, Title string } `json:"cards"`
	}
	_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID, nil, http.StatusOK), &snap)
	for _, c := range snap.Cards {
		if c.Title != "Сверить остатки" {
			continue
		}
		var detail struct {
			Comments      int `json:"comments"`
			SourceHistory struct {
				Source  string
				Pending bool
				Entries []struct{ Author, Text string }
			} `json:"sourceHistory"`
		}
		_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID+"/cards/"+c.ID, nil, http.StatusOK), &detail)
		h := detail.SourceHistory
		if h.Source != "yougile" || h.Pending || len(h.Entries) != 1 ||
			h.Entries[0].Text != "Задача перемещена в колонку «Нужно сделать»" || detail.Comments != 2 {
			t.Fatalf("история карточки: %+v, реплик %d", h, detail.Comments)
		}
		var comments struct {
			Comments []struct {
				Body string `json:"body"`
			} `json:"comments"`
		}
		_ = json.Unmarshal(owner.mustDo("GET", "/api/boards/"+boardID+"/cards/"+c.ID+"/comments", nil, http.StatusOK), &comments)
		bodies := []string{}
		for _, cm := range comments.Comments {
			bodies = append(bodies, cm.Body)
		}
		if joined := strings.Join(bodies, " | "); !strings.Contains(joined, "из YouGile: автор неизвестен\n\nСверку закончил не я") {
			t.Fatalf("реплика неизвестного автора: %q", joined)
		}
	}

	// Повтор: всё уже дотянуто.
	delete(body, "newBoardName")
	body["boardId"] = boardID
	body["apply"] = false
	raw = owner.mustDo("POST", "/api/import/yougile", body, http.StatusOK)
	// Карточка, чей чат не отдали, осталась ждать следующего переноса.
	if pending, _ := field(t, raw, "report", "historyPending").(float64); pending != 1 {
		t.Fatalf("после дотягивания ждут истории %v, ожидалась одна", pending)
	}
}
