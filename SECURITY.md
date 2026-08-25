# Безопасность

*[In English below](#security-policy)*

## Куда сообщать

**Не заводите публичную задачу.** Откройте приватное сообщение через
GitHub: вкладка **Security** → **Report a vulnerability**. Если она
недоступна, напишите владельцу репозитория напрямую.

Полезно приложить: версию (`takt version`), профиль установки (docker
compose, бинарник, чарт), что происходит и что должно происходить.
Минимальное воспроизведение ценнее подробного описания.

## Чего ждать в ответ

| Когда | Что |
| --- | --- |
| 5 рабочих дней | подтверждение, что сообщение прочитано |
| 10 рабочих дней | оценка: воспроизводится ли, насколько серьёзно |
| после исправления | выпуск, запись в `CHANGELOG.md`, упоминание вас, если захотите |

Сроки честные, а не образцовые: продукт ведёт небольшая команда. Если
ответа нет дольше обещанного — напомните, это не назойливость.

## Какие версии поддерживаются

Поддерживается последний выпуск. Обновление до него — часть
исправления: возвращать заплату в старые версии некому.

## Что мы считаем уязвимостью

Продукт ставится в собственный контур, и это меняет границу.
Уязвимость — то, что нарушает одно из обещаний:

- **данные одной организации видны другой.** Изоляция держится
  политиками базы, и любой способ их обойти — самое серьёзное, что
  здесь бывает;
- **действие выполняется без права на него** — участник делает то, что
  положено владельцу; ключ интеграции работает за пределами выданных
  разрешений; наблюдатель меняет работу;
- **чужая сессия или чужой ключ достаются постороннему** — межсайтовая
  запись, кража cookie, приглашение, применимое к другому адресу;
- **выполнение чужого кода или запроса** — внедрение SQL, разметки,
  команд;
- **вход достаётся не тому** — подмена личности через провайдера,
  связывание с чужой учётной записью по неподтверждённой почте.

Что уязвимостью **не** является:

- **находка сканера без пути к использованию.** Уязвимость
  в зависимости, которую мы не вызываем, — это `govulncheck` уже знает
  и молчит намеренно; присылайте, если покажете, что вызывается;
- **отказ в обслуживании нагрузкой от вошедшего.** Установка частная,
  людей до 130, и защита от своих же — не та задача;
- **отсутствие ограничений там, где их нет по решению.** Мягкий
  WIP-лимит не запрещает перенос осознанно, и это записано;
- **то, что делает владелец организации со своей организацией.**
  Владелец может всё по определению роли.

## Чем это проверяется у нас

Проверки разделены нарочно. Чужие инструменты — в `make security`:
`govulncheck` (есть ли известные дыры и вызываем ли мы их), `gosec`
(обычные ошибки в Go), `npm audit` (зависимости клиента). Им нужна
сеть, поэтому в `make check` их нет: он обязан проходить и в закрытом
контуре.

Свои — про этот продукт, и они как раз в `make check`
(`internal/security`): запрос к базе собирается параметрами, клиент
не вставляет сырую разметку, секретов в репозитории нет. Отдельно
и постоянно проверяются изоляция организаций
(`internal/store/isolation_test.go`), границы личности, межсайтовая
запись, разрешения ключей и ограничение частоты запросов —
требования Б1–Б10 и П8 в [`REQUIREMENTS.md`](REQUIREMENTS.md).

Ложное срабатывание не замазывают, а объясняют: у gosec —
`// #nosec G404 -- почему`, у своей проверки склейки —
`// #sql-склейка: почему`. Маркер без объяснения не считается.

Одно исключение сделано целым каталогом: `cmd/docs` — сборщик
документации — выведен из-под gosec. Довод в нём один: он не едет
заказчику. Довод в комментарии живёт до первой правки импортов,
поэтому за ним следит проверка `internal/security/shipped_test.go`:
как только `cmd/docs` окажется вкомпилирован в `cmd/takt`, она упадёт,
и исключение придётся снимать.

---

# Security policy

## Where to report

**Do not open a public issue.** Use GitHub's private reporting:
**Security** tab → **Report a vulnerability**. If that is unavailable,
write to the repository owner directly.

Useful to include: the version (`takt version`), the deployment profile
(docker compose, binary, chart), what happens and what should happen. A
minimal reproduction is worth more than a long description.

## What to expect back

| When | What |
| --- | --- |
| 5 working days | acknowledgement that the report was read |
| 10 working days | assessment: does it reproduce, how serious |
| after the fix | a release, a `CHANGELOG.md` entry, credit if you want it |

These are honest timelines rather than exemplary ones: a small team
maintains this. If it goes quiet for longer than promised, send a
reminder — that is not pestering.

## Supported versions

The latest release is supported. Upgrading to it is part of the fix:
there is nobody to backport patches into older versions.

## What counts as a vulnerability

The product is installed inside your own perimeter, and that moves the
boundary. A vulnerability is anything that breaks one of these
promises:

- **one organisation's data is visible to another.** Isolation is held
  by database policies, and any way around them is the most serious
  thing here;
- **an action is performed without the right to it** — a member doing
  what is reserved for an owner; an integration key working beyond its
  scopes; a viewer changing work;
- **someone else's session or key reaches an outsider** — cross-site
  writes, cookie theft, an invitation usable from another address;
- **execution of foreign code or queries** — SQL, markup or command
  injection;
- **sign-in lands on the wrong person** — identity spoofing through the
  provider, linking to another account via an unverified e-mail.

What is **not** a vulnerability:

- **a scanner finding with no path to exploitation.** A vulnerability
  in a dependency we never call is something `govulncheck` already
  knows and deliberately stays quiet about; send it if you can show it
  is reachable;
- **denial of service by load from a signed-in user.** The installation
  is private and sized for 130 people; defending against your own
  colleagues is not the task;
- **a missing restriction that is missing by decision.** The soft WIP
  limit deliberately does not block a move, and that is written down;
- **what an organisation owner does to their own organisation.** By
  definition of the role, an owner may do everything.

## How we check this ourselves

The checks are split on purpose. Third-party tools live in
`make security`: `govulncheck`, `gosec`, `npm audit`. They need the
network, so they are not in `make check`, which has to pass in a closed
network too.

Our own checks are about this product and are in `make check`
(`internal/security`): queries are built from parameters, the client
never inserts raw markup, no secrets in the repository. Isolation
between organisations, identity boundaries, cross-site writes, key
scopes and rate limiting are checked continuously — requirements Б1–Б10
and П8 in [`REQUIREMENTS.md`](REQUIREMENTS.md).
