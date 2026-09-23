package jira

import "strings"

// priority — приоритет Jira нашим словом. Названия Jira переводит
// на язык учётной записи, поэтому словарь двуязычный; незнакомый
// приоритет (свой у установки) остаётся «как у доски».
func priority(p *struct {
	Name string `json:"name"`
}) string {
	if p == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(p.Name)) {
	case "highest", "blocker", "критический", "наивысший", "блокирующий":
		return "highest"
	case "high", "critical", "major", "высокий", "важный":
		return "high"
	case "medium", "средний", "обычный":
		return "medium"
	case "low", "lowest", "minor", "trivial", "низкий", "самый низкий", "незначительный":
		return "low"
	}
	return ""
}
