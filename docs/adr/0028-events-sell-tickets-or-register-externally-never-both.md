# An Event sells Ticket Types or registers externally, never both

## Context

Some Organizations run Events whose sign-ups live somewhere else — Luma, Eventbrite, a Google
Form, the Organization's own site — because the Event is a free community meetup, because a
partner owns the registration, or simply because that is where the audience already is (#205).

Today the platform has no answer for them. Publishing hard-requires at least one Ticket Type, so
the Organization either invents a Ticket Type nobody should buy, leaves the Event in `draft` where
nobody can see it, or keeps the Event off the platform entirely and loses it from the Timeline,
the Organization page, and its own Storefront presence. The last is the expensive one: a real
Event disappears from discovery for no reason other than that the money changes hands elsewhere.

Letting such an Event publish is the easy half. The hard half is deciding what an Event *is* once
a second way of getting in exists — because every surface built so far assumes the answer is
"tickets". The publish gate counts Ticket Types. The Storefront listing card renders a minimum
price and a sold-out badge rolled up from them. The Event page renders a selector, a total and a
checkout. The In-Person, Import and Integration Partner sale paths all resolve a Ticket Type by
id. Whatever shape External Registration takes, it is these consumers that pay for it.

## Decision

**An Event is in exactly one of two registration modes. It sells Ticket Types, as every Event
does today, or it carries a Registration Link and sends its audience elsewhere to sign up. Never
both.**

The mode is a fact the Organization chose, held on the Event, and it is not derived from whether a
Registration Link happens to be set. The two are genuinely different facts — the mode is the
decision, the link is the URL — and keeping them apart is what makes the ordinary `draft` state
expressible: an Event that has decided to register externally but has no URL yet is an incomplete
draft, exactly as a draft with no start time is today.

The publish gate is extended rather than replaced. A ticketed Event still cannot publish with zero
Ticket Types; an externally registered Event cannot publish with no Registration Link, and reports
the Registration Link as the missing thing rather than ticket types. Everything else publish asks
for — name, slug, start time, timezone — is asked of both.

**The mode is free to change while `draft`, and frozen once `published`.** The Registration Link
itself stays editable while published, because a typo or a rescheduled registration page must be
fixable; it may not be emptied, since on a published external Event it is the only way in.

Because no Ticket Type exists, an external Event produces no Ticket Sale, and therefore no
Payment, Platform Fee, Fee IVA or Net Proceeds. It costs the Organization nothing and never
reaches the Withdrawable Balance, the Payable Balance or a Payout. The paths that would record a
sale against it — In-Person, Sale Import on either Sales Source, and the Integration Partner
endpoints — refuse it by name rather than failing as a ticket type that could not be found.

## Considered options

- **Coexisting: Ticket Types *and* a Registration Link on one Event.** Rejected because it puts
  two contradictory calls to action on a single page and leaves the Customer to guess which one is
  the real way in. Worse, it makes sold-out undefined: when the tickets sell out, nothing in the
  model answers whether the Registration Link should still work, and both answers are defensible —
  which is the sign that the question should not exist. Exclusivity is not a simplification of the
  coexisting design; it is the thing that makes the page answerable at all.
- **A link-out Ticket Type variant** — model the Registration Link as a kind of Ticket Type, so
  the Event keeps one shape and existing code keeps working. Rejected because existing code would
  *not* keep working: every consumer of a Ticket Type assumes a List Price, a capacity, a sold
  count, a Purchase Limit, a Promotion and Fee Handling, none of which a link has. It would also
  corrupt the rollups quietly rather than loudly — the minimum price and all-sold-out figures
  behind Storefront listing cards are computed across an Event's Ticket Types, so a priceless,
  capacityless row would drag a listing card's price to zero or its sold-out badge to a lie, on
  pages nobody would think to look at while making the change. A wrong card is a worse failure
  than a refused request.
- **Deriving the mode from the Registration Link's presence** — no mode at all, external means the
  URL is set. Rejected because it collapses "I have decided to register externally" into "I have
  finished typing", which makes the incomplete draft inexpressible and makes clearing a field a
  silent mode change.
- **Hybrid Events** — free RSVP externally alongside paid VIP tickets on the platform. Out of
  scope rather than rejected on principle, but it is not a reason to weaken exclusivity now:
  Organizations with that need have Free Ticket Types as a native answer.

## Consequences

**The invariant spans two tables, so it cannot be a database constraint.** The mode lives on the
Event and the Ticket Types live in their own table; no CHECK constraint can see both. It is
therefore enforced in the catalog service, on both sides and symmetrically — creating a Ticket
Type on an externally registered Event is refused, and switching an Event to external while any
Ticket Type still exists is refused. Two guards, because a single one leaves the other door open.
Anyone adding a third way to create a Ticket Type, or a second way to set the mode, is adding a
place where this invariant can be broken, and the database will not catch it.

**Mode is frozen at publish, in both directions, but for different reasons.** Ticketed → external
would orphan real Ticket Sales: Customers left holding Sale Confirmations for an Event whose page
no longer mentions tickets, while capacity, Net Proceeds and the Withdrawable Balance keep
counting those sales. External → ticketed risks nothing in itself, but it passes through the exact
state the publish gate exists to forbid — published with zero Ticket Types, which renders as an
empty selector with a zero total and a disabled button, on a card showing no price and not marked
sold out. It is frozen for that reason, which is stricter than the hazard requires; the relaxation,
if it is ever wanted, is well defined — allow it while published provided the mode change and the
first Ticket Type commit in one transaction. Since the Event status machine has no unpublish
transition, the escape hatch for a genuine change of mind is a new Event, and the refusal says so
rather than leaving an organizer hunting for a button that does not exist.

**The degenerate state is one publish gate away.** A published Event with zero Ticket Types in
`tickets` mode is the broken page described above, and it is the publish gate alone that prevents
it — the sold-out rollup lands on "not sold out" only by accident, because a boolean AND over an
empty set is NULL. This feature does not introduce that state and the mode freeze is what keeps it
from doing so. Anyone touching the publish gate should know it is load-bearing.

**Refusals must say which rule they hit.** In-Person Sale, Sale Import and the Integration Partner
sale paths would all fail on an external Event anyway, since each resolves a Ticket Type that does
not exist — but they would fail as a lookup miss, sending a staff member, or worse an Integration
Partner's program, hunting for a data problem that is not there. They check the mode early and
refuse with a dedicated error code naming External Registration, so a caller can handle the case
instead of retrying a lookup that will never succeed.

**Exclusivity is what lets every other surface stay simple.** Because a mode is one thing or the
other, the Event page swaps its whole tickets section for a single Register call to action rather
than merging two, the listing card fills its price slot with a registration label rather than
showing both a price and a link, and sold-out becomes explicitly inapplicable rather than
accidentally false. None of those simplifications survives the coexisting design, which is the
strongest argument for this one.

**Nothing changes for a ticketed Event.** The default mode is `tickets`, every existing Event
publishes and sells exactly as before, and adopting External Registration is a choice an
Organization makes per Event rather than a migration anyone lives through.
