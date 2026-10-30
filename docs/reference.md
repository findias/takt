# Reference

Lists and tables: what lives where, what things are called, what equals
what. Not read in order — read when needed.

**The interface speaks English and Russian;** the language is chosen
behind your name in the header. Buttons are named here as they appear
in the English interface.

<a id="roles"></a>
## Roles in the organisation

| Role | Reads | Changes work | Runs the organisation |
| --- | --- | --- | --- |
| **Owner** | yes | yes | yes |
| **Member** | yes | yes | no |
| **Viewer** | yes | no | no |

Every role, the viewer included, suggests ideas on the **Ideas** tab and
supports others' — an idea is not work. What becomes of an idea is decided
by the owner, and by a subdivision owner for the ideas addressed to their
subtree.

Beyond roles, the owner appoints (and a subdivision owner, below their own node):

| Appointment | What it grants |
| --- | --- |
| Subdivision owner | runs their own subtree: membership, nested nodes, boards; invites people into it as members or viewers; appoints and removes owners of the nodes below their own; deletes its boards and cards for good and erases people who belong only to it; reads the audit log of its subtree |
| Area observer | reads everything in their subtree |

<a id="board-visibility"></a>
## Board visibility

| Visibility | Who sees it |
| --- | --- |
| **Whole organisation** | everyone in the organisation |
| **Own subdivision** | people in the subdivision that owns the board |
| **Listed people only** | those listed by name |

Only someone who can see a board can be assigned to its cards: the
assignee picker offers just them, and the server refuses anyone else.
A subtask on a neighbouring team's board is assigned from that board's
people, or left unassigned. Someone who was assigned before losing
access — taken out of the board's subdivision, or the board closed or
handed to another subdivision — stays on the card and can still be
removed from it. Their circle on the card has a dashed ring, and the
card panel says **cannot see the board** next to the name: such a
person no longer gets notifications about the card. Someone removed
from the organisation keeps their name on the card and is marked
**no longer in the organisation**.

The same rule holds for **@ Mention** in a discussion: only people who
can see the board can be called in.

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
| Arrows on the card panel's left edge | widen or narrow the panel; double-click the edge to reset |

## Filters

| Filter | What it shows |
| --- | --- |
| Text | cards whose title contains the substring |
| Assignee | whose cards; separately, "nobody's". The list holds those who can see the board and those already assigned on its cards, each with how many open cards they have; it is ordered **By name**, **By cards** or **By estimate**, and typing finds a person |
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

## Iterations that close themselves

- An iteration closes itself at the midnight after its last day, by
  the database clock. The same server check runs every minute; the
  moment of closing is the midnight, not the check, so the report does
  not depend on when the check ran.
- Cards not done stay in the closed iteration as not done. Move them
  with the move-unfinished button in its report or one by one from the
  card; the closed iteration's report does not change.
- An iteration created after its end is not closed by itself: its cards
  are added after it is created, and closing it at a midnight before
  that would give an empty report.

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

Theme, density, column widths and the card panel's width are personal
and stay in the browser where they were set: the board of a colleague
does not change, and on another computer you start with the defaults.

The browser also remembers when you last opened each board: that is
what "changed while you were away" is counted from. The server does not
keep it. On another computer, or in a private window, the last day
counts.

## Groupings

None, by assignee, by label, by iteration, by priority, by parent, by epic.

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

<a id="flow"></a>
## Metrics in **Flow**

| Metric | What it answers |
| --- | --- |
| Cycle time | how many days work usually takes: half of the cards fit in the first number, 85 of 100 in the second |
| Throughput | how many cards were finished, by week — and how many were created in the same week |
| In progress | how much work is running right now |
| Ageing | what is running and how long it has been running |
| Cumulative flow | three bands by day: queued, in progress, done |
| Forecast | how many days to finish a given number of cards |
| Discarded | how much was taken off the board unfinished |

Every board counts its own metrics. Epics live on a portfolio board, so
their months never mix with a team's days.

<a id="reports-numbers"></a>
## Numbers on **Reports**

Every block on **Reports** has a "?" beside its title with the same
explanation in a few sentences. Here they are together, block by block.
All of them count the cards that match the selection above.

<a id="reports-attention"></a>
### Needs attention now

What keeps work from moving as of today. The period does not apply
here; the selection does. A card with two problems is in both lists.
Click a card and it opens at the side, so the problem can be fixed in
place.

| List | What is in it |
| --- | --- |
| Blocked | cards with a block on them, and for how many days, with the reason |
| Running longer than promised | started cards that have been running longer than the board's promise: the time work usually takes to cross the board |
| Overdue | cards whose due date has passed and which are not done |
| In progress without an assignee | started cards with nobody on them |
| No movement | cards sitting in one column longer than the board's **No movement** setting on Flow, 7 days by default |

<a id="reports-summary"></a>
### Summary for the period

| Number | What it means |
| --- | --- |
| Done | cards finished within the period |
| Created | cards created within the period |
| Discarded | cards taken off the board unfinished within the period |
| In progress | started and not finished, as of today |
| was …, +… | the same number for the previous period of the same length, right before this one, and the difference |
| Cycle time | how many days a card takes from the column where work starts to the column where it ends. "Usually" is the number half of the finished cards fit in; "almost always" is the number 85 of 100 fit in. There is no average on purpose: a few long cards would skew it |
| How long started work has been running | the same two numbers for cards that are started and not finished: half of them have been running less than the first, 85 of 100 less than the second. This is what is stuck right now |

<a id="reports-due"></a>
### Due dates

Only cards with a due date count. The share done on time is those
done in the period no later than their due date; the "was" beside it
is the same share for the previous period. The number overdue now is
how many cards have passed their date and are not done, as of today.

### Done and created by week

Dark bars are cards finished in that week, light bars are cards
created in the same week. Light bars higher than dark ones week after
week mean more work comes in than goes out, and the queue grows.

<a id="reports-blocks"></a>
### Blocks in the period

How many cards stood blocked in the period and how many days that
took, in total and per card. Days are counted within the period and
summed over every block of a card; a block that is still open counts
up to today. The table by reason shows reasons as they were written on
the cards.

### How work piles up

Three bands by day: queued, in progress, done. A band of work in
progress that keeps getting thicker means more is started than
finished.

<a id="reports-iterations"></a>
### Iterations: promised and done

Iterations that overlap the period, as of their closing; a running one
as of today.

| Column | What it means |
| --- | --- |
| Promised | cards the iteration held on its first day |
| Added on the way | cards put into the iteration after it started |
| Taken out | cards removed from the iteration before it closed |
| Done | cards finished in the iteration, including those added on the way |
| Done out of promised | done cards among the promised ones, as a share of the promise; what was added on the way does not change it. Row after row shows whether the team keeps its word |

<a id="reports-effort"></a>
### Where the effort went

Cards done in the period, laid out by label and by epic.

| Column | What it means |
| --- | --- |
| Whose | the label's scope: organisation-wide, a subdivision's or a board's |
| Done | cards done with this label or under this epic |
| Share | out of everything done in the period. A card with two labels counts for both, so shares may add up to more than a hundred |
| Sum of estimates | the estimates of those cards added up, in the unit the boards use; "(3 unestimated)" is how many had no estimate. Shown only when every selected board estimates in the same unit |

The epic is the nearest parent on a portfolio board; epics themselves
are not in the table.

<a id="reports-people"></a>
### By subdivision and by assignee

The subdivision is the one that runs the card's board. A card with
two assignees counts for both; cards with nobody on them are a row of
their own. Click a name and the selection, and the file, become that
person's.

| Column | What it means |
| --- | --- |
| Cards | cards that match the selection |
| Done | finished within the period |
| In progress | started and not finished, as of today |
| Cycle time, usually | the number half of the finished cards fit in, in days |
| Cycle time, almost always | the number 85 of 100 fit in |
| Pure work, usually | how long the card stood in work columns, without waiting in a queue, for half of the cards; the gap to cycle time is waiting |

<a id="key-scopes"></a>
## Integration key scopes

| Scope | What it opens |
| --- | --- |
| `boards:read` | reading boards, cards and ideas |
| `boards:write` | changing boards and cards; submitting ideas |
| `structure:read` | reading the subdivision structure |
| `audit:read` | reading the audit log |
| `scim:write` | provisioning people and groups from a directory (separate key only) |

## Subscription events

Card: created, renamed, description changed, estimate changed,
priority changed, due date set or cleared, moved, blocked, unblocked,
block deadline changed, block lifted by its deadline, link added,
link removed, commented on, file attached, file removed, custom field
filled, custom field cleared,
added to an iteration, removed from an iteration, work marked done,
mark removed, completion changed, epic status set, archived, restored to the board.

Idea: submitted (`idea.created`), decided (`idea.decided`) — the body
carries the idea with its supporters and, for the second, the decision.
Events reach the receiver whoever caused them: a member, a viewer, an
integration key or a background job.

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
| `DEMO` | `on` only for a public demo: sign-in offers a sandbox organisation with sample data for 24 hours, in the language chosen on the sign-in screen (English unless the visitor picks another); requires `SIGNUP=closed`, event subscriptions are switched off. Off by default |
| `STAND` | `staging` only for our own test stand of a branch: a strip on every screen and a note of the branch's commits; English unless the visitor picks another, as in the demo; event subscriptions are switched off. Cannot be combined with `DEMO=on`. Off by default |

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
| Cards in one export (**Reports**) | 50,000; the server keeps them out of memory |

## The integration API

The contract is published at `/api/v1/docs` on a running installation;
machine-readable at `/api/v1/openapi.json`. The version is in the path,
the reason for a refusal arrives as a code, and a repeat with the same
idempotency key is safe.
