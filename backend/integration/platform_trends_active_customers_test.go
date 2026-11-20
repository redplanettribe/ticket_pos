package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	platformtrendsrepo "github.com/peter/ticket_pos/backend/internal/platformtrends/repository"
	platformtrendssvc "github.com/peter/ticket_pos/backend/internal/platformtrends/service"
)

// Active Customers (#817, spec #808, ADR 0080). A Customer is active on a
// platform day they acted as themselves: used a full Customer Session in any
// way, the sign-in that mints one included, or accepted a Ticket Assignment
// through an Assignment Link. One marker holding the Customer and the day
// records it, the daily close freezes the count, and Platform Trends reads
// the frozen totals alone.
//
// The recording ships dark (ADR 0045): setupTest leaves it off, as a
// deployment is, and the tests that are about the figure switch it on with
// recordCustomerActivity.
//
// Time is moved explicitly. The fixed clock is 2026-07-07T12:00Z, so a
// Customer who signs in outside actingOn does so on 2026-07-07. actingOn
// moves every clock a Customer's act is stamped on, and closeDaysAt moves
// the Platform Trends clock and runs the close. Every test acts on the 7th
// or later and closes later still, so each says which days anybody acted on.

// recordCustomerActivity switches Customer recording on until the next
// setupTest, which switches it off again.
func recordCustomerActivity(t *testing.T) {
	t.Helper()
	sharedApp.PlatformTrendsService.WithCustomerActivityRecording(true)
	t.Cleanup(func() { sharedApp.PlatformTrendsService.WithCustomerActivityRecording(false) })
}

// actingOn moves the clocks a Customer acts on for the length of do, and
// puts the fixed clock back: the customers clock, which a Customer Session
// is authenticated and minted on and so the one a marker's day is read
// from; the consent clock beside it, as a sign-in passes through both; the
// catalog clock, which an accepted Ticket Assignment is stamped on; and the
// sales clock, which stamps the instant an Online Sale is sold.
func actingOn(t *testing.T, env *testEnv, at time.Time, do func()) {
	t.Helper()
	set := func(now func() time.Time) {
		sharedApp.CustomersService.WithClock(now)
		sharedApp.ConsentService.WithClock(now)
		sharedApp.CatalogService.WithClock(now)
		sharedApp.SalesService.WithClock(now)
	}
	set(func() time.Time { return at })
	defer set(func() time.Time { return env.fixedClock })
	do()
}

// browse makes the read every Storefront page makes for a signed-in
// Customer, and returns the status it was answered with.
func browse(t *testing.T, env *testEnv, session string) int {
	t.Helper()
	resp, _ := env.get(t, customerSessionPath, authHeader(session))
	return resp.StatusCode
}

// browseOn has the session's holder load a page at an instant, buying
// nothing.
func browseOn(t *testing.T, env *testEnv, session string, at time.Time) {
	t.Helper()
	actingOn(t, env, at, func() {
		if status := browse(t, env, session); status != http.StatusOK {
			t.Fatalf("GET %s at %s status=%d, want 200", customerSessionPath, at.Format(time.RFC3339), status)
		}
	})
}

// trendsOperator signs a Platform Operator in. A test that reads Platform
// Trends more than a few times does it once and reads through
// activeCustomersAs, since a passcode is rationed per address.
func trendsOperator(t *testing.T, env *testEnv) string {
	t.Helper()
	return operatorSession(t, env, "operator@example.com")
}

// activityFiguresAs reads Platform Trends as the Platform Operator holding
// the session and returns its two activity figures.
func activityFiguresAs(t *testing.T, env *testEnv, operator, query string) (customers, organizations platformTrendsActivityFigure) {
	t.Helper()
	var out struct {
		Figures struct {
			ActiveCustomers     platformTrendsActivityFigure `json:"active_customers"`
			ActiveOrganizations platformTrendsActivityFigure `json:"active_organizations"`
		} `json:"figures"`
	}
	operatorGetOK(t, env, operator, platformTrendsPath+query, &out)
	return out.Figures.ActiveCustomers, out.Figures.ActiveOrganizations
}

// activeCustomersAs reads Platform Trends' Active Customers through a
// Platform Operator's session.
func activeCustomersAs(t *testing.T, env *testEnv, operator, query string) platformTrendsActivityFigure {
	t.Helper()
	customers, _ := activityFiguresAs(t, env, operator, query)
	return customers
}

// readActiveCustomers signs a Platform Operator in and reads Active
// Customers.
func readActiveCustomers(t *testing.T, env *testEnv, query string) platformTrendsActivityFigure {
	t.Helper()
	return activeCustomersAs(t, env, trendsOperator(t, env), query)
}

// assertNoActivityOn checks a day is absent from the figure, which is not
// the same as the figure stating a zero for it.
func assertNoActivityOn(t *testing.T, figure platformTrendsActivityFigure, day string) {
	t.Helper()
	if bucket := activityOn(figure, day); bucket != nil {
		t.Fatalf("%s = %+v, want the day absent: it has no Active Customers figure", day, *bucket)
	}
}

// customerMarkerWrites stands where the Customer marker store stands and
// counts the writes that reach it, failing them while failing is set.
// Everything else is the real store.
type customerMarkerWrites struct {
	platformtrendssvc.CustomerMarkerStore
	writes  atomic.Int64
	failing atomic.Bool
}

func (m *customerMarkerWrites) MarkCustomerActive(ctx context.Context, customerID, day string) error {
	m.writes.Add(1)
	if m.failing.Load() {
		return errors.New("the marker store is down")
	}
	return m.CustomerMarkerStore.MarkCustomerActive(ctx, customerID, day)
}

// watchCustomerMarkerWrites puts a counting store in front of the real one
// until the next setupTest, which puts the real one back.
func watchCustomerMarkerWrites(t *testing.T) *customerMarkerWrites {
	t.Helper()
	watched := &customerMarkerWrites{CustomerMarkerStore: platformTrendsStore()}
	sharedApp.PlatformTrendsService.WithCustomerMarkerStore(watched)
	return watched
}

// customerIDOf reads a Customer's id, in SQL because no response of any
// route states one to a test that is not that Customer's own session, and
// the tests below need it to say a response does not carry it, to stage an
// erasure, or to name a Customer no route can make.
func customerIDOf(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`SELECT id FROM customers WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("read the Customer %s: %v", email, err)
	}
	return id
}

// frozenCustomerTotals reads what the daily close froze for one day's
// Active Customers, nil when the day has no such figure. It asks the store
// Platform Trends keeps its totals in, which holds the 7-day and 30-day
// counts beside the split by kind; the breakdown read serves the split from
// these same totals, and is asserted over HTTP with the breakdowns (#819).
func frozenCustomerTotals(t *testing.T, day string) *platformtrendsrepo.CustomerActivityTotals {
	t.Helper()
	days, err := platformTrendsStore().ActivityTotalsBetween(context.Background(), day, day)
	if err != nil {
		t.Fatalf("read the frozen totals of %s: %v", day, err)
	}
	if len(days) != 1 {
		t.Fatalf("%d frozen rows for %s, want 1", len(days), day)
	}
	return days[0].Customers
}

// TestASignedInCustomerBrowsingIsActiveWithoutBuying: one Customer signs in
// on the 7th and loads pages on the 8th, another signs in with Google on the
// 8th, and nobody buys anything. The read states the daily series, the mean,
// the 7-day and 30-day counts and the day recording began, and nowhere a
// Customer.
func TestASignedInCustomerBrowsingIsActiveWithoutBuying(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)

	ana := customerSignIn(t, env, "ana@example.com")
	browseOn(t, env, ana, platformNoon(2026, 7, 8))
	actingOn(t, env, platformNoon(2026, 7, 8), func() {
		readCustomerArea(t, env, ana, "")
		// A Google Sign-In mints its session without passing through the
		// middleware, and is the Customer acting all the same.
		customerGoogleSignIn(t, env, "gala@example.com")
	})

	before := readActiveCustomers(t, env, "?span=7d")
	if !before.Recorded || before.ExactSince != nil || before.DataSince != nil || before.Mean != nil {
		t.Fatalf("before any close recorded=%v exact_since=%v data_since=%v mean=%v; want recorded, since recording is on, and nothing stated: markers are read by the close alone",
			before.Recorded, shown(before.ExactSince), shown(before.DataSince), shown(before.Mean))
	}

	closeDaysAt(t, env, platformNoon(2026, 7, 10))

	figure := readActiveCustomers(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-07", 1)
	assertActivityOn(t, figure, "2026-07-08", 2)
	assertActivityOn(t, figure, "2026-07-09", 0)
	// The span reaches back to the 3rd. No day before the 7th has a figure.
	if len(figure.Buckets) != 7 {
		t.Fatalf("%d buckets, want one entry per day of the span", len(figure.Buckets))
	}
	for _, day := range []string{"2026-07-03", "2026-07-04", "2026-07-05", "2026-07-06"} {
		assertNoActivityOn(t, figure, day)
	}
	if !figure.Recorded || shown(figure.ExactSince) != "2026-07-07" || shown(figure.DataSince) != "2026-07-07" || figure.SaleHalfOnly {
		t.Fatalf("recorded=%v exact_since=%v data_since=%v sale_half_only=%v; want recorded, recording begun on 2026-07-07, no sale half",
			figure.Recorded, shown(figure.ExactSince), shown(figure.DataSince), figure.SaleHalfOnly)
	}
	assertNear(t, "mean", figure.Mean, 1)
	assertCount(t, "distinct_7d", figure.Distinct7Days, 2)
	assertCount(t, "distinct_30d", figure.Distinct30Days, 2)

	// Neither read of the day names who it was.
	resp, body := env.get(t, platformTrendsPath+"?span=7d", authHeader(trendsOperator(t, env)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d, want 200", platformTrendsPath, resp.StatusCode)
	}
	for _, email := range []string{"ana@example.com", "gala@example.com"} {
		for _, identifier := range []string{email, customerIDOf(t, env, email)} {
			if strings.Contains(string(body.Data), identifier) {
				t.Fatalf("Platform Trends carries %q: no field may return a Customer for a day", identifier)
			}
		}
	}
}

// TestActiveCustomersStatesNoChangeAgainstAComparisonFromBeforeRecordingBegan:
// a comparison span that begins before the day recording began is drawn from
// that day on, with its mean, and no change is stated against it (#826). One
// that begins on that day states its change.
func TestActiveCustomersStatesNoChangeAgainstAComparisonFromBeforeRecordingBegan(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)

	// Active: the 7th 1, the 8th 1, the 9th 0, the 10th 2, the 11th 1, the
	// 12th 0.
	ana := customerSignIn(t, env, "ana@example.com")
	for _, day := range []int{8, 10, 11} {
		browseOn(t, env, ana, platformNoon(2026, 7, day))
	}
	actingOn(t, env, platformNoon(2026, 7, 10), func() {
		customerSignIn(t, env, "bea@example.com")
	})
	closeDaysAt(t, env, platformNoon(2026, 7, 13))
	operator := trendsOperator(t, env)

	// 2026-07-09..2026-07-12 against 2026-07-05..2026-07-08, of which the
	// 7th and the 8th have a figure.
	figure := activeCustomersAs(t, env, operator, "?span=custom&from=2026-07-09&to=2026-07-12")
	if shown(figure.DataSince) != "2026-07-07" {
		t.Fatalf("data_since = %v, want 2026-07-07", shown(figure.DataSince))
	}
	assertNear(t, "mean", figure.Mean, 3.0/4)
	assertNear(t, "comparison_mean", figure.ComparisonMean, 1)
	for i, bucket := range figure.ComparisonBuckets {
		if wantPresent := i >= 2; (bucket != nil) != wantPresent {
			t.Fatalf("comparison bucket %d = %+v; want present=%v, recording having begun on 2026-07-07", i, shown(bucket), wantPresent)
		}
	}
	if figure.Change != nil {
		t.Fatalf("change = %v; want none against a comparison span that begins before recording did", *figure.Change)
	}

	// 2026-07-10..2026-07-12 against 2026-07-07..2026-07-09, recorded whole.
	figure = activeCustomersAs(t, env, operator, "?span=custom&from=2026-07-10&to=2026-07-12")
	assertNear(t, "covered mean", figure.Mean, 1)
	assertNear(t, "covered comparison_mean", figure.ComparisonMean, 2.0/3)
	assertNear(t, "covered change", figure.Change, 0.5)
}

// TestACustomerActingManyTimesInADayIsCountedOnce, and after their first
// act of a platform day no further write reaches the marker store. The
// first act of the next day is one more.
func TestACustomerActingManyTimesInADayIsCountedOnce(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)
	writes := watchCustomerMarkerWrites(t)

	ana := customerSignIn(t, env, "ana@example.com")
	if got := writes.writes.Load(); got != 1 {
		t.Fatalf("%d marker writes after signing in, want 1", got)
	}
	for i := 0; i < 20; i++ {
		at := platformNoon(2026, 7, 8).Add(time.Duration(i) * time.Minute)
		browseOn(t, env, ana, at)
		actingOn(t, env, at, func() { readCustomerArea(t, env, ana, "") })
	}
	if got := writes.writes.Load(); got != 2 {
		t.Fatalf("%d marker writes after 40 requests on the next day, want 2: one per day", got)
	}

	closeDaysAt(t, env, platformNoon(2026, 7, 10))
	figure := readActiveCustomers(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-07", 1)
	assertActivityOn(t, figure, "2026-07-08", 1)
	assertCount(t, "distinct_7d", figure.Distinct7Days, 1)
}

// TestAcceptingATicketAssignmentIsActiveAndAForwardedLinkOrAStaffSaleIsNot:
// with recording on, the only Customers active are the Customer who signed in
// and the Holder who accepted a Ticket Assignment without ever signing in.
// The Customers an import and a sale typed by staff were recorded for, and
// the one whose Confirmation Link was opened and used, are active on no day.
func TestAcceptingATicketAssignmentIsActiveAndAForwardedLinkOrAStaffSaleIsNot(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)
	writes := watchCustomerMarkerWrites(t)

	// The fixture imports sales for ana@ and bruno@, and a sale is typed in
	// by hand for dora@: staff acting, on the 7th.
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	recordManualSaleOK(t, env, f.staffSession, f.eventID,
		manualSaleBody("dora@example.com", "Dora", "Mora", f.ticketTypeID, 1, "cash", "2026-07-07T10:00:00Z"))

	actingOn(t, env, platformNoon(2026, 7, 8), func() {
		// Whoever holds Bruno's Sale Confirmation opens its link and reads
		// the sale behind it. It may have been forwarded.
		_, linkSession := redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.brunoRef), "")
		if status := browse(t, env, linkSession); status != http.StatusOK {
			t.Fatalf("GET %s on a Confirmation Link session status=%d, want 200", customerSessionPath, status)
		}
		readCustomerArea(t, env, linkSession, "")
	})
	if got := writes.writes.Load(); got != 0 {
		t.Fatalf("%d marker writes after an import, a manual sale and a Confirmation Link, want none", got)
	}

	actingOn(t, env, platformNoon(2026, 7, 8), func() {
		ana := customerSignIn(t, env, "ana@example.com")
		assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	})
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	actingOn(t, env, platformNoon(2026, 7, 9), func() {
		acceptAssignmentOK(t, env, token)
		// People click twice.
		acceptAssignmentOK(t, env, token)
	})
	// The link opened again the day after accepts nothing new: by then it
	// may be a mail client's prefetch or in somebody else's hands.
	actingOn(t, env, platformNoon(2026, 7, 10), func() { acceptAssignmentOK(t, env, token) })
	if got := writes.writes.Load(); got != 2 {
		t.Fatalf("%d marker writes, want 2: Ana signing in and Carla accepting", got)
	}

	closeDaysAt(t, env, platformNoon(2026, 7, 12))

	figure := readActiveCustomers(t, env, "?span=7d")
	// The 7th was closed with recording on and nobody counted yet: it is
	// before recording began, absent and not zero.
	assertNoActivityOn(t, figure, "2026-07-07")
	assertActivityOn(t, figure, "2026-07-08", 1)
	assertActivityOn(t, figure, "2026-07-09", 1)
	assertActivityOn(t, figure, "2026-07-10", 0)
	if shown(figure.ExactSince) != "2026-07-08" {
		t.Fatalf("exact_since = %v, want 2026-07-08, the first day a Customer was counted", shown(figure.ExactSince))
	}
	assertCount(t, "distinct_7d", figure.Distinct7Days, 2)
	assertCount(t, "distinct_30d", figure.Distinct30Days, 2)
	// Recording began on the 8th, four days before the span's last day.
	assertCount(t, "distinct_7d_days", figure.Distinct7DaysRecorded, 4)
	assertCount(t, "distinct_30d_days", figure.Distinct30DaysRecorded, 4)

	// Four nights on, the 7 days ending on the 15th are all recorded and
	// the 30 days still hold only the eight since recording began.
	closeDaysAt(t, env, platformNoon(2026, 7, 16))
	later := readActiveCustomers(t, env, "?span=7d")
	assertCount(t, "distinct_7d_days as of 2026-07-15", later.Distinct7DaysRecorded, 7)
	assertCount(t, "distinct_30d_days as of 2026-07-15", later.Distinct30DaysRecorded, 8)
}

// TestWithRecordingOffNothingIsWrittenAndActiveCustomersIsNotYetRecorded:
// the deployment as it ships. A sign-in, a day of browsing and an accepted
// Ticket Assignment write nothing, the days closed meanwhile have no Active
// Customers figure though Active Organizations has one, and the read says
// not yet recorded. Switched on, the figure begins on the first day closed
// with a Customer counted, and the days closed before stay absent; switched
// off again, the days closed after are absent too and the figure stays
// recorded.
func TestWithRecordingOffNothingIsWrittenAndActiveCustomersIsNotYetRecorded(t *testing.T) {
	env := setupTest(t)
	writes := watchCustomerMarkerWrites(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)

	ana := customerSignIn(t, env, "ana@example.com")
	browseOn(t, env, ana, platformNoon(2026, 7, 8))
	actingOn(t, env, platformNoon(2026, 7, 8), func() {
		customerGoogleSignIn(t, env, "gala@example.com")
		assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
		acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
		redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.brunoRef), "")
	})
	if got := writes.writes.Load(); got != 0 {
		t.Fatalf("%d marker writes with recording off, want none", got)
	}

	operator := trendsOperator(t, env)
	closeDaysAt(t, env, platformNoon(2026, 7, 10))
	off := activeCustomersAs(t, env, operator, "?span=7d")
	if off.Recorded || off.ExactSince != nil || off.DataSince != nil || off.Mean != nil || off.Distinct7Days != nil || off.Distinct30Days != nil ||
		off.Distinct7DaysRecorded != nil || off.Distinct30DaysRecorded != nil {
		t.Fatalf("with recording off: recorded=%v exact_since=%v data_since=%v mean=%v distinct_7d=%v distinct_30d=%v; want not recorded and nothing stated",
			off.Recorded, shown(off.ExactSince), shown(off.DataSince), shown(off.Mean), shown(off.Distinct7Days), shown(off.Distinct30Days))
	}
	for i, bucket := range off.Buckets {
		if bucket != nil {
			t.Fatalf("bucket %d = %+v with recording off, want every bucket absent, never zero", i, *bucket)
		}
	}
	// The same days are frozen: Active Organizations states them.
	_, organizations := activityFiguresAs(t, env, operator, "?span=7d")
	assertActivityOn(t, organizations, "2026-07-08", 0)

	// Recording is switched on during the 10th.
	recordCustomerActivity(t)
	browseOn(t, env, ana, platformNoon(2026, 7, 10))
	closeDaysAt(t, env, platformNoon(2026, 7, 12))

	on := activeCustomersAs(t, env, operator, "?span=7d")
	if !on.Recorded || shown(on.ExactSince) != "2026-07-10" || shown(on.DataSince) != "2026-07-10" {
		t.Fatalf("recorded=%v exact_since=%v data_since=%v; want recorded, recording begun on 2026-07-10",
			on.Recorded, shown(on.ExactSince), shown(on.DataSince))
	}
	for _, day := range []string{"2026-07-07", "2026-07-08", "2026-07-09"} {
		assertNoActivityOn(t, on, day)
	}
	assertActivityOn(t, on, "2026-07-10", 1)
	assertActivityOn(t, on, "2026-07-11", 0)
	assertNear(t, "mean", on.Mean, 0.5)
	assertCount(t, "distinct_7d", on.Distinct7Days, 1)

	// And off again: the 12th is closed with recording off.
	sharedApp.PlatformTrendsService.WithCustomerActivityRecording(false)
	browseOn(t, env, ana, platformNoon(2026, 7, 12))
	closeDaysAt(t, env, platformNoon(2026, 7, 13))

	again := activeCustomersAs(t, env, operator, "?span=7d")
	if !again.Recorded || shown(again.ExactSince) != "2026-07-10" {
		t.Fatalf("recorded=%v exact_since=%v after switching off; want still recorded, begun on 2026-07-10",
			again.Recorded, shown(again.ExactSince))
	}
	assertNoActivityOn(t, again, "2026-07-12")
	assertActivityOn(t, again, "2026-07-10", 1)
	if again.Distinct7Days != nil || again.Distinct7DaysRecorded != nil || again.Distinct30DaysRecorded != nil {
		t.Fatalf("distinct_7d = %v over %v / %v days, want none: the span's last day has no figure",
			shown(again.Distinct7Days), shown(again.Distinct7DaysRecorded), shown(again.Distinct30DaysRecorded))
	}

	// And on once more: the 13th has a figure, and the runs of days ending
	// on it hold the 10th, the 11th and the 13th. The 12th, closed while
	// recording was off, is not recorded.
	recordCustomerActivity(t)
	closeDaysAt(t, env, platformNoon(2026, 7, 14))
	resumed := activeCustomersAs(t, env, operator, "?span=7d")
	assertActivityOn(t, resumed, "2026-07-13", 0)
	assertCount(t, "distinct_7d", resumed.Distinct7Days, 1)
	assertCount(t, "distinct_7d_days", resumed.Distinct7DaysRecorded, 3)
	assertCount(t, "distinct_30d_days", resumed.Distinct30DaysRecorded, 3)
	if got := writes.writes.Load(); got != 1 {
		t.Fatalf("%d marker writes in all, want the 1 made while recording was on", got)
	}
}

// TestTheDailyCloseSplitsActiveCustomersByKind: on the 8th one Customer
// signs in for the first time, one who verified the day before browses, and
// one who verified the day before buys online. A sale imported for the
// browsing Customer that day does not make them one who bought.
func TestTheDailyCloseSplitsActiveCustomersByKind(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSessionID, "Split Fest", "split-fest", feeTestBaseCents, 100)

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
		commitBatch(t, env, adminSessionID, eventID, "split-import", []map[string]any{
			importSale("rita@example.com", gaID, 1, "2026-07-08T15:00:00Z"),
		})
	})

	closeDaysAt(t, env, platformNoon(2026, 7, 10))

	figure := readActiveCustomers(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-07", 2)
	assertActivityOn(t, figure, "2026-07-08", 3)

	want := &platformtrendsrepo.CustomerActivityTotals{
		Active: 3, Active7Days: 3, Active30Days: 3,
		New: 1, Returning: 2, ReturningBought: 1,
	}
	if got := frozenCustomerTotals(t, "2026-07-08"); !reflect.DeepEqual(got, want) {
		t.Fatalf("2026-07-08 frozen as %+v, want %+v: Nora new, Rita and Bianca returning, Bianca bought", shown(got), *want)
	}
	first := &platformtrendsrepo.CustomerActivityTotals{Active: 2, Active7Days: 2, Active30Days: 2, New: 2}
	if got := frozenCustomerTotals(t, "2026-07-07"); !reflect.DeepEqual(got, first) {
		t.Fatalf("2026-07-07 frozen as %+v, want %+v: both verified that day", shown(got), *first)
	}
	quiet := &platformtrendsrepo.CustomerActivityTotals{Active7Days: 3, Active30Days: 3}
	if got := frozenCustomerTotals(t, "2026-07-09"); !reflect.DeepEqual(got, quiet) {
		t.Fatalf("2026-07-09 frozen as %+v, want %+v: nobody active, a zero and not an absence", shown(got), *quiet)
	}
}

// TestFrozenActiveCustomersOutliveTheMarkersAndTheCustomer: a day is frozen
// with two Customers active. One Customer's markers are then deleted and the
// other Customer is deleted outright, which takes their markers with them.
// The frozen day reads exactly as it did, and the next day's 7-day count,
// frozen after, has neither of them in it.
func TestFrozenActiveCustomersOutliveTheMarkersAndTheCustomer(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)

	ana := customerSignIn(t, env, "ana@example.com")
	browseOn(t, env, ana, platformNoon(2026, 7, 8))

	// A Customer with nothing but a record, staged in SQL because every
	// Customer a route makes is held in place by evidence: a signed-in one
	// by their Consent Records and a Holder by their Ticket, both ON DELETE
	// RESTRICT. Erasure is manual (ADR 0010), so this is the fixture that
	// allows it. They act through the one call every marker is written by.
	var loneID string
	if err := env.db.QueryRow(`
		INSERT INTO customers (email, first_name, last_name, verified_at)
		VALUES ('lone@example.com', 'Lone', 'Ruiz', $1) RETURNING id
	`, platformNoon(2026, 7, 6)).Scan(&loneID); err != nil {
		t.Fatalf("stage a Customer with no evidence: %v", err)
	}
	sharedApp.PlatformTrendsService.MarkCustomerActive(context.Background(), loneID, platformNoon(2026, 7, 8))

	closeDaysAt(t, env, platformNoon(2026, 7, 9))
	const span = "?span=custom&from=2026-07-07&to=2026-07-08"
	operator := trendsOperator(t, env)
	frozen := activeCustomersAs(t, env, operator, span)
	assertActivityOn(t, frozen, "2026-07-07", 1)
	assertActivityOn(t, frozen, "2026-07-08", 2)
	assertCount(t, "distinct_7d", frozen.Distinct7Days, 2)
	frozenSplit := frozenCustomerTotals(t, "2026-07-08")

	// What erasing a Customer does to their markers, in SQL because erasure
	// has no route: Ana's markers deleted by hand, and the lone Customer's
	// row deleted, which the database follows with their markers.
	if _, err := env.db.Exec(`DELETE FROM customer_activity_markers WHERE customer_id = $1`, customerIDOf(t, env, "ana@example.com")); err != nil {
		t.Fatalf("delete a Customer's markers: %v", err)
	}
	if _, err := env.db.Exec(`DELETE FROM customers WHERE id = $1`, loneID); err != nil {
		t.Fatalf("delete a Customer, whose markers the database deletes with them: %v", err)
	}

	closeDaysAt(t, env, platformNoon(2026, 7, 10))

	after := activeCustomersAs(t, env, operator, span)
	if !reflect.DeepEqual(figureValues(after), figureValues(frozen)) {
		t.Fatalf("the frozen days read %+v after the markers and the Customer were deleted, want them as before: %+v",
			figureValues(after), figureValues(frozen))
	}
	if got := frozenCustomerTotals(t, "2026-07-08"); !reflect.DeepEqual(got, frozenSplit) {
		t.Fatalf("2026-07-08 split = %+v after the deletions, want it as frozen: %+v", shown(got), shown(frozenSplit))
	}
	// The 9th was frozen after the deletions. Nobody was active on it, and
	// nobody is left in the 7 days ending on it: both Customers' markers
	// are gone, the lone one's without anything having deleted them by name.
	next := activeCustomersAs(t, env, operator, "?span=custom&from=2026-07-07&to=2026-07-09")
	assertActivityOn(t, next, "2026-07-09", 0)
	assertCount(t, "distinct_7d", next.Distinct7Days, 0)
	assertCount(t, "distinct_30d", next.Distinct30Days, 0)
}

// TestTheDailyClosePurgesCustomerMarkersPastThirteenMonthsAndRewritesNoDay:
// a Customer who turns up for a day already frozen changes nothing about it,
// and the markers are deleted once they are older than thirteen months,
// whether or not recording is still on, leaving every frozen day as it was.
func TestTheDailyClosePurgesCustomerMarkersPastThirteenMonthsAndRewritesNoDay(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)

	ana := customerSignIn(t, env, "ana@example.com")
	browseOn(t, env, ana, platformNoon(2026, 7, 8))

	result := closeDaysAt(t, env, platformNoon(2026, 7, 9))
	if result.CustomerMarkersDeleted != 0 {
		t.Fatalf("%d Customer markers deleted on 2026-07-09, want none: the oldest is two days old", result.CustomerMarkersDeleted)
	}
	const span = "?span=custom&from=2026-07-07&to=2026-07-08"
	operator := trendsOperator(t, env)
	frozen := activeCustomersAs(t, env, operator, span)
	assertActivityOn(t, frozen, "2026-07-07", 1)
	assertActivityOn(t, frozen, "2026-07-08", 1)

	// The customers clock is on the 7th, a day already frozen: a marker
	// lands on it late, as one written just before midnight might.
	customerSignIn(t, env, "bea@example.com")
	closeDaysAt(t, env, platformNoon(2026, 7, 10))

	rewritten := activeCustomersAs(t, env, operator, span)
	if !reflect.DeepEqual(figureValues(rewritten), figureValues(frozen)) {
		t.Fatalf("the frozen days read %+v after a marker landed on one, want them as frozen: %+v",
			figureValues(rewritten), figureValues(frozen))
	}
	// Later days' counts hold the late marker, as they hold a late sale.
	assertCount(t, "distinct_7d as of 2026-07-09",
		activeCustomersAs(t, env, operator, "?span=custom&from=2026-07-07&to=2026-07-09").Distinct7Days, 2)

	// Recording is switched off, and what was recorded is still deleted on
	// time. Thirteen months before 2027-08-08 is 2026-07-08: the two markers
	// of the 7th are older than that and the marker of the 8th is not.
	sharedApp.PlatformTrendsService.WithCustomerActivityRecording(false)
	if got := closeDaysAt(t, env, platformNoon(2027, 8, 8)).CustomerMarkersDeleted; got != 2 {
		t.Fatalf("%d Customer markers deleted on 2027-08-08, want 2: the markers of 2026-07-07", got)
	}
	if got := closeDaysAt(t, env, platformNoon(2027, 8, 8)).CustomerMarkersDeleted; got != 0 {
		t.Fatalf("%d Customer markers deleted by a second run the same night, want none", got)
	}
	if got := closeDaysAt(t, env, platformNoon(2027, 8, 9)).CustomerMarkersDeleted; got != 1 {
		t.Fatalf("%d Customer markers deleted on 2027-08-09, want 1: the marker of 2026-07-08", got)
	}

	after := activeCustomersAs(t, env, operator, span)
	if !reflect.DeepEqual(figureValues(after), figureValues(frozen)) {
		t.Fatalf("the frozen days read %+v after their markers were deleted, want them as frozen: %+v",
			figureValues(after), figureValues(frozen))
	}
	if !after.Recorded || shown(after.ExactSince) != "2026-07-07" {
		t.Fatalf("recorded=%v exact_since=%v after the purge; want still recorded, begun on 2026-07-07",
			after.Recorded, shown(after.ExactSince))
	}
}

// TestACustomerMarkerStoreThatErrorsDoesNotFailTheCustomersRequest: the
// sign-in, the page and the accepted Ticket Assignment are all answered as
// if nothing were wrong, and since no write landed the next request of the
// day tries again.
func TestACustomerMarkerStoreThatErrorsDoesNotFailTheCustomersRequest(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	writes := watchCustomerMarkerWrites(t)

	writes.failing.Store(true)
	var ana string
	actingOn(t, env, platformNoon(2026, 7, 8), func() {
		// customerSignIn, assignTicketOK and acceptAssignmentOK each fail
		// the test on anything but success.
		ana = customerSignIn(t, env, "ana@example.com")
		if status := browse(t, env, ana); status != http.StatusOK {
			t.Fatalf("GET %s with the marker store down status=%d, want 200", customerSessionPath, status)
		}
		assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
		acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	})
	failed := writes.writes.Load()
	if failed < 3 {
		t.Fatalf("%d marker writes, want at least 3 that failed: the sign-in, a request, the acceptance", failed)
	}

	writes.failing.Store(false)
	browseOn(t, env, ana, platformNoon(2026, 7, 8).Add(time.Minute))
	browseOn(t, env, ana, platformNoon(2026, 7, 8).Add(2*time.Minute))
	if got := writes.writes.Load(); got != failed+1 {
		t.Fatalf("%d marker writes, want %d: the failed ones, and one retry that landed", got, failed+1)
	}

	closeDaysAt(t, env, platformNoon(2026, 7, 10))
	assertActivityOn(t, readActiveCustomers(t, env, "?span=7d"), "2026-07-08", 1)
}

// TestACustomerMarkerBelongsToThePlatformDayTheyActedOn: two Customers
// loading a page a second apart either side of midnight in Guayaquil are
// active on the two platform days.
func TestACustomerMarkerBelongsToThePlatformDayTheyActedOn(t *testing.T) {
	env := setupTest(t)
	recordCustomerActivity(t)
	late := customerSignIn(t, env, "late@example.com")
	early := customerSignIn(t, env, "early@example.com")

	// Midnight on 2026-07-09 in Guayaquil (UTC-5) is 2026-07-09T05:00Z.
	browseOn(t, env, late, time.Date(2026, 7, 9, 4, 59, 59, 0, time.UTC))
	browseOn(t, env, early, time.Date(2026, 7, 9, 5, 0, 0, 0, time.UTC))

	closeDaysAt(t, env, platformNoon(2026, 7, 10))
	figure := readActiveCustomers(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-07", 2)
	assertActivityOn(t, figure, "2026-07-08", 1)
	assertActivityOn(t, figure, "2026-07-09", 1)
}

// TestTheDailyCloseStatesTheCustomerMarkersItDeleted in its response, which
// carries counts and no Customer.
func TestTheDailyCloseStatesTheCustomerMarkersItDeleted(t *testing.T) {
	env := setupTest(t)
	resp, body := env.post(t, dailyClosePath, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("daily close status=%d, want 200", resp.StatusCode)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &fields); err != nil {
		t.Fatalf("decode daily close result: %v", err)
	}
	if string(fields["customer_markers_deleted"]) != "0" {
		t.Fatalf("customer_markers_deleted = %s, want 0", fields["customer_markers_deleted"])
	}
}
