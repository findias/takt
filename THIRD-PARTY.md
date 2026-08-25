# Чужой код в сборке / Third-party software

Здесь перечислено только то, что **едет вместе с продуктом**: попадает
в бинарник или в собранный клиент. Инструменты сборки и проверок
(`vite`, `typescript`, `playwright`, `vitest`, `libopenapi`) в поставку
не входят и здесь не перечислены — их лицензии обязывают того, кто
собирает, а не того, кто ставит.

Списку положено расходиться с `go.mod` и `web/package.json`, и это
не небрежность: в `go.mod` есть зависимости, которые вызываются только
из проверок, а в `package.json` — целый раздел `devDependencies`. Список
собран из того, что реально линкуется в `cmd/takt`, а не из объявленных
намерений.

This lists only what **ships with the product**: code linked into the
binary or bundled into the built client. Build and test tooling is not
distributed and is therefore not listed here.

## В бинарнике (Go)

| Модуль | Версия | Лицензия |
| --- | --- | --- |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/jackc/pgx/v5` | v5.10.0 | MIT |
| `github.com/jackc/pgpassfile` | v1.0.0 | MIT |
| `github.com/jackc/pgservicefile` | v0.0.0-20240606120523 | MIT |
| `github.com/jackc/puddle/v2` | v2.2.2 | MIT |
| `golang.org/x/crypto` | v0.55.0 | BSD-3-Clause |
| `golang.org/x/sync` | v0.22.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.41.0 | BSD-3-Clause |

## В собранном клиенте (npm)

| Пакет | Версия | Лицензия |
| --- | --- | --- |
| `react` | 19.2.x | MIT |
| `react-dom` | 19.2.x | MIT |
| `scheduler` | 0.27.x | MIT |
| `@atlaskit/pragmatic-drag-and-drop` | 1.8.x | Apache-2.0 |
| `@atlaskit/pragmatic-drag-and-drop-auto-scroll` | 3.0.x | Apache-2.0 |
| `@atlaskit/pragmatic-drag-and-drop-hitbox` | 1.2.x | Apache-2.0 |
| `bind-event-listener` | 3.0.x | MIT |
| `raf-schd` | 4.0.x | MIT |

## Что из этого требует упоминания

Apache-2.0 требует передавать вместе с продуктом файл `NOTICE`
с уведомлениями об авторстве. Единственная такая зависимость —
Pragmatic drag and drop (Atlassian Pty Ltd); её уведомление
воспроизведено в [`NOTICE`](NOTICE).

MIT и BSD-3-Clause требуют сохранять текст лицензии и уведомление
об авторских правах в распространяемых копиях. Тексты лицензий едут
в исходниках модулей и в `node_modules`; в комплекте для закрытого
контура (`make bundle`) — вместе с образом.

Apache-2.0 requires the `NOTICE` file to travel with the product; the
only such dependency is Pragmatic drag and drop, whose notice is
reproduced in [`NOTICE`](NOTICE). MIT and BSD-3-Clause require the
licence text and copyright notice to be retained in distributed copies.

## Как список поддерживается

Список сверяется проверкой `internal/license`: она берёт то, что
линкуется в `cmd/takt`, и требует, чтобы каждый модуль был здесь назван.
Добавили зависимость и забыли строку — проверка упадёт. Обратное —
строка про модуль, которого больше нет, — тоже: список, отставший
от сборки, хуже отсутствующего, потому что ему верят.

The list is enforced by `internal/license`: every module linked into
`cmd/takt` must appear here, and every module named here must still be
linked. A list that has drifted from the build is worse than no list,
because people trust it.
