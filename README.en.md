# Takt

**A kanban board with flow metrics, for one team.** It runs inside your
own perimeter: one process, one PostgreSQL database, no internet access
needed to install it or to run it.

[![Check](https://github.com/findias/takt/actions/workflows/check.yml/badge.svg)](https://github.com/findias/takt/actions/workflows/check.yml)
[![Security](https://github.com/findias/takt/actions/workflows/security.yml/badge.svg)](https://github.com/findias/takt/actions/workflows/security.yml)
[![Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE)

[По-русски](README.md) · [Installation](docs/en/install.md) ·
[Documentation](docs/en/overview.md) · [What changed](CHANGELOG.md) ·
[Contributing](CONTRIBUTING.md) · [Security](SECURITY.md)

Takt is named after *takt time* — the rhythm at which work leaves the
flow. That is what separates this from a task list: columns are marked
up for flow, and cycle time, throughput and the forecast are computed
from that markup rather than from fields someone filled in by hand.

![The board](docs/снимки/доска.png)

**The product interface is in Russian.** This file, and the English
documentation under [`docs/en/`](docs/en/overview.md), are for the
people who install and operate it. Everything a user clicks is quoted
in Russian, the way it appears on screen, with a translation beside it.

**To install it** — [`docs/en/install.md`](docs/en/install.md): what it
requires, docker compose, a binary under systemd, a chart in
Kubernetes, air-gapped networks, upgrades and rollback. **To use it** —
the [documentation](docs/en/overview.md). **What the product commits
not to break**, with the test file backing each promise, is in
[`REQUIREMENTS.md`](REQUIREMENTS.md) (Russian).

Below is what you need to know to work on this code: what it does, how
it is built, and why it is built that way.

## Quick look

```bash
make stand     # database, migrations, demo data, client build
make run       # the application on http://localhost:8099
```

This is a demo stand, not an installation: `make stand` wipes the
database and fills it with invented data. For a real installation see
[`docs/en/install.md`](docs/en/install.md).

Sign in as `anna@example.test` / `parol12345`. The demo fills a whole
organisation — a three-level tree of subdivisions, three boards with
all three visibilities, cards with every property, iterations, an
archive and three weeks of history. An empty interface shows neither
layout problems nor metrics, which is the point.

## What it does

The full list of what the product **commits not to break**, with the
test file backing each promise, is in
[`REQUIREMENTS.md`](REQUIREMENTS.md) (Russian). The same, briefly:

- **Board.** Columns, cards, moving with the mouse and from the
  keyboard (`Ctrl` with arrows), manual order. A move applies
  instantly and survives a lost connection and a page reload.
- **Flow.** Columns are marked up: kind (`queue`, `in_progress`,
  `done`), start and finish points, entry policy. Cycle time,
  throughput, cumulative flow and a percentile forecast come from that
  markup rather than from an eyeball estimate.
- **Work-in-progress limit per column.** Soft by default: the excess is
  highlighted and does not stop you. A hard limit is switched on by a
  flag and answers `409`.
- **Iterations.** Create, close, report on a closed one.
- **Live updates.** A change reaches everyone with the board open,
  without a reload.
- **Archive.** Of cards and of boards: what is put away comes back,
  what is deleted for good does not.
- **Organisation.** Four roles, invitations by link, subdivisions as a
  tree, board visibility by subdivision, observation of a subtree.
  Isolation between organisations is held by database policies, not by
  checks in code.
- **Sign-in.** By password and through a corporate provider (OIDC).
  Directory provisioning over SCIM.
- **Integrations.** An API with the version in the path, codes instead
  of text and safe retries; event subscriptions with a signature and
  retries; a full export of an organisation's data in one file.
- **Accessibility.** Everything works from the keyboard; contrast holds
  WCAG 2.2 AA in both themes; the page does not scroll sideways and
  does not lose content at double text size.

## What it does not do

What has been deliberately left out is listed at the end of
[`REQUIREMENTS.md`](REQUIREMENTS.md), each with what would lift the
deferral. Briefly: no file attachments on cards, no dates as a schedule
and no Gantt charts (that is a different product), and no public cloud
with self-service — the installation is meant to be private, which is
exactly why sign-up closes with a setting.

Which schema decisions cannot be taken back after the fact is in
[`research/schema-decisions.html`](research/schema-decisions.html)
(Russian).

## How it is built

```
cmd/takt/           the single executable: serve, migrate, doctor
internal/rank/      fractional indexing — string keys for card order
internal/board/     the board domain model and applying operations
internal/org/       organisations, team membership, invitations
internal/auth/      identities, membership, sessions
internal/httpapi/   routes, access, serving the client
internal/store/     connection pool, transaction scopes, migrations
migrations/         SQL, embedded into the binary
deploy/             database initialisation
web/                React + TypeScript
```

Four decisions in the schema cannot be taken back after the fact, and
were laid down before there was any data:

1. **`cards.position` — a string key of fractional indexing.** Moving a
   card is a single `UPDATE`, and two simultaneous drags do not break
   the order. The algorithm and its properties are in `internal/rank`,
   with tests.
2. **`card_events` — an append-only log.** Cycle time, the cumulative
   flow diagram and the activity feed are computed from the history of
   transitions. There is nowhere to recover that history from later.
3. **`org_id` in every table.** Free now; a migration of everything
   later.
4. **Start and finish points on columns.** Cycle time, card age and
   throughput are defined through them: without the markup the
   transition log accumulates but cannot answer "was work running
   then?". Columns can be marked up at any time; answering for the past
   cannot.

### Multi-tenancy

An organisation is a tenant. Identity and membership are separate:

```
users        e-mail and password, globally unique — this is a person
memberships  who is in which organisation and with which role
sessions     remember which organisation the person is working in now
```

A role (`owner`, `member`, `viewer`) is a property of membership, not
of the person: the same employee is an owner of their own team and a
viewer in somebody else's. There are exactly two ways into an
organisation — create it or accept an invitation; you cannot write
yourself into someone else's team.

**Isolation is provided by the database, not by code.** Every table
with data has row-level security with the policy
`org_id = app_current_org()`, where `app_current_org()` reads a
transaction setting. `store.BeginTenant` sets it, and it is
transaction-scoped — the connection goes back to the pool clean.

The key property: **with no tenant set, nothing is visible.**
`current_setting` returns NULL, the comparison yields NULL, the row
does not pass. Forgetting the scope gives you an empty answer, not
somebody else's data. One forgotten `where org_id = …` is not enough
for a leak.

> **The application must not connect as a superuser.** Policies do not
> apply at all to a superuser or to a role with `BYPASSRLS`, and the
> whole of the isolation quietly disappears: queries work, they just
> return too much. The application checks this at startup and refuses
> to run. The connection role is created by
> `deploy/postgres-init/10-app-role.sh`.

An invitation is the exception to the scheme: it is opened by a secret
link, when the organisation is not yet known and the person may not
have an account. It has a second policy that opens the row by the hash
of the token. Knowing the secret *is* the right — which is exactly what
an invitation is. Only the hash is stored, so the link cannot be shown
a second time.

### Subdivisions

A team may sit inside a team, up to five levels. The limit is not
caution: it turns "any number of ancestors" into an array of known
length that an index can work with. Linear caps at five; GitLab allows
twenty but recommends no more than five itself, and ties depth to
degradation.

The tree is stored twice: `teams.parent_id` is the source of truth,
`teams.ancestor_ids` is the path from the root, recomputed by a
trigger. GitLab did the same, replacing recursive queries with an array
and a GIN index, and for the same reason: the planner does not flatten
a recursive query inside a policy, while an array folds into an index
condition.

A role is inherited downwards and never reduced: someone in a division
is also in every department under it. "The maximum of what was
inherited" is predictable; "somewhere below it was trimmed" turns
working out an access question into an excavation. Everyone who has
implemented this settled on the same rule.

### Board visibility

Inside an organisation visibility stops being flat: a board is open to
everyone (`org`, the default), to a team (`team`), or closed
(`private` — by name only). The transaction scope is therefore wider
than the tenant: `store.InTenant` sets both the organisation and the
person, while `store.InOrg` sets only the organisation, for background
jobs that have no actor.

Observation is a row in `observers`, not a flag on a person. The
difference is that a flag inevitably means "the whole organisation":
GitLab made the observer a user type, so such a user sees the entire
instance and there is nothing to confine them to a subdivision. A row
naming a team opens exactly one subtree; a row without one opens the
whole organisation; and someone heading two divisions is expressed by
two rows. Observation is orthogonal to the role: "sees everything" and
"changes what" are different questions — otherwise we would need
`owner-observer`, `member-observer`, `viewer-observer`.

A closed board is the single exception to "sees everything", and a
deliberate one.

Two properties of this scheme are not obvious, and both are provided by
the database:

- **You can only close a board around yourself.** Postgres checks the
  `select` policy against the new row too, so a board cannot be moved
  into a state where you cannot see it. Otherwise the board would stay
  visible only to those listed, and nobody could fix it: an invisible
  board cannot be edited by anyone, the organisation owner included.
- **The membership of a closed board is visible to the organisation
  owner and to the member themselves.** The list of people on a hiring
  board is itself information.

### The audit log of administrative actions

On a board everything is logged — `card_events` records every move.
Outside the board, until recently, nothing was: who issued an
invitation, who changed a role, who moved a department, who made
someone an observer. Those questions are asked while working through an
incident, and always after the fact, so `audit_events` is a decision of
the same class as the transition log itself: either it is written from
the start, or it does not exist.

Three properties are provided by the database rather than promised:

- **A trigger writes it, not the code.** A change made by a migration,
  by a script, or by hand in `psql` reaches the log alongside one that
  came through the API. A log kept by the application stays silent in
  exactly the cases it was created for.
- **Append-only.** There are no `update` or `delete` policies at all,
  which means both are denied by default — to the organisation owner
  too.
- **The signature cannot be forged.** The insert policy requires the
  author of the record to match the identity in the transaction scope.

Secrets do not reach the log: the hash of an invitation token is cut
out, because the policy opens the invitation by that very hash —
knowing the hash is equivalent to knowing the link.

An action with no identity set is recorded with an empty author rather
than rejected: a log that breaks database maintenance would be removed
by the very next commit. The log is read by the owner and by an
observer of the whole organisation.

### The operations contract

The client sends an intent, not a computed state:

```
POST /api/boards/{id}/operations
  { "operationId": "…", "type": "MOVE_CARD",
    "payload": { "cardId": "…", "toColumnId": "…",
                 "place": "after", "afterCardId": "…" } }

→ 200 { version, patch }                            applied
→ 409 { error, columnId, currentOrder, version }    conflict
```

`place` takes `start`, `end` or `after` (the last together with
`afterCardId`) and is required by meaning: "default when the field is
absent" would mean different things in different operations.

`operationId` is stable across retries. If the response is lost on the
network, a retry with the same identifier returns the saved result
rather than creating a second card.

A `409` carries the current order of the affected column — the client
rebuilds just that column, without reloading the board. The same `409`
answers an attempt to put a card into a column whose hard limit is
exhausted.

`UPDATE_COLUMN` changes any subset of a column's properties:

```
{ "type": "UPDATE_COLUMN",
  "payload": { "columnId": "…", "kind": "in_progress",
               "isStartedPoint": true, "policy": "…",
               "wipLimit": 3, "wipLimitHard": false } }
```

A field that is not sent does not change. For the limit that means
"leave it alone" and "remove it" are different intents: only an
explicit `null` removes the limit.

## The integration API

The contract is published at `/api/v1/docs` on a running installation,
machine-readable at `/api/v1/openapi.json`. The version is in the path,
the reason for a refusal arrives as a code rather than as text, and a
repeat with the same idempotency key is safe.

## Development

```bash
make check              # gofmt, vet, every test except end-to-end and load
make e2e                # end-to-end scenarios in a real browser
make load               # behaviour under load; takes minutes
make security           # dependency vulnerabilities and static analysis
make web-dev            # Vite with hot reload on :5173
make build              # the binary, into bin/takt
make image              # the docker image, takt:dev (BASE=alpine|debian|astra)
make images             # images on all three bases
make bundle             # the payload for an air-gapped installation
```

`make check` must pass everywhere, including in a closed network, which
is why the security targets are not part of it — they need the network.

All three builds take their version from `git describe`, not from a
file: a file inside an image can be swapped, and the version has to be
the same artefact as the code. How to build the image by hand, and what
forgetting the version costs, is in
[`docs/en/install.md`](docs/en/install.md).
How to work on this codebase and how to send a patch are both
described in [`CONTRIBUTING.md`](CONTRIBUTING.md) (Russian).

## Contributing and security

How to send a patch is in [`CONTRIBUTING.md`](CONTRIBUTING.md); in
short, anything larger than a typo is worth discussing first, because
several of the refusals here are decisions with a written argument
rather than gaps. How people talk to each other is in
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).

Report vulnerabilities privately, **not as a public issue** —
[`SECURITY.md`](SECURITY.md) says how, and what you can expect back.

## Licence

Apache License 2.0 — [`LICENSE`](LICENSE). Third-party code that ships
with the product is listed in [`THIRD-PARTY.md`](THIRD-PARTY.md), and
the list is enforced by a test rather than maintained by hand.

There are deliberately no per-file licence headers: Apache-2.0 does not
require them, and eight hundred identical banners do exactly one thing —
push the comment explaining *why the code is like this* fifteen lines
further down.

<!-- перевод: README.md sha256:3c5f1c2faf2a -->
