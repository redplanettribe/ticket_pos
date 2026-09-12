package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TICKETS SOLD ON THE PUBLIC EVENT PAGE (#623, parent #619, ADR 0072). The
// Storefront states how many people are going, and the number it states is the
// platform's Tickets Sold figure — Ticket Sale Line quantities on active Ticket
// Sales, every Sales Channel counted — floored at five and null beneath.
//
// Everything here is read off the public detail over HTTP, exactly as the
// Storefront reads it. Nothing reaches into the query or the constant: the
// contract under test is the field's value, and whether the key is there at
// all. The card projection (#624) is asserted in its own file; the helpers
// below are written so it can share them.

// publicTicketsSold reads the Storefront event page and returns its
// tickets_sold field as the raw JSON it travelled as, so a test can tell a key
// carrying null from a key that is missing. Both decode to a nil *int, and
// only one of them is the contract: the Storefront branches on null, and a
// client that never receives the key learns nothing about whether the figure
// is withheld or the field forgotten.
func publicTicketsSold(t *testing.T, env *testEnv, orgSlug, eventSlug string) json.RawMessage {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &fields); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	raw, present := fields["tickets_sold"]
	if !present {
		t.Fatalf("public event carries no tickets_sold key at all; the field must be present, null beneath the floor: %s", body.Data)
	}
	return raw
}

// assertTicketsSoldNull asserts the figure is withheld: the key is present and
// its value is JSON null — never 0, never absent.
func assertTicketsSoldNull(t *testing.T, env *testEnv, orgSlug, eventSlug, why string) {
	t.Helper()
	raw := publicTicketsSold(t, env, orgSlug, eventSlug)
	if string(raw) != "null" {
		t.Fatalf("%s: tickets_sold = %s, want null", why, raw)
	}
}

// assertTicketsSold asserts the figure is stated as exactly want.
func assertTicketsSold(t *testing.T, env *testEnv, orgSlug, eventSlug string, want int, why string) {
	t.Helper()
	raw := publicTicketsSold(t, env, orgSlug, eventSlug)
	var got *int
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s: tickets_sold = %s is not an integer: %v", why, raw, err)
	}
	if got == nil {
		t.Fatalf("%s: tickets_sold = null, want %d", why, want)
	}
	if *got != want {
		t.Fatalf("%s: tickets_sold = %d, want %d", why, *got, want)
	}
}

// TestTicketsSoldIsNullBeneathTheFloorAndStatedAtIt walks the floor from the
// Customer's side: a brand-new Event says nothing, and so does one that has
// sold one or four — the key is there and it is null, not 0 (user stories 11,
// 12, 13 and 29). The fifth ticket is the first the page states.
func TestTicketsSoldIsNullBeneathTheFloorAndStatedAtIt(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Floor Fest", "floor-fest", 0, 50)

	assertTicketsSoldNull(t, env, testOrgSlug, "floor-fest", "an Event that has sold nothing")

	claimFree(t, env, "floor-fest", gaID, "one@example.com", 1)
	assertTicketsSoldNull(t, env, testOrgSlug, "floor-fest", "an Event that has sold one ticket")

	claimFree(t, env, "floor-fest", gaID, "three@example.com", 3)
	assertTicketsSoldNull(t, env, testOrgSlug, "floor-fest", "an Event that has sold four tickets")

	claimFree(t, env, "floor-fest", gaID, "five@example.com", 1)
	assertTicketsSold(t, env, testOrgSlug, "floor-fest", 5, "an Event that has sold exactly five")
}

// TestTicketsSoldIsStatedWholeAboveTheFloor: the figure is the number, not a
// bucket (out of scope: "50+ going"). Two buyers of twenty each read as forty.
func TestTicketsSoldIsStatedWholeAboveTheFloor(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Forty Fest", "forty-fest", 0, 50)

	claimFree(t, env, "forty-fest", gaID, "ana@example.com", 20)
	claimFree(t, env, "forty-fest", gaID, "bob@example.com", 20)

	assertTicketsSold(t, env, testOrgSlug, "forty-fest", 40, "an Event that has sold forty")
}

// TestTicketsSoldSumsEverySalesChannel is user stories 21 and 22: a ticket
// sold at the door or transcribed from another platform fills a seat exactly
// as one sold online does. Each channel alone is beneath the floor here, so
// the page states a figure only because it counts all of them.
func TestTicketsSoldSumsEverySalesChannel(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Channel Fest", "channel-fest", 0, 50)

	// Three online.
	claimFree(t, env, "channel-fest", gaID, "ana@example.com", 3)
	assertTicketsSoldNull(t, env, testOrgSlug, "channel-fest", "three sold online")

	// Two more typed in by hand — a Manually Recorded Sale rides the `import`
	// channel with no batch (ADR 0052).
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("door@example.com", "Dora", "Door", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	assertTicketsSold(t, env, testOrgSlug, "channel-fest", 5, "three online plus two recorded by hand")

	// Four more from a Sale Import batch.
	commitBatch(t, env, sessionID, eventID, "channel-fest-batch", []map[string]any{
		{"customer_email": "imp@example.com", "customer_first_name": "Ima", "customer_last_name": "Port", "ticket_type_id": gaID, "quantity": 4, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})
	assertTicketsSold(t, env, testOrgSlug, "channel-fest", 9, "three online, two by hand and four imported")
}

// TestTicketsSoldIgnoresACapacityHoldInFlight is user story 18: a pending
// Payment holds stock but is not a sale, so the figure states sales and not
// intentions. The hold still counts against capacity (ADR 0013), which is why
// the same page can call the Ticket Type sold out while stating fewer going
// than it holds seats — the two figures answer different questions.
func TestTicketsSoldIgnoresACapacityHoldInFlight(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Hold Fest", "hold-fest", 1000, 7)

	begun := beginCheckoutOK(t, env, testOrgSlug, "hold-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 5)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	assertTicketsSold(t, env, testOrgSlug, "hold-fest", 5, "five paid and settled")

	// Two more begun and never settled: a live Capacity Hold over the last two
	// seats.
	beginCheckoutOK(t, env, testOrgSlug, "hold-fest",
		checkoutBody("bob@example.com", "Bob", "Ng", cartLine(gaID, 2)))

	if ga := publicTicketTypes(t, env, testOrgSlug, "hold-fest")["GA"]; !ga.SoldOut {
		t.Fatalf("GA is not sold out with five sold and two held over a capacity of seven: %+v", ga)
	}
	assertTicketsSold(t, env, testOrgSlug, "hold-fest", 5, "five settled while two are held")
}

// TestTicketsSoldDropsWithASaleReversal is user story 17: a reversed Ticket
// Sale stops counting, whole-Sale. The figure falls to the floor and is still
// stated; the next reversal takes it beneath, and the page goes quiet again.
func TestTicketsSoldDropsWithASaleReversal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Undo Fest", "undo-fest", 0, 50)

	anaRef := claimFree(t, env, "undo-fest", gaID, "ana@example.com", 5)
	bobRef := claimFree(t, env, "undo-fest", gaID, "bob@example.com", 2)
	assertTicketsSold(t, env, testOrgSlug, "undo-fest", 7, "seven claimed")

	undoOwnSale(t, env, "bob@example.com", bobRef)
	assertTicketsSold(t, env, testOrgSlug, "undo-fest", 5, "Bob undid his two")

	undoOwnSale(t, env, "ana@example.com", anaRef)
	assertTicketsSoldNull(t, env, testOrgSlug, "undo-fest", "Ana undid her five as well")
}

// TestTicketsSoldSurvivesTheSalesCutoff is user story 10: a Ticket Type past
// its Sales Cutoff can no longer be bought, so the card's "from" price stops
// quoting it (ADR 0070) — but the tickets it sold are still going, and the
// page keeps stating them beside the closed verdict.
func TestTicketsSoldSurvivesTheSalesCutoff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Cutoff Fest", "cutoff-fest", 1000, 20)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 4000, 20)
	makeDiscoverable(t, env, sessionID, eventID)

	begun := beginCheckoutOK(t, env, testOrgSlug, "cutoff-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 6)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	assertTicketsSold(t, env, testOrgSlug, "cutoff-fest", 6, "six GA sold before the cutoff")

	closedAt := env.fixedClock.Add(-time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &closedAt)

	types := publicTicketTypes(t, env, testOrgSlug, "cutoff-fest")
	if !types["GA"].Closed {
		t.Fatalf("GA is not closed after its cutoff: %+v", types["GA"])
	}
	assertTicketsSold(t, env, testOrgSlug, "cutoff-fest", 6, "six GA sold after GA closed")

	// The "from" price has gone quiet on the closed Ticket Type: the card quotes
	// the still-open Balcony, not the cheaper GA that nobody can buy.
	card := orgPageCard(t, env, testOrgSlug, "cutoff-fest")
	if got := cardPriceFrom(t, card); got != types["Balcony"].PriceCents {
		t.Fatalf("price_from_cents = %d, want the still-open Balcony's %d", got, types["Balcony"].PriceCents)
	}
}

// TestTicketsSoldIsNullOnAnExternalRegistrationEvent is user story 14: an
// Event that hands its audience to a Registration Link sells nothing here and
// so has no Tickets Sold to state. Its click count is never dressed as people.
func TestTicketsSoldIsNullOnAnExternalRegistrationEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishExternalRegistrationEvent(t, env, sessionID, "https://tickets.example.com/register")

	assertTicketsSoldNull(t, env, testOrgSlug, "external-event", "an Event with External Registration")
}

// TestTicketsSoldIsStatedOnAHiddenAndOnAnEndedEvent: every reachable Event
// states it, Discoverable or not, and the tense never changes once the Event
// is over (user story 5, ADR 0072). Ended is staged the way the rest of the
// suite stages it, by moving the start into the past after the sales are in.
func TestTicketsSoldIsStatedOnAHiddenAndOnAnEndedEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Quiet Fest", "quiet-fest", 0, 50)

	claimFree(t, env, "quiet-fest", gaID, "ana@example.com", 8)

	// publishCheckoutEvent leaves the Event published but not Discoverable.
	resp, body := env.get(t, "/api/v1/public/organizations/"+testOrgSlug+"/events/quiet-fest", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var detail struct {
		Discoverable bool `json:"discoverable"`
		HasEnded     bool `json:"has_ended"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	if detail.Discoverable || detail.HasEnded {
		t.Fatalf("fixture is not a hidden, upcoming Event: %+v", detail)
	}
	assertTicketsSold(t, env, testOrgSlug, "quiet-fest", 8, "a non-Discoverable Event")

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(-48*time.Hour)); err != nil {
		t.Fatalf("move event into the past: %v", err)
	}
	resp, body = env.get(t, "/api/v1/public/organizations/"+testOrgSlug+"/events/quiet-fest", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	if !detail.HasEnded {
		t.Fatal("has_ended = false after the start was moved two days back")
	}
	assertTicketsSold(t, env, testOrgSlug, "quiet-fest", 8, "an Event that is over")
}
