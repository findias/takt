# What changed

For whoever installs and upgrades: what is different between two
installations and what to look at afterwards. Why it was decided this
way is in `research/*.html`; here is what to do on upgrade.

The rule for an entry: first whatever changes behaviour or needs action
on upgrade, then the rest. A version appears here before its tag —
otherwise the release gets made and the list gets written «later».

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
