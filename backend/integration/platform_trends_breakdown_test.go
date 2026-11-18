package integration

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// The Platform Trends breakdown (#818, spec #808): given a figure and a
// bucket, what it was made of. The fixed clock is 2026-07-07T12:00Z, so today
// is 2026-07-07 on the platform clock and the last closed day is 2026-07-06.
// That the route refuses a non-operator is asserted with every operator
// route, in operatorRoutes.

const platformTrendBreakdownPath = platformTrendsPath + "/breakdown"

// platformTrendBreakdown mirrors GET /api/v1/operator/platform-trends/breakdown.
type platformTrendBreakdown struct {
	Timezone    string `json:"timezone"`
	Figure      string `json:"figure"`
	Granularity string `json:"granularity"`
	FirstDay    string `json:"first_day"`
	LastDay     string `json:"last_day"`
	Partial     bool   `json:"partial"`
	// Recorded is false only for an Active Customers bucket with no figure,
	// and SaleHalfOnly true only for an Active Organizations bucket with a
	// day whose markers cannot be read (#819).
	Recorded     bool `json:"recorded"`
	SaleHalfOnly bool `json:"sale_half_only"`
	Totals       []struct {
		Key   string `json:"key"`
		Value int    `json:"value"`
	} `json:"totals"`
	Organizations []breakdownOrganization `json:"organizations"`
	Events        []breakdownEvent        `json:"events"`
	CustomerKinds *breakdownCustomerKinds `json:"customer_kinds"`
}

// breakdownOrganization is one Organization of a breakdown. Cause is empty
// where the response states null, which is for every figure but Active
// Organizations.
type breakdownOrganization struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Key            string `json:"key"`
	Value          int    `json:"value"`
	Cause          string `json:"cause"`
}

type breakdownCustomerKinds struct {
	New             int `json:"new"`
	Returning       int `json:"returning"`
	ReturningBought int `json:"returning_bought"`
}

type breakdownEvent struct {
	EventID          string `json:"event_id"`
	Name             string `json:"name"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	Key              string `json:"key"`
	Value            int    `json:"value"`
}

func readBreakdown(t *testing.T, env *testEnv, sessionID, query string) platformTrendBreakdown {
	t.Helper()
	var out platformTrendBreakdown
	operatorGetOK(t, env, sessionID, platformTrendBreakdownPath+query, &out)
	if out.Totals == nil || out.Organizations == nil || out.Events == nil {
		t.Fatalf("breakdown %s = %+v; want every list an array, never null", query, out)
	}
	return out
}

// breakdownTotal returns one total of a breakdown, failing when it has none
// under that key.
func breakdownTotal(t *testing.T, breakdown platformTrendBreakdown, key string) int {
	t.Helper()
	for _, total := range breakdown.Totals {
		if total.Key == key {
			return total.Value
		}
	}
	t.Fatalf("%s breakdown has no %q total: %+v", breakdown.Figure, key, breakdown.Totals)
	return 0
}

// assertOrganizations checks the Organizations of a breakdown, in order.
func assertOrganizations(t *testing.T, breakdown platformTrendBreakdown, want ...breakdownOrganization) {
	t.Helper()
	if len(breakdown.Organizations) != len(want) {
		t.Fatalf("%s organizations = %+v, want %+v", breakdown.Figure, breakdown.Organizations, want)
	}
	for i, organization := range breakdown.Organizations {
		if organization != want[i] {
			t.Fatalf("%s organizations[%d] = %+v, want %+v", breakdown.Figure, i, organization, want[i])
		}
	}
}

// assertEvents checks the Events of a breakdown, in order.
func assertEvents(t *testing.T, breakdown platformTrendBreakdown, want ...breakdownEvent) {
	t.Helper()
	if len(breakdown.Events) != len(want) {
		t.Fatalf("%s events = %+v, want %+v", breakdown.Figure, breakdown.Events, want)
	}
	for i, event := range breakdown.Events {
		if event != want[i] {
			t.Fatalf("%s events[%d] = %+v, want %+v", breakdown.Figure, i, event, want[i])
		}
	}
}

// assertSumsToTotals: under every key, the Events of a breakdown add up to
// its total, and so do its Organizations when it states any.
func assertSumsToTotals(t *testing.T, breakdown platformTrendBreakdown) {
	t.Helper()
	byEvent, byOrganization := map[string]int{}, map[string]int{}
	for _, event := range breakdown.Events {
		byEvent[event.Key] += event.Value
	}
	for _, organization := range breakdown.Organizations {
		byOrganization[organization.Key] += organization.Value
	}
	for _, total := range breakdown.Totals {
		if byEvent[total.Key] != total.Value {
			t.Fatalf("%s %s: Events add up to %d, total is %d", breakdown.Figure, total.Key, byEvent[total.Key], total.Value)
		}
		if len(breakdown.Organizations) > 0 && byOrganization[total.Key] != total.Value {
			t.Fatalf("%s %s: Organizations add up to %d, total is %d", breakdown.Figure, total.Key, byOrganization[total.Key], total.Value)
		}
	}
}

// organizationName reads an Organization's name. The harness seeds the test
// Organization and no staff route returns one by slug, as operatorOrgIDBySlug
// says of its id.
func organizationName(t *testing.T, env *testEnv, slug string) string {
	t.Helper()
	var name string
	if err := env.db.QueryRow(`SELECT name FROM organizations WHERE slug = $1`, slug).Scan(&name); err != nil {
		t.Fatalf("organization name for %q: %v", slug, err)
	}
	return name
}

// eventIDBySlug reads an Event's id where the helper that made the Event
// does not return it.
func eventIDBySlug(t *testing.T, env *testEnv, slug string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`SELECT id FROM events WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("event id for %q: %v", slug, err)
	}
	return id
}

// TestPlatformTrendBreakdownRanksTicketsSoldAndMoney: a day with sales from
// two Organizations, in two currencies, and three Events. Tickets Sold and
// the two money figures rank the Organizations and the Events, and under
// each key they add up to what the first read states for the day.
func TestPlatformTrendBreakdownRanksTicketsSoldAndMoney(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	day := time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)
	bigID, bigGAID := publishCheckoutEvent(t, env, adminSessionID, "Big Fest", "big-fest", feeTestBaseCents, 100)
	smallID, smallGAID := publishCheckoutEvent(t, env, adminSessionID, "Small Fest", "small-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "big-fest", bigGAID, "ana@example.com", 1, day)
	commitBatch(t, env, adminSessionID, bigID, "imported", []map[string]any{
		importSale("bea@example.com", bigGAID, 5, "2026-07-06T15:00:00Z"),
	})
	buyOnlineOn(t, env, testOrgSlug, "small-fest", smallGAID, "cris@example.com", 3, day)
	// The day before is another bucket.
	buyOnlineOn(t, env, testOrgSlug, "small-fest", smallGAID, "dani@example.com", 4, day.AddDate(0, 0, -1))

	_, euroGAID := euroOrganization(t, env)
	buyOnlineOn(t, env, "euro-org", "euro-fest", euroGAID, "eva@example.com", 2, day)

	testOrgID, testOrgName := operatorOrgIDBySlug(t, env, testOrgSlug), organizationName(t, env, testOrgSlug)
	euroOrgID, euroID := operatorOrgIDBySlug(t, env, "euro-org"), eventIDBySlug(t, env, "euro-fest")
	event := func(id, name, key string, value int) breakdownEvent {
		out := breakdownEvent{EventID: id, Name: name, OrganizationID: testOrgID, OrganizationName: testOrgName, Key: key, Value: value}
		if id == euroID {
			out.OrganizationID, out.OrganizationName = euroOrgID, "Euro Org"
		}
		return out
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")

	tickets := readBreakdown(t, env, operatorSessionID, "?figure=tickets_sold&first_day=2026-07-06")
	if tickets.Figure != "tickets_sold" || tickets.Granularity != "day" || tickets.FirstDay != "2026-07-06" ||
		tickets.LastDay != "2026-07-06" || tickets.Partial || tickets.Timezone != "America/Guayaquil" {
		t.Fatalf("tickets_sold breakdown = %+v; want the closed day 2026-07-06 on the platform clock", tickets)
	}
	assertOrganizations(t, tickets,
		breakdownOrganization{OrganizationID: testOrgID, Name: testOrgName, Key: "all", Value: 9},
		breakdownOrganization{OrganizationID: euroOrgID, Name: "Euro Org", Key: "all", Value: 2},
	)
	assertEvents(t, tickets,
		event(bigID, "Big Fest", "all", 6),
		event(smallID, "Small Fest", "all", 3),
		event(euroID, "Euro Fest", "all", 2),
	)
	assertSumsToTotals(t, tickets)
	if !tickets.Recorded || tickets.SaleHalfOnly || tickets.CustomerKinds != nil {
		t.Fatalf("tickets_sold breakdown = %+v; want recorded, no sale half and no Customer kinds: those are the activity figures'", tickets)
	}
	stated := 0
	for _, channel := range []string{"online", "in_person", "import"} {
		stated += bucketOn(t, seriesFor(t, trends, channel), "2026-07-06").Value
	}
	if got := breakdownTotal(t, tickets, "all"); got != 11 || got != stated {
		t.Fatalf("tickets_sold total = %d, want 11 and the %d the first read states", got, stated)
	}

	bigSold, smallSold, euroSold := feeTestAllInCents+5*feeTestBaseCents, 3*feeTestAllInCents, 2*feeTestAllInCents
	sold := readBreakdown(t, env, operatorSessionID, "?figure=money_sold&first_day=2026-07-06")
	// Ranked within each currency: EUR, then USD with the larger part first.
	assertOrganizations(t, sold,
		breakdownOrganization{OrganizationID: euroOrgID, Name: "Euro Org", Key: "EUR", Value: euroSold},
		breakdownOrganization{OrganizationID: testOrgID, Name: testOrgName, Key: "USD", Value: bigSold + smallSold},
	)
	assertEvents(t, sold,
		event(euroID, "Euro Fest", "EUR", euroSold),
		event(bigID, "Big Fest", "USD", bigSold),
		event(smallID, "Small Fest", "USD", smallSold),
	)
	assertSumsToTotals(t, sold)

	fee := readBreakdown(t, env, operatorSessionID, "?figure=platform_fee_earned&first_day=2026-07-06")
	// The import earned the platform nothing, so Small Fest outranks Big Fest.
	assertOrganizations(t, fee,
		breakdownOrganization{OrganizationID: euroOrgID, Name: "Euro Org", Key: "EUR", Value: 2 * feeTestFeeCents},
		breakdownOrganization{OrganizationID: testOrgID, Name: testOrgName, Key: "USD", Value: 4 * feeTestFeeCents},
	)
	assertEvents(t, fee,
		event(euroID, "Euro Fest", "EUR", 2*feeTestFeeCents),
		event(smallID, "Small Fest", "USD", 3*feeTestFeeCents),
		event(bigID, "Big Fest", "USD", feeTestFeeCents),
	)
	assertSumsToTotals(t, fee)

	for _, currency := range []string{"EUR", "USD"} {
		if got, want := breakdownTotal(t, sold, currency), bucketOn(t, moneySeries(t, trends.Figures.MoneySold, currency), "2026-07-06").Value; got != want || got == 0 {
			t.Fatalf("%s money sold total = %d, want the %d the first read states", currency, got, want)
		}
		if got, want := breakdownTotal(t, fee, currency), bucketOn(t, moneySeries(t, trends.Figures.PlatformFeeEarned, currency), "2026-07-06").Value; got != want || got == 0 {
			t.Fatalf("%s Platform Fee earned total = %d, want the %d the first read states", currency, got, want)
		}
	}
	if len(sold.Totals) != 2 || len(fee.Totals) != 2 {
		t.Fatalf("money totals = %+v / %+v; want one per currency and nothing across them", sold.Totals, fee.Totals)
	}
}

// TestPlatformTrendBreakdownFollowsAReversal: a reversed Ticket Sale is in
// neither the Tickets Sold nor the money sold breakdown, and an Event whose
// only sale was reversed is not listed at all; a fee the platform kept stays
// in the Platform Fee earned one, a fee it returned does not.
func TestPlatformTrendBreakdownFollowsAReversal(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	soldAt := time.Date(2026, 7, 3, 18, 0, 0, 0, time.UTC)
	standingID, standingGAID := publishCheckoutEvent(t, env, adminSessionID, "Standing Fest", "standing-fest", feeTestBaseCents, 100)
	keptID, keptGAID := publishCheckoutEvent(t, env, adminSessionID, "Kept Fest", "kept-fest", feeTestBaseCents, 100)
	undoneID, undoneGAID := publishCheckoutEvent(t, env, adminSessionID, "Undone Fest", "undone-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "standing-fest", standingGAID, "ana@example.com", 1, soldAt)
	returnedRef := buyOnlineOn(t, env, testOrgSlug, "standing-fest", standingGAID, "bea@example.com", 1, soldAt)
	keptRef := buyOnlineOn(t, env, testOrgSlug, "kept-fest", keptGAID, "cris@example.com", 2, soldAt)
	undone := commitBatch(t, env, adminSessionID, undoneID, "undone", []map[string]any{
		importSale("dani@example.com", undoneGAID, 4, "2026-07-03T15:00:00Z"),
	})

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, env, operatorSessionID, keptRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)), PlatformFeeKept: boolPtr(true), Note: strPtr("fee kept"),
	})
	operatorReverseOK(t, env, operatorSessionID, returnedRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)), PlatformFeeKept: boolPtr(false), Note: strPtr("fee returned"),
	})
	undoBatch(t, env, adminSessionID, undoneID, undone)

	testOrgID, testOrgName := operatorOrgIDBySlug(t, env, testOrgSlug), organizationName(t, env, testOrgSlug)
	event := func(id, name, key string, value int) breakdownEvent {
		return breakdownEvent{EventID: id, Name: name, OrganizationID: testOrgID, OrganizationName: testOrgName, Key: key, Value: value}
	}
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")

	tickets := readBreakdown(t, env, operatorSessionID, "?figure=tickets_sold&first_day=2026-07-03")
	assertEvents(t, tickets, event(standingID, "Standing Fest", "all", 1))
	assertOrganizations(t, tickets, breakdownOrganization{OrganizationID: testOrgID, Name: testOrgName, Key: "all", Value: 1})
	stated := 0
	for _, channel := range []string{"online", "in_person", "import"} {
		stated += bucketOn(t, seriesFor(t, trends, channel), "2026-07-03").Value
	}
	if got := breakdownTotal(t, tickets, "all"); got != 1 || got != stated {
		t.Fatalf("tickets_sold total = %d, want 1 and the %d the first read states", got, stated)
	}

	sold := readBreakdown(t, env, operatorSessionID, "?figure=money_sold&first_day=2026-07-03")
	assertEvents(t, sold, event(standingID, "Standing Fest", "USD", feeTestAllInCents))
	if got, want := breakdownTotal(t, sold, "USD"), bucketOn(t, moneySeries(t, trends.Figures.MoneySold, "USD"), "2026-07-03").Value; got != want || got != feeTestAllInCents {
		t.Fatalf("money sold total = %d, first read %d; want %d each", got, want, feeTestAllInCents)
	}

	fee := readBreakdown(t, env, operatorSessionID, "?figure=platform_fee_earned&first_day=2026-07-03")
	assertEvents(t, fee,
		event(keptID, "Kept Fest", "USD", 2*feeTestFeeCents),
		event(standingID, "Standing Fest", "USD", feeTestFeeCents),
	)
	assertOrganizations(t, fee, breakdownOrganization{OrganizationID: testOrgID, Name: testOrgName, Key: "USD", Value: 3 * feeTestFeeCents})
	if got, want := breakdownTotal(t, fee, "USD"), bucketOn(t, moneySeries(t, trends.Figures.PlatformFeeEarned, "USD"), "2026-07-03").Value; got != want || got != 3*feeTestFeeCents {
		t.Fatalf("Platform Fee earned total = %d, first read %d; want %d each", got, want, 3*feeTestFeeCents)
	}
}

// TestPlatformTrendBreakdownRanksPageViewsByEvent: the Events loaded on a
// day, most loaded first, adding up to the day's Page Views; a load on
// another day and an Event nobody loaded are not in it.
func TestPlatformTrendBreakdownRanksPageViewsByEvent(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	quietID, _ := publishCheckoutEvent(t, env, adminSessionID, "Quiet Fest", "quiet-fest", 1000, 10)
	busyID, _ := publishCheckoutEvent(t, env, adminSessionID, "Busy Fest", "busy-fest", 1000, 10)
	publishCheckoutEvent(t, env, adminSessionID, "Unseen Fest", "unseen-fest", 1000, 10)
	recordPageViews(t, env, time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC), testOrgSlug, "quiet-fest", "", 2)
	recordPageViews(t, env, time.Date(2026, 7, 5, 16, 0, 0, 0, time.UTC), testOrgSlug, "busy-fest", "", 3)
	recordPageViews(t, env, time.Date(2026, 7, 6, 2, 0, 0, 0, time.UTC), testOrgSlug, "busy-fest", "", 2)
	// 2026-07-06T05:00Z is midnight on the platform clock: the next day.
	recordPageViews(t, env, time.Date(2026, 7, 6, 5, 0, 0, 0, time.UTC), testOrgSlug, "quiet-fest", "", 7)

	testOrgID, testOrgName := operatorOrgIDBySlug(t, env, testOrgSlug), organizationName(t, env, testOrgSlug)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	views := readBreakdown(t, env, operatorSessionID, "?figure=page_views&first_day=2026-07-05")
	assertEvents(t, views,
		breakdownEvent{EventID: busyID, Name: "Busy Fest", OrganizationID: testOrgID, OrganizationName: testOrgName, Key: "all", Value: 5},
		breakdownEvent{EventID: quietID, Name: "Quiet Fest", OrganizationID: testOrgID, OrganizationName: testOrgName, Key: "all", Value: 2},
	)
	assertOrganizations(t, views)
	assertSumsToTotals(t, views)

	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	stated := bucketOn(t, countSeries(t, "page_views", trends.Figures.PageViews), "2026-07-05").Value
	if got := breakdownTotal(t, views, "all"); got != 7 || got != stated {
		t.Fatalf("page_views total = %d, want 7 and the %d the first read states", got, stated)
	}
}

// TestPlatformTrendBreakdownListsEventsPublishedAndNewOrganizations: the
// Events first published on a day and the Organizations created on it, the
// ones the first read counts there and no other, oldest first, each counting
// one. Today is a bucket too, and says it is partial.
func TestPlatformTrendBreakdownListsEventsPublishedAndNewOrganizations(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	setOrganizationCreatedAt(t, env, testOrgSlug, time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC))
	for _, org := range []struct {
		email, name, slug string
		createdAt         time.Time
	}{
		{"late@example.com", "Late Org", "late-org", time.Date(2026, 7, 5, 20, 0, 0, 0, time.UTC)},
		{"early@example.com", "Early Org", "early-org", time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)},
		{"other@example.com", "Other Day Org", "other-day-org", time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC)},
	} {
		createOrganization(t, env, verifyOTP(t, env, org.email), org.name, org.slug)
		setOrganizationCreatedAt(t, env, org.slug, org.createdAt)
	}
	createOrganization(t, env, verifyOTP(t, env, "today@example.com"), "Today Org", "today-org")

	secondID := publishEventAt(t, env, adminSessionID, "Second Fest", "second-fest", time.Date(2026, 7, 5, 21, 0, 0, 0, time.UTC))
	firstID := publishEventAt(t, env, adminSessionID, "First Fest", "first-fest", time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC))
	cancelledID := publishEventAt(t, env, adminSessionID, "Cancelled Fest", "cancelled-fest", time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC))
	postEventOK(t, env, adminSessionID, cancelledID, "cancel")
	publishEventAt(t, env, adminSessionID, "Monday Fest", "monday-fest", time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC))
	createDraftEvent(t, env, adminSessionID, "Draft Fest", "draft-fest")

	testOrgID, testOrgName := operatorOrgIDBySlug(t, env, testOrgSlug), organizationName(t, env, testOrgSlug)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")

	published := readBreakdown(t, env, operatorSessionID, "?figure=events_published&first_day=2026-07-05")
	event := func(id, name string) breakdownEvent {
		return breakdownEvent{EventID: id, Name: name, OrganizationID: testOrgID, OrganizationName: testOrgName, Key: "all", Value: 1}
	}
	assertEvents(t, published, event(firstID, "First Fest"), event(cancelledID, "Cancelled Fest"), event(secondID, "Second Fest"))
	assertOrganizations(t, published)
	if got, stated := breakdownTotal(t, published, "all"), bucketOn(t, eventsPublishedSeries(t, trends), "2026-07-05").Value; got != 3 || got != stated {
		t.Fatalf("events_published total = %d, want 3 and the %d the first read states", got, stated)
	}

	created := readBreakdown(t, env, operatorSessionID, "?figure=new_organizations&first_day=2026-07-05")
	assertOrganizations(t, created,
		breakdownOrganization{OrganizationID: operatorOrgIDBySlug(t, env, "early-org"), Name: "Early Org", Key: "all", Value: 1},
		breakdownOrganization{OrganizationID: operatorOrgIDBySlug(t, env, "late-org"), Name: "Late Org", Key: "all", Value: 1},
	)
	assertEvents(t, created)
	if got, stated := breakdownTotal(t, created, "all"), bucketOn(t, countSeries(t, "new_organizations", trends.Figures.NewOrganizations), "2026-07-05").Value; got != 2 || got != stated {
		t.Fatalf("new_organizations total = %d, want 2 and the %d the first read states", got, stated)
	}

	today := readBreakdown(t, env, operatorSessionID, "?figure=new_organizations&first_day=2026-07-07&granularity=day")
	if !today.Partial || today.FirstDay != "2026-07-07" || today.LastDay != "2026-07-07" {
		t.Fatalf("today's breakdown = %+v; want 2026-07-07 flagged partial", today)
	}
	assertOrganizations(t, today,
		breakdownOrganization{OrganizationID: operatorOrgIDBySlug(t, env, "today-org"), Name: "Today Org", Key: "all", Value: 1},
	)
	if got, stated := breakdownTotal(t, today, "all"), todayValue(t, countSeries(t, "new_organizations", trends.Figures.NewOrganizations)); got != 1 || got != stated {
		t.Fatalf("today's new_organizations total = %d, want 1 and the %d the first read states for today", got, stated)
	}
}

// TestPlatformTrendBreakdownOfNewVerifiedCustomersIsACountAlone: the number
// of Customers verified on the day, and nothing that says who they are.
func TestPlatformTrendBreakdownOfNewVerifiedCustomersIsACountAlone(t *testing.T) {
	env := setupTest(t)
	emails := []string{"ana@example.com", "bea@example.com", "cris@example.com"}
	for _, email := range emails {
		customerSignIn(t, env, email)
	}
	setCustomerVerifiedAt(t, env, "ana@example.com", time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC))
	setCustomerVerifiedAt(t, env, "bea@example.com", time.Date(2026, 7, 4, 22, 0, 0, 0, time.UTC))
	setCustomerVerifiedAt(t, env, "cris@example.com", time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC))

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	resp, body := env.get(t, platformTrendBreakdownPath+"?figure=new_verified_customers&first_day=2026-07-04", authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	verified := readBreakdown(t, env, operatorSessionID, "?figure=new_verified_customers&first_day=2026-07-04")
	if len(verified.Totals) != 1 || breakdownTotal(t, verified, "all") != 2 {
		t.Fatalf("new_verified_customers totals = %+v, want the one count, 2", verified.Totals)
	}
	assertOrganizations(t, verified)
	assertEvents(t, verified)

	// The figure's own name and the null where Active Customers states its
	// counts by kind aside, the word is in no field or value.
	raw := string(body.Data)
	if strings.Contains(strings.NewReplacer(`"figure":"new_verified_customers"`, "", `"customer_kinds":null`, "").Replace(raw), "customer") {
		t.Fatalf("the breakdown carries a customer field: %s", raw)
	}
	for _, email := range emails {
		row, exists := readHolderCustomer(t, env, email)
		if !exists {
			t.Fatalf("no Customer %s", email)
		}
		for _, identifier := range []string{email, row.id} {
			if strings.Contains(raw, identifier) {
				t.Fatalf("the breakdown identifies a Customer (%s): %s", identifier, raw)
			}
		}
	}
}

// TestPlatformTrendBreakdownOfAWeekCoversTheWholeWeek: a weekly bucket is
// broken down over all seven of its days and no day beside them, and the
// stub a custom range begins with over the days it has.
func TestPlatformTrendBreakdownOfAWeekCoversTheWholeWeek(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	firstID, firstGAID := publishCheckoutEvent(t, env, adminSessionID, "First Fest", "first-fest", feeTestBaseCents, 100)
	lastID, lastGAID := publishCheckoutEvent(t, env, adminSessionID, "Last Fest", "last-fest", feeTestBaseCents, 100)
	at := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 18, 0, 0, 0, time.UTC) }
	// The last week of a 12 month span is 2026-06-30 to 2026-07-06.
	buyOnlineOn(t, env, testOrgSlug, "first-fest", firstGAID, "ana@example.com", 1, at(6, 30))
	buyOnlineOn(t, env, testOrgSlug, "first-fest", firstGAID, "bea@example.com", 2, at(7, 2))
	buyOnlineOn(t, env, testOrgSlug, "last-fest", lastGAID, "cris@example.com", 5, at(7, 6))
	buyOnlineOn(t, env, testOrgSlug, "last-fest", lastGAID, "dani@example.com", 7, at(6, 29))
	buyOnlineOn(t, env, testOrgSlug, "last-fest", lastGAID, "eva@example.com", 9, env.fixedClock)
	// A custom range from 2026-03-01 is 128 days, so its first weekly bucket
	// is a stub of two: 2026-03-01 and 2026-03-02.
	buyOnlineOn(t, env, testOrgSlug, "first-fest", firstGAID, "fede@example.com", 3, at(3, 2))
	buyOnlineOn(t, env, testOrgSlug, "first-fest", firstGAID, "gabi@example.com", 4, at(3, 3))

	testOrgID, testOrgName := operatorOrgIDBySlug(t, env, testOrgSlug), organizationName(t, env, testOrgSlug)
	event := func(id, name string, value int) breakdownEvent {
		return breakdownEvent{EventID: id, Name: name, OrganizationID: testOrgID, OrganizationName: testOrgName, Key: "all", Value: value}
	}
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	week := readBreakdown(t, env, operatorSessionID, "?figure=tickets_sold&first_day=2026-06-30&granularity=week")
	if week.Granularity != "week" || week.FirstDay != "2026-06-30" || week.LastDay != "2026-07-06" || week.Partial {
		t.Fatalf("weekly breakdown = %+v; want 2026-06-30 to 2026-07-06, not partial", week)
	}
	assertEvents(t, week, event(lastID, "Last Fest", 5), event(firstID, "First Fest", 3))
	assertSumsToTotals(t, week)
	yearly := readPlatformTrends(t, env, operatorSessionID, "?span=12m")
	bucket := bucketOn(t, seriesFor(t, yearly, "online"), "2026-07-06")
	if bucket.FirstDay != "2026-06-30" || bucket.Value != 8 || breakdownTotal(t, week, "all") != 8 {
		t.Fatalf("the first read's last week = %+v, breakdown total %d; want 2026-06-30 holding 8 in both", bucket, breakdownTotal(t, week, "all"))
	}

	custom := readPlatformTrends(t, env, operatorSessionID, "?span=custom&from=2026-03-01&to=2026-07-06")
	stub := bucketOn(t, seriesFor(t, custom, "online"), "2026-03-01")
	if stub.LastDay != "2026-03-02" || stub.Value != 3 {
		t.Fatalf("the custom range's first bucket = %+v; want the stub 2026-03-01 to 2026-03-02 holding 3", stub)
	}
	short := readBreakdown(t, env, operatorSessionID, "?figure=tickets_sold&granularity=week&first_day="+stub.FirstDay+"&last_day="+stub.LastDay)
	if short.FirstDay != "2026-03-01" || short.LastDay != "2026-03-02" {
		t.Fatalf("stub breakdown = %+v; want 2026-03-01 to 2026-03-02", short)
	}
	assertEvents(t, short, event(firstID, "First Fest", 3))
}

// TestPlatformTrendBreakdownRefusesAnUnknownFigureOrAMalformedBucket: each
// is a 400 naming the field.
func TestPlatformTrendBreakdownRefusesAnUnknownFigureOrAMalformedBucket(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	cases := []struct {
		query string
		field string
		code  string
	}{
		{"?first_day=2026-07-06", "figure", "REQUIRED"},
		{"?figure=conversion_rate&first_day=2026-07-06", "figure", "INVALID_ENUM"},
		{"?figure=tickets_sold", "first_day", "REQUIRED"},
		{"?figure=tickets_sold&first_day=yesterday", "first_day", "INVALID_DATE"},
		{"?figure=tickets_sold&first_day=2026-7-6", "first_day", "INVALID_DATE"},
		{"?figure=tickets_sold&first_day=2026-07-06&granularity=month", "granularity", "INVALID_ENUM"},
		{"?figure=tickets_sold&first_day=2026-07-08", "first_day", "INVALID_START_AFTER_END"},
		{"?figure=tickets_sold&first_day=2026-07-01&granularity=week", "first_day", "INVALID_START_AFTER_END"},
		{"?figure=tickets_sold&first_day=2026-07-05&granularity=week&last_day=2026-07-07", "last_day", "INVALID_START_AFTER_END"},
		{"?figure=tickets_sold&first_day=2026-06-01&granularity=week&last_day=soon", "last_day", "INVALID_DATE"},
		{"?figure=tickets_sold&first_day=2026-06-01&granularity=week&last_day=2026-05-31", "last_day", "INVALID_END_BEFORE_START"},
		{"?figure=tickets_sold&first_day=2026-06-01&granularity=week&last_day=2026-06-08", "last_day", "TOO_MANY_ITEMS"},
	}
	for _, tc := range cases {
		resp, body := env.get(t, platformTrendBreakdownPath+tc.query, authHeader(operatorSessionID))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%q status=%d, want 400; error=%+v", tc.query, resp.StatusCode, body.Error)
		}
		if got := fieldErrorsByName(t, body)[tc.field]; got.Code != tc.code {
			t.Fatalf("%q %s refusal = %+v, want %s", tc.query, tc.field, got, tc.code)
		}
	}
}

// TestPlatformTrendBreakdownRanksEqualPartsByName: Events with the same
// value keep one order, by name.
func TestPlatformTrendBreakdownRanksEqualPartsByName(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	for _, slug := range []string{"zeta", "alfa", "mid"} {
		publishCheckoutEvent(t, env, adminSessionID, strings.ToUpper(slug[:1])+slug[1:]+" Fest", slug+"-fest", 1000, 10)
		recordPageViews(t, env, time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC), testOrgSlug, slug+"-fest", "", 2)
	}
	views := readBreakdown(t, env, operatorSession(t, env, "operator@example.com"), "?figure=page_views&first_day=2026-07-05")
	names := make([]string, 0, len(views.Events))
	for _, event := range views.Events {
		names = append(names, event.Name)
	}
	if len(names) != 3 || !sort.StringsAreSorted(names) {
		t.Fatalf("events = %v, want the three by name", names)
	}
}
