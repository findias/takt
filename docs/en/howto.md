# How to

Answers to specific questions. Each one is short and stands on its
own — no need to read them in order.

If this is your first time here, walk through the [first fifteen
minutes](quickstart.md) first; the rest will make more sense.

**The interface is in Russian.** Buttons are quoted the way they appear
on screen, with a translation next to them.

## Working on the board

### Find a card

Press `Ctrl+K` and start typing. The palette searches both cards and
board commands.

If the card is not found, check the filter: under a filter the board
shows less than everything, and says so in the column.

### Filter down to what you need

1. In the row above the board pick your conditions: assignee, labels,
   «горит» (urgent), «срок подходит» (due soon), «заблокированные»
   (blocked), «дольше обещанного» (past the promise), iteration.
2. To share the result, copy the page address — the filter lives in it.
3. To come back to a filter later, press **«Сохранить вид»** (save
   view) and give it a name.

Labels combine with AND: pick «срочно» and «снаружи» and you get cards
carrying both.

### Slice the board into swimlanes

In the **«Без группировки»** (no grouping) list, choose what to slice
by: assignee, label, iteration or priority.

Swimlanes are a slice, not a different board: you cannot drag a card
between them, because the lane is derived from a property of the card.
To change the lane, change the property.

### Compare cards against each other

Press **«Таблица»** (table) in the board header. Same set of cards as a
flat list: age, due date and estimate side by side.

Click a column heading to sort. The order lives in the address, so a
sorted list can be sent to someone.

### Split work into parts

1. Open the card, **«Работа»** (work) tab.
2. In the subtasks section press **«Добавить»** (add).
3. Type the name of the part.

A part is an ordinary card: it can live on a different board if a
different team does the work. The parent card grows a "so many of so
many" bar.

### Take a card off the board

Hover the card, open the **«…»** menu and choose **«В архив»** (to the
archive). A message appears with a **«Вернуть»** (restore) button, in
case that was the wrong card.

Archived cards sit in **«Архив»** and come back from there at any time.
Deleting for good asks for confirmation and cannot be undone.

### Run a sprint or a release

1. In the strip above the board press **«+ итерация»** (+ iteration).
2. Set a name, a start and an end.
3. On cards, pick the iteration (**«Работа»** tab).
4. When the time is up press **«закрыть»** (close) — a report appears:
   what made it and what did not.

## The organisation

### Invite someone

1. **«Команда»** tab → **«Пригласить»** section.
2. Enter an e-mail, pick a role, press **«Пригласить»**.
3. Send the link to the person.

The link lasts a week, is bound to that address, and is shown once. If
it gets lost, revoke the invitation and create a new one.

### Close a board to outsiders

1. Open the board and press **«Доступ»** (access) in the header.
2. Choose the visibility:
   - **«Всей организации»** — everyone in the organisation;
   - **«Своей команде»** — people in the subdivision that owns it;
   - **«Только вписанным»** — only those listed by name.
3. For the last one, add people to the list.

Closing a board adds you to it: otherwise your very first action would
lock you out.

### Handle someone leaving

Pick one of two; they do different things:

- **«Исключить»** (remove) — the person is no longer in the
  organisation. Their cards, comments and audit entries stay.
- **«Удалить данные»** (erase data) — name and e-mail are erased, the
  traces of their work remain unnamed. Irreversible, and asked about
  separately.

If someone left but the work must stay traceable, the first is enough.

### Set up subdivisions

1. The **«Структура»** (structure) tab.
2. Press **«Завести подразделение»** (create a subdivision), enter a
   name.
3. To nest one inside another, create the child inside the node.
4. Appoint a subdivision administrator — they will run their own
   subtree.

## Integrations

### Issue a key for an integration

1. **«Команда»** tab → **«Ключи для интеграций»** (integration keys).
2. State what the key is for and pick its scopes.
3. Press **«Завести»** and copy the key — it is shown once.

For the directory (SCIM), issue a **separate** key: such a key works
only against `/scim/v2` and gives no access to boards.

### Receive events in your own system

1. **«Команда»** tab → **«Подписки на события»** (event subscriptions).
2. Enter a name and the receiver's address, tick the events.
3. Press **«Завести»** and keep the signing key — we sign every
   delivery with it.

If the receiver goes down for maintenance, press **«Приостановить»**
(pause): while paused, events do not pile up, so resuming does not turn
into an avalanche.

### Work out why an event did not arrive

1. On the subscription press **«Доставки»** (deliveries).
2. Look at the attempts and the receiver's response.
3. Once the receiver is fixed, press **«Повторить»** (retry) on what
   did not arrive.

A retry also re-enables the subscription if we disabled it after a long
run of failures.

### Take all the organisation's data

**«Команда»** tab → **«Выгрузка»** (export) → **«Скачать файл»**
(download).

Tick **«Добавить журнал действий»** (include the audit log) if you need
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

<!-- перевод: docs/как.md sha256:08dc05e46871 -->
