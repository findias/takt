package httpapi

import (
	"errors"
	"net/http"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/org"
)

// Смена имени (ROADMAP 36.2): своего — в личных настройках, участника —
// владельцем на «Команде». До этого имя задавалось один раз, при
// регистрации или приглашении, и опечатка в нём жила вечно.

func (s *Server) handleChangeName(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	// Ключом имя не меняют по той же причине, что почту: у ключа нет
	// личных настроек, его имя — имя интеграции, и ведёт его владелец.
	if _, byKey := scopesOf(r); byKey {
		writeError(w, http.StatusForbidden, "ключом имя не меняют: это действие человека")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, err := auth.ChangeName(r.Context(), s.db.Pool, p.ID, req.Name)
	s.writeNameResult(w, name, err, "смена своего имени")
}

func (s *Server) handleSetMemberName(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, err := s.orgs.SetMemberName(r.Context(), p.OrgID, p.ID, r.PathValue("userId"), req.Name)
	s.writeNameResult(w, name, err, "смена имени участника")
}

func (s *Server) writeNameResult(w http.ResponseWriter, name string, err error, what string) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"name": name})
	case errors.Is(err, auth.ErrNameEmpty), errors.Is(err, auth.ErrNameTooLong):
		writeCoded(w, http.StatusBadRequest, "name_invalid", err.Error())
	case errors.Is(err, auth.ErrNameSame):
		writeCoded(w, http.StatusBadRequest, "name_same", err.Error())
	case errors.Is(err, auth.ErrNameManaged):
		writeCoded(w, http.StatusConflict, "name_managed", err.Error())
	case errors.Is(err, org.ErrNameElsewhere):
		writeCoded(w, http.StatusConflict, "name_elsewhere", err.Error())
	case errors.Is(err, org.ErrNameOwn):
		writeCoded(w, http.StatusConflict, "name_own", err.Error())
	case errors.Is(err, org.ErrServiceIdentity):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, org.ErrNotFound):
		writeError(w, http.StatusNotFound, "такого участника в организации нет")
	default:
		s.fail(w, what, err)
	}
}
