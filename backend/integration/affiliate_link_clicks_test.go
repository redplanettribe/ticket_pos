package integration

import (
	"net/http"
	"strings"
	"testing"
)

// clickAffiliateLink is the buyer's side of an Affiliate Link: the Storefront
// reports, fire-and-forget, that an Event page was reached through a ref. It
// carries no body and no credential — anyone landing on the page can reach it.
func clickAffiliateLink(t *testing.T, env *testEnv, orgSlug, eventSlug, code string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug+"/affiliate-links/"+code+"/click", nil, nil)
}

// affiliateLinkClicks reads one link's clicks off the staff list, which is the
// surface an organizer reads them on.
func affiliateLinkClicks(t *testing.T, env *testEnv, sessionID, eventID, code string) int64 {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list affiliate links status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, link := range decodeAffiliateLinks(t, body.Data) {
		if link.Code == code {
			return link.Clicks
		}
	}
	t.Fatalf("no Affiliate Link with code %q in the list", code)
	return 0
}

func TestAffiliateLinkClickOnALiveCodeCountsEveryVisit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Click Fest", "click-fest", 1000, 10)

	_, created := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "María's Instagram"})
	if created.Error != nil {
		t.Fatalf("create affiliate link error=%+v", created.Error)
	}
	code := decodeAffiliateLink(t, created.Data).Code

	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, code); clicks != 0 {
		t.Fatalf("a new Affiliate Link starts at 0 clicks, got %d", clicks)
	}

	resp, body := clickAffiliateLink(t, env, "test-org", "click-fest", code)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click status=%d, want 2xx; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, code); clicks != 1 {
		t.Fatalf("clicks=%d after one visit, want 1", clicks)
	}

	// No dedup and no visitor identification: the same buyer returning through
	// the same link counts again.
	for i := 0; i < 2; i++ {
		if resp, body := clickAffiliateLink(t, env, "test-org", "click-fest", code); resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("repeat click status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, code); clicks != 3 {
		t.Fatalf("clicks=%d after three visits, want 3", clicks)
	}
}

// Codes are issued in upper case, but a buyer who retyped one off a poster in
// lower case followed a real link — and attribution already reads it that way.
// The counter must agree, or the same visit attributes a sale it never counted.
func TestAffiliateLinkClickOnALowercaseCodeCountsTheVisit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Click Fest", "click-fest", 1000, 10)
	code := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")

	if resp, body := clickAffiliateLink(t, env, "test-org", "click-fest", strings.ToLower(code)); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("lowercase click status=%d, want 2xx; error=%+v", resp.StatusCode, body.Error)
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, code); clicks != 1 {
		t.Fatalf("clicks=%d after a visit through the lowercase code, want 1", clicks)
	}
}

func TestAffiliateLinkClickOnAnUnknownCodeIsAcceptedAndCountsNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Click Fest", "click-fest", 1000, 10)

	_, created := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Radio spot"})
	if created.Error != nil {
		t.Fatalf("create affiliate link error=%+v", created.Error)
	}
	liveCode := decodeAffiliateLink(t, created.Data).Code

	// A mistyped code, a code that never existed, and a live code on the wrong
	// Event page: none of them may become an error a buyer could trip on.
	cases := []struct {
		name      string
		orgSlug   string
		eventSlug string
		code      string
	}{
		{"a code that never existed", "test-org", "click-fest", "NOSUCHCODE"},
		{"a mistyped code", "test-org", "click-fest", "M4RIAS1NSTAGRAM"},
		{"a live code on an Event page it does not belong to", "test-org", "no-such-event", liveCode},
		{"a live code under an Organization it does not belong to", "no-such-org", "click-fest", liveCode},
	}
	for _, tc := range cases {
		resp, body := clickAffiliateLink(t, env, tc.orgSlug, tc.eventSlug, tc.code)
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Errorf("%s: status=%d, want 2xx; error=%+v", tc.name, resp.StatusCode, body.Error)
		}
		if body.Error != nil {
			t.Errorf("%s: error=%+v, want none", tc.name, body.Error)
		}
	}

	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, liveCode); clicks != 0 {
		t.Fatalf("clicks=%d, want 0 — no dead-code visit may count", clicks)
	}
}

func TestAffiliateLinkClickOnADeactivatedLinkCountsNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Click Fest", "click-fest", 1000, 10)

	_, created := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Old flyer"})
	if created.Error != nil {
		t.Fatalf("create affiliate link error=%+v", created.Error)
	}
	link := decodeAffiliateLink(t, created.Data)
	code := link.Code

	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, link.ID, map[string]any{"active": false}); resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body := clickAffiliateLink(t, env, "test-org", "click-fest", code)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click on a deactivated link status=%d, want 2xx; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("click on a deactivated link error=%+v, want none", body.Error)
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, code); clicks != 0 {
		t.Fatalf("clicks=%d, want 0 — a deactivated Affiliate Link stops counting", clicks)
	}
}
