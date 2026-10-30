package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/idea"
)

// Идеи от людей команды (ROADMAP 43, срез 1): список организации,
// подача и голос. Вызовы интерфейса, вне контракта: ключу интеграции
// идеи не нужны, а человеку — любому, в том числе наблюдателю, —
// нужны; поэтому обёртка human, а не роль.

func (s *Server) registerIdeaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ideas", s.human(s.handleIdeas))
	mux.HandleFunc("POST /api/ideas", s.human(s.handleCreateIdea))
	mux.HandleFunc("PUT /api/ideas/{id}/vote", s.human(s.handleVoteIdea))
	mux.HandleFunc("DELETE /api/ideas/{id}/vote", s.human(s.handleUnvoteIdea))
}

func (s *Server) handleIdeas(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	list, err := s.ideas.All(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список идей", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateIdea(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Title  string  `json:"title"`
		Body   string  `json:"body"`
		TeamID *string `json:"teamId"`
	}
	if !decode(w, r, &req) {
		return
	}
	i, err := s.ideas.Create(r.Context(), p.OrgID, p.ID, req.Title, req.Body, req.TeamID)
	switch {
	case errors.Is(err, idea.ErrEmptyTitle):
		// Свой код: форма кладёт отказ под поле названия, а не разбирает текст.
		writeCoded(w, http.StatusBadRequest, "idea_title_empty", err.Error())
	case errors.Is(err, idea.ErrTeamUnknown):
		writeCoded(w, http.StatusBadRequest, "idea_team_unknown", err.Error())
	case errors.Is(err, idea.ErrTitleLong):
		writeCoded(w, http.StatusBadRequest, "idea_title_long", err.Error())
	case errors.Is(err, idea.ErrBodyLong):
		writeCoded(w, http.StatusBadRequest, "idea_body_long", err.Error())
	case err != nil:
		s.fail(w, "новая идея", err)
	default:
		writeJSON(w, http.StatusCreated, i)
	}
}

func (s *Server) handleVoteIdea(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	s.voteIdea(w, r, p, true)
}

func (s *Server) handleUnvoteIdea(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	s.voteIdea(w, r, p, false)
}

func (s *Server) voteIdea(w http.ResponseWriter, r *http.Request, p auth.Principal, on bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, idea.ErrNotFound.Error())
		return
	}
	i, err := s.ideas.Vote(r.Context(), p.OrgID, p.ID, id, on)
	switch {
	case errors.Is(err, idea.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		s.fail(w, "голос за идею", err)
	default:
		writeJSON(w, http.StatusOK, i)
	}
}
