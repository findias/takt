// Отрисовка документации в HTML.
//
// Источник один — те же файлы `.md`, из которых читают в репозитории.
// Второй набор текстов, набранный в HTML руками, разошёлся бы с первым
// в тот же день, и разошёлся бы молча: у текста нет прогона, который
// упадёт.
//
// Разметка своя, без библиотеки, и это не гордость, а правило проекта:
// установка не должна требовать интернета, а зависимость ради разбора
// заголовков и таблиц — плата за то, что делается двумя сотнями строк.
// Разбирается ровно то, что встречается в наших документах; всё
// остальное отрисовщик оставляет как есть, а не догадывается.
package docs

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Страница — один документ: имя файла, заголовок и готовый HTML.
type Страница struct {
	Файл string
	// Заголовок — первый `#` документа: им называется вкладка браузера.
	Заголовок string
	// Имя — короткое название раздела в оглавлении. Отличается
	// от заголовка намеренно: «Доска» в заголовке обзора и в заголовке
	// руководства по установке — одно слово, а разделы разные.
	Имя  string
	HTML string
}

var (
	заголовок   = regexp.MustCompile(`^(#{1,4})\s+(.*)$`)
	списокТире  = regexp.MustCompile(`^[-*]\s+(.*)$`)
	списокЧисло = regexp.MustCompile(`^\d+\.\s+(.*)$`)
	разделитель = regexp.MustCompile(`^\|[\s:|-]+\|$`)
	ссылка      = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	картинка    = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)]+)\)\s*$`)
	жирный      = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	код         = regexp.MustCompile("`([^`]+)`")
)

// Отрисовать переводит markdown в HTML тела страницы.
func Отрисовать(источник string) (заголовокСтраницы, тело string) {
	строки := strings.Split(источник, "\n")
	var out strings.Builder
	var список []string
	видСписка := ""
	var таблица [][]string
	вКоде := false
	var кодовый []string

	закрытьСписок := func() {
		if len(список) == 0 {
			return
		}
		fmt.Fprintf(&out, "<%s>\n", видСписка)
		for _, пункт := range список {
			fmt.Fprintf(&out, "<li>%s</li>\n", пункт)
		}
		fmt.Fprintf(&out, "</%s>\n", видСписка)
		список = nil
		видСписка = ""
	}
	закрытьТаблицу := func() {
		if len(таблица) == 0 {
			return
		}
		out.WriteString("<div class=\"tablewrap\"><table>\n<thead><tr>")
		for _, ячейка := range таблица[0] {
			fmt.Fprintf(&out, "<th>%s</th>", строчный(ячейка))
		}
		out.WriteString("</tr></thead>\n<tbody>\n")
		for _, строка := range таблица[1:] {
			out.WriteString("<tr>")
			for _, ячейка := range строка {
				fmt.Fprintf(&out, "<td>%s</td>", строчный(ячейка))
			}
			out.WriteString("</tr>\n")
		}
		out.WriteString("</tbody>\n</table></div>\n")
		таблица = nil
	}

	for i := 0; i < len(строки); i++ {
		строка := строки[i]

		if strings.HasPrefix(строка, "```") {
			if вКоде {
				fmt.Fprintf(&out, "<pre><code>%s</code></pre>\n",
					html.EscapeString(strings.Join(кодовый, "\n")))
				кодовый = nil
				вКоде = false
			} else {
				закрытьСписок()
				закрытьТаблицу()
				вКоде = true
			}
			continue
		}
		if вКоде {
			кодовый = append(кодовый, строка)
			continue
		}

		if strings.TrimSpace(строка) == "" {
			закрытьСписок()
			закрытьТаблицу()
			continue
		}

		// Картинка — отдельный блок, а не часть абзаца: подпись
		// в alt обязательна, иначе снимок для незрячего — пустое место
		// посреди текста.
		if m := картинка.FindStringSubmatch(строка); m != nil {
			закрытьСписок()
			закрытьТаблицу()
			fmt.Fprintf(&out, `<figure><img src="%s" alt="%s" loading="lazy">`+"\n"+
				`<figcaption>%s</figcaption></figure>`+"\n",
				html.EscapeString(m[2]), html.EscapeString(m[1]), html.EscapeString(m[1]))
			continue
		}

		if m := заголовок.FindStringSubmatch(строка); m != nil {
			закрытьСписок()
			закрытьТаблицу()
			уровень := len(m[1])
			текст := строчный(m[2])
			if уровень == 1 && заголовокСтраницы == "" {
				заголовокСтраницы = обычныйТекст(m[2])
			}
			fmt.Fprintf(&out, "<h%d>%s</h%d>\n", уровень, текст, уровень)
			continue
		}

		// Таблица: строка, начинающаяся и кончающаяся вертикальной чертой.
		if strings.HasPrefix(строка, "|") && strings.HasSuffix(strings.TrimSpace(строка), "|") {
			закрытьСписок()
			if разделитель.MatchString(strings.TrimSpace(строка)) {
				continue
			}
			ячейки := strings.Split(strings.Trim(strings.TrimSpace(строка), "|"), "|")
			for j := range ячейки {
				ячейки[j] = strings.TrimSpace(ячейки[j])
			}
			таблица = append(таблица, ячейки)
			continue
		}
		закрытьТаблицу()

		if m := списокТире.FindStringSubmatch(строка); m != nil {
			if видСписка != "ul" {
				закрытьСписок()
				видСписка = "ul"
			}
			список = append(список, строчный(m[1]))
			continue
		}
		if m := списокЧисло.FindStringSubmatch(строка); m != nil {
			if видСписка != "ol" {
				закрытьСписок()
				видСписка = "ol"
			}
			список = append(список, строчный(m[1]))
			continue
		}
		// Продолжение пункта списка: строка с отступом под ним.
		if len(список) > 0 && strings.HasPrefix(строка, "  ") {
			список[len(список)-1] += " " + строчный(strings.TrimSpace(строка))
			continue
		}

		// Абзац: собираем до пустой строки, чтобы перенос в исходнике
		// не превращался в перенос на экране.
		абзац := []string{строка}
		for i+1 < len(строки) && strings.TrimSpace(строки[i+1]) != "" &&
			!strings.HasPrefix(строки[i+1], "#") && !strings.HasPrefix(строки[i+1], "|") &&
			!strings.HasPrefix(строки[i+1], "```") &&
			списокТире.FindStringSubmatch(строки[i+1]) == nil &&
			списокЧисло.FindStringSubmatch(строки[i+1]) == nil {
			i++
			абзац = append(абзац, строки[i])
		}
		fmt.Fprintf(&out, "<p>%s</p>\n", строчный(strings.Join(абзац, " ")))
	}
	закрытьСписок()
	закрытьТаблицу()
	return заголовокСтраницы, out.String()
}

// строчный разбирает то, что живёт внутри строки: код, жирный, ссылки.
// Порядок важен: сперва экранирование, потом код — иначе разметка
// внутри кода будет разобрана как разметка.
func строчный(текст string) string {
	экранированный := html.EscapeString(текст)
	экранированный = код.ReplaceAllString(экранированный, "<code>$1</code>")
	экранированный = жирный.ReplaceAllString(экранированный, "<strong>$1</strong>")
	экранированный = ссылка.ReplaceAllStringFunc(экранированный, func(m string) string {
		части := ссылка.FindStringSubmatch(m)
		цель := части[2]
		// Внутренние ссылки ведут на соседнюю страницу, а не на исходник.
		if strings.HasSuffix(цель, ".md") {
			цель = strings.TrimSuffix(цель, ".md") + ".html"
			цель = strings.TrimPrefix(цель, "../")
		}
		return fmt.Sprintf(`<a href="%s">%s</a>`, цель, части[1])
	})
	return экранированный
}

func обычныйТекст(текст string) string {
	текст = код.ReplaceAllString(текст, "$1")
	return жирный.ReplaceAllString(текст, "$1")
}
