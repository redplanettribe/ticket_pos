# The buyer is seated on the dearest Ticket, and upgrades out of a free one by election

## Context

[ADR 0048](./0048-the-buyer-holds-one-ticket-by-paying.md) made one Ticket of every Online Sale the
buyer's own, and picked it as *the first Ticket of the first line in catalog order*. It justified
that choice as arbitrary but harmless: a buyer holding the wrong one reassigns it on the Sale page
like any other.

Production disagrees. Organizations list their catalogs cheap-to-dear, so "first in catalog order"
means "cheapest in the basket" every single time a buyer mixes Ticket Types. `TP-W7CXRAEE` is the
case that opened this: one Sale, one checkout, two lines — a Community Ticket at $0 and a Community
Senior at $30. The seat went to the giveaway. And because checkout asks the Self-held Ticket's
Ticket Questions and nothing else, and the free Ticket Type asks none, that buyer was shown no
questions at all, while the $30 Ticket they are plainly attending on went out `unassigned` and owing
its one required Answer. Six active Online Sales are in that shape; three are still stranded.

The rule is therefore not a coin-flip that sometimes loses. It loses systematically, and ADR 0048's
escape hatch — reassignment — is exactly what those buyers did not do.

Behind it sat a second, larger thing the model could not say. Twenty-seven times, a buyer has taken
a free Ticket and come back later for a paid one on the same Event, several within ten minutes. They
are not buying two tickets; they are moving up, and the platform has no word for that. It records
them as people holding two Tickets and chases them, by Assignment Reminder, to name a second attendee
who does not exist.

## Decision

**The Self-held Ticket is the first Ticket of the Sale's dearest line, ties broken by catalog
order.** A buyer is presumed to attend on what they paid most for. This reverses ADR 0048's pick and
nothing else about it: the warrant is still the payment, it still makes nobody Verified, it is still
reassignable, and price is still only a guess whose remedy is reassignment. It has a second effect
worth naming — the dearer Ticket Types are the ones carrying Ticket Questions, so seating on the
dearest also seats on the Ticket whose questions checkout will actually ask.

**A buyer may elect an Upgrade, and only out of a Ticket that cost nothing.** An Upgrade is the
buyer's election that a paid Ticket takes the place of a free one; it is never a change to a Ticket,
because no Ticket in this model ever changes its Ticket Type. In one basket the free line is simply
not bought. Against a Ticket Sale already made, the free Sale is reversed **inside the same
transaction that commits the paid Sale** — so the buyer either holds the paid Ticket and not the free
one, or nothing moved at all. Surrendering before the provider has answered would let an abandoned
checkout take a Ticket and give nothing back, which is the mirror of the worst outcome this system
has.

**Free to paid, and no further.** A free Ticket carries no money and no Tax Invoice — 4,249 free
Online Sales, not one of them invoiced — so surrendering it owes nobody anything. The moment a paid
Ticket could be surrendered, "replace" means a partial refund and a credit note under
[ADR 0061](./0061-a-wrong-recipient-is-corrected-by-reissue-a-credit-note-then-a-fresh-sale-invoice-never-an-edit.md),
against a concept this platform does not have. The zero boundary is where a Ticket can be given up
without an accounting event, and that is the whole of why it sits there. A buyer moving between two
paid Ticket Types is offered nothing and holds both; that hole is accepted, and softened only by the
seating rule above.

**Offered only where the free Ticket is unambiguously the buyer's to give up:** exactly one of them,
on an active Online Sale, still held by the buyer themself. A free Ticket somebody else accepted is
not the buyer's to surrender, and a free Sale of several would take a stranger's Ticket with it —
eight such Sales exist. This admits 4,188 of 4,210 free Sales and would have refused none of the 27
historical upgrades, so the restriction costs nothing we can measure and closes a way to strand a
third party in silence.

**The Upgrade Prompt never gates a payment, and silence means keep both.** It can be ignored like any
Ticket Question. The two answers are not equally recoverable: keep-both is fixable at leisure, while
an Upgrade destroys a Ticket silently and inside the payment's own transaction. A buyer who scrolled
past a prompt they did not read must not discover afterwards that a Ticket they held is gone, so the
default falls to the reversible side. Where the buyer's seat is ambiguous — more than one free Ticket
in play, or one in the basket and another on an earlier Sale — no prompt is shown at all, because a
prompt that must ask which of several people it is about has stopped clarifying.

**The reversal is silent, and it is not a Sale Correction.** No Sale Voided notice and no No Longer
Holding mail: a buyer who upgraded has not lost a sale and should not be told they have. The reversed
free Sale names the paid Sale through the existing `replaced_by_sale_id` / `replaces_sale_id` link,
but marked as an Upgrade. `corrected` is
[ADR 0050](./0050-an-imported-sale-is-corrected-by-replacement-never-edited.md)'s word for a sale
somebody recorded wrongly; an Upgrade is a sale recorded perfectly by a buyer who changed their mind,
and letting one word mean both would tell an Organization its staff erred on a Sale no human touched.
If the free Sale went away while the buyer was at the provider, the paid Sale commits anyway and
nobody is told, because the end state is already what was wanted.

**Three stranded Sales are re-seated; nothing else is touched.** `TP-4Z3MNO5A`, `TP-U2HKDYJD` and
`TP-W7CXRAEE` have a paid Ticket with no holder at all, so moving the seat within the Sale displaces
nobody, destroys nothing, and touches no money, invoice or capacity — the same reasoning by which ADR
0048 backfilled rather than shipping to an audience it had left behind. The other three are excluded
deliberately: two buyers already hold both Tickets, and `TP-X6QRQL2O` has assigned its paid Ticket to
a third party, which is proof that "the dearer one is for a friend" is a real shape and not a
hypothetical. The 27 cross-Sale double-holders are left alone entirely — an Upgrade is *the buyer's
election*, and a migration that elects on their behalf is the platform deciding somebody did not want
a Ticket they are holding.

## Considered options

- *Keep catalog order and rely on the Upgrade Prompt.* Rejected: the prompt defaults to keep-both and
  most buyers ignore prompts, so `TP-W7CXRAEE` would reproduce exactly after all the work.
- *Let any cheaper Ticket be upgraded out of, not merely a free one.* Rejected for the refund and the
  credit note, not for the modelling.
- *Ask which Ticket is theirs when the buyer keeps both.* Rejected: it reinstates the picker ADR 0048
  refused, and turns one question into two on the surface where friction costs the most.
- *Make the prompt a required choice.* Rejected, though it is the only option that makes the bug
  impossible rather than merely rare: this platform has never gated a payment on an informational
  question, and an upgrade nicety is the wrong precedent to break that with.
- *Reuse the Sale Correction link unmarked.* Rejected — the cheapest-looking option, and the only one
  that makes an existing word mean two things.
- *Invent partial reversal* so a multi-Ticket free Sale could give up just the buyer's Ticket.
  Rejected: zero historical upgrades needed it.
- *Retro-upgrade the 27 double-holders.* Rejected: 27 silent destructions of Tickets people are
  holding, indistinguishable from the buyer who bought the free one for a friend.
