# Payout Requests: the Organization asks, the Platform Operator answers

## Context

A Payout has always been a record rather than an instruction (ADR 0014): the platform settles
with an Organization off-platform, and a Platform Operator writes down that it happened. What
was never modelled is the half that comes first. An Organization wanting its money asks for it
in a WhatsApp thread, and the operator answers by asking back — which bank, which account,
savings or current, whose cédula goes on the factura — because the platform stores none of it.
Organizations have a name, a slug, a currency and a logo, and nowhere to say where they are
paid.

That conversation is expensive twice over. Every settlement re-collects details that never
change, retyped by hand into a banking app, which is the highest-stakes typo surface the product
has. And a request that arrives in a thread can be missed, answered twice, or answered by two
operators who did not know about each other.

There is also a hazard nobody was defending against. The Withdrawable Balance counts every
active Online Sale, including one recorded ten minutes ago whose buyer can still undo it before
20:00 (ADR 0018), and including one with a Reversal Request in flight, which stays `active`
throughout by design (ADR 0024). Settling against that figure means wiring out money the buyer
may pull back — and the platform's own funds may not have cleared PayPhone yet either. Paying
against a number that can still move is the thing the platform most wants to stop doing.

## Decision

An **Org Admin may submit a Payout Request** — an amount, an optional note, and the bank details
to pay it to — which appears in a queue on the Operator Dashboard, where a **Platform Operator**
answers it by transferring the money and recording the Payout.

**A request moves nothing and counts for nothing.** No aggregate, no balance and no revenue
figure knows requests exist. The Withdrawable Balance stays exactly what it has always been —
Net Proceeds minus recorded Payouts — and requests are a queue *over* that ledger, never a
second one. A system with two places money can be said to have moved has no answer to which one
is true.

**Where an Organization is paid becomes a stored fact: the Payout Profile.** Bank name, account
type, account number, the name on the account, and the Organization's Tax ID for the factura.
The bank is free text, because Ecuador has dozens of cooperativas and a curated list's only real
effect is locking out the one its author had not heard of; a human reads this field and types it
into their own banking app, where "Banco Pichincha" serves exactly as well as an enum value. The
account number is digits only and stored as text, because leading zeros are real and
load-bearing. The account type is `ahorros` or `corriente`, in Spanish, because that is what the
receiving bank's form says — omitting it entirely would mean every transfer starting with a
message asking which one, the precise friction this feature exists to delete.

**The profile's Tax ID is `cedula` or `ruc`, never `passport`.** The existing three-value
vocabulary (ADR 0016) answers a different question — how to identify a buyer on a sales
declaration, where a foreign tourist's passport is ordinary. This one asks who is being invoiced
and wired to, and a passport holder has no Ecuadorian bank account to receive it. Same
validator, `platform.ValidateTaxID` with its check digits and province prefixes; narrower
vocabulary, because narrower is truer here.

**Every request snapshots the profile.** The profile says where to pay today; the snapshot says
where an operator was told to pay six months ago, and no later edit can rewrite it. This is the
house rule already applied to fee amounts on a sale line (ADR 0014) and to the buyer's Tax ID
(ADR 0016), and it matters more here than in either: this is a money instruction, and an
Organization that changes banks must not retroactively change the account a completed transfer
was aimed at.

**The request form is the profile editor.** Fields pre-filled from the profile, editable in
place, and saving the request saves the profile too — an organizer correcting an account number
on a request means their account number changed, and having to correct it in two places is worse
than the alternative. A complete profile is required; the endpoint refuses without one, with
field-level errors, because a request an operator cannot action is the support thread this
feature was meant to remove.

**A new figure bounds the ask: the Payable Balance.** The same arithmetic as the Withdrawable
Balance over a subset of the sales — those recorded before today in `America/Guayaquil`, with no
Reversal Request still open. Waiting for the day to turn is what makes a sale safe: the Reversal
Window closes at 20:00 Ecuador time on the day of purchase at the very latest, and only earlier
if the Event starts first, so a sale from a previous day is one its buyer can no longer undo.
The settlement lag *subsumes* the window rather than sitting beside it, and the rule is one
condition rather than two. It is keyed on `ticket_sales.created_at`, which is the instant of
payment approval — a Ticket Sale exists only for an approved Payment — so it needs no join to
`payments`, which carries no `approved_at` column anyway, and it works unchanged for free Online
Sales that never had a provider at all (ADR 0017).

Cleared sales are a subset of all sales, so **Payable ≤ Withdrawable, always**, including when
both are negative. Both figures are shown to the Organization, phrased so the gap explains
itself, because an organizer who sees only the smaller number will ask why and the answer needs
both.

**The cap binds the Organization and not the operator.** A request above the Payable Balance is
refused; a Payout above any balance is recorded as readily as it ever was. The asymmetry is the
whole point and it follows from what the two things are. A Payout is a record of money that
already moved, and an endpoint refusing to record a completed bank transfer makes the books lie
to protect a workflow — the unconditional posture ADR 0019 depends on, untouched. A request is a
claim about money that has not moved, so refusing an impossible one costs nothing and is kinder
than letting an organizer submit a number a human will decline three days later.

**The cap is checked when the request is made and never again.** The Payable Balance moves; an
organizer who asked for $900 against $1,000 payable may be looked at when it is $700. The
operator sees both the snapshot and the live figure and exercises judgement, because they are
the one at the bank. Request-time validation is a guardrail, not an invariant.

**One outstanding request per Organization**, enforced structurally by a partial unique index, in
the shape ADR 0024 already uses for Reversal Requests; a second submission returns the existing
one rather than erroring blindly. Without it the cap means nothing — three individually valid
$1,000 requests against $1,000 payable would put the arithmetic the system refused to do back on
the operator.

**The states are `pending → paid | declined | cancelled`, and there is no `approved`.** (Amended
— see *Amendment: the `processing` state* below, which adds `processing` and `failed` and explains
why they are not the `approved` this paragraph refuses.) The
operator transfers the money and then records it, exactly as they always have. An `approved`
request is a promise the platform then has to keep, and it would need its own chasing, its own
notifications and its own aging report. A decline requires a reason, shown to the asker: a queue
that swallows requests silently generates the support thread it was built to prevent. All three
end states are final; a pending request cannot be edited, only cancelled and re-asked, which is
what keeps `pending` genuinely singular.

**Fulfilment is a compare-and-swap, not a lock.** One transaction inserts the `payouts` row
exactly as the existing operator path does, then updates the request `WHERE id = $ AND status =
'pending'`. Zero rows affected rolls the whole thing back and returns an error naming who got
there first. Nothing is ever held, so no operator can leave a request stale by opening a tab and
going to lunch, and the loser of a race learns what actually happened instead of getting a
generic conflict. It makes the request path strictly safer than recording directly, which is the
point.

**Recording a Payout directly for an Organization with a pending request warns loudly and
permits it anyway** — at the same weight as the existing over-balance confirmation, offering to
fulfil the request instead. Blocking it would be the same mistake as capping the operator: the
transfer happened whether or not the system approves of how it was initiated. Silently
auto-closing the request would be worse, because a $200 direct Payout and a pending $900 request
are probably not the same event.

**The fulfilment form pre-fills the Payout amount from the request.** This is a deliberate
departure from ADR 0019, which rejected pre-filling `refunded_amount` on the grounds that a
pre-filled field is a field nobody reads. The distinction is who the number belongs to. A refund
amount is an assertion only the operator can make about a transfer whose size they chose; a
payout amount is a figure the Organization already stated and the operator agreed to by
transferring it. Pre-filling a number somebody else committed to is not the same as inventing
one. The operator can overwrite it, the request keeps what was asked, and any divergence stays
visible forever.

**Three emails, and this is the platform's first organizer-facing notification channel** — ADR
0019 recorded that none existed. The operator allowlist is told a request arrived, because a nav
badge only works for someone who already decided to look, and a Friday-evening request otherwise
waits until somebody happens to click. The requesting Member — recorded as `requested_by`, an
email exactly as `payouts.recorded_by` is, so the record outlives their membership — is told when
it is paid and when it is declined, with the reason. One request, one asker, one reply; not every
Org Admin. Delivery failures are swallowed and never block the commit, as on every other notice
path in the system.

**The operator surface gains its second cross-Organization view.** A top-level queue, oldest
first, with a pending count on the nav. The per-Organization layout works everywhere else because
operators arrive already knowing which Organization they care about; a payout request inverts
that, since the request is the reason to open the dashboard at all. Oldest-first breaks the
newest-first house convention deliberately: this is a work queue, not a history, and the oldest
unanswered request is the one about to become a complaint. Requests also appear on the
Organization's detail page, beside the payout history and the balances, where they answer "has
this Organization been paid recently, and are they asking again?"

**Bank details are stored in plain columns, masked in list contexts, and never logged.** This is
the first financial instrument the database holds — PayPhone's redirect means no card data ever
touches the platform (ADR 0012) — but an Ecuadorian account number is not a credential: it is
handed to anyone who owes you money and cannot be used to pull funds. Application-level
encryption would be decrypted on essentially every operator read, with the key inside the same
trust boundary as the database credentials, buying complexity against a threat model where an
attacker holds the database but not the service account. Masking in the queue is worth it for a
different reason: that one screen shows every Organization's details at once and is the screen
operators will screenshot into support threads.

**Both tables live in the `sales` module.** A Payout Profile is meaningless except for payouts,
and putting it in `identity` would teach the module that owns organizations, members and sessions
what a bank account and a factura are, purely because the row hangs off an Organization —
ownership by foreign key rather than by meaning. `payouts.organization_id` already references
`organizations` from inside `sales`. `organizations` itself gains no columns, and `identity` and
`catalog` learn nothing; the operator service's existing `Money` interface grows the new
operations.

## Considered options

- **A Payout Request that carries no amount** — "settle my balance", paid out at whatever the
  figure is when an operator clicks. Rejected because the amount would then be one nobody agreed
  to: the operator has no stated target to check their transfer against, which is the one thing a
  manual bank transfer really needs. It also cannot express a partial ask, and an Organization
  mid-festival wanting some now and the rest after the Event is an ordinary case. Pre-filling the
  full balance is a UI decision that can change later; "the request has no amount" is a schema
  decision that cannot.
- **Bank details on the request only, with nothing stored on the Organization** — no profile, no
  snapshot question, no editing rules. Rejected because it makes an organizer retype an account
  number every month, and each request would have to re-validate details that never change.
- **A profile with no snapshot** — the request points at the Organization's current details.
  Rejected for the reason every other snapshot in this system exists: an operator opening a
  six-month-old request must see the account they actually paid, not the account that
  Organization has today.
- **Validating the cap again at fulfilment** — refuse to pay a request whose Payable Balance has
  since dropped. Rejected because the request was valid when made and the operator is the party
  standing at the bank; showing them both numbers respects that, and refusing on their behalf
  does not.
- **Holding funds until the Event has started or ended** — the industry-standard defence, and the
  only one that addresses the real commercial risk: an Organization taking the gate money for a
  show that never happens, leaving the platform to eat the refunds. Rejected for now as a
  pricing-and-trust policy rather than payout mechanics. It would block pre-event cashflow that
  small organizers genuinely need, and every transfer is manually reviewed anyway, so an operator
  can simply decline a request from an Organization whose Event is three months out. Manual
  judgement is the control until volume justifies a rule.
- **A flat maturation lag of several days** — safer-feeling than T+1. Rejected as a magic number
  defending against nothing the Event-completion hold would not defend against better, at the cost
  of holding organizers' money for no articulable reason.
- **An `approved` state between `pending` and `paid`** — an operator accepts, transfers later,
  records later. Rejected as ceremony, in the same terms ADR 0019 used to reject modelling the
  reversal request: it adds states, notifications and a queue ahead of any volume that justifies
  them, and an approved-but-unpaid request is a debt with a state name. (This rejection stands.
  The `processing` state added by the amendment below is a different thing: it describes a
  transfer that has already been submitted to the bank, not one an operator intends to make.)
- **A claim lock so two operators cannot work the same request** — the obvious guard against the
  worst failure this feature has. Rejected in favour of the compare-and-swap, which needs nothing
  held and cannot go stale. It is an honest narrowing rather than a fix: the CAS stops the second
  *record*, not the second *transfer*, so the refusal message tells an operator who genuinely
  transferred to record it directly, or the books would quietly understate what left the account.
- **Blocking direct Payouts while a request is pending** — tempting, and wrong for the same reason
  the operator cap is wrong. The bank transfer happened; a system that refuses to write it down
  has not prevented anything, it has only stopped knowing.
- **Auto-closing a pending request when a direct Payout is recorded** — silent and a guess. An
  unrelated settlement would become a fulfilment nobody agreed to.
- **Many pending requests per Organization** — a real queue the operator works down. Rejected
  because the cap would then have to be validated against the sum of outstanding requests, which
  is real complexity that drifts anyway, or else the queue is allowed to lie.
- **A curated dropdown of Ecuadorian banks** — reads more cleanly on the dashboard. Rejected: a
  bank list is a maintenance liability whose only real effect is rejecting the cooperativa that is
  missing from it, and a free-text bank name is exactly as useful to the human retyping it.
- **Confirm-by-retyping the account number** — the standard defence against a transfer typo.
  Rejected because this is a stored profile the organizer can see and edit, reviewed by a person
  before any money moves; double entry buys little and annoys much.
- **Application-level envelope encryption of the account number** — the reflex, and rejected
  above: decrypted on every operator read, key in the same trust boundary, defending a threat
  model that does not describe the risk.
- **Renaming the balances honestly** — the total becomes an `Accrued Balance` and `Withdrawable
  Balance` is redefined as the cleared figure, which is what the words actually mean. It is the
  more truthful option and was rejected on risk rather than merit:
  `withdrawable_balance_cents` is load-bearing across three ADRs, two API surfaces and the
  operator list, and a rename spends real risk in code that moves money to buy accuracy in a
  document. The glossary entry was amended instead, so `Withdrawable Balance` now says what is
  owed rather than what can be asked for.
- **Showing the Organization only the Payable Balance** — one number, no explaining. Rejected
  because an organizer who sold $1,200 today and is offered $900 will ask why, and the answer
  requires the other number.
- **The Payout Profile in the `identity` module** — it hangs off an Organization, and `identity`
  owns organizations. Rejected as ownership by foreign key: it would put the profile's validation
  rules on the far side of a module boundary from the request that snapshots them.
- **A new `payouts` module** — clean on paper. Rejected because the new surface shares the balance
  query, the fee snapshots and the `payouts` table with `sales`, so its first act would be
  reaching back into `sales` for everything it does.

## Consequences

**Nothing here defends against an Operator Reversal, and it cannot.** The Payable Balance keeps
the buyer from pulling money back after a settlement; an operator refunding a buyer weeks later
(ADR 0019) still drives the Withdrawable Balance negative, and this feature makes that more
likely rather than less, because a settled Organization is a settled Organization. The negative
balance says so and the platform collects the difference off-platform, and there is still no
surface explaining a negative number to the organizer staring at it.

**Event-cancellation risk is absorbed deliberately and controlled only by judgement.** An
Organization can be paid the gate money for a show three months out and then not run it. Nothing
in the schema prevents this; the control is an operator reading each request before transferring,
and it will hold exactly as long as operators keep reading.

**The compare-and-swap prevents a double record, not a double payment.** If two operators both
transfer at the bank, the second one's record is refused and the books understate what left the
account until somebody records it directly. The refusal message says so, which is the whole of
the mitigation.

**Requests and reality can disagree, as Payouts already can.** A fulfilled request asserts a bank
transfer nobody verified, on the same believed-because-a-person-said-so footing as every other
money record on the operator surface, and the same deferred reconciler (ADR 0012) is the same
eventual answer.

**Partial fulfilment is not modelled.** An operator who transfers less than was asked records what
moved, the request goes `paid` for the smaller amount, and the divergence between asked and paid
is visible on the request forever. Nobody is chased for the remainder except by the Organization
asking again.

**The staff app now sends email to organizers, which it never did.** One notification channel
exists where none existed; the next organizer-facing notice will find a path already cut, and the
temptation to widen it into a general notification system is a decision that has not been made.

**Two balances mean two numbers to explain,** on the organizer's payouts page, on the operator's
Organization detail, and in every future conversation about what an Organization is owed. The
invariant that they are ordered helps, but a reader who knows only one of the terms will read the
wrong one.

**The database holds bank account numbers,** which changes the blast radius of a database
compromise and of any future logging change. The rule that bank fields never appear in logs or
error messages is enforced by nothing but review.

## Amendment: the `processing` state

Recorded after the original decision shipped, and amending it rather than superseding it. The
Decision above stands in full except where this section says otherwise.

### Why

The original state machine assumed a bank transfer either happens or does not, resolving fast
enough that an operator can transfer and record in one sitting. PayPhone does not work that way.
A transfer submitted through it can take up to 48 hours to reach the Organization's account, and
it can come back rejected — most often because the account number is wrong.

That left an operator with no honest state to be in. They had submitted a transfer they could not
yet confirm, and the only ways forward were to record a Payout for money that had not arrived, or
to leave the request `pending` and lose track of the fact that a transfer was already out there.
The first corrupts the ledger; the second is how the same request gets transferred twice.

### What changes

**Two states are added: `processing` and `failed.`**

```
pending ──→ processing ──→ paid
   │            └────────→ failed
   ├──→ paid          (unchanged — an instant transfer skips processing)
   ├──→ declined
   └──→ cancelled
```

`processing` means an operator submitted the transfer and the bank has not confirmed it. `failed`
means the bank rejected it. Both `paid` and `failed` are terminal, as `declined` and `cancelled`
already were.

**`processing` is not the `approved` this ADR rejected.** The rejection above was of a state
recording an operator's *intention* to transfer — a promise the platform then has to keep, needing
its own chasing and its own aging report. `processing` records something that has already
happened, in the world, outside the platform's control. The distinction is not stylistic: an
`approved` request is resolved by the platform doing what it said it would, while a `processing`
request is resolved by the platform *finding out* what the bank did. Nothing chases it because
nothing can.

**The ledger is untouched until the transfer is confirmed.** No `payouts` row exists while a
request is `processing`. A Payout has meant *money that moved* since ADR 0014, and a rejected
transfer means it did not — so writing the Payout on submission would mean deleting or negating
ledger rows on failure, and inventing a voided-Payout concept every balance in the system would
then have to understand. The cost of this choice is stated plainly under Consequences below.

**`processing` counts as outstanding, and "outstanding" stops meaning "pending".** The partial
unique index widens to `status IN ('pending', 'processing')`. Without this an Organization whose
transfer has already been submitted could immediately ask again for the same money — the first request would no
longer be `pending`, and no balance has moved to stop them. This is the single most load-bearing
line of the amendment.

**Fulfilment's compare-and-swap generalises rather than moves.** The guard becomes `status IN
('pending', 'processing')`. Everything the original decision says about it is unchanged, including
that zero rows affected rolls the `payouts` INSERT back, and that it prevents a double *record*
rather than a double *transfer*.

**`processing` is one-way for both parties.** An Organization cannot cancel a request whose
transfer has been submitted, and an operator cannot decline one. Cancelling would let an organizer
withdraw an ask that is thirty seconds from landing, leaving a confirmed transfer with nothing to
attach it to and the Organization free to ask again for money already on its way. An operator who
enters `processing` by mistake marks it `failed` with a reason saying so.

**A `failed` request is not retried; the Organization asks again.** The bank details on a request
are a frozen snapshot and a request cannot be edited, so the most common failure — a wrong account
number — is unfixable inside the request it happened to. Reopening it to `pending` would have an
operator retrying against the same bad details forever. The organizer corrects their Payout
Profile and submits a fresh ask.

**`failed` is distinct from `declined` and the reason column is renamed to `resolution_reason`.**
A decline is a judgement a person made; a failure is a bank sending money back. Collapsing them
would tell an organizer with a typo that the platform refused them, and would make every
decline-rate figure count bank errors as refusals. The rename is a breaking change to a response
body, taken deliberately while the only consumer is the staff app, whose client is generated from
this server and deploys with it.

**`processing` records its own who-and-when, plus an optional transfer reference.** The operator
who submits and the operator who confirms need not be the same person, and `resolved_by` must keep
meaning who *ended* it. The reference is whatever the bank hands back, optional because it is not
always given synchronously and a required field an operator cannot fill is a field they will type
`-` into. It stays on the request rather than on the Payout, which is shared with the direct path.

**A request `processing` for more than 72 hours is flagged as stale on the operator queue,
computed at read time.** No column, no job, no automated state change. ADR 0024 built a reconciler
for Sale Reversals because there was an API to poll for a definite answer; here the operator learns
the outcome by looking at PayPhone or hearing from the organizer, and an automated job with nothing
to ask has nothing to do. The threshold is 72 rather than 48 because 48 is the advertised worst
case, and a flag that fires on healthy transfers stops being read.

**No balance is re-checked at any transition.** Unchanged from the original decision — request-time
validation is a guardrail, not an invariant — and it matters more now, because a request can sit
outstanding for three days rather than one.

### Consequences of the amendment

**An Organization's Withdrawable Balance overstates what the platform holds for up to 48 hours.**
Money genuinely gone from the platform's account has no `payouts` row until it is confirmed. This
is not a new class of inaccuracy — a `pending` request has always had it — but the window is now
days rather than hours, and it is the price of never recording a Payout for money that might come
back.

**A crashed or forgotten `processing` request is invisible to everything but a human.** There is
no reconciler and no timeout. The 72-hour flag is the only backstop, and it works exactly as long
as somebody reads the queue.

**`outstanding` and `pending` are now different words for different things,** in a codebase where
they were interchangeable for the whole of the original feature's life. Every future reader adding
a query about unanswered requests has to know which they mean; the glossary says so, and the partial
unique index is the definition of record.

**None of the copy this amendment adds is covered by a test that renders it.** The repository has no
component-testing seam — no jsdom, no Testing Library, no renderer for a React Server Component —
and this amendment deliberately did not introduce one, because a first component-testing setup is
its own decision and not a rider on a payout state. So the pure helpers are tested directly under
`node --test` (`apps/staff/lib/payout-requests.test.ts`, `payouts.test.ts`) and the payloads those
helpers read are tested end to end in Go, but the wiring between them is covered only by reading
it. Specifically, NOTHING ASSERTS:

- that the organizer's outstanding-request card renders the transfer sentence
  (`transferSentSentence`) when the request is `processing`;
- that the cancel button is really absent while a request is `processing`, as opposed to
  `isCancellable` merely returning false;
- that the failure banner's button really opens the Payout Profile editor;
- that the operator's mark-processing confirmation dialog really says no Payout is written and the
  ledger is untouched.

Each of those is a sentence a reader has to check by eye, and each would survive a refactor that
dropped it. The helpers below them and the API above them would both stay green. Whoever adds the
first component test to this repository should start here.
