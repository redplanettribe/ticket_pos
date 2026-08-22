# A Ticket becomes a row, and quantities stay the truth

## Context

An Organization wants to ask the person who will hold a ticket a question — a t-shirt size on a
workshop ticket, dietary requirements on a dinner ticket. A buyer of four tickets must be able to
supply four different answers, so the answer is a fact about one unit of admission and not about
the checkout.

The model has no such unit. A **Ticket Sale Line** is `(ticket_type_id, quantity, unit_price_cents)`
and nothing anywhere represents one ticket. The glossary is deliberate about this: **Tickets Sold**
is "not a count of people… the platform does not yet know who walks in on it", and **Sale
Confirmation** is "not a per-attendee admission ticket; the model leaves room to attach individual
tickets later". This is that later.

## Decision

**A Ticket is a row.** Every unit of every Ticket Sale Line's quantity gets one, on `online`,
`in_person` and `import` alike, whether or not any Ticket Question is attached to its Ticket Type.
Existing sales are backfilled, reversed ones included.

**Minted inside the sale-commit transaction, and never before.** A Payment that is abandoned,
declined or expired leaves no Tickets, so the invariant reads simply: there is one Ticket per ticket
sold.

**A Ticket carries no status.** Whether it stands is read from its Ticket Sale. A Sale Reversal is
always whole-Sale, so a Ticket-level status could never legitimately differ from the Sale's and
could only ever drift out of step with it.

**Tickets Sold keeps summing quantities.** The figure is unchanged: the same `SUM(quantity)` over
active sales, in the same queries. Tickets are a projection of it. That the two agree is asserted in
tests, not enforced by reading the figure off the new table.

## Considered options

- **Answers on the Ticket Sale, one per question per checkout.** The cheapest model, and wrong for
  the motivating case: a cart of three shirts has three sizes, and one answer cannot hold them.
- **Answers on the Ticket Sale Line as an array indexed 1..N.** Avoids naming a new entity while
  incurring every consequence of having one — `answers[2]` with nothing in the model to say what
  ticket 2 is. Rejected because it is this decision with a worse name and no place to hang the next
  per-ticket fact.
- **Mint Tickets only for Ticket Types that have Ticket Questions.** No backfill, no rows for most
  sales. Rejected because it makes an entity an artifact of a feature: every seam downstream grows
  an `if this type has questions`, "how many Tickets does this sale have" answers 0 for almost every
  sale, and the day check-in arrives the migration happens anyway against more data.
- **Mint Tickets when checkout begins**, so answers attach to real Tickets before payment. Rejected
  because Tickets would exist for Payments that never become Sales, which is precisely the confusion
  the model avoids today by creating no Customer until a Payment is approved.
- **Switch Tickets Sold to counting Ticket rows.** One source of truth, no drift by construction.
  Rejected on three grounds: the figure went public on the Event page one commit ago (ADR 0042) and
  is not worth disturbing; the `SUM(quantity)` calls sit inside the same SELECTs as the fee and Net
  Proceeds arithmetic, so "count rows instead" means rewriting money queries; and capacity and
  Purchase Limit read those quantities against live Capacity Holds, which have no Tickets at all
  because a pending Payment holds stock before any sale exists. Quantity is therefore first-class
  regardless, and the honest description is that Tickets are derived from it.

## Consequences

**A 500-row Sale Import mints 500 Tickets.** Accepted; the table is narrow and the write is one
statement inside a transaction that was already happening.

**The name promises more than the thing does.** A reader meeting `Ticket` will expect a QR code, a
holder and a check-in state, and will find an identity and some answers. The glossary entry says so
explicitly. The alternative names — Issued Ticket, Admission — bought precision at the cost of not
being the word the domain already uses for the thing being counted.

**Drift between quantities and Tickets is possible in principle.** It is prevented by minting inside
the commit transaction rather than by a constraint, and asserted in tests. Anything that ever writes
a Ticket Sale Line outside that path must mint alongside it.

**The platform now holds data about people who are not its Customers.** A Ticket may carry a fact
about the buyer's friend. That is new, and it is the subject of ADR 0044 and ADR 0045.
