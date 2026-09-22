package help

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Поиск по справке (ROADMAP 30.3). На сервере, обычной формой: страница
// справки обходится без скриптов, и поиску они тоже не нужны.
//
// Ищет по разделам — заголовку второго или третьего уровня и тексту
// под ним, — а не по страницам: страницы длинные, и «нашлось на
// странице „Как сделать“» не говорит, куда смотреть.

// Найденное — раздел, где встретились все слова запроса.
type Найденное struct {
	Страница  Страница
	Якорь     string
	Заголовок string
	// Отрывок — HTML: текст раздела вокруг первого совпадения,
	// совпадения выделены <mark>.
	Отрывок string
}

var (
	началоРаздела = regexp.MustCompile(`<h([23]) id="([^"]+)">(.*?)</h[23]>`)
	слово         = regexp.MustCompile(`[\p{L}\p{N}]+`)
	пробелы       = regexp.MustCompile(`\s+`)
)

type раздел struct {
	страница  Страница
	якорь     string
	заголовок string
	текст     string
	строчный  string
}

// разделы — текст открытой справки, разрезанный по заголовкам.
func разделы(язык string) ([]раздел, error) {
	var все []раздел
	for _, с := range Страницы {
		готово, err := тело(с, язык)
		if err != nil {
			return nil, err
		}
		места := началоРаздела.FindAllStringSubmatchIndex(готово.тело, -1)
		for i, м := range места {
			конец := len(готово.тело)
			if i+1 < len(места) {
				конец = места[i+1][0]
			}
			заголовок := html.UnescapeString(тег.ReplaceAllString(готово.тело[м[6]:м[7]], ""))
			текст := html.UnescapeString(тег.ReplaceAllString(готово.тело[м[1]:конец], " "))
			текст = strings.TrimSpace(пробелы.ReplaceAllString(текст, " "))
			все = append(все, раздел{
				страница: с, якорь: готово.тело[м[4]:м[5]], заголовок: заголовок, текст: текст,
				строчный: strings.ToLower(заголовок + " " + текст),
			})
		}
	}
	return все, nil
}

// основа — начало слова без окончания: «блокировка» ищет и «блокировки»,
// «карточку» — и «карточка». Грубо, но русский без этого не ищется
// вовсе, а словаря словоформ в закрытом контуре взять негде.
func основа(w string) string {
	n := utf8.RuneCountInString(w)
	if n < 6 {
		return w
	}
	return string([]rune(w)[:n-2])
}

// Искать — разделы, где встретились все слова запроса. Сначала те,
// где слова есть в заголовке: раздел, который так и называется, нужнее
// раздела, где слово упомянуто мимоходом.
func Искать(язык, запрос string) ([]Найденное, error) {
	var основы []string
	for _, w := range слово.FindAllString(strings.ToLower(запрос), -1) {
		основы = append(основы, основа(w))
	}
	if len(основы) == 0 {
		return nil, nil
	}
	все, err := разделы(язык)
	if err != nil {
		return nil, err
	}
	var вЗаголовке, вТексте []Найденное
	for _, р := range все {
		подошёл := true
		for _, о := range основы {
			if !strings.Contains(р.строчный, о) {
				подошёл = false
				break
			}
		}
		if !подошёл {
			continue
		}
		н := Найденное{Страница: р.страница, Якорь: р.якорь, Заголовок: р.заголовок,
			Отрывок: отрывок(р.текст, основы)}
		if strings.Contains(strings.ToLower(р.заголовок), основы[0]) {
			вЗаголовке = append(вЗаголовке, н)
		} else {
			вТексте = append(вТексте, н)
		}
	}
	return append(вЗаголовке, вТексте...), nil
}

// отрывок — около двухсот знаков вокруг первого совпадения, совпадения
// выделены. Режется по рунам: кириллица в байтах двойная, и срез
// по байтам рвал бы букву пополам.
func отрывок(текст string, основы []string) string {
	руны := []rune(текст)
	строчные := []rune(strings.ToLower(текст))
	первое := -1
	for _, о := range основы {
		if i := индекс(строчные, []rune(о)); i >= 0 && (первое < 0 || i < первое) {
			первое = i
		}
	}
	от, до := 0, len(руны)
	if первое > 80 {
		от = первое - 80
	}
	if от+200 < до {
		до = от + 200
	}
	кусок := html.EscapeString(string(руны[от:до]))
	for _, о := range основы {
		кусок = выделить(кусок, о)
	}
	if от > 0 {
		кусок = "…" + кусок
	}
	if до < len(руны) {
		кусок += "…"
	}
	return кусок
}

func индекс(в, что []rune) int {
	for i := 0; i+len(что) <= len(в); i++ {
		if string(в[i:i+len(что)]) == string(что) {
			return i
		}
	}
	return -1
}

// выделить оборачивает совпадения в <mark>, не глядя на регистр.
func выделить(текст, основа string) string {
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(html.EscapeString(основа)))
	return re.ReplaceAllString(текст, "<mark>$0</mark>")
}

var словаПоиска = map[string]struct {
	поле, кнопка, заголовок, ничего, сколько string
}{
	"ru": {"Поиск по справке", "Найти", "Поиск",
		"По запросу «%s» ничего не нашлось. Попробуйте другое слово — или начните с раздела «Как сделать».",
		"Нашлось разделов: %d"},
	"en": {"Search help", "Search", "Search",
		"Nothing found for “%s”. Try another word — or start with “How to”.",
		"Sections found: %d"},
}

// формаПоиска — в оглавлении каждой страницы справки.
func формаПоиска(язык, запрос string) string {
	w := словаПоиска[язык]
	return fmt.Sprintf(`<form class="help-search" role="search" action="/help/%s/search">`+
		`<input type="search" name="q" value="%s" aria-label="%s" placeholder="%s">`+
		`<button type="submit">%s</button></form>`+"\n",
		язык, html.EscapeString(запрос), w.поле, w.поле, w.кнопка)
}

// СобратьПоиск — страница результатов в оболочке справки.
func СобратьПоиск(язык, запрос, версия string) (string, bool, error) {
	if язык != "ru" && язык != "en" {
		return "", false, nil
	}
	найдено, err := Искать(язык, запрос)
	if err != nil {
		return "", false, err
	}
	w := словаПоиска[язык]
	var b strings.Builder
	fmt.Fprintf(&b, "<h1>%s</h1>\n", w.заголовок)
	запрос = strings.TrimSpace(запрос)
	switch {
	case запрос == "":
	case len(найдено) == 0:
		fmt.Fprintf(&b, "<p>%s</p>\n", fmt.Sprintf(w.ничего, html.EscapeString(запрос)))
	default:
		fmt.Fprintf(&b, "<p class=\"help-count\">%s</p>\n<ol class=\"help-results\">\n", fmt.Sprintf(w.сколько, len(найдено)))
		for _, н := range найдено {
			имя := н.Страница.ИмяEn
			if язык == "ru" {
				имя = н.Страница.ИмяRu
			}
			fmt.Fprintf(&b, "<li><a href=\"/help/%s/%s#%s\">%s</a> <span class=\"help-where\">%s</span>"+
				"<p>%s</p></li>\n", язык, н.Страница.Адрес, н.Якорь, html.EscapeString(н.Заголовок),
				html.EscapeString(имя), н.Отрывок)
		}
		b.WriteString("</ol>\n")
	}
	return оболочка(Страница{Адрес: "search"}, язык, собранная{заголовок: w.заголовок, тело: b.String()}, версия, запрос), true, nil
}
