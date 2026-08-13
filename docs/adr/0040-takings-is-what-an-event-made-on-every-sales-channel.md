# Takings is what an Event made, on every Sales Channel

## Context

Sales Trends draws an Event's sales day by day, split by Ticket Type, and it draws them twice: once
counting tickets and once counting money. The tickets chart is unambiguous — a ticket is a ticket
whether it sold online or at the door. The money chart is not, and the choice of figure decides
whether the feature works at all.

The product has exactly one per-line money figure today: **Net Proceeds**, `quantity × (unit_price −
fee − fee_iva)`, defined in one place (ADR 0014) and summed by the Event's Sales summary strip, the
Withdrawable Balance, and an Affiliate Link's attributed figures. The glossary is explicit that only
Online Sales produce it, and the Sales summary enforces that with a `FILTER (WHERE ts.channel =
'online')`. ADR 0032 leans on the same rule, leaving the Sales Export's `net_proceeds` cell blank on
a non-Online Sale precisely so a spreadsheet cannot sum it.

Applied to a chart, that rule breaks. An Event that sells half its stock through Sale Imports of
Direct Sales — cash and transfers the Organization arranged itself — would show a tickets chart full
of bars beside a money chart that is flat zero on the same days. The two charts sit one above the
other by design, sharing an X axis so that divergence between them is the insight. Systematic
divergence caused by the channel, rather than by the day, makes the pair unreadable.

There is a trap in the raw arithmetic, and ADR 0032 already names it: in-person and imported lines
carry fee snapshots of **zero**, because those channels are recorded through a path that writes no
snapshot. So `LineNetProceedsSQL` without the channel filter does not error and does not blank — it
quietly returns the line's *full price*. ADR 0032 called that "a plainly wrong number" in a
spreadsheet column headed Net Proceeds, and it was right: the column claims the platform handled
money it never touched.

But the number itself is not wrong. What is wrong is calling it Net Proceeds. Money the Organization
took at the door is money the Event made; it simply never passed through the platform. The
unfiltered expression is a perfectly good answer to a question the product had not yet asked.

## Decision

**Sales Trends plots Takings: `quantity × (unit_price − fee − fee_iva)` across every Sales Channel,
with no channel filter.** Under `pass_on` Fee Handling — the default for every Event — this is
exactly the price the Organization set, wherever the ticket sold, which is why the same expression
serves both channels honestly.

**Takings is a new domain term and is never called Net Proceeds.** The two figures answer different
questions: Net Proceeds is what the platform will hand over, Takings is what the Event made. They
coincide only for an Event that sold exclusively online. Nothing that today reports Net Proceeds
changes its figure or its name.

**Takings confers no claim on the platform.** It does not feed the Withdrawable Balance, the Payable
Balance, or a Payout Request, and it never will: the money it counts beyond Net Proceeds is money
the platform never held.

**Reversed sales are excluded**, as they are from every other money figure, and Sales Trends states
the Event's reversed count beside the chart rather than letting a bar silently shrink.

## Considered options

- **Net Proceeds, online only — the consistent answer.** Matches the summary strip exactly, reuses a
  figure the codebase already defines once, and needs no new vocabulary. Rejected because it makes
  the money chart useless for exactly the Organizations that most need it: a promoter running a
  door-heavy show would see a money chart contradicting the tickets chart directly above it. A
  feature whose stated purpose is reading sales at a glance cannot ship a chart that is flat on half
  the days sales happened.
- **Gross — what buyers paid**, `quantity × unit_price` on every channel. Uniform, real, and requires
  no new concept. Rejected because under `pass_on` the online bars include the Platform Fee and Fee
  IVA — money the Organization never receives — so online days would stand roughly 11.5% taller than
  door days that earned identically. The chart's whole job is comparing days against each other, and
  this figure biases exactly that comparison, in a way no reader would suspect.
- **Two money charts, one per figure.** Honest, and rejected as an answer to a question nobody asked:
  three stacked charts to read at a glance is not a glance, and it pushes a distinction between
  platform-settled and self-collected money onto a promoter who wants to know which Saturday sold.
- **Takings, but only where it is unambiguous — blank for non-Online Sales, as the Sales Export does.**
  This is ADR 0032's rule, and it is right *there*: a spreadsheet's blank is skipped by SUM, which is
  the whole point. A chart has no blank. An absent bar reads as a day that sold nothing, which is a
  stronger and more misleading claim than the zero the Export refuses to write.

## Consequences

**The Trends tab will total to more than the Sales tab's strip, on the same Event, on the same day.**
This is the cost of the decision and the reason it is written down. An Org Admin who adds up the
Takings chart and compares it to the Net Proceeds strip will find a discrepancy and reasonably
suspect a bug. Both surfaces must label their figure by name — the strip says Net Proceeds, the chart
says Takings — and the glossary must be reachable from the difference. Anyone "fixing" the
discrepancy by adding a channel filter to Sales Trends should read this ADR first.

**`LineNetProceedsSQL` now has two callers meaning two different things**, distinguished only by
whether a channel filter accompanies it. The expression stays single-sourced — that is what keeps the
figures from drifting a penny — but the filter is now load-bearing at every call site rather than
incidental. A caller that forgets it is not computing Net Proceeds; it is computing Takings and
calling it the wrong name.

**A third money vocabulary word raises the cost of the next one.** The product now states Net
Proceeds, the Withdrawable Balance, the Payable Balance, and Takings, and staff will not all hold the
distinctions. The mitigation is that Takings appears on exactly one surface, and that surface exists
to answer one question.

**If in-person selling ever writes fee snapshots, Takings changes meaning silently.** The POS surface
is a stub today and the `in_person` channel writes no snapshot, so Takings on it is the full price. If
the platform ever takes a cut of a door sale, this expression starts netting that cut off without a
line of code changing. That would be correct — Takings is what the Organization kept — but it should
be a deliberate observation at the time, not a surprise in a chart.
