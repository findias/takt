package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/importer/pack"
)

// Выгрузчик против поддельного monday. Устройство ответов — по
// developer.monday.com (23.09.2026): GraphQL, items_page и
// next_items_page по курсору, типизированные значения колонок.
// Проверяются обещания docs/takt-fetch.md: колонки — значения колонки
// статуса по её порядку, «готово» — по done_colors; приоритет, срок
// и оценка — по названию колонки; подэлементы — подзадачами; зависимость
// — блокировкой со стороны того, кого ждут; обновления — обсуждением
// по порядку времени; группы — колонками по флагу; отказ по сложности
// пережидается; токен в пакет не попадает.

const mondayToken = "monday-secret-token-9"

func fakeMonday(t *testing.T) *httptest.Server {
	t.Helper()
	var limited atomic.Bool
	status := func(label string) map[string]any {
		return map[string]any{"id": "status", "type": "status", "text": label, "label": label}
	}
	person := func(ids ...int) map[string]any {
		var list []any
		for _, id := range ids {
			list = append(list, map[string]any{"id": id, "kind": "person"})
		}
		list = append(list, map[string]any{"id": 77, "kind": "team"})
		return map[string]any{"id": "person", "type": "people", "text": "", "persons_and_teams": list}
	}
	page1 := []any{
		map[string]any{"id": "1001", "name": "Собрать релиз", "created_at": "2026-09-01T10:00:00Z", "group": map[string]any{"id": "topics"},
			"column_values": []any{
				status("Working on it"), person(11, 12),
				map[string]any{"id": "priority", "type": "status", "text": "High", "label": "High"},
				map[string]any{"id": "date4", "type": "date", "text": "2026-10-01", "date": "2026-10-01"},
				map[string]any{"id": "numbers", "type": "numbers", "text": "5"},
				map[string]any{"id": "tags", "type": "tags", "text": "релиз, срочно"},
			},
			// monday отдаёт обновления от новых к старым.
			"updates": []any{
				map[string]any{"text_body": "Починила", "created_at": "2026-09-02T10:00:00Z", "creator": map[string]any{"id": 11}},
				map[string]any{"text_body": "Сборка упала", "created_at": "2026-09-02T09:00:00Z", "creator": map[string]any{"id": 12}},
			},
			"subitems": []any{
				map[string]any{"id": "2001", "name": "Проверить сборку", "created_at": "2026-09-01T11:00:00Z",
					"column_values": []any{map[string]any{"id": "status", "type": "status", "text": ""}}},
			}},
	}
	page2 := []any{
		map[string]any{"id": "1002", "name": "Выложить", "created_at": "2026-09-03T10:00:00Z", "group": map[string]any{"id": "new_group"},
			"column_values": []any{status("Done"),
				map[string]any{"id": "dependency", "type": "dependency", "text": "", "linked_item_ids": []any{"1001"}}}},
		map[string]any{"id": "1003", "name": "Без статуса", "created_at": "2026-09-03T11:00:00Z", "group": map[string]any{"id": "topics"},
			"column_values": []any{status("")}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != mondayToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Not Authenticated"}]}`))
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		send := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"data": data}) }
		switch {
		case strings.Contains(req.Query, "boards(limit"):
			send(map[string]any{"boards": []any{
				map[string]any{"id": "555", "name": "Продажи", "type": "board", "workspace": map[string]any{"name": "Отдел"}},
				map[string]any{"id": "556", "name": "Subitems of Продажи", "type": "sub_items_board", "workspace": nil},
			}})
		case strings.Contains(req.Query, "boards(ids"):
			ids, _ := req.Variables["id"].([]any)
			if len(ids) != 1 || ids[0] != "555" {
				send(map[string]any{"boards": []any{}})
				return
			}
			// Один раз monday отказывает по сложности — выгрузчик ждёт.
			if limited.CompareAndSwap(false, true) {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{
					"message":    "Complexity budget exhausted",
					"extensions": map[string]any{"code": "ComplexityException", "retry_in_seconds": 0.01}}}})
				return
			}
			if !strings.Contains(req.Query, "updates(limit") {
				t.Errorf("обновления не запрошены: %s", req.Query)
			}
			send(map[string]any{"boards": []any{map[string]any{
				"id": "555", "name": "Продажи",
				"columns": []any{
					map[string]any{"id": "name", "title": "Name", "type": "name", "settings_str": "{}"},
					map[string]any{"id": "person", "title": "Owner", "type": "people", "settings_str": "{}"},
					map[string]any{"id": "status", "title": "Status", "type": "status",
						"settings_str": `{"labels":{"0":"Working on it","1":"Done","2":"Stuck","5":""},"labels_positions_v2":{"5":0,"2":1,"0":2,"1":3},"done_colors":[1]}`},
					map[string]any{"id": "priority", "title": "Priority", "type": "status", "settings_str": `{"labels":{"0":"High","1":"Low"}}`},
					map[string]any{"id": "date4", "title": "Due date", "type": "date", "settings_str": "{}"},
					map[string]any{"id": "numbers", "title": "Estimate", "type": "numbers", "settings_str": "{}"},
					map[string]any{"id": "tags", "title": "Tags", "type": "tags", "settings_str": "{}"},
					map[string]any{"id": "dependency", "title": "Dependency", "type": "dependency", "settings_str": "{}"},
				},
				"groups": []any{
					map[string]any{"id": "new_group", "title": "Следующий месяц", "position": "65536.5"},
					map[string]any{"id": "topics", "title": "Этот месяц", "position": "65536"},
				},
				"items_page": map[string]any{"cursor": "c2", "items": page1},
			}}})
		case strings.Contains(req.Query, "next_items_page"):
			if req.Variables["c"] != "c2" {
				t.Errorf("курсор: %v", req.Variables["c"])
			}
			send(map[string]any{"next_items_page": map[string]any{"cursor": nil, "items": page2}})
		case strings.Contains(req.Query, "users("):
			send(map[string]any{"users": []any{
				map[string]any{"id": 11, "name": "Анна", "email": "anna@example.test"},
				map[string]any{"id": 12, "name": "Иван Петров", "email": ""},
			}})
		default:
			t.Errorf("незнакомый запрос: %s", req.Query)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchMondayWritesAPackageTaktReads(t *testing.T) {
	srv := fakeMonday(t)
	env := func(k string) string {
		if k == "MONDAY_TOKEN" {
			return mondayToken
		}
		return ""
	}
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"monday", "boards", "--url", srv.URL}, env, nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "555\tОтдел · Продажи" {
		t.Fatalf("список досок — без доски подэлементов: %q", out.String())
	}

	file := filepath.Join(t.TempDir(), "sales.takt")
	out.Reset()
	err := run(context.Background(), []string{"monday", "fetch", "--url", srv.URL, "--board", "555", "--out", file},
		env, nil, &out, &errOut)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "досок 1, карточек 4") {
		t.Fatalf("итог — обе страницы и подэлемент: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "колонки статуса «Status»") {
		t.Fatalf("выбор колонок не назван: %s", errOut.String())
	}
	raw, _ := os.ReadFile(file)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		if bytes.Contains(data, []byte(mondayToken)) {
			t.Fatalf("токен попал в пакет: %s", f.Name)
		}
	}
	pkg, err := pack.Read(raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := pkg.Plan(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan.Columns, ","); got != "Stuck,Working on it,Done" {
		t.Fatalf("колонки — значения статуса по порядку monday, без пустого: %s", got)
	}
	if plan.ColumnKinds["done"] != "done" {
		t.Fatalf("«готово» по done_colors: %v", plan.ColumnKinds)
	}
	by := map[string]importer.Card{}
	for _, c := range plan.Cards {
		by[c.Number] = c
	}
	release := by["1001"]
	if release.Column != "Working on it" || release.Priority != "high" || release.Estimate == nil || *release.Estimate != 5 ||
		release.Due == nil || release.Due.Format("2006-01-02") != "2026-10-01" ||
		strings.Join(release.Labels, ",") != "релиз,срочно" ||
		// Анна по почте, Иван без почты — к сопоставлению; команда — не человек.
		len(release.Assignees) != 1 || len(release.Unmatched) != 1 {
		t.Fatalf("1001: %+v", release)
	}
	if len(release.Comments) != 2 || release.Comments[0].Text != "Сборка упала" || release.Comments[0].AuthorName != "Иван Петров" {
		t.Fatalf("обновления — по порядку времени, с автором: %+v", release.Comments)
	}
	if len(release.Links) != 1 || release.Links[0].Kind != "blocks" || release.Links[0].To != "1002" {
		t.Fatalf("1001 держит 1002 — зависимость со стороны того, кого ждут: %+v", release.Links)
	}
	if sub := by["2001"]; sub.Parent != "1001" || sub.Column != "Working on it" {
		t.Fatalf("подэлемент — подзадача в колонке родителя: %+v", sub)
	}
	if done := by["1002"]; done.Column != "Done" {
		t.Fatalf("1002: %+v", done)
	}
	lost := strings.Join(plan.Lost, " | ")
	for _, want := range []string{"без значения колонки — встали в первую колонку: 1", "группы monday: 2", "даты завершения — у monday их нет"} {
		if !strings.Contains(lost, want) {
			t.Fatalf("потери не названы (%s): %q", want, lost)
		}
	}

	// Группы колонками — по флагу, в порядке monday.
	out.Reset()
	errOut.Reset()
	err = run(context.Background(), []string{"monday", "fetch", "--url", srv.URL, "--board", "555", "--column", "group", "--out", file},
		env, nil, &out, &errOut)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	raw, _ = os.ReadFile(file)
	pkg, err = pack.Read(raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ = pkg.Plan(1)
	if got := strings.Join(plan.Columns, ","); got != "Этот месяц,Следующий месяц" {
		t.Fatalf("колонки — группы по позиции: %s", got)
	}
}

func TestFetchMondayExplainsWhatIsMissing(t *testing.T) {
	srv := fakeMonday(t)
	ok := map[string]string{"MONDAY_TOKEN": mondayToken}
	cases := []struct {
		args []string
		env  map[string]string
		want string
	}{
		{[]string{"monday", "boards", "--url", srv.URL}, nil, "MONDAY_TOKEN"},
		{[]string{"monday", "boards", "--url", srv.URL}, map[string]string{"MONDAY_TOKEN": "чужой"}, "monday не принял токен"},
		{[]string{"monday", "fetch", "--url", srv.URL, "--out", "x.takt"}, ok, "не названа ни одна доска"},
		{[]string{"monday", "fetch", "--url", srv.URL, "--board", "9", "--out", filepath.Join(t.TempDir(), "x.takt")}, ok, "в monday нет такой доски"},
		{[]string{"monday", "fetch", "--url", srv.URL, "--board", "555", "--column", "Этап", "--out", filepath.Join(t.TempDir(), "x.takt")}, ok,
			"нет колонки статуса «Этап» — есть «Status» или --column group"},
		{[]string{"monday", "boards", "--url", srv.URL}, map[string]string{"MONDAY_TOKEN": "чужой", "TAKT_LANG": "en"}, "monday did not accept the token"},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		err := run(context.Background(), c.args, func(k string) string { return c.env[k] }, nil, &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: ждали «%s», получили %v", c.args, c.want, err)
		}
	}
}
