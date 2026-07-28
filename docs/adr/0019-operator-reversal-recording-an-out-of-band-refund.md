# Operator Reversal: recording an out-of-band refund

## Context

ADR 0018 gave the buyer a way to undo their own Online Sale, and gave it a deadline: the
Reversal Window shuts at 20:00 Ecuador time on the day of purchase, or at the Event's start if
the doors open first. Past that instant the system has no way to undo a sale at all — and the
requests do not stop arriving. An Organization asks the platform to refund a buyer days later,
through support, and somebody does it: by hand in the PayPhone dashboard while the provider
still allows it, or by bank transfer when it does not.

The money moves and the system never learns. The Ticket Sale stays `active`, its tickets stay
claimed against the Ticket Type's capacity, the Organization's Withdrawable Balance still
counts proceeds that went back to the buyer, and platform revenue still counts a Platform Fee
whose fate nobody recorded. The books drift from the money, and the only repair available was
a hand-written database correction — an operation with no record, no authority check and no
witnesses.

The platform already has a shape for this. A Payout is not an instruction to move money; it is
a Platform Operator's record that money already moved, believed as stated, stamped with who
recorded it. The refund that support just issued is the same kind of fact, arriving through the
same door.

Two things about it are worth stating up front, because they decide almost everything below.
The refund happens where the platform cannot see it — a bank transfer is invisible to the
Payment Provider, so there is nothing to check against. And the operator is the party who moved
the money, which is what makes their assertion worth believing at all.

## Decision

A **Platform Operator** may mark any active Online Sale `reversed`, recording that they already
refunded the buyer off-platform. This is the **Operator Reversal**, the third route into the
Sale Reversal ADR 0018 defined, and it is a pure record.

**The Payment Provider is never called.** Not first, not as a fallback, not "try it and see".
The money left the platform's account before the request was made, so the only thing a provider
call could achieve is refunding somebody a second time. Nor is anything verified against
PayPhone: a bank transfer never touched it, so verification would answer nothing for half the
cases and would reintroduce, on a recording path, exactly the provider dependency that makes
the customer path fragile. The operator asserts, and the system believes them.

**Authority is the Platform Operator, and only the Platform Operator**, on the operator
allowlist (ADR 0015), exercised through an ordinary Staff Session on the Operator Dashboard.
Organizations ask the platform, as they already do for Payouts.

**Eligibility is any active Online Sale, and the Reversal Window is not consulted in either
direction.** Being past the window is the reason this operation exists; being inside it is no
reason to refuse an operator whose buyer is unreachable or whose provider path is down. Paid
and free Online Sales are equally eligible. Sales on other Sales Channels are refused —
including imported ones, whose undo is the batch-level Sale Import undo, because no money for
them ever passed through the platform and so there is nothing here for anybody to assert. An
already-reversed sale is refused with the existing already-reversed error, whether the second
attempt is the operator's own double press or a race the buyer's undo won.

**A money assertion made on human say-so is never anonymous.** The sale records a third
reversal actor, `operator`, joining `customer` and `staff`, and beside it the acting operator's
email — taken from the session, never from a request body, and recorded as an email exactly as
`payouts.recorded_by` is, because the allowlist is keyed by email and the record must outlive
the operator's presence on it. An optional free-text note, bounded to a sentence's worth,
carries what no schema captures: *refunded via PayPhone dashboard*, *bank transfer, waived fee
as goodwill*.

**On a paid sale, two money facts are required and neither has a default.** `refunded_amount`
is what the buyer actually got back, in the sale's currency's minor units, strictly positive
and at most what the sale collected; it is not pre-filled, because a pre-filled number is one
nobody reads. `platform_fee_kept` is the operator's explicit statement of whether the platform
kept its commission — deliberately no default, because a default would put a decision about the
platform's revenue in the schema's mouth rather than a person's. The Platform Fee and its Fee
IVA always travel together, so the one flag decides both (ADR 0014). On a free Online Sale both
fields are absent — null, not zero — and stating either is refused: nothing was collected, so
"zero refunded" and "nothing to refund" must stay different answers.

**Platform revenue gains the single exception to status-filtered aggregates.** ADR 0018 could
say that no aggregate needed changing, because every money figure already filtered
`status = 'active'` and the status flip was the whole subtraction. That still holds on the
Organization's side, untouched: Net Proceeds, Event earnings and the Withdrawable Balance all
drop the sale by flipping one column, and the balance goes negative when a Payout already
covered it — the far-end consequence ADR 0018 anticipated, now routine rather than theoretical.
Platform revenue is the exception, and gains exactly one term: fee plus Fee IVA from reversed
sales whose Operator Reversal said the platform kept them. It is money the platform still
holds, so a figure that dropped it would be wrong. It is carried in its own columns so no
Organization-facing figure can pick it up by accident, and the Operator Dashboard discloses the
kept total whenever it is non-zero, so a revenue number that survived a reversal can be
explained. `refunded_amount` feeds no aggregate: there is no cash view for it to correct, and
it is recorded for the sale's own detail and a future reconciliation.

**The operation is irreversible.** No un-reverse exists and none is added: released capacity can
be resold within seconds, and the Sale Voided email cannot be unsent. The confirmation step
before it restates the consequences.

**Entry is a lookup by Sale Confirmation reference**, cross-Organization by design, because the
flow always begins in a support thread that carries a reference and nothing else. There is no
operator sales browser.

**The buyer gets the existing Sale Voided email** after the commit, its failure swallowed like
every other notice on this path. No organizer notification is sent — there is no organizer
notification channel, and the Organization is the party that asked. The money memo is
operator-facing only; Organization staff see the sale as reversed by the platform, through the
same status filter and reversed count as any other Sale Reversal.

The Payment stays `approved`, and the deferred `sale_reversals` audit table stays deferred —
both ADR 0018's rules, unchanged.

## Considered options

- **Call the provider, fall back to recording** — attempt `PaymentProvider.Reverse` and only
  record by hand if it refuses. Superficially the safer order, and rejected outright: by the
  time an operator reaches this form the money has already left, so a provider call that
  *succeeds* is the disaster, not the failure. The one path that must never fire is the one
  this option makes the default.
- **Verify the assertion against PayPhone before recording** — a weaker version of the same
  idea, checking the transaction's state rather than acting on it. Rejected because a bank
  transfer left no provider trace to check, so the check would pass judgement on the cases it
  understands and shrug at the rest, while making a recording path fail on provider outages.
- **Organizer-initiated marking** — let an Org Admin mark their own sale reversed after
  transferring the buyer back. Attractive because the Organization is closest to the facts, and
  rejected on both halves of the trust question: the platform cannot verify an organizer's
  claim that a transfer happened, and the same form lets an Organization erase a fee-bearing
  sale from platform revenue on its own say-so. Money assertions stay with the party that moved
  the money.
- **An Organization-side request that an operator approves** — keeps the authority where it
  belongs while giving the Organization a place to ask. Rejected as ceremony around a
  conversation that already exists: the request arrives by support thread today, and modelling
  it would add states, notifications and a queue before there is any volume to justify them.
- **Reuse the customer reversal path with an operator override** — one endpoint, one flow,
  authority as a parameter. Rejected because the two flows agree on almost nothing that
  matters: one calls a provider and the other must never, one is gated on the window and the
  other ignores it, one takes no money facts and the other requires them. Sharing the surface
  would mean a flag deciding whether a provider gets called, which is precisely the decision
  that must not be a flag. They share what they should — the repository primitive that voids the
  sale and restores `sold_count` in one transaction, and the Sale Voided email.
- **Allow it on imported sales too** — an operator can plainly see an imported row and might
  want it gone. Rejected: no money for it ever passed through the platform, so `refunded_amount`
  and `platform_fee_kept` would be assertions about a transaction the platform never had, and
  the Sale Import undo already removes them at the batch level.
- **Default `platform_fee_kept` to false, or pre-fill `refunded_amount` with the collected
  amount** — fewer keystrokes on the overwhelmingly common case. Rejected because both defaults
  are answers to questions only a person can answer, and a pre-filled field is a field nobody
  reads. The form asks; the operator states.
- **Record the refund as a negative Payout** — the ledger already carries operator-recorded
  money movements, and a negative one would drop the Withdrawable Balance by the same amount
  with no schema change. Rejected because it leaves the Ticket Sale `active`: the tickets stay
  claimed, the capacity never returns, and the buyer's Customer Area still shows a live
  purchase. The books would balance while the sale lied.
- **A `refunded` sale status distinct from `reversed`** — more faithful to what happened, and
  rejected because every consumer in the system asks one question of a sale, whether it counts,
  and a second terminal status makes each of them ask two. Provenance already says how a sale
  was reversed, without splitting the status.
- **Keep the platform fee automatically, or return it automatically** — either rule removes a
  decision from the form. Rejected in both directions: the fee's fate is genuinely negotiated
  case by case (a goodwill refund waives it, a buyer's error usually does not), and a rule would
  make platform revenue state something nobody decided.
- **An un-reverse for the mistaken marking** — the obvious safety net. Rejected because it
  cannot restore what the marking spends: capacity released is resellable immediately, and the
  Sale Voided email has already reached the buyer. A wrong marking is an operator-with-database
  incident, the same accepted posture as the money incidents ADR 0018 already records.
- **A `sale_reversals` audit table for the memo** — the natural home for four columns that
  belong to exactly one of three routes, and the thing ADR 0018 deferred. Deferred again for the
  same reason: the columns sit on the sale under a constraint that keeps them empty on every
  other route, and the Payout precedent — the record remembers who recorded it — is already the
  shape being copied.

## Consequences

**A recorded refund and a real one can disagree, and nothing in the system will notice.** An
operator who marks a sale without having refunded anybody produces a buyer with no money and no
tickets, and the platform's books will look immaculate. This is the cost of believing the
assertion, accepted for the same reason Payouts are believed, and the note and the operator's
identity are the whole of the forensics. It is the third case that would benefit from the
reconciler ADR 0012 deferred.

**Platform revenue is no longer derivable from active sales alone.** Anyone computing what the
platform earned must now add the fees kept on reversed sales, and anyone reading the figure has
to know that. The term is defined in one place and disclosed on the dashboard when non-zero,
but the invariant that made ADR 0018's aggregates so cheap — every money figure filters
`active` — is now true with an exception, and the next money figure added will have to decide
which side it is on.

**A customer reversal and an Operator Reversal with the fee returned are indistinguishable in
the revenue total.** Only kept fees diverge. This is intended, and it means the kept-fee
disclosure is the only place the difference is visible.

**Negative Withdrawable Balances become ordinary.** ADR 0018 anticipated one; this feature
manufactures them, because a refund requested weeks later is a refund on a sale that was almost
certainly paid out. The balance says so and the platform collects the difference off-platform,
but no surface yet explains a negative number to the Organization staring at it.

**The Organization loses a sale without acting and without being told**, exactly as under ADR
0018 — except here the Organization usually asked for it. The reversed count on the Event's
sales page is still the only signal, and an organizer who did not initiate the request will find
the sale reversed by the platform with no message about it.

**`refunded_amount` is recorded and read by nothing.** It exists on the sale's detail and in
nobody's arithmetic. That is deliberate — the bank balance has never been a system figure, and
PayPhone's own commission is unmodelled — but it means the field's accuracy is untested by any
total, and the first cash reconciliation view will be the first time anyone finds out whether
operators filled it in honestly.

**Free Online Sales are now releasable at any time**, which closes the rest of the hole ADR 0017
recorded and ADR 0018 half-retired: a claimed free ticket no longer depends on the buyer acting
before 20:00. Deliberate exhaustion is still untouched — an actor who never asks for a release
still holds the capacity — and the real control remains a per-email claim limit.

**Support gets a self-serve path and the database gets left alone**, which is the whole point.
The manual correction that used to be the only repair is now an authorized, attributed,
constrained action that restores capacity and notifies the buyer, and no off-book adjustment
sits behind any figure on the Operator Dashboard.
