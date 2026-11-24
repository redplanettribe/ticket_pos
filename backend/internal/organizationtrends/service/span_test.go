package service

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform/trendspan"
)

// Organization Trends reads the shared span code in month mode, on its own
// clock: 12m is calendar months and all time is offered.
func TestResolveSpanIsMonthModeOnTheServiceClock(t *testing.T) {
	t.Parallel()

	svc := New(nil, nil, nil).WithClock(func() time.Time { return time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC) })

	twelve, ok := svc.ResolveSpan(trendspan.Query{Preset: trendspan.Preset12Months})
	if !ok || twelve.Granularity != trendspan.GranularityMonth || twelve.FirstDay.Format(trendspan.DayFormat) != "2025-08-01" {
		t.Fatalf("12m = %+v, %v; want months from 2025-08-01", twelve, ok)
	}
	all, ok := svc.ResolveSpan(trendspan.Query{Preset: trendspan.PresetAll, Since: time.Date(2025, 2, 14, 0, 0, 0, 0, time.UTC)})
	if !ok || all.FirstDay.Format(trendspan.DayFormat) != "2025-02-01" || all.Comparison != trendspan.CompareNone {
		t.Fatalf("all = %+v, %v; want months from 2025-02-01 with no comparison", all, ok)
	}
}
