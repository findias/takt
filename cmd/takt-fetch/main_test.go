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
	"regexp"
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
	var throttled, tired atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api-v2/")
		// Один раз YouGile отвечает «мне тяжело» — выгрузчик обязан
		// переждать и это, а не только 429.
		if path == "chats/t-1/messages" && tired.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// А этот чат он не отдаёт вовсе: карточка едет без него, доска
		// не падает, потеря названа.
		if path == "chats/t-2/messages" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
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
	if !strings.Contains(strings.Join(plan.Lost, " | "), "не отдал: 1") {
		t.Fatalf("непрочитанный чат не назван: %q", plan.Lost)
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

// Справка — по просьбе, в стандартный вывод и без отказа; язык — из
// окружения, по умолчанию русский (как у сервера).
func TestHelpIsAskedNotAnError(t *testing.T) {
	envOf := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	cases := []struct {
		args []string
		env  map[string]string
		want string
	}{
		{nil, nil, "Команды:"},
		{[]string{"help"}, nil, "YOUGILE_KEY=ключ"},
		{[]string{"--help"}, map[string]string{"LANG": "en_US.UTF-8"}, "Commands:"},
		{[]string{"-h"}, map[string]string{"LANG": "ru_RU.UTF-8", "TAKT_LANG": "en"}, "Signing in to YouGile"},
		{[]string{"yougile", "fetch", "--help"}, nil, "--no-history"},
		{[]string{"yougile"}, map[string]string{"LC_ALL": "en_GB.UTF-8"}, "takt-fetch help"},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		if err := run(context.Background(), c.args, envOf(c.env), nil, &out, &errOut); err != nil {
			t.Errorf("%v: справка кончилась отказом: %v", c.args, err)
		}
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("%v: в справке нет %q:\n%s", c.args, c.want, out.String())
		}
	}
}

// Отказы — на языке окружения и с подсказкой, что делать.
func TestRefusalsSpeakTheLanguageOfTheEnvironment(t *testing.T) {
	en := func(k string) string {
		if k == "TAKT_LANG" {
			return "en"
		}
		return ""
	}
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"yougile", "boards"}, en, nil, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "signing in to YouGile needs YOUGILE_KEY") {
		t.Fatalf("без входа, по-английски: %v", err)
	}
	err = run(context.Background(), []string{"yougile", "push"}, en, nil, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "yougile has no command «push»") {
		t.Fatalf("незнакомая команда: %v", err)
	}
}

// Строки хода дела от клиента YouGile — тоже на языке выгрузчика.
func TestProgressSpeaksTheLanguageOfTheEnvironment(t *testing.T) {
	if got := en.progress("колонка 2 из 5: Готово"); got != "column 2 of 5: Готово" {
		t.Errorf("колонка: %q", got)
	}
	if got := en.progress("чаты: 25 из 800 задач"); got != "chats: 25 of 800 tasks" {
		t.Errorf("чаты: %q", got)
	}
	if got := ru.progress("чаты: 25 из 800 задач"); got != "чаты: 25 из 800 задач" {
		t.Errorf("по-русски строка не меняется: %q", got)
	}
}

// Всякий флаг выгрузчика описан — в справке на обоих языках и на обеих
// страницах документации. Флаг, добавленный без описания, находят
// по отказу, а не по справке, — этого проверка и не даёт.
func TestEveryFlagIsDescribed(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	flags := regexp.MustCompile(`fs\.(?:String|Bool|Var)\((?:&\w+, )?"([a-z-]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(flags) < 7 {
		t.Fatalf("флагов нашлось %d — разбор main.go разошёлся с кодом", len(flags))
	}
	pages := map[string]string{"справка ru": ru.usage, "справка en": en.usage}
	for _, p := range []string{"../../docs/takt-fetch.md", "../../docs/ru/выгрузчик.md"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		pages[p] = string(raw)
	}
	for _, f := range flags {
		for where, text := range pages {
			if !strings.Contains(text, "--"+f[1]) {
				t.Errorf("флаг --%s не описан: %s", f[1], where)
			}
		}
	}
}
