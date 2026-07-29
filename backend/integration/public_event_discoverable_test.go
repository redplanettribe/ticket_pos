package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// publicEventDiscoverability is the half of the Storefront event page payload
// these tests care about: the Discoverable flag, plus enough of the rest to
// prove a non-Discoverable Event still serves its full detail.
type publicEventDiscoverability struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Discoverable bool   `json:"discoverable"`
	HasEnded     bool   `json:"has_ended"`
	Organization struct {
		Slug string `json:"slug"`
	} `json:"organization"`
	TicketTypes []struct {
		Name       string `json:"name"`
		PriceCents int    `json:"price_cents"`
		Remaining  int    `json:"remaining"`
	} `json:"ticket_types"`
}

func getPublicEventDiscoverability(t *testing.T, env *testEnv, orgSlug, eventSlug string) publicEventDiscoverability {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event page %s status=%d error=%+v", eventSlug, resp.StatusCode, body.Error)
	}
	var detail publicEventDiscoverability
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return detail
}

// TestPublicEventDetailReportsDiscoverability pins the flag the Storefront needs
// to decide whether a crawler should index the page. ADR 0002 separates being
// reachable from advertising yourself; the payload now says which of the two an
// Event is, so the Storefront can mark a non-Discoverable Event noindex without
// asking a second endpoint.
func TestPublicEventDetailReportsDiscoverability(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Open Event", "open-event", soon, true, 2500, 100)
	publishEvent(t, env, sessionID, "Unlisted Event", "unlisted-event", soon, false, 2500, 100)

	if open := getPublicEventDiscoverability(t, env, "test-org", "open-event"); !open.Discoverable {
		t.Fatalf("expected discoverable=true for a Discoverable Event, got %+v", open)
	}
	if unlisted := getPublicEventDiscoverability(t, env, "test-org", "unlisted-event"); unlisted.Discoverable {
		t.Fatalf("expected discoverable=false for a non-Discoverable Event, got %+v", unlisted)
	}
}

// TestPublicEventDetailServesFullDetailWhenNotDiscoverable is the other half of
// ADR 0002, asserted here so exposing the flag cannot quietly become a gate: a
// published Event that does not advertise itself is still REACHABLE by direct
// link, still 200, and still serves the whole page — Organization, Ticket Types,
// prices and remaining stock — exactly as a Discoverable one does. The only
// difference between the two responses is the flag itself.
func TestPublicEventDetailServesFullDetailWhenNotDiscoverable(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Unlisted Event", "unlisted-event", soon, false, 4200, 3)

	detail := getPublicEventDiscoverability(t, env, "test-org", "unlisted-event")
	if detail.Discoverable {
		t.Fatalf("expected discoverable=false, got %+v", detail)
	}
	if detail.Slug != "unlisted-event" || detail.Name != "Unlisted Event" || detail.HasEnded {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if detail.Organization.Slug != "test-org" {
		t.Fatalf("expected organization test-org, got %q", detail.Organization.Slug)
	}
	if len(detail.TicketTypes) != 1 {
		t.Fatalf("expected 1 ticket type, got %d: %+v", len(detail.TicketTypes), detail.TicketTypes)
	}
	// 4200¢ set, quoted all in at 4683¢ under the default 'pass_on' Fee Handling —
	// the same number the Discoverable case quotes, because nothing about the
	// page is withheld from a direct visitor.
	if tt := detail.TicketTypes[0]; tt.PriceCents != 4683 || tt.Remaining != 3 {
		t.Fatalf("unexpected ticket type: %+v", tt)
	}

	// And the Event is still absent from the surfaces that advertise: reachable
	// is not the same as listed.
	resp, body := env.get(t, "/api/v1/public/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Events []publicEventCard `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Events) != 0 {
		t.Fatalf("expected the global explorer to list nothing, got %+v", page.Events)
	}
}
