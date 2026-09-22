package httpapi

import (
	"errors"
	"net/http"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/org"
)

// Смена почты (ROADMAP 23.6): сам человек — в профиле, владелец —
// участнику в «Команде». Отказы у обеих одни и те же и различаются
// кодом: форма кладёт отказ под поле, к которому он относится.

func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	// Ключом почту не меняют по той же причине, что и пароль: это имя
	// для входа человека, а у ключа входа нет.
	if _, byKey := scopesOf(r); byKey {
		writeError(w, http.StatusForbidden, "ключом почту не меняют: это действие человека")
		return
	}
	var req struct {
		Current string `json:"current"`
		Email   string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	email, err := auth.ChangeEmail(r.Context(), s.db.Pool, p.ID, req.Current, req.Email)
	s.writeEmailResult(w, email, err, "смена своей почты")
}

func (s *Server) handleSetMemberEmail(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	email, err := s.orgs.SetMemberEmail(r.Context(), p.OrgID, p.ID, r.PathValue("userId"), req.Email)
	s.writeEmailResult(w, email, err, "смена почты участника")
}

func (s *Server) writeEmailResult(w http.ResponseWriter, email string, err error, what string) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"email": email})
	case errors.Is(err, auth.ErrEmailInvalid):
		writeCoded(w, http.StatusBadRequest, "email_invalid", err.Error())
	case errors.Is(err, auth.ErrEmailSame):
		writeCoded(w, http.StatusBadRequest, "email_same", err.Error())
	case errors.Is(err, auth.ErrEmailTaken):
		writeCoded(w, http.StatusConflict, "email_taken", err.Error())
	case errors.Is(err, auth.ErrEmailManaged):
		writeCoded(w, http.StatusConflict, "email_managed", err.Error())
	case errors.Is(err, org.ErrEmailElsewhere):
		writeCoded(w, http.StatusConflict, "email_elsewhere", err.Error())
	case errors.Is(err, org.ErrEmailOwn):
		writeCoded(w, http.StatusConflict, "email_own", err.Error())
	case errors.Is(err, org.ErrServiceIdentity):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, auth.ErrWrongPassword):
		writeCoded(w, http.StatusForbidden, "password_wrong", err.Error())
	case errors.Is(err, org.ErrNotFound):
		writeError(w, http.StatusNotFound, "такого участника в организации нет")
	default:
		s.fail(w, what, err)
	}
}
