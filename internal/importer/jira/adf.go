package jira

import (
	"encoding/json"
	"strings"
)

// Описание и комментарий в облаке — документ ADF (Atlassian Document
// Format), дерево узлов; у своей установки — строка вики-разметки.
// Пакет несёт простой текст (docs/import-package.md), поэтому дерево
// разворачивается в строки: абзац — строка, пункт списка — «- »,
// пункт чек-листа — «- [x] », как у YouGile. Оформление теряется,
// содержание — нет: упоминание остаётся именем, ссылка — адресом.

type node struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Attrs   json.RawMessage `json:"attrs"`
	Content []node          `json:"content"`
}

// text — простой текст из поля: строка как есть, документ ADF
// развёрнутым, пусто — пусто.
func text(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var doc node
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var b strings.Builder
	walk(&b, doc, "")
	lines := strings.Split(b.String(), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	out := strings.Join(lines, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

func walk(b *strings.Builder, n node, indent string) {
	var attrs struct {
		Text  string `json:"text"`
		URL   string `json:"url"`
		State string `json:"state"`
	}
	_ = json.Unmarshal(n.Attrs, &attrs)
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "hardBreak":
		b.WriteString("\n" + indent)
	case "mention", "emoji", "status", "date":
		b.WriteString(attrs.Text)
	case "inlineCard", "blockCard", "embedCard":
		b.WriteString(attrs.URL)
	case "paragraph", "heading":
		children(b, n, indent)
		b.WriteString("\n")
	case "bulletList", "orderedList", "taskList":
		for _, item := range n.Content {
			mark := "- "
			if item.Type == "taskItem" {
				var st struct {
					State string `json:"state"`
				}
				_ = json.Unmarshal(item.Attrs, &st)
				mark = "- [ ] "
				if st.State == "DONE" {
					mark = "- [x] "
				}
			}
			b.WriteString(indent + mark)
			var inner strings.Builder
			children(&inner, item, indent+"  ")
			b.WriteString(strings.TrimLeft(strings.TrimRight(inner.String(), "\n"), " "))
			b.WriteString("\n")
		}
	case "codeBlock":
		children(b, n, indent)
		b.WriteString("\n")
	case "rule":
		b.WriteString("---\n")
	case "tableRow":
		var cells []string
		for _, cell := range n.Content {
			var inner strings.Builder
			children(&inner, cell, "")
			cells = append(cells, strings.TrimSpace(strings.ReplaceAll(inner.String(), "\n", " ")))
		}
		b.WriteString(strings.Join(cells, " | ") + "\n")
	case "mediaSingle", "mediaGroup", "media":
		// Вложение — файлом в Jira; в пакете его нет, и отчёт об этом
		// говорит общей строкой о вложениях.
	default:
		children(b, n, indent)
	}
}

func children(b *strings.Builder, n node, indent string) {
	for _, c := range n.Content {
		walk(b, c, indent)
	}
}
