package httpapi

import (
	"errors"
	"net/http"

	"github.com/konkov/agile/internal/apiclient"
	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/team"
)

// Структура организации: дерево подразделений, их состав и наблюдение.
//
// Читать дерево может любой участник — кто с кем работает, не секрет.
// Менять его может владелец организации и администратор подразделения
// в своей области (4.1): раздача доступа не рядовое действие, но и водить
// за каждой мелочью к владельцу в дереве из пяти уровней нельзя.
//
// Решает это политика, а не обёртка маршрута. Обёртка `s.owner` здесь
// была бы вторым источником правды о правах — и уже им побывала: отзыв
// наблюдения требовал владельца, тогда как выдачу политика разрешала
// администратору. Выдал и не снимешь.

func (s *Server) registerTeamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/teams", s.scoped(apiclient.ScopeStructureRead, s.handleListTeams))
	mux.HandleFunc("POST /api/teams", s.authed(s.handleCreateTeam))
	mux.HandleFunc("PATCH /api/teams/{id}", s.authed(s.handleUpdateTeam))
	mux.HandleFunc("DELETE /api/teams/{id}", s.authed(s.handleArchiveTeam))

	mux.HandleFunc("GET /api/teams/{id}/members", s.authed(s.handleTeamMembers))
	// Доски подразделения — второй ответ на вопрос «что это за узел»:
	// без него раскрытие узла давало только людей, а обещано это было
	// ещё этапом 2.1.
	mux.HandleFunc("GET /api/teams/{id}/boards", s.authed(s.handleTeamBoards))
	// Состав подразделения меняет и его администратор, поэтому маршруты
	// не требуют владельца: решает политика.
	mux.HandleFunc("PUT /api/teams/{id}/members/{userId}", s.authed(s.handleAddTeamMember))
	mux.HandleFunc("DELETE /api/teams/{id}/members/{userId}", s.authed(s.handleRemoveTeamMember))

	// Кто за что отвечает и кто за кем наблюдает — сведения о раздаче
	// доступа, и ключу они закрыты вместе с составом организации:
	// в них те же люди с почтами. Дерево без людей ключ читает
	// описанным /api/v1/teams.
	mux.HandleFunc("GET /api/team-admins", s.human(s.handleListAdmins))
	mux.HandleFunc("POST /api/team-admins", s.owner(s.handleGrantAdmin))
	mux.HandleFunc("DELETE /api/team-admins/{id}", s.owner(s.handleRevokeAdmin))

	mux.HandleFunc("GET /api/observers", s.human(s.handleListObservers))
	// Наблюдение за поддеревом выдаёт и снимает администратор этого
	// поддерева, поэтому маршруты не требуют владельца: решает политика.
	mux.HandleFunc("POST /api/observers", s.authed(s.handleGrantObservation))
	mux.HandleFunc("DELETE /api/observers/{id}", s.authed(s.handleRevokeObservation))
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

func (s *Server) handleTeamBoards(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	boards, err := s.teams.Boards(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if s.failTeam(w, "доски команды", err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
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
		// Отказ называет того, кто может: администратор подразделения
		// распоряжается своей областью, и «только владелец» отправило бы
		// его просить о том, что он и сам умеет.
		writeError(w, http.StatusForbidden,
			"это может владелец организации или администратор этого подразделения")
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
