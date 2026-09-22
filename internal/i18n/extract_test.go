package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Сообщения, которые сервер может сказать человеку, собираются из
// исходников, а не перечисляются руками: перечень руками отстаёт
// от кода с первой же новой ошибки, и английский посетитель получает
// русский отказ — ровно то, ради чего перевод и заводился.

// quiet — пакеты, чьи слова человеку в браузере не доезжают: журнал
// сервера, команды администратора, проверки сборки. Их русский
// остаётся русским — это язык тех, кто держит установку.
var quiet = map[string]bool{
	"apiclient": true, "ci": true, "config": true, "demo": true,
	"docs": true, "doctor": true, "i18n": true, "requirements": true,
	"retention": true, "security": true, "store": true,
	// Заметка стенда (stand): кириллица там — метка, которую ищут
	// в сообщениях коммитов, а не слова, сказанные человеку.
	"stand": true, "translation": true, "version": true,
	// Порядок карточек: его отказы — нарушенные инварианты, до человека
	// они доезжают «внутренней ошибкой».
	"rank": true,
	// Разбор ответа провайдера входа: человек видит общее «вход
	// не удался», подробность — только журнал.
	"oidc": true,
	// SCIM говорит с каталогом (Okta, Keycloak), а не с человеком,
	// и языка запроса каталоги не присылают.
	"scim": true,
}

// quietFiles — то же для отдельных файлов говорящего пакета.
var quietFiles = map[string]bool{
	"httpapi/scim.go": true,
	// Причина неудачной доставки хранится в журнале доставок как есть:
	// это запись о прошлом, а не ответ на запрос.
	"webhook/worker.go": true,
}

// notSaid — кириллица, которая не говорится никому: ключи счётчиков.
var notSaid = map[string]bool{
	"вход:%s":        true,
	"демо:песочницы": true,
}

// logged — вызовы, чьи строки уходят в журнал, а не в ответ: имена
// методов журнала и аргумент «что делали» у разборщиков ошибок.
// Номер — аргумент, который пропускается; -1 — пропускаются все.
var logged = map[string]int{
	"Info": -1, "Warn": -1, "Error": -1, "Debug": -1,
	"InfoContext": -1, "WarnContext": -1, "ErrorContext": -1,
	"fail": 1, "failAccess": 1, "failLabel": 1, "failTeam": 1,
	"writeMembershipResult": 2,
	// Текст запроса к базе, служебный комментарий потока и разбор
	// строки — не слова для человека.
	"Exec": -1, "Query": -1, "QueryRow": -1, "Fprint": -1,
	"HasPrefix": -1, "TrimPrefix": -1,
}

// formatted — вызовы, первый строковый аргумент которых — шаблон
// с глаголами %s, %d и прочими, а не готовый текст.
var formatted = map[string]int{
	"Sprintf": 0, "Errorf": 0, "badRequestf": 0, "conflictf": 1,
}

type message struct {
	key string
	pos string
}

var (
	cyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)
	verb     = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*\d*(\.\d+)?[a-zA-Z]`)
)

// goMessages собирает сообщения из Go: всякий кириллический текст
// в пакетах, говорящих с человеком, кроме уходящего в журнал.
// Склейка через «+» и шаблон Sprintf дают один ключ, где непостоянные
// части стали %s.
func goMessages(t *testing.T, root string) []message {
	t.Helper()
	var out []message
	fset := token.NewFileSet()
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if !d.IsDir() || quiet[d.Name()] {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(root, d.Name(), "*.go"))
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") || quietFiles[d.Name()+"/"+filepath.Base(path)] {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			add := func(n ast.Node, key string) {
				if cyrillic.MatchString(key) && !notSaid[key] {
					p := fset.Position(n.Pos())
					out = append(out, message{key, filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line)})
				}
			}
			var visit func(n ast.Node) bool
			visit = func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					name := callName(n)
					if skip, ok := logged[name]; ok {
						for i, a := range n.Args {
							if skip >= 0 && i != skip {
								ast.Inspect(a, visit)
							}
						}
						return false
					}
					if at, ok := formatted[name]; ok && at < len(n.Args) {
						add(n, template(n.Args[at], true))
						for i, a := range n.Args {
							if i != at {
								ast.Inspect(a, visit)
							}
						}
						return false
					}
				case *ast.BinaryExpr:
					if n.Op == token.ADD && hasString(n) {
						add(n, template(n, false))
						return false
					}
				case *ast.BasicLit:
					if n.Kind == token.STRING {
						add(n, template(n, false))
					}
				}
				return true
			}
			ast.Inspect(f, visit)
		}
	}
	return out
}

func callName(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func hasString(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if l, ok := n.(*ast.BasicLit); ok && l.Kind == token.STRING {
			found = true
		}
		return !found
	})
	return found
}

// template приводит выражение к ключу каталога: текст как есть,
// «%» текста удвоен, всё непостоянное — %s. У шаблона Sprintf
// глаголы любые сводятся к %s: перевод подставляет их как строки.
func template(e ast.Expr, format bool) string {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "%s"
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "%s"
		}
		if format {
			return verb.ReplaceAllString(s, "%s")
		}
		return strings.ReplaceAll(s, "%", "%%")
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return template(e.X, format) + template(e.Y, format)
		}
	case *ast.ParenExpr:
		return template(e.X, format)
	}
	return "%s"
}

var (
	raise   = regexp.MustCompile(`(?is)raise\s+exception\s+((?:'(?:[^']|'')*'\s*)+)`)
	sqlText = regexp.MustCompile(`'((?:[^']|'')*)'`)
)

// sqlMessages собирает отказы триггеров: их текст доезжает до человека
// через ошибку базы. Проверки самих миграций («пошла бы вхолостую»)
// видит только тот, кто их накатывает.
func sqlMessages(t *testing.T, dir string) []message {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.sql"))
	var out []message
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range raise.FindAllStringSubmatch(string(src), -1) {
			var b strings.Builder
			for _, part := range sqlText.FindAllStringSubmatch(m[1], -1) {
				b.WriteString(strings.ReplaceAll(part[1], "''", "'"))
			}
			text := b.String()
			if strings.Contains(text, "вхолостую") {
				continue
			}
			// В plpgsql подстановка — одиночный «%», а «%%» — сам знак.
			text = strings.ReplaceAll(text, "%%", "\x00")
			text = strings.ReplaceAll(text, "%", "%s")
			text = strings.ReplaceAll(text, "\x00", "%%")
			out = append(out, message{text, filepath.Base(path)})
		}
	}
	return out
}

// TestEveryMessageHasEnglish — у каждого сообщения человеку есть
// английский. Упала — значит, завели новый отказ: впишите перевод
// в en.go, ключом ровно то, что напечатано.
func TestEveryMessageHasEnglish(t *testing.T) {
	all := append(goMessages(t, ".."), sqlMessages(t, "../../migrations")...)
	if len(all) < 100 {
		t.Fatalf("собрано всего %d сообщений — извлечение ослепло", len(all))
	}
	seen := map[string]bool{}
	for _, m := range all {
		if seen[m.key] {
			continue
		}
		seen[m.key] = true
		if _, ok := en[m.key]; !ok {
			t.Errorf("%s: нет перевода\n\t%q", m.pos, m.key)
		}
	}
}

// fragments — ключи, которых нет в исходниках целиком: их подставляет
// в шаблон код, отрезав начало у другого сообщения.
var fragments = map[string]bool{
	"подразделения «%s»": true, // labels.go: TrimPrefix(labelWhere, "у ")
	"доски «%s»":         true,
	"организации":        true,
}

// TestNoStaleTranslations — перевода без сообщения не бывает. Лишний
// ключ означает, что формулировку в коде поправили, а перевод остался
// у старой: английский посетитель снова видит русский.
func TestNoStaleTranslations(t *testing.T) {
	said := map[string]bool{}
	for _, m := range append(goMessages(t, ".."), sqlMessages(t, "../../migrations")...) {
		said[m.key] = true
	}
	for key := range en {
		if !said[key] && !fragments[key] {
			t.Errorf("перевод есть, сообщения нет: %q", key)
		}
	}
}
