# How to

Answers to specific questions. Each one is short and stands on its
own — no need to read them in order.

If this is your first time here, walk through the [first fifteen
minutes](quickstart.md) first; the rest will make more sense.

**The interface speaks English and Russian;** the language is chosen
behind your name in the header. Buttons are named here as they appear
in the English interface.

<!-- anchor: board -->
## Working on the board

### Find a card

Press `Ctrl+K` and start typing. The palette searches both cards and
board commands.

If the card is not found, check the filter: under a filter the board
shows less than everything, and says so in the column.

<!-- anchor: filter -->
### Filter down to what you need

1. Pick your conditions in the filter row above the board: assignee,
   labels, **Urgent**, **Due soon**, **Blocked**, **Longer than
   promised**, iteration. Next to them you see how many cards are hidden
   and **Show all**. On a narrow screen the row is folded under
   **Filter**, which shows how many conditions are on. The quick
   conditions, from **Urgent** to **Longer than promised**, are toggles:
   a pressed one is filled and ticked. **Find a card**, at the start of
   the second row, searches by title and number.
2. To share the result, copy the page address — the filter lives in it.
3. To come back to a filter later, press **Save view** and give it a name.

Labels combine with AND: pick “Urgent” and “External” and you get cards
carrying both.

<!-- anchor: swimlanes -->
### Slice the board into swimlanes

In the **No grouping** list, choose what to slice
by: assignee, label, iteration or priority.

**By parent** gives every parent its own lane with its subtasks — the
lane is titled with the parent's number, name and how much of it is
done, and opens the parent. **By epic** gives every epic of a portfolio
board its lane, however deep its tasks sit, and puts the rest in **No
epic**. A parent on another board still gets
a lane, and its title says whose board it is. Work with no parent sits
in **No parent**, which stays even when empty. In these two groupings
subtasks are shown in the columns rather than folded inside their
parent's card, since the lane is where they belong. The choice is kept
in a saved view like the filters.

Swimlanes are a slice, not a different board: you cannot drag a card
between them, because the lane is derived from a property of the card.
To change the lane, change the property.

<!-- anchor: portfolio -->
### Keep epics on a portfolio board

Epics live on a board of their own, not in the columns of a team.
Create a board with **Epic portfolio** in **How we work**: its columns
are "Idea", "In progress" and "Done", with a soft limit of 3 on "In
progress" and no iterations. Every card on it is an epic. Split an
epic into features on the team boards: open it, **Tasks** tab, pick the
team's board next to the name, **Subtask**.

A task the team created itself is hung under an epic from the task: open
it, **Tasks** tab, the **Epic** line at the top, **Choose an epic…**,
type the epic's number or part of its title and press it. The same line
shows the epic afterwards and **Remove** takes it off. From the epic's
side, **Link to an existing card** at the bottom of its **Tasks** tab,
kind **Subtask**, finds a task on any team board. Both searches look
through every board you can see. An epic is always the parent: a team's task cannot take
an epic as its subtask, and a subtask of a team's task cannot be created
on the portfolio. A task on a team board then shows the epic in its path to
the root, and the **Tree** view gathers the branch across boards.

An epic — or any card with subtasks — moved to the finish or marked
**Done** while some of its subtasks are still open asks first: it names
how many are not done and lists them. **Move anyway** or **Mark done
anyway** closes it, and the subtasks stay open; **Cancel** leaves it
where it was.

Each board counts its own flow, so epics never mix with tasks: the
portfolio shows how long epics take and how many run at once, the team
boards show their own work. On a team board every card that belongs to
an epic carries its coloured mark — the same colour on every board;
press it to show only that epic's work, and remove the epic chip from
the filter strip to show everything again. On the portfolio itself an
epic card shows, under its progress bar, where its work lies: one badge
per team board with how many of its tasks are done there, and how many
are stuck, for example "SUP 1/4 · 2 stuck". The badge opens that board
filtered by the epic. Moving from Jira? `takt-fetch` brings the epics as a portfolio board of
the package, and their issues keep them as parents. An existing board becomes a portfolio (and
back) under **How we work** in **Flow** — **Board level**. The board
list marks portfolios **epic portfolio**.

<!-- anchor: tree -->
### See epics, features and tasks as a tree

In the view list, next to **Board** and **Table**, pick **Tree**. Every
card with parts is shown with its parts under it, level by level: the
number, the column, how much is done — by the leaves of its whole
subtree — whether it is blocked, and whether it is done. A
parent on another board heads its own branch, signed with that board.
A branch that reaches other boards is followed there to the end: parts
on other boards show with their board and column, and so do their own
parts, however many boards the branch crosses — the portfolio tree
goes from the epic down to the team's tasks. The name of a card on
another board opens it on its own board. A card on a board you
cannot see is named "Card unavailable". Cards with no parent and no parts are folded into one line
at the bottom. **▾** folds a branch. Filters do not apply to this view:
a tree with half its branches missing cannot say how far the epic is.

<!-- anchor: table -->
### Compare cards against each other

Pick **Table** in the list at the left of the board's toolbar. Same set of cards as a
flat list: age, due date and estimate side by side.

Click a column heading to sort. The order lives in the address, so a
sorted list can be sent to someone.

### Put a label on a card

1. On the card press **+ label** (or the labels themselves, shown by
   name under the title, when it has some), or open the card and find
   **Labels** on the **Work** tab. A card shows three labels and the number
   of the rest.
2. Start typing. The list offers the labels that apply on this board;
   next to each one it says where it comes from — this board, a
   subdivision, or the whole organisation.
3. Pick one, or press `Enter`.

To label several cards at once, select them and use the bar that
appears at the bottom of the screen.

<!-- anchor: label-scope -->
### Create a label that does not exist yet

Right from the card: type the new name into the label picker. Under
the list the picker offers where the label should apply:

- **this board only** — nobody else will see it offered;
- **a subdivision** — on its boards and on those of everything inside it;
- **the whole organisation** — on every board.

Pick one and the label is created and hung in one go, with a colour
chosen for you. You are only offered the places you are allowed to
create labels in: a board you can edit, a subdivision you belong to or
run, and the organisation.

If a label with that name already exists where the places overlap, the
picker offers that label instead of a second one, and an archived one
is offered back from the archive. Colours, archiving and the full list
live on **Team**, in the **Labels** section.

To change a label's colour, pick another one in the list next to it
under **Labels** on **Team**. The colour changes on every card at once.
Whoever may archive the label may recolour it.

<!-- anchor: block -->
### Block work until a date

1. Open the card's **…** menu and choose **Block…**,
   or press **Block…** in the card panel.
2. Write what you are waiting for.
3. If you know when it ends, fill in **Lifts itself (optional)**: a date and a time.

At that moment the block lifts itself: nobody has to come back to the
card. The day before, the line under the card becomes bold, and the
filter **Block ending** gathers such cards. To move the
deadline, open the card and press **Move deadline**; to lift the
block now, press **Lift the block**. To fix the reason — a typo, or what
you wait for has changed — press **Edit the reason** next to it on the
**Work** tab: the block stays the same one, and the time it has lasted
is not split in two.

A card can also wait for another card rather than a date: a part that
holds up its parent, or a **Blocks** link. The board then shows
**Waits** on one and **Holds** on the other. Both go as soon as the card
being waited for is done — moved past the finish or marked **Done** —
and a block held by that card lifts itself; the history says so.

### Make a column wider

Drag the right edge of the column header. From the keyboard: focus
the edge with `Tab` and use the arrows. Double-click the edge to return
to the usual width.

The width is remembered in this browser, for you only: a colleague's
board stays as it was.

### Move a column

In the column header press **Markup**, then **← Left** or **Right →**
next to **Place**. The cards go with the column, and their history does
not change. The new order is the board's, for everyone. To put a
queue in sprint order instead, see **Order by iteration** under
[iterations](#iterations).

<!-- anchor: columns -->
### Mark up a column and set its limit

1. In the column header press **Markup**.
2. Tick **Work starts here** on the first column where work is really
   under way, and **Work ends here** on the column where it counts as
   finished. Cycle time, throughput and the forecast are counted from
   these two marks, not from dates typed in by hand.
3. Type a **Limit** — how many cards the column should hold at once.
4. Tick **Hard limit** if a full column must refuse another card.

Over an ordinary limit the counter in the header turns amber, and a card
can still be moved in: the limit is there to make overload visible.
A hard limit refuses the move and says to make room first. The field
below the limit holds the column's entry rule — what must be done before
a card comes here.

<!-- anchor: promise -->
### Promise how long work takes

Open **Flow** and find **Board promise**. Press **Take from history**:
the promise becomes the number of days in which most of the board's
finished work passed it. From then on, a card running longer than the
promise is marked right on the board, and the **Longer than promised**
filter gathers such cards. **Drop the promise** removes it.

<!-- anchor: appearance -->
### Switch to the dark theme or a denser board

1. Click your name in the header — it opens **Personal settings**.
2. **Appearance** tab: **System**, **Light**, **Dark**; below them
   **Compact**.

The change applies at once. Theme and density are remembered in this
browser: on another device they can be different.

<!-- anchor: notifications -->
### Hear when you are needed

The bell in the header counts what is unread. It rings when someone
calls you into a discussion, assigns you to a card or blocks a card you
work on, when a block on your card has less than a day to go or lifts
at its deadline, and when your card runs longer than the board's
promise — never about what you did yourself. Press the bell and pick an entry: the card
opens and the entry counts as read. **Mark all as read** clears the
counter.

To call someone into a discussion, press **@ Mention** under the reply
and pick the person. Only people who can see the board are offered.

To stop a kind of notification, click your name, **Notifications** tab,
and untick it. What is switched off never arrives.

<!-- anchor: language -->
### Switch the interface language

Click your name, **Language** tab, and choose **Русский** or
**English**. The page reloads in the chosen language.

The choice is stored with your account: the next time you sign in on
another computer or phone, the interface opens in the same language.
Until you choose, the language follows the browser.

### Get help on the screen you are on

Press **Help** in the header, `F1` or `?`. Help opens in a
new tab, at the section about the screen you are on — the board, a card,
the table, **Flow**, the archive, the team or the structure — and in the
language of the interface. It is this same documentation, built from the
same version as the application. The search field above the contents
finds a section by any word in it, and **What’s new** lists
what changed for people using the board in this version.


Next to some concepts — a column limit, the board's promise, a block,
the flow metrics, roles, key scopes — there is a round **?**. Press it
for two or three sentences on what the concept is, with a link to the
section of help that explains it in full. `Escape` closes it.
### Change your email, your password, or sign out

All of it lives behind your name as well, on the **Sign-in** tab.

Your email is the name you sign in with. Under **Email**, enter the
new address and your password and press **Change**; from then on you
sign in with the new address. takt sends no emails, so the new address
is not confirmed by a letter. For that reason it does not link a
company identity provider sign-in to your account; if your organisation
signs in that way, ask the owner to set the address instead. When the
email is managed by the identity provider or the company directory, the
tab says so, and the address is changed there.

Under **Password**: the
current password, the new one, **Change**. Changing the
password signs out every other device; to sign them out without
changing it, press **Sign out on all devices**. **Sign out** is at the bottom of the window.

Someone who signs in through the company identity provider changes the
password there, not here.

### Split work into parts

1. Open the card, **Tasks** tab.
2. In the subtasks section, type the name of the part into
   **What needs doing?**
3. Press **Subtask**. To give the part to another team, pick their board
   in the list next to the name first.

To fix a part's name, press **Rename** next to it in the list; a part on
another team's board is renamed by opening it. Any open card is renamed
with the pencil next to its title in the panel.

A part is an ordinary card: it can live on a different board if a
different team does the work. The parent card grows a "so many of so
many" bar.

Parts can have parts of their own — an epic, its features, the tasks
under them, up to five levels. The bar on a card with grandchildren
counts the leaves of the whole tree — the tasks at the bottom, "1 of 6";
its direct parts ("0 of 3" features) are in the bar's tooltip and next
to it in the card panel. A middle card is not counted in the bar, or a
feature and its tasks would count twice; the
weight, when every leaf is estimated, is the sum of the leaves'
estimates. A blocked part at any depth stops the top card too: the
epic shows "Part blocked" when a task two levels down is stuck. A
blocked parent does not stop its parts — they are what people carry on
with.

An open part shows its path to the root above the number: "Move to the
new warehouse · Platform › Ship the warehouse release". A link on this
board opens that card here; a link on another board is signed with the
board and opens it there. A parent on a board you cannot see is named
"Unavailable card" rather than dropped, so the path is never shorter
than it is; its name and board stay closed. A path longer than three
links folds its middle into "…", and the whole chain is in the tooltip.

### Tie a card to service-desk tickets

1. Open the card, **Tasks** tab.
2. Under **Tickets**, pick the kind — **RDS**, **Service request**,
   **Change request** or **Problem** — and type the ticket number or
   paste its address.
3. Press **Add**.

A card holds as many tickets as it needs: work often answers three
service requests and ships with one change request. The list is
ordered by kind; an address opens in a new browser tab, a number stays
text. **Remove** takes a ticket off. Both show on the card's
**History** tab. Below the tickets, **Tasks** holds the parent, the
subtasks and the links to other cards.

<!-- anchor: archive -->
### Take a card off the board

Hover the card, open the **…** menu and choose **Archive**. A message appears with a **Restore** button, in
case that was the wrong card.

Archived cards sit in **Archive** and come back from there at any time.
Deleting for good asks for confirmation and cannot be undone.

<!-- anchor: iterations -->
### Run a sprint or a release

1. In the strip above the board press **+ iteration**.
2. Set a name, a start and an end, press **Create iteration**.
3. On cards, pick the iteration (**Work** tab). While the board is
   filtered by an iteration, a new card goes straight into it. The card
   then shows the iteration with its end — "Week 40 · until 4 Oct" — and
   that end is its deadline; the **Commitment** field stays for promises
   made outside. Once the iteration has ended, a card that is not done
   shows it in red.
4. The iteration closes itself at the midnight after its last day, and
   its report — what made it and what did not — is counted as of that
   moment. To close it earlier press **Close iteration**. It asks first,
   because closing freezes what the iteration contains for good, and the
   button stays inactive until you type the iteration's name — a stray
   click cannot close a sprint. An iteration created after its end, to
   record the past, does not close itself: close it once its cards are
   in.
5. Move what was not finished into the next iteration: all at once with
   the move-unfinished button in the closed iteration's report, or
   one card at a time with **Move to** on its **Work** tab. The closed
   iteration's report stays as it was — the moved cards remain in it as
   not done.

To put a queue in sprint order, open **Markup** on the column and press
**Order by iteration**: cards of earlier iterations go on top, cards
without an iteration to the end, and within one iteration the order
stays as it was. It happens once — after that you move cards by hand as
before.

A team that does not work in iterations opens **Flow** and clears **We
work in iterations** under **How we work**: iterations disappear from
the filter, the grouping, the cards, the table and the card panel.
Nothing is deleted, so nothing is asked. To bring them back, press
**Work in iterations** where the iteration strip used to be, or tick
the box again — everything returns, reports included.

<!-- anchor: import -->
## Moving in

### Import tasks from a spreadsheet

Tasks from an Excel workbook (`.xlsx`), Google Sheets or another
tracker's CSV export (Jira, YouGile, Kaiten and others can all save
one) become cards on a board.

1. **Boards** tab → **Import tasks from a spreadsheet…** under the
   form for a new board.
2. Pick the file — CSV or `.xlsx`. For CSV the encoding and the
   separator are guessed: Excel saves CSV with semicolons and in
   Windows-1251, and that is fine. For a workbook the first sheet that
   holds a table is taken (a cover or summary sheet in front is
   skipped), and **Workbook sheet** picks another; a report title above
   the table is not taken for its headers.
3. Check the file columns. The server suggests which card field each
   one goes to; change what is wrong. Only the title is required; the
   rest — board column, assignees, labels, estimate, priority, due
   date, created and finished dates, description, key in the old
   system — is optional. **Don’t import** leaves a column out.
   Any column of the file can be the **Board column**: a status, a
   section, a group — in a Notion export pick the property that holds
   the stage, in a monday one either the Status column or the group.
4. Choose where to: **To a new board** (named after the file; change
   the name if you like) or **To an existing board**. For an existing
   board, **Board columns** lists every value of the file's board column
   and where it goes: same-named ones are found on their own, an
   unknown one creates a new column unless you pick one of ours —
   “In Review” from Jira and our “In progress” are the same thing, but
   no name tells that.
5. Read **What will happen** and press the button under it: it names
   the number of cards that will come.

Nothing is written until the last step: the preview does the whole
import and throws it away, so it shows exactly what you will get.

What the preview names, so that nothing is lost silently:

- **People.** Every assignee and author of the source is listed with
  the number of their cards, and for each you choose what to do:
  - **match with a member** — pick the person from the list. Someone
    with the same email is picked already («found by email»);
  - **create an account** — owner only. The email comes from the
    source; when there is none, type it in. After the import each new
    person gets a one-time sign-in link, shown once, to pass on
    yourself; with corporate sign-in no link is needed, the first
    sign-in links the account by email. An address that already belongs
    to an account outside the organisation cannot be created — invite
    that person instead;
  - **leave out** — their cards get a label with their name (their
    email, if the spreadsheet gives no name), so the work is not lost.
    The label is technical: drawn as an outline, not offered when
    labelling by hand, found by the board filter. Two namesakes get two
    labels.

  The choice is remembered for the next import of the same source
  («chosen at the previous import»). Import the same board again after
  adding people, and the second run adds them as assignees to the cards
  that already came — without bringing those cards twice — and removes
  their labels; the button then reads **Update the imported cards**.
- **Row problems.** A row without a title is not imported; a value
  that cannot be read (an estimate that is not a number, an unknown
  priority, a date that is not a date) is dropped, and the rest of the
  row comes. Rows are numbered as in the file, the way Excel shows
  them, empty rows included.
- **How dates were read.** A date column is read as a whole in one of
  four forms: `2026-09-22`, `22.09.2026`, Jira's `22/Sep/26`, or an
  Excel date cell.
- **Columns.** A new board gets the columns from the file, put in order
  of meaning: queue, work, unrecognised stages, done. “In progress”
  becomes the start of work and “Done” the finish; if there is no done
  column, one is added. On an existing board missing columns are added
  before the done column, and its markup is left alone.
- **Labels.** Labels are found by name among those that apply to the
  board; missing ones are created as labels of this board.

Importing the same file again creates no duplicates: every card
remembers its key in the old system (the **Key in the old system**
column, or the title if there is none), and a second run skips what
has already come.

History: dates come across, moves between columns do not — the old
system had its own columns, and its moves passed off as ours would give
a metric you cannot trust. The start of work there is not known either,
so it is taken as the created date: an imported card's cycle time is
closer to its whole lead time. Assignment by import sends no
notifications.

Imported cards are marked, so they can be told apart from work lived
on the board:

- the card's history says «перенесена из таблицы» and, if the file had
  one, its key in the old system;
- **Flow** says how many cards of the report were imported, draws
  them as hollow dots on the cycle-time chart, marks them in the aging
  list, and **Count without imported cards** recalculates every figure
  — cycle time, throughput, the forecast, the cumulative flow — without
  them.

A sample spreadsheet is on the same screen (**Download a sample
spreadsheet**); its columns are recognised without any changes. One
import takes up to 10,000 rows and 5 MB and runs in seconds; split
a bigger file. Some exports stop at a fixed number of rows without
saying so — a cloud Jira CSV at 1,000 issues, a monday Excel file at
10,000 items. A file of exactly that length is named under **Not
imported**: check that everything came, and export the rest as a second
file.

<!-- anchor: import-yougile -->
### Import a board from YouGile

A YouGile board comes over whole through its API: columns, tasks,
assignees, deadlines, dates, stickers and checklists.

1. **Boards** tab → **Import tasks from a spreadsheet…** → **YouGile**.
2. Enter the email and password you sign in to YouGile with, press
   **Find companies**, pick the company and press **Get the key**. The
   password is used once, to get the company's API key; if the company
   has no key yet, one is created in YouGile and the screen says so —
   delete it there once the import is done. If you already have a key,
   **I have an API key** takes it instead. Neither the password nor the
   key is stored.
3. Pick the **YouGile board**, then where to — a new board or an
   existing one, exactly as for a spreadsheet — and read **What will
   happen**.

What comes across: every task that is not in YouGile's archive, into
the column of the same name; assignees by email; the deadline as the
due date; the created and finished dates; stickers as labels named
“Sticker: value”, except a sticker called Priority, whose values
become the card's priority; checklists at the end of the description.
Subtasks become parts of their card, including those that sit in no
column (they go to the parent's column). What does not come, the
preview lists under **Not imported**: tasks in YouGile's archive,
files, access rights and time tracking. Importing the same board again
skips what has already come.

Task chats and task history come after the cards, in the background:
YouGile answers at most 50 requests a minute and each task takes two,
so a board of 800 tasks takes about half an hour. The preview says how
long; after the import the screen shows how far it has got, and you
can leave it — the work runs on the server. Replies become the card's
discussion; YouGile's history (who moved the task, who was assigned)
appears on the card's **History** tab under **Before the import, in
YouGile**, with its own dates and authors, apart from what happened
here. If the server restarts midway, import the same board into the
same board again: it picks up the cards still waiting.

The server has to reach YouGile. In a closed network it cannot: export
a table in YouGile (Reports → Tables) and import it as a spreadsheet.
An administrator can switch import over the API off altogether with
`YOUGILE_URL=off`; for a boxed YouGile, `YOUGILE_URL` holds its address.

<!-- anchor: import-package -->
### Import a package from a closed network

A package (`.takt`) carries boards from YouGile, Jira, Trello and other
trackers into a closed network, together with what a spreadsheet
loses: subtasks, links between cards and discussions. It is built
outside, where there is internet, and carried in as a file.

1. **Boards** tab → **Import tasks from a spreadsheet…** → **Import
   package**, and pick the file.
2. The screen says where the package came from and who built it. If it
   carries several boards, pick one in **Package board**: boards are
   imported one at a time, each with its own preview.
3. Choose where to and read **What will happen**: besides cards it
   counts subtasks, links and discussion replies. A person the source
   gave no e-mail for (Trello gives e-mails only to an administrator)
   is listed by name.
4. Import. **Import the next board of the package** takes the same file
   to its next board.

A reply by someone who is not in the organisation comes on behalf of
the person importing and starts with the author's name — “from YouGile:
Ivan Petrov”, or “from YouGile: unknown author” when YouGile no longer
lists the person (someone removed from the company). A damaged package is refused as a whole and says which
part did not match. Packages over 50 MB are imported by an
administrator on the server; the format is described in
[Import package](import-package.md).

<!-- anchor: team -->
## The organisation

<!-- anchor: tasks -->
### See someone's tasks on every board

The **Tasks** tab lists the cards where a person is an assignee, on
every board you can see — your own by default. Pick someone else in
**Whose tasks**, or press **Tasks** next to their name under **Team**.
The list is a table: number, task, board, column, due date, priority,
labels and how long it has been in progress; the task opens its card on
its board. **Show finished** adds finished work. The row below narrows
the list: **Status** (not started, in progress, done — by the kind of
column, since boards name their columns differently — or blocked),
**Due** (overdue, within 3 days, no due date) and **Label** (only the
labels these tasks carry). **Clear filters** brings everything back. A private board you
have no access to is not shown, even if the person works on it. The
address keeps the choice, so the list can be sent to someone.

<!-- anchor: reports -->
### Export cards for a report

When someone asks you to "send a spreadsheet", open the **Reports**
tab. Choose the **Period** — two dates, or **30 days**, **90 days**,
**This quarter**, **Last quarter** — and narrow it down if you need to:
**Boards**, **Subdivisions** (a subdivision includes the ones nested in
it), **Assignees**, **Labels**, **Priority**, **State**, and an
**Iteration** once a single board is chosen. Empty means "any". A card
is included if it was alive during the period: created before the
period ended and not finished before it began. Its state and age are as
of today.

Below the selection you see how many cards match. Then download:

- **Excel (XLSX)** opens on a **Summary** sheet: what was selected,
  cards done and discarded in the period, work in progress, cycle time
  and age (median and 85th percentile), throughput by week and a
  cumulative flow diagram — both with charts — iteration completion and
  a breakdown by subdivision. The **Data** sheet has one row per card
  with every field, ready for pivot tables: among them **Tickets** — the
  card's RDS, service requests, change requests and problems in one
  cell, `RDS 12345; Problem PRB-7` — and, last, the whole
  **Description**. Everything on the summary is counted from those
  same rows.
- **CSV** holds the same rows without the summary.
- **JSON** is for a program: field names as in the API, values as
  codes (`done`, `high`).

Column names follow the interface language. Times are in UTC. Only
boards you can see are included. One export takes at most 50,000 cards;
if more match, the screen says how many and asks you to shorten the
period or pick boards. The address keeps the selection, so the link can
be sent to someone. The same export is open to integration keys with
`boards:read`: `GET /api/v1/reports/cards`.

A selection you repeat — "closed last quarter", "where work gets
stuck" — is saved under a name with **Save selection as a slice**; it
then sits under **Slices** and opens with one click. A ready-made
period is kept as a word, not as dates: "Last quarter" opened in January
gives October to December. Slices are your own; others do not see
them — send the link instead. **×** removes a slice, and **Restore** in
the message brings it back.

### Invite someone

1. **Team** tab → **Invite** section.
2. Enter an e-mail, pick a role, press **Invite**.
3. Send the link to the person.

The link lasts a week, is bound to that address, and is shown once. If
it gets lost, revoke the invitation and create a new one.

### Correct someone's email

An owner can change a member's email: a typo, an old address, someone
added under the wrong one. **Team** tab → **Email…** next to the
person → the new address → **Change email**. They sign in with the new
address from then on; takt sends no emails, so tell them yourself.

**Email…** is offered only where the owner may use it. Your own email
is changed behind your name. Someone who is also a member of another
organisation changes their email themselves: it is their sign-in name
in both. When the email is managed by the company identity provider or
directory, it is changed there. The change shows in the audit log as
«email: old → new».

### Give someone a sign-in link

takt sends no emails, so there is no «forgot password» letter and no
«your account is ready» letter. Instead the owner issues a link.

1. **Team** tab → **Sign-in link** next to the person.
2. Copy the link shown under the list and pass it on yourself.
3. The person opens it, chooses a password and is signed in at once.

The link works once and for a week, and is shown once. A new link
cancels the previous one; using it signs the person out everywhere else,
as a password change does. Someone the import created has not set a
password yet and is marked **not signed in yet** under **Team** — they
need such a link. It is offered for the same people as **Email…**: not
yourself, not someone who is also a member of another organisation,
not someone who signs in through the company identity provider.

<!-- anchor: visibility -->
### Close a board to outsiders

1. Open the board and press the visibility button in the header — it
   starts with `Видна:` (visible to).
2. Choose the visibility:
   - **Whole organisation** — everyone in the organisation;
   - **Own subdivision** — people in the subdivision that owns it;
   - **Listed people only** — only those listed by name.
3. For the last one, add people to the list.

Closing a board adds you to it: otherwise your very first action would
lock you out.

### Handle someone leaving

Pick one of two; they do different things:

- **Remove** — the person is no longer in the
  organisation. Their cards, comments and audit entries stay.
- **Erase data** — name and e-mail are erased, the
  traces of their work remain unnamed. Irreversible, and asked about
  separately.

If someone left but the work must stay traceable, the first is enough.

<!-- anchor: structure -->
### Set up subdivisions

1. The **Structure** tab.
2. Press **New subdivision**, enter a name.
3. Everything else about a subdivision is in the **⋮** menu at the end
   of its row: **Create a department…**,
   **Rename…**, **Move…**, **Remove subdivision**. A question opens a
   field right under the row.
4. Appoint a subdivision administrator in **Who runs what** — they will run their own subtree.

A removed subdivision is not gone: it waits under **Removed subdivisions** and comes back with
**Restore**.

## Integrations

### Issue a key for an integration

1. **Team** tab → **Integration keys**.
2. State what the key is for and pick its scopes.
3. Press **Create** and copy the key — it is shown once.

For the directory (SCIM), issue a **separate** key: such a key works
only against `/scim/v2` and gives no access to boards.

**Revoke key** asks first: the key stops working at once,
and nothing brings it back — you issue a new one.

<!-- anchor: subscriptions -->
### Receive events in your own system

1. **Team** tab → **Event subscriptions**.
2. Enter a name and the receiver's address, tick the events.
3. Press **Create** and keep the signing key — we sign every
   delivery with it.

If the receiver goes down for maintenance, press **Pause**: while paused, events do not pile up, so resuming does not turn
into an avalanche.

### Work out why an event did not arrive

1. On the subscription press **Deliveries**.
2. Look at the attempts and the receiver's response.
3. Once the receiver is fixed, press **Retry** on what
   did not arrive.

A retry also re-enables the subscription if we disabled it after a long
run of failures.

<!-- anchor: export -->
### Take all the organisation's data

**Team** tab → **Export** → **Download file**.

Tick **Include the audit log** if you need
it too: the log is usually larger than everything else combined.

## Installing and operating

Details are in the [installation guide](install.md).

### Check that the installation was done right

```sh
kubectl exec deploy/takt -- /app/takt doctor
```

It answers as a list: right schema, right address, identity provider
reachable, database notifications arriving.

### Upgrade an installation

```sh
docker load < takt-image.tar.gz
helm upgrade takt takt-*.tgz --reuse-values --set image.tag=<version>
kubectl exec deploy/takt -- /app/takt doctor
```

`--reuse-values` is mandatory: without it the settings revert to the
chart defaults.

### Close registration

Set `signup=closed` at install or upgrade time. Then only the owner
creates organisations, and the "create a new organisation" button
disappears from the sign-in screen.
