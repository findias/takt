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
	"slices"
	"strings"
	"sync/atomic"

	"github.com/konkov/agile/internal/apiclient"
	"github.com/konkov/agile/internal/audit"
	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/board"
	"github.com/konkov/agile/internal/config"
	"github.com/konkov/agile/internal/export"
	"github.com/konkov/agile/internal/metrics"
	"github.com/konkov/agile/internal/oidc"
	"github.com/konkov/agile/internal/org"
	"github.com/konkov/agile/internal/realtime"
	"github.com/konkov/agile/internal/scim"
	"github.com/konkov/agile/internal/store"
	"github.com/konkov/agile/internal/team"
	"github.com/konkov/agile/internal/webhook"
)

type Server struct {
	cfg     config.Config
	db      *store.Store
	boards  *board.Service
	orgs    *org.Service
	teams   *team.Service
	client  *apiclient.Service
	hooks   *webhook.Service
	metrics *metrics.Service
	hub     *realtime.Hub
	limiter *limiter
	audit   *audit.Service
	export  *export.Service
	// oidc — провайдер корпоративного входа; nil, когда он не настроен.
	oidc    *oidc.Provider
	scimSvc *scim.Service
	// draining — реплика получила сигнал остановки и больше не готова
	// принимать новые запросы, хотя текущие ещё дорабатывает.
	draining atomic.Bool
	log      *slog.Logger
}

func New(cfg config.Config, db *store.Store, log *slog.Logger, hub *realtime.Hub) *Server {
	s := &Server{
		cfg:     cfg,
		db:      db,
		boards:  board.New(db),
		orgs:    org.New(db),
		teams:   team.New(db),
		client:  apiclient.New(db),
		hooks:   webhook.New(db, board.EventNames()),
		metrics: metrics.New(db),
		hub:     hub,
		limiter: newLimiter(),
		audit:   audit.New(db),
		export:  export.New(db),
		scimSvc: scim.New(db),
		log:     log,
	}
	if cfg.OIDC.Enabled() {
		s.oidc = oidc.New(oidc.Config{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.RedirectURL(),
		})
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Две разные пробы, потому что вопросы разные.
	//
	// /healthz — «процесс жив». Базу он не трогает намеренно: если
	// проверять ею живость, то моргнувшая база перезапустит все реплики
	// разом, и к её возвращению они будут заняты рестартом. Лечение
	// оказалось бы хуже болезни.
	//
	// /readyz — «можно давать запросы». Здесь база нужна: без неё
	// осмысленного ответа не будет. И здесь же реплика говорит «уже нет»,
	// когда получила сигнал остановки, — до того, как перестанет
	// отвечать вовсе.
	mux.HandleFunc("GET /healthz", s.handleLive)
	mux.HandleFunc("GET /readyz", s.handleReady)

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.authed(s.handleMe))
	// Пароль и чужие сессии — свои у каждого, поэтому не под /api/org
	// и не под владельцем: отобрать вход у себя может каждый, и это
	// единственное, чем сегодня отвечают на «пароль утёк».
	mux.HandleFunc("PUT /api/me/password", s.authed(s.handleChangePassword))
	mux.HandleFunc("DELETE /api/me/sessions", s.authed(s.handleRevokeSessions))

	// Организации: список своих, создание новой, переключение активной.
	mux.HandleFunc("GET /api/orgs", s.authed(s.handleListOrgs))
	mux.HandleFunc("POST /api/orgs", s.authed(s.handleCreateOrg))
	mux.HandleFunc("POST /api/session/org", s.authed(s.handleSwitchOrg))

	// Команда активной организации.
	// Состав организации — люди с именами и почтами, самые чувствительные
	// сведения из всех, что отдаёт сервер. Ключу он закрыт целиком:
	// дерево подразделений, где почт нет вовсе, и то требует разрешения,
	// а команду выгружал любой ключ, включая заведённый ради одной доски.
	mux.HandleFunc("GET /api/team", s.human(s.handleTeam))
	mux.HandleFunc("POST /api/invites", s.owner(s.handleInvite))
	mux.HandleFunc("DELETE /api/invites/{id}", s.owner(s.handleRevokeInvite))
	mux.HandleFunc("PUT /api/members/{userId}/role", s.owner(s.handleSetRole))
	// В чём организация оценивает работу. Владелец: единица общая
	// на все доски, и менять её из карточки было бы правкой всего
	// исподтишка.
	mux.HandleFunc("PUT /api/org/estimate-unit", s.owner(s.handleSetEstimateUnit))
	mux.HandleFunc("DELETE /api/members/{userId}", s.owner(s.handleRemoveMember))
	// Исключение и обезличивание — разные действия и потому разные пути:
	// первое обратимо приглашением, второе не обратимо ничем.
	mux.HandleFunc("DELETE /api/members/{userId}/identity", s.owner(s.handleEraseMember))

	// Приглашение открывают по секретной ссылке — до входа и до аккаунта.
	mux.HandleFunc("GET /api/invites/{token}/info", s.handleInviteInfo)
	mux.HandleFunc("POST /api/invites/{token}/accept", s.handleAcceptInvite)

	s.registerTeamRoutes(mux)
	s.registerAccessRoutes(mux)
	s.registerFeedRoutes(mux)
	s.registerClientRoutes(mux)
	s.registerContractRoutes(mux)
	s.registerWebhookRoutes(mux)
	s.registerMetricsRoutes(mux)
	s.registerStreamRoutes(mux)
	s.registerExportRoutes(mux)
	s.registerOIDCRoutes(mux)
	s.registerSCIMRoutes(mux)

	mux.HandleFunc("GET /api/boards", s.scoped(apiclient.ScopeBoardsRead, s.handleListBoards))
	mux.HandleFunc("POST /api/boards", s.scoped(apiclient.ScopeBoardsWrite, s.handleCreateBoard))
	mux.HandleFunc("GET /api/boards/{id}", s.scoped(apiclient.ScopeBoardsRead, s.handleSnapshot))
	mux.HandleFunc("POST /api/boards/{id}/operations",
		s.scoped(apiclient.ScopeBoardsWrite, s.handleOperation))

	if s.cfg.WebDir != "" {
		mux.Handle("/", s.staticHandler())
	}
	// Порядок обёрток: сперва версия — она переписывает путь, — потом
	// предел частоты, потом запись в лог. Иначе в логе оказался бы путь,
	// которого в маршрутах нет.
	return logRequests(s.log, versioned(s.limited(mux)))
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.draining.Load() {
		writeError(w, http.StatusServiceUnavailable, "реплика останавливается")
		return
	}
	if err := s.db.Pool.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "база недоступна")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Drain переводит реплику в «запросов больше не давайте», не закрывая
// текущие. Между этим и остановкой нужен запас: балансировщик узнаёт
// о неготовности не мгновенно, и реплика, переставшая отвечать раньше,
// чем её вычеркнули из списка, выглядит как пятисотые у пользователя.
func (s *Server) Drain() { s.draining.Store(true) }

// --- доступ ---

// scoped требует разрешения у сервисного клиента. Для человека
// разрешений нет: он ограничен ролью, и этого достаточно — ключ же
// выдаётся под конкретную задачу, и «может всё» у него было бы
// бессмысленным по назначению.
func (s *Server) scoped(scope string, next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		if granted, ok := scopesOf(r); ok && !slices.Contains(granted, scope) {
			writeError(w, http.StatusForbidden, "ключу не выдано разрешение "+scope)
			return
		}
		next(w, r, p)
	})
}

type scopesKey struct{}

func scopesOf(r *http.Request) ([]string, bool) {
	scopes, ok := r.Context().Value(scopesKey{}).([]string)
	return scopes, ok
}

// authed принимает и человека с сессией, и сервисного клиента с ключом.
//
// Дальше по коду разницы между ними нет: у клиента есть служебная
// личность, он состоит в организации как все, и политики доступа
// не знают, кто именно за запросом.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearer(r); ok {
			principal, scopes, err := s.client.Authenticate(r.Context(), token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "ключ недействителен")
				return
			}
			// Ключ каталога сюда не ходит вовсе. У него роль владельца
			// организации — этого требуют политики базы, чтобы заводить
			// людей, — а разрешения проверяются на четырёх маршрутах
			// доски из девяноста трёх, и на остальных его держала бы
			// только роль, то есть ничто. Сужаем один ключ, а не
			// расписываем права по маршрутам: набор прав, придуманный
			// умозрительно, всё равно окажется неверным (4.3).
			if apiclient.Directory(scopes) {
				writeError(w, http.StatusForbidden,
					"ключ каталога работает только со /scim/v2; "+
						"для доступа к доскам заведите отдельный ключ")
				return
			}
			s.withIdempotency(w,
				r.WithContext(context.WithValue(r.Context(), scopesKey{}, scopes)),
				principal, next)
			return
		}

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

// bearer читает ключ из заголовка. Ключ в строке запроса не принимается
// намеренно: адреса попадают в логи прокси и в историю браузера, а секрет
// там оказываться не должен.
func bearer(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	return token, token != ""
}

// human пускает только человека с сессией.
//
// Ключу отказывают не из осторожности: это вызовы своего клиента, они
// меняются вместе с ним и ничего наружу не обещают. Обёртка scoped
// и есть обещание ключу — что ею не помечено, того ключу не обещали,
// а состав организации с именами и почтами до этого отдавался любому
// ключу, включая заведённый ради чтения одной доски.
//
// Отказ называет, чем пользоваться вместо: структура описана
// в /api/v1/teams, люди — в каталоге по SCIM.
func (s *Server) human(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		if _, byKey := scopesOf(r); byKey {
			writeError(w, http.StatusForbidden,
				"это вызов интерфейса, а не контракта: структура ключу открыта "+
					"в /api/v1/teams, люди — в /scim/v2/Users")
			return
		}
		next(w, r, p)
	})
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
	// Счёт попыток спрашивается до проверки пароля и не зависит от того,
	// заведён ли такой адрес: иначе отказ «слишком много попыток» сам
	// сообщал бы, что адрес существует.
	attempts := loginKey(req.Email)
	if ok, after := s.limiter.left(attempts, loginBurst, loginPerSec); !ok {
		tooManyAttempts(w, after)
		return
	}

	user, err := auth.Authenticate(r.Context(), s.db.Pool, req.Email, req.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
		s.limiter.spend(attempts, loginBurst, loginPerSec)
		writeError(w, http.StatusUnauthorized, "неверная почта или пароль")
		return
	}
	if err != nil {
		s.fail(w, "проверка пароля", err)
		return
	}
	s.limiter.forget(attempts)
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

// «Кто я» отвечает ключу и разрешениями тоже.
//
// Без них интеграция не отличает «разрешение не выдали» от «ключ
// отозвали»: и то и другое видно только по отказу на попытке, а отказы
// разные — первый чинит владелец организации, второй означает, что
// работать больше нечем. Человеку за браузером поля нет вовсе:
// разрешений у него не бывает, и пустой список читался бы как «ничего
// не разрешено».
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	granted, ok := scopesOf(r)
	if !ok {
		writeJSON(w, http.StatusOK, p)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		auth.Principal
		Scopes []string `json:"scopes"`
	}{Principal: p, Scopes: granted})
}

// Смена пароля. Обрывает все прочие сессии — см. auth.ChangePassword.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	// Ключом пароль не меняют: у служебной личности его нет вовсе,
	// а сессий у неё не бывает — обрывать было бы нечего.
	if _, byKey := scopesOf(r); byKey {
		writeError(w, http.StatusForbidden, "ключом пароль не меняют: это действие человека")
		return
	}
	var req struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Next) < auth.MinPasswordLen {
		writeError(w, http.StatusBadRequest, "пароль должен быть не короче 8 символов")
		return
	}
	if req.Next == req.Current {
		// Отдельным отказом, а не молчаливым успехом: человек, нажавший
		// «сменить» и получивший «готово», уверен, что сменил.
		writeError(w, http.StatusBadRequest, "новый пароль совпадает с текущим")
		return
	}

	err := auth.ChangePassword(r.Context(), s.db.Pool, p.ID, p.SessionID, req.Current, req.Next)
	switch {
	case errors.Is(err, auth.ErrWrongPassword):
		writeError(w, http.StatusForbidden, auth.ErrWrongPassword.Error())
	case errors.Is(err, auth.ErrFederated):
		writeError(w, http.StatusConflict, auth.ErrFederated.Error())
	case err != nil:
		s.fail(w, "смена пароля", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// «Выйти на всех устройствах». Своя сессия остаётся: человек нажимает
// это, чтобы закрыть чужой вход, а не свой.
func (s *Server) handleRevokeSessions(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if _, byKey := scopesOf(r); byKey {
		writeError(w, http.StatusForbidden, "у ключа нет сессий: ключ отзывают в разделе «Ключи для интеграций»")
		return
	}
	if err := auth.RevokeOtherSessions(r.Context(), s.db.Pool, p.ID, p.SessionID); err != nil {
		s.fail(w, "обрыв прочих сессий", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) handleSetEstimateUnit(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var req struct {
		Unit string `json:"unit"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.orgs.SetEstimateUnit(r.Context(), p.OrgID, p.ID, req.Unit); err != nil {
		if errors.Is(err, org.ErrNotFound) {
			writeError(w, http.StatusNotFound, "организация не найдена")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Отвечаем тем, кто теперь спрашивающий: единица приезжает вместе
	// с «кто я», и клиенту незачем перечитывать её отдельным запросом.
	p.EstimateUnit = req.Unit
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	err := s.orgs.Remove(r.Context(), p.OrgID, p.ID, r.PathValue("userId"))
	// «Нельзя» вместо «не найдено»: личность есть, просто она не человек,
	// и отказ называет, что с ней делать вместо этого.
	if errors.Is(err, org.ErrServiceIdentity) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.writeMembershipResult(w, err, "исключение из команды")
}

func (s *Server) handleEraseMember(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if r.PathValue("userId") == p.ID {
		// Обезличить себя значит остаться без входа и без способа это
		// исправить: владельцем организации уже никто не будет.
		writeError(w, http.StatusConflict, "себя обезличить нельзя")
		return
	}
	err := s.orgs.Erase(r.Context(), p.OrgID, p.ID, r.PathValue("userId"))
	switch {
	case errors.Is(err, org.ErrSharedIdentity), errors.Is(err, org.ErrServiceIdentity):
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.writeMembershipResult(w, err, "обезличивание участника")
	}
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
		// Пусто — ключ выводится из названия. Придумывать префикс номеров
		// при заведении доски человека заставлять незачем.
		Key string `json:"key"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "у доски должно быть название")
		return
	}
	b, err := s.boards.Create(r.Context(), p.OrgID, p.ID, req.Name, req.Key)
	if errors.Is(err, board.ErrBadKey) {
		writeCoded(w, http.StatusBadRequest, "board_key_invalid", board.ErrBadKey.Error())
		return
	}
	if errors.Is(err, board.ErrKeyTaken) {
		writeCoded(w, http.StatusConflict, "board_key_taken", board.ErrKeyTaken.Error())
		return
	}
	if err != nil {
		s.fail(w, "создание доски", err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	snap, err := s.boards.Snapshot(r.Context(), p.OrgID, p.ID, r.PathValue("id"))
	if errors.Is(err, board.ErrArchivedBoard) {
		writeCoded(w, http.StatusNotFound, "board_archived", board.ErrArchivedBoard.Error())
		return
	}
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
	case errors.Is(err, board.ErrReadOnlyBoard):
		// Доску он видит, и притворяться, что её нет, поздно: так у
		// наблюдателя поддерева.
		writeError(w, http.StatusForbidden, board.ErrReadOnlyBoard.Error())
	case errors.Is(err, board.ErrBadRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, board.ErrOwnerOnly):
		writeError(w, http.StatusForbidden, board.ErrOwnerOnly.Error())
	case errors.Is(err, board.ErrIterationClosed),
		errors.Is(err, board.ErrCardInAnotherIteration):
		// Не сбой и не ошибка запроса: правило итерации, о котором надо
		// сказать словами. Повтор того же запроса не поможет.
		writeError(w, http.StatusConflict, err.Error())
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
//
// Про кэш сказано явно, потому что молчание здесь толкуется браузером
// в худшую сторону: без заголовков Chrome держит index.html
// эвристически — по времени последней правки, — и после выката человек
// продолжает грузить старый скрипт. Новый сервер и старый клиент дают
// не ошибку, а пустой экран доски: клиент ждёт полей, которых прежний
// снимок не отдавал. Ровно это и случилось при проверке правки
// 19 августа: правка выглядела неработающей, хотя работала.
//
// Поэтому: index.html не кэшируется вовсе, а содержимое assets/ —
// навсегда. Имена там с отпечатком содержимого (index-BQyAfczc.js),
// значит новая сборка приносит новые имена, и «навсегда» безопасно.
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
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := os.Stat(index); err != nil {
			writeError(w, http.StatusNotFound, "фронтенд не собран: выполните make web")
			return
		}
		// `no-cache` — не «не хранить»: копия остаётся, но перед показом
		// её сверяют с сервером. Это и нужно: index.html весит меньше
		// килобайта, а сверка возвращает 304 без тела.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}

// --- утилиты ответа ---

// maxBodyBytes — предел на тело запроса. Без него чужой запрос решает,
// сколько нам занять памяти.
const maxBodyBytes = 1 << 20

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.ContentLength == 0 {
		return true
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
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

// writeError отвечает ошибкой с машиночитаемым кодом рядом с текстом.
//
// Текст пишется для человека и может меняться — на нём нельзя строить
// разбор ответа. Код не меняется никогда: интеграция ветвится по нему.
func writeError(w http.ResponseWriter, status int, message string) {
	writeCoded(w, status, codeFor(status), message)
}

func writeCoded(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

// codeFor — код по умолчанию для кода состояния. Отдельные случаи
// называются точнее там, где это помогает решить, что делать дальше.
func codeFor(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "too_many_requests"
	default:
		return "internal"
	}
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
