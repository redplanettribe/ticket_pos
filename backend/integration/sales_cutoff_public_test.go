package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// THE SALES CUTOFF ON THE PUBLIC API (#604, parent #602, ADR 0070). The second
// slice: the Storefront's payload stops quoting a price nobody can pay.
//
// A Ticket Type past its cutoff still appears with its description and its
// price, now marked closed. The Event's "from" price and its sold-out verdict
// are computed over the Ticket Types still open in time, and an Event whose
// every Ticket Type has closed says so in a field of its own. Nothing here
// refuses anything — begin-checkout is #605's, and no page changes in this
// slice.
//
// The shape is promotions_public_test.go's, deliberately: the same three
// readers (the event page, the global explorer card and the Organization page
// card) asked the same question either side of a window boundary. Instants are
// compared with sameInstant, because what travels is a moment and not a
// rendering of one.
//
// HOW THESE TESTS CLOSE A TICKET TYPE. Two ways, and the difference matters.
// Where the cutoff is the thing under test the clock is moved onto it with
// holdClocksAt, which proves the half-open edge — the cutoff instant itself is
// closed. Where closing is merely the setup for an aggregate the cutoff is set
// in the past instead and the clock is left alone, because moving the clock
// forward would also lapse the Capacity Holds the mixed-Event case needs alive.

// staffTicketTypeShape is the staff Ticket Type read narrowed to the scalars
// the update endpoint demands back. Updating is a full restatement, so setting
// a cutoff means re-sending everything else unchanged.
type staffTicketTypeShape struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
	Capacity   int    `json:"capacity"`
	SortOrder  int    `json:"sort_order"`
}

// setSalesCutoff puts a Sales Cutoff on a Ticket Type through the staff API an
// Org Admin would use, preserving the name, price, capacity and order it
// already has. A nil cutoff clears it.
func setSalesCutoff(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string, cutoffAt *time.Time) {
	t.Helper()

	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var types []staffTicketTypeShape
	if err := json.Unmarshal(body.Data, &types); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	var current *staffTicketTypeShape
	for i := range types {
		if types[i].ID == ticketTypeID {
			current = &types[i]
			break
		}
	}
	if current == nil {
		t.Fatalf("ticket type %s absent from the list of %d", ticketTypeID, len(types))
	}

	var cutoff any
	if cutoffAt != nil {
		cutoff = cutoffAt.UTC().Format(time.RFC3339Nano)
	}
	resp, body = env.patch(t, ticketTypePath(eventID, ticketTypeID), map[string]any{
		"name":            current.Name,
		"price_cents":     current.PriceCents,
		"capacity":        current.Capacity,
		"sort_order":      current.SortOrder,
		"sales_cutoff_at": cutoff,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set sales cutoff status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// orgPageCard reads the Organization page and returns the upcoming card with
// the given slug. The Organization page runs a second query over the same card
// shape as the explorer, so every aggregate has to be asked of both.
func orgPageCard(t *testing.T, env *testEnv, orgSlug, eventSlug string) publicEventCard {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/"+orgSlug+"/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Upcoming []publicEventCard `json:"upcoming"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode organization page: %v", err)
	}
	for _, card := range page.Upcoming {
		if card.Slug == eventSlug {
			return card
		}
	}
	t.Fatalf("event %s not on the organization page: %+v", eventSlug, page.Upcoming)
	return publicEventCard{}
}

// publicEventAllClosed reads the Storefront event page and returns its
// all_closed verdict — the field the page draws its "sales have closed"
// sentence from.
func publicEventAllClosed(t *testing.T, env *testEnv, orgSlug, eventSlug string) bool {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var detail struct {
		AllClosed bool `json:"all_closed"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	return detail.AllClosed
}

// TestPublicTicketTypeCarriesTheCutoffAndTheVerdict walks user stories 15, 16
// and 45 in one pass. The instant travels so the card can state the closing
// time and the countdown has something to count to; the verdict travels because
// the server owns the clock and a browser with a wrong one must never be able
// to show a stepper the API will refuse.
//
// The clock is moved exactly onto the cutoff, not past it: the window is
// half-open at the closing end, so the cutoff instant itself is already closed.
// The Event is in America/Guayaquil and the instant comes back unconverted —
// the Event's timezone is applied when it is read, not when it is sent.
func TestPublicTicketTypeCarriesTheCutoffAndTheVerdict(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Cutoff Storefront", "cutoff-storefront", 1000, 20)
	cutoffAt := env.fixedClock.Add(48 * time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &cutoffAt)

	wantInstant := cutoffAt.UTC().Format(time.RFC3339)

	before := publicTicketTypes(t, env, "test-org", "cutoff-storefront")["GA"]
	if before.SalesCutoffAt == nil {
		t.Fatalf("open Ticket Type carries no sales_cutoff_at: %+v", before)
	}
	if !sameInstant(t, *before.SalesCutoffAt, wantInstant) {
		t.Fatalf("sales_cutoff_at = %q; want %q, the instant as it was set", *before.SalesCutoffAt, wantInstant)
	}
	if before.Closed {
		t.Fatalf("Ticket Type reads closed two days before its cutoff: %+v", before)
	}

	// One second before: still open, and the payload has not moved.
	holdClocksAt(cutoffAt.Add(-time.Second))
	stillOpen := publicTicketTypes(t, env, "test-org", "cutoff-storefront")["GA"]
	if stillOpen.Closed {
		t.Fatalf("Ticket Type reads closed a second before its cutoff: %+v", stillOpen)
	}

	// At the instant itself: closed, and still listed, still described, still
	// priced — closing is a door and not a deletion.
	holdClocksAt(cutoffAt)
	closed := publicTicketTypes(t, env, "test-org", "cutoff-storefront")["GA"]
	if !closed.Closed {
		t.Fatalf("Ticket Type reads open at its own cutoff instant: %+v", closed)
	}
	if closed.PriceCents != before.PriceCents {
		t.Fatalf("closed price_cents = %d; want the %d it was priced at while open", closed.PriceCents, before.PriceCents)
	}
	if closed.SalesCutoffAt == nil || !sameInstant(t, *closed.SalesCutoffAt, wantInstant) {
		t.Fatalf("closed sales_cutoff_at = %v; want %q", closed.SalesCutoffAt, wantInstant)
	}

	// And strictly after, a month on: still closed, and the instant is still
	// stated. A closed card has to say WHEN it closed — missing something by an
	// hour and missing it by a month are different facts to a reader deciding
	// whether it is worth writing to ask.
	holdClocksAt(cutoffAt.Add(30 * 24 * time.Hour))
	longClosed := publicTicketTypes(t, env, "test-org", "cutoff-storefront")["GA"]
	if !longClosed.Closed {
		t.Fatalf("Ticket Type reads open a month past its cutoff: %+v", longClosed)
	}
	if longClosed.SalesCutoffAt == nil || !sameInstant(t, *longClosed.SalesCutoffAt, wantInstant) {
		t.Fatalf("long-closed sales_cutoff_at = %v; want the %q it was set to", longClosed.SalesCutoffAt, wantInstant)
	}

}

// TestSalesCutoffIsCarriedUnconverted proves what sameInstant alone cannot: the
// Event's timezone is applied when the instant is READ, not when it is sent.
//
// Two Events in two timezones are given the very same cutoff instant, and the
// payload has to render it identically for both. Comparing moments would pass
// even if the API had converted each one onto its own Event's clock — the
// moment survives a conversion, which is the whole point of an offset. Comparing
// the two renderings to each other does not: if the Event's timezone reached
// this field, Quito's and Madrid's would differ, and the Storefront would be
// unable to tell an offset from a moment.
func TestSalesCutoffIsCarriedUnconverted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	cutoffAt := env.fixedClock.Add(48 * time.Hour)

	quitoID, quitoGA := publishCheckoutEvent(t, env, sessionID, "Cutoff Quito", "cutoff-quito", 1000, 20)
	setSalesCutoff(t, env, sessionID, quitoID, quitoGA, &cutoffAt)

	madridID, madridGA := publishCheckoutEvent(t, env, sessionID, "Cutoff Madrid", "cutoff-madrid", 1000, 20)
	setEventTimezone(t, env, madridID, "Europe/Madrid")
	setSalesCutoff(t, env, sessionID, madridID, madridGA, &cutoffAt)

	quito := publicTicketTypes(t, env, "test-org", "cutoff-quito")["GA"]
	madrid := publicTicketTypes(t, env, "test-org", "cutoff-madrid")["GA"]
	if quito.SalesCutoffAt == nil || madrid.SalesCutoffAt == nil {
		t.Fatalf("a cutoff went missing: quito=%v madrid=%v", quito.SalesCutoffAt, madrid.SalesCutoffAt)
	}
	if *quito.SalesCutoffAt != *madrid.SalesCutoffAt {
		t.Fatalf("the same instant reads %q on an America/Guayaquil Event and %q on a Europe/Madrid one; want one unconverted rendering",
			*quito.SalesCutoffAt, *madrid.SalesCutoffAt)
	}
	if !sameInstant(t, *quito.SalesCutoffAt, cutoffAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("sales_cutoff_at = %q; want the instant %q that was set", *quito.SalesCutoffAt, cutoffAt.UTC().Format(time.RFC3339))
	}
}

// TestPublicTicketTypeWithoutACutoffIsUnchanged is the inertness guarantee ADR
// 0070 leans on instead of a feature flag: the column is unset on every row
// that exists, so a Ticket Type nobody has typed a date into must produce
// exactly today's payload — a null instant, an open verdict, and aggregates
// that say what they said before.
//
// Asserting only that nothing happened would pass with the whole feature
// deleted: a payload that never learned about cutoffs says null and false too.
// So the Event carries an ANCHOR — a cheaper sibling Ticket Type that really
// has closed — and every assertion about the quiet one is made while the
// sibling proves the machinery is running. Two of them fail on deletion: the
// sibling's own closed verdict, and the "from" price, which would drop to the
// cheap closed sibling's the moment closing stopped narrowing it.
func TestPublicTicketTypeWithoutACutoffIsUnchanged(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "No Cutoff Fest", "no-cutoff-fest", 1000, 20)
	// The anchor is CHEAPER than GA, so a card that stopped excluding closed
	// Ticket Types would quote it and this test would notice.
	anchorID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Early Bird", 500, 10)
	setSalesCutoff(t, env, sessionID, eventID, anchorID, beforeTheCutoff(env))
	makeDiscoverable(t, env, sessionID, eventID)

	types := publicTicketTypes(t, env, "test-org", "no-cutoff-fest")
	anchor := types["Early Bird"]
	if !anchor.Closed || anchor.SalesCutoffAt == nil {
		t.Fatalf("the anchoring Ticket Type reads %+v; want closed with its cutoff stated — a feature that reads nothing would read open here too", anchor)
	}
	if anchor.PriceCents >= types["GA"].PriceCents {
		t.Fatalf("fixture is not ordered: the closed anchor at %d is not cheaper than GA at %d", anchor.PriceCents, types["GA"].PriceCents)
	}

	ga := types["GA"]
	if ga.SalesCutoffAt != nil {
		t.Fatalf("Ticket Type created without a cutoff carries sales_cutoff_at=%q", *ga.SalesCutoffAt)
	}
	if ga.Closed {
		t.Fatalf("Ticket Type with no cutoff reads closed: %+v", ga)
	}
	if ga.SoldOut {
		t.Fatalf("Ticket Type with capacity to spare reads sold out: %+v", ga)
	}

	// A cutoff that never comes cannot be reached by waiting, so the far future
	// says nothing about this Ticket Type either.
	holdClocksAt(env.fixedClock.Add(365 * 24 * time.Hour))
	later := publicTicketTypes(t, env, "test-org", "no-cutoff-fest")["GA"]
	if later.Closed || later.SalesCutoffAt != nil {
		t.Fatalf("Ticket Type with no cutoff a year on = %+v; want open with a null cutoff", later)
	}

	holdClocksAt(env.fixedClock)
	card := explorerCard(t, env, "no-cutoff-fest")
	if card.AllClosed {
		t.Fatalf("explorer card all_closed = true on an Event nobody has closed: %+v", card)
	}
	if card.SoldOut {
		t.Fatalf("explorer card sold_out = true on an Event with capacity to spare: %+v", card)
	}
	// The second anchor: the cheaper sibling has closed, so the card must quote
	// the untouched Ticket Type's price. A feature that stopped narrowing the
	// "from" price would quote the cheap closed one instead.
	if got := cardPriceFrom(t, card); got != ga.PriceCents {
		t.Fatalf("card price_from_cents = %d; want the %d the only still-open Ticket Type quotes", got, ga.PriceCents)
	}
	if publicEventAllClosed(t, env, "test-org", "no-cutoff-fest") {
		t.Fatal("event page all_closed = true on an Event nobody has closed")
	}
}

// TestListingCardFromPriceIgnoresAClosedTicketType is user story 30: the price
// a card quotes must be one the reader could actually pay. The cheap Ticket
// Type closes, so the card has to stop advertising it and quote the dearer one
// that is still open — the same rule ADR 0021 already wrote for a Promotion
// window, applied to a wider question.
//
// Asserted against the event page's own figure rather than a hardcoded all-in
// number, so the two surfaces are proved to agree as well.
func TestListingCardFromPriceIgnoresAClosedTicketType(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "From Price Fest", "from-price-fest", 1000, 20)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 4000, 10)
	makeDiscoverable(t, env, sessionID, eventID)

	page := publicTicketTypes(t, env, "test-org", "from-price-fest")
	cheap, dear := page["GA"].PriceCents, page["Balcony"].PriceCents
	if cheap >= dear {
		t.Fatalf("fixture is not ordered: GA %d is not cheaper than Balcony %d", cheap, dear)
	}
	if got := cardPriceFrom(t, explorerCard(t, env, "from-price-fest")); got != cheap {
		t.Fatalf("card price_from_cents = %d before any cutoff; want the cheapest %d", got, cheap)
	}

	// The cheap one closes, an hour ago, which is how an Org Admin stops selling
	// something right now.
	closedAt := env.fixedClock.Add(-time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &closedAt)

	if got := cardPriceFrom(t, explorerCard(t, env, "from-price-fest")); got != dear {
		t.Fatalf("explorer price_from_cents = %d with the cheap Ticket Type closed; want the cheapest still-open %d", got, dear)
	}
	if got := cardPriceFrom(t, orgPageCard(t, env, "test-org", "from-price-fest")); got != dear {
		t.Fatalf("organization page price_from_cents = %d with the cheap Ticket Type closed; want %d", got, dear)
	}

	// The closed Ticket Type is still on the page, still priced at what it cost.
	after := publicTicketTypes(t, env, "test-org", "from-price-fest")
	if !after["GA"].Closed || after["GA"].PriceCents != cheap {
		t.Fatalf("closed GA = %+v; want closed and still priced at %d", after["GA"], cheap)
	}
	if after["Balcony"].Closed {
		t.Fatalf("Balcony reads closed with no cutoff of its own: %+v", after["Balcony"])
	}
}

// TestAllClosedIsTrueOnlyWhenEveryTicketTypeHasClosed is user stories 27 and
// 29. A page of dimmed cards must be able to say so in a sentence, and a
// listing must read closed rather than sold out — telling a half-empty room it
// is full is a claim the Organization has to answer for.
//
// The two Ticket Types close an hour apart and the clock is walked across both
// edges, which is what pins the SQL half of the closing rule. The aggregates
// are computed in the shared subquery rather than by catalog.ClosedAt, so the
// half-open edge — the cutoff instant itself is closed — has to be asserted
// here as well as on the per-Ticket-Type verdict, or a drift from `<=` to `<`
// down there would stay green.
func TestAllClosedIsTrueOnlyWhenEveryTicketTypeHasClosed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "All Closed Fest", "all-closed-fest", 1000, 20)
	balconyID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 4000, 10)
	makeDiscoverable(t, env, sessionID, eventID)

	if explorerCard(t, env, "all-closed-fest").AllClosed {
		t.Fatal("all_closed = true with both Ticket Types open")
	}

	// The cheap Ticket Type closes first, the dear one an hour later — an
	// early-bird tier closing months before general admission does, compressed
	// (user story 3). Both are in the future, so the clock does the closing.
	gaClosesAt := env.fixedClock.Add(24 * time.Hour)
	balconyClosesAt := gaClosesAt.Add(time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &gaClosesAt)
	setSalesCutoff(t, env, sessionID, eventID, balconyID, &balconyClosesAt)

	page := publicTicketTypes(t, env, "test-org", "all-closed-fest")
	cheap, dear := page["GA"].PriceCents, page["Balcony"].PriceCents

	// A second before GA's cutoff: nothing has closed, and the card still
	// quotes the cheap one.
	holdClocksAt(gaClosesAt.Add(-time.Second))
	open := explorerCard(t, env, "all-closed-fest")
	if open.AllClosed {
		t.Fatalf("all_closed = true a second before the first cutoff: %+v", open)
	}
	if got := cardPriceFrom(t, open); got != cheap {
		t.Fatalf("price_from_cents = %d a second before any cutoff; want the cheapest %d", got, cheap)
	}

	// At GA's cutoff instant exactly: GA has closed, Balcony has not. The
	// aggregates move at the instant itself, not the second after it.
	holdClocksAt(gaClosesAt)
	half := explorerCard(t, env, "all-closed-fest")
	if half.AllClosed {
		t.Fatalf("all_closed = true with one of two Ticket Types closed: %+v", half)
	}
	if got := cardPriceFrom(t, half); got != dear {
		t.Fatalf("price_from_cents = %d at the cheap Ticket Type's own cutoff instant; want the still-open %d", got, dear)
	}
	if publicEventAllClosed(t, env, "test-org", "all-closed-fest") {
		t.Fatal("event page all_closed = true with one of two Ticket Types closed")
	}

	// At Balcony's cutoff instant: every Ticket Type has closed.
	holdClocksAt(balconyClosesAt)
	whole := explorerCard(t, env, "all-closed-fest")
	if !whole.AllClosed {
		t.Fatalf("all_closed = false at the last Ticket Type's cutoff instant: %+v", whole)
	}
	// Closed is not sold out, on any surface.
	if whole.SoldOut {
		t.Fatalf("an entirely closed Event reads sold_out: %+v", whole)
	}
	// And it quotes no "from" price at all: there is no Ticket Type left whose
	// price a Customer could pay. The card tells the two nulls apart by
	// all_closed, the way it already tells an external Event's permanent null
	// apart by registration_mode (#211) — which is #607's to draw.
	if whole.PriceFromCents != nil {
		t.Fatalf("entirely closed Event quotes price_from_cents = %d; want none", *whole.PriceFromCents)
	}
	if !orgPageCard(t, env, "test-org", "all-closed-fest").AllClosed {
		t.Fatal("organization page all_closed = false with every Ticket Type closed")
	}
	if !publicEventAllClosed(t, env, "test-org", "all-closed-fest") {
		t.Fatal("event page all_closed = false with every Ticket Type closed")
	}

	// Reopening one of them is one edit, and the verdict follows on the next
	// read: nothing about closing is stored, so there is nothing to unwind.
	setSalesCutoff(t, env, sessionID, eventID, balconyID, nil)
	reopened := explorerCard(t, env, "all-closed-fest")
	if reopened.AllClosed {
		t.Fatal("all_closed = true after one Ticket Type's cutoff was cleared")
	}
	if got := cardPriceFrom(t, reopened); got != dear {
		t.Fatalf("price_from_cents = %d after reopening Balcony; want its %d back", got, dear)
	}
}

// TestMixedClosedAndExhaustedEventReadsSoldOut is user story 31, and the
// reason all_sold_out is judged over the still-open Ticket Types rather than
// over all of them. One Ticket Type has closed and the other is spoken for;
// the Event reads sold out, which is the stronger and more informative fact,
// and not closed.
//
// The clock never moves here: GA's cutoff is set in the past instead, because
// moving the clock past a cutoff would also lapse the Capacity Hold that makes
// Balcony sold out.
func TestMixedClosedAndExhaustedEventReadsSoldOut(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Mixed Fest", "mixed-fest", 1000, 20)
	balconyID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 4000, 2)
	makeDiscoverable(t, env, sessionID, eventID)

	closedAt := env.fixedClock.Add(-time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &closedAt)

	// Balcony's whole capacity is spoken for by a live Capacity Hold.
	beginCheckoutOK(t, env, "test-org", "mixed-fest",
		checkoutBody("mixed@example.com", "Mina", "Mixed", map[string]any{"ticket_type_id": balconyID, "quantity": 2}))

	types := publicTicketTypes(t, env, "test-org", "mixed-fest")
	if !types["GA"].Closed {
		t.Fatalf("GA is not closed: %+v", types["GA"])
	}
	if !types["Balcony"].SoldOut {
		t.Fatalf("Balcony is not sold out: %+v", types["Balcony"])
	}

	card := explorerCard(t, env, "mixed-fest")
	if !card.SoldOut {
		t.Fatalf("mixed Event card sold_out = false; want true — the still-open Ticket Types are exhausted: %+v", card)
	}
	if card.AllClosed {
		t.Fatalf("mixed Event card all_closed = true; want false — Balcony never closed: %+v", card)
	}
	if publicEventAllClosed(t, env, "test-org", "mixed-fest") {
		t.Fatal("mixed Event page all_closed = true; want false")
	}

	// The "from" price narrows the same way: GA is the cheaper of the two and
	// has closed, so the card quotes the Balcony price even though nobody can
	// buy that either. What a card advertises is the cheapest OPEN Ticket Type;
	// whether any is left is the sold_out flag's separate sentence.
	if got := cardPriceFrom(t, card); got != types["Balcony"].PriceCents {
		t.Fatalf("mixed Event price_from_cents = %d; want the still-open Balcony's %d", got, types["Balcony"].PriceCents)
	}
}

// TestAClosedTicketTypeKeepsItsPromotion is user story 42 on the Storefront's
// payload: a discount and a deadline coexist on one Ticket Type, and neither
// one erases the other.
//
// The two are deliberately overlapped — the cutoff falls INSIDE the Promotion
// window, which is the arrangement ADR 0070 refuses to validate against and
// therefore the one an organizer will actually create. Three things are pinned:
//
//   - While open, both travel: the promotional price is quoted and the cutoff
//     is stated, so the card can draw a discount and a deadline at once.
//   - Once closed, the Promotion is STILL carried. The server states facts and
//     the card decides what to draw; the Storefront's rule that a Promotion
//     badge only shows on a buyable card is presentation, and the payload it
//     narrows has to exist for it to narrow.
//   - The "from" price stops quoting the discount the moment the Ticket Type
//     closes. A card advertising a promotional price on a Ticket Type nobody
//     can buy is the exact misquote story 30 exists to prevent, and a live
//     Promotion must not rescue it.
func TestAClosedTicketTypeKeepsItsPromotion(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Promo And Cutoff", "promo-and-cutoff", promoListPriceCents, 20)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 4000, 10)
	setFeeHandling(t, env, sessionID, eventID, "Promo And Cutoff", "promo-and-cutoff", "pass_on")
	makeDiscoverable(t, env, sessionID, eventID)

	promotionEndsAt := env.fixedClock.Add(48 * time.Hour)
	cutoffAt := env.fixedClock.Add(24 * time.Hour)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, promotionEndsAt)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &cutoffAt)

	open := publicTicketTypes(t, env, "test-org", "promo-and-cutoff")
	ga, balcony := open["GA"], open["Balcony"]
	if ga.Closed {
		t.Fatalf("GA reads closed a day before its cutoff: %+v", ga)
	}
	if ga.Promotion == nil {
		t.Fatalf("a Ticket Type given both a Promotion and a cutoff carries no promotion: %+v", ga)
	}
	if ga.Promotion.PromotionalPriceCents != promoAllInCents || ga.PriceCents != promoAllInCents {
		t.Fatalf("open promoted price = %d (promotion %+v); want the promotional all-in %d",
			ga.PriceCents, *ga.Promotion, promoAllInCents)
	}
	if ga.SalesCutoffAt == nil || !sameInstant(t, *ga.SalesCutoffAt, cutoffAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("sales_cutoff_at = %v on a promoted Ticket Type; want %q", ga.SalesCutoffAt, cutoffAt.UTC().Format(time.RFC3339))
	}
	if got := cardPriceFrom(t, explorerCard(t, env, "promo-and-cutoff")); got != promoAllInCents {
		t.Fatalf("card price_from_cents = %d while the promoted Ticket Type is open; want the promotional all-in %d", got, promoAllInCents)
	}

	// The deadline arrives with a month of the discount still to run.
	holdClocksAt(cutoffAt)
	closed := publicTicketTypes(t, env, "test-org", "promo-and-cutoff")["GA"]
	if !closed.Closed {
		t.Fatalf("a promoted Ticket Type reads open at its own cutoff instant: %+v", closed)
	}
	if closed.Promotion == nil {
		t.Fatalf("the Promotion vanished when the Ticket Type closed: %+v", closed)
	}
	if closed.Promotion.PromotionalPriceCents != promoAllInCents || closed.PriceCents != promoAllInCents {
		t.Fatalf("closed promoted price = %d (promotion %+v); want the %d it was still discounted to",
			closed.PriceCents, *closed.Promotion, promoAllInCents)
	}
	if !sameInstant(t, closed.Promotion.EndsAt, promotionEndsAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("closed promotion ends_at = %q; want the %q it runs to", closed.Promotion.EndsAt, promotionEndsAt.UTC().Format(time.RFC3339))
	}

	// And the card stops advertising the discount, because nobody can pay it.
	if got := cardPriceFrom(t, explorerCard(t, env, "promo-and-cutoff")); got != balcony.PriceCents {
		t.Fatalf("card price_from_cents = %d with the promoted Ticket Type closed; want the still-open Balcony's %d", got, balcony.PriceCents)
	}
	if got := cardPriceFrom(t, orgPageCard(t, env, "test-org", "promo-and-cutoff")); got != balcony.PriceCents {
		t.Fatalf("organization page price_from_cents = %d with the promoted Ticket Type closed; want %d", got, balcony.PriceCents)
	}
}
