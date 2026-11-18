// Package service implements Platform Trends: the Operator Dashboard's reading
// of whether the platform as a whole is growing (#808, ADR 0080).
//
// It owns no tables. Every figure is read through the module that owns the
// data it counts, each exposing a platform-wide operation, and composed here
// into the one payload the page draws. The page derives nothing: bucketing,
// totals, the partial flag on today and the day exact recording began for
// each figure are all decided here and stated in the response.
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
// Sold per day and Sales Channel.
type TicketSales interface {
	PlatformTicketsSoldByDay(ctx context.Context, from, until time.Time, timezone string) ([]salessvc.PlatformTicketsSoldDay, error)
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

// ResolveSpan resolves a span query against today on the service clock.
func (s *Service) ResolveSpan(query SpanQuery) (Span, []platform.FieldError) {
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
	// Total is the figure's total for the span, today excluded. Null when its
	// series are not in one unit (money, per currency) and cannot be summed.
	Total *int `json:"total"`
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

// Trends reads every figure of Platform Trends for a resolved span.
func (s *Service) Trends(ctx context.Context, span Span) (*PlatformTrends, error) {
	from := platform.StartOfEcuadorDate(span.FirstDay)
	// Read through the end of today, so today's partial bucket comes from the
	// same query as the span.
	until := platform.StartOfEcuadorDate(span.Today.AddDate(0, 0, 1))
	if !span.endsYesterday() {
		until = platform.StartOfEcuadorDate(span.LastDay.AddDate(0, 0, 1))
	}

	days, err := s.sales.PlatformTicketsSoldByDay(ctx, from, until, platform.EcuadorTimeZone)
	if err != nil {
		return nil, err
	}
	byChannel := make(map[string]map[string]int, len(salesChannels))
	for _, day := range days {
		if byChannel[day.Channel] == nil {
			byChannel[day.Channel] = map[string]int{}
		}
		byChannel[day.Channel][day.Day] += day.Quantity
	}

	ticketsSold := PlatformTrendFigure{Recorded: true, Series: make([]PlatformTrendSeries, 0, len(salesChannels))}
	total := 0
	for _, channel := range salesChannels {
		series := seriesOf(channel, span, byChannel[channel])
		total += series.Total
		ticketsSold.Series = append(ticketsSold.Series, series)
	}
	ticketsSold.Total = &total

	return &PlatformTrends{
		Timezone: platform.EcuadorTimeZone,
		Span: PlatformTrendSpan{
			Preset:   span.Preset,
			FirstDay: span.FirstDay.Format(dayFormat),
			LastDay:  span.LastDay.Format(dayFormat),
		},
		Granularity: span.Granularity,
		Today:       span.Today.Format(dayFormat),
		Figures:     PlatformTrendFigures{TicketsSold: ticketsSold},
	}, nil
}

// seriesOf buckets one series' per-day values over the span, zero-filling
// the days that have none, and sets today aside as the partial bucket.
func seriesOf(key string, span Span, perDay map[string]int) PlatformTrendSeries {
	ranges := span.buckets()
	series := PlatformTrendSeries{Key: key, Buckets: make([]PlatformTrendBucket, 0, len(ranges))}
	for _, r := range ranges {
		value := 0
		for day := r.first; !day.After(r.last); day = day.AddDate(0, 0, 1) {
			value += perDay[day.Format(dayFormat)]
		}
		series.Total += value
		series.Buckets = append(series.Buckets, PlatformTrendBucket{
			FirstDay: r.first.Format(dayFormat),
			LastDay:  r.last.Format(dayFormat),
			Value:    value,
		})
	}
	if span.endsYesterday() {
		today := span.Today.Format(dayFormat)
		series.Today = &PlatformTrendBucket{FirstDay: today, LastDay: today, Value: perDay[today], Partial: true}
	}
	return series
}
