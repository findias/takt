// Package httpapi — HTTP-слой: маршруты, сессии, раздача фронтенда.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/board"
	"github.com/konkov/agile/internal/config"
)

type Server struct {
	cfg    config.Config
	pool   *pgxpool.Pool
	boards *board.Service
	log    *slog.Logger
}

func New(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) *Server {
	return &Server{cfg: cfg, pool: pool, boards: board.New(pool), log: log}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.authed(s.handleMe))

	mux.HandleFunc("GET /api/boards", s.authed(s.handleListBoards))
	mux.HandleFunc("POST /api/boards", s.authed(s.handleCreateBoard))
	mux.HandleFunc("GET /api/boards/{id}", s.authed(s.handleSnapshot))
	mux.HandleFunc("POST /api/boards/{id}/operations", s.authed(s.handleOperation))

	if s.cfg.WebDir != "" {
		mux.Handle("/", s.staticHandler())
	}
	return logRequests(s.log, mux)
}

// --- служебное ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "база недоступна")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type ctxKey string

const userKey ctxKey = "user"

// authed требует активную сессию и кладёт пользователя в контекст запроса.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.CookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "нужно войти")
			return
		}
		user, err := auth.UserBySession(r.Context(), s.pool, cookie.Value)
		if err != nil {
			auth.ClearCookie(w, s.cfg.SecureCookies())
			writeError(w, http.StatusUnauthorized, "сессия истекла, войдите заново")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)), user)
	}
}

// --- аутентификация ---

type registerRequest struct {
	Org      string `json:"org"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleRegister создаёт организацию и её владельца.
//
// Этап 0: регистрация открыта. К этапу 1 её закрывает приглашение по ссылке —
// иначе любой, кто дотянулся до адреса, заведёт себе организацию.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Name = strings.TrimSpace(req.Name)
	req.Org = strings.TrimSpace(req.Org)

	switch {
	case req.Email == "" || !strings.Contains(req.Email, "@"):
		writeError(w, http.StatusBadRequest, "укажите почту")
		return
	case len(req.Password) < auth.MinPasswordLen:
		writeError(w, http.StatusBadRequest, "пароль должен быть не короче 8 символов")
		return
	case req.Name == "":
		writeError(w, http.StatusBadRequest, "укажите имя")
		return
	}
	if req.Org == "" {
		req.Org = "Моя команда"
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.fail(w, "хеширование пароля", err)
		return
	}

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.fail(w, "начало транзакции", err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	var orgID string
	if err := tx.QueryRow(r.Context(),
		`insert into orgs (name) values ($1) returning id`, req.Org).Scan(&orgID); err != nil {
		s.fail(w, "создание организации", err)
		return
	}
	var userID string
	err = tx.QueryRow(r.Context(), `
		insert into users (org_id, email, name, password_hash, role)
		values ($1, $2, $3, $4, 'owner') returning id`,
		orgID, req.Email, req.Name, hash).Scan(&userID)
	if err != nil {
		if strings.Contains(err.Error(), "users_org_email_key") {
			writeError(w, http.StatusConflict, "такая почта уже зарегистрирована")
			return
		}
		s.fail(w, "создание пользователя", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.fail(w, "фиксация регистрации", err)
		return
	}

	s.startSession(w, r, userID)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	user, err := auth.Authenticate(r.Context(), s.pool, req.Email, req.Password)
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
	sessionID, expires, err := auth.CreateSession(r.Context(), s.pool, userID)
	if err != nil {
		s.fail(w, "создание сессии", err)
		return
	}
	auth.SetCookie(w, sessionID, expires, s.cfg.SecureCookies())

	user, err := auth.UserBySession(r.Context(), s.pool, sessionID)
	if err != nil {
		s.fail(w, "чтение профиля", err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		_ = auth.DeleteSession(r.Context(), s.pool, cookie.Value)
	}
	auth.ClearCookie(w, s.cfg.SecureCookies())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, user auth.User) {
	writeJSON(w, http.StatusOK, user)
}

// --- доски ---

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request, user auth.User) {
	boards, err := s.boards.List(r.Context(), user.OrgID)
	if err != nil {
		s.fail(w, "список досок", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request, user auth.User) {
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
	b, err := s.boards.Create(r.Context(), user.OrgID, req.Name)
	if err != nil {
		s.fail(w, "создание доски", err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request, user auth.User) {
	snap, err := s.boards.Snapshot(r.Context(), user.OrgID, r.PathValue("id"))
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

func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request, user auth.User) {
	if user.Role == "viewer" {
		writeError(w, http.StatusForbidden, "у вас доступ только на чтение")
		return
	}
	var req board.Request
	if !decode(w, r, &req) {
		return
	}

	result, err := s.boards.Apply(r.Context(), user.OrgID, user.ID, r.PathValue("id"), req)
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

// fail логирует причину и отдаёт клиенту нейтральное сообщение: подробности
// внутренней ошибки наружу не выносим.
func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log.Error("ошибка обработки запроса", "этап", what, "err", err)
	writeError(w, http.StatusInternalServerError, "внутренняя ошибка, попробуйте ещё раз")
}
