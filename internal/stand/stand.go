// Package stand — заметка тестового стенда ветки (ROADMAP 30.7): что
// на стенде и как это проверить, покоммитно.
//
// Источник — сами коммиты ветки поверх master (у самого master —
// с последнего выпуска), а не отдельный файл
// заметок: второй источник разойдётся с коммитами в первый же день.
// С 22.09.2026 сообщение коммита — английское, после строки `--- ru ---`
// его русский перевод, и у каждой половины блок «как проверить»
// (CONTRIBUTING.md, «Language»). Стенд показывает ту половину, на языке
// которой открыт интерфейс.
//
// Заметка вшивается в бинарник, как версия: файл рядом с кодом можно
// подменить, а заметка обязана описывать тот же артефакт, что выложен.
package stand

import (
	"embed"
	"regexp"
	"strings"
)

var cyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)

//go:embed notes
var notes embed.FS

// Разделители, которыми workflow «Стенд» пишет журнал:
// `git log --format='%H%x1f%cI%x1f%B%x1e'`. Управляющие символы,
// а не текст: в сообщении коммита может встретиться что угодно,
// кроме них.
const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
	ruMarker  = "--- ru ---"
)

// Half — одна языковая половина сообщения коммита.
type Half struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Check — как найти изменение на экране; пусто — не написано,
	// и экран говорит об этом, а не молчит.
	Check string `json:"check"`
}

// Commit — один коммит ветки.
type Commit struct {
	Hash string `json:"hash"`
	Date string `json:"date"`
	En   Half   `json:"en"`
	// Ru — русская половина; nil — перевода в сообщении нет.
	Ru *Half `json:"ru"`
	// OnlyRu — коммит написан до правила «по-английски с переводом»
	// (22.09.2026) и только по-русски: En тогда та же русская половина,
	// а экран на английском говорит, почему текст русский.
	OnlyRu bool `json:"onlyRu,omitempty"`
}

// Note — всё, что стенд говорит о себе.
type Note struct {
	Branch string `json:"branch"`
	// Since — от чего посчитаны коммиты: `master` у ветки, тег
	// последнего выпуска у самого master.
	Since   string   `json:"since"`
	Commits []Commit `json:"commits"`
}

// Read — заметка этой сборки. Сборка мимо workflow «Стенд» получает
// пустую: ни ветки, ни коммитов.
func Read() Note {
	branch, _ := notes.ReadFile("notes/branch.txt")
	since, _ := notes.ReadFile("notes/base.txt")
	log, _ := notes.ReadFile("notes/log.txt")
	return Note{Branch: strings.TrimSpace(string(branch)), Since: strings.TrimSpace(string(since)), Commits: Parse(string(log))}
}

// Parse разбирает журнал, записанный workflow «Стенд». Порядок —
// как у git log: новые сверху.
func Parse(log string) []Commit {
	commits := []Commit{}
	for _, record := range strings.Split(log, recordSep) {
		fields := strings.SplitN(strings.TrimLeft(record, "\n"), fieldSep, 3)
		if len(fields) != 3 {
			continue
		}
		en, ru, translated := strings.Cut(fields[2], "\n"+ruMarker+"\n")
		c := Commit{
			Hash: strings.TrimSpace(fields[0]),
			Date: strings.TrimSpace(fields[1]),
			En:   half(en, "How to check:"),
		}
		switch {
		case translated:
			r := half(ru, "Как проверить:")
			c.Ru = &r
		case cyrillic.MatchString(c.En.Title):
			// До 22.09.2026 сообщения писались по-русски: выдавать их за
			// английскую половину значило бы приписать над русским
			// текстом «перевода нет — показан английский».
			r := half(en, "Как проверить:")
			c.Ru, c.En, c.OnlyRu = &r, r, true
		}
		commits = append(commits, c)
	}
	return commits
}

// half делит половину сообщения на заголовок, тело и «как проверить».
// Блок проверки — от строки с меткой до конца половины: он последний
// по правилу, и всё, что после метки, — шаги.
func half(text, checkLabel string) Half {
	text = strings.TrimSpace(text)
	title, rest, _ := strings.Cut(text, "\n")
	h := Half{Title: strings.TrimSpace(title)}
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), checkLabel); ok {
			h.Body = strings.TrimSpace(strings.Join(lines[:i], "\n"))
			h.Check = strings.TrimSpace(after + "\n" + strings.Join(lines[i+1:], "\n"))
			return h
		}
	}
	h.Body = strings.TrimSpace(rest)
	return h
}
