# Reference

Lists and tables: what lives where, what things are called, what equals
what. Not read in order — read when needed.

**The interface speaks English and Russian;** the language is chosen
behind your name in the header. Buttons are named here as they appear
in the English interface.

<!-- anchor: roles -->
## Roles in the organisation

| Role | Reads | Changes work | Runs the organisation |
| --- | --- | --- | --- |
| **Owner** | yes | yes | yes |
| **Member** | yes | yes | no |
| **Viewer** | yes | no | no |

Beyond roles, the owner appoints:

| Appointment | What it grants |
| --- | --- |
| Subdivision administrator | runs their own subtree: membership, nested nodes, boards |
| Area observer | reads everything in their subtree |

## Board visibility

| Visibility | Who sees it |
| --- | --- |
| **Whole organisation** | everyone in the organisation |
| **Own subdivision** | people in the subdivision that owns the board |
| **Listed people only** | those listed by name |

## Keys

| Keys | What it does |
| --- | --- |
| `Ctrl+K` | palette: search cards and commands |
| Arrows | move between cards |
| `Ctrl` + arrows | move a card |
| `Enter` | open a card |
| `E` | rename without opening |
| `Escape` | close a panel, a menu or the palette |
| `F1` or `?` | help on the screen you are on, in a new tab |
| Arrows on a column's edge | widen or narrow the column; double-click the edge to reset |

## Filters

| Filter | What it shows |
| --- | --- |
| Text | cards whose title contains the substring |
| Assignee | whose cards; separately, "nobody's" |
| Labels | cards carrying **all** the selected labels |
| **Urgent** | highest and high priority |
| **Due soon** | due today, tomorrow, the day after, or overdue |
| **Blocked** | those where work has stalled |
| **Block ending** | blocks that lift themselves within a day, or whose deadline has just passed; shown only on boards that have such blocks |
| **Longer than promised** | those running longer than the board's promise |
| Iteration | attached to an iteration; separately, "not in an iteration" |

## Blocks with a deadline

A block may be given a moment when it lifts itself — **Lifts itself (optional)**
when blocking, **Block deadline** on a blocked card. Leave it empty and
the block stays until someone lifts it; that is the normal case.

- The deadline is a moment, not a day, shown in your own time zone.
  A moment in the past is refused: to lift a block now, use
  **Lift the block**.
- The server checks every minute, and once at start-up. The block is
  closed at its deadline, not at the moment of the check, so time spent
  blocked does not depend on when the check ran.
- It appears in the card's history and in **Changes** as **block lifted by itself: the deadline passed**, without an author, and subscribers receive
  `card.block_expired`.
- Less than a day before the deadline, the line under the card becomes
  bold and dashed. Nothing is mailed: the board is where you see it.

## Colours

One colour, one meaning — so that red still means something on a busy
board.

| Colour | Where | Means |
| --- | --- | --- |
| Red | a block, an overdue commitment, an error, an irreversible action | work has stopped, or a promise is broken |
| Amber | past the board's promise, in the table and in the workload strip; a column over its WIP limit | worth a look; the work is still moving |
| Neutral | a block deadline | the block will lift itself — rather good news |

Actions follow the same rule: green adds something, grey removes
something that can be brought back, red cannot be undone and always
asks first.

## What is remembered in the browser

Theme, density and column widths are personal and stay in the browser
where they were set: the board of a colleague does not change, and on
another computer you start with the defaults.

## Groupings

None, by assignee, by label, by iteration, by priority.

## Labels

A label belongs to one place, and that place decides where it can be
hung. Every list that offers a label also says where it comes from.

| Where it is created | Where it can be hung | Who creates and archives it |
| --- | --- | --- |
| The organisation | on every board | anyone who can edit |
| A subdivision | on the boards of the subdivision and of all subdivisions inside it | its members, its administrators, the owner |
| A board | on that board only | whoever can edit the board |

- A label that does not exist yet can be created right from the card:
  type its name into the label picker (on the card, in the card panel,
  or in the bulk bar) and pick where it should apply — this board, its
  subdivision or a wider one, or the whole organisation. It is created
  and hung in one go, with a colour picked for you; change the colour on
  **Team**. Typing the name of an existing label offers that label,
  in any letter case, and typing the name of an archived one offers to
  bring it back instead of creating a second one.
- The same name cannot be used twice where the places overlap:
  an organisation label “Urgent” rules out “Urgent” on any subdivision
  or board. The refusal names where the label already is.
- A board label is visible only to those who can see the board.
- An archived label stays on the cards it already marks and still
  shows there; it can no longer be hung until it is brought back from
  the archive on **Team**.
- When a board is handed to another subdivision, or a subdivision is
  moved in the tree, labels already hung stay on the cards. If one no
  longer applies there, the card panel says so, and it can still be
  taken off.

<!-- anchor: flow -->
## Metrics in **Flow**

| Metric | What it answers |
| --- | --- |
| Cycle time | how many days work usually takes (percentiles) |
| Throughput | how many cards were finished, by week |
| In progress | how much work is running right now |
| Ageing | what is running and how long it has been running |
| Cumulative flow | three bands by day: queued, in progress, done |
| Forecast | how many days to finish a given number of cards |
| Discarded | how much was taken off the board unfinished |

<!-- anchor: key-scopes -->
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
block deadline changed, block lifted by its deadline, link added,
link removed, commented on, custom field filled, custom field cleared,
added to an iteration, removed from an iteration, work marked done,
mark removed, archived, restored to the board.

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
| `OIDC_LABEL` | the caption on the sign-in button; unset, it reads `Company account` or, in Russian, `Корпоративный аккаунт`, by the visitor's language |
| `DEMO` | `on` only for a public demo: sign-in offers a sandbox organisation with sample data for 24 hours; requires `SIGNUP=closed`, event subscriptions are switched off. Off by default |
| `STAND` | `staging` only for our own test stand of a branch: a strip on every screen and a note of the branch's commits; event subscriptions are switched off. Cannot be combined with `DEMO=on`. Off by default |

## Subcommands

| Command | What it does |
| --- | --- |
| `takt serve` | runs: API, change stream, background jobs |
| `takt migrate` | applies migrations and exits |
| `takt doctor` | checks that the installation was done right: schema, isolation, address, the connection to the database, who may create organisations, the provider, the change stream |
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
