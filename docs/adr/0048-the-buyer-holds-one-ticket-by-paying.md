# The buyer holds one Ticket by paying

## Context

[ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md) made a Holder a person
who proved their address by clicking an Assignment Link, and left every Ticket `unassigned` until the
buyer named somebody. The commonest case — the buyer is attending on one of their own Tickets — was
therefore the one the model could not state: a buyer of three who never assigned anything held
nothing, the Holder List showed three `unassigned` Tickets, and the storefront asked that buyer, at
checkout and again on the Sale page, for three people's shirt sizes as three identical forms.

The brief that produced this ADR was blunt: a buyer must not be made to answer for each Ticket;
those Answers are the Holders' to give; the surfaces are far too long. Making that true needed one
Ticket to be the buyer's, and the question was whether that could be a presentational convention
("Ticket 1 is yours") or had to be a fact the Organization also sees.

## Decision

**One Ticket of every Online Sale is a Self-held Ticket: assigned to the buyer and `accepted` when
the Sale is made.** It is the first Ticket of the first line in catalog order, with no picker — a
buyer holding the wrong one reassigns it on the Sale page like any other. Checkout asks that
Ticket's Ticket Questions alone, under "Your ticket", still skippable under ADR 0044; the Sale's
other Tickets are not mentioned at checkout and appear on the Sale page as one collapsed row each.

**Accepted by purchase, not by link — and it makes nobody Verified.** 0046's security property was
that the accept click is Proof of Email Ownership, and a payment is not that. So the assignment row
is written with the buyer as Holder and `verified_at` is left alone; verification stays the sign-in
module's authority. The Organization sees an ordinary `accepted` Ticket under the buyer's checkout
name and address — which the Sale already disclosed to it — and a fourth assignment state was
rejected because the roster's question is who is coming, not how they came to hold the Ticket.

**Online Sales only — and existing ones are backfilled.** A door sale's or Sale Import's buyer is a
name somebody else typed and often is not attending. Every Sale in production predates this
decision, so leaving them alone would ship the feature to an audience for whom every Sale page still
says "0 of N tickets have an address" and asks the buyer to name themself; a migration therefore
holds Ticket 1 for the buyer on each existing Online Sale, stamped with the Sale's own time so the
row is indistinguishable from one the checkout would have written. It skips a reversed Sale — whether reversed by an Organizer or through a Reversal Request, whatever became of it (the
Ticket admits nobody, and the roster would say otherwise), a Sale where Ticket 1 was already handed
to someone, and a Sale where the buyer already holds any Ticket by the link flow — one Ticket, not
two.

## Considered options

- *A UI convention with no assignment behind it.* Rejected: the page would say "your ticket" while
  the Holder List said `unassigned`, and the Answer Reminder would have no better recipient than it
  has today.
- *Assigned but not accepted until the buyer is Verified.* Recommended, declined: the buyer paid,
  and a guest buyer would otherwise see their own Ticket "waiting" on them.
