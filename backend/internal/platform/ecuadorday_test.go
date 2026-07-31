package platform_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Where "today" begins, which is the boundary the Payable Balance is cut at
// (ADR 0026). America/Guayaquil is UTC-5 with no DST, so Ecuadorian midnight is
// always 05:00 UTC the same date — the arithmetic these cases lean on.
//
// The case worth having is the one in between: an instant that is one Ecuadorian
// day and a different UTC day. Reading the date in UTC would clear an
// Organization's money a day early, which is precisely the money the day rule
// exists to hold back.
func TestStartOfEcuadorDay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		instant string
		want    string
	}{
		{
			name:    "mid-morning in Ecuador",
			instant: "2026-07-07T12:00:00Z", // 07:00 in Ecuador
			want:    "2026-07-07T05:00:00Z",
		},
		{
			name:    "already tomorrow in UTC, still last night in Ecuador",
			instant: "2026-07-08T03:00:00Z", // 22:00 on the 7th in Ecuador
			want:    "2026-07-07T05:00:00Z",
		},
		{
			name:    "the boundary belongs to the day it opens",
			instant: "2026-07-08T05:00:00Z", // 00:00 on the 8th in Ecuador
			want:    "2026-07-08T05:00:00Z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := platform.StartOfEcuadorDay(mustParse(t, tc.instant))
			if !got.Equal(mustParse(t, tc.want)) {
				t.Fatalf("StartOfEcuadorDay(%s) = %s, want %s", tc.instant, got.UTC().Format(time.RFC3339), tc.want)
			}
		})
	}
}
