# Security review

What a corporate security review asks for, in one place: what gets
installed, what crosses the perimeter, what holds the boundaries, and
what is checked automatically. Next to every claim stands the thing that
enforces it — a file, a test, a command — so it can be verified instead
of believed.

Two things this page is not. It is **not a certificate**: the product is
not a certified information-security tool and does not pretend to be
one. And it is **not a review of your installation**: half of what a
reviewer cares about — TLS, network segmentation, backups, who can reach
the database — is decided by whoever installs it. That half is marked
«yours» below rather than quietly claimed.

Everything here describes the version named in the footer of this page.

## What gets installed

One process and one PostgreSQL database. No message broker, no cache, no
object storage, no agents on other machines.

| Artefact | What it is | Integrity |
| --- | --- | --- |
| `ghcr.io/findias/takt:vX.Y.Z` | image on two bases (alpine, debian), amd64 and arm64 | digest in the registry, scanned with `trivy` before publication |
| `takt-vX.Y.Z-linux-amd64.tar.gz` | binary and built client for systemd | `SHA256SUMS` in the release |
| `takt-X.Y.Z.tgz` | Helm chart | `SHA256SUMS` in the release |
| `bundle-vX.Y.Z-linux-arch/` | everything for a closed network: image, chart, documentation, SBOM | `SHA256SUMS` inside the bundle, over every file |
| `sbom/takt-server.cdx.json`, `sbom/takt-web.cdx.json` | what it is built from, CycloneDX 1.6 | summed together with the rest |

The version is compiled into the binary from `git describe`, not read
from a file: a file inside an image can be swapped, and the version has
to be the same artefact as the code. `takt version` answers even when
the database is unreachable; `takt doctor` prints it first.

## What crosses the perimeter

| Direction | What | When |
| --- | --- | --- |
| in | HTTP on `LISTEN_ADDR` (`:8080` by default) — the client, the API, SCIM | always |
| out | PostgreSQL on `DATABASE_URL` | always |
| out | the OIDC provider you configured | only with `OIDC_ISSUER` set |
| out | the addresses of subscriptions an owner created | only when subscriptions exist |

That is the whole list. The product opens **no other outbound
connection**: no telemetry, no update check, no licence server, no
crash reporting, no CDN. The client bundle and the
documentation are served by the same process from its own files — which
is why the installation works in a network with no route to the
internet at all, and why the documentation pages carry no external
reference either.

What answers without authentication: `GET /healthz` and `GET /readyz`
(a status word and nothing else), the static client, and the contract —
`/api/v1/openapi.json` with its page at `/api/v1/docs`, which describes
the server rather than anyone's data; requiring a key to read how to
obtain a key would be a closed circle. Sign-in, invitation lookup and
the OIDC callback accept anonymous requests by necessity. Everything
else requires a session cookie or a bearer key.

**In the browser.** Every answer — a successful one, a refusal and an
unknown address alike — carries a content security policy (`default-src
'self'`, `frame-ancestors 'none'`, `object-src 'none'`, `base-uri
'none'`), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, a
same-origin referrer policy, and, where `BASE_URL` is https, HSTS for a
year. The application sets them itself instead of leaving them to your
proxy: a promise that holds only where somebody else configured it is
not a promise. There is one relaxation and it is written down rather
than hidden — `style-src` allows inline, because four places in the
client set a style attribute; the page at `/api/v1/docs` keeps its own
stricter policy.

## Data

| Data | Where | How long |
| --- | --- | --- |
| e-mail, display name, password hash, OIDC issuer and subject | `users` | until the person is erased |
| boards, cards, comments, labels, estimates, subdivisions | work tables | until deleted by a person |
| sessions (identifier, expiry) | `sessions` | 30 days, or until sign-out |
| audit log | `audit_events` | indefinitely — see below |
| webhook deliveries | `webhook_deliveries` | 30 days |
| invitations | `invites` | 30 days after they stop working |
| idempotency keys | service table | 24 hours |

There are no file uploads anywhere in the product, so no user-supplied
binary is ever stored, served or executed — which also means there is
nothing here for a malware scanner to scan.

**Personal data.** Only what your own people type: a name, a work
e-mail, and whatever they write on cards. The installation is yours, the
database is yours, and the vendor receives nothing — there is no
telemetry channel to receive it through. Under 152-ФЗ that makes you the
operator and leaves no processor on our side; under GDPR the same shape
means there is no transfer to a third party to legitimise.

**Erasure.** `DELETE /api/members/{userId}/identity` («Удалить данные»
for an owner) anonymises a person: the name becomes `Удалённый
участник`, the e-mail becomes an address in the reserved `.invalid`
domain, the password hash and the OIDC binding are emptied, sessions and
invitations addressed to them are deleted, and the fact itself is
written to the audit log. What the person wrote stays: comments and card
history are the work of the organisation, and deleting them would
rewrite its record. Two limits are stated in the code and worth
repeating: a person who belongs to more than one organisation cannot be
anonymised from one of them, and an unaccepted invitation issued in
someone else's organisation is beyond reach — it goes away with its
retention period.

**Retention of the log.** The audit log is never cleaned up
automatically. A log kept for a month answers «who granted this access
in March?» with silence, and that question is the reason the log exists.
If your policy demands a limit, set it on your side of the database.

## Identity

| Way in | How it is held |
| --- | --- |
| password | at least eight characters, no composition rules; `bcrypt` with the library default cost, and the hash is all that is stored |
| corporate provider | OIDC authorisation code; the issuer must be `https` outside loopback, the client secret is required, the organisation is fixed by configuration and not chosen by the token |
| integration key | 256 bits from `crypto/rand`, shown once, stored as SHA-256; an eight-character prefix identifies it in lists; expiry optional; scopes required |
| directory (SCIM 2.0) | a separate key with the `scim:write` scope only |

**Sessions.** A row in the database, an identifier in a cookie with
`HttpOnly`, `SameSite=Lax` and `Secure` whenever `BASE_URL` is https.
Thirty days. `DELETE /api/me/sessions` ends every other session — the
answer to «my laptop was stolen». A password change is not silent
either: the session that changed it survives, the rest do not.

Eight characters and no rules about digits or capitals is a deliberate
position rather than an oversight: composition rules push people towards
`Parol123!` and towards writing it down, while the thing that actually
stops guessing — a limit on attempts — is below. Where your policy
demands more, put people behind the corporate provider: then the policy
is the provider's, which is where you can enforce it anyway.

**Password guessing.** Ten failed attempts, then one a minute. Counted
per e-mail address rather than per source: the application lives behind
a proxy, every request arrives from the same address, and a bucket on
that address would lock out the whole organisation because of one
guesser. The limitation is stated rather than hidden: one password tried
against many addresses is not caught by this, and catching it requires a
trusted-proxy configuration you would have to set up.

**Request rate.** 120 requests of burst and 2 per second per key, with
`RateLimit-*` headers on every answer; SCIM gets its own bucket — 600
and 20 — so that synchronising a directory of employees does not run
into the interactive limit. Any request body over 1 MiB is refused
before it is read.

## Access control and isolation between organisations

Three roles — «Владелец» (owner), «Участник» (member), «Наблюдатель»
(viewer) — plus two appointments an owner makes: an administrator of a
subdivision, who runs their own subtree, and an observer of an area, who
reads it. Boards are visible to the whole organisation, to one
subdivision, or to named people only.

Isolation between organisations is held by **database policies, not by
checks in the code**. Every tenant table has row-level security forced
on; the application connects with a role that has no `SUPERUSER`, no
`BYPASSRLS` and no `CREATEDB`, and every transaction carries the tenant
context. The consequence worth spelling out to a reviewer: even a
mistake in the application cannot return another organisation's row,
because the row never leaves the database. The consequence worth
spelling out to an administrator: if the application is pointed at a
superuser role, policies stop applying and the separation quietly
disappears — which is why the installation guide creates the role
explicitly and `takt doctor` checks that the policies really do apply
to the role the application connects with, naming the fix when they
do not.

## Audit and logs

The audit log is written by **database triggers**, not by application
code: membership, invitations, subdivisions, board membership,
observers, and the creation, deletion and visibility changes of boards.
A code path cannot forget to write it, and a policy prevents an actor
from signing someone else's name. It is read by an owner and by an
observer of the whole organisation; to anyone else the log looks empty
rather than forbidden.

The application log goes to standard output as JSON, one line per
request: method, path, status, milliseconds. No bodies, no headers, no
tokens, no names — collect it with whatever your platform already
collects stdout with. Health checks are not logged, or the log would be
mostly probes.

## Cryptography

| Where | What |
| --- | --- |
| passwords | bcrypt |
| session identifiers, invitation and key tokens | `crypto/rand`, 256 bits |
| stored tokens | SHA-256; the original exists only in the answer that issued it |
| webhook deliveries | HMAC-SHA256 over timestamp and body, in `X-Signature` |
| transport | TLS terminated by your proxy or ingress; the application speaks plain HTTP inside the perimeter |
| database connection | whatever `sslmode` you put in `DATABASE_URL` |

Two things to decide on your side. Over a network, the database
connection should be `sslmode=verify-full` — the example in the
installation guide uses `disable` because it connects over a local
socket, and copying that line to a remote database would be a downgrade.
And there is **no GOST cryptography** in the product; if your regime
requires it, terminate TLS with a proxy that speaks it — the application
does not care what terminates it.

## Supply chain

The build is a Go binary with `CGO_ENABLED=0` and `-trimpath`, plus a
client bundle built by Vite; nothing is fetched at run time. Images are
published on two bases and two architectures from the same Dockerfile,
and the running image is exercised before publication — `takt version`
inside it — because a build that succeeds under emulation still says
nothing about starting. A base of your own — Astra Linux, RED OS, your
own hardened image — is built from that same Dockerfile on your side:
`make image RUNTIME_BASE=your-image:tag`. It is not added to the release
matrix, because publishing an image nobody but its requester installs
means scanning and answering for it forever.

| Question | Answer | Artefact |
| --- | --- | --- |
| what is it built from | CycloneDX 1.6 for the server (with the Go standard library) and for the client | `make sbom`, attached to every release and included in the bundle |
| known vulnerabilities in what we call | `govulncheck` on every push | `make security-report` → `govulncheck.sarif` |
| and in what we merely require | an OpenVEX statement saying whether the code is reachable | `govulncheck.openvex.json` |
| static analysis | `gosec`, and CodeQL separately for data flow across functions | `gosec.sarif` |
| client dependencies | `npm audit` at high and above | `npm-audit.json` |
| the image with its base | `trivy` on pull requests and before publishing | the run of `.github/workflows/security.yml` |
| is this the file you published | SHA-256 over every artefact, SBOM and report included | `SHA256SUMS` |
| is this the file we are running | the bare binary for each architecture, next to the archive and with its own sum | `takt-vX.Y.Z-linux-amd64.bin` in the release |
| does your source really produce that binary | the build is reproducible — `CGO_ENABLED=0`, `-trimpath`, the version arriving from the linker — and the release rebuilds it and compares byte for byte before publishing | the rebuild-and-compare step of the release run |
| are updates proposed | Dependabot for Go modules, npm packages and the actions themselves | `.github/dependabot.yml` |

Suppressions are explained rather than silent: a `gosec` exception reads
`// #nosec G404 -- why`, a deliberate SQL concatenation reads
`// #sql-склейка: why`, and a marker without an explanation is not
accepted by our own check.

## Vulnerability handling

Reports go through GitHub's private reporting — **Security** →
**Report a vulnerability** — and never through a public issue. The
timelines are the honest ones rather than the impressive ones: five
working days to acknowledge, ten to assess, then a release with an entry
in `CHANGELOG.md`. The supported version is the latest one; there is
nobody to backport into older ones. What counts as a vulnerability, and
what deliberately does not, is written out in
[`SECURITY.md`](../SECURITY.md) — including the case a scanner report
usually rests on: a vulnerability in a dependency we never call.

## Hardening — the part that is yours

| Do this | Why |
| --- | --- |
| terminate TLS at the proxy or ingress, with HSTS | the application speaks plain HTTP by design; `Secure` cookies switch on from `BASE_URL` starting with `https` |
| give the application a role without `SUPERUSER`, `BYPASSRLS`, `CREATEDB` | policies do not apply to a superuser, and isolation between organisations disappears without a single error message |
| use `sslmode=verify-full` for a database over a network | otherwise credentials and data cross it in the clear |
| keep the container as the chart ships it: non-root uid 10001, read-only root filesystem, no capabilities, `seccomp: RuntimeDefault` | the application keeps no state on disk — everything is in the database — so it needs neither write access nor privileges |
| put secrets in a Secret or an `EnvironmentFile` with mode 0600 | they arrive as environment variables; a world-readable unit file undoes that |
| restrict egress to the database, and to the provider and subscription addresses if you use them | the product cannot restrict its own outbound traffic, and an owner chooses subscription addresses |
| back the database up and test recovery | there is no state anywhere else, and the migration runs before the pods are replaced — `helm rollback` returns the pods, not the schema |
| set `SIGNUP=closed` where accounts come from the directory | otherwise the first anonymous visitor can create an organisation |

## Threat model

| Threat | What stops it | What keeps it stopped |
| --- | --- | --- |
| one organisation reads another's data | forced RLS, a role without `BYPASSRLS`, tenant context per transaction | requirements Б1–Б11, isolation tests, the migration chain replayed from scratch on every `make check` |
| a stolen session cookie | `HttpOnly`, `SameSite=Lax`, `Secure` on https, revoke-all-sessions | `crosssite_test.go` |
| cross-site writes | `SameSite=Lax` and no state change on `GET` | `crosssite_test.go` |
| SQL injection | parameters only; concatenation is allowed for our own text and must say so out loud | `internal/security/sast_test.go` |
| markup injection | the client never inserts raw HTML, and the content security policy allows scripts from this origin only | `internal/security/sast_test.go`, `headers_test.go` |
| the page put in someone else's frame | `frame-ancestors 'none'` and `X-Frame-Options: DENY` on every answer | `headers_test.go` |
| password guessing | ten failures, then one attempt a minute, per address | `login_test.go` |
| a leaked integration key | scopes, optional expiry, its own rate limit; a key can never be an owner | `clients_test.go` |
| a leaked invitation token | single use, expires, opens exactly one row and nothing beyond it | `org_test.go` |
| a tampered artefact | SHA-256 over every file, SBOM alongside | `SHA256SUMS` |
| a vulnerable dependency | four scanners, a VEX statement, Dependabot | the runs of `security.yml` and `codeql.yml` |
| the server made to call an internal address | only an owner can create a subscription | **not held by the product** — restrict egress on your side |
| an owner acting against their own organisation | nothing, by definition of the role | the audit log records it |

## What the product does not do

Said plainly, because a review that discovers it later reads it as
concealment.

- **No multi-factor authentication of its own.** With a corporate
  provider it is the provider's job, and that is the recommended
  configuration; with local passwords there is no second factor.
- **No encryption at rest.** Use the database's or the disk's.
- **No GOST cryptography** anywhere, including the TLS the proxy
  terminates.
- **No built-in WAF, IPS or DLP**, and no anti-virus surface, since
  nothing can be uploaded.
- **No LDAP or Active Directory bind.** People arrive through OIDC and
  SCIM.
- **No SIEM connector.** Logs are JSON on stdout, the audit log is a
  table and an API with the `audit:read` scope.
- **No certification.** The product is not a certified means of
  protecting information, and the artefacts above are evidence, not a
  certificate.

## Where this maps in your paperwork

The mapping names what the product provides. Everything an organisational
measure requires — orders, roles, models of threats to *your* system —
stays with you; a product cannot supply those, and a table claiming
otherwise would be worse than no table.

**Russian regime**

| Requirement | What the product gives |
| --- | --- |
| 152-ФЗ, roles | the installation is yours end to end: you are the operator, and there is no processor on the vendor's side because there is no channel to one |
| 152-ФЗ art. 21, ceasing processing | anonymisation of a person in one call, with the fact in the audit log |
| ФСТЭК order 21/17, ИАФ (identification and authentication) | unique accounts, bcrypt hashes, OIDC with a corporate provider, keys with scopes |
| УПД (access control) | three roles, two appointments, three board visibilities, isolation by database policies |
| РСБ (logging of security events) | the audit log written by triggers, kept indefinitely, readable through the API |
| АНЗ (vulnerability analysis) | four scanners, SBOM, VEX, published timelines for reports |
| ОЦЛ (integrity) | checksums over every artefact, version compiled into the binary, migrations forward only |
| ЗИС (protection of the system and communications) | no outbound connections beyond the three named; TLS at your boundary |

**International**

| Framework | What the product gives |
| --- | --- |
| OWASP ASVS V2, V3 (authentication, sessions) | bcrypt, server-side sessions, attempt limits, revoke-all |
| ASVS V4 (access control) | roles and appointments enforced in the database, not in the client |
| ASVS V5 (validation) | parameterised queries and a client that never inserts raw markup, both checked automatically |
| ASVS V7 (logging) | an audit log a code path cannot skip; request logs without personal data |
| ASVS V10, V14 (configuration, build) | SBOM, checksums, scanners in CI, a non-root read-only container |
| ISO/IEC 27001 A.5.15, A.8.2, A.8.3 (access) | roles, appointments, isolation policies |
| ISO/IEC 27001 A.8.8 (technical vulnerabilities) | scanners, VEX, Dependabot, disclosure timelines |
| ISO/IEC 27001 A.8.16, A.8.15 (monitoring, logging) | audit log and JSON request logs |
| SOC 2 CC6 (logical access) | everything in the two rows above, plus keys with scopes and expiry |
| SOC 2 CC7 (operations) | health probes, drain on shutdown, migration as a pre-upgrade step, `takt doctor` |

## Verify it yourself

Nothing here needs to be taken on trust. Everything below runs on a
clone of the repository.

```bash
GOOS=linux GOARCH=amd64 make binary   # rebuild the binary of a tag…
sha256sum bin/takt                    # …and compare with SHA256SUMS
make check           # our own checks, including isolation and the migration chain
make security        # govulncheck, gosec, npm audit
make security-report # the same, as SARIF, OpenVEX and JSON
make sbom            # what it is built from, CycloneDX
takt doctor          # what a running installation actually has
sha256sum -c SHA256SUMS
```

The first two lines are the ones worth doing before anything else: check
out the tag, rebuild, and you get the same bytes we published. A sum you
can only compare against ours proves that we did not change our mind
about the file; a sum you can reproduce proves that the binary is the
source you just read.

`takt doctor` is the shortest of these and the one worth running on the
installation itself rather than on a clone: besides the schema and the
isolation it names the two settings a review always asks about — whether
the connection to the database is over a network without TLS, and who is
allowed to create organisations. Neither is called a failure, because
either can be a deliberate decision; both are named, because silence
about them reads as «all fine».

To see the isolation for yourself, connect to the database as the
application role and count boards: the answer is zero without a tenant
context, on a database with thousands of them.

Questions that this page does not answer, and reports of anything it
answers wrongly, go to the address in [`SECURITY.md`](../SECURITY.md).
