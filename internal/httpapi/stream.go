package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
)

// Поток изменений доски.
//
// Server-Sent Events, а не вебсокет, и это не экономия сил. Разговор
// здесь односторонний: клиент отправляет изменения обычными запросами
// и ждёт от сервера только «доска доехала до такой-то версии». Для этого
// вебсокет — лишний рукопожатием, лишний зависимостью и лишний тем, что
// переподключение придётся писать самому, тогда как EventSource умеет
// это сам.

func (s *Server) registerStreamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/boards/{id}/stream", s.authed(s.handleStream))
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	boardID := r.PathValue("id")

	// Право читать проверяется один раз, на входе: доска недоступна —
	// и потока не будет. Отзыв доступа посреди подписки закроет поток
	// не сразу, и это осознанно: подписка живёт минуты, а не дни.
	if _, err := s.boards.Snapshot(r.Context(), p.OrgID, p.ID, boardID); err != nil {
		if errors.Is(err, board.ErrNotFound) {
			writeError(w, http.StatusNotFound, "доска не найдена")
			return
		}
		s.fail(w, "поток изменений", err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "поток не поддерживается")
		return
	}

	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-store")
	// Прокси любят копить ответ; для потока это означает тишину до конца
	// света. Заголовок понимает nginx и ему подобные.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	changes, unsubscribe := s.hub.Subscribe(boardID)
	defer unsubscribe()

	// Пульс нужен не нам, а тому, кто посередине: прокси и мобильные сети
	// закрывают молчащее соединение. Комментарий SSE клиент игнорирует.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": пульс\n\n")
			flusher.Flush()
		case change, ok := <-changes:
			if !ok {
				return
			}
			body, err := json.Marshal(change)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: board\ndata: %s\n\n", body)
			flusher.Flush()
		}
	}
}
