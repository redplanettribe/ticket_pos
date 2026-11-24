package integration

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// The Organization Trends breakdown (#864, spec #858, ADR 0081): one bucket
// of one figure, opened into the Events that made it, ranked, on the same
// day rules as the reading, so the Events add up to the bucket. On the fixed
// clock of organization_trends_test.go, today is 2026-07-07 on the platform
// clock.

const organizationTrendsBreakdownPath = "/api/v1/staff/organization/trends/breakdown"

type orgTrendBreakdownEvent struct {
	EventID string `json:"event_id"`
	Name    string `json:"name"`
	Value   int    `json:"value"`
}

type orgTrendBreakdown struct {
	Timezone    string                   `json:"timezone"`
	Figure      string                   `json:"figure"`
	Granularity string                   `json:"granularity"`
	FirstDay    string                   `json:"first_day"`
	LastDay     string                   `json:"last_day"`
	Partial     bool                     `json:"partial"`
	Total       int                      `json:"total"`
	Events      []orgTrendBreakdownEvent `json:"events"`
}

// readOrganizationTrendsBreakdown reads one breakdown as the session's Active
// Member, query being its query string with its "?".
func readOrganizationTrendsBreakdown(t *testing.T, env *testEnv, sessionID, query string) orgTrendBreakdown {
	t.Helper()
	resp, body := env.get(t, organizationTrendsBreakdownPath+query, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("organization trends breakdown %s status=%d error=%+v", query, resp.StatusCode, body.Error)
	}
	var out orgTrendBreakdown
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode organization trends breakdown: %v", err)
	}
	return out
}

// breakdownRanking is the breakdown's Events as name=value, in its order.
func breakdownRanking(b orgTrendBreakdown) []string {
	out := make([]string, 0, len(b.Events))
	for _, event := range b.Events {
		out = append(out, event.Name+"="+strconv.Itoa(event.Value))
	}
	return out
}

// figureBucket is the stack height of a figure's bucket at index i: its
// series' values there, summed.
func figureBucket(figure orgTrendFigure, i int) int {
	sum := 0
	for _, series := range figure.Series {
		sum += series.Buckets[i].Value
	}
	return sum
}

// figureToday is a figure's today, its series' summed.
func figureToday(figure orgTrendFigure) int {
	sum := 0
	for _, series := range figure.Series {
		if series.Today != nil {
			sum += series.Today.Value
		}
	}
	return sum
}

// assertAddsUp checks that a breakdown's Events sum to its total and that the
// total is the reading's figure for the bucket.
func assertAddsUp(t *testing.T, name string, b orgTrendBreakdown, want int) {
	t.Helper()
	sum := 0
	for _, event := range b.Events {
		sum += event.Value
	}
	if sum != b.Total || b.Total != want {
		t.Fatalf("%s breakdown events sum=%d total=%d, want the reading's %d (%v)", name, sum, b.Total, want, breakdownRanking(b))
	}
}

// seedBreakdownOrganization gives the test Organization three Events that
// sold, were loaded and were held on 2026-07-02, one of them again in June,
// and returns their ids. Another Organization's Event sells and is loaded on
// the same day, and must never reach the breakdown.
func seedBreakdownOrganization(t *testing.T, env *testEnv, admin string) (big, small, imported string) {
	t.Helper()
	day := time.Date(2026, 7, 2, 17, 0, 0, 0, time.UTC)

	big, bigGA := publishCheckoutEvent(t, env, admin, "Big Fest", "big-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "big-fest", bigGA, "ana@example.com", 3, day)
	buyOnlineOn(t, env, testOrgSlug, "big-fest", bigGA, "bob@example.com", 2, day.Add(time.Hour))
	// A sale reversed on the same day is in no figure.
	reversed := day.Add(2 * time.Hour)
	buyOnlineOn(t, env, testOrgSlug, "big-fest", bigGA, "rev@example.com", 4, reversed)
	reverseSaleSoldAt(t, env, big, reversed)
	setEventStart(t, env, big, time.Date(2026, 7, 2, 23, 0, 0, 0, time.UTC))

	small, smallGA := publishCheckoutEvent(t, env, admin, "Small Fest", "small-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "small-fest", smallGA, "cris@example.com", 1, day)
	// 23:30 on 2026-07-02 in Ecuador, already 2026-07-03 in UTC: still the 2nd.
	setEventStart(t, env, small, time.Date(2026, 7, 3, 4, 30, 0, 0, time.UTC))

	imported = createDraftEvent(t, env, admin, "Import Fest", "import-fest")
	importGA := createTicketTypeWithCapacity(t, env, admin, imported, "GA", importedPriceCents, 100)
	commitBatch(t, env, admin, imported, "history", []map[string]any{
		// Made on 2026-07-02, imported today: two Ticket Sales, four tickets.
		importSale("dani@example.com", importGA, 2, "2026-07-02T15:00:00Z"),
		importSale("eli@example.com", importGA, 2, "2026-07-02T16:00:00Z"),
		// June, for the month buckets.
		importSale("fer@example.com", importGA, 6, "2026-06-10T15:00:00Z"),
		// Today, partial.
		importSale("gil@example.com", importGA, 1, "2026-07-07T06:00:00Z"),
	})

	recordPageViews(t, env, day, testOrgSlug, "small-fest", "", 9)
	recordPageViews(t, env, day, testOrgSlug, "big-fest", "", 4)
	recordPageViews(t, env, time.Date(2026, 6, 12, 15, 0, 0, 0, time.UTC), testOrgSlug, "big-fest", "", 5)

	euroOrganization(t, env)
	recordPageViews(t, env, day, "euro-org", "euro-fest", "", 50)
	return big, small, imported
}

// TestOrganizationTrendsBreakdownRanksTheEventsOfADay: a day bucket of every
// figure opens into its Events, ranked largest first, linked by id, adding
// up to that day's figure in the reading; a reversed sale, a draft Event's
// start and another Organization's Events are in none.
func TestOrganizationTrendsBreakdownRanksTheEventsOfADay(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	big, small, imported := seedBreakdownOrganization(t, env, admin)

	reading := readOrganizationTrends(t, env, admin, "?span=7d")
	// 06-30, 07-01, 07-02, ...
	const july2 = 2
	if reading.Span.Buckets[july2].FirstDay != "2026-07-02" {
		t.Fatalf("bucket %d = %+v, want 2026-07-02", july2, reading.Span.Buckets[july2])
	}

	tickets := readOrganizationTrendsBreakdown(t, env, admin, "?figure=tickets_sold&first_day=2026-07-02")
	if tickets.Granularity != "day" || tickets.FirstDay != "2026-07-02" || tickets.LastDay != "2026-07-02" || tickets.Partial || tickets.Timezone != "America/Guayaquil" {
		t.Fatalf("tickets_sold bucket = %+v", tickets)
	}
	if want := []string{"Big Fest=5", "Import Fest=4", "Small Fest=1"}; !equalStrings(breakdownRanking(tickets), want) {
		t.Fatalf("tickets_sold ranking = %v, want %v", breakdownRanking(tickets), want)
	}
	if tickets.Events[0].EventID != big || tickets.Events[1].EventID != imported || tickets.Events[2].EventID != small {
		t.Fatalf("tickets_sold event ids = %+v, want each Event's own", tickets.Events)
	}
	assertAddsUp(t, "tickets_sold", tickets, figureBucket(reading.Figures.TicketsSold, july2))

	sales := readOrganizationTrendsBreakdown(t, env, admin, "?figure=ticket_sales&first_day=2026-07-02")
	if want := []string{"Big Fest=2", "Import Fest=2", "Small Fest=1"}; !equalStrings(breakdownRanking(sales), want) {
		t.Fatalf("ticket_sales ranking = %v, want %v", breakdownRanking(sales), want)
	}
	assertAddsUp(t, "ticket_sales", sales, figureBucket(reading.Figures.TicketSales, july2))

	takings := readOrganizationTrendsBreakdown(t, env, admin, "?figure=takings&first_day=2026-07-02")
	if len(takings.Events) != 3 || takings.Events[0].Value < takings.Events[1].Value || takings.Events[1].Value < takings.Events[2].Value {
		t.Fatalf("takings ranking = %v, want three Events, largest first", breakdownRanking(takings))
	}
	if reading.Figures.Takings == nil {
		t.Fatal("the admin's reading carries no takings")
	}
	assertAddsUp(t, "takings", takings, figureBucket(*reading.Figures.Takings, july2))

	views := readOrganizationTrendsBreakdown(t, env, admin, "?figure=page_views&first_day=2026-07-02")
	if want := []string{"Small Fest=9", "Big Fest=4"}; !equalStrings(breakdownRanking(views), want) {
		t.Fatalf("page_views ranking = %v, want %v: the other Organization's 50 are not ours", breakdownRanking(views), want)
	}
	assertAddsUp(t, "page_views", views, figureBucket(reading.Figures.PageViews, july2))

	held := readOrganizationTrendsBreakdown(t, env, admin, "?figure=events_held&first_day=2026-07-02")
	if want := []string{"Big Fest=1", "Small Fest=1"}; !equalStrings(breakdownRanking(held), want) {
		t.Fatalf("events_held = %v, want %v: the draft Import Fest is never held", breakdownRanking(held), want)
	}
	assertAddsUp(t, "events_held", held, figureBucket(reading.Figures.EventsHeld, july2))

	// Today is a day too, partial, and adds up to the reading's today.
	today := readOrganizationTrendsBreakdown(t, env, admin, "?figure=tickets_sold&first_day=2026-07-07")
	if !today.Partial || today.FirstDay != "2026-07-07" {
		t.Fatalf("today's bucket = %+v, want 2026-07-07, partial", today)
	}
	assertAddsUp(t, "today's tickets_sold", today, figureToday(reading.Figures.TicketsSold))

	// A day nothing happened on is empty, not missing.
	quiet := readOrganizationTrendsBreakdown(t, env, admin, "?figure=tickets_sold&first_day=2026-07-04")
	if quiet.Total != 0 || quiet.Events == nil || len(quiet.Events) != 0 {
		t.Fatalf("a quiet day = %+v, want a zero total and an empty list", quiet)
	}
}

// TestOrganizationTrendsBreakdownOpensAMonth: a whole month and a month the
// span cuts open into their Events, adding up to the reading's month buckets.
func TestOrganizationTrendsBreakdownOpensAMonth(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	seedBreakdownOrganization(t, env, admin)

	reading := readOrganizationTrends(t, env, admin, "?span=12m")
	june, july := len(reading.Span.Buckets)-2, len(reading.Span.Buckets)-1
	if b := reading.Span.Buckets[july]; b.FirstDay != "2026-07-01" || b.LastDay != "2026-07-06" || !b.Partial {
		t.Fatalf("12m last bucket = %+v, want July cut at 2026-07-06", b)
	}

	whole := readOrganizationTrendsBreakdown(t, env, admin, "?figure=tickets_sold&first_day=2026-06-01&last_day=2026-06-30")
	if whole.Granularity != "month" || whole.FirstDay != "2026-06-01" || whole.LastDay != "2026-06-30" || whole.Partial {
		t.Fatalf("June bucket = %+v", whole)
	}
	if want := []string{"Import Fest=6"}; !equalStrings(breakdownRanking(whole), want) {
		t.Fatalf("June tickets_sold = %v, want %v", breakdownRanking(whole), want)
	}
	assertAddsUp(t, "June tickets_sold", whole, figureBucket(reading.Figures.TicketsSold, june))

	views := readOrganizationTrendsBreakdown(t, env, admin, "?figure=page_views&first_day=2026-06-01&last_day=2026-06-30")
	assertAddsUp(t, "June page_views", views, figureBucket(reading.Figures.PageViews, june))

	cut := readOrganizationTrendsBreakdown(t, env, admin, "?figure=tickets_sold&first_day=2026-07-01&last_day=2026-07-06")
	if cut.Granularity != "month" || cut.LastDay != "2026-07-06" {
		t.Fatalf("cut July bucket = %+v", cut)
	}
	// Today's sale is in no month bucket.
	if want := []string{"Big Fest=5", "Import Fest=4", "Small Fest=1"}; !equalStrings(breakdownRanking(cut), want) {
		t.Fatalf("cut July tickets_sold = %v, want %v", breakdownRanking(cut), want)
	}
	assertAddsUp(t, "cut July tickets_sold", cut, figureBucket(reading.Figures.TicketsSold, july))

	for _, figure := range []string{"ticket_sales", "takings", "page_views", "events_held"} {
		b := readOrganizationTrendsBreakdown(t, env, admin, "?figure="+figure+"&first_day=2026-07-01&last_day=2026-07-06")
		var want orgTrendFigure
		switch figure {
		case "ticket_sales":
			want = reading.Figures.TicketSales
		case "takings":
			want = *reading.Figures.Takings
		case "page_views":
			want = reading.Figures.PageViews
		case "events_held":
			want = reading.Figures.EventsHeld
		}
		assertAddsUp(t, "cut July "+figure, b, figureBucket(want, july))
	}
}

// TestOrganizationTrendsBreakdownWithholdsTakingsFromEventStaff: an Event
// Owner opens Takings like an Org Admin; Event Staff are refused it, and are
// handed every count, with no money in any of them.
func TestOrganizationTrendsBreakdownWithholdsTakingsFromEventStaff(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	owner := addMemberSession(t, env, admin, "owner@example.com", "event_owner")
	staff := addMemberSession(t, env, admin, "staff@example.com", "event_staff")
	seedBreakdownOrganization(t, env, admin)

	adminTakings := readOrganizationTrendsBreakdown(t, env, admin, "?figure=takings&first_day=2026-07-02")
	ownerTakings := readOrganizationTrendsBreakdown(t, env, owner, "?figure=takings&first_day=2026-07-02")
	if adminTakings.Total == 0 || ownerTakings.Total != adminTakings.Total {
		t.Fatalf("owner takings = %d, want the admin's %d", ownerTakings.Total, adminTakings.Total)
	}

	for _, query := range []string{
		"?figure=takings&first_day=2026-07-02",
		"?figure=takings&first_day=2026-07-01&last_day=2026-07-06",
		"?figure=takings&first_day=2026-07-07",
	} {
		resp, body := env.get(t, organizationTrendsBreakdownPath+query, authHeader(staff))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("staff takings %s status=%d, want 403", query, resp.StatusCode)
		}
		if string(body.Data) != "" && string(body.Data) != "null" {
			t.Fatalf("staff takings %s sent data %s, want none", query, body.Data)
		}
	}

	for _, figure := range []string{"tickets_sold", "ticket_sales", "page_views", "events_held"} {
		resp, body := env.get(t, organizationTrendsBreakdownPath+"?figure="+figure+"&first_day=2026-07-02", authHeader(staff))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("staff %s status=%d error=%+v", figure, resp.StatusCode, body.Error)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body.Data, &raw); err != nil {
			t.Fatalf("decode staff %s: %v", figure, err)
		}
		for key := range raw {
			if key == "takings" || key == "currency" {
				t.Fatalf("staff %s carries %q: %s", figure, key, body.Data)
			}
		}
		adminSide := readOrganizationTrendsBreakdown(t, env, admin, "?figure="+figure+"&first_day=2026-07-02")
		var got orgTrendBreakdown
		if err := json.Unmarshal(body.Data, &got); err != nil {
			t.Fatalf("decode staff %s: %v", figure, err)
		}
		if !equalStrings(breakdownRanking(got), breakdownRanking(adminSide)) {
			t.Fatalf("staff %s = %v, want the admin's %v", figure, breakdownRanking(got), breakdownRanking(adminSide))
		}
	}
}

// TestOrganizationTrendsBreakdownRefusesABucketNoReadingStates: a figure is
// required and must be known, and a bucket must be a day up to today or the
// days of one month ending before today.
func TestOrganizationTrendsBreakdownRefusesABucketNoReadingStates(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	for _, query := range []string{
		"?first_day=2026-07-02",
		"?figure=money_sold&first_day=2026-07-02",
		"?figure=tickets_sold",
		"?figure=tickets_sold&first_day=2026-7-2",
		"?figure=tickets_sold&first_day=2026-07-08",
		"?figure=tickets_sold&first_day=2026-07-03&last_day=2026-07-02",
		"?figure=tickets_sold&first_day=2026-06-20&last_day=2026-07-02",
		"?figure=tickets_sold&first_day=2026-07-01&last_day=2026-07-07",
	} {
		resp, _ := env.get(t, organizationTrendsBreakdownPath+query, authHeader(admin))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("breakdown %s status=%d, want 400", query, resp.StatusCode)
		}
	}
}
