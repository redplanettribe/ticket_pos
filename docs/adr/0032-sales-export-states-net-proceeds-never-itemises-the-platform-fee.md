# The Sales Export states Net Proceeds and never itemises the Platform Fee

## Context

The Sales Export hands an Organization a spreadsheet of an Event's Ticket Sales, one row per
sale, for the work that happens after the sale: reconciling a bank deposit, splitting a night's
takings with a partner, issuing facturas. Every one of those jobs is arithmetic on money, and
until now the only money figure the file could carry was what the buyer paid.

What the buyer paid is not what the Organization got. The platform withholds a Platform Fee and
the Fee IVA levied on it from every Online Sale (ADR 0014), so an Org Admin reconciling per sale
has to subtract 10% plus 15% of that by hand — and has to remember whether the Event's Fee
Handling was `pass_on` or `absorb` at the time, since the two produce different figures from the
same set price and the setting can have been changed since. That is a calculation people get
wrong, on a column they will paste into their accounting.

**Net Proceeds** already exists in the product, but only as an Event-level total in the Sales
tab's stat strip. Per sale, it is new information. It is derivable from data the Organization
already holds, but no surface has ever handed it over per transaction, and that is what makes
this a decision rather than a feature.

Meanwhile the raw material for a fuller answer is sitting right there. Every Ticket Sale Line
froze a fee snapshot at checkout — the base price, the buyer price, the fee, the Fee IVA, and
the rates that produced them — precisely so recorded economics never move when rates or Fee
Handling do. Emitting the fee columns would cost one line of code. The reason not to is a stance
the product has held everywhere else and has never written down: the Sales summary handler
records that "the platform's cut is never returned as a number", the checkout quotes the
Customer one all-in price with no split, and ADR 0014 keeps the fee an Organization-side
withholding rather than a line item anyone is shown.

That stance is now about to meet a file. A spreadsheet is not a screen: it is forwarded, kept,
opened by people who never saw the product, and built on. Whatever columns it has become
somebody's formulas within a week.

## Decision

**Each row of the Sales Export states its Net Proceeds, and the Platform Fee and Fee IVA are not
itemised as columns of their own.** The Organization is told what it nets, not what the platform
takes. `net_proceeds` sits immediately after `amount`, so what the buyer paid and what the sale
left the Organization are read side by side, which is where they get compared.

**The figure comes from the per-line fee snapshots the sale froze**, summed per sale — the same
SQL expression the Event's Sales summary, the Withdrawable Balance and an Affiliate Link's
attributed figures already sum, so a sale and the Event it belongs to can never disagree about
what it earned. Nothing recomputes a fee from a rate and nothing branches on the Event's current
Fee Handling: the subtraction reads the same under either mode because the snapshot always
records what the Customer actually paid. Changing an Event's Fee Handling afterwards therefore
never rewrites what an old sale earned.

**Anyone determined to derive the platform's cut can subtract `net_proceeds` from `amount`.**
That is accepted and is not a hole to be closed. The distinction is between a figure the product
states and a figure a person computes: the first is a claim the platform makes and must stand
behind, the second is arithmetic on two numbers the Organization is entitled to. The line is
about what the product says, not about what can be worked out.

### The blank-not-zero corollary

**A cell with no Net Proceeds is left blank. It is never written as zero.** This is part of the
decision, not an implementation detail, and it is the part most likely to be "tidied up" by
somebody who reads a blank as a bug.

A blank and a zero say different things. Zero is an assertion — *this sale earned the
Organization nothing* — and in a spreadsheet an assertion is not merely read, it is **summed**.
A column of zeroes silently pulls sales the platform never touched into a total about money the
platform held. A blank says the figure does not apply to this row, and SUM, AVERAGE and COUNT
all skip it, which is exactly the behaviour wanted.

`net_proceeds` is blank for:

- **Any sale that is not an Online Sale.** Only Online Sales produce Net Proceeds; money from
  other Sales Channels never passes through the platform, so nothing was withheld from it and a
  zero would wrongly assert the platform took nothing from money it never had. This one has a
  trap in it: in-person and imported lines carry fee snapshots of zero, so the raw arithmetic
  reads back as the sale's *full price*. The channel test is therefore load-bearing, not a
  tidiness check — without it the column would state a plainly wrong number rather than a
  debatable one.
- **Any reversed sale**, which drops out of the figures as it does on every other surface. The
  row itself stays, because a Sale Reversal should be visible in the file rather than a row that
  silently vanished, but money given back was never proceeds.

This mirrors ADR 0019's treatment of a free Online Sale's refund figures as absent rather than
zero, for the same reason: "zero refunded" and "nothing to refund" must stay different answers.

## Considered options

- **Carry only what the buyer paid, and no Net Proceeds at all** — the strictest reading of the
  existing stance, and the smallest file. Rejected because it leaves the person who downloaded
  it doing the platform's arithmetic by hand: a two-stage percentage, on every row, whose answer
  depends on a per-Event setting they have to remember the historical value of. The file's whole
  purpose is that the recipient should not have to recompute anything, and this is the one
  computation they cannot do reliably. It would also make the export the only money surface that
  knows less than the screen it was taken from.
- **Itemise everything: `amount`, `platform_fee`, `fee_iva`, `net_proceeds`** — the most honest
  and most useful file, arguably, and the one an accountant would ask for. Rejected because it
  reverses a stance held consistently everywhere else, and reverses it on the least reversible
  surface in the product. The platform's commission has never been presented to anybody as a
  number: not to the Customer, who is quoted one all-in price; not to the Organization, whose
  summary reports the net; not on the Event page. Making the export the sole exception would
  mean the file contradicts every screen it came from, and it would put a per-sale record of the
  platform's revenue into a spreadsheet designed to be emailed around. If the product ever
  decides to state its commission, it should do so deliberately and everywhere at once, not by
  the side door of a download.
- **State Net Proceeds only, which is what was chosen** — the middle position. It gives the
  recipient the figure they cannot compute, and states nothing about the platform's take. It is
  a real trade-off rather than a compromise for its own sake: it accepts that subtraction is
  possible in exchange for never making the claim.
- **Write zero instead of blank where Net Proceeds does not apply** — every cell filled, no
  ragged column, no recipient wondering whether something failed to generate. Rejected on the
  reasoning above: the file's numeric columns exist to be summed, and a zero is a claim that
  gets summed. The cost of the choice is real and is accepted — see below.
- **A "not applicable" text marker in the cell instead of a blank** — says out loud what a blank
  only implies. Rejected because it turns a numeric column into a mixed one: SUM over a column
  containing text is a class of spreadsheet bug that is silent in some tools and an error in
  others, and sorting the column stops being sorting by money. The `Info` sheet is where the
  file explains itself in words; the data sheet stays arithmetic.
- **Emit Net Proceeds for every sale by treating a non-Online Sale's zero fee snapshot as
  fee-free** — arithmetically defensible, since nothing was in fact withheld. Rejected because
  the result is not "the fee was zero", it is "the sale's entire price was Net Proceeds", which
  says the platform handled and passed on cash it never touched. The glossary is explicit that
  only Online Sales produce Net Proceeds; the column follows the glossary.
- **Recompute the fee per row from the Event's current rates and Fee Handling** — fewer columns
  read, one arithmetic path. Rejected outright: it is the exact failure ADR 0014's snapshots
  exist to prevent. A rate change or a Fee Handling flip would retroactively rewrite what every
  historical sale earned, and two exports of the same period taken either side of a settings
  change would disagree about money.

## Consequences

**The column cannot be withdrawn.** Organizations will build spreadsheets, templates and
month-end processes on `net_proceeds` within weeks of it shipping. Removing it, renaming it, or
changing what it means later breaks files the platform cannot see and never hears about. This is
the sense in which the decision is hard to reverse, and the reason it is recorded rather than
merely implemented.

**A ragged column will look like a bug to somebody.** An export mixing Online Sales with
in-person and imported ones has gaps in the middle of a money column, and the first instinct of
a careful reader — or a careful engineer reading a bug report about it — is to fill them in. The
blanks are the decision. Anyone changing them should have read this ADR, which is most of why it
exists.

**The platform's cut is one subtraction away, permanently.** Any Organization that wants the
number has it, and some will compute it and ask about it. The product's answer is that it does
not state the figure, not that the figure is secret; the two are different positions and this is
the first one.

**The export now depends on the per-line fee snapshots being right.** Net Proceeds was
previously only ever seen as an Event-level total, where a per-sale error rounds into a larger
number nobody checks. Per sale it is exposed to the one person able to check it against their
bank: an Org Admin reconciling a deposit. That is a genuine improvement in the system's
observability, and it also means a snapshot bug that used to hide in an aggregate now arrives in
somebody's inbox as a spreadsheet.

**The fee columns will be asked for.** An accountant looking at `amount` and `net_proceeds` will
want to know what the difference is called and see it broken into fee and IVA, and the request
will be reasonable. The answer is this ADR, and if the answer ever changes it should change
across every surface at once rather than in the export alone.
