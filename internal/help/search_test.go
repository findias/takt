package help

import (
	"strings"
	"testing"
)

func TestSearchFindsSectionsAndLeadsToThem(t *testing.T) {
	for _, случай := range []struct {
		язык, запрос, ждём string
	}{
		// Другая форма слова: в справке «Заблокировать работу до срока»
		// и «Блокировки со сроком», а ищут «блокировка».
		{"ru", "блокировка", "reference#"},
		{"ru", "Сменить язык", "howto#language"},
		{"en", "interface language", "howto#language"},
		{"en", "flow metrics", "reference#flow"},
	} {
		найдено, err := Искать(случай.язык, случай.запрос)
		if err != nil {
			t.Fatal(err)
		}
		if len(найдено) == 0 {
			t.Errorf("%s «%s»: ничего не нашлось", случай.язык, случай.запрос)
			continue
		}
		адреса := []string{}
		for _, н := range найдено {
			адрес := н.Страница.Адрес + "#" + н.Якорь
			адреса = append(адреса, адрес)
			// Каждое найденное ведёт на существующий раздел.
			if !якоря(собрать(t, случай.язык, н.Страница.Адрес))[н.Якорь] {
				t.Errorf("%s «%s»: найдено %s, а такого раздела нет", случай.язык, случай.запрос, адрес)
			}
			if !strings.Contains(н.Отрывок, "<mark>") && !strings.Contains(strings.ToLower(н.Заголовок), основа(strings.Fields(strings.ToLower(случай.запрос))[0])) {
				t.Errorf("%s «%s»: в отрывке %s совпадение не выделено", случай.язык, случай.запрос, адрес)
			}
		}
		if !strings.Contains(strings.Join(адреса, " "), случай.ждём) {
			t.Errorf("%s «%s»: ждали %s среди %v", случай.язык, случай.запрос, случай.ждём, адреса)
		}
	}
}

func TestSearchPageSaysWhatToDoWhenNothingIsFound(t *testing.T) {
	страница, ok, err := СобратьПоиск("ru", "квазиабракадабра", "проверка")
	if err != nil || !ok {
		t.Fatalf("страница поиска: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(страница, "ничего не нашлось") || !strings.Contains(страница, "Как сделать") {
		t.Error("пустой поиск не говорит, что делать дальше")
	}
	// Запрос попадает в страницу экранированным: поле поиска — вход
	// извне, и разметка из него не должна стать разметкой.
	страница, _, _ = СобратьПоиск("ru", `<script>alert(1)</script>`, "проверка")
	if strings.Contains(страница, "<script>alert") {
		t.Error("запрос попал в страницу без экранирования")
	}
	if _, ok, _ := СобратьПоиск("de", "x", "проверка"); ok {
		t.Error("поиск на незнакомом языке собрался")
	}
}
