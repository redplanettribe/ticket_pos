package invoicing

import (
	"testing"
	"time"
)

// TestClosingSweepRunsFromDayEightThroughDayTwelve tables the instant
// against the run: the month just closed is swept from 00:00 Guayaquil on
// day 8 of the following month to the end of day 12, never before or after,
// and the month swept crosses year ends.
func TestClosingSweepRunsFromDayEightThroughDayTwelve(t *testing.T) {
	gye := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, guayaquil) }
	cases := []struct {
		name    string
		now     time.Time
		running bool
		month   time.Time
	}{
		{"day 7 at the last minute", gye(2026, time.November, 7, 23, 59), false, time.Time{}},
		{"day 8 at 00:00 Guayaquil", gye(2026, time.November, 8, 0, 0), true, day(2026, time.October, 1)},
		{"day 8 in UTC but still day 7 in Guayaquil", time.Date(2026, time.November, 8, 4, 59, 0, 0, time.UTC), false, time.Time{}},
		{"day 10", gye(2026, time.November, 10, 12, 0), true, day(2026, time.October, 1)},
		{"day 12 at the last minute", gye(2026, time.November, 12, 23, 59), true, day(2026, time.October, 1)},
		{"day 13 at 00:00", gye(2026, time.November, 13, 0, 0), false, time.Time{}},
		{"January sweeps December", gye(2027, time.January, 9, 8, 0), true, day(2026, time.December, 1)},
		{"day 1", gye(2026, time.December, 1, 0, 0), false, time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			month, running := ClosingSweepRun(c.now, guayaquil)
			if running != c.running || !month.Equal(c.month) {
				t.Fatalf("run = %s, %t; want %s, %t", month.Format(time.DateOnly), running, c.month.Format(time.DateOnly), c.running)
			}
		})
	}
}

// TestClosingSweepLastEndedMonth tables the month the Operator Dashboard
// counts unswept facturas of: the latest month whose run has ended, so the
// month before last until day 12's end and the month just closed from day
// 13.
func TestClosingSweepLastEndedMonth(t *testing.T) {
	gye := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, guayaquil) }
	cases := []struct {
		name  string
		now   time.Time
		month time.Time
	}{
		{"day 13 at 00:00", gye(2026, time.November, 13, 0), day(2026, time.October, 1)},
		{"end of the month", gye(2026, time.November, 30, 23), day(2026, time.October, 1)},
		{"day 1 of the next month", gye(2026, time.December, 1, 0), day(2026, time.October, 1)},
		{"day 12, the run still going", gye(2026, time.December, 12, 23), day(2026, time.October, 1)},
		{"day 13 of December", gye(2026, time.December, 13, 0), day(2026, time.November, 1)},
		{"January before day 13", gye(2027, time.January, 5, 0), day(2026, time.November, 1)},
		{"February before day 13 crosses the year", gye(2027, time.February, 2, 0), day(2026, time.December, 1)},
		{"day 12 in Guayaquil though day 13 in UTC", time.Date(2026, time.November, 13, 4, 0, 0, 0, time.UTC), day(2026, time.September, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClosingSweepLastEnded(c.now, guayaquil); !got.Equal(c.month) {
				t.Fatalf("last ended = %s; want %s", got.Format(time.DateOnly), c.month.Format(time.DateOnly))
			}
		})
	}
}
