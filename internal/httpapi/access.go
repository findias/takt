package httpapi

import (
	"errors"
	"net/http"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/board"
)

// Доступ к доске: команда, видимость, поимённый состав.
//
// Право менять решает база, а не эти обработчики: писать в доску может
// тот, кому она доступна на запись, а состав закрытой доски раздаёт
// владелец организации. Здесь проверяется только то, что видно снаружи, —
// что человек вообще может изменять данные.

func (s *Server) registerAccessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/boards/{id}/access", s.authed(s.handleBoardAccess))
	mux.HandleFunc("PUT /api/boards/{id}/access", s.authed(s.handleSetBoardAccess))
	mux.HandleFunc("PUT /api/boards/{id}/members/{userId}", s.authed(s.handleAddBoardMember))
	mux.HandleFunc("DELETE /api/boards/{id}/members/{userId}", s.authed(s.handleRemoveBoardMember))
}

func (s *Server) handleBoardAccess(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	access, err := s.boards.Access(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failAccess(w, "доступ к доске", err) {
		return
	}
	writeJSON(w, http.StatusOK, access)
}

func (s *Server) handleSetBoardAccess(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Visibility string  `json:"visibility"`
		TeamID     *string `json:"teamId"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.boards.SetAccess(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), req.Visibility, req.TeamID)
	if s.failAccess(w, "смена видимости доски", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddBoardMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.AddMember(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("userId"))
	if s.failAccess(w, "добавление в доску", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveBoardMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	err := s.boards.RemoveMember(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("userId"))
	if s.failAccess(w, "исключение из доски", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// failAccess отвечает на ошибку и сообщает, что ответ уже написан.
//
// Потеря собственного доступа — конфликт, а не сбой: пользователь просит
// невозможного, и объяснить это надо словами, потому что база отказывает
// голым нарушением политики.
func (s *Server) failAccess(w http.ResponseWriter, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "доска не найдена")
	case errors.Is(err, board.ErrWouldLoseAccess):
		writeError(w, http.StatusConflict, board.ErrWouldLoseAccess.Error())
	case errors.Is(err, board.ErrTeamRequired):
		writeError(w, http.StatusBadRequest, board.ErrTeamRequired.Error())
	case errors.Is(err, board.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.fail(w, what, err)
	}
	return true
}
