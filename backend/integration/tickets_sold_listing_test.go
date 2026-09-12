package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TICKETS SOLD ON THE LISTING CARDS (#624, parent #619, ADR 0072). The card
// projection serves two reads — the global explorer and the Organization page
// — and both state the same nullable tickets_sold the Event page states
// (#623): the same lateral sum, the same floor, the same null contract, and
// never narrowed by the Sales Cutoff.
//
// Every assertion here is made over HTTP against BOTH listings, and against
// the Event page as well, because the promise under test is not that a card
// carries a number but that the same Event never states a number in one list
// and nothing in another, nor a different number from its own page. The
// detail helpers live in tickets_sold_public_test.go and are reused as is.

// listingTicketsSold reads one listing endpoint, finds the card with the given
// slug in the named list, and returns its tickets_sold as the raw JSON it
// travelled as — so, exactly as on the detail, a key carrying null can be
// told from a key that is missing. The card struct the rest of the suite
// decodes into cannot make that distinction, which is why this reads a map.
func listingTicketsSold(t *testing.T, env *testEnv, path, list, eventSlug string) json.RawMessage {
	t.Helper()
	resp, body := env.get(t, path, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d error=%+v", path, resp.StatusCode, body.Error)
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var cards []map[string]json.RawMessage
	if err := json.Unmarshal(page[list], &cards); err != nil {
		t.Fatalf("decode %s list %q: %v", path, list, err)
	}
	for _, card := range cards {
		var slug string
		if err := json.Unmarshal(card["slug"], &slug); err != nil || slug != eventSlug {
			continue
		}
		raw, present := card["tickets_sold"]
		if !present {
			t.Fatalf("%s card in %s carries no tickets_sold key at all; the field must be present, null beneath the floor: %s", eventSlug, path, body.Data)
		}
		return raw
	}
	t.Fatalf("event %s not in %s list %q: %s", eventSlug, path, list, body.Data)
	return nil
}

// explorerTicketsSold is the global explorer's card figure for the Event.
func explorerTicketsSold(t *testing.T, env *testEnv, eventSlug string) json.RawMessage {
	t.Helper()
	return listingTicketsSold(t, env, "/api/v1/public/events", "events", eventSlug)
}

// orgPageTicketsSold is the Organization page's card figure for the Event,
// read from the named split — "upcoming" or "past".
func orgPageTicketsSold(t *testing.T, env *testEnv, orgSlug, list, eventSlug string) json.RawMessage {
	t.Helper()
	return listingTicketsSold(t, env, "/api/v1/public/organizations/"+orgSlug+"/events", list, eventSlug)
}

// assertCardTicketsSoldNull asserts an upcoming, Discoverable Event's figure
// is withheld on the explorer card, on the Organization page card and on its
// own page alike: the key present, its value null.
func assertCardTicketsSoldNull(t *testing.T, env *testEnv, eventSlug, why string) {
	t.Helper()
	for surface, raw := range map[string]json.RawMessage{
		"explorer card":          explorerTicketsSold(t, env, eventSlug),
		"organization page card": orgPageTicketsSold(t, env, testOrgSlug, "upcoming", eventSlug),
		"event page":             publicTicketsSold(t, env, testOrgSlug, eventSlug),
	} {
		if string(raw) != "null" {
			t.Fatalf("%s: %s tickets_sold = %s, want null", why, surface, raw)
		}
	}
}

// assertCardTicketsSold asserts an upcoming, Discoverable Event states exactly
// want on the explorer card, on the Organization page card and on its own
// page — one figure on every surface that names the Event.
func assertCardTicketsSold(t *testing.T, env *testEnv, eventSlug string, want int, why string) {
	t.Helper()
	for surface, raw := range map[string]json.RawMessage{
		"explorer card":          explorerTicketsSold(t, env, eventSlug),
		"organization page card": orgPageTicketsSold(t, env, testOrgSlug, "upcoming", eventSlug),
		"event page":             publicTicketsSold(t, env, testOrgSlug, eventSlug),
	} {
		assertRawTicketsSold(t, raw, want, why+": "+surface)
	}
}

// assertRawTicketsSold asserts one raw figure is the integer want.
func assertRawTicketsSold(t *testing.T, raw json.RawMessage, want int, why string) {
	t.Helper()
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

// publishListedEvent publishes a checkout Event and makes it Discoverable, so
// it sits on both listings as well as on its own page.
func publishListedEvent(t *testing.T, env *testEnv, sessionID, name, slug string, priceCents, capacity int) (eventID, ticketTypeID string) {
	t.Helper()
	eventID, ticketTypeID = publishCheckoutEvent(t, env, sessionID, name, slug, priceCents, capacity)
	makeDiscoverable(t, env, sessionID, eventID)
	return eventID, ticketTypeID
}

// TestTicketsSoldCardIsNullBeneathTheFloorAndStatedAtIt walks the floor on
// the cards exactly as the Event page walks it: nothing, one and four are
// null on both listings — the key there, never 0 — and the fifth ticket is
// the first the cards state (user stories 11-13 and 29).
func TestTicketsSoldCardIsNullBeneathTheFloorAndStatedAtIt(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishListedEvent(t, env, sessionID, "Card Floor Fest", "card-floor-fest", 0, 50)

	assertCardTicketsSoldNull(t, env, "card-floor-fest", "an Event that has sold nothing")

	claimFree(t, env, "card-floor-fest", gaID, "one@example.com", 1)
	assertCardTicketsSoldNull(t, env, "card-floor-fest", "an Event that has sold one ticket")

	claimFree(t, env, "card-floor-fest", gaID, "three@example.com", 3)
	assertCardTicketsSoldNull(t, env, "card-floor-fest", "an Event that has sold four tickets")

	claimFree(t, env, "card-floor-fest", gaID, "five@example.com", 1)
	assertCardTicketsSold(t, env, "card-floor-fest", 5, "an Event that has sold exactly five")
}

// TestTicketsSoldCardIsStatedWholeAboveTheFloor: the card states the number,
// not a bucket, and the number is the Event page's — asserted side by side
// here, raw JSON against raw JSON, because "the card never disagrees with the
// page" is the whole reason the card reads the same field.
func TestTicketsSoldCardIsStatedWholeAboveTheFloor(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishListedEvent(t, env, sessionID, "Card Forty Fest", "card-forty-fest", 0, 50)

	claimFree(t, env, "card-forty-fest", gaID, "ana@example.com", 20)
	claimFree(t, env, "card-forty-fest", gaID, "bob@example.com", 20)

	assertCardTicketsSold(t, env, "card-forty-fest", 40, "an Event that has sold forty")

	detail := publicTicketsSold(t, env, testOrgSlug, "card-forty-fest")
	if explorer := explorerTicketsSold(t, env, "card-forty-fest"); string(explorer) != string(detail) {
		t.Fatalf("explorer card tickets_sold = %s, event page = %s; the same Event states two figures", explorer, detail)
	}
	if org := orgPageTicketsSold(t, env, testOrgSlug, "upcoming", "card-forty-fest"); string(org) != string(detail) {
		t.Fatalf("organization page card tickets_sold = %s, event page = %s; the same Event states two figures", org, detail)
	}
}

// TestTicketsSoldCardSumsEverySalesChannel is user stories 21 and 22 on the
// cards: each channel alone is beneath the floor, and the cards state a
// figure only because they count all of them — online, by hand and imported.
func TestTicketsSoldCardSumsEverySalesChannel(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishListedEvent(t, env, sessionID, "Card Channel Fest", "card-channel-fest", 0, 50)

	claimFree(t, env, "card-channel-fest", gaID, "ana@example.com", 3)
	assertCardTicketsSoldNull(t, env, "card-channel-fest", "three sold online")

	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("door@example.com", "Dora", "Door", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	assertCardTicketsSold(t, env, "card-channel-fest", 5, "three online plus two recorded by hand")

	commitBatch(t, env, sessionID, eventID, "card-channel-fest-batch", []map[string]any{
		{"customer_email": "imp@example.com", "customer_first_name": "Ima", "customer_last_name": "Port", "ticket_type_id": gaID, "quantity": 4, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})
	assertCardTicketsSold(t, env, "card-channel-fest", 9, "three online, two by hand and four imported")
}

// TestTicketsSoldCardIgnoresACapacityHoldInFlight is user story 18 on the
// cards: a live Capacity Hold fills the last seats and flips the card to sold
// out, and the going figure does not move — the badge counts seats taken, the
// line counts sales made, and a card shows both at once without contradiction.
func TestTicketsSoldCardIgnoresACapacityHoldInFlight(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishListedEvent(t, env, sessionID, "Card Hold Fest", "card-hold-fest", 1000, 7)

	begun := beginCheckoutOK(t, env, testOrgSlug, "card-hold-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 5)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	assertCardTicketsSold(t, env, "card-hold-fest", 5, "five paid and settled")
	if card := orgPageCard(t, env, testOrgSlug, "card-hold-fest"); card.SoldOut {
		t.Fatalf("card reads sold out with two seats free: %+v", card)
	}

	beginCheckoutOK(t, env, testOrgSlug, "card-hold-fest",
		checkoutBody("bob@example.com", "Bob", "Ng", cartLine(gaID, 2)))

	for _, card := range []publicEventCard{
		explorerCard(t, env, "card-hold-fest"),
		orgPageCard(t, env, testOrgSlug, "card-hold-fest"),
	} {
		if !card.SoldOut {
			t.Fatalf("card is not sold out with five sold and two held over a capacity of seven: %+v", card)
		}
	}
	assertCardTicketsSold(t, env, "card-hold-fest", 5, "five settled while two are held")
}

// TestTicketsSoldCardDropsWithASaleReversal is user story 17 on the cards: a
// reversed Ticket Sale stops counting whole-Sale, the figure falls to the
// floor and is still stated, and the next reversal takes both cards quiet.
func TestTicketsSoldCardDropsWithASaleReversal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishListedEvent(t, env, sessionID, "Card Undo Fest", "card-undo-fest", 0, 50)

	anaRef := claimFree(t, env, "card-undo-fest", gaID, "ana@example.com", 5)
	bobRef := claimFree(t, env, "card-undo-fest", gaID, "bob@example.com", 2)
	assertCardTicketsSold(t, env, "card-undo-fest", 7, "seven claimed")

	undoOwnSale(t, env, "bob@example.com", bobRef)
	assertCardTicketsSold(t, env, "card-undo-fest", 5, "Bob undid his two")

	undoOwnSale(t, env, "ana@example.com", anaRef)
	assertCardTicketsSoldNull(t, env, "card-undo-fest", "Ana undid her five as well")
}

// TestTicketsSoldCardSurvivesTheSalesCutoff is user story 10 on the cards: a
// Ticket Type past its cutoff drops out of the "from" price on both cards
// (ADR 0070), and the tickets it sold keep counting on both — the aggregate
// the price reads is narrowed by the cutoff and the one the figure reads is
// not, and this is the test that keeps them apart.
func TestTicketsSoldCardSurvivesTheSalesCutoff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishListedEvent(t, env, sessionID, "Card Cutoff Fest", "card-cutoff-fest", 1000, 20)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 4000, 20)

	begun := beginCheckoutOK(t, env, testOrgSlug, "card-cutoff-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 6)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	assertCardTicketsSold(t, env, "card-cutoff-fest", 6, "six GA sold before the cutoff")

	types := publicTicketTypes(t, env, testOrgSlug, "card-cutoff-fest")
	for _, card := range []publicEventCard{
		explorerCard(t, env, "card-cutoff-fest"),
		orgPageCard(t, env, testOrgSlug, "card-cutoff-fest"),
	} {
		if got := cardPriceFrom(t, card); got != types["GA"].PriceCents {
			t.Fatalf("price_from_cents = %d before the cutoff, want GA's %d", got, types["GA"].PriceCents)
		}
	}

	closedAt := env.fixedClock.Add(-time.Hour)
	setSalesCutoff(t, env, sessionID, eventID, gaID, &closedAt)

	types = publicTicketTypes(t, env, testOrgSlug, "card-cutoff-fest")
	if !types["GA"].Closed {
		t.Fatalf("GA is not closed after its cutoff: %+v", types["GA"])
	}
	for _, card := range []publicEventCard{
		explorerCard(t, env, "card-cutoff-fest"),
		orgPageCard(t, env, testOrgSlug, "card-cutoff-fest"),
	} {
		if got := cardPriceFrom(t, card); got != types["Balcony"].PriceCents {
			t.Fatalf("price_from_cents = %d after GA closed, want the still-open Balcony's %d", got, types["Balcony"].PriceCents)
		}
	}
	assertCardTicketsSold(t, env, "card-cutoff-fest", 6, "six GA sold after GA closed")
}

// TestTicketsSoldCardIsNullOnAnExternalRegistrationEvent is user story 14 on
// the cards: an Event that registers elsewhere sells nothing here, so both
// cards carry the key and carry null, beside the mode that lets the Storefront
// say "Register" in the price slot instead of a price.
func TestTicketsSoldCardIsNullOnAnExternalRegistrationEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://tickets.example.com/register")
	makeDiscoverable(t, env, sessionID, eventID)

	for _, card := range []publicEventCard{
		explorerCard(t, env, "external-event"),
		orgPageCard(t, env, testOrgSlug, "external-event"),
	} {
		if card.RegistrationMode != "external" {
			t.Fatalf("card registration_mode = %q, want external: %+v", card.RegistrationMode, card)
		}
	}
	assertCardTicketsSoldNull(t, env, "external-event", "an Event with External Registration")
}

// TestTicketsSoldCardIsStatedOnAnOverEventInThePastList: the Organization
// page keeps stating an Event that is over, in the same tense, on its past
// list (user story 5, ADR 0072) — and states the same figure its page does.
// The global explorer drops ended Events, so it has no card to ask. Ended is
// staged the way the rest of the suite stages it, by moving the start into
// the past after the sales are in.
func TestTicketsSoldCardIsStatedOnAnOverEventInThePastList(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishListedEvent(t, env, sessionID, "Card Over Fest", "card-over-fest", 0, 50)

	claimFree(t, env, "card-over-fest", gaID, "ana@example.com", 8)
	assertCardTicketsSold(t, env, "card-over-fest", 8, "eight sold while upcoming")

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(-48*time.Hour)); err != nil {
		t.Fatalf("move event into the past: %v", err)
	}

	past := orgPageTicketsSold(t, env, testOrgSlug, "past", "card-over-fest")
	assertRawTicketsSold(t, past, 8, "an Event that is over, on the past list")
	if detail := publicTicketsSold(t, env, testOrgSlug, "card-over-fest"); string(past) != string(detail) {
		t.Fatalf("past card tickets_sold = %s, event page = %s; the same Event states two figures", past, detail)
	}
}
