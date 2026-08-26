package license_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Лицензия проверяется тем же способом, что и требования: не «есть файл»,
// а «файл не разошёлся с тем, что мы отдаём».
//
// Открытый репозиторий обещает три вещи, и все три ломаются молча.
// Первая — текст лицензии. Его правят с добрыми намерениями («уберём
// приложение», «впишем год»), и получается лицензия, похожая
// на Apache-2.0, но не она: юристу заказчика такую надо разбирать
// заново, а значит, разбирать её будут годами. Отсюда сверка по хешу,
// а не по вхождению слов.
//
// Вторая — NOTICE. Apache-2.0 требует передавать его вместе
// с продуктом, и требование это не наше, а чужое: в сборку едет
// Pragmatic drag and drop под той же лицензией. Пропавший NOTICE —
// нарушение, которое никто не заметит до первой проверки со стороны.
//
// Третья — список чужого кода. Он врёт ровно так же, как врал бы
// список требований без проверок: зависимость добавили, строку
// в THIRD-PARTY.md написать забыли — и документ утверждает состав
// поставки, которого нет.

func корень(t *testing.T) string {
	t.Helper()
	_, файл, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не найти собственный путь")
	}
	return filepath.Join(filepath.Dir(файл), "..", "..")
}

func читать(t *testing.T, имя string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(корень(t), имя))
	if err != nil {
		t.Fatalf("%s: %v", имя, err)
	}
	return string(raw)
}

// Канонический текст Apache License 2.0. Хеш взят с текста,
// распространяемого самим фондом; он же лежит в тысячах чужих
// репозиториев и потому проверяем со стороны.
const хешApache2 = "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30"

func TestЛицензияДословная(t *testing.T) {
	сумма := sha256.Sum256([]byte(читать(t, "LICENSE")))
	if got := hex.EncodeToString(сумма[:]); got != хешApache2 {
		t.Errorf("LICENSE не совпадает с каноническим текстом Apache-2.0\n"+
			"получено %s, ожидалось %s\n"+
			"текст лицензии не правят — ни года, ни имени: имя владельца "+
			"живёт в NOTICE, а приложение в конце — часть лицензии", got, хешApache2)
	}
}

func TestNoticeНаМесте(t *testing.T) {
	notice := читать(t, "NOTICE")
	for _, обязано := range []string{
		"Takt",
		"Apache License, Version 2.0",
		"THIRD-PARTY.md",
		"Atlassian",
	} {
		if !strings.Contains(notice, обязано) {
			t.Errorf("NOTICE не упоминает %q", обязано)
		}
	}
}

// Список чужого кода сверяется с тем, что линкуется в бинарник.
//
// Именно в бинарник, а не в go.mod: в go.mod есть libopenapi, который
// зовут только проверки контракта. Он не едет заказчику, и обещать
// его в поставке значит обещать лишнее.
func TestСписокЧужогоКодаНеОтстал(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps",
		"-f", "{{if and .Module (not .Standard)}}{{.Module.Path}}{{end}}", "./cmd/takt")
	cmd.Dir = корень(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	список := читать(t, "THIRD-PARTY.md")
	видели := map[string]bool{}
	for _, модуль := range strings.Fields(string(out)) {
		if модуль == "github.com/findias/takt" || видели[модуль] {
			continue
		}
		видели[модуль] = true
		if !strings.Contains(список, "`"+модуль+"`") {
			t.Errorf("%s линкуется в cmd/takt, но в THIRD-PARTY.md его нет", модуль)
		}
	}
	if len(видели) == 0 {
		t.Fatal("go list не вернул ни одного модуля — проверка ничего не проверила")
	}

	// Обратная сторона: строка про модуль, которого в сборке больше нет.
	// Она опаснее пропущенной — ей верят, а сверить её не с чем.
	for _, строка := range strings.Split(список, "\n") {
		if !strings.HasPrefix(строка, "| `") {
			continue
		}
		модуль := strings.Trim(strings.Fields(строка)[1], "`")
		if !strings.Contains(модуль, "/") || strings.HasPrefix(модуль, "@") {
			continue // пакет npm, его сверяет соседняя проверка
		}
		if !видели[модуль] {
			t.Errorf("THIRD-PARTY.md называет %s, а в cmd/takt он больше не линкуется", модуль)
		}
	}
}

// Клиентская сторона сверяется с объявленными зависимостями, а не
// с node_modules: транзитивные пакеты без установленного дерева
// не перечислить, а требовать установленное дерево от проверки,
// которая идёт в закрытом контуре, — значит её выключить.
func TestКлиентскиеЗависимостиНазваны(t *testing.T) {
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(читать(t, "web/package.json")), &pkg); err != nil {
		t.Fatalf("web/package.json: %v", err)
	}
	if len(pkg.Dependencies) == 0 {
		t.Fatal("в web/package.json нет раздела dependencies — проверка ничего не проверила")
	}

	список := читать(t, "THIRD-PARTY.md")
	for имя := range pkg.Dependencies {
		if !strings.Contains(список, "`"+имя+"`") {
			t.Errorf("%s едет в собранном клиенте, но в THIRD-PARTY.md его нет", имя)
		}
	}
}

// Файлы, которые копируются в поставку, обязаны существовать.
//
// Список лежит в Makefile строкой `cp`, и разойтись с ним легко:
// файл переименовали — цель падает у того, кто собирает выпуск,
// а не у того, кто переименовал. Так и вышло с README.en.md, когда
// оригиналом документации стал английский: `make tarball`
// и `make bundle` перестали собираться, и заметить это было нечем
// до первого выпуска.
func TestФайлыПоставкиСуществуют(t *testing.T) {
	makefile := читать(t, "Makefile")
	строка := regexp.MustCompile(`(?m)^\s*cp ((?:[\w.-]+ )+)"\$\(`)
	найдено := строка.FindAllStringSubmatch(makefile, -1)
	if len(найдено) == 0 {
		t.Fatal("в Makefile не нашлось ни одной строки копирования в поставку — " +
			"либо их убрали, либо проверка ищет не то")
	}
	for _, m := range найдено {
		for _, файл := range strings.Fields(m[1]) {
			if _, err := os.Stat(filepath.Join(корень(t), файл)); err != nil {
				t.Errorf("Makefile копирует в поставку %s, которого нет: "+
					"цель сборки архива упадёт у того, кто делает выпуск", файл)
			}
		}
	}
}
