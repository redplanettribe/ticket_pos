# Zero-total checkouts settle without a Payment Provider

## Context

The catalog has always accepted a Ticket Type priced at zero (`price_cents` "must be zero or greater"), but the checkout path assumed money. A cart containing only Free Ticket Types reached PayPhone's Prepare with `amount: 0`, which PayPhone refuses outright — *"El Amount debe ser un entero entre 1 y 99999999"* — and the refusal surfaced to the buyer as `500 INTERNAL_ERROR`. It ran for a week on a live event with a free tier alongside paid ones: paid selections checked out, free ones 500'd. The prefill-stripping retry (#104) fired first and failed identically, because the payload was never the problem.

So the platform had a product concept it could not sell: the catalog permitted free tickets, and nothing downstream of the price could handle one.

## Decision

A checkout that totals zero is settled by the platform itself, in the begin-checkout request, without contacting a Payment Provider. It creates a Payment that is `approved` the instant it is created, commits its Ticket Sale through the same spine an approved PayPhone Payment uses, and records `payment_method = 'free'`. There is no pending state, no Capacity Hold, no provider redirect, and no return leg — the buyer never leaves the Storefront.

`Payment` is widened to match: it is a Customer's attempt to *settle a checkout*, not specifically to pay a Provider. The invariant that a Ticket Sale exists only for an approved Payment is unchanged; approved now means the checkout is settled, not that money moved.

A cart is free only when it totals zero. Any paid ticket in the cart makes the whole checkout a provider checkout, priced and redirected exactly as before.

## Considered options

- **Forbid a zero price** — require `price_cents >= 1` at Ticket Type creation. Cheapest fix and honest about being a payments platform, but it breaks a published event that already carries a free tier, and it tells Organizations running free community events that this system is not for them.
- **A $0.01 minimum** — free tiers price at one cent, and nothing else changes. Forces a real Organization to charge for something it means to give away, and sends every free claim through a card form for a penny.
- **Record the Ticket Sale directly, with no Payment behind it** — keeps `Payment` meaning exactly what it said. Rejected because the sale-commit spine carries the capacity check under row locks, the Customer upsert and its self-asserted Tax ID rules, the confirmation reference, and the Sale Confirmation email; a second path would have to reproduce all of it, and every future change to sale recording would have to remember both.
- **A `Free Claim` as a concept beside `Payment`** — the more evocative vocabulary, rejected as a contradiction: we decided a free claim *is* a Payment, and a sibling noun would say otherwise.

## Consequences

`Payment` no longer implies money moved. A reader who sees an `approved` Payment cannot assume a Provider was involved, and reporting that sums Payments must keep treating `amount_cents` as the authority on value rather than the existence of the row.

**A Tax ID is still required at zero** (ADR 0016), even though a `$0` sale has nothing to declare to the SRI. This is deliberate: excusing zero-amount sales would let an Organization price on-platform at zero, collect cash at the door, and produce an attendee list with no identification — precisely the hole ADR 0016 was written to close. It also keeps one rule for the whole checkout form, so adding a paid ticket to a free one never grows a required field under the buyer's hands.

**Free claims are bounded by capacity alone.** With no card there is no deterrent, a claim commits immediately rather than sitting behind a lapsing hold, and nothing in the system can reverse an Online Sale — so one determined actor can permanently exhaust a free tier. Accepted knowingly: the same attack exists on paid tiers with money as the only friction, the organizer's recourse is to raise capacity, and the real control (a per-email claim limit) is a feature with its own decisions rather than part of this fix. Recorded here so it is found rather than rediscovered.

`ticket_sales.payment_method` gains `'free'` in its CHECK constraint. `payments.provider` records `'free'` and is unconstrained.
