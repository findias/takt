package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("запрос",
			"метод", r.Method,
			"путь", r.URL.Path,
			"код", rec.status,
			"мс", time.Since(started).Milliseconds())
	})
}

// Flush пропускает сброс буфера дальше.
//
// Без этого обёртка ради одной строчки в логе ломает потоковые ответы:
// поток изменений доски держит соединение открытым и обязан отдавать
// написанное сразу, а обёртка, не умеющая сбрасывать, превращает его
// в тишину. Ровно на это мы и наступили.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
