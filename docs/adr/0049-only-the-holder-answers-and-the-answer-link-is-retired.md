# Only the Holder answers, and the Answer Link is retired

**Supersedes [ADR 0044](./0044-the-holder-answers-by-link-and-the-platform-never-writes-to-them.md)'s Answer Link and its "three parties may supply an Answer" rule.** Builds on
[ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md) and
[ADR 0048](./0048-the-buyer-holds-one-ticket-by-paying.md).

## Context

ADR 0044 let three parties write a Ticket's Answers: the buyer, whoever held a forwarded Answer Link,
and Event Staff. It predates Ticket Assignment. Once ADR 0046 gave a Ticket a named, proven Holder
and ADR 0048 made the buyer the Holder of exactly one Ticket, the buyer's sale page still let them
answer and edit every Ticket on the Sale, and still offered a "copy this ticket's link" beside an
address field that does the same job with proof. Two doors to one room, one of them proof-free, and
a buyer typing a friend's dietary requirements from memory.

## Decision

An Answer is given only by the Ticket's Holder — the buyer for their Self-held Ticket — or by Event
Staff. The buyer can neither enter, read nor correct an Answer on a Ticket they do not hold; of such
a Ticket they see its assignment state and nothing else. The Answer Link is retired: no link the
buyer can see opens a Ticket's questions. The Answer Reminder is addressed to Holders only, each
about their own Ticket; an unassigned Ticket is chased by nobody, since nobody can answer for it.

On every held Ticket's own surface — the buyer's "Your ticket" and a Holder's Customer Area, which
are the same panel — the questions stay open while the Ticket has an Outstanding Answer, each Answer
persists as it is given with no Save button, and the panel collapses behind a review-or-edit control
once nothing is owed.

## Considered Options

- **Keep the buyer as an editor, drop only the Answer Link.** Rejected: the "oops, I meant M"
  correction is the Holder's to make from their own Customer Area, and Event Staff remain the
  backstop; a buyer editing a Holder's Answers is the very guess ADR 0044 set out to avoid.
- **Chase the buyer about unassigned Tickets under the Answer Reminder.** Rejected for now: that is
  an assignment nudge, not an answer nudge. An Assignment Reminder is a separate decision if
  unassigned Tickets prove to pile up.

## Consequences

- The Answer Link token, its route, its page and "Copy this ticket's link" go. ADR 0044's
  disclosure rule survives in the Assignment Link unchanged.
- A Ticket Question added after a sale reaches an unaccepted Ticket only through Event Staff until
  the Ticket is accepted.
- Event Staff remain able to enter and correct any Ticket's Answers.
