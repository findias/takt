// Команда docs собирает документацию в HTML.
//
// Отдельной командой, а не подкомандой board: в образ она не едет
// и базы не касается — это инструмент сборки, а не часть продукта.
//
// Запуск: make docs. Итог — docs/html/*.html, самодостаточные страницы
// без единой внешней ссылки: их читают в закрытом контуре, с флешки
// и из папки на диске.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/konkov/agile/internal/docs"
	"github.com/konkov/agile/internal/version"
)

// Что собираем и как называем в оглавлении. Список явный: README
// и требования адресованы разным людям, и порядок здесь — порядок
// чтения, а не алфавит.
var исходники = []struct{ путь, имя string }{
	{"docs/для-команды.md", "Работа на доске"},
	{"docs/для-владельца.md", "Организация"},
	{"README.md", "Установка и обслуживание"},
	{"REQUIREMENTS.md", "Что продукт обязан делать"},
	{"CHANGELOG.md", "Что менялось"},
}

func main() {
	корень, err := os.Getwd()
	если(err)
	// Куда складывать: обычно рядом с исходниками, но проверка
	// пересборки просит временный каталог — ей нужно сравнить,
	// а не переписать.
	вывод := filepath.Join(корень, "docs", "html")
	if свой := os.Getenv("DOCS_OUT"); свой != "" {
		вывод = свой
	}
	если(os.MkdirAll(вывод, 0o755))

	оглавление := make([]docs.Пункт, 0, len(исходники))
	for _, и := range исходники {
		оглавление = append(оглавление, docs.Пункт{Файл: имяHTML(и.путь), Имя: и.имя})
	}

	for _, и := range исходники {
		raw, err := os.ReadFile(filepath.Join(корень, и.путь))
		если(err)
		заголовок, тело := docs.Отрисовать(string(raw))
		if заголовок == "" {
			заголовок = и.имя
		}
		страница := docs.Страница{Файл: имяHTML(и.путь), Заголовок: заголовок, HTML: тело}
		готово := docs.Собрать(страница, оглавление, версияСборки())
		если(os.WriteFile(filepath.Join(вывод, страница.Файл), []byte(готово), 0o644))
		fmt.Printf("  %s → docs/html/%s\n", и.путь, страница.Файл)
	}
}

func имяHTML(путь string) string {
	имя := filepath.Base(путь)
	return strings.TrimSuffix(имя, filepath.Ext(имя)) + ".html"
}

// Версия для подписи: вшитая, если собирали через make, иначе —
// спрошенная у git. Отсутствие версии в подписи документации значит,
// что на вопрос «это про какую вашу» страница отвечает молчанием.
func версияСборки() string {
	if version.Задана() {
		return version.Строка()
	}
	out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return "не задана"
	}
	return strings.TrimSpace(string(out))
}

func если(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "документация не собралась:", err)
		os.Exit(1)
	}
}
