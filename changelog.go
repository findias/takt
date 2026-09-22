// Package takt — корень репозитория. Go здесь ровно одно: списки
// изменений, вшитые в бинарник для страницы «Что нового» в справке
// (ROADMAP 30.3). Лежит в корне, потому что `go:embed` видит только
// файлы ниже своего каталога, а списки изменений живут здесь.
package takt

import "embed"

//go:embed CHANGELOG.md CHANGELOG.ru.md
var Changelog embed.FS
