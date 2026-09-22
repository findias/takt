package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Справка внутри приложения (ROADMAP 30.3): открывается без входа,
// корень ведёт на язык пришедшего, а несуществующий адрес объясняет,
// куда идти.
func TestHelpOpensWithoutSigningInInTheVisitorsLanguage(t *testing.T) {
	a := newAPI(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for lang, header := range map[string]string{"en": "en-GB,en;q=0.9", "ru": "ru-RU,ru;q=0.9"} {
		req, _ := http.NewRequest("GET", a.server.URL+"/help", nil)
		req.Header.Set("Accept-Language", header)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Location"); got != "/help/"+lang+"/overview" {
			t.Errorf("/help с %s ведёт на %q", header, got)
		}
	}

	code, body := a.session().do("GET", "/help/ru/howto", nil)
	if code != http.StatusOK || !strings.Contains(string(body), `id="board"`) {
		t.Errorf("/help/ru/howto: %d, раздела доски нет", code)
	}
	if code, _ := a.session().do("GET", "/help/en/screenshots/доска.png", nil); code != http.StatusOK {
		t.Errorf("снимок справки: %d", code)
	}
	// Поиск — обычной формой, результаты ведут в разделы.
	code, body = a.session().do("GET", "/help/ru/search?q="+url.QueryEscape("язык интерфейса"), nil)
	if code != http.StatusOK || !strings.Contains(string(body), `href="/help/ru/howto#language"`) {
		t.Errorf("поиск по справке: %d, раздела о языке среди найденного нет", code)
	}

	code, body = a.session().do("GET", "/help/ru/nothing", nil)
	if code != http.StatusNotFound || !strings.Contains(string(body), "/help/") {
		t.Errorf("несуществующая страница справки: %d %s", code, body)
	}
}
