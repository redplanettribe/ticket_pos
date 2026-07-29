# Affiliate Attribution: last click, remembered for seven days, snapshotted at checkout-begin

## Context

An Affiliate Link is a named, trackable link to an Event's page, created so an organizer can see
what "María's Instagram" or "Radio spot" actually did (#142). Creating them is settled (#145);
what was not settled is the question every attribution scheme is really about — **which sale
belongs to which link, and for how long after the click.**

The system has nothing to answer it with. A ref arrives as a query parameter on a public Event
page rendered by the Storefront; the Go API never sees the click at all. Between that click and
the checkout there may be seconds, or there may be a week: the buyer reads the lineup on the bus,
puts the phone away, and pays from a laptop that evening. Meanwhile the same buyer may click a
second promoter's link for the same Event, and may hold refs for several different Events at once.

Two more facts bound the problem. The stats are **display-only** — there is no affiliate person,
no commission and no payout, and nothing is owed on the strength of them. And a Ticket Sale is
recorded at two different moments: a paid Online Sale settles on the Payment Provider's return
leg, which carries a transaction id and nothing else, while a free one settles inside the
begin-checkout request and never returns at all (ADR 0017).

## Decision

**Affiliate Attribution is last click within a 7-day Attribution Window, per Event.**

The Storefront remembers clicks in one first-party, httpOnly cookie. Middleware writes it when an
Event page is served with a `?ref=`: the code is stored under a key made of both slugs, so a
click on one Event never displaces another's, with its own expiry seven days out. The newest
click on an Event overwrites whatever that Event remembered, including a fresh click on the same
link, which restarts its window. Nothing is validated at click time — a live code, a mistyped one
and one that never existed all set the same cookie and render the same page, because validating
would mean an API call on every page view to answer a question whose answer can change before the
buyer pays.

The begin-checkout BFF route reads the remembered code for the Event being bought and forwards it
as `affiliate_code`. **The API is cookie-agnostic**: it receives a code, resolves it against that
Event's ACTIVE Affiliate Links, and snapshots the resulting id on the `payments` row. An unknown,
mistyped or deactivated code resolves to nobody — the checkout proceeds normally and the sale is
recorded unattributed. A ref never refuses a purchase, and the buyer is never told which of the
two happened.

The snapshot is copied onto the Ticket Sale inside the single sale-commit chokepoint, so the free
zero-total path carries attribution by construction rather than by a second implementation
(ADR 0017). What the code resolved to at checkout-begin is what the sale records forever —
deactivating a link afterwards stops it attributing new sales and changes none of its history,
the same reasoning that freezes prices and fee snapshots at begin (ADR 0014).

Only Online Sales are ever attributed. The in-person and import channels have no click behind
them and no field to carry one.

The window is **7 days, defined in the cookie helper** (`apps/storefront/lib/affiliate-ref.ts`)
and nowhere else. Moving it is a one-constant change with no migration and no API change, because
the backend stores a link id and never a duration.

## Considered options

- **Same-visit attribution only** — credit a link only when the checkout happens in the session
  the click started; no cookie beyond the request. Honest to the point of uselessness: it credits
  the impulse buy and silently drops the deliberate one, which is precisely the purchase a
  promoter's audience makes. An organizer comparing two promoters would be comparing how quickly
  their audiences buy.
- **A much longer window (30–90 days, as affiliate networks use)** — rejected because those
  windows exist to justify paying commission on a lifetime of purchasing, and this feature pays
  nobody. Seven days covers "clicked on the bus, bought at home" and stops well short of
  crediting a link for a purchase a month of other marketing produced.
- **First click wins** — credit whoever introduced the buyer to the Event. Defensible, and
  rejected as unimplementable honestly: the platform sees only the clicks that reach its own
  pages, so "first" means "first we happened to observe", and it makes a promoter's own repeat
  audience uncreditable.
- **Server-side click records keyed to a visitor id** — a row per click instead of a cookie.
  Strictly more data and strictly more of everything else: a public write endpoint open to the
  internet, a visitor identity to invent and store, and a table that grows with page views rather
  than with sales, all to hold a value the browser can hold for free.
- **Resolving the code at click time and cookie-ing the link id** — rejected for the same reason
  the page does not validate: a per-page-view API call, and a decision taken days before the sale
  it decides.

## Consequences

**Some credited sales would have happened anyway.** A buyer who clicks a promoter's link and buys
five days later may well have bought regardless, and this design credits the link for it. That is
the accepted cost of a window generous enough to be useful, and it is acceptable *because* the
figures pay nobody — the day a commission is computed from them is the day this ADR needs
revisiting, not adjusting.

**Attribution follows the browser, not the person.** Clicking on a phone and buying on a laptop
is unattributed; clearing cookies, private browsing, or a shared device all move the credit
around. The figures are therefore a floor on what a link drove, not a measurement, and staff copy
should never imply otherwise.

**The window is not enforced anywhere but the Storefront.** The API attributes any live code it is
handed, so a hand-crafted request could carry a code from a click that never happened or expired
months ago. The blast radius is a display-only number on the organizer's own Event, and the
alternative — a server-side click ledger — was rejected above for costing more than the figure is
worth.

**One cookie, bounded.** All remembered clicks share a single cookie holding at most twenty
Events, oldest dropped first; each entry expires on its own seven-day schedule even while newer
clicks keep the cookie alive. httpOnly, so no page script can read a promoter's code or forge an
attribution.

`payments` and `ticket_sales` each gain a nullable `affiliate_link_id` (migration 036). Nullable
is the ordinary case: every in-person sale, every imported sale, and every buyer who arrived
directly.
