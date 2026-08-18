package httpapi

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"net/http"
	"strings"
)

// Описание контракта машиночитаемым файлом, а не текстом в README.
//
// Текстовое описание расходится с кодом молча и обнаруживается тем, кто
// по нему что-то написал. Файл хотя бы можно скормить генератору клиента
// и увидеть расхождение сразу.
//
// Файл лежит рядом и встраивается в бинарник: у нас один образ, и
// описание обязано ехать вместе с той версией, которую описывает.

//go:embed openapi.json
var openapiDocument []byte

// Отрисованная страница поверх того же файла: читать контракт глазами
// по сырому JSON можно, но описания у нас длинные и объясняют «почему», —
// а именно их и приходит читать тот, кто внедряет интеграцию.
//
// Отрисовщик лежит рядом сжатым и едет в бинарнике, а не тянется
// с чужого адреса: образ ставят и там, где интернета нет, и страница,
// которая в такой установке пустая, хуже её отсутствия. Сжатый он
// втрое меньше, а браузеру почти всегда достаётся как есть.
//
// Redoc, а не Swagger UI: тот вдвое тяжелее и умеет «попробовать
// запрос» — кнопку, которая пишет в настоящую доску настоящей
// организации. Читателю контракта она не нужна, а следы от неё
// приходится потом разбирать.

//go:embed docs/redoc-2.5.3.js.gz
var redocBundle []byte

//go:embed docs/index.html
var docsPage []byte

func (s *Server) registerContractRoutes(mux *http.ServeMux) {
	// Путь внутренний, без версии: обёртка versioned срежет её раньше,
	// чем маршрут дойдёт до мультиплексора. Снаружи это по-прежнему
	// /api/v1/openapi.json.
	//
	// Без ключа: описание — не данные организации, а обещание сервера.
	// Требовать вход, чтобы прочитать, как войти, — замкнутый круг.
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json; charset=utf-8")
		w.Header().Set("API-Version", Version)
		_, _ = w.Write(openapiDocument)
	})

	// Адреса внутри страницы — с версией и от корня: открывают её
	// по /api/v1/docs, и относительная ссылка увела бы браузер
	// на /api/v1/redoc.js, которого нет.
	page := bytes.ReplaceAll(docsPage, []byte("{{version}}"), []byte(Version))
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		w.Header().Set("API-Version", Version)
		w.Header().Set("content-security-policy", docsPolicy)
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET /api/docs/redoc.js", func(w http.ResponseWriter, r *http.Request) {
		writeGzipped(w, r, "application/javascript; charset=utf-8", redocBundle)
	})
}

// Политика запрещает странице ходить куда бы то ни было наружу.
//
// Проверять это на глаз бесполезно: своей разметке верить можно,
// а отрисовщику нельзя — Redoc в подписи «powered by» просит картинку
// с cdn.redoc.ly, и обещание «работает там, где интернета нет»
// держалось бы на честном слове чужой сборки. Запрет надёжнее уговора:
// картинки — только свои и встроенные, соединения — только к себе
// (за описанием), стили — свои и внутристрочные, потому что отрисовщик
// собирает их на ходу.
const docsPolicy = "default-src 'none'; script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
	"font-src 'self' data:; connect-src 'self'; worker-src blob:; " +
	"base-uri 'none'; form-action 'none'"

// writeGzipped отдаёт заранее сжатое как есть тому, кто говорит, что
// понимает gzip, и разжимает на лету остальным. Хранить обе копии
// в бинарнике незачем, а полагаться на то, что заголовок пришлют все,
// нельзя: без него страница получила бы вместо кода двоичный мусор.
func writeGzipped(w http.ResponseWriter, r *http.Request, contentType string, compressed []byte) {
	w.Header().Set("content-type", contentType)
	// Час, а не «навсегда»: адрес не несёт версии, и вечный кеш
	// пришлось бы обходить переименованием при каждом обновлении.
	w.Header().Set("cache-control", "public, max-age=3600")
	w.Header().Set("vary", "accept-encoding")
	if strings.Contains(r.Header.Get("accept-encoding"), "gzip") {
		w.Header().Set("content-encoding", "gzip")
		_, _ = w.Write(compressed)
		return
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		http.Error(w, "встроенный файл повреждён", http.StatusInternalServerError)
		return
	}
	defer reader.Close()
	_, _ = io.Copy(w, reader)
}
