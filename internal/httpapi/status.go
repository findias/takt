package httpapi

import (
	"errors"
	"net/http"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/google/uuid"
)

// Статус эпика словами (ROADMAP 42.3, срез 10): история — экрану,
// новая запись — тому, кому портфель открыт на запись. Вызовы
// интерфейса, вне контракта, как прогноз эпика: подписчик узнаёт
// о статусе из события card.status_set.

func (s *Server) registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cards/{id}/statuses", s.human(s.handleEpicStatuses))
	mux.HandleFunc("POST /api/boards/{id}/cards/{cardId}/statuses", s.human(s.handleSetEpicStatus))
}

func (s *Server) handleEpicStatuses(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "карточки нет или её доска вам закрыта — попросите ссылку у того, кто её прислал")
		return
	}
	list, err := s.boards.EpicStatuses(r.Context(), p.OrgID, p.ID, id)
	switch {
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "карточки нет или её доска вам закрыта — попросите ссылку у того, кто её прислал")
	case err != nil:
		s.fail(w, "статусы эпика", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"statuses": list, "kinds": board.EpicStatusKinds})
	}
}

func (s *Server) handleSetEpicStatus(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	e, err := s.boards.SetEpicStatus(r.Context(), p.OrgID, p.ID, r.PathValue("id"), r.PathValue("cardId"),
		req.Status, req.Note)
	switch {
	case errors.Is(err, board.ErrUnknownStatus):
		writeCoded(w, http.StatusBadRequest, "status_unknown", err.Error())
	case errors.Is(err, board.ErrNotEpic):
		// Не «не найдено»: карточка есть, просто она не эпик. Свой код —
		// клиент прячет форму, а не разбирает текст.
		writeCoded(w, http.StatusConflict, "status_not_epic", err.Error())
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "карточки нет или её доска вам закрыта — попросите ссылку у того, кто её прислал")
	case s.failAccess(w, "статус эпика", err):
	default:
		writeJSON(w, http.StatusCreated, e)
	}
}
