# The Holder List filters and downloads itself, and an Event Owner may read it

Specified as issue #518.
Widens [ADR 0047](./0047-the-organization-sees-the-holders-address.md) in one direction and pins it
shut in another: the Organization's sight of a Holder's address now reaches an Event Owner, and the
address of a Holder who has **not** accepted becomes unsearchable as well as undrawn.
Leaves [ADR 0032](./0032-sales-export-states-net-proceeds-never-itemises-the-platform-fee.md)'s
Sales Export exactly as it stands, including its per-Ticket sheet.

## Context

The Holder List is the Organization's answer to "who is coming": every Ticket of every live Ticket
Sale. It carries one control — an Outstanding Answers checkbox — and pages fifty rows at a time in a
fixed order, oldest sale first. On an Event of any size that is not a roster anybody can use: there is
no way to find one person, no way to see only the Tickets nobody has claimed, and no way to take the
list off the platform.

Meanwhile the file already half-exists in the wrong place. The Sales Export, taken from the Sales list
next door, carries a second sheet of one row per Ticket with each Ticket's assignment state, the
accepted Holder's name and address, and a column per Ticket Question. So the roster's contents are
already downloadable — filtered by *sale* filters, from a tab that does not show the roster, inside a
workbook whose first data sheet is money.

That misplacement has a permissions consequence nobody decided. The Holder List read is `orgAdmin`;
the Sales Export is `eventOwnerOrAdmin`. An Event Owner therefore **cannot open the Holder List and
can already download its contents**. The narrower gate on the screen holds nothing in; it is an
artifact of the list having grown out of the Ticket Questions surface, whose Org Admin gate it
inherited, rather than a judgement about roster data.

## Decision

The Holder List gains **seven filters, five sorts, and a Holder Export**, and its read opens to Event
Owners.

**Access.** The Holder List read and the Holder Export are both available to an Org Admin and an
Event Owner — one rule for holder data, matching the Sales Export that already emits it and the
glossary's "an Event Owner is equivalent in scope to an Org Admin within that Event". **Event Staff
gain neither.** They work the door; this is the platform's densest concentration of attendee personal
data, and that line does not move.

**Filters.** `q`, `assignment_state`, `ticket_type_id`, `channel`, `sold_from`/`sold_to`,
`outstanding`, and `question_id` — the last narrowing to the Tickets owing one named question, which
is a different chase from owing anything at all. `never_accepted` is **selectable** as a fourth value
of the state filter without becoming a fourth state: "who was named and never claimed" is the
morning-after question, and the marker already travels beside the state.

There is deliberately **no status filter**: a Sale Reversal means the Tickets cease to exist, so
reversed Tickets are not hidden rows. No `payment_method` or `source` either — sale facts with no
roster meaning, already on the Sales list. And no filter by Answer *value*: the Holder Export's
TRUE/FALSE column per Option answers "how many larges" as a pivot, which is better than a screen
control and is why the columns were shaped that way.

**Search is bounded by disclosure: searchable if and only if displayable.** `q` is a case-insensitive
substring over the buyer's name and address, the Sale Confirmation reference, and — for an
**accepted** Holder only — that Holder's name and address. An address a buyer typed that its owner
never accepted matches **nothing**, and neither does a purged one.

This is the load-bearing half of the decision. ADR 0047 withholds the unaccepted address because it
has no consent moment behind it and the person may not know a ticket was bought for them. A search box
that matched it would answer that same question one address at a time — type an address, get a row,
and the empty Holder cell now means *yes, they are on this list*. The display rule would survive in
the markup and die in the query.

**Sorts.** `sold_at` (default, **oldest sale first, unchanged**), `buyer`, `holder`, `ticket_type` in
catalog display order, and `owes`. Rows with no holder name sort **last in both directions** — not the
reverse of each other, deliberately, so neither direction opens on three hundred empty cells. Every
sort carries a deterministic tiebreak beneath it; without one a paginated list duplicates and drops
rows, and a roster that loses a person is worse than one that is badly ordered.

**Filters and sort live in the URL**, as the Sales list's do, because a file cannot honestly claim to
mirror filters that have no address.

**The Holder Export** is a new artifact and not an extension of the Sales Export: an `.xlsx` read off
the Holder List's own filters, one row per Ticket, Info sheet first, data sheet named anything but
"Sales". It carries the Sale Confirmation reference to join back on, the sale's date and channel, the
Ticket Type and ordinal, the buyer, the assignment state and its never-accepted marker, the accepted
Holder, and then the Ticket Question and Option columns **built by the same code as the Sales
Export's per-Ticket sheet**. It carries **no money at all**.

A filter belonging to a dark feature flag is **ignored, not refused**, and the Info sheet and the
audit line then describe **only the filters actually honoured** — never the raw query, or the file
claims to be narrower than it is and someone reads a whole roster believing it is a filtered one. The
audit line names who took it, from which Organization and Event, under which structural filters, and
how many rows; **never the search term**, which matches customer addresses, because a log aggregator
is a wider audience than the database.

The file is capped at **50,000 Tickets**, its own constant with its own reason — a synchronous
generation ceiling, not the Sales Export's symmetry with what a Sale Import would take back, which
does not transfer to a file nobody imports. Over the cap it refuses and names how many Tickets
matched, rather than truncating.

The route is renamed to `/api/v1/staff/events/{id}/holder-list`, with the export at
`/holder-list/export`; the old `/outstanding-answers` path is aliased to the same handler for one
release against deploy skew, then deleted.

## Considered options

**Whether the file is new.** A new Holder Export (chosen); a Download button handing off to the Sales
Export; extending the Sales Export to take holder filters. The roster's best filters — assignment
state, owing one named question — have no sale equivalent, so both reuses end with a file that
silently ignores the controls the reader just set. The two artifacts also count different things: one
row per Ticket Sale with money that must stay summable, one row per Ticket with none.

**The Sales Export's per-Ticket sheet.** Left untouched (chosen); retired in favour of the new file.
It is in the glossary, covered by tests, and in use. We accept two overlapping *files*; we refuse two
*implementations*, which is what the shared column builder is for.

**Access.** Org Admin and Event Owner on both surfaces (chosen); Org Admin only; Event Owner on the
file alone. The first leaves a gate with an open door beside it; the last gives someone the file but
not the page.

**What `q` matches.** Disclosed fields only (chosen); every stored address; every stored address
without marking the row. The cost of the choice is real and permanent: an Organizer who typed an
address into a Ticket Assignment cannot search for it, and must find the row by its buyer or its
reference.

**Blank ordering.** Last in both directions (chosen); a conventional nulls-first/nulls-last flip. The
convention makes the control useless in one of its two directions.

**A dark feature's filter.** Ignored (chosen); refused with a 400. A refusal turns a stale bookmark
into an error page and forces the client to know a flag that ADR 0045 exists to keep it from knowing.

**The route name.** Renamed with an alias (chosen); kept. `outstanding` becomes one filter of seven;
an endpoint named after it would serve a Holder Export from a path contradicting it, and this is the
cheapest the rename will ever be.

## Consequences

- An Event Owner can now read the Holder List, including accepted Holders' addresses. This is a
  widening of access to personal data, made deliberately and recorded here rather than discovered
  later in an audit.
- The platform emits **two** files containing attendee personal data, on two tabs, under two filter
  vocabularies. Both are logged; both are Org Admin and Event Owner only.
- `q` reaches the URL, so a customer's address reaches browser history and any pasted link. Accepted
  only because the Sales list already does exactly this; tightening it is one change across both
  screens, not a special case here.
- A test asserting that an unaccepted address returns **zero rows** is load-bearing. Adding
  `holder_email` to the search predicate is a one-line change that passes review, breaks nothing
  visible, and reopens ADR 0047.
- The 50,000 cap is a judgement, not a measurement. It is contingent on generating a file of that size
  with the full Option column set inside the request timeout — Option columns make the sheet wide, and
  width costs more than height. If the benchmark does not hold, the number comes down.
- The Outstanding Answers congratulation now fires only when `outstanding` is the sole active filter.
  Under any other filter an empty view means "nothing matched", not "every question has been
  answered", and the two must not share a sentence.
