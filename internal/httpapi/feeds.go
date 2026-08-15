package httpapi

import (
	"net/http"
	"strconv"

	"github.com/konkov/agile/internal/apiclient"
	"github.com/konkov/agile/internal/auth"
)

// Ленты: события доски и административный журнал.
//
// Обе листаются курсором, а не номером страницы: журнал только
// дописывается, и смещение по номеру на растущей ленте показывает одно
// и то же дважды, а вклинившееся между запросами пропускает.

func (s *Server) registerFeedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/boards/{id}/events", s.authed(s.handleBoardEvents))
	mux.HandleFunc("GET /api/audit", s.scoped(apiclient.ScopeAuditRead, s.handleAudit))
}

func (s *Server) handleBoardEvents(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	feed, err := s.boards.Events(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.URL.Query().Get("cardId"), cursor(r))
	if err != nil {
		s.fail(w, "лента доски", err)
		return
	}
	writeJSON(w, http.StatusOK, feed)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	page, err := s.audit.List(r.Context(), p.OrgID, p.ID, cursor(r))
	if err != nil {
		s.fail(w, "журнал действий", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// cursor читает «показывать то, что старше вот этого». Мусор в параметре —
// не ошибка запроса, а просьба показать сначала: разбирать нечего, но
// и отказывать в ленте из-за испорченной ссылки незачем.
func cursor(r *http.Request) *int64 {
	raw := r.URL.Query().Get("before")
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}
