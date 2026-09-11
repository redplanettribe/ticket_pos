# A Ticket Type closes on the Storefront's clock alone, judged once at begin-checkout

Adds the Sales Cutoff: an optional closing instant on a Ticket Type, after which the Storefront
stops selling it.
Sits beside [ADR 0021](./0021-promotions-time-boxed-price-override.md),
whose window idiom it borrows, and beside
[ADR 0025](./0025-purchase-limit-per-customer-keyed-on-the-customer.md), whose
restraint about what a catalog constraint may refuse it follows. It adds no flag, no state column
and no lifecycle.

## Context

A Ticket Type has had exactly one way to stop selling: run out. Capacity is the only thing that
has ever closed a door, and it closes it on a fact about stock rather than a decision about time.
An Organization that wants online sales to stop on Friday at six — so the door list can be
printed, so the caterer can be told a number, so the last hour belongs to the box office — has had
to delete the Ticket Type, which takes its sales history with it, or lower its capacity to what has
already sold, which tells every visitor the Event is full when it is not.

Meanwhile a Ticket Type already carries one time-boxed thing. A Promotion has an optional start
and a required end, read in the Event's timezone, evaluated half-open against an injected clock and
never persisted as a state. That machinery exists, it is understood, and the temptation is to
generalise it into a full sales window.

## Decision

**A Sales Cutoff is a closing instant and never an opening one.** A Ticket Type is on sale from
the moment its Event is published until its cutoff, and "not yet on sale" is a state this platform
does not have. An opening date is a different feature with its own question set — whether the type
lists at all, whether it prices the Event's "from" figure, whether an Event of pre-open types is
sold out — and every one of those answers is trivial here only because the type has been visible
and sellable all along. Nothing in this decision reads a start, so one can be added later without
disturbing it.

**It binds the Storefront and nothing else.** A Sale Import, a Manually Recorded Sale and a Sale
Correction's replacement are never refused by it: they record acts that already happened, usually
before the cutoff, and a correction of an old sale must stay possible for as long as the sale
exists. A future door route is not refused either — closing online sales early is *how* the box
office is given its turn, and staff standing in the room are the authority there. This is a
deliberate departure from capacity and the Purchase Limit, both of which do refuse import rows.
Capacity refuses them because a seat is physical and cannot be sold twice; a closing time is a
decision about a shop window, and a window that has shut says nothing about what happened while it
was open.

**It is judged once, at begin-checkout.** A Payment already under way settles on the terms it
started on, exactly as the price it quoted does. The alternative is taking money at the provider
and refusing the sale on the way back, which leaves a Payment to reverse and a buyer to apologise
to. The cost is accepted and real: a Payment may settle after the hold window has lapsed, so a
Ticket Type can gain a sale hours past its cutoff, and the Sales list will show it without
explanation.

**Closed is derived, never stored.** It is the comparison of the service's clock against the
cutoff, in the Event's timezone, the way a Promotion's liveness already is. There is no state
column, no scheduled job that closes anything and no moment at which a row changes to say so — so
clearing the cutoff or moving it forward reopens sales immediately, and that is the undo. Nothing
about the value is validated: a cutoff in the past is the intended way to stop selling something
right now, and a cutoff after the Event starts is a workshop selling at its own door.

**Closed is not sold out, on any surface.** They get different words, different refusal codes and
different treatment, everywhere, because they invite different behaviour from the reader: "they
are gone" ends the conversation, "we stopped selling" invites an email asking you to reopen. An
Event whose every Ticket Type has closed therefore reads as closed and not as sold out — telling a
half-empty room it is full is a claim the Organization has to answer for.

## Consequences

The derived public figures narrow to the Ticket Types still open in time. The "from" price stops
counting closed types, which is the rule ADR 0021 already wrote for Promotions applied to a wider
question, and the listing card gains a third state beside sold out. A mixed Event, where some types
are closed and the rest are exhausted, reads as sold out: it is the more informative statement.

Both apps rank the states identically — sold out, then closed, then limit reached — and a
disagreement between them is a bug rather than a matter of taste. The staff card keeps stating the
configured cutoff whatever state is winning, which is the only catch for a typo in the year, since
nothing validates the value.

No feature flag. The column is nullable and unset on every row that exists, so the feature is inert
by construction until an organizer types a date, which is a stronger guarantee than a switch
somebody can open by mistake.
