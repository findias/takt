# How to

Answers to specific questions. Each one is short and stands on its
own — no need to read them in order.

If this is your first time here, walk through the [first fifteen
minutes](quickstart.md) first; the rest will make more sense.

**The interface speaks English and Russian;** the language is chosen
behind your name in the header. Buttons are named here as they appear
in the English interface. Screenshots still show the Russian one.

<!-- anchor: board -->
## Working on the board

### Find a card

Press `Ctrl+K` and start typing. The palette searches both cards and
board commands.

If the card is not found, check the filter: under a filter the board
shows less than everything, and says so in the column.

<!-- anchor: filter -->
### Filter down to what you need

1. Above the board press **Filter** and pick your conditions:
   assignee, labels, **Urgent**, **Due soon**, **Blocked**,
   **Longer than promised**, iteration. The button shows how many are on, and next
   to it you see how many cards are hidden and **Show all**.
2. To share the result, copy the page address — the filter lives in it.
3. To come back to a filter later, press **Save view** and give it a name.

Labels combine with AND: pick “Urgent” and “External” and you get cards
carrying both.

<!-- anchor: swimlanes -->
### Slice the board into swimlanes

In the **No grouping** list, choose what to slice
by: assignee, label, iteration or priority.

Swimlanes are a slice, not a different board: you cannot drag a card
between them, because the lane is derived from a property of the card.
To change the lane, change the property.

<!-- anchor: table -->
### Compare cards against each other

Press **Table** in the board header. Same set of cards as a
flat list: age, due date and estimate side by side.

Click a column heading to sort. The order lives in the address, so a
sorted list can be sent to someone.

### Put a label on a card

1. On the card press **+ label**, or open the card and find **Labels** on
   the **Work** tab.
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
block now, press **Lift the block**.

### Make a column wider

Drag the right edge of the column header. From the keyboard: focus
the edge with `Tab` and use the arrows. Double-click the edge to return
to the usual width.

The width is remembered in this browser, for you only: a colleague's
board stays as it was.

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
### Change your password or sign out

Both live behind your name as well. **Sign-in** tab: the
current password, the new one, **Change**. Changing the
password signs out every other device; to sign them out without
changing it, press **Sign out on all devices**. **Sign out** is at the bottom of the window.

Someone who signs in through the company identity provider changes the
password there, not here.

### Split work into parts

1. Open the card, **Work** tab.
2. In the subtasks section, type the name of the part into
   **What needs doing?**
3. Press **Subtask**. To give the part to another team, pick their board
   in the list next to the name first.

A part is an ordinary card: it can live on a different board if a
different team does the work. The parent card grows a "so many of so
many" bar.

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
3. On cards, pick the iteration (**Work** tab).
4. When the time is up press **Close iteration**. It asks first, because closing freezes what the
   iteration contains for good. A report appears: what made it and
   what did not.

<!-- anchor: team -->
## The organisation

### Invite someone

1. **Team** tab → **Invite** section.
2. Enter an e-mail, pick a role, press **Invite**.
3. Send the link to the person.

The link lasts a week, is bound to that address, and is shown once. If
it gets lost, revoke the invitation and create a new one.

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
