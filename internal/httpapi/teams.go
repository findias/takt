package httpapi

import (
	"errors"
	"net/http"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/team"
)

// Структура организации: дерево подразделений, их состав и наблюдение.
//
// Читать дерево может любой участник — кто с кем работает, не секрет.
// Менять его может владелец организации: раздача доступа — не рядовое
// действие, и до появления администратора подразделения (ROADMAP, 4.1)
// другого держателя этого права нет.

func (s *Server) registerTeamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/teams", s.authed(s.handleListTeams))
	mux.HandleFunc("POST /api/teams", s.authed(s.handleCreateTeam))
	mux.HandleFunc("PATCH /api/teams/{id}", s.authed(s.handleUpdateTeam))
	mux.HandleFunc("DELETE /api/teams/{id}", s.authed(s.handleArchiveTeam))

	mux.HandleFunc("GET /api/teams/{id}/members", s.authed(s.handleTeamMembers))
	// Состав подразделения меняет и его администратор, поэтому маршруты
	// не требуют владельца: решает политика.
	mux.HandleFunc("PUT /api/teams/{id}/members/{userId}", s.authed(s.handleAddTeamMember))
	mux.HandleFunc("DELETE /api/teams/{id}/members/{userId}", s.authed(s.handleRemoveTeamMember))

	mux.HandleFunc("GET /api/team-admins", s.authed(s.handleListAdmins))
	mux.HandleFunc("POST /api/team-admins", s.owner(s.handleGrantAdmin))
	mux.HandleFunc("DELETE /api/team-admins/{id}", s.owner(s.handleRevokeAdmin))

	mux.HandleFunc("GET /api/observers", s.authed(s.handleListObservers))
	// Наблюдение за поддеревом выдаёт и администратор этого поддерева,
	// поэтому маршрут не требует владельца: решает политика.
	mux.HandleFunc("POST /api/observers", s.authed(s.handleGrantObservation))
	mux.HandleFunc("DELETE /api/observers/{id}", s.owner(s.handleRevokeObservation))
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	teams, err := s.teams.List(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список команд", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parentId"`
	}
	if !decode(w, r, &req) {
		return
	}
	t, err := s.teams.Create(r.Context(), p.OrgID, p.ID, req.Name, req.ParentID)
	if s.failTeam(w, "создание команды", err) {
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// handleUpdateTeam меняет любое подмножество свойств: не присланное поле
// не трогается. Для родителя это значит, что «оставить как есть» и
// «сделать корневой» — разные намерения, и второе выражается явным null.
func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Name     *string `json:"name"`
		ParentID *string `json:"parentId"`
		Root     bool    `json:"root"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")

	if req.Name != nil {
		if err := s.teams.Rename(r.Context(), p.OrgID, p.ID, id, *req.Name); err != nil {
			s.failTeam(w, "переименование команды", err)
			return
		}
	}
	if req.ParentID != nil || req.Root {
		parent := req.ParentID
		if req.Root {
			parent = nil
		}
		if err := s.teams.Move(r.Context(), p.OrgID, p.ID, id, parent); err != nil {
			s.failTeam(w, "перенос команды", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleArchiveTeam(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.teams.Archive(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if errors.Is(err, team.ErrNotEmpty) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if s.failTeam(w, "архивация команды", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTeamMembers(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	members, err := s.teams.Members(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failTeam(w, "состав команды", err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (s *Server) handleAddTeamMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.teams.AddMember(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("userId"))
	if s.failTeam(w, "добавление в команду", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.teams.RemoveMember(r.Context(), p.OrgID, p.ID,
		r.PathValue("id"), r.PathValue("userId"))
	if s.failTeam(w, "исключение из команды", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListObservers(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	observers, err := s.teams.Observers(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список наблюдателей", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"observers": observers})
}

func (s *Server) handleGrantObservation(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		UserID string  `json:"userId"`
		TeamID *string `json:"teamId"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "не указан человек")
		return
	}
	o, err := s.teams.Grant(r.Context(), p.OrgID, p.ID, req.UserID, req.TeamID)
	if s.failTeam(w, "выдача наблюдения", err) {
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) handleRevokeObservation(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.teams.Revoke(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failTeam(w, "отзыв наблюдения", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// failTeam отвечает на ошибку и сообщает, что ответ уже написан.
//
// Отказ ограничений дерева — глубина, цикл, повторное наблюдение — это
// ошибка клиента, а не сбой сервера, и текст у него уже человеческий:
// он написан в миграции для того, кто это увидит.
func (s *Server) failTeam(w http.ResponseWriter, what string, err error) bool {
	var tree *team.TreeError
	switch {
	case err == nil:
		return false
	case errors.Is(err, team.ErrForbidden):
		writeError(w, http.StatusForbidden, "это может только владелец организации")
	case errors.Is(err, team.ErrNotFound):
		writeError(w, http.StatusNotFound, "не найдено")
	case errors.As(err, &tree):
		writeError(w, http.StatusConflict, tree.Reason)
	default:
		s.fail(w, what, err)
	}
	return true
}

func (s *Server) handleListAdmins(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	admins, err := s.teams.Admins(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список администраторов", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"admins": admins})
}

func (s *Server) handleGrantAdmin(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		UserID string `json:"userId"`
		TeamID string `json:"teamId"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.UserID == "" || req.TeamID == "" {
		writeError(w, http.StatusBadRequest, "нужны человек и подразделение")
		return
	}
	a, err := s.teams.GrantAdmin(r.Context(), p.OrgID, p.ID, req.UserID, req.TeamID)
	if s.failTeam(w, "назначение администратора", err) {
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleRevokeAdmin(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.teams.RevokeAdmin(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failTeam(w, "снятие администратора", err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
