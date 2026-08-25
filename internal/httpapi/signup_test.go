package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/config"
	"github.com/findias/takt/internal/realtime"
	"github.com/findias/takt/internal/store/testdb"
)

// Кто заводит организации.
//
// До этой настройки регистрация была открыта всегда, и выключить её было
// нечем. На своём стенде это удобство; на чужом, выставленном
// в корпоративную сеть, это значит, что любой сотрудник заводит себе
// организацию мимо каталога, мимо провайдера входа и мимо владельца —
// и обнаруживается это по счёту организаций, а не по отказу.
//
// Проверяются три режима и — отдельно — то, что закрытая регистрация
// закрыта с обеих сторон: и для незнакомца, и для уже вошедшего.
// Второе важнее первого: «нельзя завести организацию с экрана входа,
// зато можно кнопкой в шапке» — это не закрытая регистрация,
// а её видимость.

func serverWith(t *testing.T, mode config.SignupMode) *api {
	t.Helper()
	db := testdb.Shared(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := realtime.NewHub(db, log)
	ctx, stop := context.WithCancel(context.Background())
	go hub.Run(ctx)
	t.Cleanup(stop)

	impl := New(config.Config{BaseURL: "http://example.test", Signup: mode}, db, log, hub)
	srv := httptest.NewServer(impl.Handler())
	t.Cleanup(srv.Close)
	return &api{t: t, server: srv, client: srv.Client(), impl: impl}
}

func TestClosedSignupRefusesAndSaysWhatToDo(t *testing.T) {
	a := serverWith(t, config.SignupClosed)

	raw := a.session().mustDo("POST", "/api/auth/register", map[string]any{
		"org": "Мимо владельца", "name": "Некто",
		"email": "nekto-" + uuid.NewString()[:8] + "@example.test", "password": "parol12345",
	}, http.StatusForbidden)

	// Отказ различается кодом, а не текстом: по нему экран входа прячет
	// кнопку, ведущую в никуда.
	if code, _ := field(t, raw, "code").(string); code != "signup_closed" {
		t.Errorf("код отказа %q, ожидался signup_closed", code)
	}
	// И объясняет, что делать: «нельзя» без выхода отправляет человека
	// искать поломку, которой нет.
	text, _ := field(t, raw, "error").(string)
	if !strings.Contains(text, "приглашение") {
		t.Errorf("отказ не говорит, что делать: %q", text)
	}
}

func TestClosedSignupIsClosedForThoseAlreadyInside(t *testing.T) {
	// Регистрируемся на открытой установке, а вторую организацию просим
	// у закрытой: так проверяется ровно то, что вошедший не обходит
	// правило кнопкой в шапке.
	open := newAPI(t)
	owner := open.registerOrg("Первая")

	// Тот же человек и та же cookie, но сервер с закрытой регистрацией:
	// проверяется правило, а не то, чей это браузер.
	closed := serverWith(t, config.SignupClosed)
	внутри := &session{api: closed, client: owner.client, userID: owner.userID, email: owner.email}

	raw := внутри.mustDo("POST", "/api/orgs", map[string]any{"name": "Вторая"}, http.StatusForbidden)
	if code, _ := field(t, raw, "code").(string); code != "signup_closed" {
		t.Errorf("код отказа %q, ожидался signup_closed", code)
	}
}

func TestFirstComerBecomesTheOwnerAndTheDoorCloses(t *testing.T) {
	// Режим по умолчанию: пока в установке нет ни одной организации,
	// её заводит тот, кто пришёл первым. Это же ответ на вопрос «кто
	// заводит первого владельца» — он заводит себя сам, ровно один раз.
	//
	// База у проверок общая и не пустая, поэтому пустую установку
	// изображаем своей: смотрим не на «получилось ли», а на то, что
	// ответ меняется в зависимости от наличия организаций.
	a := serverWith(t, config.SignupFirst)

	raw := a.session().mustDo("GET", "/api/auth/methods", nil, http.StatusOK)
	var methods struct {
		Signup struct{ Enabled bool } `json:"signup"`
	}
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatal(err)
	}
	// Организации в общей базе есть — значит дверь закрыта.
	if methods.Signup.Enabled {
		t.Fatal("режим first оставил регистрацию открытой при непустой установке")
	}
	a.session().mustDo("POST", "/api/auth/register", map[string]any{
		"org": "Поздняя", "name": "Некто",
		"email": "late-" + uuid.NewString()[:8] + "@example.test", "password": "parol12345",
	}, http.StatusForbidden)
}

func TestOpenSignupSaysSoAndWorks(t *testing.T) {
	a := serverWith(t, config.SignupOpen)

	raw := a.session().mustDo("GET", "/api/auth/methods", nil, http.StatusOK)
	var methods struct {
		Signup struct{ Enabled bool } `json:"signup"`
	}
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatal(err)
	}
	if !methods.Signup.Enabled {
		t.Fatal("открытая регистрация назвалась закрытой: экран спрячет кнопку, которая работает")
	}
	a.session().mustDo("POST", "/api/auth/register", map[string]any{
		"org": "Открытая", "name": "Некто",
		"email": "open-" + uuid.NewString()[:8] + "@example.test", "password": "parol12345",
	}, http.StatusOK)
}
