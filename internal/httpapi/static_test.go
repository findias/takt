package httpapi

import (
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/findias/takt/internal/config"
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

// Путь наружу не отдаёт чужого файла и не выдаёт, что он есть.
//
// Отдачей занимается http.Dir, и она за свой каталог не выходит.
// Спрашивать «есть ли такой файл» надо у неё же: пока это делал
// os.Stat по склеенному пути, «что наше» знали двое порознь. Дыры
// в этом не было — `filepath.Clean` отсчитывает от ведущей косой
// черты, — но безопасность держалась на доводе, а не на устройстве,
// и статический разбор был прав, называя это находкой.
//
// Проверяется именно обработчик статики, а не сервер целиком: и
// клиент, и мультиплексор `..` из пути вычищают сами — через них
// до обработчика доезжает уже безобидное, и проверка мерила бы их
// осторожность вместо его.
//
// Свойство держалось и прежде, просто держалось на доводе. Проверка
// закрепляет его, а не чинит: сломать её можно одной строкой —
// склейкой пути руками, и тогда она называет утёкший файл.
func TestPathOutsideTheWebDirTellsNothing(t *testing.T) {
	корень := t.TempDir()
	клиент := filepath.Join(корень, "web")
	if err := os.Mkdir(клиент, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(клиент, "index.html"), "<!doctype html><title>Доска</title>")
	write(t, filepath.Join(корень, "тайна.txt"), "пароль от базы")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	impl := New(config.Config{BaseURL: "http://example.test", WebDir: клиент}, nil, log, nil)
	обработчик := impl.staticHandler()

	ответ := func(путь string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "http://example.test/", nil)
		req.URL.Path = путь
		w := httptest.NewRecorder()
		обработчик.ServeHTTP(w, req)
		return w
	}

	наружу := ответ("/../тайна.txt")
	if strings.Contains(наружу.Body.String(), "пароль от базы") {
		t.Fatal("файл за пределами каталога клиента уехал в ответ")
	}

	// Существование чужого файла не должно быть заметно ничем: ответ
	// на путь к нему обязан совпадать с ответом на путь в никуда.
	// Иначе отдача статики превращается в способ спросить, что лежит
	// на сервере рядом.
	мимо := ответ("/../такого-нет.txt")
	if наружу.Code != мимо.Code || наружу.Body.String() != мимо.Body.String() {
		t.Errorf("по чужому файлу ответ %d/%d байт, по несуществующему — %d/%d: "+
			"разница отвечает на вопрос, есть ли он",
			наружу.Code, наружу.Body.Len(), мимо.Code, мимо.Body.Len())
	}
}

// Собранный клиент уходит сжатым тому, кто это умеет.
//
// До 21.09.2026 сервер отдавал скрипты как есть: основной кусок уходил
// в браузер 394 КБ, хотя сжатым весил 115. Порог размера сборки
// считает сжатый вес (e2e/perf.spec.ts) — и верен он, только если
// сервер действительно сжимает; иначе проверка мерила бы то, чего
// человек не получает.
func TestBuiltClientTravelsCompressed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "index.html"), "<!doctype html><title>Доска</title>")
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := strings.Repeat("console.log('доска');\n", 200)
	write(t, filepath.Join(dir, "assets", "index-abc123.js"), script)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	impl := New(config.Config{BaseURL: "http://example.test", WebDir: dir}, nil, log, nil)
	srv := httptest.NewServer(impl.Handler())
	t.Cleanup(srv.Close)

	get := func(encoding string) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+"/assets/index-abc123.js", nil)
		if encoding != "" {
			req.Header.Set("Accept-Encoding", encoding)
		}
		// Транспорт сам просит gzip и сам разжимает — тогда проверять
		// было бы нечего. Выключено, чтобы видеть ответ как есть.
		client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}

	res := get("gzip, deflate, br")
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("скрипт ушёл несжатым: Content-Encoding %q", res.Header.Get("Content-Encoding"))
	}
	if !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
		t.Error("нет Vary: Accept-Encoding — прокси отдаст сжатое тому, кто его не поймёт")
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("сжатие потеряло кэш: %q", got)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type %q: браузер не исполнит скрипт с чужим типом", ct)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if string(body) != script {
		t.Error("разжатое не совпало с файлом")
	}

	// Кто не умеет — получает как есть.
	plain := get("")
	if plain.Header.Get("Content-Encoding") != "" {
		t.Errorf("сжатое тому, кто не просил: %q", plain.Header.Get("Content-Encoding"))
	}
	raw, _ := io.ReadAll(plain.Body)
	if string(raw) != script {
		t.Error("несжатый ответ не совпал с файлом")
	}
}
