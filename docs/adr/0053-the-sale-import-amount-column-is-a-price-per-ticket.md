# The Sale Import amount column is a price per ticket

## Context

A Sale Import row has a `quantity` and an optional `amount`. Every path that records one — the
uploaded spreadsheet, the JSON commit, a Sale Correction's replacement (ADR 0050), and a Manually
Recorded Sale (ADR 0052) — puts that amount into the Sale Line's `unit_price_cents`, and the sale's
value is derived everywhere as `SUM(quantity × unit_price_cents)`. Blank means the Ticket Type's
catalog price.

The spreadsheet template said the opposite. In three places — the cell tooltip, the header note and
Instructions line 7 — it told the Organizer the column was *"the total paid"*. Two of the four
surfaces that name the field, the Sale Correction dialog and the record-a-sale form, had always said
*"Price per ticket"*. The code and the majority of the prose agreed; one document did not.

The disagreement was invisible at quantity 1, where the two readings coincide exactly, and invisible
whenever the cell was left blank. It bit only where an Organizer sold two or more tickets on one row
**and** typed a figure. That narrowness is why it survived to be found by a review of ADR 0052's
implementation rather than by a bug report.

It had, in fact, already bitten someone. An audit of the production data found exactly one affected
sale in the platform's history: six tickets sold for 180.00 all in, an Organizer who typed `180`
exactly as the template instructed, a recorded sale of 1080.00, and a hand-built Sale Correction
undoing it. One sale, one Organization, one day — and already repaired, so nothing needed restating.
That emptiness is what made this decision cheap to take either way, and it is worth recording that
the audit was run *before* the choice, because a large result would have argued the other way.

## Decision

**The amount column is the price of one ticket.** The template's prose was wrong and has been
corrected to say so, working the multiplication through where the Organizer can see it: a row of 3
at 25.00 records a sale of 75.00.

No stored figure changes. No sale is restated. The code was already right on all four paths.

## Considered options

**Make the column mean the total paid, and fix the code.** This is what every Organizer had been
told, on the surface they were told it, and it is arguably what someone holding a receipt actually
knows — they have a total, and per-ticket arithmetic is work software could do for them. It was
rejected on cost, not on principle.

The cost is not a migration; the audit established there is nothing to migrate. It is that the sale
total is **derived, never stored**. There is no total column on `ticket_sales`; the Sales list, the
buyer's own view, Takings, Net Proceeds, the Sales Export and the Sale Confirmation each recompute
`SUM(quantity × unit_price_cents)` independently. So a total has to be turned into per-unit figures
at the moment of writing, and the platform has no rule for doing that — nothing in the sales domain
has ever had to split an amount across units. Whoever built it would have invented one, and would
also have moved the fee snapshot's basis, and reversed the Sale Correction dialog's prefill, which
today divides a total by the quantity and blanks itself when the division is inexact.

Note precisely what this argument is and is not. It is **not** that a total is unstorable: nothing
stops a sale carrying several lines of the same Ticket Type, so 100.00 across 3 is representable
exactly as 2 × 33.33 plus 1 × 33.34, and an early draft of this decision wrongly called it
impossible. It is that doing so introduces a split rule, an uneven-line shape that every reader of a
Sale Line would then have to expect, and a divergence from how the online channel writes the same
column — real design cost, incurred to relabel a field that three of four surfaces already labelled
correctly.

**Say nothing and let the prose stay wrong.** Rejected on the evidence: it had already cost a real
Organizer a hand-repaired sale.

## Consequences

**The four typed-row paths are now pinned, and two of them never were.** The .xlsx path and the
Manually Recorded Sale already asserted the per-ticket reading at a quantity above one. The JSON
commit's only explicit amount was a comp at zero, where 6 × 0 = 0 under either reading, and the Sale
Correction's was on a quantity of one, where the readings agree. Both were pinning nothing, which is
the mechanism by which this survived. A test that fixes the meaning must use a quantity of two or
more **and** an explicit non-zero amount; anything else passes under both readings.

**A lump-sum sale still cannot be recorded as such.** "Six tickets, 180.00 all in" is the shape the
one affected Organizer actually had, and the column cannot express it — 180 ÷ 6 divides evenly by
luck, 100.00 across 3 does not. This decision makes the software honest about that limitation
rather than removing it, and leaves the division to the Organizer. If it proves to matter, the
answer is an affordance that divides and shows the remainder, not a change to what the column means.

**The buyer's receipt still says "Total paid", correctly.** So an Organizer transcribing from a Sale
Confirmation reads a total and must type a unit price. The corrected template prose is the only
thing bridging that gap, which makes it load-bearing copy rather than a hint.

**Any future route that records a typed row inherits this.** ADR 0052 turns on the four paths
accepting identically, and a route that read the column as a total would be a divergence bug even if
it were self-consistent. The reading belongs in the shared validator, not beside it.
