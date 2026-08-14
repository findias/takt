// Package migrations встраивает SQL-файлы в бинарник, чтобы у образа
// не было внешних зависимостей: скопировать один файл — достаточно.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
