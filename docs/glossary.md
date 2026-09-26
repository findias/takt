# Glossary

One concept, one word. These are the words the interface and this
documentation use, and what each of them means in Takt.

## Work on the board

| Word | What it means |
| --- | --- |
| Board | One team's work, moving through columns. A board has a key, a visibility and, if the team sets one, a promise |
| Board key | A short prefix of card numbers: `ПОСТ` gives `ПОСТ-14`. Set once, when the board is created: card numbers end up in correspondence and must not change |
| Card | One piece of work. Its number is the board key plus a sequence number |
| Column | A stage the work goes through: **Queue**, **In progress**, **Done** on a new board |
| Column markup | Two marks on the columns: **work starts** — from this column on, work counts as started; **finish** — here it counts as finished. Flow metrics are computed from the markup, not from dates filled in by hand |
| WIP limit | How many cards a column should hold. Going over it is highlighted, not forbidden: the limit exists to make overload visible |
| Hard limit | A limit that does refuse: a card is not moved into a full column, and the refusal says to make room first |
| Board promise | How many days work usually takes to cross the board, and with what probability — "usually 8 days with 85% probability". Cards running longer show up under **Longer than promised** |
| Block | A card that is waiting for something, always with a reason. A block may have a deadline, after which it lifts itself |
| Subtask | A part of a card. It is an ordinary card and may live on another team's board; the parent shows "so many of so many" |
| Epic, portfolio | An epic is a card on a portfolio board — a board of level **Epic portfolio**. Its parts, the features, live on team boards. Epics have their own flow and limits, and do not mix with tasks in metrics |
| Root, leaf | The top of a tree of subtasks — an epic, say — and the cards at its bottom that have no parts of their own. The progress bar of a card with grandchildren counts its leaves; **By epic** puts a task into its epic's lane |
| Board template | **Blank**, **Kanban**, **Scrum** or **Epic portfolio** when a board is created. It sets only the start and is not kept: afterwards the board is like any other |
| Label | A mark on a card. It belongs to one of three places — the organisation, a subdivision (with everything inside it) or one board — and is offered only there |
| Iteration | A sprint or a release: a name and dates. Closing it freezes what it contains for good and produces a report. A board can stop working in iterations and start again without losing any |
| Swimlanes | The board sliced into rows by assignee, label, iteration, priority, parent or epic. A slice of the same board, not another board |
| Filter | Conditions that hide the rest of the board. It lives in the page address, so it can be shared as a link |
| Saved view | A filter, a grouping and a layout with a name, to come back to |
| Archive | Where cards and boards go when taken away. Everything there can be restored; deleting for good is a separate action that says what will disappear |

## Flow metrics

| Word | What it means |
| --- | --- |
| Cycle time | How many days work takes from **work starts** to **finish**; shown as percentiles, not an average |
| Throughput | How many cards reach **finish** per week |
| Work in progress | How many cards are between **work starts** and **finish** right now |
| Age | How long a card that is still in progress has been going |
| Accumulation | Three bands by day — not started, in progress, done — to see where work piles up |
| Forecast | How many days finishing so many cards may take, as three percentiles: "if it goes on as it went" |
| Discarded | Cards taken off the board unfinished |

## The organisation

| Word | What it means |
| --- | --- |
| Organisation | Everyone who works in one Takt, and their boards. Organisations do not see each other |
| Subdivision | A department or a team in the organisation's tree. Boards and labels may belong to it |
| Owner | Reads, changes work and runs the organisation |
| Member | Reads and changes work |
| Viewer | Only reads |
| Subdivision owner | Runs their own subtree: who is in it, nested subdivisions, boards; invites people into it |
| Area observer | Reads everything in their subtree |
| Board visibility | Who sees a board: the whole organisation, its own subdivision, or listed people only |
| Integration key | Access for another system, limited to the scopes it was given; revoked in one action |
| Event subscription | An address that receives an event every time something happens to cards |
