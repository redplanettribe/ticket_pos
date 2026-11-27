package platform

import (
	"testing"
	"time"
)

func TestLatestEcuadorHourIsTodaysOnceReachedAndYesterdaysBefore(t *testing.T) {
	// 09:00 in Guayaquil is 14:00 UTC.
	nine := time.Date(2026, 12, 3, 14, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{"exactly on the hour", nine, nine},
		{"a few seconds late", nine.Add(7 * time.Second), nine},
		{"that afternoon", nine.Add(6 * time.Hour), nine},
		{"a minute early", nine.Add(-time.Minute), nine.AddDate(0, 0, -1)},
		{"past midnight UTC, still the same Ecuadorian day", time.Date(2026, 12, 4, 3, 0, 0, 0, time.UTC), nine},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := LatestEcuadorHour(tc.at, 9); !got.Equal(tc.want) {
				t.Fatalf("LatestEcuadorHour(%v, 9) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}
