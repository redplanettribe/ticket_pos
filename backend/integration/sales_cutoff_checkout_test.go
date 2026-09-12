package integration

import (
	"net/http"
	"testing"
	"time"
)

// THE SALES CUTOFF AT BEGIN-CHECKOUT (#605, parent #602, ADR 0070). The third
// slice, and the only one that refuses anything.
//
// A Customer whose tab was open when the cutoff passed presses Buy and is told
// so, in a code the Storefront can word differently from a sold-out refusal.
// The rule lives in the API rather than only in the page, so a stale tab or a
// copied request cannot sell a closed ticket.
//
// Three properties carry the whole decision, and each has a test of its own
// below because each is a place a later change could quietly do the wrong thing:
//
//   - THE CODE IS ITS OWN. A Ticket Type that is both closed and exhausted
//     answers TICKET_TYPE_CLOSED and not CAPACITY_EXCEEDED. "We stopped selling"
//     invites an email asking you to reopen; "they are all gone" ends the
//     conversation, and telling a half-empty room it is full is a claim the
//     Organization has to answer for.
//   - IT IS JUDGED ONCE. A checkout begun while the window was open settles
//     after it shuts. Nothing is re-judged on the way back from the Payment
//     Provider, so a buyer's money is never taken for a sale that is then
//     refused.
//   - IT BINDS THE STOREFRONT ALONE. A Sale Import row, a Manually Recorded Sale
//     and a Sale Correction's replacement all go through on a Ticket Type the
//     same API refuses to sell online at that very instant. A deliberate
//     departure from capacity and the Purchase Limit, both of which do refuse
//     import rows.
//
// The shape is promotions_checkout_test.go's: an Event published through the
// staff API, a cutoff set through the staff API an organizer would use, and the
// verdict asked of the real begin-checkout route.
//
// HOW THESE TESTS CLOSE A TICKET TYPE. The same two ways sales_cutoff_public_test.go
// uses, for the same reason. Where the cutoff edge itself is under test the
// clock is moved onto it with holdClocksAt, which proves the half-open boundary —
// the cutoff instant itself is closed. Everywhere else the cutoff is set in the
// past and the clock is left alone, because moving the clock forward would also
// lapse the Capacity Holds a pending Payment is standing on.

// beforeTheCutoff is a cutoff already in the past at the suite's fixed clock:
// the intended way to stop selling a Ticket Type this instant, and the setup
// every test below that is not about the boundary itself uses.
func beforeTheCutoff(env *testEnv) *time.Time {
	past := env.fixedClock.Add(-time.Hour)
	return &past
}

// refusedAsClosed asserts a begin-checkout was refused for the Sales Cutoff and
// that the refusal names the Ticket Type that closed.
//
// The code is asserted as hard as the status. A 409 on its own is
// indistinguishable from sold out, and the entire reason this refusal exists
// separately is that the Storefront keys its copy on the code (ADR 0023).
func refusedAsClosed(t *testing.T, env *testEnv, eventSlug, ticketTypeID string, body map[string]any) {
	t.Helper()
	resp, envBody := beginCheckout(t, env, testOrgSlug, eventSlug, body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("begin checkout status=%d error=%+v, want 409", resp.StatusCode, envBody.Error)
	}
	if envBody.Error == nil || envBody.Error.Code != "TICKET_TYPE_CLOSED" {
		t.Fatalf("error=%+v, want TICKET_TYPE_CLOSED", envBody.Error)
	}
	if envBody.Error.Message == "" {
		t.Fatal("TICKET_TYPE_CLOSED carries no message; the Storefront has nothing to fall back on")
	}
	if got := string(envBody.Data); got != "null" {
		t.Fatalf("data = %s on a refusal, want null", got)
	}
	details, ok := envBody.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %+v, want an object naming the Ticket Type", envBody.Error.Details)
	}
	if got := details["ticket_type_id"]; got != ticketTypeID {
		t.Fatalf("details.ticket_type_id = %v, want the closed Ticket Type %s", got, ticketTypeID)
	}
}

// TestBeginCheckoutRefusesATicketTypePastItsSalesCutoff is the feature: the
// window shut while a tab was open, and pressing Buy says so.
//
// The clock is moved exactly ONTO the cutoff rather than past it, because that
// is the edge the half-open rule decides: the cutoff instant itself is closed,
// so a Ticket Type that closes at six is not on sale at six.
func TestBeginCheckoutRefusesATicketTypePastItsSalesCutoff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Cutoff Fest", "cutoff-fest", 1000, 20)
	cutoff := env.fixedClock.Add(6 * time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &cutoff)

	// A second before it shuts the ticket sells exactly as it always has.
	holdClocksAt(cutoff.Add(-time.Second))
	begun := beginCheckoutOK(t, env, testOrgSlug, "cutoff-fest",
		checkoutBody("early@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if begun.ClientTransactionID == "" {
		t.Fatal("a checkout a second before the cutoff produced no Payment")
	}

	// On the instant itself it does not.
	holdClocksAt(cutoff)
	refusedAsClosed(t, env, "cutoff-fest", gaID,
		checkoutBody("late@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
}

// TestBeginCheckoutNamesTheClosedTicketTypeInAMixedBasket: a basket holding one
// open line and one closed one is refused, and the refusal names the line that
// closed rather than the Event or the first row in the cart.
//
// This is what the Storefront's restored-selection needs in order to tell a
// buyer WHICH line it dropped instead of silently charging them for less than
// they chose.
func TestBeginCheckoutNamesTheClosedTicketTypeInAMixedBasket(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Mixed Cart", "mixed-cart", 1000, 20)
	earlyBirdID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Early Bird", 500, 20)
	setSalesCutoff(t, env, sessionID, eventID, earlyBirdID, beforeTheCutoff(env))

	refusedAsClosed(t, env, "mixed-cart", earlyBirdID,
		checkoutBody("both@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": gaID, "quantity": 1},
			map[string]any{"ticket_type_id": earlyBirdID, "quantity": 1}))

	// And the open line on that same Event is untouched by its neighbour's
	// cutoff: closing is a fact about one Ticket Type, never about the Event.
	begun := beginCheckoutOK(t, env, testOrgSlug, "mixed-cart",
		checkoutBody("open@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if begun.ClientTransactionID == "" {
		t.Fatal("the still-open Ticket Type was refused alongside its closed sibling")
	}
}

// TestTheClosedRefusalIsNotTheSoldOutRefusal is the property the code exists
// for, tested where it is hardest: a Ticket Type that is BOTH closed and
// exhausted. It answers TICKET_TYPE_CLOSED.
//
// The control in the same test is the same Ticket Type exhausted and NOT closed,
// which still answers CAPACITY_EXCEEDED — so the assertion is that the cutoff
// changed the answer, and not merely that some 409 came back.
func TestTheClosedRefusalIsNotTheSoldOutRefusal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Last One", "last-one", 1000, 1)

	// Exhaust the single ticket, so the Ticket Type is genuinely sold out.
	begun := beginCheckoutOK(t, env, testOrgSlug, "last-one",
		checkoutBody("first@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Fatalf("sold count = %d, want the Ticket Type exhausted at 1", got)
	}

	// Sold out and open: the buyer is told the Event is full, which it is.
	body := checkoutBody("second@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1})
	resp, envBody := beginCheckout(t, env, testOrgSlug, "last-one", body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("begin checkout status=%d error=%+v, want 409", resp.StatusCode, envBody.Error)
	}
	if envBody.Error == nil || envBody.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("error=%+v, want CAPACITY_EXCEEDED while the Ticket Type is merely exhausted", envBody.Error)
	}

	// Sold out AND closed: the stronger statement about the shop window wins,
	// because "we stopped selling" and "they are all gone" invite different
	// things from the reader and only one of them is worth an email.
	setSalesCutoff(t, env, sessionID, eventID, gaID, beforeTheCutoff(env))
	refusedAsClosed(t, env, "last-one", gaID,
		checkoutBody("third@example.com", "Carla", "Diaz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
}

// TestACheckoutBegunBeforeTheCutoffSettlesAfterIt is user story 36: a Payment
// already started completes even though the cutoff passes while the buyer is on
// the provider's page, so their money is never taken for a sale that is then
// refused.
//
// A REAL cutoff, and a clock that really crosses it. The Ticket Type is given a
// closing instant ten minutes out and the checkout is begun while that instant
// is still in the future — a Ticket Type genuinely open at begin-time, judged
// open, on the terms it started on. The clock is then moved five minutes past
// the cutoff and the return leg settles anyway.
//
// The two numbers are chosen against sales.CapacityHoldWindow, which is 20
// minutes: fifteen minutes of advance crosses the cutoff while leaving the
// Capacity Hold this pending Payment stands on alive, so the only thing that
// changed between begin and confirm is that the window shut. A longer jump
// would lapse the hold and the test would stop being about the cutoff.
//
// The refusal in the middle is load-bearing rather than decorative: it is the
// proof that the clock really is past the cutoff at the moment confirm runs. A
// fresh buyer arriving at that same instant is turned away.
func TestACheckoutBegunBeforeTheCutoffSettlesAfterIt(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Underway", "underway", 1000, 20)

	cutoff := env.fixedClock.Add(10 * time.Minute)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &cutoff)

	// Ten minutes before it shuts: open, and the buyer leaves for the provider.
	begun := beginCheckoutOK(t, env, testOrgSlug, "underway",
		checkoutBody("underway@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	// The window shuts while they are on it — no staff action at all, just the
	// clock arriving at the instant an Org Admin typed.
	holdClocksAt(cutoff.Add(5 * time.Minute))
	refusedAsClosed(t, env, "underway", gaID,
		checkoutBody("newcomer@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// The Payment already under way settles on the terms it started on.
	confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if confirmed.ConfirmationRef == "" {
		t.Fatal("a checkout begun before the cutoff was refused on the way back; nothing may be re-judged there")
	}
	if got := salesCountByEmail(t, env, eventID, "underway@example.com"); got != 1 {
		t.Fatalf("active Ticket Sales = %d, want the one begun before the cutoff", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold count = %d, want the 2 tickets the settled Payment was for", got)
	}
}

// TestConfirmDoesNotReadTheSalesCutoffColumn is the same rule attacked from the
// other side, and the harsher case: rather than the clock reaching a cutoff, an
// organizer writes one INTO THE PAST underneath a Payment that is already at the
// provider. Even a Ticket Type that was never open at any point the confirm can
// see still settles, because confirm does not look at the column at all.
//
// Kept as a test of its own because it pins something story 36 does not: not
// merely "the verdict is taken at begin-checkout", but "the return leg reads
// nothing", which is what makes a mid-Payment edit safe. ADR 0070 accepts the
// cost out loud: a sale can land after its Ticket Type's cutoff, and the Sales
// list will not explain it.
func TestConfirmDoesNotReadTheSalesCutoffColumn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Underway Edit", "underway-edit", 1000, 20)

	begun := beginCheckoutOK(t, env, testOrgSlug, "underway-edit",
		checkoutBody("underway@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	// The organizer closes sales early, an hour ago, while the buyer is out.
	setSalesCutoff(t, env, sessionID, eventID, gaID, beforeTheCutoff(env))
	refusedAsClosed(t, env, "underway-edit", gaID,
		checkoutBody("newcomer@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// The Payment already under way settles on the terms it started on.
	confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if confirmed.ConfirmationRef == "" {
		t.Fatal("a checkout begun before the cutoff was refused on the way back; nothing may be re-judged there")
	}
	if got := salesCountByEmail(t, env, eventID, "underway@example.com"); got != 1 {
		t.Fatalf("active Ticket Sales = %d, want the one begun before the cutoff", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold count = %d, want the 2 tickets the settled Payment was for", got)
	}
}

// TestATicketTypeWithNoSalesCutoffChecksOutAsItAlwaysHas: the feature is inert
// on a Ticket Type nobody has typed a date into, and clearing a cutoff is the
// undo.
//
// Worth its own test because nothing else would catch a cutoff check that read
// a zero time instead of a nil one: every Ticket Type on this platform has NULL
// here, and a comparison against the zero instant would close all of them.
func TestATicketTypeWithNoSalesCutoffChecksOutAsItAlwaysHas(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "No Cutoff", "no-cutoff", 1000, 20)

	// Unset from birth: it sells.
	begun := beginCheckoutOK(t, env, testOrgSlug, "no-cutoff",
		checkoutBody("never@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	// Closed, then reopened by clearing the value: it sells again, with no job
	// having run and no row having changed state.
	setSalesCutoff(t, env, sessionID, eventID, gaID, beforeTheCutoff(env))
	refusedAsClosed(t, env, "no-cutoff", gaID,
		checkoutBody("shut@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	setSalesCutoff(t, env, sessionID, eventID, gaID, nil)
	reopened := beginCheckoutOK(t, env, testOrgSlug, "no-cutoff",
		checkoutBody("again@example.com", "Carla", "Diaz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if reopened.ClientTransactionID == "" {
		t.Fatal("clearing the cutoff did not reopen sales")
	}
}

// TestTheSalesCutoffBindsTheStorefrontAlone: the three ways staff record a sale
// that already happened all go through on a Ticket Type the very same API is
// refusing to sell online at that instant.
//
// One test rather than three, because the asymmetry IS the property: the closed
// verdict and the four accepted writes have to be true at the same moment for
// the departure from capacity and the Purchase Limit to mean anything. A Sale
// Import, a Manually Recorded Sale and a Sale Correction record acts that
// already happened, usually while the window was open, and a correction must
// stay possible for as long as the sale exists.
func TestTheSalesCutoffBindsTheStorefrontAlone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Box Office", "box-office", 1000, 50)
	setSalesCutoff(t, env, sessionID, eventID, gaID, beforeTheCutoff(env))

	// The Storefront is shut, and stays shut for the whole of this test.
	refusedAsClosed(t, env, "box-office", gaID,
		checkoutBody("online@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// A Sale Import row: history, recorded after the fact.
	commitBatch(t, env, sessionID, eventID, "batch-after-cutoff", []map[string]any{
		{"customer_email": "imported@example.com", "customer_first_name": "Bea", "customer_last_name": "Ruiz",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	if got := salesCountByEmail(t, env, eventID, "imported@example.com"); got != 1 {
		t.Fatalf("imported Ticket Sales = %d, want the row to have been recorded despite the cutoff", got)
	}

	// A Manually Recorded Sale: somebody paid at the door, or by transfer, and a
	// Member is writing it down.
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("manual@example.com", "Carla", "Diaz", gaID, 1, "cash", "2026-07-02T10:00:00Z"))
	if got := salesCountByEmail(t, env, eventID, "manual@example.com"); got != 1 {
		t.Fatalf("manually recorded Ticket Sales = %d, want one despite the cutoff", got)
	}

	// A Sale Correction's replacement: the mistake in the imported row is fixed
	// long after the window shut, which is the whole point of the departure.
	imported := saleRowByEmail(t, env, sessionID, eventID, "imported@example.com", "active")
	corrected := correctImportedSaleOK(t, env, sessionID, eventID, imported.ID,
		correctionBody("corrected@example.com", "Bea", "Ruiz", gaID, 3, "cash", "2026-07-01T10:00:00Z"))
	if corrected.ReplacementSaleID == "" {
		t.Fatal("the correction produced no replacement sale on a closed Ticket Type")
	}
	if got := salesCountByEmail(t, env, eventID, "corrected@example.com"); got != 1 {
		t.Fatalf("corrected Ticket Sales = %d, want the replacement to exist despite the cutoff", got)
	}

	// 2 imported, reversed by the correction, replaced with 3, plus 1 manual.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 4 {
		t.Fatalf("sold count = %d, want 4 — a closed Ticket Type still accepts every staff-recorded sale", got)
	}

	// And after all of it the Storefront is still shut: none of the writes above
	// reopened anything, because none of them touched the cutoff.
	refusedAsClosed(t, env, "box-office", gaID,
		checkoutBody("still@example.com", "Dora", "Paz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
}

// TestAPromotedTicketTypeIsStillRefusedPastItsCutoff is user story 42 at the
// refusing seam: a Promotion and a Sales Cutoff coexist, and the discount buys
// no exemption from the deadline.
//
// The windows deliberately overlap — the cutoff falls a day and a half inside a
// two-day Promotion — which is the arrangement ADR 0070 refuses to validate
// against and therefore the one an organizer will actually create. Worth
// refusing under a live Promotion specifically because both are windows read off
// the same clock at the same moment in begin-checkout: a cutoff check that
// accidentally rode on the promotional branch would sell this ticket, and every
// other test in this file has no Promotion to catch it.
func TestAPromotedTicketTypeIsStillRefusedPastItsCutoff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Promo Cutoff", "promo-cutoff", promoListPriceCents, 20)
	setFeeHandling(t, env, sessionID, eventID, "Promo Cutoff", "promo-cutoff", "pass_on")
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))
	cutoff := env.fixedClock.Add(6 * time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &cutoff)

	// A second before it shuts, the buyer pays the discounted price: both the
	// Promotion and the cutoff are in force and neither has cancelled the other.
	holdClocksAt(cutoff.Add(-time.Second))
	begun := beginCheckoutOK(t, env, testOrgSlug, "promo-cutoff",
		checkoutBody("early@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if begun.AmountCents != promoAllInCents {
		t.Fatalf("charged amount = %d a second before the cutoff; want the promotional all-in %d — the cutoff must not have disturbed the price",
			begun.AmountCents, promoAllInCents)
	}

	// At the cutoff instant the Promotion still has a day and a half to run, and
	// the Ticket Type is refused all the same.
	holdClocksAt(cutoff)
	refusedAsClosed(t, env, "promo-cutoff", gaID,
		checkoutBody("late@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	// The Promotion is unharmed by the closing and by the edit that set it: an
	// Org Admin who reopens the Ticket Type gets their discount back untouched,
	// at the price it always quoted.
	setSalesCutoff(t, env, sessionID, eventID, gaID, nil)
	reopened := beginCheckoutOK(t, env, testOrgSlug, "promo-cutoff",
		checkoutBody("again@example.com", "Carla", "Diaz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if reopened.AmountCents != promoAllInCents {
		t.Fatalf("charged amount = %d after clearing the cutoff; want the still-live promotional all-in %d",
			reopened.AmountCents, promoAllInCents)
	}
}
