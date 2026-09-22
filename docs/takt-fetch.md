# takt-fetch: the exporter for a closed network

`takt-fetch` takes boards out of a cloud tracker and writes them into an
[import package](import-package.md) — a single `.takt` file that is
carried into a closed network and imported into takt there. So far it
knows YouGile; Jira, Kaiten, Weeek and Trello come next.

It is a separate program on purpose. It runs where there is internet —
usually on a laptop — and the takt installed inside the closed network
does not contain it: the server that holds your boards cannot reach out
to anyone's cloud, and a security review can check that by the list of
files alone.

## Get it

Every release carries `takt-fetch` as separate files, one per system:

| System | File |
| --- | --- |
| Linux, x86-64 / ARM | `takt-fetch-<version>-linux-amd64`, `…-linux-arm64` |
| macOS, Intel / Apple | `takt-fetch-<version>-darwin-amd64`, `…-darwin-arm64` |
| Windows, x86-64 / ARM | `takt-fetch-<version>-windows-amd64.exe`, `…-windows-arm64.exe` |

Check the file against `SHA256SUMS` of the same release
(`sha256sum -c SHA256SUMS --ignore-missing`), make it executable on
Linux and macOS (`chmod +x`), and run it. Its bill of materials is
`takt-fetch.cdx.json` in the same release. From the source code:
`make fetch` builds it for this machine into `bin/takt-fetch`,
`make fetch-release` — for every system into `dist/`.

`takt-fetch version` prints its version; `takt-fetch help` prints the
built-in help.

## Sign in to YouGile

Credentials come from the environment, never from flags: flags are
visible in the process list and stay in the shell history. They are
never written into the package.

```sh
export YOUGILE_KEY=…                # the company's API key
# or an email and a password:
export YOUGILE_LOGIN=anna@company.ru
export YOUGILE_PASSWORD=…           # without it, the password is asked for
```

With an email and a password the exporter gets the company's API key
itself. If the company has no key yet, it creates one in YouGile and
says so — delete it there when you are done. If the email belongs to
several companies, name one with `--company`. On Windows use
`set YOUGILE_KEY=…` in `cmd` or `$env:YOUGILE_KEY="…"` in PowerShell.

## Find the board

```sh
takt-fetch yougile boards
```

Prints one board per line: its id, the project and the title. The id is
what `--board` takes.

## Build the package

```sh
takt-fetch yougile fetch --board <id> --out warehouse.takt --collected-by "Anna, the warehouse move"
```

| Flag | What it does |
| --- | --- |
| `--board ID` | the board; repeat it for several; `--all` takes every board of the company |
| `--out FILE` | where to write the package. It is written whole at the end: an interrupted run leaves no half package under that name |
| `--no-chats` | without task chats: faster, but the discussion does not come |
| `--no-history` | without task history: half the requests, but the cards' «before the import» history stays empty |
| `--collected-by TEXT` | who collected it and why; goes into the package as is and is shown on import |
| `--company NAME` | the company, if the email has several |
| `--url ADDRESS` | a boxed YouGile; the default is `https://ru.yougile.com` |

What goes in: columns, tasks with descriptions and checklists,
subtasks (those in no column too), assignees, stickers, deadlines,
dates, task chats and task history. What does not: files, access rights
and time tracking — the package names them, and the import preview
shows them under **Not imported**.

**How long it takes.** YouGile answers at most 50 requests a minute per
company; the exporter waits whenever YouGile asks it to, and prints
how far it has got. A task's chat and its history are two requests, so
a board of 800 tasks takes about half an hour; `--no-history` halves
that, `--no-chats` with it leaves a few minutes.

## Carry it in and import

The package file is readable only by its owner. Carry it across the
perimeter the way your rules say, then:

- up to 50 MB — on the screen: **Boards** → **Import tasks from a
  spreadsheet…** → **Import package**. The same preview as for a
  spreadsheet, with a choice for every person;
- bigger, or many boards in a row — on the server with `takt import`
  (see [Installation](install.md#moving-boards-into-a-closed-network)).

The package is checked whole on reading: a part whose checksum does not
match is refused and named.

## Language

The help and the messages follow `TAKT_LANG`, then `LC_ALL`,
`LC_MESSAGES`, `LANG` — `ru` or `en`. With none of them set it speaks
Russian, as the server does.

## When it refuses

| Message | What to do |
| --- | --- |
| signing in to YouGile needs YOUGILE_KEY or YOUGILE_LOGIN | set one of them — see the section on signing in above |
| YouGile did not accept the sign-in or the key | check the key or the password; a key belongs to one company |
| … has several companies — name one | add `--company "Name"` |
| no board named | add `--board ID` (from `takt-fetch yougile boards`) or `--all` |
| the board … has N cards — takt imports up to 10000 | split the board in YouGile |
| YouGile does not answer | check the internet connection or `--url` |
| YouGile asks to wait: too many requests | the exporter waits by itself; this means it waited out its two minutes — run it again |
| YouGile answered 500 to … | a problem on YouGile's side; run it again later |

A chat YouGile does not return does not stop the board: the card comes
without it, and the package says how many such chats there were. A
«502», «503» or «504» is waited out, not treated as a failure.
