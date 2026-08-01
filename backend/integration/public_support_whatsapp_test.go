package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Where the Organization's Support WhatsApp number is published, and — more
// importantly — where it is not (#201, parent #199, ADR 0027).
//
// The public Organization summary is ONE structure serving three payloads: the
// Event detail, every Event card in the global explorer, and the Organization
// page. The number rides only on the first. The tests below assert both halves
// of that, and the second half is the one that matters: it is the only thing
// standing between this design and a paginated, unauthenticated endpoint from
// which every Organization's support number can be harvested in a handful of
// calls.
//
// If a refactor moves population into the shared publicOrgSummary helper, these
// fail. That is their entire purpose.

// setSupportWhatsApp publishes a Support WhatsApp number on the seeded
// Organization, the way an Org Admin would from Settings.
func setSupportWhatsApp(t *testing.T, env *testEnv, sessionID, number string) {
	t.Helper()

	resp, body := env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":             "Test Org",
		"support_whatsapp": number,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set support whatsapp status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// organizationBlock decodes the `organization` object of a payload as a raw map,
// so a test can tell an ABSENT key from a null one. That distinction is the
// contract: the field is omitempty, so an Organization with no number produces
// no key at all and a client can branch on its presence.
func organizationBlock(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()

	var payload struct {
		Organization map[string]any `json:"organization"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode organization block: %v", err)
	}
	if payload.Organization == nil {
		t.Fatalf("payload carried no organization block: %s", string(raw))
	}
	return payload.Organization
}

// TestPublicEventDetailCarriesSupportWhatsApp is the feature: an Integration
// Partner, and the Storefront, read the number from the Event detail.
func TestPublicEventDetailCarriesSupportWhatsApp(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Support Fest", "support-fest", soon, true, 2500, 100)

	setSupportWhatsApp(t, env, sessionID, ecuadorMobile)

	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/support-fest", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	org := organizationBlock(t, body.Data)
	if got := org["support_whatsapp"]; got != ecuadorMobile {
		t.Fatalf("support_whatsapp on event detail = %v, want %s", got, ecuadorMobile)
	}
}

// TestPublicEventDetailOmitsSupportWhatsAppWhenUnset proves the key is absent
// rather than present-and-null, so a client branches on presence and the
// Storefront renders no link and no placeholder.
func TestPublicEventDetailOmitsSupportWhatsAppWhenUnset(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Quiet Fest", "quiet-fest", soon, true, 2500, 100)

	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/quiet-fest", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	org := organizationBlock(t, body.Data)
	if _, present := org["support_whatsapp"]; present {
		t.Fatalf("support_whatsapp key present on an Organization with no number: %+v", org)
	}
}

// TestPublicListingsNeverCarrySupportWhatsApp is the guard on ADR 0027's
// exposure decision.
//
// Both endpoints below return the same Organization summary structure the Event
// detail does, and both are unauthenticated; the explorer is paginated over
// every Organization on the platform. A number published here would be
// harvestable wholesale, which is a materially different exposure from one
// reachable by fetching each Event detail — and is not the exposure the
// organizer agreed to when the Settings form told them the number appears on
// their Event pages.
//
// The Organization here HAS a number, and the Event detail above proves the
// plumbing works, so a pass here means the exclusion is real and not an
// accident of the fixture.
func TestPublicListingsNeverCarrySupportWhatsApp(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Support Fest", "support-fest", soon, true, 2500, 100)

	setSupportWhatsApp(t, env, sessionID, ecuadorMobile)

	t.Run("global explorer cards", func(t *testing.T) {
		resp, body := env.get(t, "/api/v1/public/events", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("explorer status=%d error=%+v", resp.StatusCode, body.Error)
		}
		var page struct {
			Events []json.RawMessage `json:"events"`
		}
		if err := json.Unmarshal(body.Data, &page); err != nil {
			t.Fatalf("decode explorer page: %v", err)
		}
		if len(page.Events) == 0 {
			t.Fatal("explorer returned no events; the fixture proves nothing")
		}
		for i, card := range page.Events {
			org := organizationBlock(t, card)
			if _, present := org["support_whatsapp"]; present {
				t.Fatalf("explorer card %d published support_whatsapp: %+v", i, org)
			}
		}
	})

	t.Run("organization page", func(t *testing.T) {
		resp, body := env.get(t, "/api/v1/public/organizations/test-org/events", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("organization events status=%d error=%+v", resp.StatusCode, body.Error)
		}

		// The Organization page carries its own summary AND one per card; neither
		// may publish the number.
		org := organizationBlock(t, body.Data)
		if _, present := org["support_whatsapp"]; present {
			t.Fatalf("organization page summary published support_whatsapp: %+v", org)
		}

		var page struct {
			Upcoming []json.RawMessage `json:"upcoming"`
			Past     []json.RawMessage `json:"past"`
		}
		if err := json.Unmarshal(body.Data, &page); err != nil {
			t.Fatalf("decode organization events: %v", err)
		}
		if len(page.Upcoming) == 0 {
			t.Fatal("organization page returned no upcoming events; the fixture proves nothing")
		}
		for i, card := range append(page.Upcoming, page.Past...) {
			cardOrg := organizationBlock(t, card)
			if _, present := cardOrg["support_whatsapp"]; present {
				t.Fatalf("organization page card %d published support_whatsapp: %+v", i, cardOrg)
			}
		}
	})
}
