# The Storefront publishes Tickets Sold, floored, and calls them tickets

> **Status: partially superseded by [ADR 0072](./0072-the-storefront-calls-tickets-sold-going-and-states-it-on-every-card.md).** The wording ("tickets sold") and the Event-page-only scope are overturned there; the figure, the floor of 5 with null beneath, the silence on External Events and the absence of any Organization control stand. Nothing in this ADR was built before 0072 replaced it.

## Context

A Customer weighing an unfamiliar Event wants to know whether anybody else is going. The platform
has a figure that comes close — **Tickets Sold**, the quantities of an Event's Ticket Sale Lines
summed across active Ticket Sales on every Sales Channel — but it has only ever been a staff figure.
Nothing on a public surface has stated it, and the public Event page is deliberately sparing about
what it discloses: it publishes a Ticket Type's `remaining` and `sold_out`, never its capacity, so a
reader cannot today derive how many tickets an Event has moved. Publishing the sum is new
information leaving the building, not a re-cut of what is already out.

Three things make it more than a field addition.

**The figure is not the thing that was asked for.** The request was to show "how many people are
going". Tickets Sold is not a count of people and the glossary says so at length: a Ticket Sale of
four tickets is one buyer and possibly four friends, nobody has been checked in, and `Avoid:
attendees, headcount, seats, admissions` was written precisely because someone would one day want
that label. The alternative figure — distinct Customers holding an active sale — is a count of real
people, and a badly wrong one: it reports the buyer of four as a single body in the room.

**Most Events, most of the time, have sold very few tickets.** Every Event passes through 0, 1 and 2
on its way up. A page reading "1 ticket sold" is worse than a page reading nothing: it is
anti-social-proof on the Event's most fragile day, and it publishes a number the sole buyer can read
as being about themselves.

**Not every Event has the figure at all.** An Event with External Registration (ADR 0028) sells no
tickets here and produces no Ticket Sale, so its Tickets Sold is not zero — it is undefined. The one
number such an Event does have, `registration_click_count`, counts clicks and is explicit in its own
comment that it counts "never registrations or people".

## Decision

**The Storefront Event page states Tickets Sold, as one Event-level total.** The existing figure,
unmodified: active Ticket Sales, every Sales Channel including `import`, Capacity Holds excluded
because a pending Payment is not a sale, and a Sale Reversal dropping its tickets out whole-Sale as
it does everywhere else.

**The copy says tickets, not people** — "40 tickets sold" / "40 entradas vendidas". The glossary's
`Avoid` list now governs Customer-facing copy and not only staff surfaces.

**The figure is stated only at or above a floor of 5**, one platform-wide constant. Beneath it the
line is not rendered — no zero, no dash, no empty slot.

**External Events state nothing**, and their click count stays staff-only.

**The floor is applied in the backend, and the field is nullable.** `PublicEventDetail` carries a
`*int`; null means "there is no figure to publish here" and covers all three cases — external Event,
beneath the floor — identically, so the Storefront has one branch and no arithmetic. `0` is never
sent, because `0` is a claim.

**Every reachable Event states it, Discoverable or not, with no Organization control.**

**An ended Event keeps its figure.** It is a fact about the Event and does not stop being true at
midnight.

**The Event page only.** Not the Timeline, not the Organization page — `PublicEventCard` is
unchanged.

## Considered options

- **Say "going" or "attendees" and let the number mean tickets.** What was actually asked for, and
  the framing social proof usually takes. Rejected because it costs nothing to refuse: "40 tickets
  sold" carries the same popularity signal *and* a scarcity note that "40 going" does not, so the
  honest wording is also the better-selling one. Taking the other branch would have put a public
  surface in direct contradiction with the glossary term it renders.
- **Distinct Customers with an active sale — an actual count of people.** Rejected as the more
  misleading of the two despite being literally about people: it reports ten buyers where forty
  seats will be filled, so an Event that has nearly sold out would advertise a quarter of its draw.
- **Show the number from zero, with no floor.** Simplest, and honest in the narrow sense. Rejected
  because the figure would spend most of its life discouraging the sale it exists to encourage, and
  because a count of 1 is legible to the person who is it.
- **Bucket the figure — "50+ tickets sold".** Softens the small-number problem without a hard floor.
  Rejected because it reintroduces exactly the vagueness the wording decision removed, and needs its
  own bucket vocabulary in two languages to do it.
- **Per-Ticket-Type counts, beside each `remaining`.** Free — the aggregate is grouped by type
  anyway. Rejected because it turns the buying panel into a sales report, invites comparisons the
  Organization cannot control ("General 240, VIP 3" tells every VIP buyer they will be alone), and
  makes the floor incoherent: tiers would appear and vanish within one list, leaving a total that
  visibly disagrees with its visible parts.
- **An Organization-level or per-Event toggle.** The obvious concession, and the one that would
  destroy the signal. Absence today means one of two innocent things — external Event, or beneath
  the floor. A toggle adds a third, *this Organizer chose to hide it*, which Customers learn to read
  as "selling badly"; every Organization legitimately beneath the floor then looks like it is
  concealing something. If private Events should ever be exempted, the rule to reach for is
  "Discoverable Events only", which needs no new column and reuses a flag whose job is already to
  decide what an Event says about itself in public.
- **Apply the floor in the Storefront, over a raw count from the API.** Rejected because it would
  make the floor cosmetic: the number would sit in the JSON, and `curl` on any Event page would read
  "1 ticket sold". The codebase already holds this line — `already_held` is null rather than 0 for
  an anonymous read, on the reasoning that null and zero are different statements and the client
  must not turn one into the other.
- **Put the figure on listing cards too.** Rejected for now, not on principle: the Timeline is built
  to be scanned and the card already carries price-from, sold-out, tags, date, venue and organizer,
  and a per-card aggregate would land on the platform's hottest cached surface to answer a question
  nobody is asking until they click in. Additive later; nothing here forecloses it.

## Consequences

**Absence becomes weakly readable as "fewer than 5 sold".** A Customer comparing two ticketed Events,
one with a line and one without, can infer something about the second. This is accepted: it is a far
softer signal than the number itself would be, and absence is already the normal, unremarkable state
on every external Event.

**A number the platform refuses to call people sits where a Customer will read it as people anyway.**
The wording is the only defence, so it is load-bearing. Anyone shortening the label to "40 going"
because it reads better in a narrow column is reversing this decision, and should read this ADR
first.

**Imported sales are now public.** An Organization that sold 300 tickets on another platform and
imported them publishes 300 on its Event page. That follows from Tickets Sold counting every Sales
Channel and is correct, but it means a Sale Import is no longer a purely internal act.

**A non-Discoverable Event now publishes its scale to anyone holding its link.** A private party or a
corporate booking states how many tickets it has sold. The audience is the people who were given the
link, which is why this was judged acceptable rather than fixed.

**The figure cannot be cached.** It rides the existing public Event read, which is `force-dynamic`
and `no-store` with a test guarding it, so it is live on every load and no new invalidation problem
is introduced.

**The floor is one constant, and re-tuning it is a one-line change.** 5 was chosen against this
platform's scale — 10 would mean the line almost never rendered, and a feature that rarely appears is
one nobody trusts. If real traffic says otherwise, the number moves; nothing else does.
