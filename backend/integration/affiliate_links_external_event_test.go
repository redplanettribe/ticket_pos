package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Affiliate Links on an externally registered Event (issue #213).
//
// A link measures traffic to the Event page, and that happens identically
// whether the Event sells Ticket Types here or hands its audience to a
// Registration Link — so the whole lifecycle stays open and the click counter
// keeps counting. What changes is what is NOT reported: such a link can never
// attribute a Ticket Sale, so its attributed sales count and Net Proceeds are
// suppressed rather than reported as 0 and $0.00, which would read as "this
// link failed" instead of "this link's success is not measured in sales".
//
// Suppressed means absent on the wire: both figures come back JSON null, which
// no client can mistake for a zero.

// externalAffiliateLinkView is the staff Affiliate Link payload read with the
// two attribution figures as pointers, so "suppressed" (null) is
// distinguishable from "measured, and it is nothing yet" (0) — the distinction
// this whole ticket is about.
type externalAffiliateLinkView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Code             string `json:"code"`
	Active           bool   `json:"active"`
	URL              string `json:"url"`
	Clicks           int64  `json:"clicks"`
	SalesCount       *int   `json:"sales_count"`
	NetProceedsCents *int   `json:"net_proceeds_cents"`
}

func decodeExternalAffiliateLink(t *testing.T, raw json.RawMessage) externalAffiliateLinkView {
	t.Helper()
	var link externalAffiliateLinkView
	if err := json.Unmarshal(raw, &link); err != nil {
		t.Fatalf("decode affiliate link: %v", err)
	}
	return link
}

// externalAffiliateLinkRow reads one link off the staff list by id, with its
// figures as pointers.
func externalAffiliateLinkRow(t *testing.T, env *testEnv, sessionID, eventID, linkID string) externalAffiliateLinkView {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list affiliate links status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var links []externalAffiliateLinkView
	if err := json.Unmarshal(body.Data, &links); err != nil {
		t.Fatalf("decode affiliate links: %v", err)
	}
	for _, link := range links {
		if link.ID == linkID {
			return link
		}
	}
	t.Fatalf("no Affiliate Link %q in the Event's list", linkID)
	return externalAffiliateLinkView{}
}

// createExternalAffiliateEvent makes a draft Event that registers its audience elsewhere.
// Publishing it is another ticket's business; nothing here needs it, because an
// Affiliate Link's whole surface — the staff section and the public click route
// — is reachable on a draft exactly as on a published Event.
func createExternalAffiliateEvent(t *testing.T, env *testEnv, sessionID, name, slug string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events", map[string]any{
		"name":              name,
		"slug":              slug,
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/my-meetup",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create external event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return event.ID
}

func assertAttributionSuppressed(t *testing.T, where string, link externalAffiliateLinkView) {
	t.Helper()
	if link.SalesCount != nil {
		t.Fatalf("%s: sales_count=%d on an externally registered Event, want null", where, *link.SalesCount)
	}
	if link.NetProceedsCents != nil {
		t.Fatalf("%s: net_proceeds_cents=%d on an externally registered Event, want null", where, *link.NetProceedsCents)
	}
}

// The organizer promoting an external Event across a radio spot and three
// Instagram accounts gets the same links they would get on a ticketed Event:
// created, renamed, taken out of circulation, put back, and removed while they
// have done nothing.
func TestAffiliateLinkFullLifecycleOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createExternalAffiliateEvent(t, env, sessionID, "Community Meetup", "community-meetup")

	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Radio spot"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link on an external Event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	created := decodeExternalAffiliateLink(t, body.Data)
	if created.Code == "" || created.URL == "" {
		t.Fatalf("created link = %+v, want a code and a copyable URL", created)
	}
	if !created.Active {
		t.Fatalf("a new Affiliate Link on an external Event starts inactive: %+v", created)
	}

	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{
		"name": "Radio spot (morning)",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("rename status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := externalAffiliateLinkRow(t, env, sessionID, eventID, created.ID); got.Name != "Radio spot (morning)" {
		t.Fatalf("name after rename = %q", got.Name)
	}

	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{
		"active": false,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := externalAffiliateLinkRow(t, env, sessionID, eventID, created.ID); got.Active {
		t.Fatalf("link still active after deactivation: %+v", got)
	}

	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{
		"active": true,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("reactivate status=%d error=%+v", resp.StatusCode, body.Error)
	}
	reactivated := externalAffiliateLinkRow(t, env, sessionID, eventID, created.ID)
	if !reactivated.Active {
		t.Fatalf("link not active after reactivation: %+v", reactivated)
	}
	// The code and its URL survive the whole lifecycle, exactly as on a ticketed
	// Event: the URLs a promoter published never stop being the same URLs.
	if reactivated.Code != created.Code || reactivated.URL != created.URL {
		t.Fatalf("code/URL moved across the lifecycle: %+v, want %+v", reactivated, created)
	}

	if resp, body := deleteAffiliateLink(t, env, sessionID, eventID, created.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("delete a link that drove nothing status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list affiliate links status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if links := decodeAffiliateLinks(t, body.Data); len(links) != 0 {
		t.Fatalf("external Event has %d Affiliate Links after the delete, want none", len(links))
	}
}

// Traffic is the whole point of a link on an external Event, and it is measured
// the same way: raw visits, counted again every time.
func TestAffiliateLinkOnAnExternallyRegisteredEventCountsClicks(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createExternalAffiliateEvent(t, env, sessionID, "Community Meetup", "community-meetup")
	code := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")

	for i := 0; i < 2; i++ {
		resp, body := clickAffiliateLink(t, env, "test-org", "community-meetup", code)
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("click on an external Event page status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, code); clicks != 2 {
		t.Fatalf("clicks=%d after two visits to an external Event page, want 2", clicks)
	}
}

// The zeros are the bug. A link on an external Event will never attribute a
// sale, so both attribution figures are absent — null, never 0 — and the click
// count stands alone.
func TestAffiliateLinkOnAnExternallyRegisteredEventSuppressesAttributionFigures(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createExternalAffiliateEvent(t, env, sessionID, "Community Meetup", "community-meetup")

	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Radio spot"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	created := decodeExternalAffiliateLink(t, body.Data)
	assertAttributionSuppressed(t, "create response", created)

	assertAttributionSuppressed(t, "list row", externalAffiliateLinkRow(t, env, sessionID, eventID, created.ID))

	// The update response is the same row read back, so it suppresses the same
	// pair — a rename must not resurrect the figures the list just withheld.
	resp, body = updateAffiliateLink(t, env, sessionID, eventID, created.ID, map[string]any{"name": "Radio spot II"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertAttributionSuppressed(t, "update response", decodeExternalAffiliateLink(t, body.Data))

	// Clicks are unaffected by the suppression: the one figure that means
	// something here is reported in full.
	if resp, body := clickAffiliateLink(t, env, "test-org", "community-meetup", created.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click status=%d error=%+v", resp.StatusCode, body.Error)
	}
	after := externalAffiliateLinkRow(t, env, sessionID, eventID, created.ID)
	assertAttributionSuppressed(t, "list row after a click", after)
	if after.Clicks != 1 {
		t.Fatalf("clicks=%d, want the visit counted alongside the suppressed figures", after.Clicks)
	}
}

// A link on a ticketed Event that has driven nothing reports a real zero, and
// must keep doing so: there the zero is the truth, and suppressing it would
// hide a link that genuinely is not working.
func TestAffiliateLinkOnATicketedEventStillReportsZeroAttributionFigures(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Ticketed Fest", "ticketed-fest")

	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Radio spot"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	created := decodeExternalAffiliateLink(t, body.Data)
	if created.SalesCount == nil || *created.SalesCount != 0 {
		t.Fatalf("sales_count=%v on a ticketed Event, want a reported 0", created.SalesCount)
	}
	if created.NetProceedsCents == nil || *created.NetProceedsCents != 0 {
		t.Fatalf("net_proceeds_cents=%v on a ticketed Event, want a reported 0", created.NetProceedsCents)
	}

	listed := externalAffiliateLinkRow(t, env, sessionID, eventID, created.ID)
	if listed.SalesCount == nil || listed.NetProceedsCents == nil {
		t.Fatalf("list row = %+v on a ticketed Event, want both figures reported", listed)
	}
}

// The mode is the organizer's to change while the Event is a draft, and the
// figures follow it: an Event that turns external stops reporting them, and one
// that comes back to selling Ticket Types reports them again — history intact,
// because suppression is a read-time decision and never touched the data.
func TestAffiliateLinkAttributionFiguresFollowTheEventsRegistrationMode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Switching Fest", "switching-fest")

	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Radio spot"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	linkID := decodeExternalAffiliateLink(t, body.Data).ID

	if resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":              "Switching Fest",
		"slug":              "switching-fest",
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/switching",
	}, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("switch to external status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertAttributionSuppressed(t, "after switching to external",
		externalAffiliateLinkRow(t, env, sessionID, eventID, linkID))

	if resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":              "Switching Fest",
		"slug":              "switching-fest",
		"registration_mode": "tickets",
	}, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("switch back to tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	back := externalAffiliateLinkRow(t, env, sessionID, eventID, linkID)
	if back.SalesCount == nil || back.NetProceedsCents == nil {
		t.Fatalf("row after switching back to tickets = %+v, want both figures reported again", back)
	}
}

// Nothing about a ref may reach the Customer, on an external Event as anywhere
// else: an unknown, mistyped or deactivated code is accepted, counts nothing,
// and is never an error the visitor can trip on.
func TestDeadAffiliateCodeOnAnExternallyRegisteredEventIsAcceptedAndCountsNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createExternalAffiliateEvent(t, env, sessionID, "Community Meetup", "community-meetup")

	resp, body := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Old flyer"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	deactivated := decodeExternalAffiliateLink(t, body.Data)
	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, deactivated.ID, map[string]any{
		"active": false,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate status=%d error=%+v", resp.StatusCode, body.Error)
	}

	for _, code := range []string{deactivated.Code, "NOSUCHCODE"} {
		resp, body := clickAffiliateLink(t, env, "test-org", "community-meetup", code)
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Errorf("dead code %q: status=%d, want 2xx; error=%+v", code, resp.StatusCode, body.Error)
		}
		if body.Error != nil {
			t.Errorf("dead code %q: error=%+v, want none", code, body.Error)
		}
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, deactivated.Code); clicks != 0 {
		t.Fatalf("clicks=%d, want 0 — a dead code counts nothing on an external Event either", clicks)
	}
}
