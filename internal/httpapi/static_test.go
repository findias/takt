package httpapi

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/konkov/agile/internal/config"
)

// Кэш собранного клиента.
//
// Проверка появилась после того, как правка выглядела неработающей:
// сервер был новый, сборка новая, а браузер грузил прежний скрипт —
// index.html он держал по своему усмотрению, потому что заголовков
// про кэш не было вовсе. Молчание здесь толкуется в худшую сторону,
// и увидеть это можно только снаружи: своя разметка ни при чём.
//
// Сервер поднимается без базы: статика о ней не знает, а требовать
// TEST_DATABASE_URL ради двух заголовков значило бы пропускать
// проверку там, где базы нет.
func TestBuiltClientSaysHowLongToKeepIt(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "index.html"), "<!doctype html><title>Доска</title>")
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "assets", "index-abc123.js"), "console.log(1)")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	impl := New(config.Config{BaseURL: "http://example.test", WebDir: dir}, nil, log, nil)
	srv := httptest.NewServer(impl.Handler())
	t.Cleanup(srv.Close)

	cases := []struct {
		path  string
		cache string
		why   string
	}{
		// Отпечаток в имени файла делает вечное хранение безопасным:
		// новая сборка приносит новое имя.
		{"/assets/index-abc123.js", "public, max-age=31536000, immutable", "содержимое с отпечатком в имени"},
		// index.html не кэшируется: он и есть то, что называет имена
		// свежей сборки.
		{"/index.html", "no-cache", "оболочка клиента"},
		// Любой путь приложения отдаёт ту же оболочку — и с тем же
		// заголовком: маршрут клиента не должен кэшироваться дольше
		// самого клиента.
		{"/board/какая-нибудь", "no-cache", "маршрут клиента"},
	}
	for _, c := range cases {
		res, err := srv.Client().Get(srv.URL + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		res.Body.Close()
		if got := res.Header.Get("Cache-Control"); got != c.cache {
			t.Errorf("%s (%s): Cache-Control %q, ожидался %q", c.path, c.why, got, c.cache)
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
