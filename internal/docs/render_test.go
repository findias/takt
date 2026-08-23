package docs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konkov/agile/internal/docs"
)

// Отрисовка в HTML: что именно она обязана делать.
//
// Проверяется не «похоже на markdown», а те четыре вещи, из-за которых
// собранная страница врёт: заголовок теряется, таблица рассыпается,
// ссылка ведёт на исходник вместо соседней страницы, а разметка внутри
// кода разбирается как разметка.

func TestRenderKeepsStructure(t *testing.T) {
	заголовок, тело := docs.Отрисовать("# Заголовок страницы\n\n" +
		"Абзац с **жирным** и `кодом`.\n\n" +
		"## Раздел\n\n" +
		"- первый пункт\n- второй пункт\n\n" +
		"| № | Что | Чем |\n| --- | --- | --- |\n| Т1 | Требование | `файл.go` |\n")
	if заголовок != "Заголовок страницы" {
		t.Errorf("заголовок страницы %q — по нему называется вкладка браузера", заголовок)
	}
	для := map[string]string{
		"<h1>":           "заголовок первого уровня",
		"<h2>":           "заголовок второго уровня",
		"<strong>жирным": "жирный",
		"<code>кодом":    "код в строке",
		"<ul>":           "список",
		"<table>":        "таблица",
		"<th>№</th>":     "шапка таблицы",
		"<td>Т1</td>":    "тело таблицы",
	}
	for что, зачем := range для {
		if !strings.Contains(тело, что) {
			t.Errorf("в отрисованном нет %s (%s)", что, зачем)
		}
	}
	// Разделитель таблицы — разметка, а не строка данных.
	if strings.Contains(тело, "---") {
		t.Error("разделитель таблицы попал в отрисованное как данные")
	}
}

func TestRenderLinksToNeighbouringPages(t *testing.T) {
	_, тело := docs.Отрисовать("Смотри [для владельца](для-владельца.md) и [README](../README.md).")
	if !strings.Contains(тело, `href="для-владельца.html"`) {
		t.Errorf("ссылка ведёт на исходник, а не на соседнюю страницу: %s", тело)
	}
	if !strings.Contains(тело, `href="README.html"`) {
		t.Errorf("ссылка через каталог не переведена: %s", тело)
	}
}

func TestRenderEscapesWhatItShould(t *testing.T) {
	_, тело := docs.Отрисовать("Текст с <script>alert(1)</script> и `<b>кодом</b>`.")
	if strings.Contains(тело, "<script>") {
		t.Error("разметка из исходника уехала в страницу как разметка")
	}
	if !strings.Contains(тело, "&lt;b&gt;кодом&lt;/b&gt;") {
		t.Errorf("разметка внутри кода должна остаться видимой: %s", тело)
	}
}

// Страница обязана быть самодостаточной.
//
// Её читают там, где её открыли: в закрытом контуре, с флешки,
// из папки на диске. Ссылка на чужой адрес там показывает голый текст,
// а в худшем случае сообщает наружу, что страницу открыли.
func TestBuiltPagesAskNothingFromTheInternet(t *testing.T) {
	for имя, содержимое := range собранные(t) {
		for _, чужое := range []string{"src=\"http", "href=\"http", "//fonts.", "<script"} {
			if strings.Contains(содержимое, чужое) {
				t.Errorf("%s тянет чужое (%s): страница обязана открываться без сети", имя, чужое)
			}
		}
	}
}

// Собранное — из сегодняшнего исходника, а не из вчерашнего.
//
// Второй набор текстов, набранный в HTML руками, разойдётся с первым
// в тот же день и разойдётся молча: у текста нет прогона, который
// упадёт. Проверка пересобирает страницы во временный каталог
// и сверяет с лежащими рядом.
func TestBuiltPagesMatchTheirSources(t *testing.T) {
	времянка := t.TempDir()
	cmd := exec.Command("go", "run", "./cmd/docs")
	cmd.Dir = корень(t)
	cmd.Env = append(os.Environ(), "DOCS_OUT="+времянка)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("сборка документации не прошла: %v\n%s", err, out)
	}
	for имя, лежит := range собранные(t) {
		свежее, err := os.ReadFile(filepath.Join(времянка, имя))
		if err != nil {
			t.Errorf("%s собран из исходника, которого больше нет", имя)
			continue
		}
		if string(свежее) != лежит {
			t.Errorf("%s разошёлся с исходником: пересоберите `make docs` "+
				"и не правьте HTML руками", имя)
		}
	}
}

func собранные(t *testing.T) map[string]string {
	t.Helper()
	каталог := filepath.Join(корень(t), "docs", "html")
	записи, err := os.ReadDir(каталог)
	if err != nil {
		t.Fatalf("собранной документации нет: %v", err)
	}
	out := map[string]string{}
	for _, з := range записи {
		if !strings.HasSuffix(з.Name(), ".html") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(каталог, з.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[з.Name()] = string(raw)
	}
	if len(out) == 0 {
		t.Fatal("в docs/html нет ни одной страницы")
	}
	return out
}
