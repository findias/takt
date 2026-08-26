# Installation

How to install Takt, how to run it, and what it needs. Written for
whoever installs and keeps it running; for the people who use it, see
the [overview](overview.md).

Three ways, all giving the same application from the same image:
`docker compose` on one machine, a binary under systemd, a chart in
Kubernetes. The difference is who holds the process and who runs the
migrations.

## What you need to install it

The limits below come from measurements, not from a wish list. Beyond
them the product still has to work; it just is not obliged to hold its
response thresholds.

| What | How much |
| --- | --- |
| PostgreSQL | 16 or newer |
| Kubernetes (if you install the chart) | 1.25 or newer |
| CPU and memory per replica | 1 core, 256 MB; no state on disk |
| Database storage | from 1 GB; it grows with the event log, not with cards |
| Browser | those where Baseline newly is at least a year old |
| Network | nothing outbound; inbound through a TLS proxy |
| People per installation | up to 130 |

What the installation does **not** need: internet access, object
storage, a message queue, a separate cache. PostgreSQL is the only
external dependency. Turn on corporate sign-in and there is a second
one: the OIDC provider.

What we expect from whoever installs it. Obligations divide, and
without saying where the line falls there is nothing to check ours
against:

1. **The database is yours.** Backups, point-in-time recovery, disk
   space. The chart deliberately does not manage it — why, is in the
   section on point-in-time recovery below.
2. **A TLS proxy sits in front**, forwarding `Upgrade` and `Connection`.
   Without them the board stops updating itself.
3. **`BASE_URL` matches the address in the browser.** Invitation links
   and the change-stream address are built from it.
4. **No transaction-mode connection pooler in front of the database** —
   database notifications do not survive one.
5. **A backup is taken before an upgrade.** Migration compatibility
   with the previous version is our side; the data is yours.

There is something to check all of this with: `takt doctor` answers for
every item a machine can answer for.

## One image for every profile

One image is built. Compose and the Helm chart both take it without a
rebuild — the whole difference is environment variables and who runs
the migrations. A second `Dockerfile` would grow a divergence between
"on a server" and "in the cluster" the same day.

### Where to get it

The published image lives in the registry and is pushed by the release
run for a tag:

```bash
docker pull ghcr.io/findias/takt:v0.2.0
```

To build your own, from the repository root:

```bash
make image                    # takt:dev, alpine base, version from git describe
make image BASE=debian        # takt:dev-debian
make image BASE=astra         # takt:dev-astra
make images                   # all three at once, with a size table
```

### Three bases

There is one Dockerfile; the base of the final layer arrives as the
`RUNTIME_BASE` build argument. The build stages do not depend on it at
all: the binary is static (`CGO_ENABLED=0`) and the client is just
files.

| Base | Image | Size | Architectures |
| --- | --- | --- | --- |
| `alpine` (default) | `alpine:3.21` | 22 MB | amd64, arm64 |
| `debian` | `debian:12-slim` | 88 MB | amd64, arm64 |
| `astra` | `astra/ubi18:1.8.6` | 126 MB | amd64 only |

The choice is not about size. `debian` is taken where musl is viewed
with suspicion: the same libc as on most servers. `astra` is for
installations required to run a certified Russian OS.

**The Astra-based image is not published to any registry, and that is
not an oversight.** The licensing policy of Astra Group states that
licences are granted with no right of transfer to third parties, and an
image in a public registry is exactly a handout to an unbounded set of
people, none of whom hold one. So it is built on site by whoever does
hold the licence: `make image BASE=astra`. The base image itself pulls
anonymously from the Astra registry — no account is needed to build.

Astra publishes no arm64 image: there is no multi-architecture index
and the label reads `ru.astralinux.architecture: amd64`. Hence
`PLATFORMS_astra` is `linux/amd64` only.

By hand, the same thing:

```bash
docker build --build-arg VERSION=v0.2.0 \
  --build-arg RUNTIME_BASE=debian:12-slim \
  -t takt:v0.2.0-debian .
```

One `Dockerfile`, three stages: the client is built in
`node:22-alpine`, the binary in `golang:1.26-alpine` and statically
(`CGO_ENABLED=0`), and only those two land in the final `alpine`.
Neither Go nor Node ships. It runs as the unprivileged user `takt`
(uid 10001), listens on `:8080`, and serves the client from
`/app/web/dist`.

**`VERSION` is passed as a build argument, and that is not a
formality.** The version is linked into the binary rather than read
from a file: a file inside an image can be swapped, and the version has
to be the same artefact as the code. Hence the trap: `docker build .`
without `--build-arg` builds and runs fine, but `takt version` answers
"версия не задана" — honest, unlike an invented number, but not worth
pushing to a registry. `make image` fills `git describe` in for you.

There is no need to pull the image for `docker compose`: it builds one
itself (`build: .` in `docker-compose.yml`). For an air-gapped
installation the image travels as a file — `make bundle`; the section
on installing without internet access, below, covers it.

| Variable | What it sets |
| --- | --- |
| `BASE_URL` | the address in the browser, no trailing slash |
| `DATABASE_URL` | PostgreSQL connection string |
| `LISTEN_ADDR` | listen address, `:8080` by default |
| `WEB_DIR` | directory with the built client; empty means do not serve it |
| `SIGNUP` | who may create organisations: `first`, `open`, `closed` |
| `OIDC_ISSUER` | identity provider address; empty means password only |
| `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | the application's credentials at the provider |
| `OIDC_ORG` | the organisation a first-time arrival joins |
| `OIDC_LABEL` | the caption on the sign-in button |

**Migrations are a separate command,** `takt migrate`, not part of
startup. On one replica it makes no difference; on two, a simultaneous
start tears the database apart.

The role in `DATABASE_URL` must own its tables (or migrations will not
apply) but must **not** be a superuser and must not have `BYPASSRLS` —
row-level security does not apply to those, and tenant isolation
silently disappears. Managed clusters, CloudNativePG among them, create
such a role by default. The application checks this condition at
startup and refuses to run otherwise.

## On an ordinary machine

Two ways, both giving the same application. The difference is who holds
the process: docker or systemd.

**With docker compose** — when the machine has docker and you do not
have a database yet:

```sh
cp .env.example .env          # set POSTGRES_PASSWORD and BASE_URL
docker compose up -d
docker compose run --rm app doctor
```

Compose brings a database up alongside. For a demo stand that is right;
for production it is not — a database in a container has neither
backups nor point-in-time recovery. As soon as the installation becomes
real, move the database out and leave its address in `DATABASE_URL`.

**From the binary, without docker** — when the database already exists
and the extra layer buys nothing:

```sh
# 1. Role and database. The role owns its tables but is not a superuser
#    and has no BYPASSRLS: isolation policies do not apply to those,
#    and the separation between organisations quietly disappears.
sudo -u postgres psql <<'SQL'
create role takt login password 'YOUR_PASSWORD' nosuperuser nocreaterole nocreatedb;
create database takt owner takt;
SQL

# 2. The payload: the binary and the built client.
sudo mkdir -p /opt/takt
sudo tar xzf takt-<version>-linux-amd64.tar.gz -C /opt/takt --strip-components=1
#    or build it yourself: make build && cp -r bin/takt web/dist /opt/takt/

# 3. Settings. systemd reads this file, hence 0600 and root ownership.
sudo install -m 0600 /dev/null /etc/takt.env
sudo tee /etc/takt.env >/dev/null <<'ENV'
DATABASE_URL=postgres://takt:YOUR_PASSWORD@localhost:5432/takt?sslmode=disable
BASE_URL=https://takt.example.com
LISTEN_ADDR=127.0.0.1:8080
WEB_DIR=/opt/takt/dist
SIGNUP=first
ENV

# 4. Schema. A separate command, before the first start: two processes
#    migrating at once tear the database apart.
sudo -u takt bash -c 'set -a; . /etc/takt.env; set +a; /opt/takt/takt migrate'
```

The unit:

```ini
# /etc/systemd/system/takt.service
[Unit]
Description=Takt
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
User=takt
EnvironmentFile=/etc/takt.env
ExecStart=/opt/takt/takt serve
Restart=on-failure

# The application keeps no state on disk — everything is in the
# database. So it can be given neither write access nor extra
# privileges, and that is a consequence, not an austerity measure.
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
NoNewPrivileges=yes

# On shutdown the replica drops readiness first, waits for the proxy to
# strike it off, and only then closes connections. A shorter pause means
# torn requests for whoever was working at that moment.
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl enable --now takt

# doctor reads the same settings as the service — otherwise it checks a
# different installation and cheerfully reports that all is well.
sudo -u takt bash -c 'set -a; . /etc/takt.env; set +a; /opt/takt/takt doctor'
```

The version answers with no settings at all — `/opt/takt/takt version`.
It is asked precisely when nothing comes up.

Then put a TLS proxy in front of `127.0.0.1:8080`. The application
listens on the loopback deliberately: TLS, compression and body-size
limits are the proxy's job, and doing them twice means the two settings
will disagree. There are exactly two requirements on the proxy, both
listed above: the same `BASE_URL`, and `Upgrade` with `Connection`
forwarded.

**Upgrading** goes the same way as in a cluster, and in the same order:
back the database up, then `takt migrate`, then the new binary, then
`systemctl restart takt`, then `takt doctor`. The migration runs before
the binary is replaced, because a migration is required to work with
the *previous* version of the application and not the other way round —
that is also what makes rollback possible.

## Kubernetes

The chart is in `deploy/helm/takt`. It deliberately has no
dependencies: your database is your own, with backups and point-in-time
recovery, and slipping a replacement for it inside our chart would mean
taking responsibility for someone else's area. It follows that
installing needs no internet access — only an image in a reachable
registry.

```sh
kubectl create secret generic takt-db \
  --from-literal=DATABASE_URL='postgres://takt:...@postgres:5432/takt'

helm install takt deploy/helm/takt \
  --set baseURL=https://takt.example.com \
  --set database.existingSecret=takt-db \
  --set ingress.enabled=true --set ingress.host=takt.example.com
```

The database password is passed as a ready-made secret rather than a
chart value: from values it reaches both the release history and the
output of `helm get values`, which is read more widely than people
expect.

Migrations run as a job with a `pre-install,pre-upgrade` hook — once,
and before any new pod starts. A failed job is left behind: that is what
you look at to find out why the rollout did not move.

**Two probes, not one.** `/healthz` answers "the process is alive" and
does not touch the database: check liveness with the database and a
database that blinks restarts every replica at once, then comes back to
find them all busy restarting. `/readyz` answers "requests may be sent"
and does check the database. It is also the first thing to go dark on
shutdown: on a signal the replica drops readiness, waits five seconds
for the load balancer to strike it off, and only then closes
connections. That is why `terminationGracePeriodSeconds` in the chart
exceeds the pause and the shutdown timeout combined.

For a database inside the cluster there is
[`deploy/postgres/cloudnativepg-cluster.yaml`](../../deploy/postgres/cloudnativepg-cluster.yaml)
as an example of what we expect from one. The chart can also bring up a
database of its own (`postgresql.enabled=true`), and that is for a demo
stand only — it has no backups.

## Sign-in through a corporate provider

Configured with environment variables, not in the database. Per-
organisation settings would be right for a cloud installation, but then
the client secret would have to live in the database too — meaning
encryption, a key, key rotation, and the question "who decrypts this if
the server is lost". Corporate sign-in is installed in a closed
network, where there is one organisation and the secret already lives
in the cluster's secrets. Hence the rule: one installation, one
provider, no secrets in the database.

The redirect URI to register with the provider is built from
`BASE_URL`: `<BASE_URL>/api/auth/oidc/callback`. There is deliberately
no separate variable for it — it must not be able to disagree with
`BASE_URL`.

What happens on sign-in:

- a returning person is recognised by the pair "issuer + `sub`", not by
  e-mail: e-mail changes, and someone with a new address would
  otherwise lose their boards;
- a first-time arrival is linked to an existing account by a
  **verified** e-mail — an unverified address would let someone claim
  another person's account;
- if nobody matches, a new account is created and joined to `OIDC_ORG`.
  Anyone already in at least one organisation is added nowhere: signing
  in must not quietly change who you belong to.

The `id_token` signature is not verified, and that is deliberate: in the
code flow the token comes to us directly from the provider's endpoint
over TLS, which the specification explicitly considers sufficient (OIDC
Core, 3.1.3.7). A hand-written JWT check — base64 parsing, key
selection from JWKS, `alg` comparison — is exactly the code people get
wrong, and getting it wrong costs the whole of authentication. Claims
about the person are read from `userinfo` over the same connection.

## Directory provisioning (SCIM)

The provider pushes employees and groups to `/scim/v2/…`. The key is an
ordinary one from **«Ключи для интеграций»** (integration keys) with
the single `scim:write` scope; the
key itself determines the organisation, because a separate "where to
provision" setting would be a second source of truth.

What our side does:

- **deactivation removes membership but not the person.** Someone who
  left loses access immediately, while their cards, comments and audit
  entries stay signed by them. Deleting the record would mean rewriting
  history after the fact;
- **`DELETE` does the same as `active = false`.** Providers disagree:
  some send a deactivation, some a deletion, some both in a row;
- **groups become teams, not roles.** A role is a property of
  membership in an organisation, and the directory knows nothing about
  it: there, an employee belongs to a department, not "is an owner".
  Roles are assigned here, and provisioning does not touch them;
- **someone who left also leaves the teams** — a team that still lists
  a departed employee misleads whoever distributes work by it.

Bulk requests are not supported, and `ServiceProviderConfig` says so
honestly: they are reached for at thousands of employees, and this
installation is sized for a hundred.

## Point-in-time recovery

Backups are the database's job, and the chart deliberately does not
manage them. Only the database needs copying: the image is reproducible
from source, the static files live inside the image, and the
application keeps no state on disk — which is why it runs with a
read-only root.

Restore into a **new** cluster, not over the live one, or you lose the
ability to compare and step back if you restored to the wrong point.

What is specific to us about rolling the database back in time:

- **Webhooks will be sent again.** Retry keys and delivery marks roll
  back with everything else, and the worker works through the outbox
  afresh. Receivers must be ready for a repeat — the delivery
  identifier travels in a header, and that is how a repeat is
  recognised.
- **Sessions roll back too.** Anyone who signed in after the restore
  point is signed out; anyone issued a session before it keeps working.
  That is correct: a session is data like any other.
- **Access keys revoked after the point become valid again.** If the
  rollback is because a key leaked, revoke it once more afterwards.
- **Open boards re-read themselves.** Clients hold the change stream
  and reconnect, receiving a fresh snapshot. Nothing to do.

Test the restore before the outage, not during it: bring a copy up in a
separate namespace, apply the chart with `database.existingSecret`
pointing at the restored cluster, and open a board. It takes a quarter
of an hour and answers the only question the whole exercise exists for —
"does it actually come up from the copy?"

## Installing without internet access

```sh
make bundle          # builds dist/bundle-<version>: image, chart, README, checksums
```

The bundle travels as a file. On site:

```sh
sha256sum -c SHA256SUMS
docker load < takt-image.tar.gz        # or skopeo copy into your own mirror
helm install takt takt-*.tgz --set image.tag=<version> \
  --set baseURL=https://takt.example.com --set database.existingSecret=takt-db
```

The checksums are not a formality: into a closed network the file
travels through intermediaries, and "is this the same image" is a
question that will be asked. For the same reason the chart lets you
refer to the image by digest — a tag in a mirror can be rewritten, a
digest cannot.

Neither the chart nor the application reaches out on its own: the chart
has no dependencies and the application calls no external service. The
single exception is corporate sign-in, and it talks only to the
provider whose address you configured.

## Upgrading in a closed network

There will be more upgrades than installations. Same bundle, shorter
sequence:

```sh
sha256sum -c SHA256SUMS
docker load < takt-image.tar.gz
helm upgrade takt takt-*.tgz --reuse-values --set image.tag=<new version>
kubectl exec deploy/takt -- /app/takt doctor
```

1. **The image travels first, the release is upgraded second.** An
   upgrade to an image that is not in the mirror leaves pods in
   `ImagePullBackOff`; with `maxUnavailable: 0` the old ones keep
   serving, but the rollout hangs.
2. **Migrations run before pods are replaced,** as the `takt-migrate`
   job with a `pre-upgrade` hook. Until it passes, no new pod starts; a
   failed job stays in the cluster, and that is what you read.
3. **`--reuse-values` is mandatory** if anything was set at install
   time. Without it `helm upgrade` takes the chart defaults, and
   `signup`, `oidc` and the rest quietly revert.
4. **`doctor` afterwards** answers what readiness does not: right
   schema, right address, provider reachable, notifications arriving.

**Rollback.** `helm rollback takt` brings back the previous pods but
**not** the previous schema. What makes that safe is that migrations
are written to be compatible with the previous version of the
application, and that is checked on our side
(`migration_compat_test.go`). A database backup before the upgrade is
still required: we verify the rule, you own the data. If a migration
was declared breaking, `CHANGELOG.md` says so in its own line.

<!-- перевод: docs/установка.md sha256:9aa0e198f868 -->
