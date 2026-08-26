package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// Выпуск обещает архитектуры в двух местах сразу, и места эти
// не связаны ничем.
//
// Образы собирает матрица в `release.yml` (`platforms:`), архивы —
// цель `tarball` в Makefile под заданным GOARCH, а список оснований
// и их платформ объявлен в Makefile ещё раз (`BASE_*`, `PLATFORMS_*`),
// потому что собирать образы умеет и `make image-push`. Добавить
// архитектуру в одном месте и забыть про другое — правка на одну
// строку, а последствие у неё разное для того, кто ставит из образа,
// и для того, кто ставит из архива: «поставка под arm64» начинает
// означать две разные вещи.
//
// Проверка сверяет эти места между собой. Она не знает, какие
// архитектуры правильные, — она знает, что ответ должен быть один.

var (
	основаниеMakefile = regexp.MustCompile(`(?m)^BASE_(\w+)\s*\?=\s*(\S+)`)
	платформыMakefile = regexp.MustCompile(`(?m)^PLATFORMS_(\w+)\s*\?=\s*(\S+)`)
	списокОснований   = regexp.MustCompile(`(?m)^BASES\s*\?=\s*(.+)$`)
	архитектураАрхива = regexp.MustCompile(`GOARCH=(\w+)\s+make\s+tarball`)
)

func makefile(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(каталог(t), "..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// пары собирает `ИМЯ_основания -> значение` в карту: оба списка
// в Makefile устроены одинаково.
func пары(выражение *regexp.Regexp, текст string) map[string]string {
	найдено := map[string]string{}
	for _, m := range выражение.FindAllStringSubmatch(текст, -1) {
		найдено[m[1]] = m[2]
	}
	return найдено
}

// матрицаОбразов достаёт `strategy.matrix.include` работы, собирающей
// образы. Молчания здесь быть не должно: работа, которой нет,
// означает выпуск без образов, и узнать об этом лучше от проверки.
func матрицаОбразов(t *testing.T) []map[string]any {
	t.Helper()
	дерево, есть := процессы(t)["release.yml"]
	if !есть {
		t.Fatal("release.yml пропал — выпуску нечем собирать образы")
	}
	работы, ok := дерево["jobs"].(map[string]any)
	if !ok {
		t.Fatal("release.yml: нет ни одной работы")
	}
	образы, ok := работы["images"].(map[string]any)
	if !ok {
		t.Fatal("release.yml: нет работы images — образы никто не соберёт")
	}
	стратегия, ok := образы["strategy"].(map[string]any)
	if !ok {
		t.Fatal("release.yml, работа images: нет strategy.matrix")
	}
	матрица, ok := стратегия["matrix"].(map[string]any)
	if !ok {
		t.Fatal("release.yml, работа images: нет matrix")
	}
	включено, ok := матрица["include"].([]any)
	if !ok || len(включено) == 0 {
		t.Fatal("release.yml, работа images: матрица пуста — " +
			"выпуск не соберёт ни одного образа")
	}
	строки := make([]map[string]any, 0, len(включено))
	for _, э := range включено {
		if строка, ok := э.(map[string]any); ok {
			строки = append(строки, строка)
		}
	}
	return строки
}

func TestОснованияВыпускаОбъявленыВMakefile(t *testing.T) {
	текст := makefile(t)
	основания := пары(основаниеMakefile, текст)
	платформы := пары(платформыMakefile, текст)

	список := map[string]bool{}
	if m := списокОснований.FindStringSubmatch(текст); m != nil {
		for _, имя := range strings.Fields(m[1]) {
			список[имя] = true
		}
	}
	if len(список) == 0 {
		t.Fatal("в Makefile не нашлось BASES — сверять не с чем")
	}

	for _, строка := range матрицаОбразов(t) {
		имя, _ := строка["base"].(string)
		откуда, _ := строка["from"].(string)
		где := fmt.Sprintf("release.yml, основание %s", имя)

		объявлено, есть := основания[имя]
		if !есть {
			t.Errorf("%s: в Makefile нет BASE_%s. Выпуск публикует "+
				"основание, которого не собрать командой "+
				"`make image BASE=%s` — проверить его перед выпуском нечем",
				где, имя, имя)
			continue
		}
		if объявлено != откуда {
			t.Errorf("%s: выпуск берёт %s, а `make image BASE=%s` — %s. "+
				"Проверяется одно, уезжает другое",
				где, откуда, имя, объявлено)
		}
		if !список[имя] {
			t.Errorf("%s: основания нет в BASES, значит `make images` "+
				"его не соберёт и локально его никто не увидит", где)
		}
		if ждём, есть := платформы[имя]; есть {
			if с, _ := строка["platforms"].(string); с != ждём {
				t.Errorf("%s: выпуск собирает %s, а PLATFORMS_%s обещает %s",
					где, с, имя, ждём)
			}
		} else {
			t.Errorf("%s: в Makefile нет PLATFORMS_%s — `make image-push` "+
				"соберёт одну свою архитектуру там, где выпуск собирает "+
				"несколько, и разойдутся они молча", где, имя)
		}
	}
}

// Архивы и образы обязаны предлагать одни и те же архитектуры.
//
// Иначе выпуск отвечает на «есть ли arm64» по-разному в зависимости
// от того, как ставят: из реестра или из архива под systemd. Обе
// поставки описаны в одном руководстве и одной таблицей.
func TestАрхивыСобираютсяПодТеЖеАрхитектуры(t *testing.T) {
	уОбразов := map[string]bool{}
	for _, строка := range матрицаОбразов(t) {
		платформы, _ := строка["platforms"].(string)
		for _, п := range strings.Split(платформы, ",") {
			п = strings.TrimSpace(п)
			ос, арх, найдено := strings.Cut(п, "/")
			if !найдено {
				t.Errorf("release.yml: платформа %q не вида ос/архитектура", п)
				continue
			}
			// Архивы бывают только под linux: systemd, tar.gz.
			// Появится другая ос — сверять придётся иначе, и лучше
			// узнать об этом здесь.
			if ос != "linux" {
				t.Errorf("release.yml: платформа %q не под linux, "+
					"а архивы собираются только под него", п)
				continue
			}
			уОбразов[арх] = true
		}
	}

	уАрхивов := map[string]bool{}
	for _, m := range архитектураАрхива.FindAllStringSubmatch(шагиВыпуска(t), -1) {
		уАрхивов[m[1]] = true
	}
	if len(уАрхивов) == 0 {
		t.Fatal("release.yml: не нашлось ни одного `GOARCH=… make tarball` — " +
			"архитектура архива задаётся именно так, и сверять её иначе нечем")
	}

	if строкой(уОбразов) != строкой(уАрхивов) {
		t.Errorf("образы едут под %s, а архивы — под %s.\n"+
			"Архитектуры поставки обязаны совпадать: иначе «поддерживаем "+
			"arm64» значит разное для того, кто ставит из реестра, "+
			"и для того, кто ставит из архива",
			строкой(уОбразов), строкой(уАрхивов))
	}
}

// шагиВыпуска — все команды `run` из release.yml одной строкой.
// Разбирать именно шаг с архивами по имени было бы хрупко: имя
// человеческое и меняется.
func шагиВыпуска(t *testing.T) string {
	t.Helper()
	return командыПроцесса(процессы(t)["release.yml"])
}

func строкой(набор map[string]bool) string {
	имена := make([]string, 0, len(набор))
	for имя := range набор {
		имена = append(имена, имя)
	}
	sort.Strings(имена)
	return strings.Join(имена, ", ")
}

// Чарт не носит своей версии: её проставляет упаковка.
//
// Номера чарта и приложения принято разводить, и довод у этого есть —
// правку шаблона можно выкатить, не притворяясь, что сменилось
// приложение. Но чарт у нас отдельно не выпускается: тег один
// на образ, архив и чарт. Порознь номера означали лишь то, что один
// из двух молча стоит, — и он стоял: выпуск проставлял appVersion,
// `version` осталась в Chart.yaml на 0.2.0, и релиз v0.2.1 положил
// `takt-0.2.0.tgz`. Второй файл с тем же именем и тем же номером,
// что и у прошлого выпуска: для `helm repo` это столкновение,
// для установки из файла — вопрос «а этот чарт от какой версии».
//
// Отсюда два требования, и оба проверяются: в Chart.yaml стоит
// заглушка, а всякий, кто пакует чарт, проставляет обе версии сам.

const заглушкаЧарта = "0.0.0"

func TestВерсииЧартаВРепозиторииОстаютсяЗаглушкой(t *testing.T) {
	путь := filepath.Join(каталог(t), "..", "..", "deploy", "helm", "takt", "Chart.yaml")
	raw, err := os.ReadFile(путь)
	if err != nil {
		t.Fatal(err)
	}
	var чарт struct {
		Version    string `yaml:"version"`
		AppVersion string `yaml:"appVersion"`
	}
	if err := yaml.Unmarshal(raw, &чарт); err != nil {
		t.Fatalf("Chart.yaml не разбирается: %v", err)
	}
	for имя, что := range map[string]string{"version": чарт.Version, "appVersion": чарт.AppVersion} {
		if что != заглушкаЧарта {
			t.Errorf("Chart.yaml: %s = %q. Настоящее число здесь отстаёт "+
				"молча — его проставляет упаковка, а тут стоит %s: "+
				"признак сборки мимо выпуска, как «версия не задана» "+
				"у бинарника", имя, что, заглушкаЧарта)
		}
	}
}

func TestУпаковкаЧартаПроставляетОбеВерсии(t *testing.T) {
	источники := map[string]string{"Makefile": makefile(t)}
	for файл, дерево := range процессы(t) {
		источники[файл] = командыПроцесса(дерево)
	}

	нашлась := false
	for где, текст := range источники {
		for _, строка := range склеенные(текст) {
			if !strings.Contains(строка, "helm package") {
				continue
			}
			нашлась = true
			for _, флаг := range []string{"--version", "--app-version"} {
				if !strings.Contains(строка, флаг) {
					t.Errorf("%s: `helm package` без %s. Непроставленное "+
						"берётся из Chart.yaml, а там заглушка — уедет "+
						"чарт, который не называет ни себя, ни приложение",
						где, флаг)
				}
			}
		}
	}
	if !нашлась {
		t.Fatal("ни одной упаковки чарта не нашлось — проверка молчала бы " +
			"на любом расхождении")
	}
}

// склеенные возвращает команды строками, соединив перенос обратной
// косой чертой: `helm package` с флагами на второй строке — не другая
// команда, а та же самая.
func склеенные(текст string) []string {
	return strings.Split(strings.ReplaceAll(текст, "\\\n", " "), "\n")
}

func командыПроцесса(дерево map[string]any) string {
	работы, _ := дерево["jobs"].(map[string]any)
	var всё strings.Builder
	for _, тело := range работы {
		карта, ok := тело.(map[string]any)
		if !ok {
			continue
		}
		шаги, ok := карта["steps"].([]any)
		if !ok {
			continue
		}
		for _, ш := range шаги {
			шаг, ok := ш.(map[string]any)
			if !ok {
				continue
			}
			if команда, ok := шаг["run"].(string); ok {
				всё.WriteString(команда)
				всё.WriteString("\n")
			}
		}
	}
	return всё.String()
}
