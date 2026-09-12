# The Storefront calls Tickets Sold "going" and states it on every card

Supersedes the wording and surfaces rulings of
[ADR 0042](./0042-the-storefront-publishes-tickets-sold-floored-and-calls-them-tickets.md), and
keeps its other three: the figure is Tickets Sold, floored at 5 in the backend with null beneath,
and no Organization controls it. ADR 0042 was ratified on 2026-08-21 and never built; this ADR is
what gets built instead.

## Context

ADR 0042 answered "how many people are going" with the platform's Tickets Sold figure on the Event
page, worded "40 tickets sold", and argued at length that the copy must not say "going" or
"attendees": a Ticket Sale of four is one buyer and possibly four friends, nobody has been checked
in, and the glossary's `Avoid` list was written for exactly that label. It also kept the figure off
the listing cards, "for now, not on principle", because the Timeline is scanned and the card was
already full.

The request that reopened it asked for the number on the discovery page, worded as people going.
That is the framing social proof takes everywhere a Customer has seen it, and the surface where a
Customer is actually choosing between Events is the Timeline, not the page they have already
clicked into. Neither of ADR 0042's two arguments against the label survived being weighed against
that: the number is a count of seats that will be filled, which is what "going" means to a
Customer scanning a list, and the honesty case for "tickets sold" is a distinction no Customer
draws. The scarcity note ADR 0042 valued in "sold" is carried by the price slot and the Sold Out
badge beside it anyway.

## Decision

**The figure is still Tickets Sold, unchanged.** Line quantities of active Ticket Sales on every
Sales Channel including `in_person` and `import`, Capacity Holds excluded because a pending
Payment is not a sale, a Sale Reversal dropping its tickets whole-Sale. Not accepted Holders, which
undercount badly because most buyers never assign their spare tickets; not distinct buyers, which
ADR 0042 rightly called the more misleading figure. No new glossary term: "going" is the
Customer-facing rendering of the canonical figure, exactly as "Listed" is the staff rendering of
Discoverable, and code, prose and staff surfaces keep saying Tickets Sold.

**The copy is "{count} going" / "{count} asistirán".** A verb, not a noun: "asistentes" was
rejected because it is a headcount claim and stops being true of an Event that is over, and
"van" because "40 van" on a card does not say where. No singular form exists, because the floor
guarantees the count is never below 5.

**The floor is 5, applied in the backend, null beneath it — ADR 0042's rule, kept whole.** The
argument is stronger under this label than it was under the last one: "1 going" reads as a person,
and that person can read it. `0` is never sent, a null on the API is the only way the count is
withheld, and the Storefront has one branch and no arithmetic. An Event with External
Registration is always null: it sells no tickets here, and its click count counts clicks, never
people, so it must not be dressed as "going". Absence therefore keeps one meaning on every surface.

**Three surfaces, one rule.** The Timeline card, the Organization page card and the Event page all
state it. One nullable integer on the public card projection and one on the public detail, both
summed from the maintained per-Ticket-Type sold counts that the public catalog query already
aggregates over for `min_price`, `all_sold_out` and `ticket_count`, so the listing query gains an
aggregate in a lateral it is already running and no join. ADR 0042's reason for leaving the cards
out was that the aggregate would land on "the platform's hottest cached surface"; the explorer is
in fact `force-dynamic` with `no-store` reads, so no cache is being bypassed and nothing new goes
stale.

**Rendered as a plain text line, after the price slot.** Muted, in the venue's style, on both card
shapes and near the hero on the Event page. Not a badge, because it is a fact about the Event and
not a category; not folded into the price slot, because two independent nullable slots in one
string is four combinations to translate. Sold Out and Closed stay where they are and say
something different.

**Stated whenever it is at or above the floor, and the tense never changes.** An Event that is
Over, closed by Sales Cutoff (ADR 0070), or cancelled-but-reachable keeps its figure. Past Events
are how a Customer sees that an Organizer has a following, which is the strongest trust signal a
stranger gets; "120 going" beside a Closed badge is coherent, those people are still going; and
"went" would be a second key in two languages on a surface the Timeline never shows.

**No Organization or Event control, ADR 0042's rule kept whole.** Every reachable Event states it,
Discoverable or not. A toggle would give absence a third meaning — this Organizer chose to hide it
— which Customers learn to read as "selling badly". If unlisted Events ever need exempting, the
rule to reach for is still "Discoverable Events only", which needs no column.

## Consequences

**ADR 0042's "the wording is the only defence" clause is repealed, not defeated.** That ADR warned
that anyone shortening the label to "going" was reversing it and should read it first. This is that
reversal, read and done on purpose. The glossary's `Avoid` list on Tickets Sold goes back to
governing prose, code and staff copy; Customer-facing copy is the one carved-out exception, and it
is carved out for this label alone. "Attendees" and "asistentes" stay avoided everywhere.

**The card now makes a claim about people on the strength of a count of tickets.** A buyer of four
who comes alone is counted four times. This is accepted: the figure tracks the size of the room the
Organization is planning for, and it is the same reading the Organization itself takes from the
staff Sales strip.

**Absence is weakly readable as "fewer than 5 going", now on the list and not only on the page.** A
Customer comparing two adjacent cards, one with a line and one without, can infer something about
the second. Softer than the number would be, and already the unremarkable state of every external
Event.

**Imported and door sales are public on every listing.** An Organization that imported 300 sales
from another platform advertises 300 going on the Timeline. Correct, and a Sale Import is now even
less of a purely internal act than ADR 0042 made it.

**One constant serves three surfaces.** The floor lives in one named place in the backend, and the
card and the page cannot disagree about whether an Event has enough going to say so. Re-tuning it
is a one-line change.
