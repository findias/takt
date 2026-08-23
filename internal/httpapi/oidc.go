package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/konkov/agile/internal/auth"
	"github.com/konkov/agile/internal/oidc"
)

// Вход через корпоративный провайдер: две ручки и одна временная cookie.
//
// Между «ушёл к провайдеру» и «вернулся с кодом» надо что-то помнить:
// случайное значение state и проверочный код PKCE. Обычно это кладут
// в серверную сессию, но сессии до входа у человека нет, и заводить её
// ради двух минут — значит завести таблицу, её уборку и её же утечку.
//
// Здесь это короткоживущая cookie: state сверяется с тем, что вернул
// провайдер, и сходятся они только у того же браузера, который начинал
// вход. Подделать её нельзя, не имея доступа к браузеру, — а имея его,
// подделывать уже нечего.

const oidcStateCookie = "board_oidc"

type oidcState struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
}

func (s *Server) registerOIDCRoutes(mux *http.ServeMux) {
	// Способы входа сообщаются до входа: клиент не должен догадываться,
	// показывать ли кнопку, по наличию ошибки на несуществующей ручке.
	mux.HandleFunc("GET /api/auth/methods", s.handleAuthMethods)

	if s.oidc == nil {
		return
	}
	mux.HandleFunc("GET /api/auth/oidc/start", s.handleOIDCStart)
	mux.HandleFunc("GET /api/auth/oidc/callback", s.handleOIDCCallback)
}

func (s *Server) handleAuthMethods(w http.ResponseWriter, r *http.Request) {
	type method struct {
		Enabled bool   `json:"enabled"`
		Label   string `json:"label,omitempty"`
	}
	// Регистрация едет тем же ответом, что и способы входа, и по той же
	// причине: экран входа спрашивает их разом, чтобы не предлагать
	// дверь, которой нет. Кнопка «Завести новую организацию», ведущая
	// к отказу, — это та же дверь, только про другое.
	//
	// Ошибку счёта здесь не показываем: способы входа важнее, и молчать
	// о них из-за недоступной базы значит закрыть вход целиком.
	// Недоступная база всё равно скажет о себе на первом же входе.
	allowed, err := s.signupAllowed(r.Context())
	if err != nil {
		s.log.Error("проверка режима регистрации", "err", err)
		allowed = false
	}
	writeJSON(w, http.StatusOK, map[string]method{
		"password": {Enabled: true},
		"oidc":     {Enabled: s.oidc != nil, Label: s.cfg.OIDC.Label},
		"signup":   {Enabled: allowed},
	})
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	state, err := oidc.Random()
	if err != nil {
		s.fail(w, "вход через провайдера", err)
		return
	}
	verifier, err := oidc.Random()
	if err != nil {
		s.fail(w, "вход через провайдера", err)
		return
	}

	target, err := s.oidc.AuthURL(r.Context(), state, verifier)
	if err != nil {
		// Провайдер недоступен — это не ошибка человека, но и войти он
		// сейчас не сможет. Сообщение честное, без подробностей: адреса
		// внутренних конечных точек посторонним ни к чему.
		s.log.Error("провайдер входа недоступен", "err", err)
		s.backToLogin(w, r, "Вход через провайдера сейчас недоступен")
		return
	}

	raw, err := json.Marshal(oidcState{State: state, Verifier: verifier})
	if err != nil {
		s.fail(w, "вход через провайдера", err)
		return
	}
	// #nosec G124 -- Secure берётся из конфигурации; SameSite и HttpOnly
	// стоят ниже, и оба выбраны с доводом.
	http.SetCookie(w, &http.Cookie{
		Name:  oidcStateCookie,
		Value: url.QueryEscape(string(raw)),
		Path:  "/api/auth/oidc",
		// Lax, а не Strict: возврат от провайдера — это переход с чужого
		// сайта, и при Strict cookie не отправилась бы ровно тогда, когда
		// она нужна.
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		// Десять минут: столько занимает ввод пароля и второго фактора
		// у провайдера. Дольше — уже не вход, а забытая вкладка.
		MaxAge: int((10 * time.Minute).Seconds()),
	})
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	// Отказ у провайдера — обычное дело: человек передумал или ему
	// не разрешено. Это не ошибка сервера.
	if reason := r.URL.Query().Get("error"); reason != "" {
		s.log.Info("провайдер отказал во входе", "причина", reason)
		s.backToLogin(w, r, "Провайдер не пустил")
		return
	}

	cookie, err := r.Cookie(oidcStateCookie)
	if err != nil {
		s.backToLogin(w, r, "Вход занял слишком много времени, попробуйте заново")
		return
	}
	// Cookie одноразовая: оставленная после возврата, она позволила бы
	// повторить обмен тем же кодом.
	s.clearOIDCState(w)

	raw, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		s.backToLogin(w, r, "Вход не удался, попробуйте заново")
		return
	}
	var saved oidcState
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		s.backToLogin(w, r, "Вход не удался, попробуйте заново")
		return
	}
	// Главная проверка всего потока: вернулись туда же, откуда уходили.
	// Без неё чужой запрос с подсунутым кодом входил бы в чужую учётную
	// запись руками ничего не подозревающего человека.
	if saved.State == "" || saved.State != r.URL.Query().Get("state") {
		s.log.Info("возврат от провайдера с чужим state")
		s.backToLogin(w, r, "Вход не удался, попробуйте заново")
		return
	}

	claims, err := s.oidc.Exchange(r.Context(), r.URL.Query().Get("code"), saved.Verifier)
	if err != nil {
		s.log.Error("обмен кода у провайдера", "err", err)
		s.backToLogin(w, r, "Провайдер не подтвердил вход")
		return
	}
	// Связывать по неподтверждённой почте нельзя, а заводить учётную запись
	// на неё — значит поверить в адрес, в который не верит сам провайдер.
	// Проверяет это сама операция входа; здесь отказ только переводится
	// в слова для того, кто вернулся из браузера.
	user, err := auth.FederatedLogin(r.Context(), s.db.Pool,
		s.oidc.Issuer(), claims.Subject, claims.Email, claims.EmailVerified,
		claims.Name, s.cfg.OIDC.OrgSlug, auth.RoleMember)
	if errors.Is(err, auth.ErrUnverifiedEmail) {
		s.log.Info("провайдер вернул неподтверждённую почту", "sub", claims.Subject)
		s.backToLogin(w, r, "Провайдер не подтвердил вашу почту")
		return
	}
	if err != nil {
		s.log.Error("зачисление пришедшего от провайдера", "err", err)
		s.backToLogin(w, r, "Вход не удался, обратитесь к администратору")
		return
	}

	sessionID, expires, err := auth.CreateSession(r.Context(), s.db.Pool, user.ID)
	if err != nil {
		s.fail(w, "вход через провайдера", err)
		return
	}
	auth.SetCookie(w, sessionID, expires, s.cfg.SecureCookies())
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) clearOIDCState(w http.ResponseWriter) {
	// #nosec G124 -- то же: Secure из конфигурации.
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    "",
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		MaxAge:   -1,
	})
}

// backToLogin возвращает человека на экран входа с объяснением.
//
// Не JSON с кодом ошибки: сюда приходит браузер по переходу от провайдера,
// и показать ему голый JSON значит показать тупик. Причина едет параметром,
// клиент её показывает.
func (s *Server) backToLogin(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/?error="+url.QueryEscape(reason), http.StatusFound)
}
