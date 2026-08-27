package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// Заголовки безопасности обязаны стоять на всяком ответе, а не только
// на удачном.
//
// Проверяется обещание, а не список: браузер обязан узнать, что типы
// не угадываются, что страницу нельзя вставить в чужую рамку и откуда
// разрешено брать скрипты. Отдельно проверяется отказ — на нём щель
// и живёт: страница ошибки такая же страница.
func TestSecurityHeadersOnEveryAnswer(t *testing.T) {
	a := newAPI(t)

	обязательные := map[string]func(string) bool{
		"Content-Security-Policy": func(v string) bool {
			return strings.Contains(v, "default-src 'self'") &&
				strings.Contains(v, "frame-ancestors 'none'") &&
				strings.Contains(v, "object-src 'none'")
		},
		"X-Content-Type-Options": func(v string) bool { return v == "nosniff" },
		"X-Frame-Options":        func(v string) bool { return v == "DENY" },
		"Referrer-Policy":        func(v string) bool { return v != "" },
	}

	// Три разных ответа: удачный, отказ по личности и отказ по адресу.
	// Обёртки у них разные, и заголовки терялись бы поодиночке.
	for _, путь := range []string{"/healthz", "/api/me", "/api/такого-нет"} {
		resp, err := a.client.Get(a.server.URL + путь)
		if err != nil {
			t.Fatalf("%s: %v", путь, err)
		}
		_ = resp.Body.Close()
		for имя, годен := range обязательные {
			if v := resp.Header.Get(имя); !годен(v) {
				t.Errorf("%s (код %d): заголовок %s = %q — не то, что обещано",
					путь, resp.StatusCode, имя, v)
			}
		}
	}
}

// Страница описания контракта ставит свою политику, и она строже общей:
// у неё нет ни своих запросов, ни форм. Общая обёртка не должна её
// перебивать — иначе строгая политика тихо превращается в обычную.
func TestContractPageKeepsItsOwnPolicy(t *testing.T) {
	a := newAPI(t)

	resp, err := a.client.Get(a.server.URL + "/api/v1/docs")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("страница описания: код %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") {
		t.Errorf("страница описания отдала общую политику вместо своей: %q", got)
	}
}

// HSTS ставится только там, где есть https. Установка по http, получившая
// его, перестаёт открываться вовсе — и на год: браузер запомнил.
func TestHSTSFollowsTheScheme(t *testing.T) {
	if got := заголовки(false).Get("Strict-Transport-Security"); got != "" {
		t.Errorf("установка по http получила HSTS: %q", got)
	}
	if got := заголовки(true).Get("Strict-Transport-Security"); got == "" {
		t.Error("установка по https не получила HSTS")
	}
}

// заголовки прогоняет пустой обработчик через обёртку: проверяется сама
// обёртка, и поднимать ради неё сервер с базой незачем.
func заголовки(secure bool) http.Header {
	w := &записанное{header: http.Header{}}
	secureHeaders(secure, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(w, &http.Request{})
	return w.header
}

type записанное struct{ header http.Header }

func (з *записанное) Header() http.Header       { return з.header }
func (з *записанное) Write([]byte) (int, error) { return 0, nil }
func (з *записанное) WriteHeader(int)           {}
