// Package service implements Platform Trends: the Operator Dashboard's reading
// of whether the platform as a whole is growing (#808, ADR 0080).
//
// It owns no tables. Every figure is read through the module that owns the
// data it counts, each exposing a platform-wide operation, and composed here
// into the one payload the page draws. The page derives nothing: bucketing,
// totals, the partial flag on today, the comparison series aligned bucket to
// bucket, the change, the first day each figure has data and the day exact
// recording began for each figure are all decided here and stated in the
// response.
//
// Every day here is the platform's day, on the clock the Reversal Window and
// the Payable Balance already read (platform.EcuadorTimeZone). No Event's
// timezone is read anywhere in this module.
package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// TicketSales is what Platform Trends needs from sales: the platform's Tickets
// Sold per day and Sales Channel, and the first day it sold anything.
type TicketSales interface {
	PlatformTicketsSoldByDay(ctx context.Context, from, until time.Time, timezone string) ([]salessvc.PlatformTicketsSoldDay, error)
	PlatformFirstTicketSoldDay(ctx context.Context, timezone string) (string, bool, error)
}

// salesChannels is every Sales Channel, in the order the series are stated;
// the ticket_sales CHECK constraint admits no other.
// Each is always present, zero or not, so the page's legend never changes
// shape with the data.
var salesChannels = []string{"online", "in_person", "import"}

// Service reads Platform Trends.
type Service struct {
	sales TicketSales
	now   func() time.Time
}

// New returns a Platform Trends service reading through the given modules.
func New(sales TicketSales) *Service {
	return &Service{sales: sales, now: time.Now}
}

// WithClock overrides the clock today is read from (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// ResolveSpan resolves a span query against today on the service clock, and
// reports false for a custom range with no closed day in it.
func (s *Service) ResolveSpan(query SpanQuery) (Span, bool) {
	return ResolveSpan(query, s.now())
}

// PlatformTrends is the whole of Platform Trends for one span: the span as
// resolved, the bucket length chosen for it, and every figure.
type PlatformTrends struct {
	// Timezone names the platform clock every day here is read on.
	Timezone string `json:"timezone" example:"America/Guayaquil"`
	// Span is the span the figures cover, as resolved: a custom range that
	// reached today or later is stated here ending at yesterday.
	Span PlatformTrendSpan `json:"span"`
	// Comparison is the span every figure is set beside, as many days long
	// as Span and bucketed the same way.
	Comparison PlatformTrendComparison `json:"comparison"`
	// Granularity is the length of every bucket: day for a span of up to 90
	// days, week beyond.
	Granularity PlatformTrendGranularity `json:"granularity"`
	// Today is the platform day the reading was taken on, which is never
	// inside the span.
	Today string `json:"today" example:"2026-07-07"`
	// Figures carries one entry per figure of Platform Trends.
	Figures PlatformTrendFigures `json:"figures"`
}

// PlatformTrendSpan is a resolved span's preset and its first and last day, both
// included.
type PlatformTrendSpan struct {
	Preset   SpanPreset `json:"preset"`
	FirstDay string     `json:"first_day" example:"2026-06-07"`
	LastDay  string     `json:"last_day" example:"2026-07-06"`
}

// PlatformTrendComparison is the comparison span: its kind and its first and
// last day, both included. It may reach before a figure has data, which each
// figure's data_since says.
type PlatformTrendComparison struct {
	Kind     ComparisonKind `json:"kind" enums:"previous,year_earlier"`
	FirstDay string         `json:"first_day" example:"2026-05-08"`
	LastDay  string         `json:"last_day" example:"2026-06-06"`
}

// PlatformTrendFigures is every figure Platform Trends states. It is built for the eight
// the page draws (#808) and carries those whose reads have shipped.
type PlatformTrendFigures struct {
	// TicketsSold is the quantity over active Ticket Sales, by the instant
	// sold, one series per Sales Channel.
	TicketsSold PlatformTrendFigure `json:"tickets_sold"`
}

// PlatformTrendFigure is one figure for the span: its series, its total, whether it is
// recorded at all and from which day it is exact.
type PlatformTrendFigure struct {
	// Recorded is false while the platform does not record this figure at
	// all, which the page states as "not yet recorded" rather than drawing a
	// zero that means "not measured".
	Recorded bool `json:"recorded"`
	// ExactSince is the platform day exact recording of this figure began.
	// Buckets before it are a stand-in or partial. Null when the figure has
	// always been exact.
	ExactSince *string `json:"exact_since" example:"2026-07-07"`
	// DataSince is the first platform day this figure has data: no bucket
	// before it is stated in a comparison, since missing history is absent
	// and never zero. Null when the figure has no data at all.
	DataSince *string `json:"data_since" example:"2026-01-15"`
	// Total is the figure's total for the span, today excluded. Null when its
	// series are not in one unit (money, per currency) and cannot be summed.
	Total *int `json:"total"`
	// ComparisonTotal is the figure's total over the comparison span from
	// DataSince on. Null when Total is, or when the comparison span has no
	// data.
	ComparisonTotal *int `json:"comparison_total"`
	// Change is Total against ComparisonTotal as a signed ratio, 0.5 for half
	// as much again and -0.5 for half as much. Null, never zero or infinite,
	// when ComparisonTotal is null or zero.
	Change *float64 `json:"change" example:"0.25"`
	// Series is the figure's series in a stable order, each always present.
	Series []PlatformTrendSeries `json:"series"`
}

// PlatformTrendSeries is one line of a figure: a Sales Channel for Tickets Sold.
type PlatformTrendSeries struct {
	// Key names the series: for Tickets Sold, the Sales Channel.
	Key string `json:"key" example:"online"`
	// Total is the series' total for the span, today excluded.
	Total int `json:"total"`
	// Buckets is every bucket of the span, oldest first, the last one ending
	// on the span's last day. A bucket with nothing in it is zero, never
	// absent.
	Buckets []PlatformTrendBucket `json:"buckets"`
	// Today is today's bucket, flagged partial and in no total. Present only
	// when the span ends yesterday, which every preset does; a custom range
	// that ends earlier has no today beside it.
	Today *PlatformTrendBucket `json:"today"`
	// ComparisonTotal is the series' total over the comparison span from the
	// figure's data_since on. Null when the comparison span has no data.
	ComparisonTotal *int `json:"comparison_total"`
	// Change is Total against ComparisonTotal as a signed ratio. Null, never
	// zero or infinite, when ComparisonTotal is null or zero.
	Change *float64 `json:"change" example:"-0.1"`
	// ComparisonBuckets has one entry per entry of Buckets, at the same
	// index: the comparison span's bucket of the same length. An entry is
	// null where its bucket lies wholly before the figure's data_since, and a
	// bucket holding data_since starts on it. Today has no counterpart.
	ComparisonBuckets []*PlatformTrendBucket `json:"comparison_buckets"`
}

// PlatformTrendBucket is one bucket of a series: the days it covers, both included, and
// its value.
type PlatformTrendBucket struct {
	FirstDay string `json:"first_day" example:"2026-07-06"`
	LastDay  string `json:"last_day" example:"2026-07-06"`
	Value    int    `json:"value"`
	// Partial is true on today's bucket alone: a day still running.
	Partial bool `json:"partial"`
}

// Trends reads every figure of Platform Trends for a resolved span and its
// comparison.
func (s *Service) Trends(ctx context.Context, span Span) (*PlatformTrends, error) {
	ticketsSold, err := s.ticketsSold(ctx, span)
	if err != nil {
		return nil, err
	}

	compared := span.compared()
	return &PlatformTrends{
		Timezone: platform.EcuadorTimeZone,
		Span: PlatformTrendSpan{
			Preset:   span.Preset,
			FirstDay: span.FirstDay.Format(dayFormat),
			LastDay:  span.LastDay.Format(dayFormat),
		},
		Comparison: PlatformTrendComparison{
			Kind:     span.Comparison,
			FirstDay: compared.FirstDay.Format(dayFormat),
			LastDay:  compared.LastDay.Format(dayFormat),
		},
		Granularity: span.Granularity,
		Today:       span.Today.Format(dayFormat),
		Figures:     PlatformTrendFigures{TicketsSold: ticketsSold},
	}, nil
}

// ticketsSold reads Tickets Sold, one series per Sales Channel.
func (s *Service) ticketsSold(ctx context.Context, span Span) (PlatformTrendFigure, error) {
	first, ok, err := s.sales.PlatformFirstTicketSoldDay(ctx, platform.EcuadorTimeZone)
	if err != nil {
		return PlatformTrendFigure{}, err
	}
	var dataSince *time.Time
	if ok {
		day, err := time.Parse(dayFormat, first)
		if err != nil {
			return PlatformTrendFigure{}, err
		}
		dataSince = &day
	}

	byChannel := make(map[string]map[string]int, len(salesChannels))
	for _, window := range readWindows(span, dataSince) {
		days, err := s.sales.PlatformTicketsSoldByDay(ctx, window.from, window.until, platform.EcuadorTimeZone)
		if err != nil {
			return PlatformTrendFigure{}, err
		}
		for _, day := range days {
			if byChannel[day.Channel] == nil {
				byChannel[day.Channel] = map[string]int{}
			}
			byChannel[day.Channel][day.Day] += day.Quantity
		}
	}
	return figureOf(span, dataSince, salesChannels, byChannel, true), nil
}

// readWindow is one run of instants a figure is read over, [from, until).
type readWindow struct {
	from  time.Time
	until time.Time
}

// readWindows is what a figure must read to be stated over a span: the span
// itself through the end of today, when today is beside it, and the part of
// the comparison span from dataSince on that the first window does not
// already cover. The windows never overlap, so a figure may add up what each
// returns; the comparison is read apart rather than as one run reaching back
// to it, because a year-earlier comparison would otherwise read the year in
// between. A figure with no data reads no comparison at all.
func readWindows(span Span, dataSince *time.Time) []readWindow {
	until := span.LastDay.AddDate(0, 0, 1)
	if span.endsYesterday() {
		until = span.Today.AddDate(0, 0, 1)
	}
	windows := []readWindow{{from: platform.StartOfEcuadorDate(span.FirstDay), until: platform.StartOfEcuadorDate(until)}}

	if dataSince == nil {
		return windows
	}
	compared := span.compared()
	first, end := compared.FirstDay, compared.LastDay.AddDate(0, 0, 1)
	if first.Before(*dataSince) {
		first = *dataSince
	}
	if end.After(span.FirstDay) {
		end = span.FirstDay
	}
	if first.Before(end) {
		windows = append(windows, readWindow{from: platform.StartOfEcuadorDate(first), until: platform.StartOfEcuadorDate(end)})
	}
	return windows
}

// figureOf states one figure from its series' values per day, keyed by series
// key and then by day: each series over the span and beside its comparison,
// in the order keys gives, each always present. It is how every figure is
// built, so each gets the comparison, the change and the absent history the
// same way. A summable figure, one whose series share a unit, also states its
// totals across every series. The figure is stated as recorded and always
// exact; a figure that is not sets Recorded and ExactSince itself.
func figureOf(span Span, dataSince *time.Time, keys []string, perSeries map[string]map[string]int, summable bool) PlatformTrendFigure {
	figure := PlatformTrendFigure{Recorded: true, Series: make([]PlatformTrendSeries, 0, len(keys))}
	if dataSince != nil {
		since := dataSince.Format(dayFormat)
		figure.DataSince = &since
	}

	total, comparisonTotal, compared := 0, 0, false
	for _, key := range keys {
		series := seriesOf(key, span, dataSince, perSeries[key])
		total += series.Total
		if series.ComparisonTotal != nil {
			comparisonTotal += *series.ComparisonTotal
			compared = true
		}
		figure.Series = append(figure.Series, series)
	}
	if summable {
		figure.Total = &total
		if compared {
			figure.ComparisonTotal = &comparisonTotal
			figure.Change = change(total, comparisonTotal)
		}
	}
	return figure
}

// seriesOf buckets one series' per-day values over the span, zero-filling
// the days that have none, sets today aside as the partial bucket, and lines
// the comparison span's buckets up beside the span's.
func seriesOf(key string, span Span, dataSince *time.Time, perDay map[string]int) PlatformTrendSeries {
	ranges := span.buckets()
	series := PlatformTrendSeries{Key: key, Buckets: make([]PlatformTrendBucket, 0, len(ranges))}
	for _, bucket := range ranges {
		value := sumDays(perDay, bucket)
		series.Total += value
		series.Buckets = append(series.Buckets, PlatformTrendBucket{
			FirstDay: bucket.first.Format(dayFormat),
			LastDay:  bucket.last.Format(dayFormat),
			Value:    value,
		})
	}
	if span.endsYesterday() {
		today := span.Today.Format(dayFormat)
		series.Today = &PlatformTrendBucket{FirstDay: today, LastDay: today, Value: perDay[today], Partial: true}
	}

	comparisonRanges := span.compared().buckets()
	series.ComparisonBuckets = make([]*PlatformTrendBucket, 0, len(comparisonRanges))
	comparisonTotal, compared := 0, false
	for _, bucket := range comparisonRanges {
		if dataSince == nil || bucket.last.Before(*dataSince) {
			series.ComparisonBuckets = append(series.ComparisonBuckets, nil)
			continue
		}
		if bucket.first.Before(*dataSince) {
			bucket.first = *dataSince
		}
		value := sumDays(perDay, bucket)
		comparisonTotal += value
		compared = true
		series.ComparisonBuckets = append(series.ComparisonBuckets, &PlatformTrendBucket{
			FirstDay: bucket.first.Format(dayFormat),
			LastDay:  bucket.last.Format(dayFormat),
			Value:    value,
		})
	}
	if compared {
		series.ComparisonTotal = &comparisonTotal
		series.Change = change(series.Total, comparisonTotal)
	}
	return series
}

// sumDays is the total of the days a bucket covers.
func sumDays(perDay map[string]int, bucket bucketRange) int {
	value := 0
	for day := bucket.first; !day.After(bucket.last); day = day.AddDate(0, 0, 1) {
		value += perDay[day.Format(dayFormat)]
	}
	return value
}

// change is total against comparison as a signed ratio, and nil when the
// comparison is zero: growth from nothing has no ratio, and stating it as
// zero or infinite would be a figure nobody could read.
func change(total, comparison int) *float64 {
	if comparison == 0 {
		return nil
	}
	ratio := float64(total-comparison) / float64(comparison)
	return &ratio
}
