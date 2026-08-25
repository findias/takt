# Reference

Lists and tables: what lives where, what things are called, what equals
what. Not read in order — read when needed.

**The interface is in Russian.** On-screen wording is quoted, with a
translation next to it.

## Roles in the organisation

| Role | Reads | Changes work | Runs the organisation |
| --- | --- | --- | --- |
| «Владелец» (owner) | yes | yes | yes |
| «Участник» (member) | yes | yes | no |
| «Наблюдатель» (viewer) | yes | no | no |

Beyond roles, the owner appoints:

| Appointment | What it grants |
| --- | --- |
| Subdivision administrator | runs their own subtree: membership, nested nodes, boards |
| Area observer | reads everything in their subtree |

## Board visibility

| Visibility | Who sees it |
| --- | --- |
| «Всей организации» | everyone in the organisation |
| «Своей команде» | people in the subdivision that owns the board |
| «Только вписанным» | those listed by name |

## Keys

| Keys | What it does |
| --- | --- |
| `Ctrl+K` | palette: search cards and commands |
| Arrows | move between cards |
| `Ctrl` + arrows | move a card |
| `Enter` | open a card |
| `E` | rename without opening |
| `Escape` | close a panel, a menu or the palette |

## Filters

| Filter | What it shows |
| --- | --- |
| Text | cards whose title contains the substring |
| Assignee | whose cards; separately, "nobody's" |
| Labels | cards carrying **all** the selected labels |
| «Горит» | highest and high priority |
| «Срок подходит» | due today, tomorrow, the day after, or overdue |
| «Заблокированные» | those where work has stalled |
| «Дольше обещанного» | those running longer than the board's promise |
| Iteration | attached to an iteration; separately, "not in an iteration" |

## Groupings

None, by assignee, by label, by iteration, by priority.

## Metrics in «Поток» (flow)

| Metric | What it answers |
| --- | --- |
| Cycle time | how many days work usually takes (percentiles) |
| Throughput | how many cards were finished, by week |
| In progress | how much work is running right now |
| Ageing | what is running and how long it has been running |
| Cumulative flow | three bands by day: queued, in progress, done |
| Forecast | how many days to finish a given number of cards |
| Discarded | how much was taken off the board unfinished |

## Integration key scopes

| Scope | What it opens |
| --- | --- |
| `boards:read` | reading boards and cards |
| `boards:write` | changing boards and cards |
| `structure:read` | reading the subdivision structure |
| `audit:read` | reading the audit log |
| `scim:write` | provisioning people and groups from a directory (separate key only) |

## Subscription events

Card: created, renamed, description changed, estimate changed,
priority changed, due date set or cleared, moved, blocked, unblocked,
link added, link removed, commented on, custom field filled, custom
field cleared, added to an iteration, removed from an iteration, work
marked done, mark removed, archived, restored to the board.

## Installation settings

| Variable | What it sets |
| --- | --- |
| `BASE_URL` | the address in the browser, no trailing slash |
| `DATABASE_URL` | PostgreSQL connection string |
| `LISTEN_ADDR` | listen address, `:8080` by default |
| `SIGNUP` | who creates organisations: `first`, `open`, `closed` |
| `WEB_DIR` | directory with the built client |
| `OIDC_ISSUER` | identity provider address; empty means password only |
| `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` | credentials at the provider |
| `OIDC_ORG` | the organisation a first-time arrival joins |
| `OIDC_LABEL` | the caption on the sign-in button |

## Subcommands

| Command | What it does |
| --- | --- |
| `takt serve` | runs: API, change stream, background jobs |
| `takt migrate` | applies migrations and exits |
| `takt doctor` | checks that the installation was done right |
| `takt version` | answers which version this is |
| `takt demo` | fills an empty database with demo data |

## Limits

| What | How much |
| --- | --- |
| People per installation | 130 |
| Boards open at once | 100 receive the change |
| Cards on a board | 500 without losing responsiveness |
| Rows in the table | 1000, sorting under 200 ms |
| First paint of the board | under a second |

## The integration API

The contract is published at `/api/v1/docs` on a running installation;
machine-readable at `/api/v1/openapi.json`. The version is in the path,
the reason for a refusal arrives as a code, and a repeat with the same
idempotency key is safe.

<!-- перевод: docs/справочник.md sha256:731fd9dbf237 -->
