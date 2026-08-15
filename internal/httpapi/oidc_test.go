package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/konkov/agile/internal/config"
	"github.com/konkov/agile/internal/realtime"
	"github.com/konkov/agile/internal/store"
)

// Вход через корпоративный провайдер проверяется против поддельного
// провайдера, а не против заглушки нашего же кода: весь смысл этого
// потока — в том, что уходит по проводу и что возвращается обратно.
// Поддельный провайдер сам сверяет PKCE, поэтому проверка не выродится
// в «мы отправили что-то и сами себе поверили».

type fakeIdP struct {
	server *httptest.Server
	// challenge — то, что мы отправили провайдеру; сверяется при обмене.
	challenge string
	claims    map[string]any
	// tokenCalls — сколько раз меняли код: повтор обмена тем же кодом
	// не должен получаться.
	tokenCalls int
}

func newFakeIdP(t *testing.T, claims map[string]any) *fakeIdP {
	t.Helper()
	idp := &fakeIdP{claims: claims}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"issuer":                 idp.server.URL,
			"authorization_endpoint": idp.server.URL + "/auth",
			"token_endpoint":         idp.server.URL + "/token",
			"userinfo_endpoint":      idp.server.URL + "/userinfo",
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		idp.tokenCalls++
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, _, ok := r.BasicAuth(); !ok {
			t.Error("клиент не назвался при обмене кода")
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != idp.challenge {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"access_token": "доступ-" + uuid.NewString(),
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, idp.claims)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

type federated struct {
	t      *testing.T
	server *httptest.Server
	db     *store.Store
	idp    *fakeIdP
	// browser не ходит по редиректам сам: смысл проверки в том, куда
	// именно нас отправляют.
	browser *http.Client
	orgID   string
	orgSlug string
}

func newFederated(t *testing.T, claims map[string]any) *federated {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("не задан TEST_DATABASE_URL, интеграционные тесты пропущены")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("подключение к тестовой базе: %v", err)
	}
	t.Cleanup(db.Close)

	f := &federated{t: t, db: db, idp: newFakeIdP(t, claims)}
	f.orgSlug = "sso-" + uuid.NewString()[:8]
	if err := db.Pool.QueryRow(ctx,
		`insert into orgs (name, slug) values ($1, $2) returning id`,
		"Организация входа", f.orgSlug).Scan(&f.orgID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `delete from orgs where id = $1`, f.orgID)
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := realtime.NewHub(db, log)
	hubCtx, stopHub := context.WithCancel(ctx)
	go hub.Run(hubCtx)
	t.Cleanup(stopHub)

	srv := httptest.NewServer(nil)
	cfg := config.Config{
		BaseURL: srv.URL,
		OIDC: config.OIDCConfig{
			Issuer:       f.idp.server.URL,
			ClientID:     "board",
			ClientSecret: "секрет",
			OrgSlug:      f.orgSlug,
			Label:        "Корпоративный аккаунт",
		},
	}
	srv.Config.Handler = New(cfg, db, log, hub).Handler()
	t.Cleanup(srv.Close)
	f.server = srv

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	f.browser = &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return f
}

// start проходит первый шаг и возвращает state, который провайдер обязан
// вернуть обратно.
func (f *federated) start() string {
	f.t.Helper()
	resp, err := f.browser.Get(f.server.URL + "/api/auth/oidc/start")
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		f.t.Fatalf("начало входа: код %d, тело %s", resp.StatusCode, body)
	}
	target, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		f.t.Fatal(err)
	}
	q := target.Query()
	if q.Get("code_challenge_method") != "S256" {
		f.t.Errorf("PKCE не запрошен: %v", q)
	}
	if q.Get("redirect_uri") != f.server.URL+"/api/auth/oidc/callback" {
		f.t.Errorf("обратный адрес: %q", q.Get("redirect_uri"))
	}
	f.idp.challenge = q.Get("code_challenge")
	return q.Get("state")
}

func (f *federated) callback(state string) *http.Response {
	f.t.Helper()
	resp, err := f.browser.Get(f.server.URL + "/api/auth/oidc/callback?code=код&state=" + url.QueryEscape(state))
	if err != nil {
		f.t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp
}

// me отвечает на вопрос «вошли ли мы».
func (f *federated) me() (int, map[string]any) {
	f.t.Helper()
	resp, err := f.browser.Get(f.server.URL + "/api/me")
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return resp.StatusCode, out
}

func TestFirstFederatedLoginCreatesPersonAndEnrolsThem(t *testing.T) {
	email := "sso-" + uuid.NewString()[:8] + "@example.test"
	f := newFederated(t, map[string]any{
		"sub": "внешний-" + uuid.NewString(), "email": email,
		"email_verified": true, "name": "Пришедший впервые",
	})
	t.Cleanup(func() {
		_, _ = f.db.Pool.Exec(context.Background(), `delete from users where email = $1`, email)
	})

	if resp := f.callback(f.start()); resp.StatusCode != http.StatusFound ||
		resp.Header.Get("Location") != "/" {
		t.Fatalf("возврат от провайдера: код %d, куда %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}

	code, me := f.me()
	if code != http.StatusOK {
		t.Fatalf("после входа /api/me: код %d", code)
	}
	if me["email"] != email {
		t.Errorf("вошли не тем: %v", me["email"])
	}
	// Пришедший впервые зачислен в названную организацию — иначе войти-то
	// он вошёл, а работать ему негде.
	if me["orgSlug"] != f.orgSlug {
		t.Errorf("организация после входа: %v, ожидалась %q", me["orgSlug"], f.orgSlug)
	}
	if me["role"] != "member" {
		t.Errorf("роль после входа: %v", me["role"])
	}
}

// Почта меняется, sub — нет. Вернувшийся с новой почтой обязан остаться
// собой, иначе он потеряет свои доски.
func TestReturningPersonIsRecognisedBySubjectNotEmail(t *testing.T) {
	subject := "внешний-" + uuid.NewString()
	first := "before-" + uuid.NewString()[:8] + "@example.test"
	f := newFederated(t, map[string]any{
		"sub": subject, "email": first, "email_verified": true, "name": "Человек",
	})
	t.Cleanup(func() {
		_, _ = f.db.Pool.Exec(context.Background(), `delete from users where oidc_subject = $1`, subject)
	})

	f.callback(f.start())
	_, me := f.me()
	userID := me["id"]

	// У провайдера сменили почту.
	second := "after-" + uuid.NewString()[:8] + "@example.test"
	f.idp.claims["email"] = second
	f.callback(f.start())

	_, again := f.me()
	if again["id"] != userID {
		t.Errorf("после смены почты завелась вторая учётная запись: %v против %v",
			again["id"], userID)
	}
	if again["email"] != second {
		t.Errorf("почта не обновилась: %v", again["email"])
	}
}

// Возврат с чужим state — единственная защита от того, что вход в чужую
// учётную запись сделают руками ничего не подозревающего человека.
func TestCallbackWithForeignStateIsRefused(t *testing.T) {
	f := newFederated(t, map[string]any{
		"sub": "внешний-" + uuid.NewString(), "email": "x@example.test",
		"email_verified": true, "name": "Кто-то",
	})

	f.start()
	resp := f.callback("подсунутое-значение")
	if resp.StatusCode != http.StatusFound ||
		!strings.HasPrefix(resp.Header.Get("Location"), "/?error=") {
		t.Fatalf("чужой state: код %d, куда %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if code, _ := f.me(); code != http.StatusUnauthorized {
		t.Errorf("после отказа мы всё-таки вошли: код %d", code)
	}
	if f.idp.tokenCalls != 0 {
		t.Errorf("код обменяли, не сверив state: обменов %d", f.idp.tokenCalls)
	}
}

// Провайдер, не подтвердивший почту, не должен уметь отдать чужую учётную
// запись: связывание идёт по адресу, а адрес в этом случае непроверенный.
func TestUnverifiedEmailIsRefused(t *testing.T) {
	f := newFederated(t, map[string]any{
		"sub": "внешний-" + uuid.NewString(), "email": "chuzhoy@example.test",
		"email_verified": false, "name": "Непроверенный",
	})

	resp := f.callback(f.start())
	if !strings.HasPrefix(resp.Header.Get("Location"), "/?error=") {
		t.Fatalf("неподтверждённая почта: куда %q", resp.Header.Get("Location"))
	}
	if code, _ := f.me(); code != http.StatusUnauthorized {
		t.Errorf("вошли с неподтверждённой почтой: код %d", code)
	}
}

// Уже состоящий в организации не должен молча оказаться ещё и в той,
// куда зачисляют новичков.
func TestExistingMemberIsNotEnrolledElsewhere(t *testing.T) {
	subject := "внешний-" + uuid.NewString()
	email := "already-" + uuid.NewString()[:8] + "@example.test"
	f := newFederated(t, map[string]any{
		"sub": subject, "email": email, "email_verified": true, "name": "Свой",
	})

	// Заводим человека в собственной организации — как будто он уже работал
	// здесь до появления корпоративного входа.
	ctx := context.Background()
	var otherOrg, userID string
	if err := f.db.Pool.QueryRow(ctx,
		`insert into orgs (name, slug) values ($1, $2) returning id`,
		"Другая", "other-"+uuid.NewString()[:8]).Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.db.Pool.Exec(ctx, `delete from orgs where id = $1`, otherOrg) })
	if err := f.db.Pool.QueryRow(ctx,
		`insert into users (email, name, password_hash) values ($1, 'Свой', 'x') returning id`,
		email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.db.Pool.Exec(ctx, `delete from users where id = $1`, userID) })
	if _, err := f.db.Pool.Exec(ctx,
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		otherOrg, userID); err != nil {
		t.Fatal(err)
	}

	f.callback(f.start())

	code, me := f.me()
	if code != http.StatusOK {
		t.Fatalf("вход своего: код %d", code)
	}
	// Связался с той же учётной записью, а не завёл вторую.
	if me["id"] != userID {
		t.Errorf("вход завёл вторую учётную запись: %v против %v", me["id"], userID)
	}
	var orgs int
	if err := f.db.Pool.QueryRow(ctx,
		`select count(*) from memberships where user_id = $1`, userID).Scan(&orgs); err != nil {
		t.Fatal(err)
	}
	if orgs != 1 {
		t.Errorf("человека зачислили ещё куда-то: организаций %d", orgs)
	}
}

func TestAuthMethodsTellTheClientWhatToShow(t *testing.T) {
	f := newFederated(t, map[string]any{"sub": "x"})
	resp, err := f.browser.Get(f.server.URL + "/api/auth/methods")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]struct {
		Enabled bool   `json:"enabled"`
		Label   string `json:"label"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("разбор ответа: %v; тело %s", err, body)
	}
	if !out["oidc"].Enabled || out["oidc"].Label != "Корпоративный аккаунт" {
		t.Errorf("способы входа: %+v", out)
	}
}
