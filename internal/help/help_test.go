package help

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Справка обязана держать три обещания (ROADMAP 30.3): у каждого экрана
// есть раздел, ссылка на него ведёт на существующий якорь, и обе
// языковые версии на месте. Проверяется на собранных страницах, а не
// на исходниках: читает человек то, что собрано.

var (
	якорьВСтранице = regexp.MustCompile(`id="([^"]+)"`)
	ссылкаСправки  = regexp.MustCompile(`href="/help/(ru|en)/([a-z-]+)(?:#([^"]*))?"`)
	картинкаВТеле  = regexp.MustCompile(`<img src="screenshots/([^"]+)"`)
	темаЭкрана     = regexp.MustCompile(`^\s*\w+: '([a-z-]+)#([a-z0-9-]+)',$`)
)

func собрать(t *testing.T, язык, адрес string) string {
	t.Helper()
	страница, ok, err := Собрать(язык, адрес, "проверка")
	if err != nil || !ok {
		t.Fatalf("/help/%s/%s не собралась: ok=%v err=%v", язык, адрес, ok, err)
	}
	return страница
}

func якоря(страница string) map[string]bool {
	есть := map[string]bool{}
	for _, m := range якорьВСтранице.FindAllStringSubmatch(страница, -1) {
		есть[m[1]] = true
	}
	return есть
}

func TestEveryPageExistsInBothLanguages(t *testing.T) {
	for _, с := range Страницы {
		for _, язык := range []string{"ru", "en"} {
			страница := собрать(t, язык, с.Адрес)
			if !strings.Contains(страница, `<html lang="`+язык+`">`) {
				t.Errorf("/help/%s/%s объявляет не тот язык", язык, с.Адрес)
			}
			if !strings.Contains(страница, "<h1>") {
				t.Errorf("/help/%s/%s без заголовка", язык, с.Адрес)
			}
		}
	}
	if _, ok, _ := Собрать("de", "overview", "проверка"); ok {
		t.Error("справка на незнакомом языке собралась")
	}
	if _, ok, _ := Собрать("ru", "../go.mod", "проверка"); ok {
		t.Error("справка собралась по адресу, которого нет в списке")
	}
}

// Темы экранов берутся из того самого файла, который читает клиент:
// список, переписанный сюда руками, разошёлся бы с ним молча.
func TestEveryScreenLeadsToAnExistingSection(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/shared/lib/help.ts")
	if err != nil {
		t.Fatal(err)
	}
	найдено := 0
	for _, строка := range strings.Split(string(raw), "\n") {
		m := темаЭкрана.FindStringSubmatch(строка)
		if m == nil {
			continue
		}
		найдено++
		for _, язык := range []string{"ru", "en"} {
			if !якоря(собрать(t, язык, m[1]))[m[2]] {
				t.Errorf("экран ведёт на /help/%s/%s#%s, а такого раздела нет: "+
					"якорь ставится строкой `<a id=\"%s\"></a>` над заголовком, "+
					"и в оригинале, и в переводе", язык, m[1], m[2], m[2])
			}
		}
	}
	if найдено < 5 {
		t.Fatalf("тем экранов нашлось %d — разбор help.ts сломался и ничего не проверяет", найдено)
	}
}

func TestLinksInsideHelpLeadSomewhere(t *testing.T) {
	for _, с := range Страницы {
		for _, язык := range []string{"ru", "en"} {
			страница := собрать(t, язык, с.Адрес)
			for _, m := range ссылкаСправки.FindAllStringSubmatch(страница, -1) {
				цель, ok, _ := Собрать(m[1], m[2], "проверка")
				if !ok {
					t.Errorf("/help/%s/%s ссылается на /help/%s/%s, а такой страницы нет",
						язык, с.Адрес, m[1], m[2])
					continue
				}
				if m[3] != "" && !якоря(цель)[m[3]] {
					t.Errorf("/help/%s/%s ссылается на /help/%s/%s#%s, а такого раздела нет",
						язык, с.Адрес, m[1], m[2], m[3])
				}
			}
			for _, m := range картинкаВТеле.FindAllStringSubmatch(страница, -1) {
				if _, ok := Снимок(язык, m[1]); !ok {
					t.Errorf("/help/%s/%s показывает снимок %s, а его нет", язык, с.Адрес, m[1])
				}
			}
		}
	}
}

// «Что нового» — часть списка изменений для работающих на доске, той
// версии, что открыта; у сборки между выпусками — самой новой.
func TestWhatsNewShowsTheUserPartOfTheRunningVersion(t *testing.T) {
	for язык, ждём := range map[string][]string{
		"ru": {"Что нового в v0.3.0", "Личные настройки — за именем", "полном списке"},
		"en": {"What’s new in v0.3.0", "Personal settings live behind your name", "the full list"},
	} {
		страница, ok, err := Собрать(язык, "whats-new", "v0.3.0")
		if err != nil || !ok {
			t.Fatalf("%s: «Что нового» не собралась: ok=%v err=%v", язык, ok, err)
		}
		for _, текст := range ждём {
			if !strings.Contains(страница, текст) {
				t.Errorf("%s: в «Что нового» нет %q", язык, текст)
			}
		}
		// Раздел для тех, кто ставит, сюда не попадает: работающему
		// на доске миграции и настройки ни к чему.
		if strings.Contains(страница, "0056") {
			t.Errorf("%s: в «Что нового» попало то, что для ставящих", язык)
		}
	}
	// Сборка между выпусками говорит, что она новее выпуска, — какой бы
	// раздел ни был самым новым к тому времени.
	страница, _, _ := Собрать("ru", "whats-new", "v0.2.3-40-gabcdef0")
	if !strings.Contains(страница, "Эта сборка новее последнего выпуска") {
		t.Error("сборка между выпусками не говорит, что она новее выпуска")
	}
	// Выпуск без изменений для работающих говорит об этом, а не молчит.
	страница, _, _ = Собрать("ru", "whats-new", "v0.2.3")
	if !strings.Contains(страница, "Что нового в v0.2.3") || !strings.Contains(страница, "ничего не менялось") {
		t.Error("у выпуска без пользовательских изменений «Что нового» не объясняет пустоту")
	}
}
