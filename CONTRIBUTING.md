# Как участвовать

*[In English below](#contributing)*

Спасибо, что читаете это до правки, а не после.

## Сначала — договориться

**Заведите обсуждение прежде, чем писать код**, если правка больше
чинения опечатки. Причина не в церемонии: у этого продукта записано,
что он обязуется делать и чего не будет делать никогда
([`REQUIREMENTS.md`](REQUIREMENTS.md)), и часть отказов — решения
с записанным доводом, а не пробелы. Патч, добавляющий диаграмму Ганта,
будет отклонён не потому, что он плох, а потому, что об этом уже
подумали и решили иначе. Обидно узнавать такое после выходных за
клавиатурой.

Чинение поломки, опечатки, перевода и документации — можно сразу.

## Что должно сойтись

```bash
make check      # формат, vet, все тесты кроме сквозных и нагрузочных
```

`make check` обязан проходить и в закрытом контуре, поэтому проверок,
которым нужна сеть, в нём нет. Их запускают отдельно:

```bash
make security   # уязвимости зависимостей и статический разбор
make e2e        # сквозные сценарии в настоящем браузере (нужен Chrome)
make load       # поведение под нагрузкой, идёт минуты
```

`go test ./...` без базы падает нарочно: почти всё проверяется против
настоящего PostgreSQL, и прогон без него однажды напечатал «ok»
у каждого пакета, пропустив 248 проверок из 259. Либо база задана
(`make check` задаёт её сам), либо прогон объявлен коротким:
`go test -short ./...`.

## Правила, из-за которых патч возвращают чаще всего

**Обещание закрепляется проверкой.** Новая возможность — это строка
в `REQUIREMENTS.md` и файл проверки в графе «чем закреплено». Пустая
графа означает, что требование держится на памяти; ровно ради этого
документ и заведён, и `internal/requirements` за этим следит.

**Схема меняется только миграцией, вперёд.** Миграция обязана работать
со старой версией приложения: она идёт до замены подов, и между её
концом и выкаткой старый код работает на новой схеме. Удаление колонки,
переименование и новая обязательная колонка без умолчания ломают
не нас, а заказчика — и ломают ещё раз при откате. Убирают лишнее
в два шага: версия N перестаёт этим пользоваться, версия N+1 удаляет.

**Изоляция организаций держится базой.** Проверка в коде забывается при
следующей правке; политика действует на любой запрос. Патч, который
чинит утечку добавлением `where org_id = …`, чинит симптом.

**Отказ объясняет, что делать.** «Не найдено» вместо «нельзя»
отправляет человека искать несуществующую поломку. Клиент различает
случаи кодом, а не текстом: разбор текста ломает экран при первой
правке формулировки.

**Необратимое спрашивает, обратимое — нет.** Архивация предлагает
вернуть и не задаёт вопросов; удаление насовсем называет то, что
исчезнет.

**Ложное срабатывание объясняют, а не замазывают.** У gosec —
`// #nosec G404 -- почему`, у своей проверки склейки запросов —
`// #sql-склейка: почему`. Маркер без объяснения не считается.

Подробности того же, но для того, кто правит каждый день, —
в [`внутренняя-инструкция.md`](внутренняя-инструкция.md) и в `внутренние-правила/skills/`.

## Язык

**Код, комментарии, сообщения об ошибках и интерфейс — по-русски.**
Это не предпочтение, а следствие: продукт русскоязычный, и отказ,
написанный по-английски, увидит тот, кто на нём не читает.

Комментарий объясняет **почему** так, а не что делает строка: что
делает, видно из неё самой.

Документация — на двух языках. Правка русской страницы обязывает
поправить английскую: `internal/translation` следит за этим отпечатком
исходника и падает, когда оригинал ушёл вперёд. Отпечаток подписывает
только то, что исходник видели в этом виде, — переставить его, не читая,
значит соврать себе же.

## Что происходит с патчем

Проверки идут в GitHub Actions: `make check` на каждый пуш и запрос,
`make security` — по расписанию и на запросах, меняющих зависимости.
Присланное с красной проверкой не смотрят: чинить сборку быстрее, чем
объяснять, почему она красная.

Отправляя патч, вы соглашаетесь, что он распространяется на условиях
Apache License 2.0 — той же, что и остальной репозиторий. Отдельного
соглашения подписывать не нужно.

---

# Contributing

Thank you for reading this before making a change rather than after.

## Agree first

**Open an issue before writing code** if the change is bigger than a
typo. Not ceremony: this product records what it commits to do and what
it will never do ([`REQUIREMENTS.md`](REQUIREMENTS.md), Russian), and
several of those refusals are decisions with a written argument behind
them, not gaps. A patch adding Gantt charts will be turned down not
because it is bad but because the question was already considered and
decided the other way. That is a miserable thing to learn after a
weekend at the keyboard.

Fixes to bugs, typos, translations and documentation: go straight ahead.

## What has to pass

```bash
make check      # formatting, vet, every test except end-to-end and load
```

`make check` must pass in a closed network too, so anything needing the
internet is not in it. Those run separately: `make security`,
`make e2e` (needs Chrome), `make load`.

`go test ./...` without a database fails on purpose: almost everything
is verified against a real PostgreSQL, and a run without one once
printed "ok" for every package while skipping 248 checks out of 259.
Either the database is configured (`make check` sets it up itself) or
the run declares itself short: `go test -short ./...`.

## The rules patches most often trip over

**A promise is backed by a test.** A new capability means a line in
`REQUIREMENTS.md` and a test file in the "backed by" column. An empty
column means the requirement rests on memory, which is the very thing
the document exists to prevent, and `internal/requirements` enforces it.

**The schema changes only by migration, forward.** A migration must
work with the *previous* version of the application: it runs before
pods are replaced, so between its end and the rollout the old code runs
against the new schema. Dropping a column, renaming one, or adding a
required column without a default breaks the customer, not us — and
breaks them again on rollback. Remove things in two steps: version N
stops using it, version N+1 drops it.

**Isolation between organisations is held by the database.** A check in
code is forgotten at the next edit; a policy applies to every query. A
patch that fixes a leak by adding `where org_id = …` fixes the symptom.

**A refusal explains what to do.** "Not found" instead of "not allowed"
sends someone hunting for a breakage that does not exist. The client
distinguishes cases by code, never by text.

**The irreversible asks, the reversible does not.**

**A false positive is explained, not papered over.** For gosec,
`// #nosec G404 -- why`; for our own query-concatenation check,
`// #sql-склейка: why`. A marker without an explanation does not count.

## Language

**Code, comments, error messages and the interface are in Russian.**
Not a preference but a consequence: the product is Russian-language,
and an error written in English will be read by someone who does not
read English.

A comment explains **why**, not what the line does.

Documentation is bilingual. Editing a Russian page obliges you to edit
the English one: `internal/translation` watches a fingerprint of the
source and fails when the original has moved ahead.

## What happens to a patch

Checks run in GitHub Actions. A submission with a red check is not
reviewed: fixing the build is faster than explaining why it is red.

By submitting a patch you agree that it is distributed under the Apache
License 2.0, the same as the rest of the repository. There is no
separate agreement to sign.
