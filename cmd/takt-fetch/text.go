package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/findias/takt/internal/i18n"
)

// Тексты выгрузчика на двух языках. Свой набор, а не каталог сервера:
// сервер переводит отказы по русскому тексту, а у программы командной
// строки текстов немного, и держать их парами нагляднее — справка
// и сообщения видны целиком рядом с переводом.
//
// Язык — из окружения: TAKT_LANG, затем LC_ALL, LC_MESSAGES, LANG
// (ru_RU.UTF-8 → русский, en_US.UTF-8 → английский). Ничего не задано —
// русский, как у сервера.

type texts struct {
	usage          string
	usageYougile   string
	unknownSource  string
	unknownCommand func(cmd string) string
	noOut          string
	noBoard        string
	needLogin      string
	passwordPrompt func(login string) string
	noPassword     string
	noCompany      func(company, login string) string
	manyCompanies  func(login string, names []string) string
	keyCreated     string
	boardOf        func(i, n int) string
	tooBig         func(title string, n, limit int) string
	board          func(id string) string
	written        func(file string, boards, cards int) string
	carry          string
	// progress — строка хода дела от клиента YouGile на языке выгрузчика.
	progress func(s string) string
}

func textsFor(l i18n.Lang) texts {
	if l == i18n.EN {
		return en
	}
	return ru
}

// langOf — язык по окружению.
func langOf(env func(string) string) i18n.Lang {
	for _, name := range []string{"TAKT_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(env(name)))
		switch {
		case v == "" || v == "c" || v == "posix":
			continue
		case strings.HasPrefix(v, "en"):
			return i18n.EN
		default:
			return i18n.RU
		}
	}
	return i18n.RU
}

var ru = texts{
	usage: `takt-fetch — выгрузчик досок в пакет переноса takt

Запускают там, где есть интернет: выгрузчик заходит в YouGile, собирает
доски вместе с подзадачами, чатами и историей задач и пишет файл-пакет
(.takt). Пакет несут в закрытый контур и переносят в takt: экраном
«Перенос задач» → «Пакет переноса» или командой takt import на сервере.

Команды:
  takt-fetch yougile boards [флаги]            какие доски есть: id, проект, название
  takt-fetch yougile fetch --board ID --out ФАЙЛ.takt [флаги]
                                               собрать пакет
  takt-fetch version                           версия выгрузчика
  takt-fetch help                              эта справка

Вход в YouGile — через окружение, не флагами (флаги видны в списке
процессов и остаются в истории оболочки):
  YOUGILE_KEY=ключ                          ключ API компании
  YOUGILE_LOGIN=почта [YOUGILE_PASSWORD=…]  почта и пароль; пароль, если не задан,
                                            спросят с клавиатуры. Нет ключа у компании —
                                            его заведут в YouGile и скажут об этом

Флаги:
  --url АДРЕС          адрес YouGile (по умолчанию https://ru.yougile.com)
  --company ИМЯ        компания, если их у почты несколько
  --board ID           доска; можно несколько раз; --all — все доски компании
  --out ФАЙЛ           куда записать пакет (перезаписывается целиком)
  --no-chats           без чатов задач: быстрее, но обсуждение не переедет
  --no-history         без истории задач: вдвое меньше запросов, но история
                       «до переноса» у карточек будет пуста
  --collected-by ТЕКСТ кто собрал и зачем — попадёт в пакет как есть

Сколько ждать: YouGile пускает 50 запросов в минуту на компанию, и выгрузчик
ждёт, когда его просят подождать. С чатами и историей это два запроса
на задачу — доска в 800 задач собирается около получаса.

Пример:
  export YOUGILE_KEY=…
  takt-fetch yougile boards
  takt-fetch yougile fetch --board 5c2e… --out склад.takt --collected-by "Анна, переезд"

Язык справки и сообщений — из TAKT_LANG или LANG (ru, en).
Подробно: https://github.com/findias/takt/blob/master/docs/ru/выгрузчик.md
`,
	usageYougile: `takt-fetch yougile boards [флаги]
takt-fetch yougile fetch --board ID --out ФАЙЛ.takt [флаги]

Всё о командах, входе и флагах — takt-fetch help.
`,
	unknownSource: "источник пока один — yougile; остальные придут следующими (справка: takt-fetch help)",
	unknownCommand: func(cmd string) string {
		return fmt.Sprintf("у yougile нет команды «%s» — есть boards и fetch (справка: takt-fetch help)", cmd)
	},
	noOut:     "не назван файл пакета: --out склад.takt",
	noBoard:   "не названа ни одна доска: --board ID (список — takt-fetch yougile boards) или --all",
	needLogin: "нужен вход в YouGile: YOUGILE_KEY или YOUGILE_LOGIN (и пароль)",
	passwordPrompt: func(login string) string {
		return "Пароль YouGile для " + login + " (виден при наборе; YOUGILE_PASSWORD его заменяет): "
	},
	noPassword: "пароль не введён",
	noCompany: func(company, login string) string {
		return fmt.Sprintf("компании %q у %s нет", company, login)
	},
	manyCompanies: func(login string, names []string) string {
		return fmt.Sprintf("у %s несколько компаний — назовите одну: --company «%s»", login, strings.Join(names, "» | «"))
	},
	keyCreated: "В YouGile заведён ключ API для выгрузки; когда закончите, его можно удалить там.",
	boardOf:    func(i, n int) string { return fmt.Sprintf("доска %d из %d…", i, n) },
	tooBig: func(title string, n, limit int) string {
		return fmt.Sprintf("на доске «%s» %d карточек — takt переносит до %d за раз; разделите её в YouGile", title, n, limit)
	},
	board: func(id string) string { return "доска " + id },
	written: func(file string, boards, cards int) string {
		return fmt.Sprintf("Пакет записан: %s — досок %d, карточек %d.", file, boards, cards)
	},
	carry:    "Перенесите его в закрытый контур и откройте в takt: «Перенос задач» → «Пакет переноса».",
	progress: func(s string) string { return s },
}

var en = texts{
	usage: `takt-fetch — exports boards into a takt import package

Run it where there is internet: the exporter signs in to YouGile, collects
boards with their subtasks, task chats and task history, and writes a
package file (.takt). The package is carried into the closed network and
imported into takt: on the «Import tasks» screen → «Import package», or
with takt import on the server.

Commands:
  takt-fetch yougile boards [flags]            which boards there are: id, project, title
  takt-fetch yougile fetch --board ID --out FILE.takt [flags]
                                               build the package
  takt-fetch version                           the exporter's version
  takt-fetch help                              this help

Signing in to YouGile — through the environment, not flags (flags show in
the process list and stay in the shell history):
  YOUGILE_KEY=key                           the company's API key
  YOUGILE_LOGIN=email [YOUGILE_PASSWORD=…]  email and password; without a password
                                            it is asked for on the keyboard. If the
                                            company has no key, one is created in
                                            YouGile and you are told so

Flags:
  --url ADDRESS        YouGile address (default https://ru.yougile.com)
  --company NAME       the company, if the email has several
  --board ID           a board; may be repeated; --all takes every board
  --out FILE           where to write the package (overwritten whole)
  --no-chats           without task chats: faster, but discussions do not come
  --no-history         without task history: half the requests, but the cards'
                       «before the import» history stays empty
  --collected-by TEXT  who collected it and why — goes into the package as is

How long: YouGile allows 50 requests a minute per company, and the exporter
waits when asked to. With chats and history that is two requests a task —
a board of 800 tasks takes about half an hour.

Example:
  export YOUGILE_KEY=…
  takt-fetch yougile boards
  takt-fetch yougile fetch --board 5c2e… --out warehouse.takt --collected-by "Anna, the move"

Help and messages follow TAKT_LANG or LANG (ru, en).
More: https://github.com/findias/takt/blob/master/docs/takt-fetch.md
`,
	usageYougile: `takt-fetch yougile boards [flags]
takt-fetch yougile fetch --board ID --out FILE.takt [flags]

Everything about commands, signing in and flags — takt-fetch help.
`,
	unknownSource: "there is only one source so far — yougile; the others come next (help: takt-fetch help)",
	unknownCommand: func(cmd string) string {
		return fmt.Sprintf("yougile has no command «%s» — there are boards and fetch (help: takt-fetch help)", cmd)
	},
	noOut:     "no package file named: --out warehouse.takt",
	noBoard:   "no board named: --board ID (list them with takt-fetch yougile boards) or --all",
	needLogin: "signing in to YouGile needs YOUGILE_KEY or YOUGILE_LOGIN (and a password)",
	passwordPrompt: func(login string) string {
		return "YouGile password for " + login + " (visible as you type; YOUGILE_PASSWORD replaces it): "
	},
	noPassword: "no password entered",
	noCompany:  func(company, login string) string { return fmt.Sprintf("%s has no company %q", login, company) },
	manyCompanies: func(login string, names []string) string {
		return fmt.Sprintf("%s has several companies — name one: --company «%s»", login, strings.Join(names, "» | «"))
	},
	keyCreated: "An API key for the export was created in YouGile; you can delete it there when you are done.",
	boardOf:    func(i, n int) string { return fmt.Sprintf("board %d of %d…", i, n) },
	tooBig: func(title string, n, limit int) string {
		return fmt.Sprintf("the board «%s» has %d cards — takt imports up to %d at a time; split it in YouGile", title, n, limit)
	},
	board: func(id string) string { return "board " + id },
	written: func(file string, boards, cards int) string {
		return fmt.Sprintf("Package written: %s — %d boards, %d cards.", file, boards, cards)
	},
	carry: "Carry it into the closed network and open it in takt: «Import tasks» → «Import package».",
	// Клиент YouGile говорит о ходе дела двумя строками; переводятся
	// они здесь, а не каталогом сервера: это не отказ, и сервер их
	// не печатает никогда.
	progress: func(s string) string {
		if m := columnLine.FindStringSubmatch(s); m != nil {
			return fmt.Sprintf("column %s of %s: %s", m[1], m[2], m[3])
		}
		if m := chatsLine.FindStringSubmatch(s); m != nil {
			return fmt.Sprintf("chats: %s of %s tasks", m[1], m[2])
		}
		return s
	},
}

var (
	columnLine = regexp.MustCompile(`^колонка (\d+) из (\d+): (.*)$`)
	chatsLine  = regexp.MustCompile(`^чаты: (\d+) из (\d+) задач$`)
)
