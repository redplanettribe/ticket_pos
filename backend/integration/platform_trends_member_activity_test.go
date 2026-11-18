package integration

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	platformtrendssvc "github.com/peter/ticket_pos/backend/internal/platformtrends/service"
)

// The Member half of Active Organization (#816, spec #808, ADR 0080). An
// Organization is active on a platform day a Member worked in it through a
// Staff Session, sale or no sale: the request that loads the Active Member
// leaves one marker for the Organization and the day, and the daily close
// counts it beside the Organizations a Ticket Sale was recorded for.
//
// Time is moved explicitly. workingAt moves the identity clock, which is the
// clock a Staff Session is authenticated on and so the one a marker's day is
// read from; closeDaysAt moves the Platform Trends clock and runs the close.
// A Member "works" here by loading GET /api/v1/staff/me, the read every
// staff page makes, and only ever inside workingAt, so each test says on
// which days anybody worked.

// workingAt moves the identity clock for the length of do, and puts the
// fixed clock back.
func workingAt(t *testing.T, env *testEnv, at time.Time, do func()) {
	t.Helper()
	sharedApp.IdentityService.WithClock(func() time.Time { return at })
	defer sharedApp.IdentityService.WithClock(func() time.Time { return env.fixedClock })
	do()
}

// loadStaffPage makes the read every staff page makes, as the Member the
// session belongs to, and returns the status it was answered with.
func loadStaffPage(t *testing.T, env *testEnv, sessionID string) int {
	t.Helper()
	resp, _ := env.get(t, staffMePath, authHeader(sessionID))
	return resp.StatusCode
}

// workOn has the session's Member load a staff page at an instant.
func workOn(t *testing.T, env *testEnv, sessionID string, at time.Time) {
	t.Helper()
	workingAt(t, env, at, func() {
		if status := loadStaffPage(t, env, sessionID); status != http.StatusOK {
			t.Fatalf("GET %s at %s status=%d, want 200", staffMePath, at.Format(time.RFC3339), status)
		}
	})
}

// newOrganizationWithMember creates an Organization and returns the Staff
// Session of its Org Admin, who is its Active Member. Creating it loads no
// Active Member, so it marks no day.
func newOrganizationWithMember(t *testing.T, env *testEnv, slug string) string {
	t.Helper()
	sessionID := verifyOTP(t, env, slug+"@example.com")
	createOrganization(t, env, sessionID, slug, slug)
	return sessionID
}

// markerWrites stands where the Organization marker store stands and counts
// the writes that reach it, failing them while failing is set. Everything
// else is the real store.
type markerWrites struct {
	platformtrendssvc.OrganizationMarkerStore
	writes  atomic.Int64
	failing atomic.Bool
}

func (m *markerWrites) MarkOrganizationActive(ctx context.Context, organizationID, day string) error {
	m.writes.Add(1)
	if m.failing.Load() {
		return errors.New("the marker store is down")
	}
	return m.OrganizationMarkerStore.MarkOrganizationActive(ctx, organizationID, day)
}

// watchMarkerWrites puts a counting store in front of the real one until the
// next setupTest, which puts the real one back.
func watchMarkerWrites(t *testing.T) *markerWrites {
	t.Helper()
	watched := &markerWrites{OrganizationMarkerStore: organizationMarkerStore()}
	sharedApp.PlatformTrendsService.WithOrganizationMarkerStore(watched)
	return watched
}

// TestAMemberWorkingMakesTheirOrganizationActiveOnADayWithNoSale: nothing
// has ever been sold on the platform. The Organization is active on the two
// days its Member loaded a staff page, the read names the first of them as
// the day exact recording began, and the figure is no longer the sale half
// alone.
func TestAMemberWorkingMakesTheirOrganizationActiveOnADayWithNoSale(t *testing.T) {
	env := setupTest(t)
	member := newOrganizationWithMember(t, env, "preparing-org")
	newOrganizationWithMember(t, env, "absent-org")

	workOn(t, env, member, platformNoon(2026, 7, 3))
	workOn(t, env, member, platformNoon(2026, 7, 5))

	before := readActiveOrganizations(t, env, "?span=7d")
	if before.DataSince != nil || before.ExactSince != nil || !before.SaleHalfOnly {
		t.Fatalf("before any close data_since=%v exact_since=%v sale_half_only=%v; want nothing stated: markers are read by the close alone",
			shown(before.DataSince), shown(before.ExactSince), before.SaleHalfOnly)
	}

	result := closeDaysAt(t, env, env.fixedClock)
	if result.DaysFrozen != 4 || shown(result.FirstDayFrozen) != "2026-07-03" || shown(result.LastDayFrozen) != "2026-07-06" {
		t.Fatalf("close = %+v; want the 4 days from the first marker, 2026-07-03, through 2026-07-06", result)
	}

	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-03", 1)
	assertActivityOn(t, figure, "2026-07-04", 0)
	assertActivityOn(t, figure, "2026-07-05", 1)
	assertActivityOn(t, figure, "2026-07-06", 0)
	if shown(figure.ExactSince) != "2026-07-03" || figure.SaleHalfOnly || !figure.Recorded {
		t.Fatalf("exact_since=%v sale_half_only=%v recorded=%v; want exact since 2026-07-03, both halves, recorded",
			shown(figure.ExactSince), figure.SaleHalfOnly, figure.Recorded)
	}
	if shown(figure.DataSince) != "2026-07-03" {
		t.Fatalf("data_since = %v, want 2026-07-03", shown(figure.DataSince))
	}
	assertCount(t, "distinct_7d", figure.Distinct7Days, 1)
	assertCount(t, "distinct_30d", figure.Distinct30Days, 1)
}

// TestAnOrganizationActiveByASaleAndByAMemberIsCountedOnce, in the day's
// count and in the 7-day and 30-day counts, beside one active by a Member
// alone and one by a sale alone.
func TestAnOrganizationActiveByASaleAndByAMemberIsCountedOnce(t *testing.T) {
	env := setupTest(t)
	both := newSellingOrganization(t, env, "both-org")
	selling := newSellingOrganization(t, env, "selling-org")
	working := newOrganizationWithMember(t, env, "working-org")

	// newSellingOrganization worked on the fixed clock's day, the 7th, which
	// is today and in no frozen day. The imports below are recorded on that
	// day's identity clock too; only the sales clock says the 5th and 6th.
	both.recordImport(t, env, platformNoon(2026, 7, 5), "2026-07-05T15:00:00Z")
	workOn(t, env, both.sessionID, platformNoon(2026, 7, 5))
	workOn(t, env, working, platformNoon(2026, 7, 5))
	selling.recordImport(t, env, platformNoon(2026, 7, 6), "2026-07-06T15:00:00Z")
	workOn(t, env, both.sessionID, platformNoon(2026, 7, 6))

	closeDaysAt(t, env, env.fixedClock)

	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-05", 2)
	assertActivityOn(t, figure, "2026-07-06", 2)
	assertCount(t, "distinct_7d", figure.Distinct7Days, 3)
	assertCount(t, "distinct_30d", figure.Distinct30Days, 3)
}

// TestAPlatformOperatorOnOperatorRoutesMarksNoOrganization: the operator is
// a Member of an Organization and their Staff Session carries that Active
// Member, as it does in Platform mode. A day spent on the Operator Dashboard
// leaves the Organization inactive; the same session loading a staff page
// the day after marks it.
func TestAPlatformOperatorOnOperatorRoutesMarksNoOrganization(t *testing.T) {
	env := setupTest(t)
	other := newOrganizationWithMember(t, env, "other-org")
	workOn(t, env, other, platformNoon(2026, 7, 3))

	operator := newOrganizationWithMember(t, env, "operator-org")
	seedPlatformOperator(t, env, "operator-org@example.com")

	workingAt(t, env, platformNoon(2026, 7, 4), func() {
		var trends platformTrends
		operatorGetOK(t, env, operator, platformTrendsPath+"?span=7d", &trends)
		operatorOrganizations(t, env, operator)
	})
	workOn(t, env, operator, platformNoon(2026, 7, 5))

	closeDaysAt(t, env, env.fixedClock)

	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-03", 1)
	assertActivityOn(t, figure, "2026-07-04", 0)
	assertActivityOn(t, figure, "2026-07-05", 1)
}

// TestManyRequestsInADayWriteOneMarker: after a Member's first request of a
// platform day no further write reaches the marker store, whichever Member
// of the Organization asks, and the first request of the next day is one
// more. The day counts the Organization once.
func TestManyRequestsInADayWriteOneMarker(t *testing.T) {
	env := setupTest(t)
	admin := newOrganizationWithMember(t, env, "busy-org")
	colleague := addMemberSession(t, env, admin, "colleague@example.com", "event_owner")
	writes := watchMarkerWrites(t)

	for i := 0; i < 20; i++ {
		workOn(t, env, admin, platformNoon(2026, 7, 5).Add(time.Duration(i)*time.Minute))
		workOn(t, env, colleague, platformNoon(2026, 7, 5).Add(time.Duration(i)*time.Minute))
	}
	if got := writes.writes.Load(); got != 1 {
		t.Fatalf("%d marker writes after 40 requests in one day, want 1", got)
	}

	workOn(t, env, admin, platformNoon(2026, 7, 6))
	workOn(t, env, admin, platformNoon(2026, 7, 6).Add(time.Hour))
	if got := writes.writes.Load(); got != 2 {
		t.Fatalf("%d marker writes after the next day's requests, want 2", got)
	}

	closeDaysAt(t, env, env.fixedClock)
	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-05", 1)
	assertActivityOn(t, figure, "2026-07-06", 1)
}

// TestAMarkerStoreThatErrorsDoesNotFailTheStaffRequest: the request is
// answered as if nothing were wrong, and since the write did not land the
// next request of the day tries it again.
func TestAMarkerStoreThatErrorsDoesNotFailTheStaffRequest(t *testing.T) {
	env := setupTest(t)
	member := newOrganizationWithMember(t, env, "unlucky-org")
	writes := watchMarkerWrites(t)

	writes.failing.Store(true)
	workOn(t, env, member, platformNoon(2026, 7, 5))
	if got := writes.writes.Load(); got != 1 {
		t.Fatalf("%d marker writes, want the 1 that failed", got)
	}

	writes.failing.Store(false)
	workOn(t, env, member, platformNoon(2026, 7, 5).Add(time.Minute))
	workOn(t, env, member, platformNoon(2026, 7, 5).Add(2*time.Minute))
	if got := writes.writes.Load(); got != 2 {
		t.Fatalf("%d marker writes, want 2: the failed one, and one retry that landed", got)
	}

	closeDaysAt(t, env, env.fixedClock)
	assertActivityOn(t, readActiveOrganizations(t, env, "?span=7d"), "2026-07-05", 1)
}

// TestAMarkerBelongsToThePlatformDayTheMemberWorkedOn: two Members working a
// second apart either side of midnight in Guayaquil mark the two platform
// days.
func TestAMarkerBelongsToThePlatformDayTheMemberWorkedOn(t *testing.T) {
	env := setupTest(t)
	late := newOrganizationWithMember(t, env, "late-org")
	early := newOrganizationWithMember(t, env, "early-org")
	other := newOrganizationWithMember(t, env, "other-org")

	// Midnight on 2026-07-05 in Guayaquil (UTC-5) is 2026-07-05T05:00Z.
	workOn(t, env, late, time.Date(2026, 7, 5, 4, 59, 59, 0, time.UTC))
	workOn(t, env, early, time.Date(2026, 7, 5, 5, 0, 0, 0, time.UTC))
	workOn(t, env, other, time.Date(2026, 7, 5, 5, 0, 1, 0, time.UTC))

	closeDaysAt(t, env, env.fixedClock)
	figure := readActiveOrganizations(t, env, "?span=7d")
	assertActivityOn(t, figure, "2026-07-04", 1)
	assertActivityOn(t, figure, "2026-07-05", 2)
}

// TestADayFrozenBeforeMarkersBeganStaysTheSaleHalfOnly: the days closed
// before any Member was recorded keep their figures, a marker that turns up
// for one of them afterwards changes nothing, and the read names the first
// day frozen with markers as the day exact recording began. A later run
// does not move that day.
func TestADayFrozenBeforeMarkersBeganStaysTheSaleHalfOnly(t *testing.T) {
	env := setupTest(t)
	// Setting the Organization up is a Member working on the fixed clock's
	// day, the 7th, which no close below freezes before markers are counted.
	org := newSellingOrganization(t, env, "old-org")
	sessionID := org.sessionID
	org.recordImport(t, env, platformNoon(2026, 7, 1), "2026-07-01T15:00:00Z")

	first := closeDaysAt(t, env, platformNoon(2026, 7, 3))
	if first.DaysFrozen != 2 {
		t.Fatalf("first close = %+v; want 2026-07-01 and 2026-07-02 frozen", first)
	}
	sharedApp.PlatformTrendsService.WithClock(func() time.Time { return env.fixedClock })
	saleHalf := readActiveOrganizations(t, env, "?span=7d")
	if saleHalf.ExactSince != nil || !saleHalf.SaleHalfOnly {
		t.Fatalf("exact_since=%v sale_half_only=%v; want none and true: no day was frozen with a marker",
			shown(saleHalf.ExactSince), saleHalf.SaleHalfOnly)
	}
	assertActivityOn(t, saleHalf, "2026-07-01", 1)
	assertActivityOn(t, saleHalf, "2026-07-02", 0)

	// A marker for the 2nd, a day already frozen, and one for the 4th.
	workOn(t, env, sessionID, platformNoon(2026, 7, 2))
	workOn(t, env, sessionID, platformNoon(2026, 7, 4))

	closeDaysAt(t, env, env.fixedClock)
	figure := readActiveOrganizations(t, env, "?span=7d")
	if shown(figure.ExactSince) != "2026-07-04" || figure.SaleHalfOnly {
		t.Fatalf("exact_since=%v sale_half_only=%v; want 2026-07-04, the first day frozen with markers, and false",
			shown(figure.ExactSince), figure.SaleHalfOnly)
	}
	for _, day := range []string{"2026-07-01", "2026-07-02"} {
		if got, want := activityOn(figure, day), activityOn(saleHalf, day); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %+v after the later close, want it as it was frozen: %+v", day, shown(got), shown(want))
		}
	}
	assertActivityOn(t, figure, "2026-07-03", 0)
	assertActivityOn(t, figure, "2026-07-04", 1)

	// A Member working later, and the closes after it, leave the day alone.
	workOn(t, env, sessionID, platformNoon(2026, 7, 8))
	closeDaysAt(t, env, platformNoon(2026, 7, 10))
	if later := readActiveOrganizations(t, env, "?span=7d"); shown(later.ExactSince) != "2026-07-04" {
		t.Fatalf("exact_since = %v after a later close, want it still 2026-07-04", shown(later.ExactSince))
	}
}

// TestTheDailyCloseDeletesMarkersOlderThanThirteenMonths, and the frozen
// figures of the days they marked read exactly as they did. A marker
// thirteen months old to the day is kept one more night.
func TestTheDailyCloseDeletesMarkersOlderThanThirteenMonths(t *testing.T) {
	env := setupTest(t)
	member := newOrganizationWithMember(t, env, "long-ago-org")
	workOn(t, env, member, platformNoon(2026, 7, 3))
	workOn(t, env, member, platformNoon(2026, 7, 4))

	result := closeDaysAt(t, env, env.fixedClock)
	if result.OrganizationMarkersDeleted != 0 {
		t.Fatalf("%d markers deleted on 2026-07-07, want none: the oldest is four days old", result.OrganizationMarkersDeleted)
	}
	const span = "?span=custom&from=2026-07-01&to=2026-07-06"
	frozen := readActiveOrganizations(t, env, span)
	assertActivityOn(t, frozen, "2026-07-03", 1)
	assertActivityOn(t, frozen, "2026-07-04", 1)

	// Thirteen months before 2027-08-04 is 2026-07-04: the marker of the
	// 3rd is older than that and the marker of the 4th is not.
	if got := closeDaysAt(t, env, platformNoon(2027, 8, 4)).OrganizationMarkersDeleted; got != 1 {
		t.Fatalf("%d markers deleted on 2027-08-04, want 1: the marker of 2026-07-03", got)
	}
	if got := closeDaysAt(t, env, platformNoon(2027, 8, 4)).OrganizationMarkersDeleted; got != 0 {
		t.Fatalf("%d markers deleted by a second run the same night, want none", got)
	}
	if got := closeDaysAt(t, env, platformNoon(2027, 8, 5)).OrganizationMarkersDeleted; got != 1 {
		t.Fatalf("%d markers deleted on 2027-08-05, want 1: the marker of 2026-07-04", got)
	}

	after := readActiveOrganizations(t, env, span)
	if !reflect.DeepEqual(after.Buckets, frozen.Buckets) || !reflect.DeepEqual(after.Mean, frozen.Mean) ||
		!reflect.DeepEqual(after.Distinct7Days, frozen.Distinct7Days) || !reflect.DeepEqual(after.Distinct30Days, frozen.Distinct30Days) {
		t.Fatalf("the frozen days read %+v after their markers were deleted, want them as before: %+v", figureValues(after), figureValues(frozen))
	}
	if shown(after.ExactSince) != "2026-07-03" {
		t.Fatalf("exact_since = %v after the purge, want it still 2026-07-03", shown(after.ExactSince))
	}
}
