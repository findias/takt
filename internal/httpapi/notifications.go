package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/auth"
)

// Уведомления внутри приложения (ROADMAP, этап 29). Только для людей:
// у ключа интеграции нет ни колокольчика, ни карточек «своих».
func (s *Server) registerNotificationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/notifications", s.human(s.handleNotifications))
	mux.HandleFunc("POST /api/notifications/read", s.human(s.handleNotificationsRead))
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
