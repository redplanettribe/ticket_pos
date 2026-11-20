package integration

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// Active Organizations and the daily close that freezes it (#815, spec #808,
// ADR 0080). An Organization is active on the platform day a Ticket Sale was
// recorded for it: the day the sale reached the platform, on any Sales
// Channel, reversed or not. The close freezes each closed day into totals
// that are never rewritten, and Platform Trends reads those totals alone.
//
// Time is moved explicitly. recordingAt moves the sales clock, which stamps
// the instant a sale is recorded; closeDaysAt moves the Platform Trends
// clock, which says which days are closed, and runs the close through its
// internal route. The read is taken wherever the Platform Trends clock was
// left, and setupTest puts every clock back on 2026-07-07T12:00Z.

const dailyClosePath = "/api/v1/internal/platform-trends/close"

// dailyCloseResult mirrors POST /api/v1/internal/platform-trends/close.
type dailyCloseResult struct {
	DaysFrozen     int     `json:"days_frozen"`
	FirstDayFrozen *string `json:"first_day_frozen"`
	LastDayFrozen  *string `json:"last_day_frozen"`
	ClosedThrough  string  `json:"closed_through"`

	OrganizationMarkersDeleted int `json:"organization_markers_deleted"`
	CustomerMarkersDeleted     int `json:"customer_markers_deleted"`
}

type platformTrendsActivityFigure struct {
	Recorded     bool    `json:"recorded"`
	ExactSince   *string `json:"exact_since"`
	DataSince    *string `json:"data_since"`
	SaleHalfOnly bool    `json:"sale_half_only"`

	Mean           *float64 `json:"mean"`
	ComparisonMean *float64 `json:"comparison_mean"`
	Change         *float64 `json:"change"`
	Distinct7Days  *int     `json:"distinct_7d"`
	Distinct30Days *int     `json:"distinct_30d"`

	Distinct7DaysRecorded  *int `json:"distinct_7d_days"`
	Distinct30DaysRecorded *int `json:"distinct_30d_days"`

	Buckets           []*platformTrendsActivityBucket `json:"buckets"`
	ComparisonBuckets []*platformTrendsActivityBucket `json:"comparison_buckets"`
}

type platformTrendsActivityBucket struct {
	FirstDay string  `json:"first_day"`
	LastDay  string  `json:"last_day"`
	Value    float64 `json:"value"`
}

// platformNoon is noon on a platform day, as an instant: 17:00Z.
func platformNoon(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 17, 0, 0, 0, time.UTC)
}

// recordingAt moves the sales clock, which stamps the instant a Ticket Sale
// is recorded on the platform, for the length of do, and puts the fixed
// clock back.
func recordingAt(t *testing.T, env *testEnv, at time.Time, do func()) {
	t.Helper()
	sharedApp.SalesService.WithClock(func() time.Time { return at })
	defer sharedApp.SalesService.WithClock(func() time.Time { return env.fixedClock })
	do()
}

// closeDaysAt moves the Platform Trends clock to at, where it stays until
// the test moves it again or ends, and runs the daily close through its
// internal route. It takes no credential, as no internal route does: Cloud
// Run IAM authenticates the scheduler before the request reaches the API.
func closeDaysAt(t *testing.T, env *testEnv, at time.Time) dailyCloseResult {
	t.Helper()
	sharedApp.PlatformTrendsService.WithClock(func() time.Time { return at })
	resp, body := env.post(t, dailyClosePath, nil, nil)
	if resp.StatusCode != http.StatusOK || body.Error != nil {
		t.Fatalf("daily close status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out dailyCloseResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode daily close result: %v", err)
	}
	return out
}

// readActiveOrganizations reads Platform Trends as a Platform Operator and
// returns its Active Organizations.
func readActiveOrganizations(t *testing.T, env *testEnv, query string) platformTrendsActivityFigure {
	t.Helper()
	var out struct {
		Figures struct {
			ActiveOrganizations platformTrendsActivityFigure `json:"active_organizations"`
		} `json:"figures"`
	}
	operatorGetOK(t, env, operatorSession(t, env, "operator@example.com"), platformTrendsPath+query, &out)
	return out.Figures.ActiveOrganizations
}

// sellingOrganization creates an Organization with one Event that can take
// an import, and returns what a sale for it needs.
type sellingOrganization struct {
	sessionID    string
	eventID      string
	ticketTypeID string
	slug         string
}

func newSellingOrganization(t *testing.T, env *testEnv, slug string) sellingOrganization {
	t.Helper()
	sessionID := verifyOTP(t, env, slug+"@example.com")
	createOrganization(t, env, sessionID, slug, slug)
	eventID := createDraftEvent(t, env, sessionID, slug+" fest", slug+"-fest")
	return sellingOrganization{
		sessionID:    sessionID,
		eventID:      eventID,
		ticketTypeID: createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 1000),
		slug:         slug,
	}
}

// recordImport records one imported sale for the Organization at an instant,
// saying it was sold at soldAt, and returns the batch.
func (o sellingOrganization) recordImport(t *testing.T, env *testEnv, recordedAt time.Time, soldAt string) string {
	t.Helper()
	var batchID string
	recordingAt(t, env, recordedAt, func() {
		key := fmt.Sprintf("%s-%d", o.slug, recordedAt.UnixNano())
		batchID = commitBatch(t, env, o.sessionID, o.eventID, key, []map[string]any{
			importSale(key+"@example.com", o.ticketTypeID, 1, soldAt),
		})
	})
	return batchID
}

// activityOn returns the bucket covering a day, and nil when the day is
// absent from the series.
func activityOn(figure platformTrendsActivityFigure, day string) *platformTrendsActivityBucket {
	for _, bucket := range figure.Buckets {
		if bucket != nil && bucket.FirstDay <= day && day <= bucket.LastDay {
			return bucket
		}
	}
	return nil
}

func assertActivityOn(t *testing.T, figure platformTrendsActivityFigure, day string, want float64) {
	t.Helper()
	bucket := activityOn(figure, day)
	if bucket == nil {
		t.Fatalf("%s is absent from the series, want %v", day, want)
	}
	if math.Abs(bucket.Value-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", day, bucket.Value, want)
	}
}

func assertNear(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", what, shown(got), want)
	}
}

// shown is what a nullable field holds, for a failure message.
func shown[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func assertCount(t *testing.T, what string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", what, shown(got), want)
	}
}

// TestTheDailyCloseCountsAnOrganizationOnceOnADayASaleWasRecordedForIt: one
// Organization with an Online Sale and an imported sale on the same day,
// another whose only sale that day was later reversed, and a third that
// recorded nothing. The day counts two.
func TestTheDailyCloseCountsAnOrganizationOnceOnADayASaleWasRecordedForIt(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSessionID, "Busy Fest", "busy-fest", feeTestBaseCents, 100)
	day := platformNoon(2026, 7, 5)

	recordingAt(t, env, day, func() {
		begin := beginCheckoutOK(t, env, "test-org", "busy-fest",
			checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
		confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
		commitBatch(t, env, adminSessionID, eventID, "busy-import", []map[string]any{
			importSale("bea@example.com", gaID, 1, "2026-07-05T15:00:00Z"),
		})
	})

	reversed := newSellingOrganization(t, env, "reversed-org")
	batchID := reversed.recordImport(t, env, day, "2026-07-05T16:00:00Z")
	undoBatch(t, env, reversed.sessionID, reversed.eventID, batchID)

	newSellingOrganization(t, env, "quiet-org")

	result := closeDaysAt(t, env, env.fixedClock)
	if result.DaysFrozen != 2 || shown(result.FirstDayFrozen) != "2026-07-05" || shown(result.LastDayFrozen) != "2026-07-06" || result.ClosedThrough != "2026-07-06" {
		t.Fatalf("close = %+v; want 2026-07-05 and 2026-07-06 frozen, closed through yesterday", result)
	}

	figure := readActiveOrganizations(t, env, "?span=7d")
	if !figure.Recorded || figure.ExactSince != nil || !figure.SaleHalfOnly {
		t.Fatalf("recorded=%v exact_since=%v sale_half_only=%v; want recorded, no recording-began day, the sale half only",
			figure.Recorded, shown(figure.ExactSince), figure.SaleHalfOnly)
	}
	if shown(figure.DataSince) != "2026-07-05" {
		t.Fatalf("data_since = %v, want 2026-07-05, the day of the first recorded Ticket Sale", shown(figure.DataSince))
	}
	if len(figure.Buckets) != 7 {
		t.Fatalf("%d buckets, want one entry per day of the span", len(figure.Buckets))
	}
	assertActivityOn(t, figure, "2026-07-05", 2)
	// Nobody was active on the 6th: it is frozen as a zero, not left out.
	assertActivityOn(t, figure, "2026-07-06", 0)
	// The reversal dropped the Tickets Sold of the day and not its activity.
	var trends platformTrends
	operatorGetOK(t, env, operatorSession(t, env, "operator@example.com"), platformTrendsPath+"?span=7d", &trends)
	if got := bucketOn(t, seriesFor(t, trends, "import"), "2026-07-05").Value; got != 1 {
		t.Fatalf("import Tickets Sold on 2026-07-05 = %d, want 1: the reversed sale is in no bucket", got)
	}
}

// TestActiveOrganizationsReadsThePlatformDayASaleWasRecordedOn: two sales
// recorded a second apart either side of midnight in Guayaquil land on the
// two platform days.
func TestActiveOrganizationsReadsThePlatformDayASaleWasRecordedOn(t *testing.T) {
	env := setupTest(t)
	late := newSellingOrganization(t, env, "late-org")
	early := newSellingOrganization(t, env, "early-org")
	other := newSellingOrganization(t, env, "other-org")

	// Midnight on 2026-07-05 in Guayaquil (UTC-5) is 2026-07-05T05:00Z.
	late.recordImport(t, env, time.Date(2026, 7, 5, 4, 59, 59, 0, time.UTC), "2026-07-01T12:00:00Z")
	early.recordImport(t, env, time.Date(2026, 7, 5, 5, 0, 0, 0, time.UTC), "2026-07-01T12:00:00Z")
	other.recordImport(t, env, time.Date(2026, 7, 5, 5, 0, 1, 0, time.UTC), "2026-07-01T12:00:00Z")

	closeDaysAt(t, env, env.fixedClock)
	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-04", 1)
	assertActivityOn(t, figure, "2026-07-05", 2)
}

// TestABackdatedImportIsActivityOnTheDayItWasRecorded: an old sale imported
// on the 6th is the Organization working on the 6th, while Tickets Sold
// keeps counting it on the day it says it was sold.
func TestABackdatedImportIsActivityOnTheDayItWasRecorded(t *testing.T) {
	env := setupTest(t)
	org := newSellingOrganization(t, env, "backdating-org")
	org.recordImport(t, env, platformNoon(2026, 7, 6), "2026-07-02T15:00:00Z")

	closeDaysAt(t, env, env.fixedClock)

	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-06", 1)
	if bucket := activityOn(figure, "2026-07-02"); bucket != nil {
		t.Fatalf("2026-07-02 = %+v; want absent: nothing was recorded that day and no close reaches before the first recorded sale", bucket)
	}
	if shown(figure.DataSince) != "2026-07-06" {
		t.Fatalf("data_since = %v, want 2026-07-06", shown(figure.DataSince))
	}

	var trends platformTrends
	operatorGetOK(t, env, operatorSession(t, env, "operator@example.com"), platformTrendsPath+"?span=7d", &trends)
	imported := seriesFor(t, trends, "import")
	if got := bucketOn(t, imported, "2026-07-02").Value; got != 1 {
		t.Fatalf("Tickets Sold on 2026-07-02 = %d, want the 1 ticket on its sold day", got)
	}
	if got := bucketOn(t, imported, "2026-07-06").Value; got != 0 {
		t.Fatalf("Tickets Sold on 2026-07-06 = %d, want 0: Tickets Sold does not move to the recorded day", got)
	}
}

// TestTheDailyCloseIsIdempotentAndCatchesUpMissedNights: a second run the
// same night freezes nothing, and one run after three missed nights freezes
// all three.
func TestTheDailyCloseIsIdempotentAndCatchesUpMissedNights(t *testing.T) {
	env := setupTest(t)
	org := newSellingOrganization(t, env, "steady-org")
	org.recordImport(t, env, platformNoon(2026, 7, 1), "2026-07-01T15:00:00Z")
	org.recordImport(t, env, platformNoon(2026, 7, 4), "2026-07-04T15:00:00Z")

	first := closeDaysAt(t, env, platformNoon(2026, 7, 3))
	if first.DaysFrozen != 2 || shown(first.FirstDayFrozen) != "2026-07-01" || shown(first.LastDayFrozen) != "2026-07-02" {
		t.Fatalf("first close = %+v; want 2026-07-01 and 2026-07-02 frozen", first)
	}
	before := readActiveOrganizations(t, env, "?span=7d")

	again := closeDaysAt(t, env, platformNoon(2026, 7, 3))
	if again.DaysFrozen != 0 || again.FirstDayFrozen != nil || again.LastDayFrozen != nil || again.ClosedThrough != "2026-07-02" {
		t.Fatalf("second close the same night = %+v; want nothing frozen", again)
	}
	after := readActiveOrganizations(t, env, "?span=7d")
	if fmt.Sprintf("%+v", figureValues(before)) != fmt.Sprintf("%+v", figureValues(after)) {
		t.Fatalf("a second close changed the figure:\nbefore %+v\nafter  %+v", figureValues(before), figureValues(after))
	}

	// The nights of the 4th, 5th and 6th are missed; the run on the 6th
	// freezes the 3rd, 4th and 5th.
	caughtUp := closeDaysAt(t, env, platformNoon(2026, 7, 6))
	if caughtUp.DaysFrozen != 3 || shown(caughtUp.FirstDayFrozen) != "2026-07-03" || shown(caughtUp.LastDayFrozen) != "2026-07-05" {
		t.Fatalf("catch-up close = %+v; want 2026-07-03 through 2026-07-05 frozen", caughtUp)
	}
	figure := readActiveOrganizations(t, env, "?span=custom&from=2026-07-01&to=2026-07-05")
	for day, want := range map[string]float64{"2026-07-01": 1, "2026-07-02": 0, "2026-07-03": 0, "2026-07-04": 1, "2026-07-05": 0} {
		assertActivityOn(t, figure, day, want)
	}
}

// figureValues flattens a figure's buckets for comparing two reads.
func figureValues(figure platformTrendsActivityFigure) []any {
	out := []any{shown(figure.Mean), shown(figure.Distinct7Days), shown(figure.Distinct30Days), shown(figure.DataSince)}
	for _, bucket := range figure.Buckets {
		out = append(out, shown(bucket))
	}
	return out
}

// TestAFrozenDayIsNeverRewritten: a sale that turns up for a day after the
// day was frozen changes none of that day's totals, on any later run.
func TestAFrozenDayIsNeverRewritten(t *testing.T) {
	env := setupTest(t)
	onTime := newSellingOrganization(t, env, "on-time-org")
	onTime.recordImport(t, env, platformNoon(2026, 7, 5), "2026-07-05T15:00:00Z")

	closeDaysAt(t, env, env.fixedClock)
	frozen := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, frozen, "2026-07-05", 1)

	// The sales clock is moved back to stand for a sale whose recording
	// instant falls inside a day the close has already frozen.
	tardy := newSellingOrganization(t, env, "tardy-org")
	tardy.recordImport(t, env, platformNoon(2026, 7, 5), "2026-07-05T16:00:00Z")

	if result := closeDaysAt(t, env, env.fixedClock); result.DaysFrozen != 0 {
		t.Fatalf("close after a late sale = %+v; want nothing frozen", result)
	}
	later := closeDaysAt(t, env, platformNoon(2026, 7, 8))
	if later.DaysFrozen != 1 || shown(later.FirstDayFrozen) != "2026-07-07" {
		t.Fatalf("next night's close = %+v; want 2026-07-07 alone", later)
	}

	figure := readActiveOrganizations(t, env, "?span=custom&from=2026-06-30&to=2026-07-06")
	assertActivityOn(t, figure, "2026-07-05", 1)
	assertActivityOn(t, figure, "2026-07-06", 0)
	assertCount(t, "distinct_7d as of 2026-07-06", figure.Distinct7Days, 1)
	assertCount(t, "distinct_30d as of 2026-07-06", figure.Distinct30Days, 1)
	// The first frozen day is 2026-07-05, so both runs of days ending on
	// the 6th hold two recorded days.
	assertCount(t, "distinct_7d_days as of 2026-07-06", figure.Distinct7DaysRecorded, 2)
	assertCount(t, "distinct_30d_days as of 2026-07-06", figure.Distinct30DaysRecorded, 2)
}

// TestActiveOrganizationsStatesTheMeanTheDistinctCountsAndTheComparison: the
// span figure is the mean daily count, the 7-day and 30-day figures count an
// Organization once however many days it was active, and the change is the
// span's mean against the comparison's.
func TestActiveOrganizationsStatesTheMeanTheDistinctCountsAndTheComparison(t *testing.T) {
	env := setupTest(t)
	regular := newSellingOrganization(t, env, "regular-org")
	once := newSellingOrganization(t, env, "once-org")
	earlier := newSellingOrganization(t, env, "earlier-org")

	// The history begins before the comparison span, so all of it is frozen.
	earlier.recordImport(t, env, platformNoon(2026, 6, 20), "2026-06-20T15:00:00Z")
	earlier.recordImport(t, env, platformNoon(2026, 6, 25), "2026-06-25T15:00:00Z")
	for _, day := range []int{1, 3, 5} {
		regular.recordImport(t, env, platformNoon(2026, 7, day), "2026-07-01T15:00:00Z")
	}
	once.recordImport(t, env, platformNoon(2026, 7, 3), "2026-07-03T15:00:00Z")

	closeDaysAt(t, env, env.fixedClock)
	figure := readActiveOrganizations(t, env, "?span=7d")

	// 2026-06-30 .. 2026-07-06: 0, 1, 0, 2, 0, 1, 0.
	for day, want := range map[string]float64{"2026-06-30": 0, "2026-07-01": 1, "2026-07-02": 0, "2026-07-03": 2, "2026-07-04": 0, "2026-07-05": 1, "2026-07-06": 0} {
		assertActivityOn(t, figure, day, want)
	}
	assertNear(t, "mean", figure.Mean, 4.0/7)
	assertCount(t, "distinct_7d", figure.Distinct7Days, 2)
	assertCount(t, "distinct_30d", figure.Distinct30Days, 3)

	// 2026-06-23 .. 2026-06-29 holds one active day, the 25th.
	assertNear(t, "comparison_mean", figure.ComparisonMean, 1.0/7)
	assertNear(t, "change", figure.Change, 3)
	if len(figure.ComparisonBuckets) != 7 {
		t.Fatalf("%d comparison buckets, want one per bucket of the span", len(figure.ComparisonBuckets))
	}
	third := figure.ComparisonBuckets[2]
	if third == nil || third.FirstDay != "2026-06-25" || third.LastDay != "2026-06-25" || third.Value != 1 {
		t.Fatalf("comparison bucket beside 2026-07-02 = %+v; want 2026-06-25 carrying 1", shown(third))
	}
}

// TestActiveOrganizationsStatesNoChangeAgainstAPartlyFrozenComparison: a
// comparison span that begins before the first frozen day is drawn from that
// day on, with its mean, and no change is stated against it (#826). One that
// begins on the first frozen day states its change.
func TestActiveOrganizationsStatesNoChangeAgainstAPartlyFrozenComparison(t *testing.T) {
	env := setupTest(t)
	org := newSellingOrganization(t, env, "young-org")
	// The first sale the platform ever recorded, inside 2026-06-23..2026-06-29.
	org.recordImport(t, env, platformNoon(2026, 6, 26), "2026-06-26T15:00:00Z")
	org.recordImport(t, env, platformNoon(2026, 7, 1), "2026-07-01T15:00:00Z")
	org.recordImport(t, env, platformNoon(2026, 7, 3), "2026-07-03T15:00:00Z")

	closeDaysAt(t, env, env.fixedClock)
	figure := readActiveOrganizations(t, env, "?span=7d")
	if shown(figure.DataSince) != "2026-06-26" {
		t.Fatalf("data_since = %v, want 2026-06-26", shown(figure.DataSince))
	}
	assertNear(t, "mean", figure.Mean, 2.0/7)
	// The four frozen days of the comparison span, one of them active.
	assertNear(t, "comparison_mean", figure.ComparisonMean, 1.0/4)
	for i, bucket := range figure.ComparisonBuckets {
		if wantPresent := i >= 3; (bucket != nil) != wantPresent {
			t.Fatalf("comparison bucket %d = %+v; want present=%v, the first frozen day being 2026-06-26", i, shown(bucket), wantPresent)
		}
	}
	if figure.Change != nil {
		t.Fatalf("change = %v; want none against a comparison span frozen in part", *figure.Change)
	}

	// 2026-07-01..2026-07-05 against 2026-06-26..2026-06-30, frozen whole.
	figure = readActiveOrganizations(t, env, "?span=custom&from=2026-07-01&to=2026-07-05")
	assertNear(t, "covered mean", figure.Mean, 2.0/5)
	assertNear(t, "covered comparison_mean", figure.ComparisonMean, 1.0/5)
	assertNear(t, "covered change", figure.Change, 1)
}

// TestTheDistinctCountsReachExactlySevenAndThirtyDaysBack: the 7 days ending
// on 2026-07-06 begin on 2026-06-30 and the 30 days on 2026-06-07. An
// Organization active on the first day of a run is in its count, and one
// active the day before is not.
func TestTheDistinctCountsReachExactlySevenAndThirtyDaysBack(t *testing.T) {
	env := setupTest(t)
	for slug, day := range map[string]time.Time{
		"day-31-org": platformNoon(2026, 6, 6),
		"day-30-org": platformNoon(2026, 6, 7),
		"day-8-org":  platformNoon(2026, 6, 29),
		"day-7-org":  platformNoon(2026, 6, 30),
	} {
		// The Members setting these up and importing are working, which is
		// activity too (#816). They do it on 2026-06-01, before either run
		// of days begins, so the counts below are the recorded sales alone.
		workingAt(t, env, platformNoon(2026, 6, 1), func() {
			newSellingOrganization(t, env, slug).recordImport(t, env, day, "2026-06-01T15:00:00Z")
		})
	}

	closeDaysAt(t, env, env.fixedClock)
	figure := readActiveOrganizations(t, env, "?span=7d")
	assertCount(t, "distinct_7d", figure.Distinct7Days, 1)
	assertCount(t, "distinct_30d", figure.Distinct30Days, 3)
	// Every day from 2026-06-01 on is frozen: both runs are recorded whole.
	assertCount(t, "distinct_7d_days", figure.Distinct7DaysRecorded, 7)
	assertCount(t, "distinct_30d_days", figure.Distinct30DaysRecorded, 30)

	// A night closed on its own reads the same thirty days as a catch-up.
	sharedApp.SalesService.WithClock(func() time.Time { return env.fixedClock })
	closeDaysAt(t, env, platformNoon(2026, 7, 8))
	figure = readActiveOrganizations(t, env, "?span=7d")
	assertCount(t, "distinct_7d as of 2026-07-07", figure.Distinct7Days, 0)
	assertCount(t, "distinct_30d as of 2026-07-07", figure.Distinct30Days, 2)
}

// TestTheFirstDailyCloseFreezesEveryDayBackToTheFirstRecordedSale, and a
// weekly bucket is the mean of its frozen days: the week holding the first
// day starts on it, and the weeks before it are absent.
func TestTheFirstDailyCloseFreezesEveryDayBackToTheFirstRecordedSale(t *testing.T) {
	env := setupTest(t)
	org := newSellingOrganization(t, env, "old-org")
	// A Sunday. The yearly span's weeks run Tuesday to Monday, so its week
	// is 2026-02-24 .. 2026-03-02 and holds two frozen days.
	org.recordImport(t, env, platformNoon(2026, 3, 1), "2026-03-01T15:00:00Z")
	// Every day of the week ending 2026-07-06 but the last.
	for day := 30; day <= 35; day++ {
		org.recordImport(t, env, platformNoon(2026, 6, day), "2026-06-30T15:00:00Z")
	}

	result := closeDaysAt(t, env, env.fixedClock)
	// March 1st through July 6th: 31 + 30 + 31 + 30 + 6 days.
	if result.DaysFrozen != 128 || shown(result.FirstDayFrozen) != "2026-03-01" || shown(result.LastDayFrozen) != "2026-07-06" {
		t.Fatalf("first close = %+v; want the 128 days from 2026-03-01 through 2026-07-06", result)
	}

	figure := readActiveOrganizations(t, env, "?span=12m")
	if len(figure.Buckets) != 52 {
		t.Fatalf("%d buckets, want 52 weeks", len(figure.Buckets))
	}
	if bucket := activityOn(figure, "2026-02-23"); bucket != nil {
		t.Fatalf("the week before the first recorded sale = %+v; want absent", bucket)
	}
	first := activityOn(figure, "2026-03-01")
	if first == nil || first.FirstDay != "2026-03-01" || first.LastDay != "2026-03-02" || first.Value != 0.5 {
		t.Fatalf("the week holding 2026-03-01 = %+v; want 2026-03-01..2026-03-02 with a mean of 0.5", shown(first))
	}
	last := figure.Buckets[51]
	if last == nil || last.FirstDay != "2026-06-30" || last.LastDay != "2026-07-06" || math.Abs(last.Value-6.0/7) > 1e-9 {
		t.Fatalf("the last week = %+v; want 2026-06-30..2026-07-06 with a mean of 6/7", shown(last))
	}
	// Seven active days over the 128 frozen ones.
	assertNear(t, "mean", figure.Mean, 7.0/128)
	assertCount(t, "distinct_7d", figure.Distinct7Days, 1)
}

// TestADayTheCloseHasNotFrozenIsAbsentNotZero: the days the close has not
// reached are left out of the series and of the mean, and the distinct
// counts, which are as of the span's last day, are not stated.
func TestADayTheCloseHasNotFrozenIsAbsentNotZero(t *testing.T) {
	env := setupTest(t)

	// No close has ever run and nothing was ever sold.
	empty := readActiveOrganizations(t, env, "?span=7d")
	if !empty.Recorded || empty.DataSince != nil || empty.Mean != nil || empty.ComparisonMean != nil || empty.Change != nil ||
		empty.Distinct7Days != nil || empty.Distinct30Days != nil || empty.Distinct7DaysRecorded != nil || empty.Distinct30DaysRecorded != nil {
		t.Fatalf("with nothing frozen the figure = %+v; want recorded, and nothing stated", figureValues(empty))
	}
	if len(empty.Buckets) != 7 || len(empty.ComparisonBuckets) != 7 {
		t.Fatalf("buckets = %d / %d, want 7 entries each", len(empty.Buckets), len(empty.ComparisonBuckets))
	}
	for i := range empty.Buckets {
		if empty.Buckets[i] != nil || empty.ComparisonBuckets[i] != nil {
			t.Fatalf("entry %d = %+v / %+v; want absent", i, shown(empty.Buckets[i]), shown(empty.ComparisonBuckets[i]))
		}
	}

	org := newSellingOrganization(t, env, "waiting-org")
	org.recordImport(t, env, platformNoon(2026, 7, 1), "2026-07-01T15:00:00Z")
	org.recordImport(t, env, platformNoon(2026, 7, 5), "2026-07-05T15:00:00Z")

	// The close last ran on the 4th; the reading is taken on the 7th.
	closeDaysAt(t, env, platformNoon(2026, 7, 4))
	sharedApp.PlatformTrendsService.WithClock(func() time.Time { return env.fixedClock })

	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-01", 1)
	assertActivityOn(t, figure, "2026-07-02", 0)
	assertActivityOn(t, figure, "2026-07-03", 0)
	for _, day := range []string{"2026-06-30", "2026-07-04", "2026-07-05", "2026-07-06"} {
		if bucket := activityOn(figure, day); bucket != nil {
			t.Fatalf("%s = %+v; want absent: the close has not frozen it", day, bucket)
		}
	}
	assertNear(t, "mean over the three frozen days", figure.Mean, 1.0/3)
	if figure.Distinct7Days != nil || figure.Distinct30Days != nil || figure.Distinct7DaysRecorded != nil || figure.Distinct30DaysRecorded != nil {
		t.Fatalf("distinct counts = %v / %v over %v / %v days; want none while the span's last day is not frozen",
			shown(figure.Distinct7Days), shown(figure.Distinct30Days), shown(figure.Distinct7DaysRecorded), shown(figure.Distinct30DaysRecorded))
	}
	if figure.ComparisonMean != nil || figure.Change != nil {
		t.Fatalf("comparison_mean=%v change=%v; want none: the comparison span has no frozen day",
			shown(figure.ComparisonMean), shown(figure.Change))
	}
}

// A daily close that cannot reach its database answers 500 and checks in
// failed under its monitor's name (ADR 0079).
func TestADailyCloseThatFailsChecksInFailed(t *testing.T) {
	setupTest(t)
	const job = "ticket-pos-daily-close"
	fake := reportertest.New()
	var now atomic.Int64
	now.Store(fixedClock.UnixNano())
	reporting, app := startAppReportingTo(t, fake, &now)
	app.DB.Pool.Close()

	resp, _ := reporting.post(t, dailyClosePath, nil, nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	checkIns := fake.CheckIns()
	if len(checkIns) != 2 || checkIns[0] != (reportertest.CheckIn{Job: job, Status: platform.CheckInInProgress}) ||
		checkIns[1].Job != job || checkIns[1].Status != platform.CheckInFailed {
		t.Fatalf("check-ins = %+v, want %s started then failed", checkIns, job)
	}
}
