package requirements_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Требования сверяются с тем, чем они закреплены.
//
// Документ, который ссылается на несуществующую проверку, хуже
// отсутствующего: он утверждает, что обещание держится, а держаться
// ему не на чем. Ровно та болезнь, ради которой требования и написаны, —
// обещание, существующее только в тексте.
//
// Проверяется три вещи, и все три ломаются молча: номер требования
// повторился (значит, на него ссылаются двое и правят одного),
// графа «чем закреплено» пуста (требование держится на памяти),
// названный файл или каталог не существует (проверку переименовали
// или убрали, а строка осталась).

func документ(t *testing.T) string {
	t.Helper()
	_, файл, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не найти собственный путь")
	}
	корень := filepath.Join(filepath.Dir(файл), "..", "..")
	raw, err := os.ReadFile(filepath.Join(корень, "REQUIREMENTS.md"))
	if err != nil {
		t.Fatalf("требований нет вовсе: %v", err)
	}
	return string(raw)
}

func корень(t *testing.T) string {
	t.Helper()
	_, файл, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(файл), "..", "..")
}

// строкаТаблицы — «| Т1 | Требование | Чем закреплено |».
var строкаТаблицы = regexp.MustCompile(`^\|\s*([ТПУСВ]\d+)\s*\|([^|]*)\|(.*)\|\s*$`)

// путь — то, что выглядит файлом или каталогом проекта: `internal/...`,
// `web/e2e/...`, `Dockerfile`. Берём из обратных кавычек: в тексте
// встречаются и слова с точками, и они не пути.
var путь = regexp.MustCompile("`([A-Za-z0-9_./*-]+)`")

type требование struct {
	номер  string
	текст  string
	чем    string
	строка int
}

func требования(t *testing.T) []требование {
	t.Helper()
	out := []требование{}
	for i, строка := range strings.Split(документ(t), "\n") {
		m := строкаТаблицы.FindStringSubmatch(строка)
		if m == nil {
			continue
		}
		out = append(out, требование{
			номер:  strings.TrimSpace(m[1]),
			текст:  strings.TrimSpace(m[2]),
			чем:    strings.TrimSpace(m[3]),
			строка: i + 1,
		})
	}
	if len(out) == 0 {
		t.Fatal("в требованиях нет ни одной строки таблицы — проверка ничего не проверяет")
	}
	return out
}

func TestRequirementNumbersAreUnique(t *testing.T) {
	занято := map[string]int{}
	for _, тр := range требования(t) {
		if прежняя, есть := занято[тр.номер]; есть {
			t.Errorf("номер %s занят дважды: строки %d и %d — ссылка на него означает разное",
				тр.номер, прежняя, тр.строка)
		}
		занято[тр.номер] = тр.строка
	}
}

func TestEveryRequirementSaysWhatHoldsIt(t *testing.T) {
	for _, тр := range требования(t) {
		if тр.чем == "" {
			t.Errorf("%s (%s): графа «чем закреплено» пуста — требование держится на памяти",
				тр.номер, тр.текст)
		}
	}
}

func TestEveryNamedCheckExists(t *testing.T) {
	база := корень(t)
	for _, тр := range требования(t) {
		// Незакреплённое названо вслух и объяснено — это законно,
		// в отличие от пустой графы.
		if strings.HasPrefix(тр.чем, "не закреплено") {
			if !strings.Contains(тр.чем, ":") {
				t.Errorf("%s: сказано «не закреплено», но не сказано почему", тр.номер)
			}
			continue
		}
		названо := путь.FindAllStringSubmatch(тр.чем, -1)
		if len(названо) == 0 {
			t.Errorf("%s: в графе «чем закреплено» нет ни одного файла: %q", тр.номер, тр.чем)
			continue
		}
		for _, найдено := range названо {
			имя := найдено[1]
			// Звёздочка — семейство файлов (contract_*_test.go): проверяем
			// каталог, в котором они лежат.
			проверяемое := имя
			if strings.Contains(имя, "*") {
				проверяемое = filepath.Dir(имя)
			}
			if _, err := os.Stat(filepath.Join(база, проверяемое)); err != nil {
				t.Errorf("%s ссылается на %s, которого нет: проверку переименовали, а строка осталась",
					тр.номер, имя)
			}
		}
	}
}
