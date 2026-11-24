package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Organization Trends (#861, spec #858, ADR 0081): Tickets Sold and Ticket
// Sales over a chosen span, read live for the Active Member's Organization on
// the platform clock. The fixed clock is 2026-07-07 12:00 UTC, 07:00 on the
// platform clock (America/Guayaquil, UTC-5): today is 2026-07-07, so 7d is
// 2026-06-30 through 2026-07-06 and its previous span 2026-06-23 through
// 2026-06-29.

const organizationTrendsPath = "/api/v1/staff/organization/trends"

type orgTrendBucket struct {
	FirstDay string `json:"first_day"`
	LastDay  string `json:"last_day"`
	Value    int    `json:"value"`
	Partial  bool   `json:"partial"`
}

type orgTrendSeries struct {
	Key               string           `json:"key"`
	Total             int              `json:"total"`
	Buckets           []orgTrendBucket `json:"buckets"`
	Today             *orgTrendBucket  `json:"today"`
	ComparisonTotal   *int             `json:"comparison_total"`
	Change            *float64         `json:"change"`
	ComparisonBuckets []orgTrendBucket `json:"comparison_buckets"`
}

type orgTrendFigure struct {
	Total           int              `json:"total"`
	ComparisonTotal *int             `json:"comparison_total"`
	Change          *float64         `json:"change"`
	Series          []orgTrendSeries `json:"series"`
}

type orgTrendDays struct {
	FirstDay string `json:"first_day"`
	LastDay  string `json:"last_day"`
	Partial  bool   `json:"partial"`
}

type organizationTrends struct {
	Timezone string `json:"timezone"`
	Today    string `json:"today"`
	Currency string `json:"currency"`
	Span     struct {
		Preset   string         `json:"preset"`
		FirstDay string         `json:"first_day"`
		LastDay  string         `json:"last_day"`
		Buckets  []orgTrendDays `json:"buckets"`
	} `json:"span"`
	Comparison *struct {
		Kind     string `json:"kind"`
		FirstDay string `json:"first_day"`
		LastDay  string `json:"last_day"`
	} `json:"comparison"`
	Granularity string `json:"granularity"`
	Figures     struct {
		TicketsSold orgTrendFigure  `json:"tickets_sold"`
		TicketSales orgTrendFigure  `json:"ticket_sales"`
		Takings     *orgTrendFigure `json:"takings"`
		PageViews   orgTrendFigure  `json:"page_views"`
		EventsHeld  orgTrendFigure  `json:"events_held"`
	} `json:"figures"`
}

// readOrganizationTrends reads Organization Trends as the session's Active
// Member, query being the address's query string with its "?".
func readOrganizationTrends(t *testing.T, env *testEnv, sessionID, query string) organizationTrends {
	t.Helper()
	resp, body := env.get(t, organizationTrendsPath+query, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("organization trends %s status=%d error=%+v", query, resp.StatusCode, body.Error)
	}
	var out organizationTrends
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode organization trends: %v", err)
	}
	return out
}

// channelSeries returns one Sales Channel's series of Tickets Sold.
func channelSeries(t *testing.T, figure orgTrendFigure, channel string) orgTrendSeries {
	t.Helper()
	for _, series := range figure.Series {
		if series.Key == channel {
			return series
		}
	}
	t.Fatalf("figure has no %q series: %+v", channel, figure.Series)
	return orgTrendSeries{}
}

// bucketValues is every bucket's value, oldest first.
func bucketValues(buckets []orgTrendBucket) []int {
	out := make([]int, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, b.Value)
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestOrganizationTrendsPutsASaleOnThePlatformDayItWasMade: a sale imported
// today but made last week lands on its own day; a sale made at 23:30 in
// Ecuador, already the next day in UTC, lands on the Ecuador day; a reversed
// sale lands on no day; and a sale made today is drawn apart as the partial
// today, in no total and no comparison.
func TestOrganizationTrendsPutsASaleOnThePlatformDayItWasMade(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)

	_, onlineGA := publishCheckoutEvent(t, env, admin, "Online Fest", "online-fest", feeTestBaseCents, 100)
	// Sold now, at the fixed clock: today.
	begun := beginCheckoutOK(t, env, testOrgSlug, "online-fest", checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(onlineGA, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	// Sold online on 2026-07-05.
	buyOnlineOn(t, env, testOrgSlug, "online-fest", onlineGA, "fer@example.com", 2, time.Date(2026, 7, 5, 18, 0, 0, 0, time.UTC))

	importEvent := createDraftEvent(t, env, admin, "Import Fest", "import-fest")
	importGA := createTicketTypeWithCapacity(t, env, admin, importEvent, "GA", importedPriceCents, 100)
	commitBatch(t, env, admin, importEvent, "history", []map[string]any{
		// Made on 2026-07-01, imported today.
		importSale("bea@example.com", importGA, 5, "2026-07-01T15:00:00Z"),
		// 23:30 on 2026-07-02 in Ecuador, already 2026-07-03 in UTC.
		importSale("cris@example.com", importGA, 3, "2026-07-03T04:30:00Z"),
		// 00:30 on 2026-07-03 in Ecuador.
		importSale("dani@example.com", importGA, 1, "2026-07-03T05:30:00Z"),
		// 2026-07-04, and reversed.
		importSale("eli@example.com", importGA, 4, "2026-07-04T15:00:00Z"),
		// 2026-06-25, in the previous span.
		importSale("gil@example.com", importGA, 2, "2026-06-25T15:00:00Z"),
		// Earlier today, imported.
		importSale("hal@example.com", importGA, 6, "2026-07-07T06:00:00Z"),
	})
	reverseSaleSoldAt(t, env, importEvent, time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC))

	got := readOrganizationTrends(t, env, admin, "?span=7d")
	if got.Timezone != "America/Guayaquil" || got.Today != "2026-07-07" || got.Currency != "USD" {
		t.Fatalf("timezone=%q today=%q currency=%q", got.Timezone, got.Today, got.Currency)
	}
	if got.Granularity != "day" || got.Span.Preset != "7d" || got.Span.FirstDay != "2026-06-30" || got.Span.LastDay != "2026-07-06" {
		t.Fatalf("span = %+v granularity=%q", got.Span, got.Granularity)
	}
	if len(got.Span.Buckets) != 7 {
		t.Fatalf("span.buckets = %d, want 7", len(got.Span.Buckets))
	}
	for _, b := range got.Span.Buckets {
		if b.Partial || b.FirstDay != b.LastDay {
			t.Fatalf("span bucket %+v, want a whole day", b)
		}
	}
	if got.Comparison == nil || got.Comparison.Kind != "previous" || got.Comparison.FirstDay != "2026-06-23" || got.Comparison.LastDay != "2026-06-29" {
		t.Fatalf("comparison = %+v, want previous 2026-06-23..2026-06-29", got.Comparison)
	}

	sold := got.Figures.TicketsSold
	if len(sold.Series) != 3 {
		t.Fatalf("tickets_sold series = %+v, want one per Sales Channel", sold.Series)
	}
	imported := channelSeries(t, sold, "import")
	// 06-30, 07-01, 07-02, 07-03, 07-04, 07-05, 07-06
	if want := []int{0, 5, 3, 1, 0, 0, 0}; !equalInts(bucketValues(imported.Buckets), want) {
		t.Fatalf("import buckets = %v, want %v", bucketValues(imported.Buckets), want)
	}
	online := channelSeries(t, sold, "online")
	if want := []int{0, 0, 0, 0, 0, 2, 0}; !equalInts(bucketValues(online.Buckets), want) {
		t.Fatalf("online buckets = %v, want %v", bucketValues(online.Buckets), want)
	}
	if inPerson := channelSeries(t, sold, "in_person"); inPerson.Total != 0 || len(inPerson.Buckets) != 7 {
		t.Fatalf("in_person = %+v, want seven zero buckets", inPerson)
	}
	if imported.Today == nil || !imported.Today.Partial || imported.Today.FirstDay != "2026-07-07" || imported.Today.Value != 6 {
		t.Fatalf("import today = %+v, want 6 on 2026-07-07, partial", imported.Today)
	}
	if online.Today == nil || online.Today.Value != 1 || !online.Today.Partial {
		t.Fatalf("online today = %+v, want 1, partial", online.Today)
	}
	if imported.Total != 9 || online.Total != 2 || sold.Total != 11 {
		t.Fatalf("totals import=%d online=%d figure=%d, want 9, 2 and 11: today is in none", imported.Total, online.Total, sold.Total)
	}
	if sold.ComparisonTotal == nil || *sold.ComparisonTotal != 2 {
		t.Fatalf("tickets_sold comparison_total = %v, want 2", intOrNil(sold.ComparisonTotal))
	}
	if sold.Change == nil || *sold.Change != 4.5 {
		t.Fatalf("tickets_sold change = %v, want 4.5 (11 against 2)", sold.Change)
	}
	if want := []int{0, 0, 2, 0, 0, 0, 0}; !equalInts(bucketValues(imported.ComparisonBuckets), want) {
		t.Fatalf("import comparison buckets = %v, want %v", bucketValues(imported.ComparisonBuckets), want)
	}

	sales := got.Figures.TicketSales
	if len(sales.Series) != 1 || sales.Series[0].Key != "all" {
		t.Fatalf("ticket_sales series = %+v, want one keyed all", sales.Series)
	}
	if want := []int{0, 1, 1, 1, 0, 1, 0}; !equalInts(bucketValues(sales.Series[0].Buckets), want) {
		t.Fatalf("ticket_sales buckets = %v, want %v", bucketValues(sales.Series[0].Buckets), want)
	}
	if sales.Total != 4 || sales.ComparisonTotal == nil || *sales.ComparisonTotal != 1 {
		t.Fatalf("ticket_sales total=%d comparison=%v, want 4 and 1", sales.Total, intOrNil(sales.ComparisonTotal))
	}
	if today := sales.Series[0].Today; today == nil || today.Value != 2 || !today.Partial {
		t.Fatalf("ticket_sales today = %+v, want 2 sales, partial", today)
	}
}

// TestOrganizationTrendsReadsAYearEarlier: year sets a day span beside the
// same weekdays 52 weeks earlier.
func TestOrganizationTrendsReadsAYearEarlier(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	importEvent := createDraftEvent(t, env, admin, "Import Fest", "import-fest")
	importGA := createTicketTypeWithCapacity(t, env, admin, importEvent, "GA", importedPriceCents, 100)
	commitBatch(t, env, admin, importEvent, "history", []map[string]any{
		// 2025-07-02 is 364 days before 2026-07-01.
		importSale("ana@example.com", importGA, 4, "2025-07-02T15:00:00Z"),
		importSale("bea@example.com", importGA, 2, "2026-07-01T15:00:00Z"),
	})

	got := readOrganizationTrends(t, env, admin, "?span=7d&compare=year")
	if got.Comparison == nil || got.Comparison.Kind != "year" || got.Comparison.FirstDay != "2025-07-01" || got.Comparison.LastDay != "2025-07-07" {
		t.Fatalf("comparison = %+v, want year 2025-07-01..2025-07-07", got.Comparison)
	}
	sold := got.Figures.TicketsSold
	if sold.Total != 2 || sold.ComparisonTotal == nil || *sold.ComparisonTotal != 4 || sold.Change == nil || *sold.Change != -0.5 {
		t.Fatalf("tickets_sold total=%d comparison=%v change=%v, want 2, 4 and -0.5", sold.Total, intOrNil(sold.ComparisonTotal), sold.Change)
	}
	imported := channelSeries(t, sold, "import")
	if want := []int{0, 4, 0, 0, 0, 0, 0}; !equalInts(bucketValues(imported.ComparisonBuckets), want) {
		t.Fatalf("import comparison buckets = %v, want %v", bucketValues(imported.ComparisonBuckets), want)
	}
}

// TestOrganizationTrendsDrawsLongSpansInCalendarMonths: 12m is the current
// month and the eleven before it, the current one cut at yesterday and
// flagged partial, with today apart; a custom range over 90 days is months
// cut at both ends; previous and year shift a month span back by whole
// months.
func TestOrganizationTrendsDrawsLongSpansInCalendarMonths(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	importEvent := createDraftEvent(t, env, admin, "Import Fest", "import-fest")
	importGA := createTicketTypeWithCapacity(t, env, admin, importEvent, "GA", importedPriceCents, 100)
	commitBatch(t, env, admin, importEvent, "history", []map[string]any{
		importSale("ana@example.com", importGA, 3, "2024-09-10T15:00:00Z"),
		importSale("bea@example.com", importGA, 5, "2025-08-20T15:00:00Z"),
		importSale("cris@example.com", importGA, 2, "2025-09-15T15:00:00Z"),
		importSale("dani@example.com", importGA, 7, "2026-01-10T15:00:00Z"),
		importSale("eli@example.com", importGA, 1, "2026-02-03T15:00:00Z"),
		importSale("fer@example.com", importGA, 4, "2026-07-02T15:00:00Z"),
		importSale("gil@example.com", importGA, 9, "2026-07-07T06:00:00Z"),
	})

	year := readOrganizationTrends(t, env, admin, "?span=12m&compare=year")
	if year.Granularity != "month" || year.Span.FirstDay != "2025-08-01" || year.Span.LastDay != "2026-07-06" {
		t.Fatalf("12m span = %+v granularity=%q", year.Span, year.Granularity)
	}
	if len(year.Span.Buckets) != 12 {
		t.Fatalf("12m buckets = %d, want 12", len(year.Span.Buckets))
	}
	for i, b := range year.Span.Buckets[:11] {
		if b.Partial {
			t.Fatalf("bucket %d %+v is partial, want a whole month", i, b)
		}
	}
	last := year.Span.Buckets[11]
	if last.FirstDay != "2026-07-01" || last.LastDay != "2026-07-06" || !last.Partial {
		t.Fatalf("last bucket = %+v, want 2026-07-01..2026-07-06, partial", last)
	}
	imported := channelSeries(t, year.Figures.TicketsSold, "import")
	// Aug 2025 .. Jul 2026
	if want := []int{5, 2, 0, 0, 0, 7, 1, 0, 0, 0, 0, 4}; !equalInts(bucketValues(imported.Buckets), want) {
		t.Fatalf("12m buckets = %v, want %v", bucketValues(imported.Buckets), want)
	}
	if !imported.Buckets[11].Partial || imported.Buckets[0].Partial {
		t.Fatalf("12m bucket partial flags: first=%v last=%v, want false and true", imported.Buckets[0].Partial, imported.Buckets[11].Partial)
	}
	if imported.Today == nil || imported.Today.Value != 9 || !imported.Today.Partial || imported.Today.FirstDay != "2026-07-07" {
		t.Fatalf("12m today = %+v, want today's 9 apart, partial", imported.Today)
	}
	if year.Figures.TicketsSold.Total != 19 {
		t.Fatalf("12m total = %d, want 19: today is in no total", year.Figures.TicketsSold.Total)
	}
	if year.Comparison == nil || year.Comparison.FirstDay != "2024-08-01" || year.Comparison.LastDay != "2025-07-06" {
		t.Fatalf("12m year comparison = %+v, want 2024-08-01..2025-07-06", year.Comparison)
	}
	if cmp := year.Figures.TicketsSold.ComparisonTotal; cmp == nil || *cmp != 3 {
		t.Fatalf("12m comparison total = %v, want 3", intOrNil(cmp))
	}
	if imported.ComparisonBuckets[1].Value != 3 {
		t.Fatalf("12m comparison buckets = %v, want September 2024's 3 beside September 2025", bucketValues(imported.ComparisonBuckets))
	}

	custom := readOrganizationTrends(t, env, admin, "?span=custom&from=2026-01-15&to=2026-06-10")
	if custom.Granularity != "month" || len(custom.Span.Buckets) != 6 {
		t.Fatalf("custom granularity=%q buckets=%+v, want six months", custom.Granularity, custom.Span.Buckets)
	}
	first, end := custom.Span.Buckets[0], custom.Span.Buckets[5]
	if first.FirstDay != "2026-01-15" || first.LastDay != "2026-01-31" || !first.Partial {
		t.Fatalf("custom first bucket = %+v, want 2026-01-15..2026-01-31, partial", first)
	}
	if end.FirstDay != "2026-06-01" || end.LastDay != "2026-06-10" || !end.Partial {
		t.Fatalf("custom last bucket = %+v, want 2026-06-01..2026-06-10, partial", end)
	}
	if custom.Span.Buckets[1].Partial {
		t.Fatalf("custom February = %+v, want whole", custom.Span.Buckets[1])
	}
	// The sale of 2026-01-10 is before the cut: January holds nothing.
	if want := []int{0, 1, 0, 0, 0, 0}; !equalInts(bucketValues(channelSeries(t, custom.Figures.TicketsSold, "import").Buckets), want) {
		t.Fatalf("custom buckets = %v, want %v", bucketValues(channelSeries(t, custom.Figures.TicketsSold, "import").Buckets), want)
	}
	for _, s := range custom.Figures.TicketsSold.Series {
		if s.Today != nil {
			t.Fatalf("custom range ending before yesterday has today %+v, want none", s.Today)
		}
	}
	// Previous: the six months before, keeping the cuts.
	if custom.Comparison == nil || custom.Comparison.Kind != "previous" || custom.Comparison.FirstDay != "2025-07-15" || custom.Comparison.LastDay != "2025-12-10" {
		t.Fatalf("custom comparison = %+v, want previous 2025-07-15..2025-12-10", custom.Comparison)
	}
	if want := []int{0, 5, 2, 0, 0, 0}; !equalInts(bucketValues(channelSeries(t, custom.Figures.TicketsSold, "import").ComparisonBuckets), want) {
		t.Fatalf("custom comparison buckets = %v, want %v", bucketValues(channelSeries(t, custom.Figures.TicketsSold, "import").ComparisonBuckets), want)
	}
}

// TestOrganizationTrendsAllTimeStartsAtTheEarlierOfCreationAndFirstSale: all
// time starts on the first of the month holding the earlier of the
// Organization's creation, on the platform clock, and its first sale's day,
// and carries no comparison.
func TestOrganizationTrendsAllTimeStartsAtTheEarlierOfCreationAndFirstSale(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	// 22:00 on 2025-03-31 in Ecuador, already April in UTC.
	setOrganizationCreatedAt(t, env, testOrgSlug, time.Date(2025, 4, 1, 3, 0, 0, 0, time.UTC))

	got := readOrganizationTrends(t, env, admin, "?span=all&compare=year")
	if got.Granularity != "month" || got.Span.Preset != "all" || got.Span.FirstDay != "2025-03-01" || got.Span.LastDay != "2026-07-06" {
		t.Fatalf("all span = %+v granularity=%q, want months from 2025-03-01", got.Span, got.Granularity)
	}
	if got.Comparison != nil {
		t.Fatalf("all comparison = %+v, want none", got.Comparison)
	}
	sold := got.Figures.TicketsSold
	if sold.ComparisonTotal != nil || sold.Change != nil {
		t.Fatalf("all comparison_total=%v change=%v, want none", intOrNil(sold.ComparisonTotal), sold.Change)
	}
	// A span with no sales reads as zeros.
	if sold.Total != 0 || len(sold.Series) != 3 || len(sold.Series[0].Buckets) != 17 {
		t.Fatalf("all tickets_sold = %+v, want 17 zero months per channel", sold)
	}
	for _, s := range sold.Series {
		if len(s.ComparisonBuckets) != 0 || s.ComparisonTotal != nil || s.Change != nil {
			t.Fatalf("all series %q carries a comparison: %+v", s.Key, s)
		}
		for _, b := range s.Buckets {
			if b.Value != 0 {
				t.Fatalf("all series %q bucket %+v, want zero", s.Key, b)
			}
		}
	}

	importEvent := createDraftEvent(t, env, admin, "Import Fest", "import-fest")
	importGA := createTicketTypeWithCapacity(t, env, admin, importEvent, "GA", importedPriceCents, 100)
	commitBatch(t, env, admin, importEvent, "history", []map[string]any{
		// 21:00 on 2024-10-31 in Ecuador.
		importSale("ana@example.com", importGA, 3, "2024-11-01T02:00:00Z"),
	})
	got = readOrganizationTrends(t, env, admin, "?span=all")
	if got.Span.FirstDay != "2024-10-01" {
		t.Fatalf("all first_day = %q, want 2024-10-01, the month of the first sale's day", got.Span.FirstDay)
	}
	imported := channelSeries(t, got.Figures.TicketsSold, "import")
	if imported.Buckets[0].Value != 3 || imported.Buckets[0].Partial || got.Figures.TicketsSold.Total != 3 {
		t.Fatalf("all first bucket = %+v total=%d, want October 2024 whole with 3", imported.Buckets[0], got.Figures.TicketsSold.Total)
	}
}

// TestOrganizationTrendsGivesEveryMemberTheCountsOfTheirOwnOrganization:
// Event Staff, an Event Owner and an Org Admin read the same counts, and
// another Organization's sales never reach them.
func TestOrganizationTrendsGivesEveryMemberTheCountsOfTheirOwnOrganization(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	owner := addMemberSession(t, env, admin, "owner@example.com", "event_owner")
	staff := addMemberSession(t, env, admin, "staff@example.com", "event_staff")

	_, ga := publishCheckoutEvent(t, env, admin, "Count Fest", "count-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "count-fest", ga, "ana@example.com", 2, time.Date(2026, 7, 3, 18, 0, 0, 0, time.UTC))

	euro, euroGA := euroOrganization(t, env)
	buyOnlineOn(t, env, "euro-org", "euro-fest", euroGA, "bea@example.com", 5, time.Date(2026, 7, 3, 19, 0, 0, 0, time.UTC))

	for name, session := range map[string]string{"admin": admin, "owner": owner, "staff": staff} {
		got := readOrganizationTrends(t, env, session, "?span=7d")
		if got.Figures.TicketsSold.Total != 2 || got.Figures.TicketSales.Total != 1 || got.Currency != "USD" {
			t.Fatalf("%s reads tickets=%d sales=%d currency=%q, want 2, 1 and USD", name, got.Figures.TicketsSold.Total, got.Figures.TicketSales.Total, got.Currency)
		}
	}
	theirs := readOrganizationTrends(t, env, euro, "?span=7d")
	if theirs.Figures.TicketsSold.Total != 5 || theirs.Currency != "EUR" {
		t.Fatalf("the other Organization reads tickets=%d currency=%q, want 5 and EUR", theirs.Figures.TicketsSold.Total, theirs.Currency)
	}
}

// TestOrganizationTrendsRefusesAMalformedSpan: an unknown span or comparison,
// and a custom range with no closed day, are refused.
func TestOrganizationTrendsRefusesAMalformedSpan(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	for _, query := range []string{
		"?span=52w",
		"?compare=year_earlier",
		"?span=custom&from=2026-07-01",
		"?span=custom&from=2026-07-05&to=2026-07-01",
		"?span=custom&from=2026-07-07&to=2026-07-09",
	} {
		resp, _ := env.get(t, organizationTrendsPath+query, authHeader(admin))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status=%d, want 400", query, resp.StatusCode)
		}
	}
}
