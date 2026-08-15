// Package httpapi — HTTP-слой: маршруты, сессии, раздача фронтенда.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/konkov/agile/internal/audit"
	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/board"
	"github.com/konkov/agile/internal/config"
	"github.com/konkov/agile/internal/org"
	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/team"
)

type Server struct {
	cfg    config.Config
	db     *store.Store
	boards *board.Service
	orgs   *org.Service
	teams  *team.Service
	audit  *audit.Service
	log    *slog.Logger
}

func New(cfg config.Config, db *store.Store, log *slog.Logger) *Server {
	return &Server{
		cfg:    cfg,
		db:     db,
		boards: board.New(db),
		orgs:   org.New(db),
		teams:  team.New(db),
		audit:  audit.New(db),
		log:    log,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.authed(s.handleMe))

	// Организации: список своих, создание новой, переключение активной.
	mux.HandleFunc("GET /api/orgs", s.authed(s.handleListOrgs))
	mux.HandleFunc("POST /api/orgs", s.authed(s.handleCreateOrg))
	mux.HandleFunc("POST /api/session/org", s.authed(s.handleSwitchOrg))

	// Команда активной организации.
	mux.HandleFunc("GET /api/team", s.authed(s.handleTeam))
	mux.HandleFunc("POST /api/invites", s.owner(s.handleInvite))
	mux.HandleFunc("DELETE /api/invites/{id}", s.owner(s.handleRevokeInvite))
	mux.HandleFunc("PUT /api/members/{userId}/role", s.owner(s.handleSetRole))
	mux.HandleFunc("DELETE /api/members/{userId}", s.owner(s.handleRemoveMember))

	// Приглашение открывают по секретной ссылке — до входа и до аккаунта.
	mux.HandleFunc("GET /api/invites/{token}/info", s.handleInviteInfo)
	mux.HandleFunc("POST /api/invites/{token}/accept", s.handleAcceptInvite)

	s.registerTeamRoutes(mux)
	s.registerAccessRoutes(mux)
	s.registerFeedRoutes(mux)

	mux.HandleFunc("GET /api/boards", s.authed(s.handleListBoards))
	mux.HandleFunc("POST /api/boards", s.authed(s.handleCreateBoard))
	mux.HandleFunc("GET /api/boards/{id}", s.authed(s.handleSnapshot))
	mux.HandleFunc("POST /api/boards/{id}/operations", s.authed(s.handleOperation))

	if s.cfg.WebDir != "" {
		mux.Handle("/", s.staticHandler())
	}
	return logRequests(s.log, mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Pool.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "база недоступна")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- доступ ---

// authed требует активную сессию и подставляет автора запроса вместе
// с его ролью в активной организации.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.CookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "нужно войти")
			return
		}
		principal, err := auth.PrincipalBySession(r.Context(), s.db.Pool, cookie.Value)
		if err != nil {
			auth.ClearCookie(w, s.cfg.SecureCookies())
			writeError(w, http.StatusUnauthorized, "сессия истекла, войдите заново")
			return
		}
		next(w, r, principal)
	}
}

// owner дополнительно требует роль владельца в активной организации.
func (s *Server) owner(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		if !p.CanAdmin() {
			writeError(w, http.StatusForbidden, "это может только владелец организации")
			return
		}
		next(w, r, p)
	})
}

// --- аутентификация ---

type registerRequest struct {
	Org      string `json:"org"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleRegister заводит личность и её первую организацию.
//
// Этап 0: регистрация открыта. Присоединиться к существующей организации
// можно только по приглашению — самостоятельно вписать себя в чужую
// команду нельзя.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Name = strings.TrimSpace(req.Name)
	req.Org = strings.TrimSpace(req.Org)
	if req.Org == "" {
		req.Org = "Моя команда"
	}
	if msg := validateSignup(req.Email, req.Name, req.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	userID, err := s.createUser(r, req.Email, req.Name, req.Password)
	if errors.Is(err, errEmailTaken) {
		writeError(w, http.StatusConflict, "такая почта уже зарегистрирована — войдите")
		return
	}
	if err != nil {
		s.fail(w, "создание пользователя", err)
		return
	}
	if _, err := s.orgs.Create(r.Context(), req.Org, userID); err != nil {
		s.fail(w, "создание организации", err)
		return
	}
	s.startSession(w, r, userID)
}

var errEmailTaken = errors.New("почта занята")

func (s *Server) createUser(r *http.Request, email, name, password string) (string, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	var id string
	err = s.db.Pool.QueryRow(r.Context(), `
		insert into users (email, name, password_hash) values ($1, $2, $3) returning id`,
		email, name, hash).Scan(&id)
	if err != nil && strings.Contains(err.Error(), "users_email_key") {
		return "", errEmailTaken
	}
	return id, err
}

func validateSignup(email, name, password string) string {
	switch {
	case email == "" || !strings.Contains(email, "@"):
		return "укажите почту"
	case name == "":
		return "укажите имя"
	case len(password) < auth.MinPasswordLen:
		return "пароль должен быть не короче 8 символов"
	}
	return ""
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	user, err := auth.Authenticate(r.Context(), s.db.Pool, req.Email, req.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
		writeError(w, http.StatusUnauthorized, "неверная почта или пароль")
		return
	}
	if err != nil {
		s.fail(w, "проверка пароля", err)
		return
	}
	s.startSession(w, r, user.ID)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) {
	sessionID, expires, err := auth.CreateSession(r.Context(), s.db.Pool, userID)
	if errors.Is(err, auth.ErrNoMembership) {
		writeError(w, http.StatusForbidden, "вас исключили из всех организаций — попросите новое приглашение")
		return
	}
	if err != nil {
		s.fail(w, "создание сессии", err)
		return
	}
	auth.SetCookie(w, sessionID, expires, s.cfg.SecureCookies())

	principal, err := auth.PrincipalBySession(r.Context(), s.db.Pool, sessionID)
	if err != nil {
		s.fail(w, "чтение профиля", err)
		return
	}
	writeJSON(w, http.StatusOK, principal)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		_ = auth.DeleteSession(r.Context(), s.db.Pool, cookie.Value)
	}
	auth.ClearCookie(w, s.cfg.SecureCookies())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, p auth.Principal) {
	writeJSON(w, http.StatusOK, p)
}

// --- организации ---

func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	memberships, err := auth.Memberships(r.Context(), s.db.Pool, p.ID)
	if err != nil {
		s.fail(w, "список организаций", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orgs": memberships, "activeOrgId": p.OrgID})
}

func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "у организации должно быть название")
		return
	}
	membership, err := s.orgs.Create(r.Context(), req.Name, p.ID)
	if err != nil {
		s.fail(w, "создание организации", err)
		return
	}
	if err := auth.SwitchOrg(r.Context(), s.db.Pool, p.SessionID, p.ID, membership.OrgID); err != nil {
		s.fail(w, "переключение на новую организацию", err)
		return
	}
	writeJSON(w, http.StatusCreated, membership)
}

func (s *Server) handleSwitchOrg(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		OrgID string `json:"orgId"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := auth.SwitchOrg(r.Context(), s.db.Pool, p.SessionID, p.ID, req.OrgID)
	if errors.Is(err, auth.ErrNoMembership) {
		// Чужая организация неотличима от несуществующей.
		writeError(w, http.StatusForbidden, "у вас нет доступа к этой организации")
		return
	}
	if err != nil {
		s.fail(w, "переключение организации", err)
		return
	}
	principal, err := auth.PrincipalBySession(r.Context(), s.db.Pool, p.SessionID)
	if err != nil {
		s.fail(w, "чтение профиля", err)
		return
	}
	writeJSON(w, http.StatusOK, principal)
}

// --- команда ---

func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	members, err := s.orgs.Members(r.Context(), p.OrgID)
	if err != nil {
		s.fail(w, "состав команды", err)
		return
	}
	body := map[string]any{"members": members, "invites": []any{}}
	// Список приглашений — сведения для администрирования, рядовым
	// участникам он не нужен.
	if p.CanAdmin() {
		invites, err := s.orgs.PendingInvites(r.Context(), p.OrgID)
		if err != nil {
			s.fail(w, "список приглашений", err)
			return
		}
		body["invites"] = invites
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Role == "" {
		req.Role = auth.RoleMember
	}
	invite, err := s.orgs.Invite(r.Context(), p.OrgID, p.ID, req.Email, req.Role, s.cfg.BaseURL)
	switch {
	case errors.Is(err, org.ErrAlreadyMember):
		writeError(w, http.StatusConflict, "этот человек уже в команде")
	case err != nil && !errors.Is(err, org.ErrNotFound):
		if isUserError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.fail(w, "создание приглашения", err)
	default:
		writeJSON(w, http.StatusCreated, invite)
	}
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.orgs.RevokeInvite(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if errors.Is(err, org.ErrNotFound) {
		writeError(w, http.StatusNotFound, "приглашение не найдено")
		return
	}
	if err != nil {
		s.fail(w, "отзыв приглашения", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetRole(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.orgs.SetRole(r.Context(), p.OrgID, p.ID, r.PathValue("userId"), req.Role)
	s.writeMembershipResult(w, err, "смена роли")
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.orgs.Remove(r.Context(), p.OrgID, p.ID, r.PathValue("userId"))
	s.writeMembershipResult(w, err, "исключение из команды")
}

func (s *Server) writeMembershipResult(w http.ResponseWriter, err error, what string) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, org.ErrNotFound):
		writeError(w, http.StatusNotFound, "участник не найден")
	case errors.Is(err, org.ErrLastOwner):
		writeError(w, http.StatusConflict, "в организации должен остаться хотя бы один владелец")
	default:
		s.fail(w, what, err)
	}
}

// --- приглашения по ссылке ---

func (s *Server) handleInviteInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.orgs.LookupInvite(r.Context(), r.PathValue("token"))
	if errors.Is(err, org.ErrInviteInvalid) {
		writeError(w, http.StatusNotFound, "ссылка недействительна или срок её действия истёк")
		return
	}
	if err != nil {
		s.fail(w, "чтение приглашения", err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// handleAcceptInvite принимает приглашение. Если аккаунта ещё нет, он
// создаётся здесь же — на почту из приглашения, а не на присланную клиентом:
// иначе ссылку можно было бы применить к любому адресу.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	token := r.PathValue("token")

	userID := ""
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		if p, err := auth.PrincipalBySession(r.Context(), s.db.Pool, cookie.Value); err == nil {
			userID = p.ID
		}
	}

	if userID == "" {
		info, err := s.orgs.LookupInvite(r.Context(), token)
		if errors.Is(err, org.ErrInviteInvalid) {
			writeError(w, http.StatusNotFound, "ссылка недействительна или срок её действия истёк")
			return
		}
		if err != nil {
			s.fail(w, "чтение приглашения", err)
			return
		}
		if !info.NeedsAccount {
			writeError(w, http.StatusUnauthorized, "войдите под этой почтой, чтобы принять приглашение")
			return
		}
		name := strings.TrimSpace(req.Name)
		if msg := validateSignup(info.Email, name, req.Password); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		userID, err = s.createUser(r, info.Email, name, req.Password)
		if errors.Is(err, errEmailTaken) {
			writeError(w, http.StatusConflict, "аккаунт с этой почтой уже есть — войдите")
			return
		}
		if err != nil {
			s.fail(w, "создание пользователя", err)
			return
		}
	}

	membership, err := s.orgs.Accept(r.Context(), token, userID)
	if errors.Is(err, org.ErrInviteInvalid) {
		writeError(w, http.StatusNotFound, "ссылка недействительна или срок её действия истёк")
		return
	}
	if err != nil {
		s.fail(w, "принятие приглашения", err)
		return
	}

	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		if p, err := auth.PrincipalBySession(r.Context(), s.db.Pool, cookie.Value); err == nil {
			_ = auth.SwitchOrg(r.Context(), s.db.Pool, p.SessionID, p.ID, membership.OrgID)
			principal, err := auth.PrincipalBySession(r.Context(), s.db.Pool, p.SessionID)
			if err == nil {
				writeJSON(w, http.StatusOK, principal)
				return
			}
		}
	}
	s.startSession(w, r, userID)
}

// --- доски ---

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	boards, err := s.boards.List(r.Context(), p.OrgID, p.ID)
	if err != nil {
		s.fail(w, "список досок", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "у доски должно быть название")
		return
	}
	b, err := s.boards.Create(r.Context(), p.OrgID, p.ID, req.Name)
	if err != nil {
		s.fail(w, "создание доски", err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	snap, err := s.boards.Snapshot(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if errors.Is(err, board.ErrNotFound) {
		writeError(w, http.StatusNotFound, "доска не найдена")
		return
	}
	if err != nil {
		s.fail(w, "чтение доски", err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if !p.CanEdit() {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req board.Request
	if !decode(w, r, &req) {
		return
	}

	result, err := s.boards.Apply(r.Context(), p.OrgID, p.ID, r.PathValue("id"), req)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "доска не найдена")
	case errors.Is(err, board.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		var conflict *board.ConflictError
		if errors.As(err, &conflict) {
			// 409 несёт текущий порядок колонки: клиент пересобирается
			// точечно, без перезагрузки доски
			writeJSON(w, http.StatusConflict, conflict)
			return
		}
		s.fail(w, "применение операции", err)
	}
}

// --- статика ---

// staticHandler отдаёт собранный фронтенд и возвращает index.html на любой
// неизвестный путь: маршрутизация живёт на клиенте.
func (s *Server) staticHandler() http.Handler {
	root := http.Dir(s.cfg.WebDir)
	fileServer := http.FileServer(root)
	index := filepath.Join(s.cfg.WebDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "неизвестный метод API")
			return
		}
		path := filepath.Join(s.cfg.WebDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := os.Stat(index); err != nil {
			writeError(w, http.StatusNotFound, "фронтенд не собран: выполните make web")
			return
		}
		http.ServeFile(w, r, index)
	})
}

// --- утилиты ответа ---

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.ContentLength == 0 {
		return true
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "не удалось разобрать тело запроса")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// isUserError отличает ошибку ввода от поломки: первую можно показать
// пользователю дословно, вторую — нет.
func isUserError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "укажите") || strings.HasPrefix(msg, "неизвестная роль")
}

// fail логирует причину и отдаёт клиенту нейтральное сообщение: подробности
// внутренней ошибки наружу не выносим.
func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log.Error("ошибка обработки запроса", "этап", what, "err", err)
	writeError(w, http.StatusInternalServerError, "внутренняя ошибка, попробуйте ещё раз")
}
