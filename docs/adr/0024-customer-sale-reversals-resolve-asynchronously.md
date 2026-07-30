# Customer Sale Reversals resolve asynchronously through a Reversal Request

## Context

ADR 0018 calls PayPhone's `Reverse/Client` synchronously, inside the buyer's request, under an
advisory lock, and writes nothing before the call. `payPhoneTimeout` is 10 seconds. When PayPhone
takes longer than that — which it does — the request fails, the buyer is shown an error, and the
system has learned nothing: it does not know whether the money left.

Because nothing was written before the call, there is nothing to come back to. The Ticket Sale
stays `active`, `sold_count` stays consumed, and the Organization's dashboard shows revenue that
may no longer exist. This is the same stranded state ADR 0018 named as the one the system can
never leave — reached by timeout rather than by a failed local write.

In practice the system already recovers from this, and the recovery mechanism is **the buyer**.
They press Undo again; the second call is answered `400 {"errorCode":24}` ("ya se encuentra
cancelada"); the 2026-07-28 amendment reads that as success; the sale reverses. A buyer who does
not press again leaves the sale stranded indefinitely.

That amendment is what makes everything below possible. ADR 0018's "never retry Reverse" rule was
written when a timeout was an unrecoverable ambiguity — re-posting risked returning the money
twice. Since errorCode 24 became a receipt, re-posting is no longer a gamble but a **question with
a definite answer**:

| Answer                    | Meaning                                       |
| ------------------------- | --------------------------------------------- |
| `true`                    | we just reversed it                           |
| 4xx `errorCode 24`        | already reversed — the money has already left |
| other 4xx (20, 40, 42)    | genuinely refused; nothing happened            |
| 5xx or timeout            | still unknown; ask again later                 |

## Decision

A Customer's ask to undo their own Online Sale is recorded as a **Reversal Request** before
PayPhone is called, and the platform — not the buyer — pursues it to a definite answer.

**The synchronous path is unchanged for every answer that is definite.** Most reversals answer
well inside 10 seconds and those buyers keep their instant "undone"; a definite 4xx refusal stays
an immediate error, because nothing happened and there is nothing to resolve later. Only a
**timeout or a 5xx** — the genuinely unknown answers — leave the Reversal Request `in_flight` and
return `202 Accepted` with `status: "pending"`.

**A pending reversal changes nothing about the money or the tickets.** The Ticket Sale stays
`active` and its capacity stays consumed, because the money has not been confirmed returned. The
buyer is told we are processing it, not that it is done. This is the whole reason the optimistic
shape stays rejected: ADR 0018 refused local-commit-then-roll-back because released capacity
cannot be reclaimed, and that reasoning is untouched.

**A Reversal Request lives in its own table**, `sale_reversals`, one mutated row per reversal with
an attempt count and last error. It is not a `ticket_sales.status` value: seven aggregate queries
filter `ts.status = 'active'` — affiliate commission, the operator dashboard, the Withdrawable
Balance, event sales, capacity, import undo, and `ReverseSales` itself — and during pending the
sale must keep counting in every one of them. A partial unique index,
`UNIQUE (ticket_sale_id) WHERE status <> 'refused'`, makes a second live Reversal Request per sale
structurally impossible; a second press returns the existing row's pending state rather than
creating one. `refused` is excluded because a refusal means nothing happened, so a buyer still
inside the Window is entitled to a genuinely new ask.

**A Reversal Request ends in one of three states**, and `needs_attention` is the one with no
analogue today:

- **`succeeded`** — a probe answered `true` or errorCode 24. The Ticket Sale flips to `reversed`
  through the same `ReverseSales` primitive, capacity returns, the void notice sends.
- **`refused`** — a probe answered a definite 4xx. The Ticket Sale stays `active` and the buyer is
  emailed the bad news we deferred.
- **`needs_attention`** — 24 hours of unknown answers. It exists so that a permanently unwell
  PayPhone produces a queue somebody can read rather than a row retrying forever.

**The Reversal Reconciler** drives this: a Cloud Scheduler tick against an authenticated
`POST /api/v1/internal/reversals/drain`, backing off 10s, 30s, 2m, 5m, 15m, then every 30m with
jitter. The buyer's own stuck request is additionally drained opportunistically when they load the
Customer Area, so the common case resolves in seconds. Every actor takes the existing
`pg_try_advisory_lock(18, hashtext(saleID))` before probing, so the buyer's press, the poll and the
tick can never overlap on one sale; a contended drain skips that row and tries next tick.

**A tick claims one Reversal Request at a time and probes it before claiming the next**, and stops on
a budget of its own that expires before Cloud Scheduler's 90-second attempt deadline — the binding
constraint, since Scheduler abandons the request long before Cloud Run's 300-second timeout would.
Claiming the whole batch up front would have been the obvious shape and is the wrong one: a run that
stopped at its budget would leave the rest of that batch claimed and invisible to every other actor
until the claim expired, so stopping early would have made those buyers wait *longer*. Claiming as it
goes means everything a run does not reach is exactly as due as it found it. The budget must be the
innermost of the three deadlines because it is the only one that stops a run politely: the other two
abandon the request where it stands, and the write recording what PayPhone just said fails on the very
context that was cancelled.

**A retry is the continuation of an authorised reversal, not a new one — so it never re-checks
eligibility.** The Reversal Window governs when a Customer may *ask*; it says nothing about how
long the platform may take to find out what happened. Writing the Reversal Request is the record
of the authorisation, and `EligibilityAt` gates writing it, not calling PayPhone. A reconciler
that re-checked the Window would refuse its own retry at 20:05 for a request made at 19:58 —
permanently abandoning the case where the money has *already left*, which is the worst outcome the
whole design has to offer. Two timestamps keep this unambiguous: `sale_reversals.requested_at` is
when the buyer pressed and must sit inside the Window; `ticket_sales.reversed_at` stays what it is
today, the moment we learned it succeeded.

The Reconciler also finishes the **`SALE_REVERSAL_NOT_COMMITTED`** case — PayPhone said yes and
our local write failed — which is today a log line and a hand-repair. It is a Reversal Request in
state `succeeded` whose local commit never landed — recorded as a column on the request itself rather
than asked of the sale, because a queue every tick reads must be the size of what is broken and not of
every reversal ever completed — and the loop retries that commit until it lands: the commit alone, on a
path with no provider call in it, because the answer is already
recorded and a second Reverse against a payment that has already been reversed is the double refund
everything above is arranged to avoid. A commit that will not land within the day ends as an
Unresolved Reversal like the rest, but a differently shaped one: the money is known to have gone
back, so there is nothing to look up on PayPhone's dashboard and the Operator's move is simply to
void the sale (ADR 0019).

## Considered options

- **Optimistic commit: mark the sale reversed immediately and confirm later.** The instant answer
  the buyer wants, and no new pending state anywhere. Rejected for the reason ADR 0018 already
  rejected it: a probe answering errorCode 42 or 20 means the money never moved, and by then the
  capacity has been released and possibly resold, so the buyer is told they were refunded when
  they were not and there is no way back.
- **Just raise the timeout to 30–60s and retry once in-request.** No new state, no new table, no
  scheduler. Rejected because it makes the unknown rarer without eliminating it, while holding an
  HTTP request and a dedicated Postgres connection open for a minute — and the case it still fails
  is exactly the one we are here to fix.
- **A third `ticket_sales.status` value.** The obvious shape, and wrong: it silently drops a
  pending sale out of all seven `status = 'active'` aggregates at the very moment the money is
  still ours.
- **Columns on `ticket_sales` instead of a table.** Cheaper, and consistent with migrations 031 and
  032. Rejected because the row has to exist *while the answer is unknown* and carry a retry
  schedule and an error — that is a record with a lifecycle, not two more columns on a table that
  already carries six reversal ones.
- **A generic `jobs`/outbox table.** More reusable, and a much larger commitment: the backend has
  no background execution of any kind today. A purpose-built table is also a queue you can
  `SELECT` from during an incident.
- **Cloud Tasks, one task per stuck reversal.** The natural GCP fit for retry-with-backoff.
  Rejected because it splits the retry state between Postgres and a queue's delivery config, and
  the question an operator asks — "what is stuck, and why" — should be answerable in SQL.
- **In-process goroutine ticker.** Zero new infrastructure. Rejected on the deployment: with
  `api_min_instances = 0` there is frequently no instance running and CPU is throttled outside
  request handling, so a reversal stalling at 21:00 on a quiet evening would not be retried until
  someone visited — the one case that matters, given a 20:00 cutoff.
- **Traffic-driven draining alone**, in the `ExpireStalePayments` style. Consistent with the house
  pattern and kept as an accelerator, but rejected as the only driver for the same evening-quiet
  reason.
- **Generalising into a `payment_operations` table that also reconciles the checkout's
  take-the-money-lose-the-sale case.** Structurally the same failure, and ADR 0012 deferred a
  reconciler for exactly it. Rejected for now: checkout's recovery semantics differ (capacity never
  held cannot be un-sold), and a second case should earn the generalisation rather than be
  predicted.

## Consequences

**This is the project's first scheduled background execution and its first internal-scoped
endpoint.** Nothing in the backend has ever run outside a request — no goroutine workers, no
tickers, no cron. The endpoint is deliberately safe to curl by hand, so the incident runbook
exists before the automation does.

**`api_min_instances = 0` stops being true in practice.** A tick every minute is a request every
minute, so the API never idles long enough to scale to zero: a warm instance and its database
connection pool are now held around the clock rather than only while somebody is on the site. That
is a real change to what this deployment costs and to how many Cloud SQL connections stand open
against a shared-core instance, and it is the price of the deployment argument above — the whole reason an
in-process ticker was rejected is that nothing is running at 21:00 on a quiet evening, and the fix
for that is something that keeps an instance running. The cadence is a variable
(`reversal_reconciler_schedule`), so the trade can be made differently: at five or ten minutes the
API would idle down between ticks and cost less, and a buyer whose reversal timed out would wait
that long for the platform to ask again — which the opportunistic Customer Area drain already
covers for a buyer who stayed, and nobody covers for the one who closed the tab. A minute is chosen
because that person is the reason this exists.

**ADR 0018's "the provider is called first, and nothing is retried" is amended, not overturned.**
The provider is still called before anything local is written. What changes is that a *silence*
from the provider is now pursued rather than reported, which the errorCode 24 amendment made safe.
A definite refusal is still final and still unexplained to the buyer.

**On the `refused` path we break a promise.** We tell the buyer we are processing their refund and
later tell them we could not do it. That is worse than today's instant error for that buyer. It is
accepted because it only occurs on the timeout-then-definitely-refused path, and the alternative
promise — telling them their money is back when it is not — is the one ADR 0018 refused.

**`needs_attention` is a queue with no notification.** It is loudly logged and queryable, and
nothing tells anybody it happened, because the system has no operator- or organizer-facing
notification channel of any kind — the gap ADR 0018 already recorded. The manual resolution path
does exist: an Operator who confirms on PayPhone's dashboard that the money left records it as an
Operator Reversal (ADR 0019).

**The whole design rests on one external assumption**: that PayPhone answers a repeated Reverse
with errorCode 24 rather than refunding twice. One production incident and the existing manual
buyer-retries-and-it-works behaviour both support it, and it is confirmed by hand in the sandbox
(`docs/manual-verification-payphone-reversal.md`) rather than only against our fake. If it were
ever false, the Reconciler would double-refund at scale rather than once.

**Free Online Sales, Operator Reversals, Sale Import undo, Prepare and Confirm are untouched.** A
free sale calls no provider and stays fully synchronous, with no Reversal Request at all. Confirm
carries the same timeout exposure and the same never-retry rule, and keeps them: its documented
retry is the buyer's return page re-calling it.

**Partial reversal is still absent** and this changes nothing about that.
