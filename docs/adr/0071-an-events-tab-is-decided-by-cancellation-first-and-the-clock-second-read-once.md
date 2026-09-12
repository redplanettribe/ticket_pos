# An Event's tab is decided by cancellation first and the clock second, read once

Splits the staff Events list into three tabs — Active, Past, Cancelled — that partition every
Event an Organization has. Membership is decided by two rules applied in order: a `cancelled`
Event goes to Cancelled whatever its date, and everything else is placed by whether its runtime
has ended. The clock is the viewer's browser, read once when the list renders.

Borrows its clock idiom from
[ADR 0070](./0070-a-ticket-type-closes-on-the-storefronts-clock-alone-judged-once-at-begin-checkout.md),
which judges the Sales Cutoff on the Storefront's clock at a single moment rather than
continuously. It adds no column, no status and no endpoint parameter.

## Context

The staff Events list has shown every Event an Organization has ever had, in creation order, since
it was built. `ListEventsByOrganizationID` is `WHERE organization_id = $1 ORDER BY created_at DESC,
name ASC` with no filter, no limit and no pagination, and the page fetches it once from a
`useEffect`. For an Organization with three Events that is merely untidy. For one with three years
of them it buries the Event happening on Saturday under everything that already happened, and
creation order means the dates on screen do not even run in one direction.

Two axes decide whether an Event still wants attention, and they are not the same axis. One is the
lifecycle status — `draft`, `published`, `cancelled` — and the other is the clock. An Event can be
finished without being cancelled, and cancelled without being finished. Any split of this list has
to say which of the two wins where they disagree, and the vocabulary had no word for the answer:
`CONTEXT.md` put "active/inactive" on the Event status avoid list precisely because it invited
this confusion.

## Decision

**Cancelling outranks the clock.** A `cancelled` Event is in Cancelled forever, however old, and is
never in Past. The alternative — letting an aged cancellation fall through into Past — gives the
Cancelled tab a decay problem: it would hold only cancellations of *future* Events, drain on its
own as its contents age, and move rows between tabs with nobody touching them.

**Over is an instant comparison, not a status.** An Event is over when its end instant has passed,
or its start instant where it has no end. This is exactly the filter the global explorer already
ships at `public_repository.go`, `COALESCE(e.ends_at, e.starts_at) >= now`, so the staff tabs and
the Storefront agree by construction rather than by coincidence. The Event's timezone governs how
its date is printed and never whether it has passed.

**Publishing has no say in it.** A draft dated next month is Active beside a published Event
selling tickets, and a draft dated last March is Past beside the Events that ran. Past means it is
over, not that it happened. Pulling drafts into a fourth tab was rejected: drafts are the work most
in need of finishing, and Active is where unfinished work belongs.

**An Event with no start is never over.** `starts_at` is nullable and only checked at publish
time, so a dateless draft is a real row. It stays in Active, pinned above the dated Events, until
it is dated, cancelled or deleted. Treating "over" as something that must be proven rather than
assumed keeps the rows that need finishing in front of the person who can finish them.

**The browser splits one payload, and reads the clock once.** The endpoint gains no parameter and
the repository keeps its signature; the page fetches the list as it always has and partitions the
array in the client, sorting Active ascending and the other two descending by start. The clock is
read at render and never again — no interval, no re-partition, no row sliding out from under a
click — for the same reason ADR 0070 judges the Sales Cutoff once.

**The tabs are routes.** `/events`, `/events/past` and `/events/cancelled`, drawn with `PageTabs`,
following the position already written at `apps/staff/app/events/[id]/sales/layout.tsx`.

## Consequences

Two tabs now grow without bound, and neither is paginated. This is not a regression — the list has
never had a limit — but it is now a shape the design has blessed rather than inherited, and Past
and Cancelled are where it will be felt first.

Because the tabs are routes and the split is client-side, every tab switch remounts and refetches
the entire list. Three counts ride in the tab labels, which is free only because the browser holds
the whole payload; it is also the first tab strip in the app to carry counts.

The clock being read once means an Events page left open overnight shows yesterday's Event in
Active. That staleness is deliberate.

`CONTEXT.md` gains **Active Event** and **Past Event**, and the Event status avoid list is narrowed
so that it bars active/inactive as the name of a *status value* rather than outright. "Active"
now names two things in the glossary — an Active Member is the Member selected on a Staff Session
and has nothing to do with Events. The two are never in the same sentence, but the collision is
real and is recorded here rather than discovered later.

The Spanish tab labels were left open here and settled in #612: **Activos**, **Pasados** and
**Cancelados**. Activos was chosen over Próximos because it stays true of an Event mid-run and of
a dateless draft, both of which Próximos excludes. Pasados was chosen over Finalizados and
Realizados because Past means the Event is over, not that it took place, and a draft never
published is Past. The Spanish carries the same Active Member collision recorded above, since the
session switcher already reads "Organización activa"; Vigentes would have avoided it but reads
legalistic against a draft, and the collision is judged harmless in context.

One question is left open on purpose: pagination for the two unbounded tabs.
