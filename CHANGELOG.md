# What changed

For whoever installs and upgrades: what is different between two
installations and what to look at afterwards. Why it was decided this
way is in `research/*.html`; here is what to do on upgrade.

The rule for an entry: first whatever changes behaviour or needs action
on upgrade, then the rest. A version appears here before its tag —
otherwise the release gets made and the list gets written «later».

## v0.3.0 — 21 September 2026

**Fourteen migrations, all safe for the running version.** `0052` gives
labels a scope, `0053` gives blocks a deadline, `0054` rewrites the
helper functions behind the access policies, `0055` marks demo
sandboxes and lets an organisation be deleted as a whole, `0056` stores
each person's interface language (empty until they choose), `0057`
adds in-app notifications, each visible only to its recipient and only
while they can see the board, `0058` stores which kinds of notification
a person has switched off, `0059` adds the two time-driven kinds
(a block ending within a day, a card past the board's promise), `0060`
gives a card an optional key in the system it was imported from, so
that importing the same file twice creates no duplicates, `0061` marks
an email a person set for themselves (see the next paragraph), `0062`
gives labels a kind, so that an import can mark the cards of a person it
did not find, `0063` adds one-time sign-in links and marks an account
whose password has not been set yet, `0064` remembers what was chosen
for each person of an import source, `0065` keeps a card's history
from the system it was imported from. They
run in the
`pre-upgrade` hook as usual; pods of v0.2.3 keep working on the new
schema, and `helm rollback` of the pods needs nothing else.

**The server now does one job on its own: it lifts blocks whose
deadline has passed.** It runs inside `takt serve` once at start-up and
then every minute; nothing to configure. With several replicas each one
runs it, and that is safe: a block is closed once, at its deadline, not
at the moment of the check. Every ten minutes the same loop also writes
the time-driven notifications; with several replicas each is written
once, because a notification about the same thing is not written
twice.

**Subscriptions: two new events, not switched on by themselves.**
`card.block_until` (a block's deadline was set or changed) and
`card.block_expired` (a block lifted itself, without an author). A
subscription receives only the events ticked on it, so existing ones
stay as they were; tick the new events to receive them. A receiver
that rejects event types it does not know should learn these two.

**The integration contract grows, nothing is taken away.** The metrics
report (`GET /api/v1/boards/{id}/metrics`) gains `imported`,
`withoutImported` and an `imported` flag on finished and aging cards;
`?withoutImported=true` leaves imported cards out. `BLOCK_CARD`
accepts an optional `until` (ISO 8601 with a zone, in the future);
`SET_BLOCK_UNTIL` moves or clears it. A label created without a scope
is an organisation label, exactly as before.

**Operations and the board snapshot are about five times faster.** The
access-policy helpers were planned anew on every call; now the plan is
kept. On the demo board an operation went from 71 to 18 ms and the
snapshot from 71 to 13 ms. Nothing to do: it comes with `0054`.

**The server answers in the language of the request.** Refusals and
the names the server creates itself (default columns, «Моя команда»)
come in English when the `lang` cookie or `Accept-Language` asks for
it. Without either the answer is Russian, as before, so integrations
see no change.

**A new release file, `takt-fetch`, that is not installed.** The
exporter for moving into a closed network: run where there is internet,
it collects YouGile boards — with subtasks, task chats and task
history — into a `.takt` package. Built for linux, macOS and Windows,
with its own SBOM and sum; it is not in the image, the chart or the
bundle. How to use it is on its own page, `docs/takt-fetch.md`, and in
`takt-fetch help`, in Russian or English by `TAKT_LANG` / `LANG`.

**A new command, `takt import`, for boards coming into a closed
network.** It imports an import package (`.takt`, see
`docs/import-package.md`) on the server, on behalf of the person named
in `--as` and with their rights; without `--apply` it only previews.
Packages up to 50 MB can also be imported on the screen.

**A new setting, `YOUGILE_URL`, and a new outbound connection.**
Importing a board from YouGile makes the server call YouGile, only
while someone does so. Unset, that is the cloud `https://ru.yougile.com`;
a boxed YouGile has its own address; `off` switches import over the API
off. In a closed network set `off` and keep importing spreadsheets.
The Helm chart takes it as `yougile.url`.

**Corporate sign-in no longer links by an email a person typed in
themselves.** People can now change their own email, and takt sends no
letters to confirm it. Such an address is marked, and the first sign-in
through the identity provider does not attach to that account: whoever
typed someone else's address in advance would otherwise receive that
person's sign-in. The newcomer is told the address is taken and to ask
the administrator; the owner corrects the other account's email under
«Команда». Accounts that existed before the upgrade link as before.

**A new setting, `DEMO`, and it must stay off on your installation.**
`DEMO=on` is for the public demo only: the sign-in screen offers anyone
a sandbox organisation with sample data for 24 hours. It refuses to
start unless `SIGNUP=closed`. Unset, nothing changes.

**Another setting to leave unset: `STAND`.** `STAND=staging` is for our
own test stand of a branch — a strip on every screen and a note of what
the branch adds. It cannot be combined with `DEMO=on`.

### For people using the board

- **The interface speaks English as well as Russian.** It follows the
  browser until the person chooses; the choice is stored with the
  account and follows them to any device. The English documentation
  names the English buttons and shows English screenshots.
- **Help inside the application.** «Справка» in the header, `F1` or `?`
  opens the documentation at the section about the screen you are on,
  in the interface language, with a search over every section and
  a «Что нового» (what's new) page for this version and a glossary. It is
  served by the server itself, built from the same version, and needs
  no internet.
- **Import from a spreadsheet.** «Перенести задачи из таблицы…» under
  the new-board form takes an Excel workbook (`.xlsx`, any sheet) or
  a CSV from Google Sheets or another tracker's export, suggests which card field each column goes to, and
  shows what will happen before anything is written: how many cards,
  which emails were not found (nobody is created), which rows have
  problems, how dates were read. Cards go to a new board or an
  existing one, where each value of the file's column can be sent to
  one of the board's columns instead of creating a new one; importing the same file again skips what has already
  come. Dates come across, moves between columns do not. Imported cards
  are marked in their history and in «Поток», which can count without
  them.
- **Sign-in links.** takt sends no emails, so the owner issues a link
  under «Команда» («Ссылка для входа»): the person opens it, chooses a
  password and is signed in. It works once and for a week; a new one
  cancels the old. This is also how a forgotten password is replaced.
- **Import: a choice for every person.** The preview lists each
  assignee and author of the source: match them with a member, create
  an account (owner only; each new person gets a sign-in link), or leave
  them out. The choice is remembered for the next import of the same
  source, and a repeated import that only adds assignees can now be
  applied («Update the imported cards»).
- **Nobody disappears from imported cards.** An assignee the import did
  not find among the organisation's people leaves a label with their
  name on each of their cards. The label is drawn as an outline, is not
  offered when labelling by hand, and the filter finds it. Once the
  person is added and the board imported again, they become the
  assignee and the label comes off.
- **Import from YouGile.** The same screen takes a YouGile board over
  its API: sign in once to get the company's API key, pick the board,
  see what will happen. Tasks, columns, subtasks as parts, assignees by
  email, deadlines, dates, stickers as labels (a priority sticker as the
  priority) and checklists come across; what does not — YouGile's
  archive, chats, files, rights, time tracking — is listed before the
  import, and so is a subtask YouGile would not return: one such subtask
  does not stop the board. People are matched, created or left out as
  chosen in the preview (see below). When YouGile answers something
  unexpected, the refusal says what it answered instead of «internal
  error». A card's history says «imported from YouGile» with the task's
  number there, so it can be found in the old system.
- **Import packages.** The import screen reads a `.takt` package — boards
  from another tracker built outside a closed network and carried in —
  with subtasks, links and discussions, one board at a time with the
  same preview. A damaged package is refused and names the part that
  did not match.
- **Notifications.** A bell in the header counts what is unread: being
  called into a discussion (**@ Mention** under a reply), assigned to
  a card, a block on your card, less than a day left on that block, the
  block lifting at its deadline, and your card running longer than the
  board's promise.
  An entry opens the card. Each kind can be switched off under your
  name. You see a notification only while you can see its board.
- **A “?” next to concepts** — a column limit, the board's promise,
  a block, label scopes, the flow metrics, roles, key scopes: two or
  three sentences on what it is, and a link to the help section.
- **Personal settings live behind your name.** Clicking your name in
  the header opens «Личные настройки»: language, theme and density,
  password. «Выйти» is there too; the half-circle button is gone. Theme
  and density stay in the browser, since they often differ between
  devices.
- **Changing an email.** Your own — behind your name, on the
  «Вход» tab, with your password. A member's — by the owner, «Почта…»
  under «Команда», for someone who belongs to no other organisation.
  Both show in the audit log as «email: old → new». An email managed by
  the identity provider or directory is changed there.
- **A busy YouGile is waited out, not treated as a failure.** «502»,
  «503» and «504» are retried like «too many requests»; a chat YouGile
  will not return no longer stops the board — the card comes without it
  and the number of such chats is named, and in the background fetch
  that card simply waits for the next import of the board.
- **YouGile history and chats come across.** After an import over the
  API, task chats and task history are fetched in the background
  (YouGile allows 50 requests a minute; about half an hour for 800
  tasks), with progress on the import screen. Replies become the card's
  discussion, and YouGile's history shows on the card's «История» tab
  under «До переноса», with its own dates. `takt-fetch` puts the history
  into the package too (`--no-history` skips it).
- **A «Tasks» tab.** A person's cards on every board you can see —
  yours by default, anyone's from the list or from «Tasks» next to their
  name under «Команда». A private board you cannot see stays hidden.
- **The board's toolbar is rebuilt.** The view (Board, Table, Changes)
  is a list at the left; the filter is the main panel and stays open
  on a wide screen (on a narrow one it is folded under «Отбор» as
  before); the card search is a row below.
- **Labels are named on the card.** Under the title, as text — three
  of them and «+N»; they used to be coloured dots with the name only in
  a tooltip. Pressing them edits the card's labels.
- **Labels in three scopes.** A label belongs to the organisation, a
  subdivision (and everything inside it) or a single board, and every
  list says where it comes from. The same name cannot be used twice
  where scopes overlap; a board label is seen only by those who see the
  board.
- **A label is created right from the card.** Type a name that does not
  exist, pick where it applies, and it is created and hung in one go.
- **Blocks with a deadline.** «Снимется само» when blocking; a day
  before, the card says so, and the filter «Блокировка истекает» gathers
  such cards. The time blocked stays honest in the flow metrics.
- **Column width is dragged with the mouse** or set from the keyboard,
  and remembered in the browser. Neighbouring columns no longer merge
  into one surface.
- **The side panel makes room** instead of covering the board's
  controls.
- **The board header is one line**: views, search, «Отбор» with the
  number of active conditions, grouping, saved views; theme and density
  moved out of it, behind your name.
- **Red means "stopped".** Blocks, overdue commitments, errors and
  irreversible actions only; "worth a look" is now amber. Actions that
  remove something reversible are grey, not green.
- **Two actions now ask first**, because neither can be undone:
  revoking an integration key and deleting your own reply.
- **«Структура»: one ⋮ menu per row** instead of four visible actions;
  «Закрыть итерацию» is named in full.

### Dependencies

`golang.org/x/crypto` 0.57.0, `pgx` 5.11.0, `vitest` updated.
`golang.org/x/text` (already in the build through `x/net`) is now used
directly: it reads CSV saved by Excel in Windows-1251.

## v0.2.3 — 27 August 2026

**The binary is released as a file of its own, and you can obtain its
sum yourself.** Next to the archive there is now
`takt-vX.Y.Z-linux-amd64.bin` (and `-arm64`) with its own sum: that is
what gets checked in place — an installation from the archive leaves
`/opt/takt/takt` on disk, and the sum of the archive does not answer
that question. The build is reproducible, and the release proves it:
it rebuilds the binary from the same tag and compares byte for byte
before publishing. So the sum need not be taken on our word —
`GOOS=linux GOARCH=amd64 make binary` produces the same bytes.

**The sums of a release verify for whoever downloaded it.** Assets in a
release lie flat — GitHub keeps no directories for them — while the sums
were computed with paths (`sbom/takt-server.cdx.json`). For anyone who
downloaded v0.2.2 and ran `sha256sum -c SHA256SUMS`, seven lines out of
thirteen did not check out. The SBOM and the reports are now flattened
before the sums are computed.

**What to do about v0.2.2:** nothing. The sums file in that release was
replaced by a recomputed one — the files themselves are the same, the
values matched to the digit, only the names in the list changed.

**A release started by hand builds the tag it was given.** The `tag`
input was declared in the run dialog and read by not a single line: the
build took the branch the button was pressed on.

## v0.2.2 — 27 August 2026

**Every answer carries security headers.** A content security policy
(`default-src 'self'`, scripts from this origin only, no framing, no
objects), `nosniff`, `X-Frame-Options: DENY`, a same-origin referrer
policy, and — where `BASE_URL` is https — HSTS for a year. The
application sets them itself rather than leaving them to a proxy: every
installation configures its proxy differently, and a promise that holds
only on somebody else's side is not a promise.

**What to do on upgrade:** if you embed the board into another page in a
frame, the embedding stops working — that is what the prohibition is
for. If your proxy already sets headers of its own, the browser receives
both sets and applies both, meaning the stricter of the two; check that
together they are not stricter than the client needs.

**The chart names its own version.** Until now only the release set it,
and only `appVersion`: `version` sat in `Chart.yaml` and never moved, so
release v0.2.1 shipped `takt-0.2.0.tgz` — a file with the same name and
the same chart number as the previous release. Both versions now come
from the tag, and the chart of release v0.2.2 is called
`takt-0.2.2.tgz`.

**What to do on upgrade:** nothing, if the chart is installed from a
file (`helm install takt takt-*.tgz`) — the name resolves itself. If you
keep a `helm repo` mirror the chart is copied into by hand, check what
is in it: `takt-0.2.0.tgz` from v0.2.0 and from v0.2.1 are different
files under one name.

**The chart straight from the repository** now asks for the image
`ghcr.io/findias/takt:0.0.0` and does not find it: `Chart.yaml` holds a
placeholder, not a number. Before, it quietly installed the previous
version. The image tag is set at install time as before —
`--set image.tag=…`.

**The bill of materials and the scanner reports are now handed over as
files.** The release and the bundle for a closed network now carry
`sbom/*.cdx.json` (CycloneDX: the server together with the Go standard
library, and the client) and scanner reports in SARIF, OpenVEX and JSON.
They are produced by `make sbom` and `make security-report`.

**What to do on upgrade:** nothing. But `SHA256SUMS` is longer now: in
the release it lists the SBOM and the reports as well, and in the bundle
every file, including the nested directories of the documentation.
Before, the bundle did not get a sums file at all: the command tripped
over a directory and broke the build on its last step.

**`takt doctor` names what a security review asks about.** Two new lines
in the inspection: the connection to the database (whether it goes over
a network and with which `sslmode` — with advice about `verify-full` if
it is in the clear) and who may create organisations (`SIGNUP`). Neither
is called a failure — a trusted network and open registration can both
be decisions — but the inspection will no longer keep quiet about them.
The database password does not reach the output.

**A page for a security review** — `docs/security-review.md` and its
Russian translation `docs/ru/проверка-иб.md`: what crosses the
perimeter, what data lies where and for how long, what holds the
isolation between organisations, what the product deliberately does not
do, and where all of that maps in the paperwork — Russian and
international alike.

## v0.2.1 — 26 August 2026

**Upgrade without putting it off: a leak between organisations is
closed.**

The policies for background jobs recognised the worker by a single
sign — it has no tenant. The sign is wrong: there is no tenant when an
invitation is accepted either, nor in an exchange over an integration
key. Row policies combine with OR, so the holder of any invitation
link — that is, anyone who was ever invited anywhere — was shown the
worker's tables in full, across every organisation.

What was visible: event subscriptions (`webhooks`) together with the
`secret` column that deliveries are signed with, the delivery log, and
also invitations and audit entries older than the cleanup period. The
signing secret makes it possible to forge a delivery into someone else's
handler.

**What to do on upgrade:**

- upgrade. Migration `0051` fixes the schema; there is nothing to do by
  hand;
- **change the subscription secrets** if invitation links in your
  installation were used by people outside the organisation that owns
  the subscription. Consider the previous secrets disclosed:
  `docs/reference.md`, the section on integrations, explains where to
  change them;
- the integration keys need no rotation: their hashes were not exposed
  across that boundary.

Checked by `internal/org/org_test.go` — requirement Б11 in
`REQUIREMENTS.md`. The check fails on the old schema and passes on the
new one.

**Also**

- images are built on two bases, `alpine` and `debian`, and for both
  architectures, amd64 and arm64; the base is set by a build argument.
  Before publication every image is started on its own architecture:
  previously all that was known about the other one was that it
  compiled;
- **the bundle for a closed network is built for the architecture you
  name**: `make bundle BUNDLE_ARCH=arm64`, defaulting to your own. The
  directory is now called `dist/bundle-<version>-linux-<architecture>`;
  if you have scripts referring to the old name `dist/bundle-<version>`,
  they need fixing. Before, the bundle was quietly built for the
  architecture of the build machine, and on a server of another
  architecture that ended in «exec format error» after installation;
- CodeQL analysis of the code, `trivy` analysis of the image before
  publication, dependency updates proposed by Dependabot;
- a release by tag would have failed: the description was extracted from
  `CHANGELOG.md` by an `awk` call with an illegal variable name.

## v0.2.0 — 25 August 2026

The first release that can be shown to anyone: the repository opens up.
That barely touches the code — it touches everything around it.

**A change of name**

The product is called **Takt**. The former «Доска» (board) could not be
a name: the same word denotes a domain entity in this code — the
`boards` table, the `internal/board` package, the `/board/{id}` route —
and every mention had to be read twice.

**What to do when upgrading from v0.1.0:**

- the subcommand name changed: `board serve` → `takt serve`, and the
  same for `migrate`, `doctor`, `version`, `demo`. In the image the
  executable is now `/app/takt`;
- the chart is called `takt` and lives in `deploy/helm/takt`. Upgrading
  the previous release in place is not provided for: `helm uninstall
  board` and `helm install takt`, with the database left alone — the
  schema did not change;
- the image moved to `ghcr.io/findias/takt`;
- the role and the database of the development stand are called `takt`,
  the container `takt-dev-db`. An installation with an external database
  is unaffected: those names are yours, in `DATABASE_URL`.

The database schema did not change; this release has no migrations.

**Licence**

Apache License 2.0. There was no licence at all before, and that means
not «help yourself» but «you may not»: without explicit permission the
rights stay entirely with the author.

Chosen for its patent clause: whoever hands the code on and then goes to
court over patents on what was made in it loses the licence. For a
product installed inside a company and modified there, that matters more
than the brevity of MIT.

Third-party code that travels with the product is listed by name in
`THIRD-PARTY.md` — eight Go modules and five npm packages. Build tools
are not there: their licences bind whoever builds, not whoever installs.
`NOTICE` ships with the bundle and with the archive.

**Documentation**

There were two English pages; now there are eight: overview, the first
fifteen minutes, how to, reference, cheat sheet, design decisions,
installation, and the title page.

The installation guide became a document of its own — `docs/install.md`
and `docs/ru/установка.md` — and gained what it had lacked:
**requirements in one table** (versions, memory, disk, browsers,
network) and **running from a binary under systemd**, next to docker
compose and the chart. The README became a title page.

**Installation**

- `make tarball` builds an archive for installing from a binary: the
  binary itself, the built client, the licence and the list of
  third-party code. Archives for linux/amd64 and linux/arm64 with
  checksums are attached to the release;
- the bundle for a closed network (`make bundle`) now carries `LICENSE`
  and `NOTICE` too: Apache-2.0 requires passing them on with every copy,
  and the bundle is a copy.

**Fixed**

- **the version stopped being compiled into the binary** — the path in
  `-ldflags` drifted away from the module path during the rename. The
  linker does not complain about a symbol that does not exist: it
  quietly writes nothing, the build proceeds, the image is assembled,
  and only `takt version` answers «version not set». Pinned by a check:
  the path is compared with `go.mod`.

**What this release still does not promise**

- Nobody has ever performed the installation in a real cluster: the
  chart is checked by rendering and by consistency. For the same reason
  this is not 1.0 — until then the API contract may change.
- There are no attachments on cards.

## v0.1.0 — 23 August 2026

The first release with a number. Before it there was no version at all:
the repository had no tags, the bundle was built from a hash with a
`-dirty` suffix, and the binary itself stored no version — there was
nothing to answer «which one do we have» with, neither for the customer
nor for us.

**Installation and upgrade**

- `takt version` — which version this is; it answers even when the
  database is unreachable.
- `takt doctor` — a check that the installation was done right: is the
  schema applied in full, do the isolation policies hold, is `BASE_URL`
  the right one (and therefore will a secure cookie arrive), is the
  sign-in provider reachable, do database notifications get through.
  Readiness and liveness answer a different question — «the process is
  alive».
- `SIGNUP` — who creates organisations: `first` (the default: the first
  to arrive becomes the owner, after that by invitation only), `open`,
  `closed`. **A change of behaviour:** until now registration was always
  open and there was no way to turn it off.
- The database can be brought up by the chart
  (`postgresql.enabled=true`) — for a stand. The main arrangement is
  unchanged: outside, on hardware or in a separate operator.
- The version of the installation is visible on the «Команда» (Team)
  screen.

**To know when upgrading**

- `helm rollback` brings back the pods but not the database schema.
  Migrations are written to be compatible with the previous version of
  the application — that is what a rollback rests on — but a backup
  before the upgrade is still required.
- Migrations run as a separate job before the pods are rolled out; the
  application deliberately does not start on an uninitialised database.

**What this release does not promise**

- Nobody has ever performed the installation: the chart is checked by
  rendering and by consistency, but not by installing into a real
  cluster.
- There are no attachments on cards. The `STORAGE` setting is gone: it
  was read and used by nobody, and a setting without behaviour promises
  a capability and forces you to mount a volume nobody needs.
