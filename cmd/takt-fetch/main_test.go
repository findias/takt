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

	"github.com/findias/takt/internal/importer/pack"
)

// Выгрузчик против поддельного YouGile (устройство API — как в
// internal/importer/yougile). Проверяются обещания docs/import-package.md
// со стороны пишущего: подзадачи и чаты едут; вложение и удалённое
// сообщение — нет, и это названо; ключ в пакет не попадает; предел
// запросов YouGile выгрузку не роняет; файл пакета — только владельцу.

const fakeKey = "fetch-secret-key-42"

func fakeYougile(t *testing.T) *httptest.Server {
	t.Helper()
	var throttled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api-v2/")
		// Один раз YouGile просит подождать — выгрузчик обязан переждать.
		if path == "columns" && throttled.CompareAndSwap(false, true) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		page := func(items ...any) {
			off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			out := map[string]any{"paging": map[string]any{"next": false}, "content": []any{}}
			if off < len(items) {
				out["content"] = items[off:]
			}
			_ = json.NewEncoder(w).Encode(out)
		}
		switch path {
		case "projects":
			page(map[string]any{"id": "p-1", "title": "Логистика"})
		case "boards":
			page(map[string]any{"id": "b-1", "title": "Склад", "projectId": "p-1"})
		case "columns":
			page(map[string]any{"id": "c-1", "title": "Нужно сделать", "boardId": "b-1"},
				map[string]any{"id": "c-2", "title": "Готово", "boardId": "b-1"})
		case "users":
			page(map[string]any{"id": "u-1", "email": "anna@example.test", "realName": "Анна"},
				map[string]any{"id": "u-2", "realName": "Иван Петров"})
		case "string-stickers":
			page()
		case "tasks":
			switch r.URL.Query().Get("columnId") {
			case "c-1":
				page(map[string]any{"id": "t-1", "title": "Сверить остатки", "columnId": "c-1", "subtasks": []string{"t-2", "t-3"}},
					map[string]any{"id": "t-2", "title": "Часть в колонке", "columnId": "c-1"})
			default:
				page()
			}
		case "tasks/t-3":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "t-3", "title": "Часть без колонки"})
		case "chats/t-1/messages":
			human := []any{map[string]any{"id": 1757000000000, "fromUserId": "u-2", "text": "Ряд первый сверен"},
				map[string]any{"id": 1757000060000, "fromUserId": "u-1", "textHtml": "<p>Принято</p>"},
				map[string]any{"id": 1757000120000, "fromUserId": "u-1", "text": ""},
				map[string]any{"id": 1757000180000, "fromUserId": "u-1", "text": "стёрто", "deleted": true}}
			// С includeSystem чат отдаёт и системные сообщения — историю
			// задачи; отличить их можно только сравнением двух выборок.
			if r.URL.Query().Get("includeSystem") == "true" {
				human = append([]any{map[string]any{"id": 1756999000000, "fromUserId": "u-1",
					"text": "Анна переместила задачу в колонку «Готово»"}}, human...)
			}
			page(human...)
		default:
			if strings.HasPrefix(path, "chats/") {
				page()
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchWritesAPackageTaktReads(t *testing.T) {
	srv := fakeYougile(t)
	file := filepath.Join(t.TempDir(), "склад.takt")
	env := func(k string) string {
		if k == "YOUGILE_KEY" {
			return fakeKey
		}
		return ""
	}
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"yougile", "fetch", "--url", srv.URL, "--board", "b-1",
		"--out", file, "--collected-by", "проверка"}, env, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "досок 1, карточек 3") {
		t.Fatalf("итог: %s", out.String())
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("файл пакета: %v, права %v", err, info.Mode().Perm())
	}

	raw, _ := os.ReadFile(file)
	// Ключа нет ни в одной части — ищем в распакованном, а не в сжатом.
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		if bytes.Contains(data, []byte(fakeKey)) {
			t.Fatalf("ключ API попал в пакет: %s", f.Name)
		}
	}

	pkg, err := pack.Read(raw)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.CollectedBy != "проверка" || pkg.Manifest.Source.System != "yougile" {
		t.Fatalf("манифест: %+v", pkg.Manifest)
	}
	plan, err := pkg.Plan(1)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]int{}
	for i, c := range plan.Cards {
		byTitle[c.Title] = i
	}
	parent := plan.Cards[byTitle["Сверить остатки"]]
	if len(parent.Comments) != 2 || parent.Comments[0].Text != "Ряд первый сверен" || parent.Comments[1].Text != "Принято" {
		t.Fatalf("чат: удалённое и вложение не едут, HTML — текстом: %+v", parent.Comments)
	}
	if parent.Comments[0].AuthorName != "Иван Петров" || parent.Comments[0].AuthorEmail != "" {
		t.Fatalf("автор без почты назван по имени: %+v", parent.Comments[0])
	}
	for _, title := range []string{"Часть в колонке", "Часть без колонки"} {
		c := plan.Cards[byTitle[title]]
		if c.Parent != "t-1" || c.Column != "Нужно сделать" {
			t.Fatalf("%s: родитель %q, колонка %q", title, c.Parent, c.Column)
		}
	}
	if len(parent.History) != 1 || parent.History[0].Text != "Анна переместила задачу в колонку «Готово»" ||
		parent.History[0].AuthorName != "Анна" {
		t.Fatalf("история задачи — системным сообщением, не репликой: %+v", parent.History)
	}
	if !plan.HistoryCollected {
		t.Fatal("пакет не говорит, что история собрана")
	}
	if !strings.Contains(strings.Join(plan.Lost, " | "), "вложения): 1") {
		t.Fatalf("вложение не названо: %q", plan.Lost)
	}
}

func TestFetchSaysWhatIsMissing(t *testing.T) {
	var out, errOut bytes.Buffer
	none := func(string) string { return "" }
	if err := run(context.Background(), []string{"jira", "fetch"}, none, nil, &out, &errOut); err == nil ||
		!strings.Contains(err.Error(), "пока один") {
		t.Fatalf("незнакомый источник: %v", err)
	}
	if err := run(context.Background(), []string{"yougile", "boards"}, none, nil, &out, &errOut); err == nil ||
		!strings.Contains(err.Error(), "YOUGILE_KEY") {
		t.Fatalf("без входа: %v", err)
	}
}
