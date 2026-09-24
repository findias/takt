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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/findias/takt/internal/importer"
	"github.com/findias/takt/internal/importer/pack"
)

// Выгрузчик против поддельной Jira — облачной и своей установки.
// Устройство ответов — по описанию Atlassian (Agile 1.0, REST v3 и v2).
// Проверяются обещания docs/takt-fetch.md: колонки — по статусам доски,
// подзадачи, связи, оценка, комментарии со второй страницы; задача
// в статусе вне колонок и скрытая почта названы; токен в пакет
// не попадает; предел запросов выгрузку не роняет.

const jiraToken = "jira-secret-token-77"

func fakeJira(t *testing.T, cloud bool) *httptest.Server {
	t.Helper()
	var throttled atomic.Bool
	api := "/rest/api/2/"
	if cloud {
		api = "/rest/api/3/"
	}
	// Описание и комментарий: в облаке — документ ADF, у своей
	// установки — строка.
	doc := func(ru string, adf map[string]any) any {
		if cloud {
			return adf
		}
		return ru
	}
	para := func(parts ...any) map[string]any { return map[string]any{"type": "paragraph", "content": parts} }
	txt := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	person := func(id, name, email string) map[string]any {
		p := map[string]any{"displayName": name}
		if cloud {
			p["accountId"] = id
		} else {
			p["key"], p["name"] = id, id
		}
		if email != "" {
			p["emailAddress"] = email
		}
		return p
	}
	// Эпик: облако называет его уровнем иерархии, своя установка —
	// только названием типа.
	epicType := map[string]any{"name": "Epic"}
	if cloud {
		epicType["hierarchyLevel"] = 1
	}
	withEpic := func(f map[string]any) map[string]any {
		if cloud {
			f["parent"] = map[string]any{"id": "20", "key": "DEV-9", "fields": map[string]any{"issuetype": epicType}}
		} else {
			f["customfield_10008"] = "DEV-9"
		}
		return f
	}
	anna := person("acc-1", "Анна", "anna@example.test")
	ivan := person("acc-2", "Иван Петров", "")
	firstComment := map[string]any{"author": ivan, "created": "2026-09-02T09:00:00.000+0300",
		"body": doc("Сборка упала на тестах", map[string]any{"type": "doc", "content": []any{para(txt("Сборка упала на тестах"))}})}
	secondComment := map[string]any{"author": anna, "created": "2026-09-02T10:00:00.000+0300",
		"body": doc("Починила", map[string]any{"type": "doc", "content": []any{para(txt("Починила"))}})}

	issues := []map[string]any{
		{"id": "10", "key": "DEV-1", "fields": map[string]any{
			"summary": "Собрать релиз", "status": map[string]any{"id": "3"},
			"assignee": anna, "labels": []string{"релиз"}, "priority": map[string]any{"name": "High"},
			"duedate": "2026-10-01", "created": "2026-09-01T10:00:00.000+0300",
			"description": doc("Шаги сборки", map[string]any{"type": "doc", "content": []any{
				para(txt("Шаги сборки для "), map[string]any{"type": "mention", "attrs": map[string]any{"text": "@Анна"}}),
				map[string]any{"type": "taskList", "content": []any{
					map[string]any{"type": "taskItem", "attrs": map[string]any{"state": "DONE"}, "content": []any{txt("Собрать")}},
					map[string]any{"type": "taskItem", "attrs": map[string]any{"state": "TODO"}, "content": []any{txt("Выложить")}},
				}},
			}}),
			"customfield_10016": 5,
			"issuelinks": []any{
				map[string]any{"type": map[string]any{"name": "Blocks"}, "outwardIssue": map[string]any{"id": "11"}},
				map[string]any{"type": map[string]any{"name": "Relates"}, "inwardIssue": map[string]any{"id": "12"}},
			},
			// Поиск отдал один комментарий из двух — второй придёт
			// отдельным запросом.
			"comment": map[string]any{"comments": []any{firstComment}, "total": 2},
		}},
		{"id": "11", "key": "DEV-2", "fields": map[string]any{
			"summary": "Проверить сборку", "status": map[string]any{"id": "1"}, "parent": map[string]any{"id": "10"},
			"created": "2026-09-01T11:00:00.000+0300",
		}},
		{"id": "12", "key": "DEV-3", "fields": map[string]any{
			"summary": "Закрыто давно", "status": map[string]any{"id": "10002"},
			"issuelinks": []any{map[string]any{"type": map[string]any{"name": "Relates"}, "outwardIssue": map[string]any{"id": "10"}}},
			"created":    "2026-08-01T11:00:00.000+0300", "resolutiondate": "2026-08-20T18:00:00.000+0300",
		}},
		// Статус не привязан к колонкам доски: на доске Jira её не видно.
		{"id": "13", "key": "DEV-4", "fields": map[string]any{"summary": "Вне доски", "status": map[string]any{"id": "6"}}},
		// Эпик на самой доске: едет на портфель, а не в колонку команды.
		{"id": "15", "key": "DEV-6", "fields": map[string]any{"summary": "Эпик на доске", "status": map[string]any{"id": "1"},
			"issuetype": epicType}},
		// Задача эпика, которого на доске нет: облако знает его родителем,
		// своя установка — полем «Epic Link».
		{"id": "14", "key": "DEV-5", "fields": withEpic(map[string]any{"summary": "Задача эпика", "status": map[string]any{"id": "1"}})},
	}
	// Эпик вне доски — приходит отдельным поиском.
	outerEpic := map[string]any{"id": "20", "key": "DEV-9", "fields": map[string]any{
		"summary": "Большой переезд", "status": map[string]any{"id": "3"}, "issuetype": epicType}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, basic := r.BasicAuth()
		switch {
		case cloud && !(basic && user == "anna@example.test" && pass == jiraToken),
			!cloud && r.Header.Get("Authorization") != "Bearer "+jiraToken:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/rest/agile/1.0/board":
			send(map[string]any{"isLast": true, "values": []any{map[string]any{"id": 12, "name": "Разработка", "type": "kanban",
				"location": map[string]any{"projectKey": "DEV", "projectName": "Платформа"}}}})
		case "/rest/agile/1.0/board/12":
			send(map[string]any{"id": 12, "name": "Разработка"})
		case "/rest/agile/1.0/board/12/configuration":
			send(map[string]any{
				"filter":   map[string]any{"id": "10001"},
				"subQuery": map[string]any{"query": "fixVersion in unreleasedVersions() OR fixVersion is EMPTY"},
				"columnConfig": map[string]any{"columns": []any{
					map[string]any{"name": "К выполнению", "statuses": []any{map[string]any{"id": "1"}}},
					map[string]any{"name": "В работе", "statuses": []any{map[string]any{"id": "3"}}},
					map[string]any{"name": "Пустая", "statuses": []any{}},
					map[string]any{"name": "Готово", "statuses": []any{map[string]any{"id": "10002"}}},
				}},
				"estimation": map[string]any{"type": "field", "field": map[string]any{"fieldId": "customfield_10016"}},
			})
		case api + "status":
			cat := func(id, key string) map[string]any {
				return map[string]any{"id": id, "statusCategory": map[string]any{"key": key}}
			}
			send([]any{cat("1", "new"), cat("3", "indeterminate"), cat("10002", "done"), cat("6", "done")})
		case "/rest/api/3/search/jql", "/rest/api/2/search":
			if r.URL.Path != api+"search"+map[bool]string{true: "/jql"}[cloud] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// Один раз Jira просит подождать — выгрузчик пережидает.
			if throttled.CompareAndSwap(false, true) {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			var body struct {
				JQL           string   `json:"jql"`
				Fields        []string `json:"fields"`
				NextPageToken string   `json:"nextPageToken"`
				StartAt       int      `json:"startAt"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.JQL == "id in (20)" || body.JQL == `key in ("DEV-9")` {
				page := map[string]any{"issues": []any{outerEpic}}
				if cloud {
					page["isLast"] = true
				} else {
					page["startAt"], page["total"] = 0, 1
				}
				send(page)
				return
			}
			if !cloud && !strings.Contains(strings.Join(body.Fields, ","), "customfield_10008") {
				t.Errorf("своя установка: поиск без поля «Epic Link»: %+v", body.Fields)
			}
			if !strings.HasPrefix(body.JQL, "filter = 10001 AND (fixVersion") || !strings.Contains(strings.Join(body.Fields, ","), "customfield_10016") {
				t.Errorf("запрос поиска: %+v", body)
			}
			// Две страницы: первая — две задачи.
			from := body.StartAt
			if cloud && body.NextPageToken != "" {
				from, _ = strconv.Atoi(strings.TrimPrefix(body.NextPageToken, "p"))
			}
			to := min(from+2, len(issues))
			page := map[string]any{"issues": issues[from:to]}
			if cloud {
				if to < len(issues) {
					page["nextPageToken"] = "p" + strconv.Itoa(to)
				} else {
					page["isLast"] = true
				}
			} else {
				page["startAt"], page["total"] = from, len(issues)
			}
			send(page)
		case "/rest/api/2/field":
			send([]any{
				map[string]any{"id": "summary", "schema": map[string]any{"type": "string"}},
				map[string]any{"id": "customfield_10008", "schema": map[string]any{"custom": "com.pyxis.greenhopper.jira:gh-epic-link"}},
			})
		case api + "issue/10/comment":
			send(map[string]any{"comments": []any{firstComment, secondComment}, "total": 2})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchJiraWritesAPackageTaktReads(t *testing.T) {
	for _, cloud := range []bool{true, false} {
		name := map[bool]string{true: "облако", false: "своя установка"}[cloud]
		t.Run(name, func(t *testing.T) {
			srv := fakeJira(t, cloud)
			env := func(k string) string {
				switch {
				case k == "JIRA_TOKEN":
					return jiraToken
				case k == "JIRA_EMAIL" && cloud:
					return "anna@example.test"
				}
				return ""
			}
			var out, errOut bytes.Buffer
			if err := run(context.Background(), []string{"jira", "boards", "--url", srv.URL}, env, nil, &out, &errOut); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "12\tDEV Платформа · Разработка (kanban)") {
				t.Fatalf("список досок: %q", out.String())
			}

			file := filepath.Join(t.TempDir(), "dev.takt")
			out.Reset()
			err := run(context.Background(), []string{"jira", "fetch", "--url", srv.URL, "--board", "12", "--out", file},
				env, nil, &out, &errOut)
			if err != nil {
				t.Fatalf("%v\n%s", err, errOut.String())
			}
			// Доска команды — четыре задачи, портфель — два эпика.
			if !strings.Contains(out.String(), "досок 2, карточек 6") {
				t.Fatalf("итог: %s", out.String())
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
				if bytes.Contains(data, []byte(jiraToken)) {
					t.Fatalf("токен попал в пакет: %s", f.Name)
				}
			}
			pkg, err := pack.Read(raw)
			if err != nil {
				t.Fatal(err)
			}
			if pkg.Manifest.Source.System != "jira" {
				t.Fatalf("источник: %+v", pkg.Manifest.Source)
			}
			plan, err := pkg.Plan(1)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(plan.Columns, ","); got != "К выполнению,В работе,Готово" {
				t.Fatalf("колонки — по порядку доски, без пустой: %s", got)
			}
			if plan.ColumnKinds["готово"] != "done" || plan.ColumnKinds["в работе"] != "in_progress" || plan.ColumnKinds["к выполнению"] != "queue" {
				t.Fatalf("разметка по категориям статусов: %v", plan.ColumnKinds)
			}
			by := map[string]importer.Card{}
			for _, c := range plan.Cards {
				by[c.Number] = c
			}
			release := by["DEV-1"]
			if release.Column != "В работе" || release.Priority != "high" || release.Estimate == nil || *release.Estimate != 5 ||
				release.Due == nil || release.Due.Format("2006-01-02") != "2026-10-01" || len(release.Assignees) != 1 {
				t.Fatalf("DEV-1: %+v", release)
			}
			if cloud && (!strings.Contains(release.Description, "Шаги сборки для @Анна") ||
				!strings.Contains(release.Description, "- [x] Собрать\n- [ ] Выложить")) {
				t.Fatalf("описание ADF — текстом с чек-листом: %q", release.Description)
			}
			if len(release.Comments) != 2 || release.Comments[1].Text != "Починила" ||
				release.Comments[0].AuthorName != "Иван Петров" {
				t.Fatalf("комментарии — оба, второй со своей страницы: %+v", release.Comments)
			}
			if len(release.Links) != 1 || release.Links[0].Kind != "blocks" || release.Links[0].To != "11" {
				t.Fatalf("связи DEV-1 — только исходящая: %+v", release.Links)
			}
			if old := by["DEV-3"]; len(old.Links) != 1 || old.Links[0].Kind != "relates" || old.Done == nil {
				t.Fatalf("DEV-3: %+v", old)
			}
			if by["DEV-2"].Parent != "10" {
				t.Fatalf("подзадача: %+v", by["DEV-2"])
			}
			if _, onTeam := by["DEV-6"]; onTeam {
				t.Fatalf("эпик остался в колонке команды: %+v", by["DEV-6"])
			}
			if c := by["DEV-5"]; c.Parent != "20" || c.ParentBoard != "Эпики" {
				t.Fatalf("задача эпика вне доски: %+v", c)
			}

			// Портфель — последней доской пакета: эпики с доски и вне её.
			epics, err := pkg.Plan(2)
			if err != nil {
				t.Fatal(err)
			}
			col := map[string]string{}
			for _, c := range epics.Cards {
				col[c.Number] = c.Column
			}
			if !epics.Portfolio || col["DEV-6"] != "Идея" || col["DEV-9"] != "В работе" || len(col) != 2 {
				t.Fatalf("портфель: уровень %v, эпики %v", epics.Portfolio, col)
			}
			if len(epics.ForeignParts) != 1 || epics.ForeignParts[0] != (importer.ForeignPart{Parent: "20", Child: "14"}) {
				t.Fatalf("части эпиков на доске команды: %+v", epics.ForeignParts)
			}
			lost := strings.Join(plan.Lost, " | ")
			if !strings.Contains(lost, "нет среди колонок доски Jira: 1") || !strings.Contains(lost, "приватности Jira: 1") {
				t.Fatalf("потери не названы: %q", lost)
			}
		})
	}
}
