package integration

import (
	"net/http"
	"testing"
	"time"
)

// recordPageView is the Storefront's side of ADR 0057: one fire-and-forget call
// per render of an Event page, carrying the Affiliate Link code the visitor
// arrived through when there was one. No credential — anyone landing on the
// page can reach it.
func recordPageView(t *testing.T, env *testEnv, orgSlug, eventSlug, code string) (*http.Response, envelope) {
	t.Helper()
	body := map[string]any{}
	if code != "" {
		body["code"] = code
	}
	return env.post(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug+"/page-views", body, nil)
}

// pageViewBuckets reads one bucket's rows straight off the table: the buckets
// have no read endpoint until the trends payload lands (#413), and what this
// ticket promises is the shape of what is stored — hourly rows holding nothing
// but a count.
func pageViewBuckets(t *testing.T, env *testEnv, eventID string, linkID *string) map[time.Time]int64 {
	t.Helper()
	query := `SELECT hour, view_count FROM event_page_views WHERE event_id = $1 AND affiliate_link_id IS NULL`
	args := []any{eventID}
	if linkID != nil {
		query = `SELECT hour, view_count FROM event_page_views WHERE event_id = $1 AND affiliate_link_id = $2`
		args = append(args, *linkID)
	}
	rows, err := env.db.Query(query, args...)
	if err != nil {
		t.Fatalf("read page view buckets: %v", err)
	}
	defer rows.Close()
	buckets := map[time.Time]int64{}
	for rows.Next() {
		var hour time.Time
		var count int64
		if err := rows.Scan(&hour, &count); err != nil {
			t.Fatalf("scan page view bucket: %v", err)
		}
		buckets[hour.UTC()] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read page view buckets: %v", err)
	}
	return buckets
}

// wantBucket asserts one bucket set is exactly one row at the given UTC hour
// with the given count.
func wantBucket(t *testing.T, buckets map[time.Time]int64, hour time.Time, count int64) {
	t.Helper()
	if len(buckets) != 1 {
		t.Fatalf("buckets=%v, want exactly one row", buckets)
	}
	if got := buckets[hour]; got != count {
		t.Fatalf("bucket at %s = %d, want %d (all: %v)", hour, got, count, buckets)
	}
}

func TestEventPageViewWithoutARefCountsOnlyTheEventBucket(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "View Fest", "view-fest", 1000, 10)
	code := newAffiliateLink(t, env, sessionID, eventID, "María's Instagram")

	resp, body := recordPageView(t, env, "test-org", "view-fest", "")
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("page view status=%d, want 2xx; error=%+v", resp.StatusCode, body.Error)
	}

	hour := env.fixedClock.UTC().Truncate(time.Hour)
	wantBucket(t, pageViewBuckets(t, env, eventID, nil), hour, 1)
	var linkRows int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM event_page_views WHERE event_id = $1 AND affiliate_link_id IS NOT NULL`, eventID).Scan(&linkRows); err != nil {
		t.Fatalf("count link buckets: %v", err)
	}
	if linkRows != 0 {
		t.Fatalf("link bucket rows=%d after a ref-less load, want 0", linkRows)
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, code); clicks != 0 {
		t.Fatalf("clicks=%d after a ref-less load, want 0", clicks)
	}
}

func TestEventPageViewThroughALiveLinkCountsAllThree(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "View Fest", "view-fest", 1000, 10)

	_, created := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "María's Instagram"})
	if created.Error != nil {
		t.Fatalf("create affiliate link error=%+v", created.Error)
	}
	link := decodeAffiliateLink(t, created.Data)

	if resp, body := recordPageView(t, env, "test-org", "view-fest", link.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
	}

	hour := env.fixedClock.UTC().Truncate(time.Hour)
	wantBucket(t, pageViewBuckets(t, env, eventID, nil), hour, 1)
	wantBucket(t, pageViewBuckets(t, env, eventID, &link.ID), hour, 1)
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, link.Code); clicks != 1 {
		t.Fatalf("clicks=%d, want 1 — the counter stays authoritative and moves with the bucket", clicks)
	}
}

func TestEventPageViewThroughADeadCodeCountsThePageViewOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "View Fest", "view-fest", 1000, 10)

	_, created := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Old flyer"})
	if created.Error != nil {
		t.Fatalf("create affiliate link error=%+v", created.Error)
	}
	link := decodeAffiliateLink(t, created.Data)
	if resp, body := updateAffiliateLink(t, env, sessionID, eventID, link.ID, map[string]any{"active": false}); resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A deactivated link's code and a code that never existed are the same
	// thing here: an arrival on the page, counted; a Click, not.
	for _, code := range []string{link.Code, "NOSUCHCODE"} {
		if resp, body := recordPageView(t, env, "test-org", "view-fest", code); resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("page view with code %q status=%d error=%+v", code, resp.StatusCode, body.Error)
		}
	}

	hour := env.fixedClock.UTC().Truncate(time.Hour)
	wantBucket(t, pageViewBuckets(t, env, eventID, nil), hour, 2)
	if buckets := pageViewBuckets(t, env, eventID, &link.ID); len(buckets) != 0 {
		t.Fatalf("deactivated link buckets=%v, want none", buckets)
	}
	if clicks := affiliateLinkClicks(t, env, sessionID, eventID, link.Code); clicks != 0 {
		t.Fatalf("clicks=%d, want 0 — a deactivated Affiliate Link stops counting", clicks)
	}
}

// Slugs that name no Event count nothing: there was no Event page to view. The
// call is still accepted — it is a page load, and a buyer can do nothing about
// where a URL pointed.
func TestEventPageViewOnNoSuchEventIsAcceptedAndCountsNothing(t *testing.T) {
	env := setupTest(t)
	resp, body := recordPageView(t, env, "no-such-org", "no-such-event", "")
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("status=%d, want 2xx; error=%+v", resp.StatusCode, body.Error)
	}
	var rows int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM event_page_views`).Scan(&rows); err != nil {
		t.Fatalf("count buckets: %v", err)
	}
	if rows != 0 {
		t.Fatalf("bucket rows=%d, want 0", rows)
	}
}

func TestEventPageViewsAccumulateHourlyAcrossUTCBoundaries(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "View Fest", "view-fest", 1000, 10)

	// Three loads inside one hour land in ONE row; the clock then crosses the
	// UTC hour boundary and the next load opens a NEW row. The bucket hour is
	// the truncated UTC hour, whatever minute the load arrived at.
	for i := 0; i < 3; i++ {
		if resp, body := recordPageView(t, env, "test-org", "view-fest", ""); resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
	later := env.fixedClock.Add(47 * time.Minute)
	sharedApp.AffiliatesService.WithClock(func() time.Time { return later })
	if resp, body := recordPageView(t, env, "test-org", "view-fest", ""); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
	}

	nextHour := env.fixedClock.Add(75 * time.Minute)
	sharedApp.AffiliatesService.WithClock(func() time.Time { return nextHour })
	if resp, body := recordPageView(t, env, "test-org", "view-fest", ""); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
	}

	buckets := pageViewBuckets(t, env, eventID, nil)
	firstHour := env.fixedClock.UTC().Truncate(time.Hour)
	if len(buckets) != 2 {
		t.Fatalf("buckets=%v, want two rows across the boundary", buckets)
	}
	if buckets[firstHour] != 4 {
		t.Fatalf("first hour=%d, want 4 — same-hour loads accumulate in one row", buckets[firstHour])
	}
	if buckets[firstHour.Add(time.Hour)] != 1 {
		t.Fatalf("next hour=%d, want 1 — the boundary opens a new row", buckets[firstHour.Add(time.Hour)])
	}
}

// Buckets are anonymous by construction: the row carries the four bucket
// columns and nothing else, so there is no column an identity, IP or user agent
// could even be written to (ADR 0057).
func TestEventPageViewBucketsStoreCountsOnly(t *testing.T) {
	env := setupTest(t)
	rows, err := env.db.Query(`
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'event_page_views' ORDER BY column_name
	`)
	if err != nil {
		t.Fatalf("read columns: %v", err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read columns: %v", err)
	}
	want := []string{"affiliate_link_id", "event_id", "hour", "view_count"}
	if len(columns) != len(want) {
		t.Fatalf("columns=%v, want exactly %v", columns, want)
	}
	for i, name := range want {
		if columns[i] != name {
			t.Fatalf("columns=%v, want exactly %v", columns, want)
		}
	}
}

// The delete-only-without-history rule is the click counter's, not the
// buckets': a link that drew no clicks is deletable however many bucket rows
// sit near it, and its own rows (which the real write path could never have
// given it without a click) go with it rather than blocking it.
func TestPageViewBucketsNeverBlockAffiliateLinkDeletion(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "View Fest", "view-fest", 1000, 10)

	_, created := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Mistake"})
	if created.Error != nil {
		t.Fatalf("create affiliate link error=%+v", created.Error)
	}
	link := decodeAffiliateLink(t, created.Data)

	// The Event has traffic, and the link even has a bucket row (planted
	// directly — the real path never writes one without a click). Neither is
	// history.
	if resp, body := recordPageView(t, env, "test-org", "view-fest", ""); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if _, err := env.db.Exec(`
		INSERT INTO event_page_views (event_id, affiliate_link_id, hour, view_count)
		VALUES ($1, $2, $3, 1)
	`, eventID, link.ID, env.fixedClock.UTC().Truncate(time.Hour)); err != nil {
		t.Fatalf("plant link bucket: %v", err)
	}

	resp, body := deleteAffiliateLink(t, env, sessionID, eventID, link.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d, want 200 — buckets are not history; error=%+v", resp.StatusCode, body.Error)
	}

	// And one that HAS a click keeps refusing, exactly as before.
	_, created2 := createAffiliateLink(t, env, sessionID, eventID, map[string]any{"name": "Worked"})
	if created2.Error != nil {
		t.Fatalf("create second link error=%+v", created2.Error)
	}
	clicked := decodeAffiliateLink(t, created2.Data)
	if resp, body := recordPageView(t, env, "test-org", "view-fest", clicked.Code); resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, _ := deleteAffiliateLink(t, env, sessionID, eventID, clicked.ID); resp.StatusCode == http.StatusOK {
		t.Fatalf("a link with a click was deleted — click_count must stay the rule")
	}
}
