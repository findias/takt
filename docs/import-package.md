# Import package

**Format version 1.** takt reads it — on the import screen and with
`takt import` on the server. [`takt-fetch`](takt-fetch.md) from the same release writes
it; so far from YouGile, the other trackers listed below come next.

## What it is for

An import package is one file that carries boards from another tracker
— YouGile, Jira, Trello, Kaiten, Weeek, Asana, Notion, ClickUp, monday —
into takt installed in a closed network.

The move is cut in two at the perimeter. Outside, where there is
internet, `takt-fetch` signs in to the other tracker, collects the
boards and writes a package. The file is carried across by whatever
means the network allows. Inside, takt reads the package and imports
it through the same preview as a spreadsheet: where each column goes,
whose e-mails were not found, what did not come. The takt server in
the closed network opens no new connection for this, and has no code
that talks to other trackers' clouds beyond what it already had.

Why a package and not a spreadsheet: a spreadsheet loses what has no
cell — subtasks, links between cards, discussions, several boards at
once. A package keeps them.

## The file

A ZIP archive with the `.takt` extension. Inside:

| Entry | What |
| --- | --- |
| `manifest.json` | what the package is, where it came from, a SHA-256 of every other entry |
| `boards/<n>.json` | one board: columns, people, labels, cards; `<n>` is 1, 2, 3… |

Nothing else is read; any other entry is refused, not ignored, so that
a package cannot carry something past the reader unnoticed. Text is
UTF-8 JSON. Moments are RFC 3339 in UTC (`2026-09-22T10:15:00Z`), dates
without a time are `YYYY-MM-DD`.

## manifest.json

```json
{
  "format": "takt-import-package",
  "version": 1,
  "createdAt": "2026-09-22T10:15:00Z",
  "createdBy": "takt-fetch v0.4.0",
  "collectedBy": "Anna Clarke, migration of the warehouse boards",
  "source": {
    "system": "yougile",
    "url": "https://ru.yougile.com",
    "account": "Northern logistics"
  },
  "boards": [
    { "file": "boards/1.json", "sha256": "9f2c…", "title": "Warehouse", "cards": 214 }
  ],
  "lost": ["task chats: files attached to messages"]
}
```

| Field | Meaning |
| --- | --- |
| `format` | always `takt-import-package`: tells a package from any other JSON |
| `version` | the format version, an integer. The reader refuses a version it does not know and says which versions it reads |
| `createdAt`, `createdBy` | when and by which program the package was written |
| `collectedBy` | free text: who collected it and why. Not a signature — the package is not signed in version 1 |
| `source.system` | where the boards came from: `yougile`, `jira`, `trello`, `kaiten`, `weeek`, `asana`, `notion`, `clickup`, `monday` |
| `source.url`, `source.account` | which installation and which account or company, for the report |
| `boards` | every board entry with its SHA-256 (lower-case hex), title and number of cards |
| `lost` | what the source had and the package does not carry, in words — shown in the preview under **Not imported** |

**Integrity.** The reader computes SHA-256 of every board entry and
compares it with the manifest; one mismatch refuses the whole package
and names the entry. This catches a file damaged on the way. It does
not prove who wrote the package: the package has no signature in
version 1.

## boards/&lt;n&gt;.json

```json
{
  "externalId": "b-1",
  "title": "Warehouse",
  "columns": [
    { "externalId": "c-1", "title": "To do", "kind": "queue" },
    { "externalId": "c-2", "title": "In progress", "kind": "in_progress" },
    { "externalId": "c-3", "title": "Done", "kind": "done" }
  ],
  "people": [
    { "externalId": "u-1", "email": "anna@example.test", "name": "Anna Clarke" },
    { "externalId": "u-2", "email": null, "name": "Ivan Petrov" }
  ],
  "labels": [
    { "externalId": "st-2", "name": "Area: Warehouse 2" }
  ],
  "cards": [
    {
      "externalId": "t-1",
      "title": "Reconcile stock",
      "description": "Warehouse 2, by Friday",
      "column": "c-1",
      "assignees": ["u-1", "u-2"],
      "labels": ["st-2"],
      "estimate": 3,
      "priority": "high",
      "due": "2026-09-30",
      "createdAt": "2026-09-02T08:00:00Z",
      "finishedAt": null,
      "parent": null,
      "links": [{ "kind": "blocks", "to": "t-4" }],
      "comments": [
        { "author": "u-2", "at": "2026-09-03T09:12:00Z", "text": "Started with row 1" }
      ]
    }
  ],
  "lost": []
}
```

Every object is referred to by its `externalId` — the identifier in the
source system, a string. References point inside the same board file,
with one exception: a card's `parent` may be a card on another board of
the same package — an epic on the portfolio board and its feature on a
team board. Boards are moved one at a time and in any order; whichever
of the two is moved second makes the card a part of its parent, and
until then the report names the board still to move. A link target on
another board, or a `parent` found on no board of the package, is
dropped and named in the report.

**Columns** are listed in board order. `kind` is a hint — `queue`,
`in_progress`, `done` or `null` when the exporter could not tell; the
preview shows it and a person can send any column to another one on an
existing board, exactly as for a spreadsheet.

A board with `"historyCollected": true` says the exporter collected
task history: a card without `history` simply had none, and nothing is
fetched later.

A board with `"level": "portfolio"` is an epic portfolio: moved into a
new board, it becomes a portfolio board without iterations, and its
cards are epics. Without `level`, or with `"team"`, it is a team board.
An older takt ignores the field and creates an ordinary board.

**People** are matched by e-mail against the organisation, case
insensitively; in the preview the owner can match anyone with a
member, create an account or leave them out, and nobody is created
without that choice. `email` is `null`
when the source did not give it — Trello gives members' e-mails only to
a workspace administrator, so a package collected by anyone else has
no e-mails, and `takt-fetch` says so before it starts. A person without
a match is not assigned; the report lists them by e-mail or by name, and
their cards get a technical label with their name — one per person, told
apart by `externalId`, so two namesakes get two labels.

**Labels** become labels of the board by name; a label with the same
name that already applies to the board is reused.

**Cards**:

| Field | Required | Meaning |
| --- | --- | --- |
| `externalId` | yes | the task's identifier in the source; with `source.system` it is the key that makes a second import skip what already came |
| `number` | no | the task's number as people know it (`DEV-12`); the card's history names it, so the task can be found in the old system. Without it the history names `externalId`, unless that is a UUID |
| `title` | yes | non-empty; a card without a title is not imported and is named |
| `description` | no | plain text; the exporter turns the source's markup into text and appends checklists as `- [x] item` lines |
| `column` | yes | a column `externalId` of this board |
| `assignees`, `labels` | no | `externalId`s of people and labels of this board |
| `estimate` | no | a positive number |
| `priority` | no | `low`, `medium`, `high`, `highest`; absent means `medium` |
| `due` | no | a date |
| `createdAt`, `finishedAt` | no | moments; `finishedAt` on a card in a done column is when it was finished |
| `parent` | no | the `externalId` of the parent card: the card becomes its part (subtask) |
| `links` | no | `blocks` or `relates`, to a card `externalId` of this board |
| `comments` | no | in time order; `author` is a person `externalId` or `null` |
| `history` | no | the task's history in the source — who did what and when — in time order, the same shape as `comments`. The card shows it under «before the import», apart from its history here |

The key of a second import is `source.system` with the card's
`externalId`. For YouGile it is the same key as import over the API
uses, so a board brought over the API first and from a package later
does not come twice.

**Comments** come across with their date. The author is the matched
person when their e-mail is found; otherwise the comment is written on
behalf of the person importing, and begins with the author's name as
text — “from YouGile: Ivan Petrov”, or “unknown author” when the
source does not name them — so that nobody is invented and
nothing is attributed to the wrong person.

**History** follows the rule of every import: dates come across, moves
between columns do not. The old system had its own columns, and its
moves passed off as ours would give a metric no one can trust.
Imported cards are marked, and **Flow** can count without them.

## Limits

| What | Limit | Beyond it |
| --- | --- | --- |
| cards on one board | 10,000 | the board is refused with the number; split it at the source |
| package uploaded on the import screen | 50 MB | the screen says so; an administrator imports it from the command line on the server, where the file size is not limited |
| one entry unpacked | 256 MB | the package is refused: a small archive that unpacks into gigabytes is not a package |
| entries | 1,000 | the package is refused |

## What never goes into a package

Passwords, API keys, tokens and session cookies of the source system.
`takt-fetch` takes them from the command line or the environment and
does not write them anywhere; a test searches every package it writes
for the key it was given.

## Versions

A change that an existing reader would misread — a field renamed,
a meaning changed, a required field added — raises `version`. Adding an
optional field does not: readers ignore fields they do not know inside
`boards/<n>.json`. A reader states every version it accepts, and a
newer package is refused with the versions this installation reads,
so that the answer is “upgrade takt”, not a half-imported board.
