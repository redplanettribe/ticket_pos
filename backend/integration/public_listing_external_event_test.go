package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Storefront listing cards for an externally registered Event (issue #211).
// Discovery is most of what an Organization puts an external Event on the
// platform for, so it is listed exactly like a ticketed one — and the card has
// to say which it is, because a card with no price line is otherwise how a
// broken Event looks.

// publishDiscoverableExternalEvent publishes an externally registered Event and
// lists it, which is two steps because discoverability may only be set on an
// Event that is already published. The slug is "external-event", the one
// publishExternalRegistrationEvent gives it.
func publishDiscoverableExternalEvent(t *testing.T, env *testEnv, sessionID, registrationURL string) string {
	t.Helper()
	eventID := publishExternalRegistrationEvent(t, env, sessionID, registrationURL)
	resp, body := env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
		"discoverable": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set external event discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return eventID
}

// explorerCards reads the global explorer's page of cards.
func explorerCards(t *testing.T, env *testEnv) []publicEventCard {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Events []publicEventCard `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode explorer page: %v", err)
	}
	return page.Events
}

// organizationUpcomingCards reads the Organization page's upcoming cards.
func organizationUpcomingCards(t *testing.T, env *testEnv, orgSlug string) []publicEventCard {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/"+orgSlug+"/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("org events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var result struct {
		Upcoming []publicEventCard `json:"upcoming"`
	}
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode org events: %v", err)
	}
	return result.Upcoming
}

// cardBySlug picks one Event's card out of a listing, failing when the listing
// does not hold it — a listing missing the Event is the failure the caller is
// usually really asking about.
func cardBySlug(t *testing.T, where string, cards []publicEventCard, slug string) publicEventCard {
	t.Helper()
	for _, card := range cards {
		if card.Slug == slug {
			return card
		}
	}
	t.Fatalf("%s does not list %q: %+v", where, slug, cards)
	return publicEventCard{}
}

// An external Event and a ticketed one sit side by side in both listings, and
// each card says which of the two it is. Without the mode, the external card's
// null price is indistinguishable from a ticketed Event whose price is missing
// by accident.
func TestPublicListingCardsCarryTheRegistrationMode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	publishDiscoverableExternalEvent(t, env, sessionID, "https://lu.ma/my-meetup")
	publishEvent(t, env, sessionID, "Ticketed Gig", "ticketed-gig",
		env.fixedClock.Add(10*24*time.Hour), true, 2500, 100)

	for _, listing := range []struct {
		where string
		cards []publicEventCard
	}{
		{"the Timeline", explorerCards(t, env)},
		{"the Organization page", organizationUpcomingCards(t, env, "test-org")},
	} {
		external := cardBySlug(t, listing.where, listing.cards, "external-event")
		if external.RegistrationMode != "external" {
			t.Fatalf("%s: external card registration_mode = %q; want external",
				listing.where, external.RegistrationMode)
		}
		// No price, and no pretending to one: what the other site charges is
		// something this platform does not know and never will.
		if external.PriceFromCents != nil {
			t.Fatalf("%s: external card price_from_cents = %d; want null",
				listing.where, *external.PriceFromCents)
		}

		// The ticketed card is completely unchanged: it names its own mode and
		// still quotes the buyer price of its cheapest ticket — 2500¢ under
		// 'pass_on' Fee Handling is 2788¢ all in (ADR 0014).
		ticketed := cardBySlug(t, listing.where, listing.cards, "ticketed-gig")
		if ticketed.RegistrationMode != "tickets" {
			t.Fatalf("%s: ticketed card registration_mode = %q; want tickets",
				listing.where, ticketed.RegistrationMode)
		}
		if ticketed.PriceFromCents == nil || *ticketed.PriceFromCents != 2788 {
			t.Fatalf("%s: ticketed card price_from_cents = %v; want 2788",
				listing.where, ticketed.PriceFromCents)
		}
	}
}

// An externally registered Event has no capacity on this platform to exhaust,
// so it is never sold out — said explicitly, rather than landing there by way of
// an aggregate over zero Ticket Types coming back null. The ticketed Event in
// the same listing proves sold_out still reports what it always did.
func TestExternalEventIsNeverReportedAsSoldOutOnAListingCard(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	publishDiscoverableExternalEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	// A ticketed Event with exactly one ticket, then bought: genuinely sold out.
	exhaustedID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Exhausted Gig", "exhausted-gig", 2500, 1)
	resp, body := env.put(t, "/api/v1/staff/events/"+exhaustedID+"/discoverable", map[string]any{
		"discoverable": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}
	buyOnline(t, env, "exhausted-gig", ticketTypeID, "ana@example.com")

	for _, listing := range []struct {
		where string
		cards []publicEventCard
	}{
		{"the Timeline", explorerCards(t, env)},
		{"the Organization page", organizationUpcomingCards(t, env, "test-org")},
	} {
		if external := cardBySlug(t, listing.where, listing.cards, "external-event"); external.SoldOut {
			t.Fatalf("%s: an externally registered Event is reported sold out, and never may be: %+v",
				listing.where, external)
		}
		if exhausted := cardBySlug(t, listing.where, listing.cards, "exhausted-gig"); !exhausted.SoldOut {
			t.Fatalf("%s: a ticketed Event with no tickets left is not reported sold out: %+v",
				listing.where, exhausted)
		}
	}
}
