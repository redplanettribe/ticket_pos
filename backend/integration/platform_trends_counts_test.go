package integration

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Platform Trends' three counts read off data the platform has always kept
// (#813, spec #808): new Verified Customers by the instant of verification,
// new Organizations by the instant of creation, and Page Views by the hourly
// whole-page buckets. Each is one series, keyed "all", with a total of its
// own; none has a recording-began day. The fixed clock is 2026-07-07T12:00Z,
// so today is 2026-07-07 on the platform clock and a 7d span is 2026-06-30 to
// 2026-07-06, set beside 2026-06-23 to 2026-06-29.

// countSeries returns the one series of a count figure, failing when the
// figure is not recorded, has a recording-began day, or is not one series.
func countSeries(t *testing.T, name string, figure platformTrendsFigure) platformTrendsSeries {
	t.Helper()
	if !figure.Recorded || figure.ExactSince != nil {
		t.Fatalf("%s recorded=%v exact_since=%v; want recorded with no recording-began day", name, figure.Recorded, figure.ExactSince)
	}
	if len(figure.Series) != 1 || figure.Series[0].Key != "all" {
		t.Fatalf("%s series = %+v; want the one series \"all\"", name, figure.Series)
	}
	return figure.Series[0]
}

// todayValue is the value of today's partial bucket.
func todayValue(t *testing.T, series platformTrendsSeries) int {
	t.Helper()
	if series.Today == nil || !series.Today.Partial {
		t.Fatalf("today = %+v; want the partial bucket", series.Today)
	}
	return series.Today.Value
}

// assertCompared checks a count figure's total, comparison total and change,
// on the figure and on its one series alike.
func assertCompared(t *testing.T, name string, figure platformTrendsFigure, total, comparison int, change float64) {
	t.Helper()
	series := countSeries(t, name, figure)
	if figure.Total == nil || *figure.Total != total || series.Total != total {
		t.Fatalf("%s total = %v (series %d), want %d", name, figure.Total, series.Total, total)
	}
	if figure.ComparisonTotal == nil || *figure.ComparisonTotal != comparison ||
		series.ComparisonTotal == nil || *series.ComparisonTotal != comparison {
		t.Fatalf("%s comparison_total = %v (series %v), want %d", name, figure.ComparisonTotal, series.ComparisonTotal, comparison)
	}
	if figure.Change == nil || *figure.Change != change || series.Change == nil || *series.Change != change {
		t.Fatalf("%s change = %v (series %v), want %v", name, figure.Change, series.Change, change)
	}
}

// comparisonOn returns the comparison bucket covering a day, nil when that
// bucket is absent.
func comparisonOn(t *testing.T, series platformTrendsSeries, day string) *platformTrendsBucket {
	t.Helper()
	for _, bucket := range series.ComparisonBuckets {
		if bucket != nil && bucket.FirstDay <= day && day <= bucket.LastDay {
			return bucket
		}
	}
	return nil
}

// assertDataSince checks the first day a figure states it has data.
func assertDataSince(t *testing.T, name string, figure platformTrendsFigure, day string) {
	t.Helper()
	if figure.DataSince == nil || *figure.DataSince != day {
		t.Fatalf("%s data_since = %v, want %s", name, figure.DataSince, day)
	}
}

// setCustomerVerifiedAt restages when a Verified Customer was verified. A
// sign-in verifies at the fixed clock, which is today, and no route backdates
// a verification; a span is made of closed days, so the suite moves it in
// SQL, as it moves an Online Sale's sold_at.
func setCustomerVerifiedAt(t *testing.T, env *testEnv, email string, at time.Time) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE customers SET verified_at = $1 WHERE email = $2 AND verified_at IS NOT NULL`, at, email)
	if err != nil {
		t.Fatalf("move verification of %s: %v", email, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving the verification of %s affected %d rows, want 1", email, n)
	}
}

// setOrganizationCreatedAt restages when an Organization was created, in SQL
// for the reason setCustomerVerifiedAt is: creation is stamped at the fixed
// clock, and by the wall clock for the seeded one, and no route backdates it.
func setOrganizationCreatedAt(t *testing.T, env *testEnv, slug string, at time.Time) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE organizations SET created_at = $1 WHERE slug = $2`, at, slug)
	if err != nil {
		t.Fatalf("move creation of %s: %v", slug, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving the creation of %s affected %d rows, want 1", slug, n)
	}
}

// recordPageViews loads an Event page n times at one instant, through an
// Affiliate Link when code is not empty.
func recordPageViews(t *testing.T, env *testEnv, at time.Time, orgSlug, eventSlug, code string, n int) {
	t.Helper()
	sharedApp.AffiliatesService.WithClock(func() time.Time { return at })
	for i := 0; i < n; i++ {
		if resp, body := recordPageView(t, env, orgSlug, eventSlug, code); resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
}

// TestPlatformTrendsCountsACustomerOnTheDayTheyVerified: a passcode, a Google
// Sign-In and an accepted Assignment Link each make one Verified Customer
// today. One of them was minted, unverified, by an import days earlier, and
// that day counts nobody; an imported Customer who never verifies counts on
// no day at all.
func TestPlatformTrendsCountsACustomerOnTheDayTheyVerified(t *testing.T) {
	env := setupTest(t)
	// The fixture imports sales for ana@ and bruno@, minting both Customers
	// unverified. Their records are staged as minted on 2026-07-03, in SQL
	// because an import mints its Customers when it is committed, whatever day
	// its sales say they were sold, and no route backdates a record.
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	if _, err := env.db.Exec(`UPDATE customers SET created_at = $1`, time.Date(2026, 7, 3, 15, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("move Customer creation: %v", err)
	}

	ana := customerSignIn(t, env, "ana@example.com")
	customerGoogleSignIn(t, env, "gala@example.com")
	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

	if row, exists := readHolderCustomer(t, env, "bruno@example.com"); !exists || row.verifiedAt.Valid {
		t.Fatalf("bruno@ exists=%v verified=%v; want an imported Customer never verified", exists, row.verifiedAt.Valid)
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	figure := trends.Figures.NewVerifiedCustomers
	series := countSeries(t, "new_verified_customers", figure)

	if got := todayValue(t, series); got != 3 {
		t.Fatalf("today = %d, want 3: one each by passcode, Google Sign-In and Assignment Link", got)
	}
	for _, bucket := range series.Buckets {
		if bucket.Value != 0 {
			t.Fatalf("%s = %d, want 0: a Customer counts when verified, never when the record was minted", bucket.FirstDay, bucket.Value)
		}
	}
	if figure.Total == nil || *figure.Total != 0 {
		t.Fatalf("total = %v, want 0 with today in no total", figure.Total)
	}
	assertDataSince(t, "new_verified_customers", figure, "2026-07-07")

	// Signing in again verifies nobody anew.
	customerSignIn(t, env, "ana@example.com")
	customerGoogleSignIn(t, env, "gala@example.com")
	again := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	if got := todayValue(t, countSeries(t, "new_verified_customers", again.Figures.NewVerifiedCustomers)); got != 3 {
		t.Fatalf("today after signing in again = %d, want 3 still", got)
	}
}

// TestPlatformTrendsComparesNewVerifiedCustomers: verifications either side
// of platform midnight land on their own days, and the span is set beside the
// one before it from the first verification on.
func TestPlatformTrendsComparesNewVerifiedCustomers(t *testing.T) {
	env := setupTest(t)
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com", "e@example.com"} {
		customerSignIn(t, env, email)
	}
	// Midnight on 2026-07-05 in Guayaquil is 2026-07-05T05:00Z.
	setCustomerVerifiedAt(t, env, "a@example.com", time.Date(2026, 7, 5, 4, 59, 59, 0, time.UTC))
	setCustomerVerifiedAt(t, env, "b@example.com", time.Date(2026, 7, 5, 5, 0, 0, 0, time.UTC))
	setCustomerVerifiedAt(t, env, "c@example.com", time.Date(2026, 7, 6, 20, 0, 0, 0, time.UTC))
	setCustomerVerifiedAt(t, env, "d@example.com", time.Date(2026, 6, 26, 15, 0, 0, 0, time.UTC))
	setCustomerVerifiedAt(t, env, "e@example.com", time.Date(2026, 6, 27, 15, 0, 0, 0, time.UTC))

	trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=7d")
	figure := trends.Figures.NewVerifiedCustomers
	series := countSeries(t, "new_verified_customers", figure)

	for day, want := range map[string]int{"2026-07-04": 1, "2026-07-05": 1, "2026-07-06": 1, "2026-07-03": 0} {
		if got := bucketOn(t, series, day).Value; got != want {
			t.Fatalf("%s = %d, want %d", day, got, want)
		}
	}
	assertCompared(t, "new_verified_customers", figure, 3, 2, 0.5)
	assertDataSince(t, "new_verified_customers", figure, "2026-06-26")
	if bucket := comparisonOn(t, series, "2026-06-25"); bucket != nil {
		t.Fatalf("comparison bucket on 2026-06-25 = %+v, want it absent before the first verification", bucket)
	}
	if bucket := comparisonOn(t, series, "2026-06-27"); bucket == nil || bucket.Value != 1 {
		t.Fatalf("comparison bucket on 2026-06-27 = %+v, want 1", bucket)
	}
}

// TestPlatformTrendsCountsAnOrganizationOnItsCreationDay: Organizations
// created either side of platform midnight land on their own days, a House
// Organization counts like any other, and the span is set beside the one
// before it.
func TestPlatformTrendsCountsAnOrganizationOnItsCreationDay(t *testing.T) {
	env := setupTest(t)
	orgAdminSession(t, env)
	createOrganization(t, env, verifyOTP(t, env, "north@example.com"), "North Org", "north-org")
	createOrganization(t, env, verifyOTP(t, env, "south@example.com"), "South Org", "south-org")
	createOrganization(t, env, verifyOTP(t, env, "today@example.com"), "Today Org", "today-org")

	// The seeded Organization stands in the comparison span; the House
	// Organization and one more stand either side of midnight on 2026-07-05.
	setOrganizationCreatedAt(t, env, "demo-venue", time.Date(2026, 6, 26, 15, 0, 0, 0, time.UTC))
	setOrganizationCreatedAt(t, env, "test-org", time.Date(2026, 7, 5, 4, 59, 59, 0, time.UTC))
	setOrganizationCreatedAt(t, env, "north-org", time.Date(2026, 7, 5, 5, 0, 0, 0, time.UTC))
	setOrganizationCreatedAt(t, env, "south-org", time.Date(2026, 7, 2, 15, 0, 0, 0, time.UTC))

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, operatorOrgIDBySlug(t, env, "test-org"))

	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	figure := trends.Figures.NewOrganizations
	series := countSeries(t, "new_organizations", figure)

	for day, want := range map[string]int{"2026-07-02": 1, "2026-07-04": 1, "2026-07-05": 1, "2026-07-06": 0} {
		if got := bucketOn(t, series, day).Value; got != want {
			t.Fatalf("%s = %d, want %d", day, got, want)
		}
	}
	if got := todayValue(t, series); got != 1 {
		t.Fatalf("today = %d, want the 1 Organization created today", got)
	}
	assertCompared(t, "new_organizations", figure, 3, 1, 2)
	assertDataSince(t, "new_organizations", figure, "2026-06-26")
}

// TestPlatformTrendsSumsPageViewsAcrossEvents: a day's Page Views are the
// whole-page loads of every Event. A load that came through an Affiliate
// Link is one Page View, though it is stored as the link's Click as well.
func TestPlatformTrendsSumsPageViewsAcrossEvents(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, adminSessionID, "View Fest", "view-fest", 1000, 10)
	code := newAffiliateLink(t, env, adminSessionID, eventID, "Radio")

	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	publishCheckoutEvent(t, env, otherSessionID, "Other Fest", "other-fest", 1000, 10)

	yesterday := time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)
	recordPageViews(t, env, yesterday, "test-org", "view-fest", "", 3)
	recordPageViews(t, env, yesterday, "test-org", "view-fest", code, 2)
	recordPageViews(t, env, yesterday.Add(3*time.Hour), "other-org", "other-fest", "", 4)
	// The span before: 2 loads, one of them through the link.
	before := time.Date(2026, 6, 27, 18, 0, 0, 0, time.UTC)
	recordPageViews(t, env, before, "test-org", "view-fest", "", 1)
	recordPageViews(t, env, before, "test-org", "view-fest", code, 1)
	recordPageViews(t, env, env.fixedClock, "other-org", "other-fest", "", 5)

	// What is stored, read in SQL because no read states the whole-page and
	// link rows of two Events together: the day holds more counts than loads,
	// which is what the figure must not add up.
	var stored int
	if err := env.db.QueryRow(`SELECT SUM(view_count) FROM event_page_views WHERE hour >= $1 AND hour < $2`,
		time.Date(2026, 7, 6, 5, 0, 0, 0, time.UTC), time.Date(2026, 7, 7, 5, 0, 0, 0, time.UTC)).Scan(&stored); err != nil {
		t.Fatalf("read stored buckets: %v", err)
	}
	if stored != 11 {
		t.Fatalf("stored counts on 2026-07-06 = %d, want 11: 9 loads and the 2 Clicks stored beside them", stored)
	}

	trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=7d")
	figure := trends.Figures.PageViews
	series := countSeries(t, "page_views", figure)

	if got := bucketOn(t, series, "2026-07-06").Value; got != 9 {
		t.Fatalf("2026-07-06 = %d, want 9: 5 loads of one Event, 2 of them through an Affiliate Link, and 4 of another", got)
	}
	if got := todayValue(t, series); got != 5 {
		t.Fatalf("today = %d, want 5", got)
	}
	assertCompared(t, "page_views", figure, 9, 2, 3.5)
	assertDataSince(t, "page_views", figure, "2026-06-27")
	if bucket := comparisonOn(t, series, "2026-06-26"); bucket != nil {
		t.Fatalf("comparison bucket on 2026-06-26 = %+v, want it absent before the first Page View", bucket)
	}
}

// TestPlatformTrendsPutsAPageViewOnItsPlatformDay: the hourly buckets are
// UTC and platform midnight is 05:00Z, so the 04:00Z hour closes one platform
// day and the 05:00Z hour opens the next, for an Event held in Madrid, where
// both hours are the same morning.
func TestPlatformTrendsPutsAPageViewOnItsPlatformDay(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, adminSessionID, "Madrid Fest", "madrid-fest", 1000, 10)
	setEventTimezone(t, env, eventID, "Europe/Madrid")

	recordPageViews(t, env, time.Date(2026, 7, 5, 4, 59, 59, 0, time.UTC), "test-org", "madrid-fest", "", 1)
	recordPageViews(t, env, time.Date(2026, 7, 5, 5, 0, 0, 0, time.UTC), "test-org", "madrid-fest", "", 4)

	trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=7d")
	series := countSeries(t, "page_views", trends.Figures.PageViews)
	if got := bucketOn(t, series, "2026-07-04").Value; got != 1 {
		t.Fatalf("2026-07-04 = %d, want the 1 load at 23:59:59 in Guayaquil", got)
	}
	if got := bucketOn(t, series, "2026-07-05").Value; got != 4 {
		t.Fatalf("2026-07-05 = %d, want the 4 loads at 00:00 in Guayaquil", got)
	}
}

// TestPlatformTrendsCountsWithNoHistoryHaveNoComparison: with nothing ever
// verified or viewed, the figures are zeros with nothing to compare against,
// never a zero comparison. New Organizations is not here: the harness seeds
// an Organization, so the platform is never without one.
func TestPlatformTrendsCountsWithNoHistoryHaveNoComparison(t *testing.T) {
	env := setupTest(t)
	trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=7d")
	for name, figure := range map[string]platformTrendsFigure{
		"new_verified_customers": trends.Figures.NewVerifiedCustomers,
		"page_views":             trends.Figures.PageViews,
	} {
		series := countSeries(t, name, figure)
		if figure.Total == nil || *figure.Total != 0 || len(series.Buckets) != 7 {
			t.Fatalf("%s total = %v over %d buckets, want 0 over 7", name, figure.Total, len(series.Buckets))
		}
		if figure.DataSince != nil || figure.ComparisonTotal != nil || figure.Change != nil {
			t.Fatalf("%s data_since=%v comparison_total=%v change=%v; want all absent", name, figure.DataSince, figure.ComparisonTotal, figure.Change)
		}
	}
}

// TestPlatformTrendsStatesNoRate: nothing in the response is a rate, a
// conversion or any other figure divided into Page Views.
func TestPlatformTrendsStatesNoRate(t *testing.T) {
	env := setupTest(t)
	var raw json.RawMessage
	operatorGetOK(t, env, operatorSession(t, env, "operator@example.com"), platformTrendsPath+"?span=7d", &raw)

	var figures struct {
		Figures map[string]json.RawMessage `json:"figures"`
	}
	if err := json.Unmarshal(raw, &figures); err != nil {
		t.Fatalf("decode figures: %v", err)
	}
	for name := range figures.Figures {
		if strings.Contains(name, "rate") || strings.Contains(name, "conversion") {
			t.Fatalf("figure %q is a rate; Platform Trends states none", name)
		}
	}
	// And no field anywhere in it is named as one. The words are looked for
	// as JSON keys, so a value that merely contains one is not mistaken.
	for _, word := range []string{"rate", "conversion", "per_view", "per_page_view"} {
		if regexp.MustCompile(`"[a-z_]*` + word + `[a-z_]*":`).MatchString(string(raw)) {
			t.Fatalf("the response has a field named with %q; Platform Trends states no rate built on Page Views", word)
		}
	}
}
