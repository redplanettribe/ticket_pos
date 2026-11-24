package trendspan

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// 2026-07-08T03:00Z is still 2026-07-07 on the platform clock (22:00 in
// Guayaquil), so today is the 7th and yesterday the 6th: a UTC reading would
// shift every span by a day.
var now = time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC)

// noonOn is noon on the platform clock on a day, an instant safely inside it.
func noonOn(t *testing.T, raw string) time.Time {
	t.Helper()
	return day(t, raw).Add(17 * time.Hour)
}

func day(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(DayFormat, raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func custom(t *testing.T, from, to string) Query {
	t.Helper()
	return Query{Preset: PresetCustom, From: day(t, from), To: day(t, to)}
}

func resolve(t *testing.T, query Query, long Granularity, at time.Time) Span {
	t.Helper()
	span, ok := Resolve(query, long, at)
	if !ok {
		t.Fatalf("refused %+v", query)
	}
	return span
}

// describe renders buckets as first..last, a trailing * marking a partial one.
func describe(buckets []Bucket) string {
	parts := make([]string, 0, len(buckets))
	for _, b := range buckets {
		part := b.First.Format(DayFormat) + ".." + b.Last.Format(DayFormat)
		if b.Partial {
			part += "*"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

func assertSpan(t *testing.T, span Span, first, last string) {
	t.Helper()
	if got := span.FirstDay.Format(DayFormat) + ".." + span.LastDay.Format(DayFormat); got != first+".."+last {
		t.Fatalf("span = %s, want %s..%s", got, first, last)
	}
}

// Spans of up to 90 days are day buckets in either mode; only longer ones
// take the long grain.
func TestShortSpansAreDayBucketsInEitherMode(t *testing.T) {
	t.Parallel()

	for _, long := range []Granularity{GranularityWeek, GranularityMonth} {
		for _, preset := range []Preset{Preset7Days, Preset30Days, Preset90Days} {
			span := resolve(t, Query{Preset: preset}, long, now)
			if span.Granularity != GranularityDay {
				t.Fatalf("%s in %s mode is drawn in %s; want days", preset, long, span.Granularity)
			}
			buckets := span.Buckets()
			if len(buckets) != span.Days() {
				t.Fatalf("%s: %d buckets for %d days", preset, len(buckets), span.Days())
			}
			for _, b := range buckets {
				if !b.First.Equal(b.Last) || b.Partial {
					t.Fatalf("%s: day bucket %s is not one whole day", preset, describe([]Bucket{b}))
				}
			}
		}
	}
}

// The week mode is Platform Trends': 12m is 52 weeks and a year earlier is
// 364 days back.
func TestWeekModeTwelveMonthsIsFiftyTwoWeeks(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: Preset12Months, Compare: CompareYearEarlier}, GranularityWeek, now)
	assertSpan(t, span, "2025-07-08", "2026-07-06")
	if span.Granularity != GranularityWeek || len(span.Buckets()) != 52 {
		t.Fatalf("%s with %d buckets; want 52 weeks", span.Granularity, len(span.Buckets()))
	}
	compared, ok := span.Compared()
	if !ok {
		t.Fatal("no comparison")
	}
	assertSpan(t, compared, "2024-07-09", "2025-07-07")
}

func TestWeekModeRefusesAllTime(t *testing.T) {
	t.Parallel()

	if _, ok := Resolve(Query{Preset: PresetAll, Since: day(t, "2025-01-01")}, GranularityWeek, now); ok {
		t.Fatal("week mode has no all time span")
	}
}

// 12m in month mode is the current month and the eleven before it, through
// yesterday: the last month is cut at yesterday and flagged partial.
func TestMonthModeTwelveMonths(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: Preset12Months}, GranularityMonth, now)
	assertSpan(t, span, "2025-08-01", "2026-07-06")
	if span.Granularity != GranularityMonth {
		t.Fatalf("granularity = %s, want month", span.Granularity)
	}
	want := "2025-08-01..2025-08-31 2025-09-01..2025-09-30 2025-10-01..2025-10-31 2025-11-01..2025-11-30 " +
		"2025-12-01..2025-12-31 2026-01-01..2026-01-31 2026-02-01..2026-02-28 2026-03-01..2026-03-31 " +
		"2026-04-01..2026-04-30 2026-05-01..2026-05-31 2026-06-01..2026-06-30 2026-07-01..2026-07-06*"
	if got := describe(span.Buckets()); got != want {
		t.Fatalf("buckets = %s\nwant      %s", got, want)
	}
}

// On the first of a month, yesterday closed the month before, so 12m is
// eleven whole months and today alone lies in the current one.
func TestMonthModeTwelveMonthsOnTheFirstOfAMonth(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: Preset12Months}, GranularityMonth, noonOn(t, "2026-07-01"))
	assertSpan(t, span, "2025-08-01", "2026-06-30")
	buckets := span.Buckets()
	if len(buckets) != 11 || buckets[len(buckets)-1].Partial {
		t.Fatalf("buckets = %s; want eleven whole months", describe(buckets))
	}
	today, ok := span.TodayBucket()
	if !ok || !today.Partial || today.First.Format(DayFormat) != "2026-07-01" {
		t.Fatalf("today = %+v, %v; want 2026-07-01 flagged partial", today, ok)
	}
}

// A long custom range is cut into calendar months, the first and last cut by
// the span and flagged partial, the ones between whole.
func TestMonthBucketsFlagTheMonthsTheSpanCuts(t *testing.T) {
	t.Parallel()

	span := resolve(t, custom(t, "2025-11-15", "2026-03-10"), GranularityMonth, now)
	want := "2025-11-15..2025-11-30* 2025-12-01..2025-12-31 2026-01-01..2026-01-31 2026-02-01..2026-02-28 2026-03-01..2026-03-10*"
	if got := describe(span.Buckets()); got != want {
		t.Fatalf("buckets = %s\nwant      %s", got, want)
	}

	whole := resolve(t, custom(t, "2026-01-01", "2026-04-30"), GranularityMonth, now)
	for _, b := range whole.Buckets() {
		if b.Partial {
			t.Fatalf("a span of whole months flagged %s partial", describe([]Bucket{b}))
		}
	}
}

// All time starts on the first of the month holding the day the caller
// supplies and carries no comparison.
func TestAllTimeStartsOnTheMonthOfTheSuppliedDay(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: PresetAll, Since: day(t, "2024-03-17"), Compare: CompareYearEarlier}, GranularityMonth, now)
	assertSpan(t, span, "2024-03-01", "2026-07-06")
	if span.Preset != PresetAll || span.Granularity != GranularityMonth {
		t.Fatalf("preset %s, granularity %s; want all in months", span.Preset, span.Granularity)
	}
	if buckets := span.Buckets(); len(buckets) != 29 || buckets[0].Partial {
		t.Fatalf("%d buckets, first %s; want 29 from a whole March 2024", len(buckets), describe(buckets[:1]))
	}
	if span.Comparison != CompareNone {
		t.Fatalf("comparison = %s, want none", span.Comparison)
	}
	if compared, ok := span.Compared(); ok {
		t.Fatalf("all time compared with %s..%s", compared.FirstDay.Format(DayFormat), compared.LastDay.Format(DayFormat))
	}
	series := span.Series(map[string]int{"2024-03-01": 4})
	if series.Comparison != nil || series.ComparisonTotal != nil {
		t.Fatal("all time states a comparison")
	}
	if series.Total != 4 {
		t.Fatalf("total = %d, want 4", series.Total)
	}
}

// All time always holds a closed day: with no day supplied, or one in the
// current month, it starts on the first of the month holding yesterday.
func TestAllTimeAlwaysHoldsAClosedDay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		since time.Time
		at    time.Time
		first string
		last  string
	}{
		{"no day supplied", time.Time{}, now, "2026-07-01", "2026-07-06"},
		{"supplied day today", day(t, "2026-07-07"), now, "2026-07-01", "2026-07-06"},
		{"created on the first, read that day", day(t, "2026-07-01"), noonOn(t, "2026-07-01"), "2026-06-01", "2026-06-30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			span := resolve(t, Query{Preset: PresetAll, Since: tc.since}, GranularityMonth, tc.at)
			assertSpan(t, span, tc.first, tc.last)
		})
	}
}

// A year earlier sets each month beside the same calendar month a year
// before, cut on the same days, so the partial last month is set beside the
// same days of that month.
func TestMonthModeYearEarlierIsTheSameCalendarMonths(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: Preset12Months, Compare: CompareYearEarlier}, GranularityMonth, now)
	compared, ok := span.Compared()
	if !ok {
		t.Fatal("no comparison")
	}
	assertSpan(t, compared, "2024-08-01", "2025-07-06")
	ours, theirs := span.Buckets(), compared.Buckets()
	if len(ours) != len(theirs) {
		t.Fatalf("%d buckets beside %d", len(ours), len(theirs))
	}
	for i := range ours {
		if ours[i].First.AddDate(-1, 0, 0) != theirs[i].First || ours[i].Partial != theirs[i].Partial {
			t.Fatalf("bucket %d %s beside %s", i, describe(ours[i:i+1]), describe(theirs[i:i+1]))
		}
	}
	if got := describe(theirs[len(theirs)-1:]); got != "2025-07-01..2025-07-06*" {
		t.Fatalf("last compared bucket = %s, want 2025-07-01..2025-07-06*", got)
	}
}

// A year before a leap day's month is a February that ends on the 28th: the
// whole month is set beside the whole month.
func TestMonthModeYearEarlierAcrossALeapDay(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: PresetCustom, From: day(t, "2027-12-01"), To: day(t, "2028-02-29"), Compare: CompareYearEarlier}, GranularityMonth, noonOn(t, "2028-06-01"))
	compared, _ := span.Compared()
	assertSpan(t, compared, "2026-12-01", "2027-02-28")
	if got := describe(compared.Buckets()); got != "2026-12-01..2026-12-31 2027-01-01..2027-01-31 2027-02-01..2027-02-28" {
		t.Fatalf("compared buckets = %s", got)
	}
}

// The previous span is as many months just before the span, cut on the same
// days, so its buckets line up with the span's one for one.
func TestMonthModePreviousIsTheSameNumberOfMonthsBefore(t *testing.T) {
	t.Parallel()

	cut := resolve(t, custom(t, "2025-11-15", "2026-03-10"), GranularityMonth, now)
	compared, ok := cut.Compared()
	if !ok {
		t.Fatal("no comparison")
	}
	want := "2025-06-15..2025-06-30* 2025-07-01..2025-07-31 2025-08-01..2025-08-31 2025-09-01..2025-09-30 2025-10-01..2025-10-10*"
	if got := describe(compared.Buckets()); got != want {
		t.Fatalf("previous buckets = %s\nwant               %s", got, want)
	}

	whole := resolve(t, custom(t, "2026-01-01", "2026-04-30"), GranularityMonth, now)
	compared, _ = whole.Compared()
	assertSpan(t, compared, "2025-09-01", "2025-12-31")

	twelve := resolve(t, Query{Preset: Preset12Months}, GranularityMonth, now)
	compared, _ = twelve.Compared()
	assertSpan(t, compared, "2024-08-01", "2025-07-06")
}

// Day buckets keep the 364-day year in month mode too, so a day is set beside
// the same weekday.
func TestMonthModeDayBucketsKeepTheWeekdayYear(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: Preset7Days, Compare: CompareYearEarlier}, GranularityMonth, now)
	compared, _ := span.Compared()
	assertSpan(t, compared, "2025-07-01", "2025-07-07")
}

// everyDay is a per-day series holding one on every day from first to last
// and a hundred on today, so a total that took today in would show it.
func everyDay(t *testing.T, first, last, today string) map[string]int {
	t.Helper()
	perDay := map[string]int{}
	for d := day(t, first); !d.After(day(t, last)); d = d.AddDate(0, 0, 1) {
		perDay[d.Format(DayFormat)] = 1
	}
	perDay[today] = 100
	return perDay
}

// Today lies outside the span: it is stated apart, flagged partial, and in no
// bucket, total or comparison.
func TestTodayIsPartialAndOutsideEveryTotal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		query Query
		long  Granularity
	}{
		{Query{Preset: Preset7Days}, GranularityMonth},
		{Query{Preset: Preset7Days, Compare: CompareYearEarlier}, GranularityWeek},
		{Query{Preset: Preset12Months}, GranularityWeek},
		{Query{Preset: Preset12Months}, GranularityMonth},
		{Query{Preset: Preset12Months, Compare: CompareYearEarlier}, GranularityMonth},
		{Query{Preset: PresetAll, Since: day(t, "2023-01-01")}, GranularityMonth},
	} {
		t.Run(fmt.Sprintf("%s %s %s", tc.query.Preset, tc.query.Compare, tc.long), func(t *testing.T) {
			t.Parallel()
			span := resolve(t, tc.query, tc.long, now)
			series := span.Series(everyDay(t, "2020-01-01", "2026-07-06", "2026-07-07"))

			if series.Today == nil || !series.Today.Partial || series.Today.Value != 100 ||
				series.Today.First.Format(DayFormat) != "2026-07-07" || !series.Today.Last.Equal(series.Today.First) {
				t.Fatalf("today = %+v; want 2026-07-07 alone, flagged partial, with its 100", series.Today)
			}
			if series.Total != span.Days() {
				t.Fatalf("total = %d over %d days; today was counted", series.Total, span.Days())
			}
			sum := 0
			for _, b := range series.Buckets {
				sum += b.Value
				if !b.Last.Before(span.Today) {
					t.Fatalf("bucket %s reaches today", describe([]Bucket{b.Bucket}))
				}
			}
			if sum != series.Total {
				t.Fatalf("buckets add up to %d, total %d", sum, series.Total)
			}
			if compared, ok := span.Compared(); ok {
				if series.ComparisonTotal == nil || *series.ComparisonTotal != compared.Days() {
					t.Fatalf("comparison total = %v over %d days", series.ComparisonTotal, compared.Days())
				}
				if len(series.Comparison) != len(series.Buckets) {
					t.Fatalf("%d comparison buckets beside %d", len(series.Comparison), len(series.Buckets))
				}
			}
		})
	}
}

// In month mode the current month's today portion stays out of the current
// month's bucket: the bucket is cut at yesterday and flagged partial, and
// today is stated apart.
func TestTheCurrentMonthsTodayPortionStaysOutOfItsBucket(t *testing.T) {
	t.Parallel()

	span := resolve(t, Query{Preset: Preset12Months}, GranularityMonth, now)
	series := span.Series(everyDay(t, "2026-07-01", "2026-07-06", "2026-07-07"))
	last := series.Buckets[len(series.Buckets)-1]
	if got := describe([]Bucket{last.Bucket}); got != "2026-07-01..2026-07-06*" || last.Value != 6 {
		t.Fatalf("current month = %s holding %d; want 2026-07-01..2026-07-06* holding 6", got, last.Value)
	}
	if series.Total != 6 || series.Today == nil || series.Today.Value != 100 {
		t.Fatalf("total %d, today %+v; want 6 and today's 100 apart", series.Total, series.Today)
	}
}

// A span that ends before yesterday has no today beside it.
func TestAPastSpanHasNoToday(t *testing.T) {
	t.Parallel()

	span := resolve(t, custom(t, "2025-11-15", "2026-03-10"), GranularityMonth, now)
	if _, ok := span.TodayBucket(); ok {
		t.Fatal("a span ending before yesterday stated today")
	}
	if series := span.Series(map[string]int{"2026-07-07": 5}); series.Today != nil {
		t.Fatal("a span ending before yesterday stated today")
	}
}

func TestCustomRangeIsCutAtYesterdayAndRefusedWithNoClosedDay(t *testing.T) {
	t.Parallel()

	span := resolve(t, custom(t, "2026-01-01", "2026-07-20"), GranularityMonth, now)
	assertSpan(t, span, "2026-01-01", "2026-07-06")
	if _, ok := span.TodayBucket(); !ok {
		t.Fatal("a range cut at yesterday has today beside it")
	}
	if _, ok := Resolve(custom(t, "2026-07-07", "2026-07-09"), GranularityMonth, now); ok {
		t.Fatal("accepted a range starting today")
	}
}
