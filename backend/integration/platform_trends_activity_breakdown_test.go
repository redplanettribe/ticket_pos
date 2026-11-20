package integration

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	platformtrendssvc "github.com/peter/ticket_pos/backend/internal/platformtrends/service"
)

// The breakdown of the two activity figures (#819, spec #808, ADR 0080).
// Active Organizations is listed with the cause of each; Active Customers is
// counted by kind and never listed.
//
// Time is moved explicitly, as in the tests of the two figures: recordImport
// moves the sales clock, workOn the identity clock, actingOn the clocks a
// Customer acts on, and closeDaysAt moves the Platform Trends clock, where it
// stays, and runs the daily close. The fixed clock is 2026-07-07T12:00Z, and
// setting an Organization up through the staff routes is a Member working on
// that day.

func activeOrganization(t *testing.T, env *testEnv, slug, cause string, days int) breakdownOrganization {
	t.Helper()
	// The Organizations of these tests are created with their slug as name.
	return breakdownOrganization{OrganizationID: operatorOrgIDBySlug(t, env, slug), Name: slug, Key: "all", Value: days, Cause: cause}
}

// assertOrganizationsAddUp: the Organizations of an Active Organizations
// breakdown add up to its one total, which is what the test expects.
func assertOrganizationsAddUp(t *testing.T, breakdown platformTrendBreakdown, want int) {
	t.Helper()
	sum := 0
	for _, organization := range breakdown.Organizations {
		sum += organization.Value
	}
	if len(breakdown.Totals) != 1 || breakdownTotal(t, breakdown, "all") != want || sum != want {
		t.Fatalf("%s %s totals = %+v, Organizations add up to %d; want the one total, %d, in both",
			breakdown.Figure, breakdown.FirstDay, breakdown.Totals, sum, want)
	}
}

// TestActiveOrganizationsBreakdownListsEachOrganizationWithItsCause: on one
// day an Organization is active by a recorded sale, one by a Member working
// and one by both. The day's breakdown lists the three with the id their
// operator page is linked on, and they add up to what the first read states
// for the day. A week counts the days each was active on, and today, which
// no close has frozen, is answered from the same sales and markers.
func TestActiveOrganizationsBreakdownListsEachOrganizationWithItsCause(t *testing.T) {
	env := setupTest(t)
	both := newSellingOrganization(t, env, "both-org")
	selling := newSellingOrganization(t, env, "selling-org")
	working := newOrganizationWithMember(t, env, "working-org")

	both.recordImport(t, env, platformNoon(2026, 7, 5), "2026-07-05T15:00:00Z")
	workOn(t, env, both.sessionID, platformNoon(2026, 7, 5))
	selling.recordImport(t, env, platformNoon(2026, 7, 5), "2026-07-05T15:00:00Z")
	workOn(t, env, working, platformNoon(2026, 7, 5))
	workOn(t, env, both.sessionID, platformNoon(2026, 7, 6))
	workOn(t, env, working, platformNoon(2026, 7, 6))

	closeDaysAt(t, env, env.fixedClock)
	operator := trendsOperator(t, env)

	day := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-05")
	assertOrganizations(t, day,
		activeOrganization(t, env, "both-org", "both", 1),
		activeOrganization(t, env, "selling-org", "sale", 1),
		activeOrganization(t, env, "working-org", "member", 1),
	)
	assertOrganizationsAddUp(t, day, 3)
	assertEvents(t, day)
	if day.SaleHalfOnly || !day.Recorded || day.Partial || day.CustomerKinds != nil {
		t.Fatalf("2026-07-05 breakdown = %+v; want both halves, recorded, closed, and no Customer kinds", day)
	}
	_, figure := activityFiguresAs(t, env, operator, "?span=7d")
	assertActivityOn(t, figure, "2026-07-05", 3)

	week := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-06-30&granularity=week")
	// Ranked by the days each was active on, then by name. The sale of the
	// 5th and the Member working on the 6th are one Organization active by
	// both.
	assertOrganizations(t, week,
		activeOrganization(t, env, "both-org", "both", 2),
		activeOrganization(t, env, "working-org", "member", 2),
		activeOrganization(t, env, "selling-org", "sale", 1),
	)
	assertOrganizationsAddUp(t, week, 5)
	// The week's mean in the first read is those 5 over its 2 frozen days.
	_, yearly := activityFiguresAs(t, env, operator, "?span=12m")
	assertActivityOn(t, yearly, "2026-07-06", 2.5)

	// Setting the two selling Organizations up was their Members working
	// today.
	today := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-07")
	assertOrganizations(t, today,
		activeOrganization(t, env, "both-org", "member", 1),
		activeOrganization(t, env, "selling-org", "member", 1),
	)
	if !today.Partial || today.SaleHalfOnly {
		t.Fatalf("today's breakdown = %+v; want it partial, with both halves", today)
	}

	// A day nobody was active on is a zero and an empty list.
	quiet := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-01")
	assertOrganizations(t, quiet)
	assertOrganizationsAddUp(t, quiet, 0)
}

// TestActiveOrganizationsBreakdownOfADayWithoutMarkersIsTheSaleHalfOnly: a
// day frozen before any Member's work was counted lists the Organizations a
// sale was recorded for and says it is the sale half only, whatever marker
// turned up for it since. A day frozen with markers lists both halves, until
// the daily close deletes its markers thirteen months on: then it lists the
// sale half and says so, and its frozen figure still counts both.
func TestActiveOrganizationsBreakdownOfADayWithoutMarkersIsTheSaleHalfOnly(t *testing.T) {
	env := setupTest(t)
	old := newSellingOrganization(t, env, "old-org")
	preparing := newOrganizationWithMember(t, env, "preparing-org")
	old.recordImport(t, env, platformNoon(2026, 7, 1), "2026-07-01T15:00:00Z")

	// The 1st and the 2nd are frozen with no Member's work counted.
	closeDaysAt(t, env, platformNoon(2026, 7, 3))
	// A marker lands late on the 2nd; the 4th has a sale and two Members.
	workOn(t, env, old.sessionID, platformNoon(2026, 7, 2))
	old.recordImport(t, env, platformNoon(2026, 7, 4), "2026-07-04T15:00:00Z")
	workOn(t, env, old.sessionID, platformNoon(2026, 7, 4))
	workOn(t, env, preparing, platformNoon(2026, 7, 4))
	closeDaysAt(t, env, env.fixedClock)
	operator := trendsOperator(t, env)

	first := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-01")
	assertOrganizations(t, first, activeOrganization(t, env, "old-org", "sale", 1))
	if !first.SaleHalfOnly {
		t.Fatalf("2026-07-01 breakdown = %+v; want it to say the sale half only: it was frozen before markers were counted", first)
	}
	second := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-02")
	assertOrganizations(t, second)
	assertOrganizationsAddUp(t, second, 0)
	if !second.SaleHalfOnly {
		t.Fatalf("2026-07-02 breakdown = %+v; want the sale half only and nobody listed: the marker landed after the day was frozen at zero", second)
	}

	fourth := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-04")
	assertOrganizations(t, fourth,
		activeOrganization(t, env, "old-org", "both", 1),
		activeOrganization(t, env, "preparing-org", "member", 1),
	)
	if fourth.SaleHalfOnly {
		t.Fatalf("2026-07-04 breakdown = %+v; want both halves: it is the first day frozen with markers", fourth)
	}

	// A week that begins before markers were counted reads them from the
	// 4th on, and says some of it is the sale half only.
	week := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-06-30&granularity=week")
	assertOrganizations(t, week,
		activeOrganization(t, env, "old-org", "both", 2),
		activeOrganization(t, env, "preparing-org", "member", 1),
	)
	if !week.SaleHalfOnly {
		t.Fatalf("the week's breakdown = %+v; want it to say the sale half only: four of its days were frozen without markers", week)
	}

	// Thirteen months before 2027-08-05 is 2026-07-05: the three markers of
	// the 2nd and the 4th are older than that, and are deleted.
	if got := closeDaysAt(t, env, platformNoon(2027, 8, 5)).OrganizationMarkersDeleted; got != 3 {
		t.Fatalf("%d markers deleted on 2027-08-05, want 3: those of 2026-07-02 and 2026-07-04", got)
	}
	purged := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-04")
	assertOrganizations(t, purged, activeOrganization(t, env, "old-org", "sale", 1))
	assertOrganizationsAddUp(t, purged, 1)
	if !purged.SaleHalfOnly {
		t.Fatalf("2026-07-04 breakdown after the purge = %+v; want it to say the sale half only", purged)
	}
	_, frozen := activityFiguresAs(t, env, operator, "?span=custom&from=2026-07-01&to=2026-07-06")
	assertActivityOn(t, frozen, "2026-07-04", 2)

	// The 7th is inside the thirteen months still: its markers are read.
	kept := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-07")
	assertOrganizations(t, kept, activeOrganization(t, env, "old-org", "member", 1))
	if kept.SaleHalfOnly {
		t.Fatalf("2026-07-07 breakdown = %+v; want both halves: its markers are kept", kept)
	}
}

// customerKindsOn reads the Active Customers breakdown of a bucket.
func customerKindsOn(t *testing.T, env *testEnv, operator, bucket string) platformTrendBreakdown {
	t.Helper()
	breakdown := readBreakdown(t, env, operator, "?figure=active_customers&"+bucket)
	assertOrganizations(t, breakdown)
	assertEvents(t, breakdown)
	if breakdown.SaleHalfOnly {
		t.Fatalf("active_customers %s says the sale half only, which Active Customers has none of", bucket)
	}
	return breakdown
}

// assertCustomerKinds checks a recorded Active Customers breakdown: its
// three counts, and its one total, which is new plus returning.
func assertCustomerKinds(t *testing.T, breakdown platformTrendBreakdown, want breakdownCustomerKinds) {
	t.Helper()
	if !breakdown.Recorded || breakdown.CustomerKinds == nil || *breakdown.CustomerKinds != want {
		t.Fatalf("active_customers %s to %s recorded=%v kinds=%+v, want recorded with %+v",
			breakdown.FirstDay, breakdown.LastDay, breakdown.Recorded, shown(breakdown.CustomerKinds), want)
	}
	if len(breakdown.Totals) != 1 || breakdownTotal(t, breakdown, "all") != want.New+want.Returning {
		t.Fatalf("active_customers %s totals = %+v, want the one total, %d", breakdown.FirstDay, breakdown.Totals, want.New+want.Returning)
	}
}

// assertNotRecorded checks an Active Customers breakdown says the bucket has
// no figure, and states no count that could be read as a zero.
func assertNotRecorded(t *testing.T, breakdown platformTrendBreakdown) {
	t.Helper()
	if breakdown.Recorded || breakdown.CustomerKinds != nil || len(breakdown.Totals) != 0 {
		t.Fatalf("active_customers %s to %s recorded=%v kinds=%+v totals=%+v; want not recorded and nothing stated, never a zero",
			breakdown.FirstDay, breakdown.LastDay, breakdown.Recorded, shown(breakdown.CustomerKinds), breakdown.Totals)
	}
}

// TestActiveCustomersBreakdownStatesTheKindsTheDailyCloseFroze: on the 8th
// one Customer signs in for the first time, one who verified the day before
// browses and one who verified the day before buys online. The breakdown
// states the three counts, the same after every Customer marker is deleted,
// and a week adds its days up.
func TestActiveCustomersBreakdownStatesTheKindsTheDailyCloseFroze(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)
	adminSessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "Split Fest", "split-fest", feeTestBaseCents, 100)

	rita := customerSignIn(t, env, "rita@example.com")
	buyerSession(t, env, "bianca@example.com")
	actingOn(t, env, platformNoon(2026, 7, 8), func() {
		customerSignIn(t, env, "nora@example.com")
		if status := browse(t, env, rita); status != http.StatusOK {
			t.Fatalf("GET %s status=%d, want 200", customerSessionPath, status)
		}
		begin := beginCheckoutOK(t, env, "test-org", "split-fest",
			checkoutBody("bianca@example.com", "Bianca", "Vera", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
		confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	})
	closeDaysAt(t, env, platformNoon(2026, 7, 10))
	operator := trendsOperator(t, env)

	read := func() []platformTrendBreakdown {
		return []platformTrendBreakdown{
			customerKindsOn(t, env, operator, "first_day=2026-07-07"),
			customerKindsOn(t, env, operator, "first_day=2026-07-08"),
			customerKindsOn(t, env, operator, "first_day=2026-07-09"),
			customerKindsOn(t, env, operator, "first_day=2026-07-03&granularity=week"),
		}
	}
	frozen := read()
	assertCustomerKinds(t, frozen[0], breakdownCustomerKinds{New: 2})
	assertCustomerKinds(t, frozen[1], breakdownCustomerKinds{New: 1, Returning: 2, ReturningBought: 1})
	// Nobody was active on the 9th: a zero, and recorded.
	assertCustomerKinds(t, frozen[2], breakdownCustomerKinds{})
	// The week from the 3rd holds the three days that have a figure.
	assertCustomerKinds(t, frozen[3], breakdownCustomerKinds{New: 3, Returning: 2, ReturningBought: 1})
	assertActivityOn(t, activeCustomersAs(t, env, operator, "?span=7d"), "2026-07-08", 3)

	// What erasing every Customer would do to their markers, in SQL because
	// erasure has no route.
	if _, err := env.db.Exec(`DELETE FROM customer_activity_markers`); err != nil {
		t.Fatalf("delete the Customers' markers: %v", err)
	}
	if after := read(); !reflect.DeepEqual(after, frozen) {
		t.Fatalf("the breakdowns read %+v after the markers were deleted, want them as frozen: %+v", after, frozen)
	}

	// Today is not closed: it has no figure yet, which is not a zero.
	assertNotRecorded(t, customerKindsOn(t, env, operator, "first_day=2026-07-10"))
}

// TestActiveCustomersBreakdownOfABucketClosedWithRecordingOffIsNotRecorded:
// the deployment as it ships. Days closed with recording off have no figure,
// and their breakdown says not recorded. Switched on, the days closed from
// then on have one, the days closed before stay not recorded, and a week
// holding both states the days that have a figure.
func TestActiveCustomersBreakdownOfABucketClosedWithRecordingOffIsNotRecorded(t *testing.T) {
	env := setupTest(t)
	ana := customerSignIn(t, env, "ana@example.com")
	browseOn(t, env, ana, platformNoon(2026, 7, 8))

	closeDaysAt(t, env, platformNoon(2026, 7, 10))
	operator := trendsOperator(t, env)
	assertNotRecorded(t, customerKindsOn(t, env, operator, "first_day=2026-07-08"))
	assertNotRecorded(t, customerKindsOn(t, env, operator, "first_day=2026-07-03&granularity=week"))
	// The same day is frozen: Active Organizations answers for it.
	if organizations := readBreakdown(t, env, operator, "?figure=active_organizations&first_day=2026-07-08"); !organizations.Recorded {
		t.Fatalf("active_organizations 2026-07-08 = %+v; want it recorded", organizations)
	}

	// Recording is switched on during the 10th.
	recordCustomerActivity(t)
	browseOn(t, env, ana, platformNoon(2026, 7, 10))
	closeDaysAt(t, env, platformNoon(2026, 7, 13))

	assertNotRecorded(t, customerKindsOn(t, env, operator, "first_day=2026-07-08"))
	assertCustomerKinds(t, customerKindsOn(t, env, operator, "first_day=2026-07-10"), breakdownCustomerKinds{Returning: 1})
	assertCustomerKinds(t, customerKindsOn(t, env, operator, "first_day=2026-07-11"), breakdownCustomerKinds{})
	// The week from the 6th: the 10th, 11th and 12th have a figure.
	assertCustomerKinds(t, customerKindsOn(t, env, operator, "first_day=2026-07-06&granularity=week"), breakdownCustomerKinds{Returning: 1})
}

// TestNoPlatformTrendsResponseCarriesACustomer walks every response of
// Platform Trends: the first read for every span and both comparisons, which
// states every figure, and the breakdown of every figure for a closed day,
// for today and for a week. Customers have signed in, browsed, bought online
// and been imported, with recording on, so every figure that counts Customers
// has some to count. No response carries the id, the email or a name of any
// Customer on the platform.
func TestNoPlatformTrendsResponseCarriesACustomer(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSessionID, "Walk Fest", "walk-fest", feeTestBaseCents, 100)

	xiomara := customerSignIn(t, env, "xiomara.quispe@example.com")
	buyerSession(t, env, "ulises.zambrano@example.com")
	actingOn(t, env, platformNoon(2026, 7, 8), func() {
		customerSignIn(t, env, "wilfrida.yupanqui@example.com")
		if status := browse(t, env, xiomara); status != http.StatusOK {
			t.Fatalf("GET %s status=%d, want 200", customerSessionPath, status)
		}
		begin := beginCheckoutOK(t, env, "test-org", "walk-fest",
			checkoutBody("ulises.zambrano@example.com", "Ulises", "Zambrano", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
		confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
		commitBatch(t, env, adminSessionID, eventID, "walk-import", []map[string]any{{
			"customer_email": "venancio.huaman@example.com", "customer_first_name": "Venancio", "customer_last_name": "Huaman",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-08T15:00:00Z",
		}})
	})
	recordPageViews(t, env, platformNoon(2026, 7, 8), testOrgSlug, "walk-fest", "", 2)
	closeDaysAt(t, env, platformNoon(2026, 7, 10))

	// Every Customer on the platform, read in SQL because no route lists
	// them to anyone, and what of each would identify them.
	rows, err := env.db.Query(`SELECT id, email, COALESCE(first_name, ''), COALESCE(last_name, '') FROM customers`)
	if err != nil {
		t.Fatalf("read the Customers: %v", err)
	}
	defer rows.Close()
	var identifiers []string
	customers, named := 0, 0
	for rows.Next() {
		var id, email, firstName, lastName string
		if err := rows.Scan(&id, &email, &firstName, &lastName); err != nil {
			t.Fatalf("scan a Customer: %v", err)
		}
		customers++
		identifiers = append(identifiers, id, email)
		for _, name := range []string{firstName, lastName} {
			if name != "" {
				named++
				identifiers = append(identifiers, name)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the Customers: %v", err)
	}
	if customers != 4 || named < 4 {
		t.Fatalf("%d Customers with %d names between them; want the 4 of this test, two of them named", customers, named)
	}

	operator := trendsOperator(t, env)
	walk := func(path string) json.RawMessage {
		t.Helper()
		resp, body := env.get(t, path, authHeader(operator))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d error=%+v, want 200", path, resp.StatusCode, body.Error)
		}
		for _, identifier := range identifiers {
			if strings.Contains(string(body.Data), identifier) {
				t.Fatalf("GET %s carries %q, which identifies a Customer: %s", path, identifier, body.Data)
			}
		}
		return body.Data
	}

	for _, span := range []string{"span=7d", "span=30d", "span=90d", "span=12m", "span=custom&from=2026-07-01&to=2026-07-09"} {
		for _, compare := range []string{"previous", "year_earlier"} {
			var trends struct {
				Figures map[string]json.RawMessage `json:"figures"`
			}
			if err := json.Unmarshal(walk(platformTrendsPath+"?"+span+"&compare="+compare), &trends); err != nil {
				t.Fatalf("decode Platform Trends: %v", err)
			}
			// The first read states every figure there is, and the walk of
			// the breakdowns below covers each of them.
			if len(trends.Figures) != len(platformtrendssvc.BreakdownFigures) {
				t.Fatalf("Platform Trends states %d figures, the breakdown knows %d", len(trends.Figures), len(platformtrendssvc.BreakdownFigures))
			}
			for _, figure := range platformtrendssvc.BreakdownFigures {
				if _, ok := trends.Figures[string(figure)]; !ok {
					t.Fatalf("Platform Trends states no %s", figure)
				}
			}
		}
	}

	for _, figure := range platformtrendssvc.BreakdownFigures {
		for _, bucket := range []string{"first_day=2026-07-08", "first_day=2026-07-10", "first_day=2026-07-03&granularity=week"} {
			walk(platformTrendBreakdownPath + "?figure=" + string(figure) + "&" + bucket)
		}
	}

	// The walk read figures with Customers in them, not empty answers.
	assertCustomerKinds(t, customerKindsOn(t, env, operator, "first_day=2026-07-08"),
		breakdownCustomerKinds{New: 1, Returning: 2, ReturningBought: 1})
	verified := readBreakdown(t, env, operator, "?figure=new_verified_customers&first_day=2026-07-08")
	if breakdownTotal(t, verified, "all") != 1 {
		t.Fatalf("new_verified_customers 2026-07-08 = %+v, want 1", verified.Totals)
	}
}
