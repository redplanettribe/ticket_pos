package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Affiliate Link trends (issue #413, ADR 0057): one bounded payload behind the
// affiliate tab's graphs — the Event's timezone and currency, its links, the
// hourly Page View and Click buckets exactly as stored (UTC hours), and the
// Attributed Sales figures derived per hour at read time from active Online
// Sales, bucketed in the Event's own timezone like every other sales surface.
//
// The derivation is the point: nothing about sales is stored for this payload,
// so a Sale Reversal retroactively edits the graph the way it edits every
// other aggregate, and the view buckets remain the only thing ADR 0057 ever
// wrote down.

// affiliateTrendsPayload mirrors GET /staff/events/{id}/affiliate-links/trends.
// SalesBuckets is a pointer so the External Registration case — JSON null,
// "not measured here" — stays distinguishable from "measured, and nothing yet".
type affiliateTrendsPayload struct {
	Timezone     string                        `json:"timezone"`
	Currency     string                        `json:"currency"`
	Links        []affiliateTrendsLink         `json:"links"`
	ViewBuckets  []affiliateTrendsViewBucket   `json:"view_buckets"`
	SalesBuckets *[]affiliateTrendsSalesBucket `json:"sales_buckets"`
}

type affiliateTrendsLink struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type affiliateTrendsViewBucket struct {
	Hour   time.Time `json:"hour"`
	LinkID *string   `json:"link_id"`
	Views  int64     `json:"views"`
}

type affiliateTrendsSalesBucket struct {
	Hour             string `json:"hour"`
	LinkID           string `json:"link_id"`
	Sales            int    `json:"sales"`
	Tickets          int    `json:"tickets"`
	NetProceedsCents int    `json:"net_proceeds_cents"`
}

func getAffiliateTrends(t *testing.T, env *testEnv, sessionID, eventID string) (*http.Response, envelope) {
	t.Helper()
	return env.get(t, "/api/v1/staff/events/"+eventID+"/affiliate-links/trends", authHeader(sessionID))
}

// affiliateTrendsOK reads the payload under an allowed session, asserting the
// house envelope on the way through.
func affiliateTrendsOK(t *testing.T, env *testEnv, sessionID, eventID string) affiliateTrendsPayload {
	t.Helper()
	resp, body := getAffiliateTrends(t, env, sessionID, eventID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("affiliate trends status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("affiliate trends error=%+v, want null on success", body.Error)
	}
	if body.RequestID == "" {
		t.Fatal("affiliate trends carries no request_id")
	}
	var out affiliateTrendsPayload
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode affiliate trends: %v", err)
	}
	return out
}

// viewBucketFor finds the one view bucket at an hour for a link (nil = the
// Event's whole-page bucket), failing when the payload carries none or two.
func viewBucketFor(t *testing.T, trends affiliateTrendsPayload, hour time.Time, linkID *string) affiliateTrendsViewBucket {
	t.Helper()
	var found *affiliateTrendsViewBucket
	for i, bucket := range trends.ViewBuckets {
		sameLink := (bucket.LinkID == nil) == (linkID == nil) &&
			(linkID == nil || (bucket.LinkID != nil && *bucket.LinkID == *linkID))
		if bucket.Hour.Equal(hour) && sameLink {
			if found != nil {
				t.Fatalf("two view buckets for the same (hour, link): %+v", trends.ViewBuckets)
			}
			found = &trends.ViewBuckets[i]
		}
	}
	if found == nil {
		t.Fatalf("no view bucket at %s for link %v (all: %+v)", hour, linkID, trends.ViewBuckets)
	}
	return *found
}

// One GET carries everything the graphs draw: the zone and currency to label
// with, the links to build a legend from, the stored view buckets, and the
// sales figures derived beside them.
func TestAffiliateTrendsReturnsBucketsLinksAndDerivedSalesInOnePayload(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Trend Fest", "trend-fest", 1000, 10)

	_, createdA := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "María's Instagram"})
	if createdA.Error != nil {
		t.Fatalf("create link A error=%+v", createdA.Error)
	}
	linkA := decodeAffiliateLink(t, createdA.Data)
	_, createdB := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Radio spot"})
	if createdB.Error != nil {
		t.Fatalf("create link B error=%+v", createdB.Error)
	}
	linkB := decodeAffiliateLink(t, createdB.Data)
	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, linkB.ID, map[string]any{"active": false}); resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate link B status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// One organic load and two arrivals through link A, all inside the fixed
	// clock's hour.
	for _, code := range []string{"", linkA.Code, linkA.Code} {
		if resp, body := recordPageView(t, env, "test-org", "trend-fest", code); resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}

	// One attributed Online Sale of two tickets through link A.
	begun := beginCheckoutOK(t, env, "test-org", "trend-fest",
		affiliateCheckoutBody("ana@example.com", lastClick(linkA.Code), cartLine(ticketTypeID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	trends := affiliateTrendsOK(t, env, sessionID, eventID)
	if trends.Timezone != "America/Guayaquil" || trends.Currency != "USD" {
		t.Fatalf("timezone/currency = %q/%q, want America/Guayaquil/USD", trends.Timezone, trends.Currency)
	}

	// Every link is present with its active state — the legend's full set, a
	// deactivated one included.
	if len(trends.Links) != 2 {
		t.Fatalf("links=%+v, want both", trends.Links)
	}
	byID := map[string]affiliateTrendsLink{}
	for _, link := range trends.Links {
		byID[link.ID] = link
	}
	if link, ok := byID[linkA.ID]; !ok || !link.Active || link.Name != "María's Instagram" {
		t.Fatalf("link A in payload = %+v, want active with its name", byID[linkA.ID])
	}
	if link, ok := byID[linkB.ID]; !ok || link.Active {
		t.Fatalf("link B in payload = %+v, want present and inactive", byID[linkB.ID])
	}

	// View buckets come back as stored: UTC hours. All three loads sit in the
	// Event's whole-page bucket, the two ref'd ones also in link A's.
	hour := env.fixedClock.UTC().Truncate(time.Hour)
	if got := viewBucketFor(t, trends, hour, nil).Views; got != 3 {
		t.Fatalf("whole-page bucket=%d, want 3", got)
	}
	if got := viewBucketFor(t, trends, hour, &linkA.ID).Views; got != 2 {
		t.Fatalf("link A bucket=%d, want 2", got)
	}

	// The sale arrives derived, in the Event's OWN timezone: sold at 12:00 UTC,
	// which is 07:00 in Guayaquil, with the same Net Proceeds the Event's own
	// summary states — asserted against that figure rather than arithmetic
	// repeated here, so the two surfaces cannot drift.
	if trends.SalesBuckets == nil {
		t.Fatal("sales_buckets is null on a ticketed Event, want figures")
	}
	buckets := *trends.SalesBuckets
	if len(buckets) != 1 {
		t.Fatalf("sales_buckets=%+v, want the one attributed sale's bucket", buckets)
	}
	summary := salesSummaryOK(t, env, sessionID, eventID)
	if summary.NetProceedsCents <= 0 {
		t.Fatalf("the Event's Net Proceeds are %d; the fixture is not exercising the figure", summary.NetProceedsCents)
	}
	want := affiliateTrendsSalesBucket{
		Hour: "2026-07-07T07:00", LinkID: linkA.ID,
		Sales: 1, Tickets: 2, NetProceedsCents: summary.NetProceedsCents,
	}
	if buckets[0] != want {
		t.Fatalf("sales bucket=%+v, want %+v", buckets[0], want)
	}
}

// Nothing about sales is stored for this payload, so a Sale Reversal edits the
// past: the bucket the sale sat in simply stops carrying it.
func TestAffiliateTrendsReversalDropsTheSaleOutOfPastBuckets(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, sessionID, "Trend Fest", "trend-fest", 1000, 10)
	code := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")

	begun := beginCheckoutOK(t, env, "test-org", "trend-fest",
		affiliateCheckoutBody("ana@example.com", lastClick(code), cartLine(ticketTypeID, 1)))
	confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	before := affiliateTrendsOK(t, env, sessionID, eventID)
	if before.SalesBuckets == nil || len(*before.SalesBuckets) != 1 {
		t.Fatalf("sales_buckets before reversal=%+v, want the sale's bucket", before.SalesBuckets)
	}

	reverseSale(t, env, confirmed.ConfirmationRef)

	after := affiliateTrendsOK(t, env, sessionID, eventID)
	if after.SalesBuckets == nil {
		t.Fatal("sales_buckets is null after a reversal, want an empty set — still measured")
	}
	if len(*after.SalesBuckets) != 0 {
		t.Fatalf("sales_buckets after reversal=%+v, want none — the reversal edits the past", *after.SalesBuckets)
	}
}

// An Event that registers its audience elsewhere can never attribute a sale, so
// its sales figures are null — "not measured here" — while its view buckets
// count on exactly as a ticketed Event's do.
func TestAffiliateTrendsOnAnExternallyRegisteredEventNullsSales(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createExternalAffiliateEvent(t, env, sessionID, "Community Meetup", "community-meetup")
	code := newAffiliateLink(t, env, sessionID, eventID, "Radio spot")

	if resp, body := recordPageView(t, env, "test-org", "community-meetup", code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
	}

	trends := affiliateTrendsOK(t, env, sessionID, eventID)
	if trends.SalesBuckets != nil {
		t.Fatalf("sales_buckets=%+v on an externally registered Event, want null", *trends.SalesBuckets)
	}
	hour := env.fixedClock.UTC().Truncate(time.Hour)
	if got := viewBucketFor(t, trends, hour, nil).Views; got != 1 {
		t.Fatalf("whole-page bucket=%d, want the arrival counted", got)
	}
}

// An Event with no history answers with an empty, well-formed payload: empty
// arrays a chart can render an honest empty state from, never nulls it must
// defend against.
func TestAffiliateTrendsOnAQuietEventIsEmptyButWellFormed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Quiet Fest", "quiet-fest", 1000, 10)

	trends := affiliateTrendsOK(t, env, sessionID, eventID)
	if trends.Timezone != "America/Guayaquil" || trends.Currency != "USD" {
		t.Fatalf("timezone/currency = %q/%q even with no history, want them stated", trends.Timezone, trends.Currency)
	}
	if trends.Links == nil || len(trends.Links) != 0 {
		t.Fatalf("links=%+v, want an empty array", trends.Links)
	}
	if trends.ViewBuckets == nil || len(trends.ViewBuckets) != 0 {
		t.Fatalf("view_buckets=%+v, want an empty array", trends.ViewBuckets)
	}
	if trends.SalesBuckets == nil || len(*trends.SalesBuckets) != 0 {
		t.Fatalf("sales_buckets=%v, want an empty array on a quiet ticketed Event", trends.SalesBuckets)
	}
}

// The payload takes the guard the rest of the affiliate-links resource carries:
// Org Admins and Event Owners read it, Event Staff are refused, and another
// Organization's admin is told no such Event exists.
func TestAffiliateTrendsAuthorization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Guarded Fest", "guarded-fest", 1000, 10)

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

	ownerSessionID := addMember("owner@example.com", "event_owner")
	if resp, body := getAffiliateTrends(t, env, ownerSessionID, eventID); resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner trends status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := addMember("doorstaff@example.com", "event_staff")
	resp, body := getAffiliateTrends(t, env, staffSessionID, eventID)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff trends status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("event staff trends error=%+v, want FORBIDDEN", body.Error)
	}

	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	resp, body = getAffiliateTrends(t, env, otherSessionID, eventID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other-org trends status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("other-org trends error=%+v, want EVENT_NOT_FOUND", body.Error)
	}
}
