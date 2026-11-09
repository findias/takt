package httpapi

import (
	"errors"
	"net/http"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/google/uuid"
)

func (s *Server) registerVacationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/availability", s.human(s.handleAvailability))
	mux.HandleFunc("PUT /api/boards/{id}/vacation-warning-days", s.authed(s.handleVacationWarningDays))
	mux.HandleFunc("PUT /api/availability/timezone", s.human(s.handleAvailabilityZone))
	mux.HandleFunc("PUT /api/vacations/{id}", s.human(s.handleSaveVacation))
	mux.HandleFunc("PUT /api/coverages/{id}", s.human(s.handleProposeCoverage))
	mux.HandleFunc("PATCH /api/coverages/{id}", s.human(s.handleRespondCoverage))
}

func (s *Server) handleVacationWarningDays(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Days *int `json:"days"`
	}
	if !decode(w, r, &req) || !validAvailabilityID(w, r.PathValue("id")) {
		return
	}
	if !s.availabilityError(w, s.boards.SetVacationWarningDays(r.Context(), p.OrgID, p.ID, r.PathValue("id"), req.Days)) {
		w.WriteHeader(http.StatusNoContent)
	}
}
func (s *Server) availabilityError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, board.ErrAvailabilityChanged):
		writeCoded(w, 409, "availability_changed", err.Error())
	case errors.Is(err, board.ErrVacationOverlap):
		writeCoded(w, 409, "vacation_overlap", err.Error())
	case errors.Is(err, board.ErrCoverageInvalid):
		writeCoded(w, 409, "coverage_invalid", err.Error())
	case errors.Is(err, board.ErrNotFound):
		writeError(w, 404, "запись не найдена или больше вам недоступна")
	default:
		if errors.Is(err, board.ErrBadRequest) {
			writeError(w, 400, err.Error())
		} else if !s.failAccess(w, "отпуска и замещения", err) {
			s.fail(w, "отпуска и замещения", err)
		}
	}
	return true
}
func validAvailabilityID(w http.ResponseWriter, id string) bool {
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, 400, "укажите идентификатор записи UUID")
		return false
	}
	return true
}
func (s *Server) handleAvailability(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	boardID := r.URL.Query().Get("board")
	if boardID != "" && !validAvailabilityID(w, boardID) {
		return
	}
	data, err := s.boards.Availability(r.Context(), p.OrgID, p.ID, boardID)
	if !s.availabilityError(w, err) {
		writeJSON(w, 200, data)
	}
}
func (s *Server) handleAvailabilityZone(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		TimeZone string `json:"timeZone"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !s.availabilityError(w, s.boards.SetAvailabilityZone(r.Context(), p.OrgID, p.ID, req.TimeZone)) {
		w.WriteHeader(204)
	}
}
func (s *Server) handleSaveVacation(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var v board.Vacation
	if !decode(w, r, &v) || !validAvailabilityID(w, r.PathValue("id")) {
		return
	}
	v.ID = r.PathValue("id")
	out, err := s.boards.SaveVacation(r.Context(), p.OrgID, p.ID, v)
	if !s.availabilityError(w, err) {
		writeJSON(w, 200, out)
	}
}
func (s *Server) handleProposeCoverage(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var c board.Coverage
	if !decode(w, r, &c) || !validAvailabilityID(w, r.PathValue("id")) {
		return
	}
	c.ID = r.PathValue("id")
	for _, id := range []string{c.VacationID, c.CardID, c.SubstituteID} {
		if !validAvailabilityID(w, id) {
			return
		}
	}
	out, err := s.boards.ProposeCoverage(r.Context(), p.OrgID, p.ID, c)
	if !s.availabilityError(w, err) {
		writeJSON(w, 200, out)
	}
}
func (s *Server) handleRespondCoverage(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if !decode(w, r, &req) || !validAvailabilityID(w, r.PathValue("id")) {
		return
	}
	out, err := s.boards.RespondCoverage(r.Context(), p.OrgID, p.ID, r.PathValue("id"), req.Status, req.Version)
	if !s.availabilityError(w, err) {
		writeJSON(w, 200, out)
	}
}
