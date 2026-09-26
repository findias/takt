// Package help — справка внутри приложения (ROADMAP 30.3).
//
// Источник один — те же `docs/*.md` и их переводы, что читают
// в репозитории и собирают в docs/html. Второй набор текстов «для
// приложения» разошёлся бы с первым в первую же неделю. Страницы
// собираются на лету из исходников, вшитых в бинарник (пакет docsrc),
// поэтому справка всегда той же версии, что приложение.
//
// Адреса одинаковые у обоих языков: `/help/ru/howto#board` и
// `/help/en/howto#board`. Приложение знает один адрес на экран,
// а язык подставляет свой; якоря у оригинала и перевода общие
// (`<!-- anchor: … -->` в исходнике).
package help

import (
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"

	docsrc "github.com/findias/takt/docs"
	"github.com/findias/takt/internal/docs"
)

// Страница справки: адрес, исходники на двух языках, имя в оглавлении.
type Страница struct {
	Адрес        string
	En, Ru       string
	ИмяEn, ИмяRu string
	// Для администратора установки: ставящему и проверяющему, а не тому,
	// кто работает на доске. В оглавлении — отдельной группой ниже,
	// чтобы человек с доски не начинал чтение с Kubernetes.
	Админ bool
}

// Страницы — порядок чтения, тот же, что у собранной документации:
// обучение, задача, справка, объяснение, затем установка.
var Страницы = []Страница{
	{"overview", "overview.md", "ru/индекс.md", "Overview", "О продукте", false},
	{"quickstart", "quickstart.md", "ru/старт.md", "First 15 minutes", "Первые 15 минут", false},
	{"howto", "howto.md", "ru/как.md", "How to", "Как сделать", false},
	{"reference", "reference.md", "ru/справочник.md", "Reference", "Справочник", false},
	{"cheatsheet", "cheatsheet.md", "ru/памятка.md", "Cheat sheet", "Памятка", false},
	{"glossary", "glossary.md", "ru/словарь.md", "Glossary", "Словарь", false},
	{"architecture", "architecture.md", "ru/устройство.md", "Design decisions", "Устройство", true},
	{"install", "install.md", "ru/установка.md", "Installation", "Установка", true},
	{"security-review", "security-review.md", "ru/проверка-иб.md", "Security review", "Проверка ИБ", true},
	{"import-package", "import-package.md", "ru/пакет-переноса.md", "Import package", "Пакет переноса", true},
	{"takt-fetch", "takt-fetch.md", "ru/выгрузчик.md", "takt-fetch", "Выгрузчик", true},
}

// Исходники, которых в справке нет (требования, список изменений, README),
// открываются на GitHub: там они и живут.
const репозиторий = "https://github.com/findias/takt/blob/master/"

var (
	ссылкаНаСтраницу = regexp.MustCompile(`href="([^"#:]+)\.html(#[^"]*)?"`)
	разделСтраницы   = regexp.MustCompile(`<h2 id="([^"]+)">(.*?)</h2>`)
	тег              = regexp.MustCompile(`<[^>]+>`)
)

// адресПоФайлу — какой странице справки соответствует собранный файл
// (`howto.html`, `как.html`) на любом из языков.
var адресПоФайлу = func() map[string]string {
	m := map[string]string{}
	for _, с := range Страницы {
		m[strings.TrimSuffix(path.Base(с.En), ".md")] = с.Адрес
		m[strings.TrimSuffix(path.Base(с.Ru), ".md")] = с.Адрес
	}
	return m
}()

// Найти — страница по адресу.
func Найти(адрес string) (Страница, bool) {
	for _, с := range Страницы {
		if с.Адрес == адрес {
			return с, true
		}
	}
	return Страница{}, false
}

type собранная struct {
	заголовок, тело string
}

var (
	кэш   = map[string]собранная{}
	кэшMu sync.Mutex
)

// тело — HTML страницы без оболочки. Собирается один раз: исходники
// вшиты и за время жизни процесса не меняются.
func тело(с Страница, язык string) (собранная, error) {
	ключ := язык + "/" + с.Адрес
	кэшMu.Lock()
	defer кэшMu.Unlock()
	if готово, ok := кэш[ключ]; ok {
		return готово, nil
	}
	файл := с.En
	if язык == "ru" {
		файл = с.Ru
	}
	raw, err := fs.ReadFile(docsrc.FS, файл)
	if err != nil {
		return собранная{}, err
	}
	// Русские — вложенными, как в собранной документации: тогда ссылка
	// наружу из `ru/` сохраняет `../`, и по ней видно, что она ведёт
	// на английскую страницу.
	отрисовать := docs.Отрисовать
	if язык == "ru" {
		отрисовать = docs.ОтрисоватьВложенную
	}
	заголовок, html := отрисовать(string(raw))
	готово := собранная{заголовок: заголовок, тело: переписатьСсылки(html, язык)}
	кэш[ключ] = готово
	return готово, nil
}

// переписатьСсылки ведёт ссылки между страницами документации на
// страницы справки того же языка, а на остальные исходники — на GitHub.
func переписатьСсылки(тело, язык string) string {
	return ссылкаНаСтраницу.ReplaceAllStringFunc(тело, func(m string) string {
		части := ссылкаНаСтраницу.FindStringSubmatch(m)
		имя := path.Base(части[1])
		якорь := части[2]
		if адрес, ok := адресПоФайлу[имя]; ok {
			// Ссылка на другой язык — «Read in English» с русской страницы,
			// «По-русски» с английской — ведёт на страницу того языка,
			// куда вела в исходнике.
			цель := язык
			switch {
			case язык == "ru" && strings.HasPrefix(части[1], "../"):
				цель = "en"
			case язык == "en" && strings.HasPrefix(части[1], "ru/"):
				цель = "ru"
			}
			return fmt.Sprintf(`href="/help/%s/%s%s"`, цель, адрес, якорь)
		}
		return fmt.Sprintf(`href="%s%s.md%s"`, репозиторий, имя, якорь)
	})
}

// Собрать — готовая страница справки. ok=false — такой страницы нет.
func Собрать(язык, адрес, версия string) (string, bool, error) {
	if адрес == адресНового {
		return СобратьНовое(язык, версия)
	}
	с, ok := Найти(адрес)
	if !ok || (язык != "ru" && язык != "en") {
		return "", false, nil
	}
	готово, err := тело(с, язык)
	if err != nil {
		return "", false, err
	}
	return оболочка(с, язык, готово, версия, ""), true, nil
}

// Снимок экрана со страницы справки. Имя берётся только из вшитого
// каталога: путь с `..` в нём просто не найдётся.
func Снимок(язык, имя string) ([]byte, bool) {
	каталог := "screenshots/"
	if язык == "ru" {
		каталог = "ru/screenshots/"
	}
	if strings.ContainsAny(имя, "/\\") {
		return nil, false
	}
	raw, err := fs.ReadFile(docsrc.FS, каталог+имя)
	return raw, err == nil
}

type слова struct {
	справка, назад, разделы, админ, язык, другой, другойКод, подпись string
	// версия — строка под заголовком справки: номер выпуска, его
	// изменения и репозиторий (задание владельца 26.09.2026, этап 35.3).
	версия, изменения, гитхаб string
}

var словарь = map[string]слова{
	"ru": {"Справка Takt", "← Вернуться в Takt", "Разделы справки",
		"Для администратора установки", "Язык справки", "English", "en",
		"Takt, версия %s. Справка собрана из той же версии, что приложение.",
		"Версия %s", "Что нового", "Takt на GitHub"},
	"en": {"Takt help", "← Back to Takt", "Help sections",
		"For whoever installs it", "Help language", "Русский", "ru",
		"Takt, version %s. This help is built from the same version as the app.",
		"Version %s", "What’s new", "Takt on GitHub"},
}

// Стили — ровно то, что стоит внутри <style> каждой страницы справки.
// Сервер считает от этого текста хеш для политики содержимого: общая
// политика встроенных стилей не допускает (разбор ZAP, 26.09.2026),
// а справке разрешён ровно её собственный <style> и ничего больше.
func Стили() string { return docs.Стиль() + стильСправки }

// Оформление справки поверх оформления документации: оглавление слева,
// на узком экране — сверху.
const стильСправки = `
.help{display:grid;grid-template-columns:16rem minmax(0,1fr);min-height:100vh}
.help-side{position:sticky;top:0;align-self:start;max-height:100vh;min-height:100vh;overflow:auto;
padding:1.5rem 1rem;border-right:1px solid var(--rule);background:var(--surface)}
.help-side nav{display:block;border:0;padding:0;margin:0 0 1.25rem}
.help-version{font-size:.85rem;color:var(--ink-3);margin:.5rem 0 1rem}
.help-side nav a{display:block;margin:.1rem 0}
.help-side .help-sub{margin:.1rem 0 .5rem .75rem;padding-left:.5rem;
border-left:1px solid var(--rule)}
.help-side .help-sub a{font-size:.9rem;padding:.15rem .5rem}
.help-back{display:inline-block;margin-bottom:1rem;font-weight:600;text-decoration:none}
.help-title{font-size:.8rem;text-transform:uppercase;letter-spacing:.06em;
color:var(--ink-3);margin:0 0 .4rem}
.help-lang{margin:0 0 1.25rem;font-size:.9rem}
.help .sheet{margin:0;max-width:46rem}
/* Таблица в собранной документации шире колонки текста и выступает
   в обе стороны; здесь слева оглавление, и выступ уводил её первый
   столбец под него. В справке таблица — по ширине текста и листается
   вбок, если не влезла. */
.help .tablewrap{width:auto;margin-left:0}
h2[id],h3[id]{scroll-margin-top:1rem}
:target{background:var(--accent-soft);border-radius:var(--radius);
box-shadow:0 0 0 .4rem var(--accent-soft)}
@media (max-width:52rem){.help{display:block}
.help-side{position:static;max-height:none;min-height:0;border-right:0;border-bottom:1px solid var(--rule)}}
.help-search{display:flex;gap:.4rem;margin:0 0 1.25rem}
.help-search input{flex:1;min-width:0;font:inherit;font-size:.9rem;padding:.35rem .5rem;
border:1px solid var(--ink-3);border-radius:var(--radius);background:var(--paper);color:var(--ink)}
.help-search button{font:inherit;font-size:.9rem;padding:.35rem .7rem;cursor:pointer;
border:1px solid var(--accent);border-radius:var(--radius);background:var(--accent);color:var(--surface)}
.help-search button:hover{filter:brightness(1.1)}
.help-results{padding-left:1.25rem}
.help-results li{margin:0 0 1.1rem}
.help-results p{margin:.2rem 0 0;color:var(--ink-2)}
.help-where{color:var(--ink-3);font-size:.85rem;margin-left:.35rem}
.help-count{color:var(--ink-3)}
/* Выделение без отступов: отступ внутри слова разрывает его надвое —
   «За блокиров ать». */
mark{background:var(--accent-soft);color:inherit}
@media print{.help{display:block}.help-side{display:none}}
`

func оболочка(с Страница, язык string, готово собранная, версия, запрос string) string {
	w := словарь[язык]
	имяСтраницы := func(x Страница) string {
		if язык == "ru" {
			return x.ИмяRu
		}
		return x.ИмяEn
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html>\n<html lang=%q>\n<head>\n<meta charset=\"utf-8\">\n", язык)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	fmt.Fprintf(&b, "<title>%s · %s</title>\n", html.EscapeString(готово.заголовок), w.справка)
	// Значок вкладки — тот же, что у приложения: справку открывают
	// рядом с доской, и две вкладки одного продукта должны узнаваться.
	b.WriteString("<link rel=\"icon\" href=\"/favicon.svg\" type=\"image/svg+xml\">\n")
	fmt.Fprintf(&b, "<style>%s</style>\n</head>\n<body>\n<div class=\"help\">\n", Стили())

	fmt.Fprintf(&b, "<aside class=\"help-side\" aria-label=%q>\n", w.разделы)
	fmt.Fprintf(&b, "<a class=\"help-back\" href=\"/\">%s</a>\n", w.назад)
	// Переход на другой язык ведёт туда же — и с тем же запросом, если
	// открыт поиск.
	туда := с.Адрес
	if с.Адрес == "search" {
		туда += "?q=" + url.QueryEscape(запрос)
	}
	fmt.Fprintf(&b, "<p class=\"help-lang\">%s: <a href=\"/help/%s/%s\" lang=%q hreflang=%q>%s</a></p>\n",
		w.язык, w.другойКод, html.EscapeString(туда), w.другойКод, w.другойКод, w.другой)
	b.WriteString(формаПоиска(язык, запрос))

	группа := func(админ bool) {
		fmt.Fprintf(&b, "<nav aria-label=%q>\n", map[bool]string{false: w.справка, true: w.админ}[админ])
		for _, x := range Страницы {
			if x.Админ != админ {
				continue
			}
			текущая := ""
			if x.Адрес == с.Адрес {
				текущая = ` aria-current="page"`
			}
			fmt.Fprintf(&b, "<a href=\"/help/%s/%s\"%s>%s</a>\n", язык, x.Адрес, текущая, html.EscapeString(имяСтраницы(x)))
			// У открытой страницы — её разделы: страницы длинные,
			// и без них до нужного раздела листают вслепую.
			if x.Адрес == с.Адрес {
				разделы := разделСтраницы.FindAllStringSubmatch(готово.тело, -1)
				if len(разделы) > 0 {
					b.WriteString("<div class=\"help-sub\">\n")
					for _, р := range разделы {
						fmt.Fprintf(&b, "<a href=\"#%s\">%s</a>\n", р[1], тег.ReplaceAllString(р[2], ""))
					}
					b.WriteString("</div>\n")
				}
			}
		}
		// «Что нового» — последним пунктом справки для работающих:
		// страницы у неё нет, она собирается из списка изменений.
		if !админ {
			текущая := ""
			if с.Адрес == адресНового {
				текущая = ` aria-current="page"`
			}
			fmt.Fprintf(&b, "<a href=\"/help/%s/%s\"%s>%s</a>\n", язык, адресНового, текущая, словаНового[язык].имя)
		}
		b.WriteString("</nav>\n")
	}
	// Номер выпуска, его изменения и репозиторий — наверху, на виду:
	// первое, что спрашивают о справке, — к какой она версии, и что
	// в этой версии нового.
	fmt.Fprintf(&b, "<p class=\"help-version\">%s · <a href=\"/help/%s/%s\">%s</a> · <a href=\"%s\">%s</a></p>\n",
		html.EscapeString(fmt.Sprintf(w.версия, версия)), язык, адресНового, w.изменения,
		"https://github.com/findias/takt", w.гитхаб)
	fmt.Fprintf(&b, "<p class=\"help-title\">%s</p>\n", w.справка)
	группа(false)
	fmt.Fprintf(&b, "<p class=\"help-title\">%s</p>\n", w.админ)
	группа(true)
	b.WriteString("</aside>\n")

	b.WriteString("<main class=\"sheet\">\n")
	b.WriteString(готово.тело)
	fmt.Fprintf(&b, "<footer>%s</footer>\n", fmt.Sprintf(w.подпись, html.EscapeString(версия)))
	b.WriteString("</main>\n</div>\n</body>\n</html>\n")
	return b.String()
}
