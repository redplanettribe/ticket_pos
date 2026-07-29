package integration

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
)

// affiliateLinkView mirrors the staff Affiliate Link payload. It is deliberately
// a superset-tolerant struct: later tickets add clicks, attributed sales, and
// Net Proceeds to the same rows, and this decode must keep working.
type affiliateLinkView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Active    bool   `json:"active"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

func createAffiliateLink(t *testing.T, env *testEnv, sessionID, eventID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", body, authHeader(sessionID))
}

func decodeAffiliateLink(t *testing.T, raw json.RawMessage) affiliateLinkView {
	t.Helper()
	var link affiliateLinkView
	if err := json.Unmarshal(raw, &link); err != nil {
		t.Fatalf("decode affiliate link: %v", err)
	}
	return link
}

func decodeAffiliateLinks(t *testing.T, raw json.RawMessage) []affiliateLinkView {
	t.Helper()
	var links []affiliateLinkView
	if err := json.Unmarshal(raw, &links); err != nil {
		t.Fatalf("decode affiliate links: %v", err)
	}
	return links
}

// affiliateCodePattern is the unambiguous, URL-safe alphabet the generated code
// is drawn from. The test asserts the shape a buyer has to be able to retype,
// not the generator's internals.
var affiliateCodePattern = regexp.MustCompile(`^[0-9A-HJ-NP-Z]{8,}$`)

func TestAffiliateLinkCreateReturnsGeneratedCodeAndStorefrontURL(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")

	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{
		"name": "María's Instagram",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}

	link := decodeAffiliateLink(t, body.Data)
	if link.ID == "" {
		t.Fatalf("expected an id, got %+v", link)
	}
	if link.Name != "María's Instagram" {
		t.Errorf("name=%q, want %q", link.Name, "María's Instagram")
	}
	if !affiliateCodePattern.MatchString(link.Code) {
		t.Errorf("code=%q does not look like a generated, URL-safe, unambiguous code", link.Code)
	}
	if !link.Active {
		t.Errorf("a new Affiliate Link should be active, got %+v", link)
	}
	if link.CreatedAt == "" {
		t.Errorf("expected created_at, got %+v", link)
	}
	// The copyable Storefront URL: the Event page carrying the code as ref.
	want := "http://storefront.example/test-org/events/rave-a?ref=" + link.Code
	if link.URL != want {
		t.Errorf("url=%q, want %q", link.URL, want)
	}
}

func TestAffiliateLinkCodeIsNotClientSettable(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")

	_, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{
		"name": "Radio spot",
		"code": "CHOSENBYME",
	})
	if body.Error != nil {
		t.Fatalf("create affiliate link error=%+v", body.Error)
	}
	link := decodeAffiliateLink(t, body.Data)
	if link.Code == "CHOSENBYME" {
		t.Fatalf("the code must be system-generated, got the client's %q", link.Code)
	}
}

func TestAffiliateLinkListShowsTheEventsLinks(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")
	otherEventID := createDraftEvent(t, env, sessionID, "Rave B", "rave-b")

	_, first := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "María's Instagram"})
	_, second := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Radio spot"})
	if first.Error != nil || second.Error != nil {
		t.Fatalf("create affiliate links errors=%+v %+v", first.Error, second.Error)
	}
	if _, other := createAffiliateLink(t, env, sessionID, otherEventID, map[string]any{"name": "Other Event flyer"}); other.Error != nil {
		t.Fatalf("create affiliate link on other event error=%+v", other.Error)
	}

	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list affiliate links status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}

	links := decodeAffiliateLinks(t, body.Data)
	if len(links) != 2 {
		t.Fatalf("expected the Event's 2 Affiliate Links, got %+v", links)
	}
	names := map[string]affiliateLinkView{}
	for _, link := range links {
		names[link.Name] = link
	}
	for _, want := range []string{"María's Instagram", "Radio spot"} {
		link, ok := names[want]
		if !ok {
			t.Fatalf("expected %q in the list, got %+v", want, links)
		}
		if link.Code == "" || !link.Active || link.CreatedAt == "" || link.URL == "" {
			t.Errorf("%q row is missing display fields: %+v", want, link)
		}
	}
	if _, leaked := names["Other Event flyer"]; leaked {
		t.Fatalf("another Event's Affiliate Link leaked into the list: %+v", links)
	}
}

func TestAffiliateLinkCodesAreUniqueWithinAnEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")

	seen := map[string]struct{}{}
	for i := 0; i < 12; i++ {
		_, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Promoter"})
		if body.Error != nil {
			t.Fatalf("create affiliate link error=%+v", body.Error)
		}
		link := decodeAffiliateLink(t, body.Data)
		if _, dup := seen[link.Code]; dup {
			t.Fatalf("code %q collided within the Event", link.Code)
		}
		seen[link.Code] = struct{}{}
	}
}

func TestAffiliateLinkNameIsRequired(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")

	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "   "})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank name status=%d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("blank name error=%+v, want VALIDATION_FAILED", body.Error)
	}
	if body.Data != nil && string(body.Data) != "null" {
		t.Fatalf("expected null data on failure, got %s", body.Data)
	}
}

func TestAffiliateLinksAreFullAccessOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave A", "rave-a")

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	// An Event Owner has full access, exactly as an Org Admin does.
	ownerSessionID := addMember("owner@example.com", "event_owner")
	if resp, body := createAffiliateLink(t, env, ownerSessionID, eventID, map[string]any{"name": "Owner's link"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("event owner create status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(ownerSessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner list status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Event Staff are refused both the read and the write.
	staffSessionID := addMember("doorstaff@example.com", "event_staff")
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff list status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("event staff list error=%+v, want FORBIDDEN", body.Error)
	}
	resp, body = createAffiliateLink(t, env, staffSessionID, eventID, map[string]any{"name": "Sneaky"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff create status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("event staff create error=%+v, want FORBIDDEN", body.Error)
	}

	// Another Organization's Org Admin sees no such Event at all.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(otherSessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other-org list status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("other-org list error=%+v, want EVENT_NOT_FOUND", body.Error)
	}

	// Unauthenticated is refused before any of the above is considered.
	if resp, _ := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status=%d, want 401", resp.StatusCode)
	}
}

// The buyer's side of the feature: an Affiliate Link's ref is a tag on a URL and
// nothing else, so the Event page reads identically with a live code, a mistyped
// one, and one that never existed.
func TestEventPageRendersNormallyWithAnyAffiliateRef(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Ref Fest", "ref-fest", 1000, 10)

	_, created := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "María's Instagram"})
	if created.Error != nil {
		t.Fatalf("create affiliate link error=%+v", created.Error)
	}
	liveCode := decodeAffiliateLink(t, created.Data).Code

	path := "/api/v1/public/organizations/test-org/events/ref-fest"
	resp, plain := env.get(t, path, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event page status=%d error=%+v", resp.StatusCode, plain.Error)
	}

	for _, ref := range []string{liveCode, "M4RIAS-1NSTAGRAM", "NOSUCHCODE", ""} {
		resp, body := env.get(t, path+"?ref="+ref, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("event page with ref=%q status=%d error=%+v", ref, resp.StatusCode, body.Error)
		}
		if body.Error != nil {
			t.Fatalf("event page with ref=%q error=%+v", ref, body.Error)
		}
		if string(body.Data) != string(plain.Data) {
			t.Fatalf("event page with ref=%q differs from the untagged page", ref)
		}
	}
}
