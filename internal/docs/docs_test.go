package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Документация сверяется с продуктом, а не живёт рядом с ним.
//
// Цена документа, который никто не сверяет, в этом проекте уже измерена:
// три сверки подряд нашли двадцать шесть расхождений, и почти каждое —
// обещание, жившее только в тексте. Документация для пользующихся
// врёт по-своему: она называет кнопку, которой нет, вкладку, которую
// переименовали, роль, которой не бывает. Читающий при этом решает,
// что не понял, — и идёт спрашивать.
//
// Проверяется то, что можно проверить дословно: подписи в «ёлочках»,
// названия ролей и видимостей, ссылки на файлы и упомянутые команды.
// Смысл текста не проверяется ничем и проверяться не может — это
// сказано вслух, чтобы зелёный прогон не читался как «документация
// верна».

func корень(t *testing.T) string {
	t.Helper()
	_, файл, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не найти собственный путь")
	}
	return filepath.Join(filepath.Dir(файл), "..", "..")
}

func документы(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	каталог := filepath.Join(корень(t), "docs")
	записи, err := os.ReadDir(каталог)
	if err != nil {
		t.Fatalf("документации нет вовсе: %v", err)
	}
	for _, з := range записи {
		if !strings.HasSuffix(з.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(каталог, з.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out["docs/"+з.Name()] = string(raw)
	}
	if len(out) == 0 {
		t.Fatal("в docs/ нет ни одного документа — проверка ничего не проверяет")
	}
	return out
}

// клиент — весь текст интерфейса разом: подписи ищутся по нему,
// а не по одному файлу, потому что кнопка и вкладка живут в разных.
func клиент(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	база := filepath.Join(корень(t), "web", "src")
	err := filepath.Walk(база, func(путь string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !(strings.HasSuffix(путь, ".tsx") || strings.HasSuffix(путь, ".ts")) {
			return nil
		}
		raw, err := os.ReadFile(путь)
		if err != nil {
			return err
		}
		b.Write(raw)
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// В ёлочках в документации стоят подписи интерфейса: «Сохранить вид»,
// «Структура», «Доступ». Если такой подписи в клиенте нет — либо её
// переименовали, либо документация выдумала.
var вЁлочках = regexp.MustCompile(`«([^»]{2,40})»`)

// Подписью считается то, что начинается с заглавной буквы: в ёлочках
// стоят и цитаты («что мы обещали и на когда»), и они — речь, а не
// элементы интерфейса. Правило грубое, зато не требует списка
// исключений, который сам стал бы местом, где прячут расхождения.
func подпись(строка string) bool {
	руны := []rune(строка)
	return len(руны) > 0 && строка != strings.ToLower(строка) &&
		string(руны[0]) == strings.ToUpper(string(руны[0]))
}

func TestDocsNameOnlyRealLabels(t *testing.T) {
	текст := клиент(t)
	for файл, содержимое := range документы(t) {
		for _, m := range вЁлочках.FindAllStringSubmatch(содержимое, -1) {
			// Перенос строки в markdown — верстка, а не часть подписи.
			название := strings.Join(strings.Fields(m[1]), " ")
			if !подпись(название) {
				continue
			}
			// Подпись может быть собрана из кусков (`Завести карточку
			// в «Очередь»`), поэтому ищем вхождение, а не равенство.
			if !strings.Contains(текст, название) {
				t.Errorf("%s называет «%s», а в интерфейсе такой подписи нет: "+
					"её переименовали или её не было", файл, название)
			}
		}
	}
}

// Ссылки на файлы обязаны вести на существующее: документация читается
// целиком редко, а по ссылкам ходят всегда.
var ссылка = regexp.MustCompile(`\]\(([^)#]+)\)`)

func TestDocsLinksLeadSomewhere(t *testing.T) {
	for файл, содержимое := range документы(t) {
		for _, m := range ссылка.FindAllStringSubmatch(содержимое, -1) {
			цель := m[1]
			if strings.HasPrefix(цель, "http") {
				continue
			}
			путь := filepath.Join(корень(t), filepath.Dir(файл), цель)
			if _, err := os.Stat(путь); err != nil {
				t.Errorf("%s ссылается на %s, которого нет", файл, цель)
			}
		}
	}
}

// Названные пути API и подкоманды обязаны существовать: «запустите
// board doctor» в документе к продукту без такой команды — это
// потерянный час того, кто читает.
func TestDocsMentionRealCommandsAndPaths(t *testing.T) {
	main, err := os.ReadFile(filepath.Join(корень(t), "cmd", "board", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	маршруты := ""
	каталог := filepath.Join(корень(t), "internal", "httpapi")
	записи, err := os.ReadDir(каталог)
	if err != nil {
		t.Fatal(err)
	}
	for _, з := range записи {
		if !strings.HasSuffix(з.Name(), ".go") || strings.Contains(з.Name(), "_test") {
			continue
		}
		raw, _ := os.ReadFile(filepath.Join(каталог, з.Name()))
		маршруты += string(raw)
	}

	команда := regexp.MustCompile("`board ([a-z]+)`")
	адрес := regexp.MustCompile("`(/(?:api|scim)/[a-zA-Z0-9/_{}.-]*)`")
	for файл, содержимое := range документы(t) {
		for _, m := range команда.FindAllStringSubmatch(содержимое, -1) {
			if !strings.Contains(string(main), `case "`+m[1]+`"`) &&
				!strings.Contains(string(main), `command == "`+m[1]+`"`) {
				t.Errorf("%s зовёт `board %s`, а такой подкоманды нет", файл, m[1])
			}
		}
		for _, m := range адрес.FindAllStringSubmatch(содержимое, -1) {
			// Из адреса берём начало до первого параметра: документация
			// называет семейство («/scim/v2»), а не конкретный маршрут.
			начало := strings.SplitN(strings.TrimPrefix(m[1], "/api/v1"), "{", 2)[0]
			начало = strings.TrimSuffix(начало, "/")
			if начало == "" || !strings.Contains(маршруты, начало) {
				t.Errorf("%s называет адрес %s, которого в маршрутах нет", файл, m[1])
			}
		}
	}
}

// Роли и видимости названы теми же словами, что в интерфейсе:
// «Наблюдатель области» и «Наблюдающий» — разница, из-за которой
// человек ищет несуществующую настройку.
func TestDocsUseTheSameWordsAsTheProduct(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(корень(t), "web", "src", "shared", "api", "names.ts"))
	if err != nil {
		t.Fatal(err)
	}
	словарь := string(raw)
	слова := regexp.MustCompile(`'([А-ЯЁ][^']+)'`).FindAllStringSubmatch(словарь, -1)
	if len(слова) == 0 {
		t.Fatal("словарь названий пуст — сверять не с чем")
	}

	// Обратная сторона: документы обязаны пользоваться этими словами,
	// а не своими. Проверяем, что каждое слово словаря где-то в docs/
	// встречается: роль или видимость, о которой не сказано нигде,
	// означает дыру в документации, а не лишнее слово в коде.
	весь := ""
	for _, содержимое := range документы(t) {
		весь += содержимое
	}
	for _, m := range слова {
		if !strings.Contains(весь, m[1]) {
			t.Errorf("документация нигде не говорит про «%s», хотя продукт это показывает", m[1])
		}
	}
}
