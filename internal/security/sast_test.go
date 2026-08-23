package security_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Свои проверки безопасности — те, которых чужие инструменты не делают,
// потому что не знают этого продукта.
//
// Готовые своё дело делают и стоят в `make security`: govulncheck
// сверяет зависимости и стандартную библиотеку с базой уязвимостей
// и говорит, достижим ли вызов; gosec ищет обычные ошибки в коде на Go;
// `npm audit` — то же для клиента. Здесь другое: инварианты этого
// продукта, нарушение которых для стороннего инструмента выглядит
// обычным кодом.
//
// Все три — про то, чем этот продукт ломается по-настоящему: изоляция
// организаций держится политиками базы (а значит, склеенный запрос
// опаснее обычного), клиент никогда не вставляет сырую разметку,
// и секрет, попавший в репозиторий, оттуда уже не убрать.

func корень(t *testing.T) string {
	t.Helper()
	_, файл, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не найти собственный путь")
	}
	return filepath.Join(filepath.Dir(файл), "..", "..")
}

// файлы обходит исходники проекта, пропуская чужое и собранное.
func файлы(t *testing.T, расширения ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	пропустить := []string{"node_modules", "/dist", "/.git", "/bin", "/test-results", "/screenshots"}
	err := filepath.Walk(корень(t), func(путь string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			for _, чужое := range пропустить {
				if strings.Contains(путь, чужое) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		подходит := false
		for _, р := range расширения {
			if strings.HasSuffix(путь, р) {
				подходит = true
			}
		}
		if !подходит {
			return nil
		}
		raw, err := os.ReadFile(путь)
		if err != nil {
			return err
		}
		относительный, _ := filepath.Rel(корень(t), путь)
		out[относительный] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("файлов %v не найдено — проверка ничего не проверяет", расширения)
	}
	return out
}

// Запрос собирается параметрами, а не склейкой с чем попало.
//
// Изоляция организаций держится политиками базы, и подстановка,
// собранная строкой, обходит не только типы, но и рассуждение о том,
// чьи это данные.
//
// Склейка с константой при этом законна и в коде есть: список колонок
// (`cardFields`) вынесен в константу, чтобы не переписывать его
// в двенадцати запросах. Отличить одно от другого регулярным выражением
// нельзя — отсюда разбор кода: всё, что приклеено к запросу, обязано
// быть литералом или константой пакета. Первая редакция проверки
// этого не делала и выдала двенадцать ложных срабатываний на константах
// — то есть ровно тот шум, из-за которого проверки безопасности
// перестают читать.
func TestSQLIsBuiltFromParametersNotStrings(t *testing.T) {
	fset := token.NewFileSet()
	константы := map[string]bool{}
	файлыКода := map[string]*ast.File{}
	файлыСтроками := map[string][]string{}

	for путь := range файлы(t, ".go") {
		полный := filepath.Join(корень(t), путь)
		дерево, err := parser.ParseFile(fset, полный, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", путь, err)
		}
		файлыКода[путь] = дерево
		файлыСтроками[путь] = strings.Split(файлы(t, ".go")[путь], "\n")
		// Константы пакета собираются со всех файлов: запрос в одном
		// файле склеивается с константой из другого.
		ast.Inspect(дерево, func(n ast.Node) bool {
			decl, ok := n.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}
			for _, spec := range decl.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for _, имя := range v.Names {
						константы[имя.Name] = true
					}
				}
			}
			return true
		})
	}

	for путь, дерево := range файлыКода {
		// Проверки сами собирают запросы — по именам таблиц, ролей
		// и политик, — и делают это над своими же строками, а не над
		// чужим вводом: пользовательского ввода в них нет вовсе.
		if strings.HasSuffix(путь, "_test.go") {
			continue
		}
		ast.Inspect(дерево, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Exec", "Query", "QueryRow":
			default:
				return true
			}
			// Второй довод — сам запрос.
			склейка, ok := call.Args[1].(*ast.BinaryExpr)
			if !ok || склейка.Op != token.ADD {
				return true
			}
			for _, часть := range части(склейка) {
				if своё(часть, константы) {
					continue
				}
				// Осознанное исключение объявляется вслух и рядом:
				// строка «// #sql-склейка: <почему безопасно>» над
				// запросом. Так можно — но так видно в ревью, а молча
				// нельзя.
				строка := fset.Position(часть.Pos()).Line
				if объявлено(файлыСтроками[путь], строка) {
					continue
				}
				t.Errorf("%s:%d: к запросу приклеено не своё — "+
					"подстановка идёт параметром ($1), иначе обходятся и типы, "+
					"и рассуждение о том, чьи это данные. Если приклеенное всё-таки своё, "+
					"скажите это вслух: «// #sql-склейка: <почему>» над запросом",
					путь, строка)
			}
			return true
		})
	}
}

// своё — собрано ли выражение целиком из нашего текста: литералов
// и констант проекта. Вызов функции прозрачен: `strings.ReplaceAll`
// над константой даёт то же самое, что константа, — а вот над доводом
// функции уже нет, и такой довод здесь и ловится.
//
// Константы узнаются по имени, включая обращение через пакет
// (`realtime.Channel`): точный ответ дал бы разбор типов, но он тянет
// сборку всего дерева зависимостей ради одной проверки. Обмануть эту
// эвристику можно, лишь назвав переменную так же, как объявленную
// где-то константу, — и это будет видно в ревью.
func своё(выражение ast.Expr, константы map[string]bool) bool {
	switch v := выражение.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return константы[v.Name]
	case *ast.SelectorExpr:
		return константы[v.Sel.Name]
	case *ast.CallExpr:
		for _, довод := range v.Args {
			if !своё(довод, константы) {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		return своё(v.X, константы) && своё(v.Y, константы)
	case *ast.ParenExpr:
		return своё(v.X, константы)
	default:
		return false
	}
}

// объявлено — есть ли рядом со строкой запроса объяснение склейки.
// Ищем в пятнадцати строках выше: запрос многострочный, и маркер
// ставится перед ним, а не посреди SQL.
func объявлено(строки []string, где int) bool {
	от := где - 15
	if от < 0 {
		от = 0
	}
	if где > len(строки) {
		где = len(строки)
	}
	for _, строка := range строки[от:где] {
		if i := strings.Index(строка, "#sql-склейка:"); i >= 0 {
			// Маркер без объяснения — тот же молчаливый пропуск,
			// только с ключевым словом.
			return strings.TrimSpace(строка[i+len("#sql-склейка:"):]) != ""
		}
	}
	return false
}

// части разбирает цепочку «a + b + c» в список слагаемых.
func части(выражение ast.Expr) []ast.Expr {
	bin, ok := выражение.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return []ast.Expr{выражение}
	}
	return append(части(bin.X), части(bin.Y)...)
}

// Клиент не вставляет сырую разметку.
//
// Названия карточек, имена людей и тексты обсуждений приходят от других
// людей той же организации. React экранирует их сам — ровно до первого
// `dangerouslySetInnerHTML`, и появляется он обычно ради жирного шрифта
// в подсказке.
func TestClientNeverInsertsRawHTML(t *testing.T) {
	опасное := regexp.MustCompile(`dangerouslySetInnerHTML|\.innerHTML\s*=|insertAdjacentHTML|document\.write`)
	for путь, текст := range файлы(t, ".ts", ".tsx") {
		if strings.Contains(путь, ".test.") || strings.Contains(путь, "e2e/") {
			continue
		}
		if m := опасное.FindString(текст); m != "" {
			строка := strings.Count(текст[:strings.Index(текст, m)], "\n") + 1
			t.Errorf("%s:%d: %s — разметка от другого человека вставляется как есть", путь, строка, m)
		}
	}
}

// Секрета в репозитории нет.
//
// Секрет, однажды попавший в историю, из неё уже не убрать: правка
// удаляет его из файла, а не из прошлого. Проверка грубая — длинные
// строки в присваиваниях с говорящим именем, — и это осознанно: тонкая
// пропускает, а грубая заставляет объяснить каждый случай.
func TestNoSecretsInTheRepository(t *testing.T) {
	присваивание := regexp.MustCompile(
		`(?i)\b(password|passwd|secret|token|api[_-]?key|client[_-]?secret)\b\s*[:=]{1,2}\s*"([^"]{12,})"`)
	// Пароль стенда — не секрет: он объявлен в демонстрационных данных,
	// напечатан в README и существует ровно для того, чтобы войти
	// на свой же стенд.
	известные := map[string]string{
		"parol12345":        "пароль демонстрационного стенда",
		"novyy-parol-12345": "пароль в проверке смены пароля",
	}
	for путь, текст := range файлы(t, ".go", ".ts", ".tsx", ".yaml", ".yml", ".sql") {
		for _, m := range присваивание.FindAllStringSubmatch(текст, -1) {
			значение := m[2]
			if _, известно := известные[значение]; известно {
				continue
			}
			// Подстановки и ссылки на секреты кластера — не секреты.
			if strings.Contains(значение, "${") || strings.Contains(значение, "{{") ||
				strings.Contains(значение, "$(") || strings.HasPrefix(значение, "postgres://") {
				continue
			}
			// Русские слова секретом не бывают: так выглядит начало
			// склейки в проверках («секретный-хеш-» + случайное).
			if кириллица.MatchString(значение) {
				continue
			}
			строка := strings.Count(текст[:strings.Index(текст, m[0])], "\n") + 1
			t.Errorf("%s:%d: похоже на секрет в коде (%s): из истории его уже не убрать",
				путь, строка, m[1])
		}
	}
}

var кириллица = regexp.MustCompile(`[а-яА-ЯёЁ]`)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
