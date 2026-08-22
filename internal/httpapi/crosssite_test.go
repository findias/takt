package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/konkov/agile/internal/auth"
)

// Запрос с чужой страницы: чем он остановлен.
//
// Проверяется не «стоит SameSite=Lax» — так проверка ловила бы правку,
// а не поломку, и падала бы на замене одной защиты другой. Проверяется
// обещание: подделка, которую браузер способен отправить с чужого сайта,
// до записи не доходит. Держать его может браузер (Lax у cookie сессии)
// или код (сверка Origin на изменяющих маршрутах) — но кто-то обязан.

// loginCookie входит паролем и возвращает cookie сессии как она ушла
// в браузер: с атрибутами, а не одним значением.
func (a *api) loginCookie(email, password string) *http.Cookie {
	a.t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		a.t.Fatal(err)
	}
	resp, err := a.client.Post(a.server.URL+"/api/auth/login",
		"application/json", bytes.NewReader(body))
	if err != nil {
		a.t.Fatalf("вход: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("вход: код %d, тело: %s", resp.StatusCode, raw)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	a.t.Fatalf("вход не отдал cookie %q", auth.CookieName)
	return nil
}

// createBoard заводит доску запросом, собранным вручную: тип тела
// и Origin здесь — то, чем подделка отличается от своей записи.
func (a *api) createBoard(cookie *http.Cookie, contentType, origin, name string) (int, []byte) {
	a.t.Helper()
	body, err := json.Marshal(map[string]any{"name": name})
	if err != nil {
		a.t.Fatal(err)
	}
	req, err := http.NewRequest("POST", a.server.URL+"/api/boards", bytes.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.AddCookie(cookie)
	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("заведение доски: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func sameSiteName(s http.SameSite) string {
	switch s {
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return "не задан"
	}
}

func TestCrossSiteWriteHasSomeDefence(t *testing.T) {
	a := newAPI(t)
	owner := a.registerOrg("Компания под чужой страницей")
	cookie := a.loginCookie(owner.email, "parol12345")

	// Сперва — что подделывается настоящая запись. Без этого отказ ниже
	// ничего не значил бы: его мог бы дать пропавший маршрут или сессия,
	// не доехавшая до сервера.
	if code, raw := a.createBoard(cookie, "application/json", "", "Своя запись"); code != http.StatusCreated {
		t.Fatalf("своя запись: код %d, ожидался 201; тело: %s", code, raw)
	}

	// А теперь тот же запрос, каким его отправит форма с чужой страницы:
	// Content-Type — text/plain, других форме не дают, и предварительный
	// запрос на него не требуется, так что до сервера он доходит; тело
	// при этом остаётся тем же JSON.
	code, raw := a.createBoard(cookie, "text/plain", "https://chuzhaya.example", "С чужой страницы")
	if code < 400 {
		switch cookie.SameSite {
		case http.SameSiteLaxMode, http.SameSiteStrictMode:
			// Обещание держит браузер: с чужой страницы он эту cookie
			// на POST не отправит, и до обработчика запрос не дойдёт.
			// Сервер согласился потому, что здесь cookie приложена руками.
		default:
			t.Fatalf("запись с чужой страницы принята (код %d), а cookie сессии отдана "+
				"с SameSite=%s — значит, межсайтовую подделку не останавливает ничто.\n"+
				"Одно из двух: вернуть SameSiteLaxMode в auth.SetCookie либо "+
				"сверять Origin на изменяющих маршрутах.\nтело: %s",
				code, sameSiteName(cookie.SameSite), raw)
		}
	}
}
