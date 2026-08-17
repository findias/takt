package httpapi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/konkov/agile/internal/board"
)

// Описание не должно расходиться с кодом молча.
//
// Ради этого файл описания и заведён, и он всё равно разошёлся: в нём
// оказалось 18 типов операций из 22 и 8 кодов ошибок из 11 — причём
// два недостающих кода были описаны словами в том же файле двадцатью
// строками выше. Прежняя проверка смотрела наличие трёх путей и такого
// дрейфа не видела вовсе.
//
// Здесь сверяются перечисления: типы операций — с диспетчером,
// коды ошибок — с литералами writeCoded, имена событий — со списком,
// из которого их и рассылают. Читается код разбором исходников, а не
// вызовами: список, собранный вызовами, пришлось бы где-то держать,
// и он стал бы вторым источником правды — тем самым, от которого
// проверка и защищает.
//
// Проверка обязана уметь падать и на своей слепоте: везде, где довод
// вызова оказывается не строкой, она останавливается с объяснением,
// а не молча считает, что там ничего нет.

func TestContractEnumerationsMatchCode(t *testing.T) {
	doc := contractDoc(t)

	sameSet(t, "типы операций", "описании",
		dispatchedOperations(t),
		enumAt(t, doc, "components", "schemas", "Operation", "properties", "type", "enum"))

	sameSet(t, "коды ошибок", "описании",
		codesWritten(t),
		enumAt(t, doc, "components", "schemas", "Error", "properties", "code", "enum"))

	// Имена событий: «card.» приписывается при постановке в очередь
	// доставки (logEvent), поэтому приписывается и здесь.
	names := make([]string, 0, len(board.EventKinds))
	for _, kind := range board.EventKinds {
		names = append(names, "card."+kind)
	}
	sameSet(t, "имена событий", "описании",
		names,
		enumAt(t, doc, "components", "schemas", "EventName", "enum"))
}

// Список событий обещан наружу, значит обязан быть полным: событие,
// которое рассылается, но не названо, — это подписка, которую никто
// не заведёт.
func TestEventKindsCoverEverythingLogged(t *testing.T) {
	sameSet(t, "виды событий", "списке EventKinds", loggedEventKinds(t), board.EventKinds)
}

// --- чтение описания ---

func contractDoc(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(openapiDocument, &doc); err != nil {
		t.Fatalf("описание не разбирается: %v", err)
	}
	return doc
}

func enumAt(t *testing.T, doc map[string]any, path ...string) []string {
	t.Helper()
	where := strings.Join(path, ".")
	var node any = doc
	for _, step := range path {
		m, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("в описании нет %s", where)
		}
		node = m[step]
	}
	raw, ok := node.([]any)
	if !ok {
		t.Fatalf("в описании нет перечисления %s", where)
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("в перечислении %s не строка: %v", where, value)
		}
		out = append(out, text)
	}
	return out
}

// sameSet называет расхождение в обе стороны: «в коде есть, а там нет»
// чинится дописыванием, обратное — вычёркиванием, и путать их дорого.
func sameSet(t *testing.T, what, where string, code, listed []string) {
	t.Helper()
	var missing, extra []string
	for _, value := range code {
		if !slices.Contains(listed, value) {
			missing = append(missing, value)
		}
	}
	for _, value := range listed {
		if !slices.Contains(code, value) {
			extra = append(extra, value)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s: в коде есть, в %s нет: %s", what, where, strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("%s: в %s есть, в коде нет: %s", what, where, strings.Join(extra, ", "))
	}
}

// --- чтение кода ---

func parseGo(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("%s не разбирается: %v", path, err)
	}
	return file
}

// goFiles — исходники пакета без проверок: проверка, читающая проверки,
// нашла бы придуманные в них коды и события.
func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, path := range all {
		if !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
	}
	if len(out) == 0 {
		t.Fatalf("в %s не нашлось исходников — проверка ослепла", dir)
	}
	return out
}

func literal(t *testing.T, where string, node ast.Expr) string {
	t.Helper()
	lit, ok := node.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("%s: ожидалась строка, а стоит выражение — "+
			"проверка перестала видеть код, почините её вместе с правкой", where)
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	return value
}

// dispatchedOperations — то, что доска на самом деле умеет: ветки
// switch в диспетчере. Операции, дописанной мимо него, не существует.
func dispatchedOperations(t *testing.T) []string {
	t.Helper()
	file := parseGo(t, "../board/ops.go")
	out := []string{}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "dispatch" {
			return true
		}
		found = true
		ast.Inspect(fn, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, item := range clause.List {
				out = append(out, literal(t, "диспетчер операций", item))
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatal("в internal/board/ops.go не нашлось dispatch — проверка ослепла")
	}
	return out
}

// codesWritten — все коды, которые вообще могут доехать до клиента:
// названные явно и умолчания по коду состояния.
func codesWritten(t *testing.T) []string {
	t.Helper()
	out := []string{}
	for _, path := range goFiles(t, ".") {
		ast.Inspect(parseGo(t, path), func(node ast.Node) bool {
			if fn, ok := node.(*ast.FuncDecl); ok && fn.Name.Name == "codeFor" {
				ast.Inspect(fn, func(node ast.Node) bool {
					ret, ok := node.(*ast.ReturnStmt)
					if !ok {
						return true
					}
					for _, value := range ret.Results {
						out = append(out, literal(t, "codeFor", value))
					}
					return true
				})
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || name.Name != "writeCoded" || len(call.Args) < 3 {
				return true
			}
			// Единственный нелитеральный довод, который здесь разрешён, —
			// codeFor(status) внутри writeError: его значения собраны
			// выше. Всё прочее означает, что код собирается на лету,
			// и тогда описать его нечем.
			if inner, ok := call.Args[2].(*ast.CallExpr); ok {
				if fn, ok := inner.Fun.(*ast.Ident); ok && fn.Name == "codeFor" {
					return true
				}
			}
			out = append(out, literal(t, path+": writeCoded", call.Args[2]))
			return true
		})
	}
	return out
}

// loggedEventKinds — виды, с которыми на самом деле зовут logEvent.
// Вид приходит либо строкой, либо переменной, которой в той же функции
// присвоены строки; третьего вида записи в пакете нет, и появись он —
// проверка остановится, а не промолчит.
func loggedEventKinds(t *testing.T) []string {
	t.Helper()
	const kindArg = 6 // ctx, tx, orgID, boardID, cardID, actorID, kind
	out := []string{}
	for _, path := range goFiles(t, "../board") {
		ast.Inspect(parseGo(t, path), func(node ast.Node) bool {
			fn, ok := node.(*ast.FuncDecl)
			if !ok || fn.Name.Name == "logEvent" {
				return true
			}
			ast.Inspect(fn, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := call.Fun.(*ast.Ident)
				if !ok || name.Name != "logEvent" || len(call.Args) <= kindArg {
					return true
				}
				where := path + ": logEvent"
				if ident, ok := call.Args[kindArg].(*ast.Ident); ok {
					out = append(out, assignedStrings(t, where, fn, ident.Name)...)
					return true
				}
				out = append(out, literal(t, where, call.Args[kindArg]))
				return true
			})
			return false
		})
	}
	return out
}

// assignedStrings собирает всё, что присваивается переменной внутри
// функции. Пусто здесь — не «событий нет», а «мы их не увидели»,
// и это остановка.
func assignedStrings(t *testing.T, where string, fn *ast.FuncDecl, name string) []string {
	t.Helper()
	out := []string{}
	ast.Inspect(fn, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i, target := range assign.Lhs {
			ident, ok := target.(*ast.Ident)
			if !ok || ident.Name != name {
				continue
			}
			out = append(out, literal(t, where+": "+name, assign.Rhs[i]))
		}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("%s: не видно, чем бывает %s — проверка ослепла", where, name)
	}
	return out
}
