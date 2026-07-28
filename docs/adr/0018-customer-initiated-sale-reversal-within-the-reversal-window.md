# Customer-initiated Sale Reversal within the Reversal Window

## Context

Nothing in the system could reverse an Online Sale. A buyer who bought the wrong tier, the wrong
quantity, or the wrong night had exactly one recourse: email the Organization and hope somebody
logged into the PayPhone dashboard and issued the reversal by hand. ADR 0012 reserved
`PaymentProvider.Reverse` for this and left every implementation returning
`ErrPaymentReverseNotSupported`; ADR 0017 recorded the absence as a live hole, since a free tier
with no reversal path can be permanently exhausted by one determined actor.

PayPhone does in fact expose a reversal API — `POST /api/Reverse/Client`, keyed on the
`clientId` we already send as our `client_transaction_id`, on the same origin and bearer token
as Prepare and Confirm. Three properties of it decide the shape of everything below:

- **The window is same-day, until 20:00 Ecuador time.** Not 24 hours, not a rolling period — a
  wall-clock hour. A buyer purchasing at 19:55 has five minutes; one purchasing at 20:30 never
  has any.
- **Reversals are all-or-nothing.** No partial amount is documented, so no partial reversal of a
  Ticket Sale is possible.
- **There is no error code for "too late."** The published catalogue documents 20
  (transaction not found), 40 (not a reversal) and 42 (*"El reverso no se puede ejecutar…
  contáctese con el banco emisor"*) — an issuing-bank refusal unrelated to timing. A refusal
  therefore cannot be reliably explained to the buyer.

## Decision

A signed-in Customer may reverse their own Online Sale, whole-Sale, without asking anybody.

The **Reversal Window** runs from the moment the Payment is approved until the earlier of 20:00
`America/Guayaquil` on the day of purchase, or the Event's start. It is a platform rule, not a
provider rule: it governs free Online Sales identically, where there is no money to return and
no provider to call. That PayPhone's own limit coincides with part of it is a fact about
PayPhone, kept inside the provider (ADR 0012).

The window closes at Event start because the platform holds no record of attendance — a Sale
Confirmation is explicitly not a per-attendee admission ticket and nothing is scanned at the
door. Without that guard, a buyer at a same-day show could attend and then reverse, and the
system could not tell that apart from a change of mind. Same-day events are the ordinary case
for a same-day window, so this is not an edge.

**The provider is called first.** Only on PayPhone's `true` is the Ticket Sale marked `reversed`,
`sold_count` restored, and the void notice sent. `ticket_sales` gains `reversed_at` and
`reversed_by`; the Payment stays `approved`, because the checkout genuinely did settle and the
reversal is a later event on the Sale rather than a retroactive edit to the checkout's outcome.

Authority is the Customer Session, never the Confirmation Link. The link is a read credential
that travels by email — forwarded, shared, quoted in support threads — and reversal is the first
mutating, money-moving action a Customer can take.

The window is **offered, not guaranteed**. Because an in-window refusal cannot be explained, a
failure leaves the Sale untouched and tells the buyer so, with their Sale Confirmation reference
and a pointer to the Organization; the error code goes to the log, not to the buyer.

## Considered options

- **Per-Event opt-in, like Fee Handling** — the Organization decides whether its buyers may undo.
  Respects a no-refund stance, but produces a Storefront where the button is absent for reasons
  the buyer cannot see, and splits every support conversation in two. Rejected in favour of one
  rule the platform can state plainly.
- **Customer requests, staff approves** — preserves organizer control and is the shape most
  ticketing products use. Rejected as unworkable against a 20:00 cutoff: an organizer replying in
  two hours has missed the window on an evening purchase, so the flow would routinely produce
  approvals that can no longer be executed. The constraint, not a preference, killed this.
- **Staff-only reversal from the Sales list** — mirrors the existing Sale Import undo authority
  exactly and needs no new customer surface. Rejected because it leaves the buyer's recourse
  where it already is, dependent on somebody being at a desk during the only hours that matter.
- **Local commit first, roll back if PayPhone refuses** — gives the buyer an instant answer.
  Rejected because the rollback can fail permanently: capacity released and resold cannot be
  reclaimed, leaving a buyer told they were refunded when they were not.
- **A `reversed` Payment status** — would make `payments` the single money-truth for
  reconciliation. Rejected because it makes `approved` non-terminal and contradicts ADR 0017's
  widening of Payment to mean *the checkout settled*.
- **Mapping PayPhone's error codes to buyer-facing messages** — rejected because the catalogue
  has no cutoff code, so any timing explanation would be a guess, and guessing wrong about
  someone's money is worse than saying less.
- **A separate `sale_reversals` audit table** — better forensics, and the natural home for the
  incident case below. Deferred as disproportionate until the feature has volume; two columns
  answer the questions that actually get asked.

## Consequences

**Amendment (2026-07-28): errorCode 24 is a receipt, not a refusal.** In production, a Reverse
call timed out on our side after PayPhone had already processed it; the retry was answered
`400 {"errorCode":24,"message":"La transacción ya se encuentra cancelada"}` — the transaction is
already cancelled. Under the original rule that answer was a refusal, which stranded the system
in the one state it can never leave: money returned, Ticket Sale forever active, the
Organization's dashboard showing revenue that does not exist. errorCode 24 on a 4xx is therefore
the single provider answer that completes a reversal without a literal `true` — it is proof the
money already left, so the sale follows it. Every other refusal shape, and every 5xx, still
reverses nothing.

**A successful reversal followed by a failed local commit is a real, silent failure mode.** The
buyer has their money and their tickets, and the Organization's dashboard shows revenue that no
longer exists. It is loudly logged and nothing else — the same treatment, and the same accepted
residual risk, as the checkout incident already documented on `payments.ticket_sale_id`. ADR 0012
deferred a reconciler; this decision does not add one, and this is the second case that would
benefit from it.

**No aggregate needed changing.** Net Proceeds, the Withdrawable Balance and the Operator
Dashboard's platform revenue all already filter `ts.status = 'active'`, so a reversal propagates
by flipping one column. The already-anticipated consequence at the far end holds: a sale reversed
after it was paid out drives the Withdrawable Balance negative, and it says so.

**The Organization loses a sale without acting and without being told.** No email is sent to
staff — there is no organizer-facing notification anywhere in the system, and choosing recipients
is a question this codebase has never had to answer. The Event's sales page surfaces a reversed
count so the drop is explained rather than silent, but an organizer who is not looking will not
know.

**A buyer can be denied a right we advertised**, through `errorCode 42` or a clock disagreement
at the boundary, and cannot be told which. Accepted: because the provider is called first, the
refusal costs the buyer nothing but the attempt.

**Free tiers are no longer permanently exhaustible by accident**, which retires part of ADR 0017's
recorded hole — a claimed free ticket can now be released the same day. Deliberate exhaustion is
untouched: an actor who simply never reverses still holds the capacity, and the real control
remains a per-email claim limit.

**Partial reversal is now conspicuously absent.** A buyer who wants to drop one of three tickets
must reverse the whole Sale and re-buy — at a price that may have changed, into capacity that may
have gone. This is PayPhone's constraint, not a choice, and it is the first thing that would need
revisiting under a provider that supports partial amounts.
