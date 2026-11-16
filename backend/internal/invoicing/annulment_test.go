package invoicing

import (
	"testing"
	"time"
)

// guayaquil is the zone the window is read in, as the service passes it
// (sri.Guayaquil, UTC-5 with no daylight saving). Declared here because the
// core does not import the adapter.
var guayaquil = time.FixedZone("America/Guayaquil", -5*60*60)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// TestAnnulmentWindowDeadlineIsDaySevenOfTheFollowingMonth tables the
// deadline and the warning date across month and year ends: day 7 and day 1
// of the month after the Emission Date, never a holiday rollover.
func TestAnnulmentWindowDeadlineIsDaySevenOfTheFollowingMonth(t *testing.T) {
	cases := []struct {
		name        string
		issuedOn    time.Time
		deadline    time.Time
		warningFrom time.Time
	}{
		{"first of a month", day(2026, time.July, 1), day(2026, time.August, 7), day(2026, time.August, 1)},
		{"mid month", day(2026, time.July, 15), day(2026, time.August, 7), day(2026, time.August, 1)},
		{"last of a 31-day month", day(2026, time.July, 31), day(2026, time.August, 7), day(2026, time.August, 1)},
		{"last of January into February", day(2026, time.January, 31), day(2026, time.February, 7), day(2026, time.February, 1)},
		{"end of February in a leap year", day(2028, time.February, 29), day(2028, time.March, 7), day(2028, time.March, 1)},
		{"December into the next year", day(2026, time.December, 31), day(2027, time.January, 7), day(2027, time.January, 1)},
		{"early December", day(2026, time.December, 1), day(2027, time.January, 7), day(2027, time.January, 1)},
		{"a day inside the warning stretch is still its own month", day(2026, time.August, 3), day(2026, time.September, 7), day(2026, time.September, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := AnnulmentWindowFor(c.issuedOn, guayaquil)
			if !w.Deadline.Equal(c.deadline) {
				t.Errorf("deadline = %s; want %s", w.Deadline.Format(time.DateOnly), c.deadline.Format(time.DateOnly))
			}
			if !w.WarningFrom.Equal(c.warningFrom) {
				t.Errorf("warning from = %s; want %s", w.WarningFrom.Format(time.DateOnly), c.warningFrom.Format(time.DateOnly))
			}
			if !w.IssuedOn.Equal(c.issuedOn) {
				t.Errorf("issued on = %s; want %s", w.IssuedOn, c.issuedOn)
			}
		})
	}
}

// TestAnnulmentWindowIsReadInGuayaquil tables the instant against the
// window: it closes at the end of day 7 in Guayaquil, not in UTC, and its
// warning stretch starts at 00:00 Guayaquil on day 1.
func TestAnnulmentWindowIsReadInGuayaquil(t *testing.T) {
	w := AnnulmentWindowFor(day(2026, time.July, 20), guayaquil)
	gye := func(m time.Month, d, h, min int) time.Time { return time.Date(2026, m, d, h, min, 0, 0, guayaquil) }
	cases := []struct {
		name     string
		now      time.Time
		open     bool
		warning  bool
		daysLeft int
		today    time.Time
	}{
		{"the day of issue", gye(time.July, 20, 9, 0), true, false, 18, day(2026, time.July, 20)},
		{"last minute before the warning", gye(time.July, 31, 23, 59), true, false, 7, day(2026, time.July, 31)},
		// 04:00 UTC on 1 August is still 31 July in Guayaquil.
		{"1 August in UTC, 31 July in Guayaquil", time.Date(2026, time.August, 1, 4, 0, 0, 0, time.UTC), true, false, 7, day(2026, time.July, 31)},
		{"the warning starts at 00:00 Guayaquil on day 1", gye(time.August, 1, 0, 0), true, true, 6, day(2026, time.August, 1)},
		{"the last day", gye(time.August, 7, 12, 0), true, true, 0, day(2026, time.August, 7)},
		{"the last minute of the last day", gye(time.August, 7, 23, 59), true, true, 0, day(2026, time.August, 7)},
		// 03:00 UTC on 8 August is still 7 August in Guayaquil.
		{"8 August in UTC, 7 August in Guayaquil", time.Date(2026, time.August, 8, 3, 0, 0, 0, time.UTC), true, true, 0, day(2026, time.August, 7)},
		{"closed at 00:00 Guayaquil on day 8", gye(time.August, 8, 0, 0), false, false, 0, day(2026, time.August, 8)},
		{"long closed", gye(time.October, 1, 0, 0), false, false, 0, day(2026, time.October, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := w.Open(c.now); got != c.open {
				t.Errorf("open = %v; want %v", got, c.open)
			}
			if got := w.Warning(c.now); got != c.warning {
				t.Errorf("warning = %v; want %v", got, c.warning)
			}
			if got := w.DaysLeft(c.now); got != c.daysLeft {
				t.Errorf("days left = %d; want %d", got, c.daysLeft)
			}
			if got := w.Today(c.now); !got.Equal(c.today) {
				t.Errorf("today = %s; want %s", got.Format(time.DateOnly), c.today.Format(time.DateOnly))
			}
		})
	}
	if got := w.TotalDays(); got != 18 {
		t.Errorf("total days = %d; want 18, from 20 July to 7 August", got)
	}
	if want := gye(time.August, 8, 0, 0); !w.ClosesAt.Equal(want) {
		t.Errorf("closes at = %s; want %s", w.ClosesAt, want)
	}
}

// TestAnnulmentWindowAcrossTheYearEnd: a factura of 31 December closes at the
// end of 7 January in Guayaquil, and warns from 1 January.
func TestAnnulmentWindowAcrossTheYearEnd(t *testing.T) {
	w := AnnulmentWindowFor(day(2026, time.December, 31), guayaquil)
	newYearUTC := time.Date(2027, time.January, 1, 2, 0, 0, 0, time.UTC) // 31 Dec 21:00 in Guayaquil
	if w.Warning(newYearUTC) {
		t.Errorf("warning at %s; the new year has not begun in Guayaquil", newYearUTC)
	}
	newYearGYE := time.Date(2027, time.January, 1, 0, 0, 0, 0, guayaquil)
	if !w.Warning(newYearGYE) || w.DaysLeft(newYearGYE) != 6 {
		t.Errorf("at %s: warning=%v days left=%d; want the warning with 6 days left", newYearGYE, w.Warning(newYearGYE), w.DaysLeft(newYearGYE))
	}
	if w.Open(time.Date(2027, time.January, 8, 0, 0, 0, 0, guayaquil)) {
		t.Error("open on 8 January in Guayaquil; want closed")
	}
}

// TestIsFactura: the two kinds Mark annulled widens on, and not the Credit
// Note, whose Mark annulled stays as it was.
func TestIsFactura(t *testing.T) {
	for kind, want := range map[DocumentKind]bool{
		DocumentKindSale: true, DocumentKindManual: true, DocumentKindCreditNote: false,
	} {
		if got := IsFactura(kind); got != want {
			t.Errorf("IsFactura(%s) = %v; want %v", kind, got, want)
		}
	}
}
