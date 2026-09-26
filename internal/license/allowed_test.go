package license_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Лицензии чужого кода в поставке — разрешённые и настоящие
// (PROMPT-TESTING.md, уровень 8).
//
// THIRD-PARTY.md сверяется с составом сборки (TestСписокЧужогоКодаНеОтстал),
// но колонка «Лицензия» в нём набрана руками. Ввод свободного ПО
// в организацию спрашивает ровно её: что за лицензии едут в поставке
// и нет ли среди них таких, что обязывают раскрыть код заказчика. Здесь
// каждая строка сверяется с настоящей лицензией модуля — текстом LICENSE
// из кэша модулей Go и полем license в package.json пакета npm — и
// проверяется, что она из разрешённого набора. Сеть не нужна: то и другое
// лежит у того, кто собирает, в том числе в закрытом контуре.

// разрешённые — лицензии, не обязывающие раскрывать код того, кто
// поставляет продукт вместе с ними. Копилефт (GPL, LGPL, AGPL, MPL,
// EPL) сюда не входит намеренно: его появление — решение юриста,
// а не строка в списке.
var разрешённые = map[string]bool{
	"MIT": true, "BSD-2-Clause": true, "BSD-3-Clause": true,
	"Apache-2.0": true, "ISC": true, "0BSD": true,
}

func TestЛицензииЧужогоКодаРазрешеныИНастоящие(t *testing.T) {
	строки := 0
	for _, строка := range strings.Split(читать(t, "THIRD-PARTY.md"), "\n") {
		if !strings.HasPrefix(строка, "| `") {
			continue
		}
		поля := strings.Split(строка, "|")
		if len(поля) < 4 {
			continue
		}
		имя := strings.Trim(strings.TrimSpace(поля[1]), "`")
		записано := strings.TrimSpace(поля[3])
		строки++

		if !разрешённые[записано] {
			t.Errorf("%s: лицензия %q не из разрешённого набора — это решение юриста, а не строка в списке", имя, записано)
			continue
		}
		var настоящая string
		if strings.Contains(имя, "/") && !strings.HasPrefix(имя, "@") {
			настоящая = лицензияМодуля(t, имя)
		} else {
			настоящая = лицензияПакета(t, имя)
		}
		if настоящая != "" && настоящая != записано {
			t.Errorf("%s: в THIRD-PARTY.md записано %s, а у самого модуля — %s", имя, записано, настоящая)
		}
	}
	if строки < 10 {
		t.Fatalf("в THIRD-PARTY.md нашлось %d строк — разбор сломан", строки)
	}
}

// лицензияМодуля узнаёт лицензию модуля Go по тексту его LICENSE
// в кэше модулей. Модуля в кэше нет — это провал, а не пропуск:
// сборки без него не бывает.
func лицензияМодуля(t *testing.T, модуль string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", модуль)
	cmd.Dir = корень(t)
	out, err := cmd.Output()
	dir := strings.TrimSpace(string(out))
	if err != nil || dir == "" {
		t.Errorf("%s: нет в кэше модулей (go mod download): %v", модуль, err)
		return ""
	}
	for _, имя := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
		raw, err := os.ReadFile(filepath.Join(dir, имя))
		if err != nil {
			continue
		}
		if вид := видЛицензии(string(raw)); вид != "" {
			return вид
		}
		t.Errorf("%s: текст %s не узнан — допишите признак в видЛицензии или проверьте руками", модуль, имя)
		return ""
	}
	t.Errorf("%s: в модуле нет файла лицензии", модуль)
	return ""
}

// лицензияПакета берёт лицензию пакета npm из его package.json.
// Дерева node_modules нет — пакеты не установлены, и сверять не с чем:
// это видно отказом, а не молчанием.
func лицензияПакета(t *testing.T, пакет string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(корень(t), "web", "node_modules", пакет, "package.json"))
	if err != nil {
		t.Errorf("%s: нет web/node_modules — npm ci, затем снова", пакет)
		return ""
	}
	var pkg struct {
		License string `json:"license"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Errorf("%s: package.json не разобрать: %v", пакет, err)
		return ""
	}
	return pkg.License
}

// видЛицензии узнаёт лицензию по её обязательным фразам. Признаки взяты
// из канонических текстов: у каждой лицензии своя фраза, которую
// правообладатель не волен менять.
func видЛицензии(текст string) string {
	t := strings.Join(strings.Fields(текст), " ")
	switch {
	case strings.Contains(t, "Apache License") && strings.Contains(t, "Version 2.0"):
		return "Apache-2.0"
	case strings.Contains(t, "Permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(t, "Redistribution and use in source and binary forms"):
		if strings.Contains(t, "Neither the name") || strings.Contains(t, "may be used to endorse or promote") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	case strings.Contains(t, "Permission to use, copy, modify, and/or distribute this software"):
		return "ISC"
	}
	return ""
}
