package service

import (
	"testing"
	"time"
)

// 2026-07-08T03:00Z is still 2026-07-07 on the platform clock (22:00 in
// Guayaquil), so today is the 7th and yesterday the 6th: a UTC reading would
// shift every span by a day.
var spanNow = time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC)

func TestResolveSpanPresetsEndYesterday(t *testing.T) {
	t.Parallel()

	cases := []struct {
		preset      SpanPreset
		first       string
		granularity PlatformTrendGranularity
	}{
		{"", "2026-06-07", GranularityDay},
		{Preset7Days, "2026-06-30", GranularityDay},
		{Preset30Days, "2026-06-07", GranularityDay},
		{Preset90Days, "2026-04-08", GranularityDay},
		{Preset12Months, "2025-07-08", GranularityWeek},
	}
	for _, tc := range cases {
		t.Run(string(tc.preset), func(t *testing.T) {
			t.Parallel()
			span, ok := ResolveSpan(SpanQuery{Preset: tc.preset}, spanNow)
			if !ok {
				t.Fatal("refused a preset")
			}
			if got := span.FirstDay.Format(dayFormat); got != tc.first {
				t.Fatalf("first day = %s, want %s", got, tc.first)
			}
			if got := span.LastDay.Format(dayFormat); got != "2026-07-06" {
				t.Fatalf("last day = %s, want yesterday 2026-07-06", got)
			}
			if got := span.Today.Format(dayFormat); got != "2026-07-07" {
				t.Fatalf("today = %s, want 2026-07-07 on the platform clock", got)
			}
			if span.Granularity != tc.granularity {
				t.Fatalf("granularity = %s, want %s", span.Granularity, tc.granularity)
			}
		})
	}
}

func day(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(dayFormat, raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func custom(t *testing.T, from, to string) SpanQuery {
	t.Helper()
	return SpanQuery{Preset: PresetCustom, From: day(t, from), To: day(t, to)}
}

func TestResolveSpanCustomRangeIsCutAtYesterday(t *testing.T) {
	t.Parallel()

	span, ok := ResolveSpan(custom(t, "2026-07-01", "2026-07-20"), spanNow)
	if !ok {
		t.Fatal("refused a range with closed days in it")
	}
	if span.FirstDay.Format(dayFormat) != "2026-07-01" || span.LastDay.Format(dayFormat) != "2026-07-06" {
		t.Fatalf("span = %s..%s, want 2026-07-01..2026-07-06", span.FirstDay.Format(dayFormat), span.LastDay.Format(dayFormat))
	}
	if !span.endsYesterday() {
		t.Fatal("a range cut at yesterday ends yesterday")
	}

	past, ok := ResolveSpan(custom(t, "2026-06-01", "2026-06-03"), spanNow)
	if !ok {
		t.Fatal("refused a past range")
	}
	if past.endsYesterday() {
		t.Fatal("a range ending before yesterday has no today beside it")
	}
}

// A custom range starting today, on the platform clock, has no closed day.
func TestResolveSpanRefusesARangeWithNoClosedDay(t *testing.T) {
	t.Parallel()

	if _, ok := ResolveSpan(custom(t, "2026-07-07", "2026-07-09"), spanNow); ok {
		t.Fatal("accepted a range starting today; a span ends at the close of yesterday")
	}
	if _, ok := ResolveSpan(custom(t, "2026-07-06", "2026-07-09"), spanNow); !ok {
		t.Fatal("refused a range starting yesterday, which has one closed day")
	}
}

// Weekly buckets are counted back from the last day, so the last bucket ends
// on it and any stub is the oldest one.
func TestWeeklyBucketsCountBackFromTheLastDay(t *testing.T) {
	t.Parallel()

	span, ok := ResolveSpan(custom(t, "2026-03-25", "2026-07-06"), spanNow)
	if !ok {
		t.Fatal("refused")
	}
	if span.Granularity != GranularityWeek {
		t.Fatalf("a %d-day span is drawn in %s; want weeks", span.days(), span.Granularity)
	}
	buckets := span.buckets()
	last := buckets[len(buckets)-1]
	if last.first.Format(dayFormat) != "2026-06-30" || last.last.Format(dayFormat) != "2026-07-06" {
		t.Fatalf("last bucket = %s..%s, want 2026-06-30..2026-07-06", last.first.Format(dayFormat), last.last.Format(dayFormat))
	}
	first := buckets[0]
	if first.first.Format(dayFormat) != "2026-03-25" {
		t.Fatalf("first bucket starts %s, want the span's first day 2026-03-25", first.first.Format(dayFormat))
	}
	// 104 days: fourteen whole weeks and a six-day stub at the front.
	if len(buckets) != 15 || first.last.Format(dayFormat) != "2026-03-30" {
		t.Fatalf("%d buckets, first %s..%s; want 15 with a stub ending 2026-03-30",
			len(buckets), first.first.Format(dayFormat), first.last.Format(dayFormat))
	}
	for i := 1; i < len(buckets); i++ {
		if !buckets[i].first.Equal(buckets[i-1].last.AddDate(0, 0, 1)) {
			t.Fatalf("bucket %d starts %s; the buckets must be contiguous", i, buckets[i].first.Format(dayFormat))
		}
	}
}
