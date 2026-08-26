# How it works, and why

This page does not say where to click; it says why things are the way
they are. People read it when a product decision looks strange: there
is almost always an argument behind it worth knowing.

Instructions are in [How to](howto.md), lists in the
[Reference](reference.md).

## Card order is manual

Cards sit in a column in the order somebody put them there. Neither
priority nor a due date changes it.

The reason: order answers "what do I take next", and a person answers
that, not a sort. Priority says what matters more; a due date says what
was promised outside. Those are three different questions, and
substituting one for another loses two of the three.

## A column limit is highlighted, not enforced

An exceeded limit is visible but does not stop you moving a card.

A limit exists to make overload visible, not to argue with someone who
has already decided. None of the products that have limits block the
move: a hard stop breaks the single scenario the limit is there for.

## Blocking is a reason, not a status

You cannot block a card without an explanation: the reason is required
and shows on the board itself.

"Blocked" on its own answers no question — neither "who are we waiting
for" nor "what now". A reason answers both.

## The irreversible asks, the reversible does not

Archiving a card takes one press, with «Вернуть» (restore) right next
to it. Deleting a board for good takes typing its name.

People close "are you sure?" dialogues without reading, so the
confirmation stands where you cannot come back, and stays out of the
way where you can.

## Changes spread on their own

An open board updates for everyone looking at it, without a reload.

One database connection per process holds this up, not one per tab: a
hundred open boards is the stated scale, and a subscription per tab
would have run out of connections somewhere in the thirties.

## Isolation between organisations is held by the database

One organisation's data is invisible to another not because the code
says so, but because the database's policies are built that way.

A check in code is forgotten at the next edit; a database policy
applies to every query, including the one written in a hurry. For the
same reason the application refuses to start if the role it connects
with bypasses policies.

## The forecast speaks of probability, not of a date

«Поток» gives three numbers instead of one: half the cases land within
so many days, 85 % within so many, 95 % within so many.

A single number where a probability is being computed reads as a
promise. The forecast samples random past weeks and answers exactly one
question: what happens if things continue as they have been. If there
is not enough past, it says that too.

## Metrics come from work, not from ticks

Cycle time, throughput and ageing are taken from when a card entered
work and when it was carried to the end.

Separate "started" and "finished" fields, filled in by hand, drift from
reality in the first busy week — and drift silently.

## A board's key never changes

The key — the prefix of card numbers — is set once.

A card number ends up in correspondence, in neighbouring teams'
backlogs and in other systems. Changing it would invalidate every
existing reference at once.

## An invitation is addressed and single-use

The link is bound to an e-mail and shown once: only a fingerprint of it
is stored.

A link that works for any address is a door into your organisation,
lying in somebody's inbox.

## Removing and anonymising are different actions

Someone who left stops being in the organisation, but their cards,
comments and audit entries stay: the work was done, and erasing it
because they left is not on. Anonymising is a separate action,
irreversible, and asked about separately.

## Delivery gives up out loud

If a receiver does not answer, we retry eight times with a doubling
pause — about two hours — and then disable the subscription.

Quietly accumulating undelivered events for years is worse than
stopping and saying so: all that time, the neighbouring system believes
nothing is happening here.
