package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// Выгрузчик против поддельного Kaiten. Устройство ответов — по
// developers.kaiten.ru (23.09.2026). Проверяются обещания
// docs/takt-fetch.md: подколонки — отдельными колонками с именем
// родительской, дорожки — метками, несколько родителей — первый
// и названо, блокировка карточкой — связью «блокирует», комментарий
// в HTML — текстом; вторая страница карточек дочитана; токен в пакет
// не попадает; просьба подождать выгрузку не роняет.

const kaitenToken = "kaiten-secret-token-42"

func fakeKaiten(t *testing.T) *httptest.Server {
	t.Helper()
	var throttled atomic.Bool
	anna := map[string]any{"id": 1, "full_name": "Анна", "email": "anna@example.test"}
	ivan := map[string]any{"id": 2, "full_name": "Иван Петров", "username": "ivan"}
	id := func(v int) *int { return &v }

	cards := []map[string]any{
		{"id": 501, "title": "Собрать релиз", "description": "Шаги сборки", "state": 2, "column_id": 31, "lane_id": 71,
			"members": []any{anna, ivan}, "tags": []any{map[string]any{"name": "релиз"}}, "size": 5, "asap": true,
			"due_date": "2026-10-01T00:00:00.000Z", "created": "2026-09-01T10:00:00.000Z"},
		{"id": 502, "title": "Проверить сборку", "state": 1, "column_id": 10, "lane_id": 72,
			"parents_ids": []int{501, 999, 503}, "created": "2026-09-01T11:00:00.000Z",
			"blockers": []any{map[string]any{"blocker_card_id": id(503), "released": false}}},
		{"id": 503, "title": "Закрыто давно", "state": 3, "column_id": 40, "lane_id": 71,
			"created": "2026-08-01T11:00:00.000Z", "completed_at": "2026-08-20T18:00:00.000Z"},
		// Лежит прямо в колонке с подколонками — встанет в первую из них.
		// Блокировка снята — связи «блокирует» от неё не остаётся.
		{"id": 504, "title": "Без подколонки", "state": 2, "column_id": 30, "lane_id": 71, "created": "2026-09-02T11:00:00.000Z",
			"blockers": []any{map[string]any{"blocker_card_id": id(501), "released": true}}},
	}
	// Хвост, чтобы карточек было больше страницы: Kaiten отдаёт по сто.
	for i := range 100 {
		cards = append(cards, map[string]any{"id": 600 + i, "title": fmt.Sprintf("Мелочь %d", i), "state": 1,
			"column_id": 10, "lane_id": 71, "created": "2026-09-03T10:00:00.000Z"})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+kaitenToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/api/latest/spaces":
			send([]any{map[string]any{"id": 7, "title": "Склад"}, map[string]any{"id": 8, "title": "Закрытое"}})
		case "/api/latest/spaces/7/boards":
			send([]any{map[string]any{"id": 345, "title": "Поставки"}})
		case "/api/latest/spaces/8/boards":
			w.WriteHeader(http.StatusForbidden)
		case "/api/latest/boards/345":
			send(map[string]any{"id": 345, "title": "Поставки",
				"columns": []any{
					map[string]any{"id": 40, "title": "Готово", "sort_order": 4, "type": 3},
					map[string]any{"id": 10, "title": "Очередь", "sort_order": 1, "type": 1},
					map[string]any{"id": 30, "title": "В работе", "sort_order": 2, "type": 2},
					map[string]any{"id": 32, "title": "Проверка", "sort_order": 2, "column_id": 30},
					map[string]any{"id": 31, "title": "Делаем", "sort_order": 1, "column_id": 30, "type": 2},
				},
				"lanes": []any{
					map[string]any{"id": 71, "title": "Обычные", "sort_order": 1},
					map[string]any{"id": 72, "title": "Срочные", "sort_order": 2},
				}})
		case "/api/latest/cards":
			q := r.URL.Query()
			if q.Get("board_id") != "345" || q.Get("condition") != "1" || q.Get("limit") != "100" {
				t.Errorf("запрос карточек: %s", r.URL.RawQuery)
			}
			// Один раз Kaiten просит подождать — выгрузчик пережидает.
			if throttled.CompareAndSwap(false, true) {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			from, _ := strconv.Atoi(q.Get("offset"))
			send(cards[min(from, len(cards)):min(from+100, len(cards))])
		case "/api/latest/cards/501/comments":
			send([]any{
				map[string]any{"text": "<p>Сборка упала</p><p>на тестах &amp; линтере</p>", "type": 2, "created": "2026-09-02T09:00:00.000Z", "author": ivan},
				map[string]any{"text": "Починила", "type": 1, "created": "2026-09-02T10:00:00.000Z", "author": anna},
				map[string]any{"text": "стёрто", "type": 1, "created": "2026-09-02T11:00:00.000Z", "author": anna, "deleted": true},
			})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/latest/cards/") && strings.HasSuffix(r.URL.Path, "/comments") {
				send([]any{})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchKaitenWritesAPackageTaktReads(t *testing.T) {
	srv := fakeKaiten(t)
	env := func(k string) string {
		if k == "KAITEN_TOKEN" {
			return kaitenToken
		}
		return ""
	}
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"kaiten", "boards", "--url", srv.URL}, env, nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "345\tСклад · Поставки" {
		t.Fatalf("список досок — без пространства, куда не пускают: %q", out.String())
	}

	file := filepath.Join(t.TempDir(), "sklad.takt")
	out.Reset()
	err := run(context.Background(), []string{"kaiten", "fetch", "--url", srv.URL, "--board", "345", "--out", file},
		env, nil, &out, &errOut)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "досок 1, карточек 104") {
		t.Fatalf("итог — обе страницы карточек: %s", out.String())
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
		if bytes.Contains(data, []byte(kaitenToken)) {
			t.Fatalf("токен попал в пакет: %s", f.Name)
		}
	}
	pkg, err := pack.Read(raw)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Source.System != "kaiten" {
		t.Fatalf("источник: %+v", pkg.Manifest.Source)
	}
	plan, err := pkg.Plan(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan.Columns, ","); got != "Очередь,В работе: Делаем,В работе: Проверка,Готово" {
		t.Fatalf("колонки — по порядку доски, подколонки с именем родительской: %s", got)
	}
	if plan.ColumnKinds["очередь"] != "queue" || plan.ColumnKinds["в работе: проверка"] != "in_progress" || plan.ColumnKinds["готово"] != "done" {
		t.Fatalf("разметка по типу колонки, у подколонки без типа — от родительской: %v", plan.ColumnKinds)
	}
	by := map[string]importer.Card{}
	for _, c := range plan.Cards {
		by[c.Number] = c
	}
	release := by["501"]
	if release.Column != "В работе: Делаем" || release.Priority != "highest" || release.Estimate == nil || *release.Estimate != 5 ||
		release.Due == nil || release.Due.Format("2006-01-02") != "2026-10-01" ||
		// Иван без почты: план не выдумывает его, а просит сопоставить.
		len(release.Assignees) != 1 || len(release.Unmatched) != 1 {
		t.Fatalf("501: %+v", release)
	}
	if len(release.Links) != 0 {
		t.Fatalf("снятая блокировка стала связью: %+v", release.Links)
	}
	if !strings.Contains(strings.Join(release.Labels, ","), "Дорожка: Обычные") || !strings.Contains(strings.Join(release.Labels, ","), "релиз") {
		t.Fatalf("метки 501 — своя и дорожка: %v", release.Labels)
	}
	if len(release.Comments) != 2 || release.Comments[0].Text != "Сборка упала\nна тестах & линтере" ||
		release.Comments[0].AuthorName != "Иван Петров" {
		t.Fatalf("комментарии — HTML текстом, удалённый не едет: %+v", release.Comments)
	}
	if by["502"].Parent != "501" {
		t.Fatalf("родитель — первый с этой доски: %+v", by["502"])
	}
	if old := by["503"]; old.Done == nil || len(old.Links) != 1 || old.Links[0].Kind != "blocks" || old.Links[0].To != "502" {
		t.Fatalf("503 закрыта и держит 502: %+v", old)
	}
	if by["504"].Column != "В работе: Делаем" {
		t.Fatalf("карточка в колонке с подколонками — в первой подколонке: %+v", by["504"])
	}
	lost := strings.Join(plan.Lost, " | ")
	for _, want := range []string{"дорожки Kaiten стали метками: 2", "несколькими родителями — переедут под первым: 1", "без почты в ответе Kaiten: 1"} {
		if !strings.Contains(lost, want) {
			t.Fatalf("потери не названы (%s): %q", want, lost)
		}
	}
}

func TestFetchKaitenExplainsWhatIsMissing(t *testing.T) {
	srv := fakeKaiten(t)
	cases := []struct {
		args []string
		env  map[string]string
		want string
	}{
		{[]string{"kaiten", "boards"}, map[string]string{"KAITEN_TOKEN": kaitenToken}, "не назван адрес Kaiten"},
		{[]string{"kaiten", "boards", "--url", srv.URL}, nil, "KAITEN_TOKEN"},
		{[]string{"kaiten", "boards", "--url", srv.URL}, map[string]string{"KAITEN_TOKEN": "чужой"}, "Kaiten не принял токен"},
		{[]string{"kaiten", "fetch", "--url", srv.URL, "--out", "x.takt"}, map[string]string{"KAITEN_TOKEN": kaitenToken}, "не названа ни одна доска"},
		{[]string{"kaiten", "fetch", "--url", srv.URL, "--board", "9", "--out", filepath.Join(t.TempDir(), "x.takt")},
			map[string]string{"KAITEN_TOKEN": kaitenToken}, "в Kaiten нет такой доски"},
		{[]string{"kaiten", "boards", "--url", srv.URL}, map[string]string{"KAITEN_TOKEN": "чужой", "TAKT_LANG": "en"}, "Kaiten did not accept the token"},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		err := run(context.Background(), c.args, func(k string) string { return c.env[k] }, nil, &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: ждали «%s», получили %v", c.args, c.want, err)
		}
	}
}
