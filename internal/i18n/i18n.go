// Package i18n переводит то, что сервер говорит человеку: отказы
// и названия, которые сервер заводит сам.
//
// Исходный язык — русский, и сообщения в коде остаются русскими:
// каталог ключуется самим русским текстом, а не идентификаторами.
// Иначе ради перевода пришлось бы переписать каждую ошибку в коде,
// а русский текст в месте, где ошибка рождается, — то, что читает
// разработчик. Цена — ключ меняется вместе с формулировкой; её платит
// проверка полноты (extract_test.go), которая собирает сообщения
// из исходников и падает на каждом непереведённом.
//
// Интеграциям язык не меняется: без cookie `lang` и без
// Accept-Language с английским впереди ответ остаётся русским, каким
// был до перевода.
package i18n

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Lang — язык ответа. Пустое значение — русский, исходный.
type Lang string

const (
	RU Lang = "ru"
	EN Lang = "en"
)

// FromRequest выбирает язык ответа. Первым — cookie `lang`: её ставит
// клиент по выбору в «Оформлении», и она едет с каждым переходом
// браузера, в том числе с возвратом от провайдера входа, где своих
// заголовков клиент не поставит. Дальше — Accept-Language: первый
// из знакомых языков по убыванию веса.
func FromRequest(r *http.Request) Lang {
	if c, err := r.Cookie("lang"); err == nil {
		switch Lang(c.Value) {
		case RU, EN:
			return Lang(c.Value)
		}
	}
	return fromAccept(r.Header.Get("Accept-Language"))
}

func fromAccept(header string) Lang {
	best, bestQ := RU, 0.0
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		base, _, _ := strings.Cut(strings.ToLower(tag), "-")
		var l Lang
		switch base {
		case "ru":
			l = RU
		case "en":
			l = EN
		default:
			continue
		}
		// Строго больше: при равных весах побеждает названный раньше.
		if q > bestQ {
			best, bestQ = l, q
		}
	}
	return best
}

type ctxKey struct{}

// WithLang кладёт язык в контекст — для названий, которые сервер
// заводит сам глубоко в пакетах (колонки новой доски, данные
// песочницы): туда доезжает контекст, а ответ — нет.
func WithLang(ctx context.Context, l Lang) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// Of — язык из контекста; без него русский.
func Of(ctx context.Context) Lang {
	if l, ok := ctx.Value(ctxKey{}).(Lang); ok {
		return l
	}
	return RU
}

// Name — название, которое сервер заводит сам, на языке того,
// для кого заводит.
func Name(ctx context.Context, ru string) string { return Say(Of(ctx), ru) }

// Say переводит сообщение на язык ответа. Незнакомое возвращается
// как есть: русский отказ лучше пустого.
func Say(l Lang, msg string) string {
	if l != EN || msg == "" {
		return msg
	}
	return say(msg, 0)
}

// Составное сообщение («разбор MOVE_CARD: карточка не найдена»)
// переводится по частям: подставленное в шаблон тоже может оказаться
// сообщением. Глубина ограничена — подстановки не вкладываются
// бесконечно, а по кругу гонять строку незачем.
func say(msg string, depth int) string {
	if s, ok := exact()[msg]; ok {
		return s
	}
	if depth > 3 {
		return msg
	}
	for _, p := range patterns() {
		m := p.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		args := make([]any, len(m)-1)
		for i, a := range m[1:] {
			args[i] = say(a, depth+1)
		}
		return fmt.Sprintf(p.en, args...)
	}
	return msg
}

type pattern struct {
	re *regexp.Regexp
	en string
	// fixed — сколько в шаблоне постоянного текста. Первым пробуется
	// самый определённый: «%s: %s» подходит почти ко всему и обязан
	// проигрывать шаблону, который назвал больше.
	fixed int
}

var (
	once     sync.Once
	exactMap map[string]string
	patList  []pattern
)

func build() {
	exactMap = map[string]string{}
	for ru, en := range en {
		if !strings.Contains(strings.ReplaceAll(ru, "%%", ""), "%s") {
			exactMap[strings.ReplaceAll(ru, "%%", "%")] = strings.ReplaceAll(en, "%%", "%")
			continue
		}
		var re strings.Builder
		re.WriteString("^")
		fixed := 0
		for i, piece := range strings.Split(ru, "%s") {
			if i > 0 {
				re.WriteString("(.+?)")
			}
			piece = strings.ReplaceAll(piece, "%%", "%")
			fixed += len(piece)
			re.WriteString(regexp.QuoteMeta(piece))
		}
		re.WriteString("$")
		patList = append(patList, pattern{regexp.MustCompile("(?s)" + re.String()), en, fixed})
	}
	sort.SliceStable(patList, func(i, j int) bool {
		if patList[i].fixed != patList[j].fixed {
			return patList[i].fixed > patList[j].fixed
		}
		return patList[i].en < patList[j].en
	})
}

func exact() map[string]string { once.Do(build); return exactMap }
func patterns() []pattern      { once.Do(build); return patList }
