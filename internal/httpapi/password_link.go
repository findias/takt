package httpapi

import (
	"errors"
	"net/http"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/org"
)

// Ссылка «задать пароль» (ROADMAP 23.6): владелец выпускает, человек
// открывает, задаёт пароль и сразу входит. Токен едет в теле, как
// у приглашения: адреса попадают в логи прокси.

func (s *Server) handleIssuePasswordLink(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	link, err := s.orgs.IssuePasswordLink(r.Context(), p.OrgID, p.ID, r.PathValue("userId"), s.cfg.BaseURL)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, link)
	case errors.Is(err, org.ErrPasswordLinkOwn):
		writeCoded(w, http.StatusConflict, "password_link_own", err.Error())
	case errors.Is(err, org.ErrPasswordLinkElsewhere):
		writeCoded(w, http.StatusConflict, "password_link_elsewhere", err.Error())
	case errors.Is(err, org.ErrPasswordLinkFederated):
		writeCoded(w, http.StatusConflict, "password_link_federated", err.Error())
	case errors.Is(err, org.ErrServiceIdentity):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, org.ErrNotFound):
		writeError(w, http.StatusNotFound, "такого участника в организации нет")
	default:
		s.fail(w, "выпуск ссылки для входа", err)
	}
}

func (s *Server) handlePasswordLinkInfo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	info, err := s.orgs.LookupPasswordLink(r.Context(), req.Token)
	if errors.Is(err, org.ErrPasswordLinkInvalid) {
		writeCoded(w, http.StatusNotFound, "password_link_invalid", err.Error())
		return
	}
	if err != nil {
		s.fail(w, "чтение ссылки для входа", err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleUsePasswordLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	userID, orgID, err := s.orgs.UsePasswordLink(r.Context(), req.Token, req.Password)
	switch {
	case errors.Is(err, auth.ErrPasswordShort):
		writeCoded(w, http.StatusBadRequest, "password_short", err.Error())
		return
	case errors.Is(err, org.ErrPasswordLinkInvalid):
		writeCoded(w, http.StatusNotFound, "password_link_invalid", err.Error())
		return
	case err != nil:
		s.fail(w, "пароль по ссылке", err)
		return
	}
	// Сессия открывается в той организации, где выпустили ссылку: человек
	// шёл туда, а не в первую попавшуюся из своих.
	sessionID, expires, err := auth.CreateSession(r.Context(), s.db.Pool, userID)
	if err != nil {
		s.fail(w, "создание сессии", err)
		return
	}
	if err := auth.SwitchOrg(r.Context(), s.db.Pool, sessionID, userID, orgID); err != nil {
		s.fail(w, "выбор организации", err)
		return
	}
	auth.SetCookie(w, sessionID, expires, s.cfg.SecureCookies())
	principal, err := auth.PrincipalBySession(r.Context(), s.db.Pool, sessionID)
	if err != nil {
		s.fail(w, "чтение профиля", err)
		return
	}
	s.rememberLang(w, principal.Lang)
	writeJSON(w, http.StatusOK, principal)
}
