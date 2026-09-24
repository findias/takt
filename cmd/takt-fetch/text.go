package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer/jira"
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
	usage           string
	usageYougile    string
	usageJira       string
	usageKaiten     string
	usageMonday     string
	unknownSource   string
	unknownCommand  func(source, cmd string) string
	jiraNoURL       string
	jiraNeedLogin   string
	jiraNoBoard     string
	kaitenNoURL     string
	kaitenNeedLogin string
	kaitenNoBoard   string
	mondayNeedLogin string
	mondayNoBoard   string
	// mondayColumns — что стало колонками доски monday.
	mondayColumns func(byGroup bool, status string) string
	// lane — имя метки, которой едет дорожка Kaiten.
	lane           func(title string) string
	noOut          string
	noBoard        string
	needLogin      string
	passwordPrompt func(login string) string
	noPassword     string
	noCompany      func(company, login string) string
	manyCompanies  func(login string, names []string) string
	keyCreated     string
	boardOf        func(i, n int) string
	// epics — доска эпиков Jira и её колонки (этап 33.6), и строка хода дела.
	epics   jira.PortfolioNames
	epicsOf func(n int) string
	tooBig  func(title string, n, limit int) string
	board   func(id string) string
	written func(file string, boards, cards int) string
	carry   string
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

// #nosec G101 -- тексты справки и подсказок, а не учётные данные: «пароль» в них — подпись вопроса, а не значение
var ru = texts{
	usage: `takt-fetch — выгрузчик досок в пакет переноса takt

Запускают там, где есть интернет (или рядом со своей Jira или Kaiten
в том же контуре): выгрузчик заходит в YouGile, Jira, Kaiten или monday, собирает доски вместе
с подзадачами, связями и обсуждением и пишет файл-пакет (.takt). Пакет
несут в закрытый контур и переносят в takt: экраном «Перенос задач» →
«Пакет переноса» или командой takt import на сервере.

Команды:
  takt-fetch yougile boards [флаги]            какие доски есть: id, проект, название
  takt-fetch yougile fetch --board ID --out ФАЙЛ.takt [флаги]
                                               собрать пакет
  takt-fetch jira boards --url АДРЕС           доски Jira: id, проект, название, вид
  takt-fetch jira fetch --url АДРЕС --board ID --out ФАЙЛ.takt [флаги]
                                               собрать пакет из Jira
  takt-fetch kaiten boards --url АДРЕС         доски Kaiten: id, пространство, название
  takt-fetch kaiten fetch --url АДРЕС --board ID --out ФАЙЛ.takt [флаги]
                                               собрать пакет из Kaiten
  takt-fetch monday boards                     доски monday: id, рабочее пространство, название
  takt-fetch monday fetch --board ID --out ФАЙЛ.takt [флаги]
                                               собрать пакет из monday
  takt-fetch version                           версия выгрузчика
  takt-fetch help                              эта справка

Вход в YouGile — через окружение, не флагами (флаги видны в списке
процессов и остаются в истории оболочки):
  YOUGILE_KEY=ключ                          ключ API компании
  YOUGILE_LOGIN=почта [YOUGILE_PASSWORD=…]  почта и пароль; пароль, если не задан,
                                            спросят с клавиатуры. Нет ключа у компании —
                                            его заведут в YouGile и скажут об этом

Вход в Jira — тоже через окружение:
  JIRA_EMAIL=почта JIRA_TOKEN=токен         облако (…atlassian.net): почта и токен API
                                            (id.atlassian.com → Security → API tokens)
  JIRA_TOKEN=токен                          своя установка (Data Center, Server):
                                            личный токен из профиля

Вход в Kaiten — тоже через окружение:
  KAITEN_TOKEN=токен                        API-ключ из профиля Kaiten (облако и коробка)

Вход в monday — тоже через окружение:
  MONDAY_TOKEN=токен                        личный токен: аватар → Developers → API token

Флаги:
  --url АДРЕС          адрес YouGile (по умолчанию https://ru.yougile.com);
                       у Jira обязателен: https://компания.atlassian.net;
                       у Kaiten тоже: https://компания.kaiten.ru или адрес коробки;
                       у monday не нужен
  --company ИМЯ        компания, если их у почты несколько
  --board ID           доска; можно несколько раз; --all — все доски компании
  --out ФАЙЛ           куда записать пакет (перезаписывается целиком)
  --no-chats           без чатов задач: быстрее, но обсуждение не переедет
  --no-history         без истории задач: вдвое меньше запросов, но история
                       «до переноса» у карточек будет пуста
  --no-comments        Jira, Kaiten и monday: без комментариев — меньше запросов,
                       обсуждение не переедет
  --no-epics           Jira: эпики не выносить на доску-портфель — эпик на доске
                       останется её карточкой, эпик вне доски не переедет
  --column ИМЯ         monday: какая колонка статуса станет колонками доски;
                       group — группы; без флага — первая колонка статуса
  --collected-by ТЕКСТ кто собрал и зачем — попадёт в пакет как есть

Сколько ждать: YouGile пускает 50 запросов в минуту на компанию, и выгрузчик
ждёт, когда его просят подождать. С чатами и историей это два запроса
на задачу — доска в 800 задач собирается около получаса.

Пример:
  export YOUGILE_KEY=…
  takt-fetch yougile boards
  takt-fetch yougile fetch --board 5c2e… --out склад.takt --collected-by "Анна, переезд"

  export JIRA_EMAIL=anna@company.ru JIRA_TOKEN=…
  takt-fetch jira boards --url https://company.atlassian.net
  takt-fetch jira fetch --url https://company.atlassian.net --board 12 --out dev.takt

  export KAITEN_TOKEN=…
  takt-fetch kaiten boards --url https://company.kaiten.ru
  takt-fetch kaiten fetch --url https://company.kaiten.ru --board 345 --out склад.takt

  export MONDAY_TOKEN=…
  takt-fetch monday boards
  takt-fetch monday fetch --board 1234567890 --out продажи.takt

Язык справки и сообщений — из TAKT_LANG или LANG (ru, en).
Подробно: https://github.com/findias/takt/blob/master/docs/ru/выгрузчик.md
`,
	usageYougile: `takt-fetch yougile boards [флаги]
takt-fetch yougile fetch --board ID --out ФАЙЛ.takt [флаги]

Всё о командах, входе и флагах — takt-fetch help.
`,
	usageJira: `takt-fetch jira boards --url АДРЕС
takt-fetch jira fetch --url АДРЕС --board ID --out ФАЙЛ.takt [флаги]

Всё о командах, входе и флагах — takt-fetch help.
`,
	usageKaiten: `takt-fetch kaiten boards --url АДРЕС
takt-fetch kaiten fetch --url АДРЕС --board ID --out ФАЙЛ.takt [флаги]

Всё о командах, входе и флагах — takt-fetch help.
`,
	usageMonday: `takt-fetch monday boards
takt-fetch monday fetch --board ID --out ФАЙЛ.takt [флаги]

Всё о командах, входе и флагах — takt-fetch help.
`,
	unknownSource: "такого источника нет — есть yougile, jira, kaiten и monday (справка: takt-fetch help)",
	unknownCommand: func(source, cmd string) string {
		return fmt.Sprintf("у %s нет команды «%s» — есть boards и fetch (справка: takt-fetch help)", source, cmd)
	},
	jiraNoURL:       "не назван адрес Jira: --url https://компания.atlassian.net (или адрес своей установки)",
	jiraNeedLogin:   "нужен вход в Jira: JIRA_EMAIL и JIRA_TOKEN для облака или один JIRA_TOKEN (личный токен) для своей установки",
	jiraNoBoard:     "не названа ни одна доска: --board ID (список — takt-fetch jira boards --url …)",
	kaitenNoURL:     "не назван адрес Kaiten: --url https://компания.kaiten.ru (или адрес своей коробки)",
	kaitenNeedLogin: "нужен вход в Kaiten: KAITEN_TOKEN — API-ключ из профиля Kaiten",
	kaitenNoBoard:   "не названа ни одна доска: --board ID (список — takt-fetch kaiten boards --url …)",
	lane:            func(title string) string { return "Дорожка: " + title },
	mondayNeedLogin: "нужен вход в monday: MONDAY_TOKEN — личный токен (аватар → Developers → API token)",
	mondayNoBoard:   "не названа ни одна доска: --board ID (список — takt-fetch monday boards)",
	mondayColumns: func(byGroup bool, status string) string {
		if byGroup {
			return "колонки доски — группы monday"
		}
		return "колонки доски — значения колонки статуса «" + status + "» (другая — --column)"
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
	epics:      jira.PortfolioNames{Title: "Эпики", Idea: "Идея", Work: "В работе", Done: "Готово"},
	epicsOf: func(n int) string {
		return fmt.Sprintf("эпики: %d — отдельной доской-портфелем…", n)
	},
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

// #nosec G101 -- тексты справки и подсказок, а не учётные данные: «password» в них — подпись вопроса, а не значение
var en = texts{
	usage: `takt-fetch — exports boards into a takt import package

Run it where there is internet (or next to your own Jira or Kaiten inside
the same network): the exporter signs in to YouGile, Jira, Kaiten or monday, collects boards with
their subtasks, links and discussion, and writes a package file (.takt).
The package is carried into the closed network and imported into takt:
on the «Import tasks» screen → «Import package», or with takt import on
the server.

Commands:
  takt-fetch yougile boards [flags]            which boards there are: id, project, title
  takt-fetch yougile fetch --board ID --out FILE.takt [flags]
                                               build the package
  takt-fetch jira boards --url ADDRESS         Jira boards: id, project, title, type
  takt-fetch jira fetch --url ADDRESS --board ID --out FILE.takt [flags]
                                               build the package from Jira
  takt-fetch kaiten boards --url ADDRESS       Kaiten boards: id, space, title
  takt-fetch kaiten fetch --url ADDRESS --board ID --out FILE.takt [flags]
                                               build the package from Kaiten
  takt-fetch monday boards                     monday boards: id, workspace, title
  takt-fetch monday fetch --board ID --out FILE.takt [flags]
                                               build the package from monday
  takt-fetch version                           the exporter's version
  takt-fetch help                              this help

Signing in to YouGile — through the environment, not flags (flags show in
the process list and stay in the shell history):
  YOUGILE_KEY=key                           the company's API key
  YOUGILE_LOGIN=email [YOUGILE_PASSWORD=…]  email and password; without a password
                                            it is asked for on the keyboard. If the
                                            company has no key, one is created in
                                            YouGile and you are told so

Signing in to Jira — through the environment too:
  JIRA_EMAIL=email JIRA_TOKEN=token         cloud (…atlassian.net): email and API token
                                            (id.atlassian.com → Security → API tokens)
  JIRA_TOKEN=token                          your own installation (Data Center, Server):
                                            a personal access token from the profile

Signing in to Kaiten — through the environment too:
  KAITEN_TOKEN=token                        the API key from your Kaiten profile (cloud and on-premises)

Signing in to monday — through the environment too:
  MONDAY_TOKEN=token                        a personal token: avatar → Developers → API token

Flags:
  --url ADDRESS        YouGile address (default https://ru.yougile.com);
                       required for Jira: https://company.atlassian.net;
                       and for Kaiten: https://company.kaiten.ru or your own address;
                       not needed for monday
  --company NAME       the company, if the email has several
  --board ID           a board; may be repeated; --all takes every board
  --out FILE           where to write the package (overwritten whole)
  --no-chats           without task chats: faster, but discussions do not come
  --no-history         without task history: half the requests, but the cards'
                       «before the import» history stays empty
  --no-comments        Jira, Kaiten and monday: without comments — fewer requests,
                       but the discussion does not come
  --no-epics           Jira: do not move epics to a portfolio board — an epic on
                       the board stays its card, an epic off the board does not come
  --column NAME        monday: which status column becomes the board's columns;
                       group — the groups; without it — the first status column
  --collected-by TEXT  who collected it and why — goes into the package as is

How long: YouGile allows 50 requests a minute per company, and the exporter
waits when asked to. With chats and history that is two requests a task —
a board of 800 tasks takes about half an hour.

Example:
  export YOUGILE_KEY=…
  takt-fetch yougile boards
  takt-fetch yougile fetch --board 5c2e… --out warehouse.takt --collected-by "Anna, the move"

  export JIRA_EMAIL=anna@company.com JIRA_TOKEN=…
  takt-fetch jira boards --url https://company.atlassian.net
  takt-fetch jira fetch --url https://company.atlassian.net --board 12 --out dev.takt

  export KAITEN_TOKEN=…
  takt-fetch kaiten boards --url https://company.kaiten.ru
  takt-fetch kaiten fetch --url https://company.kaiten.ru --board 345 --out warehouse.takt

  export MONDAY_TOKEN=…
  takt-fetch monday boards
  takt-fetch monday fetch --board 1234567890 --out sales.takt

Help and messages follow TAKT_LANG or LANG (ru, en).
More: https://github.com/findias/takt/blob/master/docs/takt-fetch.md
`,
	usageYougile: `takt-fetch yougile boards [flags]
takt-fetch yougile fetch --board ID --out FILE.takt [flags]

Everything about commands, signing in and flags — takt-fetch help.
`,
	usageJira: `takt-fetch jira boards --url ADDRESS
takt-fetch jira fetch --url ADDRESS --board ID --out FILE.takt [flags]

Everything about commands, signing in and flags — takt-fetch help.
`,
	usageKaiten: `takt-fetch kaiten boards --url ADDRESS
takt-fetch kaiten fetch --url ADDRESS --board ID --out FILE.takt [flags]

Everything about commands, signing in and flags — takt-fetch help.
`,
	usageMonday: `takt-fetch monday boards
takt-fetch monday fetch --board ID --out FILE.takt [flags]

Everything about commands, signing in and flags — takt-fetch help.
`,
	unknownSource: "there is no such source — there are yougile, jira, kaiten and monday (help: takt-fetch help)",
	unknownCommand: func(source, cmd string) string {
		return fmt.Sprintf("%s has no command «%s» — there are boards and fetch (help: takt-fetch help)", source, cmd)
	},
	jiraNoURL:       "no Jira address named: --url https://company.atlassian.net (or your own installation's address)",
	jiraNeedLogin:   "signing in to Jira needs JIRA_EMAIL and JIRA_TOKEN for the cloud, or JIRA_TOKEN alone (a personal access token) for your own installation",
	jiraNoBoard:     "no board named: --board ID (list them with takt-fetch jira boards --url …)",
	kaitenNoURL:     "no Kaiten address named: --url https://company.kaiten.ru (or your own installation's address)",
	kaitenNeedLogin: "signing in to Kaiten needs KAITEN_TOKEN — the API key from your Kaiten profile",
	kaitenNoBoard:   "no board named: --board ID (list them with takt-fetch kaiten boards --url …)",
	lane:            func(title string) string { return "Lane: " + title },
	mondayNeedLogin: "signing in to monday needs MONDAY_TOKEN — a personal token (avatar → Developers → API token)",
	mondayNoBoard:   "no board named: --board ID (list them with takt-fetch monday boards)",
	mondayColumns: func(byGroup bool, status string) string {
		if byGroup {
			return "the board's columns are the monday groups"
		}
		return "the board's columns are the values of the status column «" + status + "» (another one — --column)"
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
	epics:      jira.PortfolioNames{Title: "Epics", Idea: "Idea", Work: "In progress", Done: "Done"},
	epicsOf:    func(n int) string { return fmt.Sprintf("epics: %d — as a separate portfolio board…", n) },
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
		if m := issuesLine.FindStringSubmatch(s); m != nil {
			return "issues: " + m[1]
		}
		if m := commentsLine.FindStringSubmatch(s); m != nil {
			return fmt.Sprintf("comments: %s of %s issues", m[1], m[2])
		}
		if m := cardsLine.FindStringSubmatch(s); m != nil {
			return "cards: " + m[1]
		}
		if m := cardCommentsLine.FindStringSubmatch(s); m != nil {
			return fmt.Sprintf("comments: %s of %s cards", m[1], m[2])
		}
		if m := itemsLine.FindStringSubmatch(s); m != nil {
			return "items: " + m[1]
		}
		return s
	},
}

var (
	columnLine = regexp.MustCompile(`^колонка (\d+) из (\d+): (.*)$`)
	chatsLine  = regexp.MustCompile(`^чаты: (\d+) из (\d+) задач$`)
	// Строки хода дела клиента Jira.
	issuesLine   = regexp.MustCompile(`^задачи: (\d+)$`)
	commentsLine = regexp.MustCompile(`^комментарии: (\d+) из (\d+) задач$`)
	// Строки хода дела клиента Kaiten.
	cardsLine        = regexp.MustCompile(`^карточки: (\d+)$`)
	cardCommentsLine = regexp.MustCompile(`^комментарии: (\d+) из (\d+) карточек$`)
	// Строка хода дела клиента monday.
	itemsLine = regexp.MustCompile(`^элементы: (\d+)$`)
)
