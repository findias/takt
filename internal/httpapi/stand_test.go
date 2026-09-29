package httpapi

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/findias/takt/internal/config"
	"github.com/findias/takt/internal/demo"
)

// Тестовый стенд ветки (ROADMAP 30.7). Как и у демо, главное —
// что у установки заказчика его нет: ни ручки, ни отказа подписок.

func standAPI(t *testing.T) *api {
	return newAPIWith(t, func(c *config.Config) {
		c.Stand = true
		// Открытая регистрация — только ради того, чтобы проверке было
		// откуда взять организацию; на площадке стенд закрыт.
		c.Signup = config.SignupOpen
	})
}

func TestStandNoteDoesNotExistOutsideTheStand(t *testing.T) {
	a := newAPI(t)
	if code, _ := a.session().do("GET", "/api/stand", nil); code != http.StatusNotFound {
		t.Errorf("без STAND=staging заметка ответила %d, а ручки быть не должно вовсе", code)
	}
}

func TestStandTellsWhatIsOnItWithoutSigningIn(t *testing.T) {
	a := standAPI(t)
	// Без входа: заметку читают и на экране входа, где нужен пароль.
	raw := a.session().mustDo("GET", "/api/stand", nil, http.StatusOK)
	for _, key := range []string{"branch", "version", "email", "password"} {
		if _, ok := field(t, raw, key).(string); !ok {
			t.Errorf("в заметке нет строки %q: %s", key, raw)
		}
	}
	// Список, а не null: у сборки без журнала коммитов нет, и клиент
	// читает длину.
	if _, ok := field(t, raw, "commits").([]any); !ok {
		t.Errorf("коммиты не списком: %s", raw)
	}
}

func TestStandRefusesSubscriptions(t *testing.T) {
	a := standAPI(t)
	owner := a.registerOrg("Стенд без подписок")
	code, body := owner.do("POST", "/api/webhooks", map[string]any{
		"name": "Наружу", "url": "https://example.test/hook", "events": []string{"card.created"},
	})
	if code != http.StatusForbidden || field(t, body, "code") != "demo_disabled" {
		t.Errorf("подписка на стенде: %d %s, ожидался отказ", code, body)
	}
}

// Заметка называет вход на своём языке: у стенда английская копия
// организации, и английскому читателю русская показала бы русские
// доски под английскими подписями. Без выбранного языка стенд
// английский, как и демо.
func TestStandNoteNamesTheOrganisationOfItsLanguage(t *testing.T) {
	a := standAPI(t)
	for _, c := range []struct{ cookie, email string }{
		{"", demo.EnglishEmail(demo.People[0])},
		{"en", demo.EnglishEmail(demo.People[0])},
		{"ru", demo.People[0].Email},
	} {
		s := a.session()
		if c.cookie != "" {
			u, _ := url.Parse(a.server.URL)
			s.client.Jar.SetCookies(u, []*http.Cookie{{Name: "lang", Value: c.cookie}})
		}
		raw := s.mustDo("GET", "/api/stand", nil, http.StatusOK)
		if got := field(t, raw, "email"); got != c.email {
			t.Errorf("cookie %q: заметка зовёт входить как %v, а нужно %s", c.cookie, got, c.email)
		}
	}
}
