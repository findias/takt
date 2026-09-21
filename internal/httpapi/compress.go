package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Сжатие собранного клиента.
//
// До 21.09.2026 сервер отдавал скрипты как есть: основной кусок уходил
// браузеру в 394 КБ, хотя сжатым весит 115. Прокси у заказчика это
// иногда исправлял, а иногда нет — и тогда каждый первый заход
// с мобильной сети стоил втрое дороже, чем мог.
//
// Сжимаются только assets/: имена там с отпечатком содержимого, файл
// под именем не меняется никогда, и сжать его можно один раз, а дальше
// отдавать из памяти. Ключ — имя, размер и время правки: `web/dist`
// пересобирают под работающим сервером (CLAUDE.md), и подложенный
// файл с тем же именем не должен получить чужое сжатое тело.
//
// Сжимается текст, а не картинки и шрифты: те уже сжаты своим
// форматом, и второй раз ничего не выигрывается.

var compressible = map[string]bool{".js": true, ".css": true, ".svg": true, ".json": true, ".map": true}

type compressedKey struct {
	path  string
	size  int64
	mtime int64
}

var compressedCache sync.Map // compressedKey → []byte

// serveCompressed отдаёт сжатое, если клиент его понимает и файл того
// стоит. Возвращает false — и тогда файл уходит обычным путём.
func (s *Server) serveCompressed(w http.ResponseWriter, r *http.Request, root http.FileSystem, info fs.FileInfo) bool {
	ext := filepath.Ext(r.URL.Path)
	if !compressible[ext] {
		return false
	}
	// Vary — всем ответам этого пути, и сжатым, и нет: иначе прокси
	// отдаст сжатую копию тому, кто её не поймёт.
	w.Header().Add("Vary", "Accept-Encoding")
	if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return false
	}

	key := compressedKey{r.URL.Path, info.Size(), info.ModTime().UnixNano()}
	body, ok := compressedCache.Load(key)
	if !ok {
		f, err := root.Open(r.URL.Path)
		if err != nil {
			return false
		}
		raw, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return false
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := zw.Write(raw); err != nil {
			return false
		}
		if err := zw.Close(); err != nil {
			return false
		}
		body, _ = compressedCache.LoadOrStore(key, buf.Bytes())
	}

	data := body.([]byte)
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		return true
	}
	_, _ = w.Write(data)
	return true
}

// acceptsGzip — просит ли клиент gzip. «gzip;q=0» — это отказ, а не
// просьба: так клиент говорит, что сжатое ему не годится.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.TrimSpace(name) != "gzip" && strings.TrimSpace(name) != "*" {
			continue
		}
		if q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q="); ok {
			if v, err := strconv.ParseFloat(q, 64); err == nil && v == 0 {
				return false
			}
		}
		return true
	}
	return false
}
