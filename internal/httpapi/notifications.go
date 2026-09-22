package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/auth"
)

// Уведомления внутри приложения (ROADMAP, этап 29). Только для людей:
// у ключа интеграции нет ни колокольчика, ни карточек «своих».
func (s *Server) registerNotificationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/notifications", s.human(s.handleNotifications))
	mux.HandleFunc("POST /api/notifications/read", s.human(s.handleNotificationsRead))
	mux.HandleFunc("GET /api/notifications/stream", s.human(s.handleNotificationsStream))
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	page, err := s.boards.Notifications(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "чтение уведомлений", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// Прочитано: перечисленные — или все разом, если список пуст.
func (s *Server) handleNotificationsRead(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	for _, id := range req.IDs {
		if _, err := uuid.Parse(id); err != nil {
			writeError(w, http.StatusBadRequest, "уведомление называется идентификатором, а это не он")
			return
		}
	}
	if err := s.boards.MarkNotificationsRead(r.Context(), p.OrgID, p.ID, req.IDs); err != nil {
		s.fail(w, "отметка уведомлений", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Поток своих уведомлений: колокольчик в шапке узнаёт о новом сразу,
// на какой бы странице человек ни был, — поток доски знает только
// открытую доску. Несёт он не сами уведомления, а весть «перечитай»:
// видимость перепроверяет чтение, и в поток ей незачем.
func (s *Server) handleNotificationsStream(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "поток не поддерживается")
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	changes, unsubscribe := s.hub.SubscribeUser(p.ID)
	defer unsubscribe()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": пульс\n\n")
			flusher.Flush()
		case _, ok := <-changes:
			if !ok {
				return
			}
			fmt.Fprint(w, "event: notifications\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}

// Какие поводы уведомлений человек выключил. Весь список разом:
// экран показывает все поводы флажками и отправляет их состояние.
func (s *Server) handleMuteNotifications(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Muted []string `json:"muted"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := auth.SetMutedNotifications(r.Context(), s.db.Pool, p.ID, req.Muted)
	switch {
	case errors.Is(err, auth.ErrUnknownReason):
		writeError(w, http.StatusBadRequest, auth.ErrUnknownReason.Error())
	case err != nil:
		s.fail(w, "выбор поводов уведомлений", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
