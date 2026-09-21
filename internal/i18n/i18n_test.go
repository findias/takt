package i18n

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLanguageOfTheRequest(t *testing.T) {
	cases := []struct {
		accept, cookie string
		want           Lang
	}{
		// Интеграция без заголовка получает прежний, русский ответ.
		{"", "", RU},
		{"en", "", EN},
		{"en-GB,en;q=0.9", "", EN},
		{"ru-RU,ru;q=0.9,en;q=0.8", "", RU},
		{"de-DE,en;q=0.5,ru;q=0.4", "", EN},
		{"de-DE,fr", "", RU},
		{"en;q=0", "", RU},
		// Выбор в «Оформлении» сильнее языка браузера.
		{"en", "ru", RU},
		{"ru", "en", EN},
		{"ru", "xx", RU},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept-Language", c.accept)
		}
		if c.cookie != "" {
			r.Header.Set("Cookie", "lang="+c.cookie)
		}
		if got := FromRequest(r); got != c.want {
			t.Errorf("Accept-Language %q, cookie %q: %s, ожидался %s", c.accept, c.cookie, got, c.want)
		}
	}
}

func TestSay(t *testing.T) {
	cases := []struct{ ru, en string }{
		{"доска не найдена", "board not found"},
		// Подстановка: пользовательское имя переносится как есть.
		{"колонка «Тест», где стояла карточка, тоже в архиве — сначала верните её",
			"column “Тест”, where the card was, is archived too — restore it first"},
		// Составное: подставленное само оказывается сообщением.
		{"разбор MOVE_CARD: карточка уже удалена", "parsing MOVE_CARD: the card has already been deleted"},
		{"метка «срочно» принадлежит подразделения «Склад» и на этой доске не действует",
			"label “срочно” belongs to subdivision “Склад” and does not apply on this board"},
		{"метка «x» уже есть у доски «Пост» и действует здесь же — вторая с тем же названием на одной карточке была бы неотличима",
			"label “x” already exists on board “Пост” and applies here too — a second one with the same name would be indistinguishable on a card"},
		// Незнакомое остаётся как было, а не пропадает.
		{"что-то совсем новое", "что-то совсем новое"},
	}
	for _, c := range cases {
		if got := Say(EN, c.ru); got != c.en {
			t.Errorf("%q\n\tполучили %q\n\tожидали  %q", c.ru, got, c.en)
		}
		if got := Say(RU, c.ru); got != c.ru {
			t.Errorf("по-русски сообщение изменилось: %q", got)
		}
	}
}

// Шаблоны каталога собираются без паники и каждый узнаёт сам себя:
// иначе ключ с опечаткой в глаголе молча не сработает.
func TestEveryTemplateMatchesItself(t *testing.T) {
	for ru := range en {
		probe := strings.ReplaceAll(strings.ReplaceAll(ru, "%s", "Икс"), "%%", "%")
		if Say(EN, probe) == probe {
			t.Errorf("шаблон не узнаёт сам себя: %q", ru)
		}
	}
}
