package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Границу таблиц личности держит код, а не политики, — значит, её надо
// стеречь так же, как политики.
//
// `orgs`, `users`, `sessions` и `memberships` под RLS не попадают
// намеренно (миграция 0002): к ним обращаются до того, как известна
// организация. Про них записано, что границу проверяет код, и до
// 22 августа 2026 стерегло эту запись только внимание: 81 запрос,
// просмотренный глазами. Просмотр был неполон — наблюдение и права
// администратора подразделения принимали человека из чужой организации
// и отдавали наружу его имя и почту.
//
// Отсутствие такой проверки не отказывает, а молчит: строка заводится,
// join к `users` возвращает того, кого ему назвали. Увидеть это можно
// только прогоном, поэтому у каждого действия над человеком есть своя
// проверка посторонним, а здесь стоит сторож над самим их наличием.
//
// Опознаются действия по подписи: метод, берущий и того, кто действует
// (`actorID`), и того, над кем действуют (`userID`), — это и есть место,
// где идентификатор человека приходит снаружи. Новый такой метод обязан
// появиться в списке ниже вместе с проверкой, иначе эта падает.
//
// Чего сторож не видит: идентификатор, приехавший внутри операции доски
// (`ASSIGN_CARD`, упоминания в обсуждении) — там он лежит в теле, а не
// в подписи. Эти места проверяются `board.TestStrangerFromAnotherOrgIsRefused`,
// но добавить туда четвёртое так же молча по-прежнему можно.
var actionsOnAPerson = map[string]string{
	"board.AddMember":    "TestStrangerFromAnotherOrgIsRefused",
	"board.RemoveMember": "TestStrangerFromAnotherOrgIsRefused",
	"org.SetRole":        "TestStrangerFromAnotherOrgIsRefused",
	"org.Remove":         "TestStrangerFromAnotherOrgIsRefused",
	"org.Erase":          "TestStrangerFromAnotherOrgIsRefused",
	"team.AddMember":     "TestStrangerFromAnotherOrgIsRefused",
	"team.RemoveMember":  "TestStrangerFromAnotherOrgIsRefused",
	"team.Grant":         "TestStrangerFromAnotherOrgIsRefused",
	"team.GrantAdmin":    "TestStrangerFromAnotherOrgIsRefused",
}

func TestEveryActionOnAPersonIsProbedWithAStranger(t *testing.T) {
	found := actionsFoundInCode(t)

	for _, name := range found {
		probe, listed := actionsOnAPerson[name]
		if !listed {
			t.Errorf("%s принимает и actorID, и userID, но проверки посторонним у него нет.\n"+
				"Такой метод берёт идентификатор человека снаружи, а `users` политиками "+
				"не закрыта: без проверки членства он заведёт строку на чужого и отдаст "+
				"наружу его имя и почту. Напишите проверку и внесите метод сюда",
				name)
			continue
		}
		pkg, _, _ := strings.Cut(name, ".")
		if !testExists(t, filepath.Join("..", pkg), probe) {
			t.Errorf("%s ссылается на проверку %s, а её в пакете %s нет: "+
				"либо переименовали, либо удалили", name, probe, pkg)
		}
	}

	for name := range actionsOnAPerson {
		if !contains(found, name) {
			t.Errorf("%s записан здесь, а в коде такого метода нет: "+
				"переименовали — поправьте список, удалили — уберите строку", name)
		}
	}
}

// actionsFoundInCode ищет методы, берущие и того, кто действует,
// и того, над кем действуют. Читается код разбором исходников, а не
// списком рядом: список стал бы вторым источником правды — тем самым,
// от которого проверка и защищает.
func actionsFoundInCode(t *testing.T) []string {
	t.Helper()
	var out []string
	root := ".."
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			t.Fatalf("%s: разбор не удался: %v — проверка перестала видеть код", path, perr)
		}
		pkg := file.Name.Name
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() {
				continue
			}
			var actor, target bool
			for _, param := range fn.Type.Params.List {
				for _, n := range param.Names {
					switch n.Name {
					case "actorID":
						actor = true
					case "userID", "memberID":
						target = true
					}
				}
			}
			if actor && target {
				out = append(out, pkg+"."+fn.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход исходников: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("не нашлось ни одного действия над человеком — проверка ослепла")
	}
	sort.Strings(out)
	return out
}

func testExists(t *testing.T, dir, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("чтение %s: %v", dir, err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("%s: разбор не удался: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Name.Name == name {
				return true
			}
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
