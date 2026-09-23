# takt-fetch: the exporter for a closed network

`takt-fetch` takes boards out of a cloud tracker and writes them into an
[import package](import-package.md) — a single `.takt` file that is
carried into a closed network and imported into takt there. So far it
knows YouGile, Jira — the cloud one and your own installation (Data
Center, Server) — Kaiten, cloud or on-premises, and monday; Weeek and
Trello come next.

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

## Jira

The same package from a Jira Software board. A board, not a project:
a board has columns and a filter, and a project has neither.

**Sign in.** Through the environment again. The cloud
(`…atlassian.net`) takes an email and an API token — create the token at
id.atlassian.com → **Security** → **API tokens**. Your own installation
(Data Center, Server) takes a personal access token from your profile,
without an email. There is no password option: the cloud does not accept
one over the API, and a person's password in a migration tool is exactly
what tokens are for avoiding.

```sh
export JIRA_EMAIL=anna@company.com   # the cloud only
export JIRA_TOKEN=…
takt-fetch jira boards --url https://company.atlassian.net
takt-fetch jira fetch --url https://company.atlassian.net --board 12 --out dev.takt
```

`boards` prints the id, the project, the title and the board type
(`kanban` or `scrum`). `--board` may be repeated. `--url` is required —
there is no default Jira. `--no-comments` leaves the comments out:
fewer requests, but the discussion does not come.

What goes in: the board's columns in their order, each issue into the
column of its status; the summary, the description as plain text
(checklists as `- [x]` lines, mentions as names), the assignee, labels,
priority, due date, the estimate from the field the board estimates by
(usually Story Points), the created and resolved dates, subtasks whose
parent is on the same board, **Blocks** links as blocks and all other
links as relates, and comments. Columns get a hint from their statuses'
category — *To Do*, *In Progress*, *Done* — which the preview lets you
change.

What does not, and is named under **Not imported**: attachments, time
tracking, sprints, versions and the change history; issues whose status
is mapped to no column of the board (Jira does not show them on the
board either) — as a number. In the cloud a person may hide their email
in their profile, and no administrator can undo that; such people come
by name, the package says how many there are, and you match them in
the preview.

**Your own Jira inside the closed network.** Then carrying anything
across the perimeter is not needed at all: run `takt-fetch` on a machine
inside the same network, next to Jira, with `--url` set to its address.
If its certificate is signed by your company's own authority, point
`SSL_CERT_FILE` at that authority's certificate — the exporter does not
switch checking off.

**How long.** Issues come a hundred per request; comments that did not
fit into the search answer take one more request per issue. When Jira
asks to wait (429, 503), the exporter waits and carries on.

## Kaiten

The same package from a Kaiten board, in the cloud (`company.kaiten.ru`)
or on your own server — the API is the same.

**Sign in.** With the API key from your Kaiten profile, through the
environment, as with Jira:

```sh
export KAITEN_TOKEN=…
takt-fetch kaiten boards --url https://company.kaiten.ru
takt-fetch kaiten fetch --url https://company.kaiten.ru --board 345 --out warehouse.takt
```

`boards` prints the id, the space and the title of every board the key
can see; a space the key has no access to is skipped, as Kaiten itself
does. `--url` is required, `--board` may be repeated, `--no-comments`
leaves the comments out — comments are one request per card.

What goes in: the board's columns in their order. A column with
subcolumns becomes one column per subcolumn, named `Column: subcolumn`,
so that three "In progress" of different columns do not merge; a card
lying in such a column itself goes into its first subcolumn. The column
type in Kaiten — queue, in progress, done — becomes the column's hint.
Then the title, the description, every card member as an assignee, tags
as labels, the size as the estimate, the ASAP flag as the highest
priority, the due date, the created date, the finish date of a card
that is done, the parent, blocks by another card of the board as
**blocks** links, and comments (HTML comments as plain text).

**Lanes are not columns.** In Kaiten they are rows across the board;
takt builds rows by grouping. So when a board has more than one lane,
each card gets a label `Lane: name`, and after the import the grouping
**By label** lays the board out the same way.

**A card with several parents** — Kaiten allows it, takt keeps a tree —
moves under the first parent on the board; the package says how many
such cards there were.

What does not come, and is named under **Not imported**: archived cards,
the reasons of blocks, attachments, checklists, time tracking and the
history of moves. People whose email Kaiten did not return come by name;
you match them in the preview.

**Kaiten inside the closed network.** As with Jira: run `takt-fetch` next
to it, with `--url` set to its address, and `SSL_CERT_FILE` if its
certificate is signed by your own authority.

**How long.** Cards come a hundred per request, plus one request per
card for comments. When Kaiten asks to wait (429), the exporter waits
until the moment Kaiten names and carries on.

## monday

The same package from a monday.com board. This is the source where the
exporter matters most: monday's own Excel export stops at 10,000 items
and does not say so, while its API pages through everything.

**Sign in.** With a personal token: your avatar → **Developers** →
**API token**. `--url` is not needed — monday has one API address.

```sh
export MONDAY_TOKEN=…
takt-fetch monday boards
takt-fetch monday fetch --board 1234567890 --out sales.takt
```

`boards` prints the id, the workspace and the title; documents and the
hidden subitem boards are left out.

**What becomes the columns.** A monday board has no columns in the
kanban sense; they come either from a status column or from the groups.
Without a flag the exporter takes the first status column that is not
called priority, says which one it took, and makes a column of each of
its values in monday's order; the values monday counts as done mark
their column as done. `--column "Stage"` names another status column,
`--column group` takes the groups in their order. An item with no value
in that column goes into the first column, and the package says how
many.

What goes in: the item's name, the people of every people column
(a team in it is not a person and is skipped), tags as labels, the
priority from a status column called priority, the due date from the
date column called due (or the only date column), the estimate from the
number column called estimate, the created date, subitems as subtasks
in their parent's column, dependencies as **blocks** links from the item
that is waited for, and updates as the discussion, oldest first.

What does not, and is named under **Not imported**: finish dates —
monday has none, so a done card gets the moment of the import; files,
time tracking, formulas, connections between boards. People whose email
monday did not return come by name; you match them in the preview.

**How long.** A hundred items per request with their updates and
subitems. monday counts query complexity per minute; when the budget
runs out it names the wait, and the exporter waits and carries on.

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
| no Jira address named | add `--url https://company.atlassian.net` or your installation's address |
| signing in to Jira needs JIRA_EMAIL and JIRA_TOKEN | set both for the cloud, `JIRA_TOKEN` alone for your own installation |
| Jira did not accept the sign-in | check the email and the token; a cloud token is paired with its email |
| Jira will not let you see this board | the account has no permission to browse the board's project |
| the address answers, but it is not Jira | `--url` points to something else, a login page or a proxy |
| the board … has more than 10000 issues | narrow the board's filter in Jira, or collect the board in parts |
| no Kaiten address named | add `--url https://company.kaiten.ru` or your own server's address |
| signing in to Kaiten needs KAITEN_TOKEN | set the API key from your Kaiten profile |
| Kaiten did not accept the token | the key was revoked or belongs to another Kaiten |
| Kaiten will not let you see this board | the account has no access to the board's space |
| the address answers, but it is not Kaiten | `--url` points to something else, a login page or a proxy |
| the board … has more than 10000 cards | split the board in Kaiten |
| signing in to monday needs MONDAY_TOKEN | set the personal token from your monday profile |
| monday did not accept the token | the token was regenerated, or it belongs to another account |
| the board has no status column … | name one of the listed columns in `--column`, or `--column group` |
| the board … has more than 10000 items | split the board in monday |

A chat YouGile does not return does not stop the board: the card comes
without it, and the package says how many such chats there were. A
«502», «503» or «504» is waited out, not treated as a failure.
