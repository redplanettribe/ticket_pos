package service

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform/trendspan"
)

// The span arithmetic is shared with Organization Trends in
// platform/trendspan. Platform Trends reads it in week mode: a span longer
// than 90 days is drawn in weeks, 12m is 52 weeks and a year earlier is 364
// days back. What stays here is the wire: the names Platform Trends states a
// span in, which its OpenAPI documents, and which offer no all time span and
// no month grain. They are literals, as the OpenAPI generator reads them, and
// each is the same string as the trendspan name it is converted to.

// dayFormat is the calendar-date layout every day of Platform Trends speaks
// in (see trendspan.DayFormat).
const dayFormat = trendspan.DayFormat

// SpanPreset is the span an operator chose: one of four fixed lengths ending at
// the close of yesterday, or a custom range of days.
type SpanPreset string

const (
	Preset7Days    SpanPreset = "7d"
	Preset30Days   SpanPreset = "30d"
	Preset90Days   SpanPreset = "90d"
	Preset12Months SpanPreset = "12m"
	PresetCustom   SpanPreset = "custom"
)

// Presets is every span a caller may name, in the order the page offers them.
var Presets = []SpanPreset{Preset7Days, Preset30Days, Preset90Days, Preset12Months, PresetCustom}

// MaxCustomSpanDays bounds a custom range (see trendspan.MaxCustomDays).
const MaxCustomSpanDays = trendspan.MaxCustomDays

// DayFormat is the layout a day travels in on the wire, YYYY-MM-DD.
const DayFormat = dayFormat

// PlatformTrendGranularity is the length of a bucket.
type PlatformTrendGranularity string

const (
	GranularityDay  PlatformTrendGranularity = "day"
	GranularityWeek PlatformTrendGranularity = "week"
)

// ComparisonKind is the span a span is set beside: the one of the same length
// that ends the day before it starts, or the one of the same length 52 weeks
// earlier.
type ComparisonKind string

const (
	ComparePrevious    ComparisonKind = "previous"
	CompareYearEarlier ComparisonKind = "year_earlier"
)

// Comparisons is every comparison a caller may name, the default first.
var Comparisons = []ComparisonKind{ComparePrevious, CompareYearEarlier}

// SpanQuery is a span as the caller asked for it, already checked for shape
// by the handler. From and To are set for a custom span alone, as the first
// and last day asked for, both included and From never after To. An empty
// Compare is the default, the previous span.
type SpanQuery struct {
	Preset  SpanPreset
	From    time.Time
	To      time.Time
	Compare ComparisonKind
}

// Span is a resolved span (see trendspan.Span), its preset, grain and
// comparison in Platform Trends' own names.
type Span struct {
	trendspan.Span
	Preset      SpanPreset
	Granularity PlatformTrendGranularity
	Comparison  ComparisonKind
}

func spanOf(shared trendspan.Span) Span {
	return Span{
		Span:        shared,
		Preset:      SpanPreset(shared.Preset),
		Granularity: PlatformTrendGranularity(shared.Granularity),
		Comparison:  ComparisonKind(shared.Comparison),
	}
}

// ResolveSpan turns a query into the span it names on the platform clock, and
// reports false for a custom range with no closed day in it (see
// trendspan.Resolve, read here in week mode).
func ResolveSpan(query SpanQuery, now time.Time) (Span, bool) {
	shared, ok := trendspan.Resolve(trendspan.Query{
		Preset:  trendspan.Preset(query.Preset),
		From:    query.From,
		To:      query.To,
		Compare: trendspan.Comparison(query.Compare),
	}, trendspan.GranularityWeek, now)
	if !ok {
		return Span{}, false
	}
	return spanOf(shared), true
}

// days is how many days the span covers, both ends included.
func (s Span) days() int {
	return s.Span.Days()
}

// endsYesterday reports whether the span runs up to today, which is when today
// is read beside it as the partial bucket.
func (s Span) endsYesterday() bool {
	return s.Span.EndsYesterday()
}

// compared is the comparison span (see trendspan.Span.Compared): as many days
// as the span, ending the day before it starts or 52 weeks earlier. A week
// mode span always carries one.
func (s Span) compared() Span {
	shared, _ := s.Span.Compared()
	return spanOf(shared)
}

// civilDate is the platform day an instant falls on, carried as midnight UTC.
func civilDate(instant time.Time) time.Time {
	return trendspan.CivilDate(instant)
}

// bucketRange is one bucket's first and last day, both included.
type bucketRange struct {
	first time.Time
	last  time.Time
}

// buckets cuts the span into its buckets, oldest first (see
// trendspan.Span.Buckets): one per day, or weeks counted back from the last
// day with any stub the oldest bucket.
func (s Span) buckets() []bucketRange {
	shared := s.Span.Buckets()
	out := make([]bucketRange, 0, len(shared))
	for _, b := range shared {
		out = append(out, bucketRange{first: b.First, last: b.Last})
	}
	return out
}
