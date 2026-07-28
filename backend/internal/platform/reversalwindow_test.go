package platform_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Reversal Window is the one piece of this feature that cannot be checked by
// looking at it: it mixes two clocks that only agree by accident. The 20:00
// cutoff is Ecuadorian wall-clock time and is the same instant for every Event on
// the platform; the Event's start is an instant recorded against the Event's own
// IANA timezone, which may be anywhere. Conflating them is the defect these tests
// exist to prevent, so several of them pick times that would pass under the wrong
// reading and fail under the right one.
//
// Every expectation below is written in UTC. America/Guayaquil is UTC-5 with no
// DST, so 20:00 there is always 01:00 UTC the following day — an arithmetic fact
// the cases lean on rather than restate.

// mustParse is a UTC instant, spelled the way the assertions read.
func mustParse(t *testing.T, value string) time.Time {
	t.Helper()
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return instant
}

func TestEcuadorTimeZoneIsGuayaquil(t *testing.T) {
	t.Parallel()

	// The constant is the platform's own wall clock, and the whole feature is
	// stated in it. Renaming the zone would silently move every deadline.
	if platform.EcuadorTimeZone != "America/Guayaquil" {
		t.Fatalf("EcuadorTimeZone = %q, want America/Guayaquil", platform.EcuadorTimeZone)
	}
}

// TestReversalWindowClosesAtTheEcuadorCutoffWhenTheEventIsFarOff is the ordinary
// case: somebody buys in the morning for a show weeks away, so nothing but the
// 20:00 cutoff can end their window.
func TestReversalWindowClosesAtTheEcuadorCutoffWhenTheEventIsFarOff(t *testing.T) {
	t.Parallel()

	approvedAt := mustParse(t, "2026-07-07T14:00:00Z") // 09:00 in Ecuador
	eventStart := mustParse(t, "2026-08-20T01:00:00Z")

	window := platform.NewReversalWindow(approvedAt, eventStart)

	want := mustParse(t, "2026-07-08T01:00:00Z") // 20:00 on 7 July in Ecuador
	if !window.ClosesAt.Equal(want) {
		t.Fatalf("ClosesAt = %s, want %s", window.ClosesAt.UTC().Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if !window.OpensAt.Equal(approvedAt) {
		t.Fatalf("OpensAt = %s, want the Payment approval instant %s",
			window.OpensAt.UTC().Format(time.RFC3339), approvedAt.Format(time.RFC3339))
	}
	if !window.IsOpenAt(mustParse(t, "2026-07-07T16:30:00Z")) {
		t.Fatal("a sale bought this morning is not reversible this afternoon, and it should be")
	}
}

// TestReversalWindowIsClosedOnceTheEcuadorCutoffPasses walks the boundary: open
// up to the last instant before 20:00 Ecuador time, closed at 20:00 exactly, and
// closed for good afterwards. The cutoff is an instant the window runs *until*,
// not one it includes.
func TestReversalWindowIsClosedOnceTheEcuadorCutoffPasses(t *testing.T) {
	t.Parallel()

	approvedAt := mustParse(t, "2026-07-07T14:00:00Z")
	eventStart := mustParse(t, "2026-08-20T01:00:00Z")
	window := platform.NewReversalWindow(approvedAt, eventStart)

	cases := []struct {
		name string
		now  string
		open bool
	}{
		{"a minute before the cutoff", "2026-07-08T00:59:00Z", true},
		{"at the cutoff", "2026-07-08T01:00:00Z", false},
		{"a minute after the cutoff", "2026-07-08T01:01:00Z", false},
		{"the next morning", "2026-07-08T14:00:00Z", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := window.IsOpenAt(mustParse(t, tc.now)); got != tc.open {
				t.Fatalf("IsOpenAt(%s) = %v, want %v", tc.now, got, tc.open)
			}
		})
	}
}

// TestReversalWindowEndsAtEventStartWhenTheEventIsBeforeTheCutoff is the
// same-day show: a matinee starting at 15:00 Ecuador time ends the window five
// hours before the cutoff would have. The platform holds no record of
// attendance, so the window cannot outlive the doors opening.
func TestReversalWindowEndsAtEventStartWhenTheEventIsBeforeTheCutoff(t *testing.T) {
	t.Parallel()

	approvedAt := mustParse(t, "2026-07-07T13:00:00Z") // 08:00 in Ecuador
	eventStart := mustParse(t, "2026-07-07T20:00:00Z") // 15:00 in Ecuador, same day

	window := platform.NewReversalWindow(approvedAt, eventStart)

	if !window.ClosesAt.Equal(eventStart) {
		t.Fatalf("ClosesAt = %s, want the Event start %s",
			window.ClosesAt.UTC().Format(time.RFC3339), eventStart.Format(time.RFC3339))
	}
	if !window.IsOpenAt(mustParse(t, "2026-07-07T19:59:00Z")) {
		t.Fatal("the window must still be open a minute before the doors")
	}
	if window.IsOpenAt(mustParse(t, "2026-07-07T20:00:01Z")) {
		t.Fatal("the window must be shut once the Event has started, cutoff or no cutoff")
	}
}

// TestReversalWindowNeverOpensForAPurchaseAfterTheCutoff is the case with no
// window at all. Somebody buying at 20:30 Ecuador time has already missed the
// only cutoff their purchase date offers, and the next day's 20:00 is not theirs.
// The window is not "closed later" — it never opens.
func TestReversalWindowNeverOpensForAPurchaseAfterTheCutoff(t *testing.T) {
	t.Parallel()

	approvedAt := mustParse(t, "2026-07-08T01:30:00Z") // 20:30 on 7 July in Ecuador
	eventStart := mustParse(t, "2026-08-20T01:00:00Z")

	window := platform.NewReversalWindow(approvedAt, eventStart)

	for _, now := range []string{
		"2026-07-08T01:30:00Z", // the instant of purchase
		"2026-07-08T01:31:00Z",
		"2026-07-08T14:00:00Z", // the following morning, before the next 20:00
		"2026-07-09T00:59:00Z", // a minute before the NEXT day's cutoff
	} {
		if window.IsOpenAt(mustParse(t, now)) {
			t.Fatalf("IsOpenAt(%s) = true; a purchase made after 20:00 never has a window", now)
		}
	}
	if !window.ClosesAt.Before(window.OpensAt) {
		t.Fatalf("ClosesAt %s is not before OpensAt %s; the window should be empty",
			window.ClosesAt.UTC().Format(time.RFC3339), window.OpensAt.UTC().Format(time.RFC3339))
	}
}

// TestReversalWindowUsesEcuadorTimeNotTheEventsTimezone is the test this ticket
// exists for.
//
// The Event is in Tokyo — fourteen hours from Ecuador, and a calendar day ahead
// for most of the day — and it starts at 20:00 *Tokyo* time. If the cutoff were
// computed in the Event's own zone, the two 20:00s would look interchangeable and
// the window would close at 2026-07-07T11:00Z. It closes at the Ecuadorian 20:00,
// 2026-07-08T01:00Z, because the cutoff is a platform rule that knows nothing
// about where the Event is.
func TestReversalWindowUsesEcuadorTimeNotTheEventsTimezone(t *testing.T) {
	t.Parallel()

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}

	approvedAt := mustParse(t, "2026-07-07T14:00:00Z") // 09:00 on 7 July in Ecuador; 23:00 in Tokyo
	// 20:00 on 8 July in Tokyo — the same wall-clock hour as the cutoff, in a
	// different zone, and deliberately so.
	eventStart := time.Date(2026, 7, 8, 20, 0, 0, 0, tokyo)

	window := platform.NewReversalWindow(approvedAt, eventStart)

	want := mustParse(t, "2026-07-08T01:00:00Z")
	if !window.ClosesAt.Equal(want) {
		t.Fatalf("ClosesAt = %s, want the Ecuadorian cutoff %s — the Event's 20:00 is not the platform's",
			window.ClosesAt.UTC().Format(time.RFC3339), want.Format(time.RFC3339))
	}
	// The wrong reading — 20:00 in Tokyo on the purchase date — would land here.
	if wrong := mustParse(t, "2026-07-07T11:00:00Z"); window.ClosesAt.Equal(wrong) {
		t.Fatal("ClosesAt was computed in the Event's timezone; the cutoff is Ecuadorian and only Ecuadorian")
	}
	if !window.IsOpenAt(mustParse(t, "2026-07-07T23:00:00Z")) {
		t.Fatal("the window must still be open at 18:00 Ecuador time, whatever date it is in Tokyo")
	}
}

// TestReversalWindowPurchaseDateIsEcuadorsNotTheEventsProves the *other* half of
// the same confusion: which calendar date the cutoff is anchored to.
//
// The purchase lands at 22:00 on 7 July in Ecuador — past the cutoff, so there is
// no window — while in Tokyo it is already midday on 8 July. Anchoring the cutoff
// to the Event's date would hand this buyer a window running to 8 July's 20:00.
func TestReversalWindowPurchaseDateIsEcuadorsNotTheEvents(t *testing.T) {
	t.Parallel()

	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatalf("load Pacific/Auckland: %v", err)
	}

	approvedAt := mustParse(t, "2026-07-08T03:00:00Z") // 22:00 on 7 July in Ecuador
	eventStart := time.Date(2026, 7, 20, 19, 30, 0, 0, auckland)

	window := platform.NewReversalWindow(approvedAt, eventStart)

	if window.IsOpenAt(approvedAt) {
		t.Fatal("a purchase at 22:00 Ecuador time has no window, whatever the date is at the venue")
	}
	if window.IsOpenAt(mustParse(t, "2026-07-08T14:00:00Z")) {
		t.Fatal("the window reopened on the Event's calendar date; the purchase date is Ecuador's")
	}
}

// TestReversalWindowCutoffIsStableAcrossTheYear pins the no-DST assumption the
// arithmetic above rests on. Ecuador does not observe daylight saving, so the
// cutoff is 01:00 UTC the next day in January exactly as in July. A future
// tzdata change here would break the feature quietly, and this is where it
// would be caught.
func TestReversalWindowCutoffIsStableAcrossTheYear(t *testing.T) {
	t.Parallel()

	eventStart := mustParse(t, "2027-12-31T00:00:00Z")
	cases := []struct{ approvedAt, closesAt string }{
		{"2026-01-15T14:00:00Z", "2026-01-16T01:00:00Z"},
		{"2026-07-15T14:00:00Z", "2026-07-16T01:00:00Z"},
		{"2026-11-02T18:30:00Z", "2026-11-03T01:00:00Z"},
	}
	for _, tc := range cases {
		window := platform.NewReversalWindow(mustParse(t, tc.approvedAt), eventStart)
		if want := mustParse(t, tc.closesAt); !window.ClosesAt.Equal(want) {
			t.Fatalf("approved %s: ClosesAt = %s, want %s",
				tc.approvedAt, window.ClosesAt.UTC().Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
}

// TestReversalWindowIsNotOpenBeforeItsPayment: the window opens when the Payment
// is approved and not a moment sooner, so a clock that has not reached the
// purchase yet reports nothing reversible.
func TestReversalWindowIsNotOpenBeforeItsPayment(t *testing.T) {
	t.Parallel()

	approvedAt := mustParse(t, "2026-07-07T14:00:00Z")
	window := platform.NewReversalWindow(approvedAt, mustParse(t, "2026-08-20T01:00:00Z"))

	if window.IsOpenAt(mustParse(t, "2026-07-07T13:59:59Z")) {
		t.Fatal("the window is open before the Payment was approved")
	}
	if !window.IsOpenAt(approvedAt) {
		t.Fatal("the window is not open at the instant of approval; it opens there")
	}
}

// TestReversalWindowIgnoresTheZoneItsInputsArriveIn: the computation reads
// instants, so the same two moments expressed in any location produce the same
// window. Callers hand it whatever the database and the driver produced.
func TestReversalWindowIgnoresTheZoneItsInputsArriveIn(t *testing.T) {
	t.Parallel()

	utc := platform.NewReversalWindow(
		mustParse(t, "2026-07-07T14:00:00Z"),
		mustParse(t, "2026-08-20T01:00:00Z"),
	)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}
	shifted := platform.NewReversalWindow(
		mustParse(t, "2026-07-07T14:00:00Z").In(tokyo),
		mustParse(t, "2026-08-20T01:00:00Z").In(tokyo),
	)

	if !utc.ClosesAt.Equal(shifted.ClosesAt) || !utc.OpensAt.Equal(shifted.OpensAt) {
		t.Fatalf("window depends on the location its inputs carry: %+v vs %+v", utc, shifted)
	}
}
