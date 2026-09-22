package httpapi

import (
	"errors"
	"net/http"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
)

// Задачи человека со всех досок, которые видит спрашивающий
// (board.Tasks). Без ?user= — свои.
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	person := r.URL.Query().Get("user")
	if person == "" {
		person = p.ID
	}
	list, err := s.boards.Tasks(r.Context(), p.OrgID, p.ID, person, r.URL.Query().Get("done") == "1")
	switch {
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "такого участника в организации нет")
	case err != nil:
		s.fail(w, "задачи человека", err)
	default:
		writeJSON(w, http.StatusOK, list)
	}
}
