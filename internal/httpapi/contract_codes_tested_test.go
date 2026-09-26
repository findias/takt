package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Каждый код отказа из контракта проверен хоть одним тестом
// (PROMPT-TESTING.md, уровень 3).
//
// contract_drift_test смотрит из кода в описание: код, которого нет
// в описании, — ошибка. Обратного не смотрел никто: код, объявленный
// клиенту, но ни разу не вызванный тестом, может не возникать вовсе
// или возникать с другим статусом — клиент различает случаи по коду,
// а не по тексту (CLAUDE.md), и сломанный код ломает экран молча.
// На 26.09.2026 таких было 19 из 48.
//
// Код считается проверенным, если он написан в кавычках в тесте сервера
// или в тесте клиента. Исключение — только с причиной.

// непроверяемыеКоды — коды, которые тестом не вызвать, и почему.
var непроверяемыеКоды = map[string]string{
	"too_many_requests": "предел частоты вызывается только нагрузочным прогоном (internal/httpapi/load_test.go, тег load, make load), вне make check",
	"report_too_large":  "предел — константа report.MaxRows = 50 000; завести 50 001 карточку в make check — минуты, а поменять предел в тесте нечем. Проход рядом с пределом замеряет internal/report/load_test.go (тег load)",
}

func TestEveryContractCodeIsExercisedByATest(t *testing.T) {
	raw, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if c, ok := v["code"].(map[string]any); ok {
				if list, ok := c["enum"].([]any); ok {
					for _, x := range list {
						codes[x.(string)] = true
					}
				}
			}
			for _, x := range v {
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(spec)
	if len(codes) < 20 {
		t.Fatalf("в контракте нашлось %d кодов — разбор сломан", len(codes))
	}

	var corpus strings.Builder
	for _, pattern := range []string{
		"../*/*_test.go", "../../web/src/**/*.test.ts", "../../web/src/**/*.test.tsx",
		"../../web/src/*/*.test.ts", "../../web/src/*/*/*.test.ts", "../../web/src/*/*/*.test.tsx",
		"../../web/e2e/*.ts",
	} {
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			if strings.HasSuffix(f, "contract_codes_tested_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			corpus.Write(b)
		}
	}
	text := corpus.String()

	var untested []string
	for code := range codes {
		if _, ok := непроверяемыеКоды[code]; ok {
			continue
		}
		if !strings.Contains(text, `"`+code+`"`) && !strings.Contains(text, `'`+code+`'`) {
			untested = append(untested, code)
		}
	}
	sort.Strings(untested)
	if len(untested) > 0 {
		t.Errorf("коды контракта, которых не вызывает ни один тест (%d): %s\n"+
			"вызовите отказ в тесте и сверьте код — или назовите код в непроверяемыеКоды с причиной",
			len(untested), strings.Join(untested, ", "))
	}
	for code := range непроверяемыеКоды {
		if !codes[code] {
			t.Errorf("%s назван непроверяемым, но в контракте его нет — уберите строку", code)
		}
	}
}
