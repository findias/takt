package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/apiclient"
	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/idea"
)

// Идеи от людей команды (ROADMAP 43). Список и подача — в контракте
// (срез 4): бот из чата приносит идею так же, как человек, под своим
// именем ключа; разрешения — те же, что у досок: читать доски — читать
// идеи, писать — подавать. Голос и решение остаются вызовами
// интерфейса: это действия человека, в том числе наблюдателя, —
// поэтому обёртка human, а не роль.

func (s *Server) registerIdeaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ideas", s.scoped(apiclient.ScopeBoardsRead, s.handleIdeas))
	mux.HandleFunc("POST /api/ideas", s.scoped(apiclient.ScopeBoardsWrite, s.handleCreateIdea))
	mux.HandleFunc("PUT /api/ideas/{id}/vote", s.human(s.handleVoteIdea))
	mux.HandleFunc("DELETE /api/ideas/{id}/vote", s.human(s.handleUnvoteIdea))
	mux.HandleFunc("GET /api/ideas/{id}/decisions", s.human(s.handleIdeaDecisions))
	mux.HandleFunc("POST /api/ideas/{id}/decisions", s.human(s.handleDecideIdea))
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

func (s *Server) handleIdeaDecisions(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, idea.ErrNotFound.Error())
		return
	}
	list, err := s.ideas.Decisions(r.Context(), p.OrgID, p.ID, id)
	switch {
	case errors.Is(err, idea.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		s.fail(w, "история решений по идее", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"decisions": list, "kinds": idea.Kinds})
	}
}

// Решение по идее (ROADMAP 43, срез 2). Отказы, у которых есть поле
// в форме, идут с кодом; остальные — словами с общим кодом.
func (s *Server) handleDecideIdea(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, idea.ErrNotFound.Error())
		return
	}
	var req idea.DecideRequest
	if !decode(w, r, &req) {
		return
	}
	i, err := s.ideas.Decide(r.Context(), p.OrgID, p.ID, id, req, idea.Cards{Boards: s.boards})
	switch {
	case errors.Is(err, idea.ErrReasonRequired):
		writeCoded(w, http.StatusBadRequest, "idea_reason_required", err.Error())
	case errors.Is(err, idea.ErrBoardRequired):
		writeCoded(w, http.StatusBadRequest, "idea_board_required", err.Error())
	case errors.Is(err, idea.ErrBoardReadOnly):
		writeCoded(w, http.StatusBadRequest, "idea_board_readonly", err.Error())
	case errors.Is(err, idea.ErrTargetRequired):
		writeCoded(w, http.StatusBadRequest, "idea_target_required", err.Error())
	case errors.Is(err, idea.ErrTargetSelf):
		writeCoded(w, http.StatusBadRequest, "idea_target_self", err.Error())
	case errors.Is(err, idea.ErrKindUnknown), errors.Is(err, idea.ErrNoteLong):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, idea.ErrBoardEmpty):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, idea.ErrCannotDecide):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, idea.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case s.failAccess(w, "решение по идее", err):
	default:
		writeJSON(w, http.StatusCreated, i)
	}
}
