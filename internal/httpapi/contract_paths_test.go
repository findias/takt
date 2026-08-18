package httpapi

import (
	"encoding/json"
	"go/ast"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Проверка дрейфа сверяет перечисления, но не сами адреса: маршрут,
// открытый ключу и не описанный, она не видит — так и осталось
// незамеченным GET /boards/{id}/metrics. Обратное тоже: путь,
// переименованный в коде, остаётся в описании и ведёт в 404.
//
// Кто здесь считается интеграционным, решает код, а не список рядом.
// Обёртка scoped — это и есть обещание ключу: она объявляет, каким
// разрешением маршрут берётся, и ставится только там, где наружу
// открыто нарочно. Остальное — вызовы своего клиента, они меняются
// вместе с ним и описаны быть не должны, иначе описание пообещает
// то, чего никто не обещал.

// Сверху вниз: что открыто ключу — обязано быть описано.
func TestContractDescribesEveryKeyedRoute(t *testing.T) {
	described := describedRoutes(t)
	for _, route := range registeredRoutes(t) {
		if route.scope == "" {
			continue
		}
		want := contractPath(route.path)
		if want == "" {
			t.Errorf("%s %s открыт ключу (%s), но лежит вне /api — описать его нечем",
				route.method, route.path, route.scope)
			continue
		}
		if !slices.Contains(described, route.method+" "+want) {
			t.Errorf("%s %s открыт ключу с разрешением %s, а в описании его нет",
				route.method, route.path, route.scope)
		}
	}
}

// Снизу вверх: что описано — обязано существовать. Метод сверяется
// вместе с путём: описанный POST по адресу, где есть только GET, —
// то же обещание в пустоту, что и выдуманный путь.
func TestContractPathsExistInCode(t *testing.T) {
	registered := registeredRoutes(t)
	for _, route := range describedRoutes(t) {
		method, path, _ := strings.Cut(route, " ")
		found := slices.ContainsFunc(registered, func(r routeDecl) bool {
			return r.method == method && contractPath(r.path) == path
		})
		if !found {
			t.Errorf("в описании есть %s, а маршрута в коде нет: "+
				"либо путь переименовали, либо метод другой", route)
		}
	}
}

// contractPath переводит адрес маршрута в адрес описания: снаружи
// интеграция ходит по /api/v1/…, обёртка versioned срезает версию
// раньше маршрутизатора, а в описании база вынесена в servers.
// Пустая строка — «этого адреса в описании быть не может».
func contractPath(route string) string {
	if !strings.HasPrefix(route, "/api/") {
		return ""
	}
	return strings.TrimPrefix(route, "/api")
}

func describedRoutes(t *testing.T) []string {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(openapiDocument, &doc); err != nil {
		t.Fatalf("описание не разбирается: %v", err)
	}
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete}
	out := []string{}
	for path, item := range doc.Paths {
		for key := range item {
			method := strings.ToUpper(key)
			if slices.Contains(methods, method) {
				out = append(out, method+" "+path)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("в описании не нашлось путей — проверка ослепла")
	}
	sort.Strings(out)
	return out
}

type routeDecl struct {
	method string
	path   string
	// scope — разрешение из обёртки scoped; пусто у маршрутов, которые
	// ключу не открывались.
	scope string
}

// registeredRoutes читает маршруты разбором исходников, а не у самого
// http.ServeMux: список зарегистрированных путей он не отдаёт, а список
// рядом с проверкой стал бы вторым источником правды — тем самым,
// от которого проверка и защищает.
func registeredRoutes(t *testing.T) []routeDecl {
	t.Helper()
	out := []routeDecl{}
	for _, path := range goFiles(t, ".") {
		ast.Inspect(parseGo(t, path), func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fun, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || fun.Sel.Name != "HandleFunc" || len(call.Args) < 2 {
				return true
			}
			route := literal(t, path+": адрес маршрута", call.Args[0])
			method, target, ok := strings.Cut(route, " ")
			if !ok {
				t.Fatalf("%s: у маршрута %q нет метода — проверка перестала "+
					"понимать запись, почините её вместе с правкой", path, route)
			}
			out = append(out, routeDecl{method: method, path: target,
				scope: scopeOf(t, path, call.Args[1])})
			return true
		})
	}
	if len(out) == 0 {
		t.Fatal("не нашлось ни одного маршрута — проверка ослепла")
	}
	return out
}

// scopeOf достаёт разрешение из s.scoped(apiclient.ScopeXxx, …).
// Значение берётся именем константы, а не её содержимым: в тексте
// ошибки полезнее «ScopeBoardsRead», чем «boards:read», — по имени
// маршрут находится поиском.
func scopeOf(t *testing.T, where string, handler ast.Expr) string {
	t.Helper()
	call, ok := handler.(*ast.CallExpr)
	if !ok {
		return ""
	}
	fun, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || fun.Sel.Name != "scoped" {
		return ""
	}
	if len(call.Args) == 0 {
		t.Fatalf("%s: у scoped нет доводов — проверка перестала видеть код", where)
	}
	scope, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("%s: разрешение у scoped задано выражением, а не константой — "+
			"проверка его не прочитает, почините её вместе с правкой", where)
	}
	return scope.Sel.Name
}
