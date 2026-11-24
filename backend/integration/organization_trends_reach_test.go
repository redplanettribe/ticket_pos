package integration

import (
	"testing"
	"time"
)

// Organization Trends' Page Views and Events held (#863, spec #858, ADR
// 0081), on the fixed clock of organization_trends_test.go: today is
// 2026-07-07 on the platform clock (America/Guayaquil, UTC-5), 7d is
// 2026-06-30 through 2026-07-06 and its previous span 2026-06-23 through
// 2026-06-29.

// orgCountSeries returns a count figure's one series, keyed all, after checking
// it is the only one.
func orgCountSeries(t *testing.T, name string, figure orgTrendFigure) orgTrendSeries {
	t.Helper()
	if len(figure.Series) != 1 || figure.Series[0].Key != "all" {
		t.Fatalf("%s series = %+v, want one keyed all", name, figure.Series)
	}
	return figure.Series[0]
}

// TestOrganizationTrendsPutsAPageViewOnThePlatformDayOfItsHour: a load at
// 23:30 in Ecuador, already the next day in UTC, lands on the Ecuador day; the
// Organization's Event pages are summed; a load through an Affiliate Link
// counts once; today's loads are drawn apart as the partial today.
func TestOrganizationTrendsPutsAPageViewOnThePlatformDayOfItsHour(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	publishCheckoutEvent(t, env, admin, "View Fest", "view-fest", feeTestBaseCents, 100)
	publishCheckoutEvent(t, env, admin, "Second Fest", "second-fest", feeTestBaseCents, 100)

	// 23:30 on 2026-07-02 in Ecuador, already 2026-07-03 in UTC.
	recordPageViews(t, env, time.Date(2026, 7, 3, 4, 30, 0, 0, time.UTC), testOrgSlug, "view-fest", "", 3)
	// 00:10 on 2026-07-03 in Ecuador.
	recordPageViews(t, env, time.Date(2026, 7, 3, 5, 10, 0, 0, time.UTC), testOrgSlug, "view-fest", "", 2)
	// 2026-07-05, on both pages, one through a code no link answers to.
	recordPageViews(t, env, time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC), testOrgSlug, "view-fest", "", 4)
	recordPageViews(t, env, time.Date(2026, 7, 5, 20, 0, 0, 0, time.UTC), testOrgSlug, "second-fest", "nobody", 1)
	// 2026-06-25, in the previous span.
	recordPageViews(t, env, time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC), testOrgSlug, "second-fest", "", 6)
	// Today, at the fixed clock.
	recordPageViews(t, env, env.fixedClock, testOrgSlug, "view-fest", "", 1)

	got := readOrganizationTrends(t, env, admin, "?span=7d")
	views := got.Figures.PageViews
	series := orgCountSeries(t, "page_views", views)
	// 06-30, 07-01, 07-02, 07-03, 07-04, 07-05, 07-06
	if want := []int{0, 0, 3, 2, 0, 5, 0}; !equalInts(bucketValues(series.Buckets), want) {
		t.Fatalf("page_views buckets = %v, want %v", bucketValues(series.Buckets), want)
	}
	if series.Today == nil || !series.Today.Partial || series.Today.FirstDay != "2026-07-07" || series.Today.Value != 1 {
		t.Fatalf("page_views today = %+v, want 1 on 2026-07-07, partial", series.Today)
	}
	if views.Total != 10 || series.Total != 10 {
		t.Fatalf("page_views total = %d (series %d), want 10: today is in none", views.Total, series.Total)
	}
	if views.ComparisonTotal == nil || *views.ComparisonTotal != 6 {
		t.Fatalf("page_views comparison_total = %v, want 6", intOrNil(views.ComparisonTotal))
	}
	if want := []int{0, 0, 6, 0, 0, 0, 0}; !equalInts(bucketValues(series.ComparisonBuckets), want) {
		t.Fatalf("page_views comparison buckets = %v, want %v", bucketValues(series.ComparisonBuckets), want)
	}

	// A month span sums the days of each month on the platform clock.
	months := readOrganizationTrends(t, env, admin, "?span=12m")
	monthly := orgCountSeries(t, "page_views", months.Figures.PageViews)
	june, july := monthly.Buckets[len(monthly.Buckets)-2], monthly.Buckets[len(monthly.Buckets)-1]
	if june.FirstDay != "2026-06-01" || june.Value != 6 || july.FirstDay != "2026-07-01" || july.Value != 10 {
		t.Fatalf("12m June = %+v, July = %+v; want 6 and 10", june, july)
	}
}

// TestOrganizationTrendsHoldsAPublishedEventOnThePlatformDayItStarts: a
// published Event is held on the platform day its start falls in, an External
// Registration Event included; a draft and a cancelled Event are never held,
// whatever their start.
func TestOrganizationTrendsHoldsAPublishedEventOnThePlatformDayItStarts(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)

	// 23:30 on 2026-07-01 in Ecuador, already 2026-07-02 in UTC.
	late, _ := publishCheckoutEvent(t, env, admin, "Late Fest", "late-fest", feeTestBaseCents, 100)
	setEventStart(t, env, late, time.Date(2026, 7, 2, 4, 30, 0, 0, time.UTC))
	// Registers elsewhere, on 2026-07-04.
	external := publishExternalRegistrationEvent(t, env, admin, "https://lu.ma/org-trends")
	setEventStart(t, env, external, time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC))
	// A draft and a cancelled Event, both on 2026-07-03.
	draft := createDraftEvent(t, env, admin, "Draft Fest", "draft-fest")
	setEventStart(t, env, draft, time.Date(2026, 7, 3, 15, 0, 0, 0, time.UTC))
	cancelled, _ := publishCheckoutEvent(t, env, admin, "Called Off", "called-off", feeTestBaseCents, 100)
	postEventOK(t, env, admin, cancelled, "cancel")
	setEventStart(t, env, cancelled, time.Date(2026, 7, 3, 16, 0, 0, 0, time.UTC))
	// 2026-06-26, in the previous span.
	earlier, _ := publishCheckoutEvent(t, env, admin, "June Fest", "june-fest", feeTestBaseCents, 100)
	setEventStart(t, env, earlier, time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC))
	// Later today.
	today, _ := publishCheckoutEvent(t, env, admin, "Today Fest", "today-fest", feeTestBaseCents, 100)
	setEventStart(t, env, today, time.Date(2026, 7, 7, 23, 0, 0, 0, time.UTC))

	got := readOrganizationTrends(t, env, admin, "?span=7d")
	held := got.Figures.EventsHeld
	series := orgCountSeries(t, "events_held", held)
	// 06-30, 07-01, 07-02, 07-03, 07-04, 07-05, 07-06
	if want := []int{0, 1, 0, 0, 1, 0, 0}; !equalInts(bucketValues(series.Buckets), want) {
		t.Fatalf("events_held buckets = %v, want %v", bucketValues(series.Buckets), want)
	}
	if held.Total != 2 {
		t.Fatalf("events_held total = %d, want 2", held.Total)
	}
	if series.Today == nil || !series.Today.Partial || series.Today.Value != 1 {
		t.Fatalf("events_held today = %+v, want 1, partial", series.Today)
	}
	if held.ComparisonTotal == nil || *held.ComparisonTotal != 1 {
		t.Fatalf("events_held comparison_total = %v, want 1", intOrNil(held.ComparisonTotal))
	}
	// The External Registration Event is held and sold nothing here.
	if got.Figures.TicketsSold.Total != 0 || got.Figures.Takings == nil || got.Figures.Takings.Total != 0 {
		t.Fatalf("tickets_sold=%d takings=%+v, want zero", got.Figures.TicketsSold.Total, got.Figures.Takings)
	}
}

// TestOrganizationTrendsKeepsPageViewsAndEventsHeldToTheirOrganization: every
// Member reads the same Page Views and Events held, another Organization's
// never reach them, and an Organization nobody loaded reads zeros.
func TestOrganizationTrendsKeepsPageViewsAndEventsHeldToTheirOrganization(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	owner := addMemberSession(t, env, admin, "owner@example.com", "event_owner")
	staff := addMemberSession(t, env, admin, "staff@example.com", "event_staff")

	ours, _ := publishCheckoutEvent(t, env, admin, "Our Fest", "our-fest", feeTestBaseCents, 100)
	setEventStart(t, env, ours, time.Date(2026, 7, 3, 15, 0, 0, 0, time.UTC))
	recordPageViews(t, env, time.Date(2026, 7, 3, 15, 0, 0, 0, time.UTC), testOrgSlug, "our-fest", "", 2)

	euro, _ := euroOrganization(t, env)
	euroEvent := eventIDBySlug(t, env, "euro-fest")
	setEventStart(t, env, euroEvent, time.Date(2026, 7, 3, 16, 0, 0, 0, time.UTC))
	recordPageViews(t, env, time.Date(2026, 7, 3, 16, 0, 0, 0, time.UTC), "euro-org", "euro-fest", "", 7)
	quiet := verifyOTP(t, env, "quiet@example.com")
	createOrganization(t, env, quiet, "Quiet Org", "quiet-org")

	for name, session := range map[string]string{"admin": admin, "owner": owner, "staff": staff} {
		got := readOrganizationTrends(t, env, session, "?span=7d")
		if got.Figures.PageViews.Total != 2 || got.Figures.EventsHeld.Total != 1 {
			t.Fatalf("%s reads page_views=%d events_held=%d, want 2 and 1", name, got.Figures.PageViews.Total, got.Figures.EventsHeld.Total)
		}
	}
	theirs := readOrganizationTrends(t, env, euro, "?span=7d")
	if theirs.Figures.PageViews.Total != 7 || theirs.Figures.EventsHeld.Total != 1 {
		t.Fatalf("the other Organization reads page_views=%d events_held=%d, want 7 and 1", theirs.Figures.PageViews.Total, theirs.Figures.EventsHeld.Total)
	}

	none := readOrganizationTrends(t, env, quiet, "?span=7d")
	for name, figure := range map[string]orgTrendFigure{"page_views": none.Figures.PageViews, "events_held": none.Figures.EventsHeld} {
		series := orgCountSeries(t, name, figure)
		if want := []int{0, 0, 0, 0, 0, 0, 0}; !equalInts(bucketValues(series.Buckets), want) || figure.Total != 0 {
			t.Fatalf("%s of an Organization nobody loaded = %+v, want seven zeros", name, series)
		}
		if figure.ComparisonTotal == nil || *figure.ComparisonTotal != 0 || series.Today == nil || series.Today.Value != 0 {
			t.Fatalf("%s comparison=%v today=%+v, want zero, not missing", name, intOrNil(figure.ComparisonTotal), series.Today)
		}
	}
}
