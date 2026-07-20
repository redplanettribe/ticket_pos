package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// publishEvent creates a draft event, fills publishable fields, adds a ticket type,
// publishes it, and (optionally) marks it discoverable via the discoverability
// endpoint, which requires the event to already be published. Returns the event ID.
func publishEvent(t *testing.T, env *testEnv, sessionID, name, slug string, startsAt time.Time, discoverable bool, priceCents, capacity int) string {
	t.Helper()
	eventID := createDraftEvent(t, env, sessionID, name, slug)

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":       name,
		"slug":       slug,
		"starts_at":  startsAt.Format(time.RFC3339),
		"timezone":   "America/New_York",
		"venue_name": "The Hall",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        "General Admission",
		"price_cents": priceCents,
		"capacity":    capacity,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	if discoverable {
		resp, body = env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
			"discoverable": true,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("set discoverable status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}

	return eventID
}

type publicEventCard struct {
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	PriceFromCents *int   `json:"price_from_cents"`
	SoldOut        bool   `json:"sold_out"`
	Organization   struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"organization"`
}

func TestPublicGlobalExplorerListsDiscoverableUpcoming(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	later := env.fixedClock.Add(30 * 24 * time.Hour)
	past := env.fixedClock.Add(-10 * 24 * time.Hour)

	// Discoverable + upcoming: should appear, soonest first.
	publishEvent(t, env, sessionID, "Later Event", "later-event", later, true, 5000, 100)
	publishEvent(t, env, sessionID, "Soon Event", "soon-event", soon, true, 2500, 100)
	// Published but not discoverable: excluded.
	publishEvent(t, env, sessionID, "Hidden Event", "hidden-event", soon, false, 1000, 100)
	// Discoverable but in the past: excluded from the global explorer.
	publishEvent(t, env, sessionID, "Past Event", "past-event", past, true, 1000, 100)

	resp, body := env.get(t, "/api/v1/public/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public events status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var page struct {
		Events     []publicEventCard `json:"events"`
		NextCursor *string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}

	if len(page.Events) != 2 {
		t.Fatalf("expected 2 discoverable upcoming events, got %d: %+v", len(page.Events), page.Events)
	}
	if page.Events[0].Slug != "soon-event" || page.Events[1].Slug != "later-event" {
		t.Fatalf("expected soonest first, got %s then %s", page.Events[0].Slug, page.Events[1].Slug)
	}
	if page.Events[0].PriceFromCents == nil || *page.Events[0].PriceFromCents != 2500 {
		t.Fatalf("expected price_from_cents 2500, got %+v", page.Events[0].PriceFromCents)
	}
	if page.Events[0].Organization.Slug != "test-org" {
		t.Fatalf("expected organization test-org, got %q", page.Events[0].Organization.Slug)
	}
}

func TestPublicGlobalExplorerSearchAndCursor(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	base := env.fixedClock.Add(24 * time.Hour)
	publishEvent(t, env, sessionID, "Jazz Night", "jazz-night", base, true, 1000, 100)
	publishEvent(t, env, sessionID, "Synth Live", "synth-live", base.Add(time.Hour), true, 1000, 100)
	publishEvent(t, env, sessionID, "Rock Show", "rock-show", base.Add(2*time.Hour), true, 1000, 100)

	// Search matches event name.
	resp, body := env.get(t, "/api/v1/public/events?q=jazz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var searchPage struct {
		Events []publicEventCard `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &searchPage); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchPage.Events) != 1 || searchPage.Events[0].Slug != "jazz-night" {
		t.Fatalf("expected only jazz-night, got %+v", searchPage.Events)
	}

	// Paging: limit 2 yields a cursor; following it returns the remainder.
	resp, body = env.get(t, "/api/v1/public/events?limit=2", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page1 status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page1 struct {
		Events     []publicEventCard `json:"events"`
		NextCursor *string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(body.Data, &page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1.Events) != 2 || page1.NextCursor == nil {
		t.Fatalf("expected 2 events and a cursor, got %d cursor=%v", len(page1.Events), page1.NextCursor)
	}

	resp, body = env.get(t, "/api/v1/public/events?limit=2&cursor="+*page1.NextCursor, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page2 status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page2 struct {
		Events     []publicEventCard `json:"events"`
		NextCursor *string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(body.Data, &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2.Events) != 1 || page2.Events[0].Slug != "rock-show" || page2.NextCursor != nil {
		t.Fatalf("expected final rock-show page, got %+v cursor=%v", page2.Events, page2.NextCursor)
	}
}

func TestPublicOrganizationEventsSplitUpcomingAndPast(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	upcoming := env.fixedClock.Add(10 * 24 * time.Hour)
	past := env.fixedClock.Add(-5 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Upcoming Gig", "upcoming-gig", upcoming, true, 1000, 100)
	publishEvent(t, env, sessionID, "Past Gig", "past-gig", past, true, 1000, 100)
	publishEvent(t, env, sessionID, "Hidden Gig", "hidden-gig", upcoming, false, 1000, 100)

	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("org events status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var result struct {
		Organization struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"organization"`
		Upcoming []publicEventCard `json:"upcoming"`
		Past     []publicEventCard `json:"past"`
	}
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode org events: %v", err)
	}
	if result.Organization.Slug != "test-org" {
		t.Fatalf("expected org test-org, got %+v", result.Organization)
	}
	if len(result.Upcoming) != 1 || result.Upcoming[0].Slug != "upcoming-gig" {
		t.Fatalf("expected only upcoming-gig upcoming, got %+v", result.Upcoming)
	}
	if len(result.Past) != 1 || result.Past[0].Slug != "past-gig" {
		t.Fatalf("expected only past-gig past, got %+v", result.Past)
	}
}

func TestPublicOrganizationEventsNotFound(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	resp, body := env.get(t, "/api/v1/public/organizations/nope/events", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "ORGANIZATION_NOT_FOUND" {
		t.Fatalf("expected ORGANIZATION_NOT_FOUND, got %+v", body.Error)
	}
}

func TestPublicEventPageReachableByDirectLinkEvenWhenHidden(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	// Not discoverable, but published: must be reachable by direct link.
	publishEvent(t, env, sessionID, "Hidden Event", "hidden-event", soon, false, 4200, 3)

	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/hidden-event", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event page status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var detail struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		HasEnded    bool   `json:"has_ended"`
		TicketTypes []struct {
			Name       string `json:"name"`
			PriceCents int    `json:"price_cents"`
			Remaining  int    `json:"remaining"`
			SoldOut    bool   `json:"sold_out"`
		} `json:"ticket_types"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Slug != "hidden-event" || detail.HasEnded {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if len(detail.TicketTypes) != 1 {
		t.Fatalf("expected 1 ticket type, got %d", len(detail.TicketTypes))
	}
	tt := detail.TicketTypes[0]
	if tt.PriceCents != 4200 || tt.Remaining != 3 || tt.SoldOut {
		t.Fatalf("unexpected ticket type: %+v", tt)
	}
}

func TestPublicEventPageDraftNotFound(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A draft (never published) event must not be reachable publicly.
	createDraftEvent(t, env, sessionID, "Draft Event", "draft-event")

	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/draft-event", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for draft, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("expected EVENT_NOT_FOUND, got %+v", body.Error)
	}
}
