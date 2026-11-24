package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/trendspan"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// Organization Trends (#861, #862): the figures of the Organization over a chosen
// span, beneath Home now. The span arithmetic is trendspan's, in month mode;
// every figure is read live per platform day and stated over the span with
// trendspan.Span.Series, so today stays apart and in no total.

// salesChannels is every Sales Channel, in the order the series are stated,
// as Platform Trends states them; each is always present, zero or not, so the
// page's key never changes shape with the data.
var salesChannels = []string{"online", "in_person", "import"}

// countSeriesKey keys the one series of a figure that is a plain count.
const countSeriesKey = "all"

// OrganizationTrendGranularity is the length of a bucket: a day for a span of
// up to 90 days, a calendar month beyond and for all time.
type OrganizationTrendGranularity string

const (
	GranularityDay   OrganizationTrendGranularity = "day"
	GranularityMonth OrganizationTrendGranularity = "month"
)

// OrganizationTrendComparisonKind is the span a span is set beside, in the
// wire's words: the span before it, or the same span a year earlier.
type OrganizationTrendComparisonKind string

const (
	ComparePrevious OrganizationTrendComparisonKind = "previous"
	CompareYear     OrganizationTrendComparisonKind = "year"
)

// TrendSpanQuery is a span as the caller asked for it, in the wire's words,
// already checked for shape by the handler: an empty Preset is 30 days and an
// empty Compare the previous span. From and To are set for a custom span
// alone. All time carries no comparison, whatever Compare says.
type TrendSpanQuery struct {
	Preset  trendspan.Preset
	From    time.Time
	To      time.Time
	Compare OrganizationTrendComparisonKind
}

// OrganizationTrends is Organization Trends over one span.
type OrganizationTrends struct {
	// Timezone names the platform clock every day here is read on.
	Timezone string `json:"timezone" example:"America/Guayaquil"`
	// Today is the platform day the reading was taken on, never inside the
	// span.
	Today string `json:"today" example:"2026-07-07"`
	// Currency is the Organization's.
	Currency string `json:"currency" example:"USD"`
	// Span is the span as resolved: a custom range reaching today or later
	// ends at yesterday here.
	Span OrganizationTrendSpan `json:"span"`
	// Comparison is the span every figure is set beside, bucketed the same
	// way; null for all time, which has none.
	Comparison *OrganizationTrendComparison `json:"comparison"`
	// Granularity is the length of every bucket.
	Granularity OrganizationTrendGranularity `json:"granularity" enums:"day,month"`
	// Figures carries one entry per figure.
	Figures OrganizationTrendFigures `json:"figures"`
}

// OrganizationTrendSpan is a resolved span's preset, its first and last day,
// both included, and its buckets.
type OrganizationTrendSpan struct {
	Preset   string `json:"preset" enums:"7d,30d,90d,12m,all,custom"`
	FirstDay string `json:"first_day" example:"2026-06-07"`
	LastDay  string `json:"last_day" example:"2026-07-06"`
	// Buckets is every bucket of the span, oldest first: one entry per entry
	// of every series' buckets, at the same index.
	Buckets []OrganizationTrendDays `json:"buckets"`
}

// OrganizationTrendDays is the days one bucket covers, both included, and
// whether it is partial: a month the span cuts.
type OrganizationTrendDays struct {
	FirstDay string `json:"first_day" example:"2026-07-01"`
	LastDay  string `json:"last_day" example:"2026-07-06"`
	Partial  bool   `json:"partial"`
}

// OrganizationTrendComparison is the comparison span: its kind and its first
// and last day, both included.
type OrganizationTrendComparison struct {
	Kind     OrganizationTrendComparisonKind `json:"kind" enums:"previous,year"`
	FirstDay string                          `json:"first_day" example:"2026-05-08"`
	LastDay  string                          `json:"last_day" example:"2026-06-06"`
}

// OrganizationTrendFigures is every figure Organization Trends states.
type OrganizationTrendFigures struct {
	// TicketsSold is the quantity over active Ticket Sales, by the platform
	// day each was made, one series per Sales Channel.
	TicketsSold OrganizationTrendFigure `json:"tickets_sold"`
	// TicketSales is how many active Ticket Sales were made, by the platform
	// day each was made, in one series keyed all.
	TicketSales OrganizationTrendFigure `json:"ticket_sales"`
	// Takings is what the active Ticket Sales took, in the Organization's
	// minor units, by the platform day each was made, one series per Sales
	// Channel: Sales Trends' Takings (ADR 0040), summed across the
	// Organization. Null for Event Staff, who are handed no money.
	Takings *OrganizationTrendFigure `json:"takings"`
}

// OrganizationTrendFigure is one figure: its total over the span, the
// comparison span's, the change between them, and its series. Today is in
// neither total.
type OrganizationTrendFigure struct {
	Total int `json:"total"`
	// ComparisonTotal is null for all time.
	ComparisonTotal *int `json:"comparison_total"`
	// Change is a signed ratio (0.5 for half as much again), null when the
	// comparison total is zero or there is no comparison.
	Change *float64                  `json:"change"`
	Series []OrganizationTrendSeries `json:"series"`
}

// OrganizationTrendSeries is one series of a figure.
type OrganizationTrendSeries struct {
	// Key is the Sales Channel, or all.
	Key string `json:"key" example:"online"`
	// Total is the series' total over the span, today excluded.
	Total int `json:"total"`
	// Buckets is one entry per bucket of the span, zero where nothing sold.
	Buckets []OrganizationTrendBucket `json:"buckets"`
	// Today is today alone, flagged partial and in no total, when the span
	// runs up to it; null otherwise.
	Today *OrganizationTrendBucket `json:"today"`
	// ComparisonTotal is null for all time.
	ComparisonTotal *int     `json:"comparison_total"`
	Change          *float64 `json:"change"`
	// ComparisonBuckets is one entry per entry of Buckets, at the same index,
	// over the comparison span; empty for all time.
	ComparisonBuckets []OrganizationTrendBucket `json:"comparison_buckets"`
}

// OrganizationTrendBucket is one bucket's days, both included, its value and
// whether it is partial: today, or a month the span cuts.
type OrganizationTrendBucket struct {
	FirstDay string `json:"first_day" example:"2026-07-01"`
	LastDay  string `json:"last_day" example:"2026-07-01"`
	Value    int    `json:"value"`
	Partial  bool   `json:"partial"`
}

// Trends reads Organization Trends over the span the query names, for the
// reader's Organization. It reports false for a custom range with no closed
// day in it, which the handler refuses.
func (s *Service) Trends(ctx context.Context, reader Reader, query TrendSpanQuery) (*OrganizationTrends, bool, error) {
	origin, err := s.sales.OrganizationOrigin(ctx, reader.OrganizationID)
	if err != nil {
		return nil, false, err
	}
	compare := trendspan.ComparePrevious
	if query.Compare == CompareYear {
		compare = trendspan.CompareYearEarlier
	}
	span, ok := s.ResolveSpan(trendspan.Query{
		Preset: query.Preset, From: query.From, To: query.To, Compare: compare,
		Since: allTimeSince(origin),
	})
	if !ok {
		return nil, false, nil
	}

	ticketsByChannel := make(map[string]map[string]int, len(salesChannels))
	takingsByChannel := make(map[string]map[string]int, len(salesChannels))
	for _, channel := range salesChannels {
		ticketsByChannel[channel] = map[string]int{}
		takingsByChannel[channel] = map[string]int{}
	}
	salesByDay := map[string]int{}
	// The span, with today, and the comparison span are read apart, so a
	// year-earlier comparison does not read the year between. Where the two
	// overlap (a custom range longer than a year) the same cells are read
	// twice, so each is set, never added.
	seen := map[[2]string]bool{}
	for _, window := range readWindows(span) {
		days, err := s.sales.OrganizationSalesByDay(ctx, reader.OrganizationID, window.from, window.until, platform.EcuadorTimeZone)
		if err != nil {
			return nil, false, err
		}
		for _, day := range days {
			cell := [2]string{day.Day, day.Channel}
			if seen[cell] {
				continue
			}
			seen[cell] = true
			if ticketsByChannel[day.Channel] == nil {
				ticketsByChannel[day.Channel] = map[string]int{}
				takingsByChannel[day.Channel] = map[string]int{}
			}
			ticketsByChannel[day.Channel][day.Day] += day.TicketsSold
			takingsByChannel[day.Channel][day.Day] += day.TakingsCents
			salesByDay[day.Day] += day.TicketSales
		}
	}

	ticketSeries := make([]trendspan.Series, 0, len(salesChannels))
	takingsSeries := make([]trendspan.Series, 0, len(salesChannels))
	keys := make([]string, 0, len(salesChannels))
	for _, channel := range salesChannels {
		ticketSeries = append(ticketSeries, span.Series(ticketsByChannel[channel]))
		takingsSeries = append(takingsSeries, span.Series(takingsByChannel[channel]))
		keys = append(keys, channel)
	}

	out := &OrganizationTrends{
		Timezone:    platform.EcuadorTimeZone,
		Today:       span.Today.Format(trendspan.DayFormat),
		Currency:    origin.Currency,
		Span:        spanOf(span),
		Granularity: OrganizationTrendGranularity(span.Granularity),
		Figures: OrganizationTrendFigures{
			TicketsSold: figureOf(keys, ticketSeries),
			TicketSales: figureOf([]string{countSeriesKey}, []trendspan.Series{span.Series(salesByDay)}),
		},
	}
	if reader.seesTakings() {
		takings := figureOf(keys, takingsSeries)
		out.Figures.Takings = &takings
	}
	if compared, ok := span.Compared(); ok {
		kind := ComparePrevious
		if span.Comparison == trendspan.CompareYearEarlier {
			kind = CompareYear
		}
		out.Comparison = &OrganizationTrendComparison{
			Kind:     kind,
			FirstDay: compared.FirstDay.Format(trendspan.DayFormat),
			LastDay:  compared.LastDay.Format(trendspan.DayFormat),
		}
	}
	return out, true, nil
}

// allTimeSince is the day all time starts from: the earlier of the
// Organization's creation and its earliest Ticket Sale's sale day, each on
// the platform clock, as trendspan.Query.Since takes it.
func allTimeSince(origin salessvc.OrganizationOrigin) time.Time {
	since := trendspan.CivilDate(origin.CreatedAt)
	if origin.FirstSoldAt != nil {
		if first := trendspan.CivilDate(*origin.FirstSoldAt); first.Before(since) {
			since = first
		}
	}
	return since
}

type readWindow struct {
	from, until time.Time
}

// readWindows is the instants a reading reads sales over: the span through
// the end of today when it runs up to today, and the comparison span.
func readWindows(span trendspan.Span) []readWindow {
	last := span.LastDay
	if span.EndsYesterday() {
		last = span.Today
	}
	windows := []readWindow{{from: platform.StartOfEcuadorDate(span.FirstDay), until: platform.StartOfEcuadorDate(last.AddDate(0, 0, 1))}}
	if compared, ok := span.Compared(); ok {
		windows = append(windows, readWindow{
			from:  platform.StartOfEcuadorDate(compared.FirstDay),
			until: platform.StartOfEcuadorDate(compared.LastDay.AddDate(0, 0, 1)),
		})
	}
	return windows
}

func spanOf(span trendspan.Span) OrganizationTrendSpan {
	buckets := span.Buckets()
	out := OrganizationTrendSpan{
		Preset:   string(span.Preset),
		FirstDay: span.FirstDay.Format(trendspan.DayFormat),
		LastDay:  span.LastDay.Format(trendspan.DayFormat),
		Buckets:  make([]OrganizationTrendDays, 0, len(buckets)),
	}
	for _, b := range buckets {
		out.Buckets = append(out.Buckets, OrganizationTrendDays{
			FirstDay: b.First.Format(trendspan.DayFormat), LastDay: b.Last.Format(trendspan.DayFormat), Partial: b.Partial,
		})
	}
	return out
}

// figureOf states a figure from its series, keyed in the same order: the
// figure's totals are its series' summed.
func figureOf(keys []string, series []trendspan.Series) OrganizationTrendFigure {
	figure := OrganizationTrendFigure{Series: make([]OrganizationTrendSeries, 0, len(series))}
	var comparisonTotal *int
	for i, s := range series {
		figure.Total += s.Total
		if s.ComparisonTotal != nil {
			if comparisonTotal == nil {
				comparisonTotal = new(int)
			}
			*comparisonTotal += *s.ComparisonTotal
		}
		stated := OrganizationTrendSeries{
			Key:               keys[i],
			Total:             s.Total,
			Buckets:           bucketsOf(s.Buckets),
			ComparisonTotal:   s.ComparisonTotal,
			Change:            changeOf(s.Total, s.ComparisonTotal),
			ComparisonBuckets: bucketsOf(s.Comparison),
		}
		if s.Today != nil {
			today := bucketOf(*s.Today)
			stated.Today = &today
		}
		figure.Series = append(figure.Series, stated)
	}
	figure.ComparisonTotal = comparisonTotal
	figure.Change = changeOf(figure.Total, comparisonTotal)
	return figure
}

func bucketsOf(valued []trendspan.Valued) []OrganizationTrendBucket {
	out := make([]OrganizationTrendBucket, 0, len(valued))
	for _, v := range valued {
		out = append(out, bucketOf(v))
	}
	return out
}

func bucketOf(v trendspan.Valued) OrganizationTrendBucket {
	return OrganizationTrendBucket{
		FirstDay: v.First.Format(trendspan.DayFormat),
		LastDay:  v.Last.Format(trendspan.DayFormat),
		Value:    v.Value,
		Partial:  v.Partial,
	}
}

// changeOf is the signed ratio of a total against its comparison's, nil when
// there is no comparison or it came to zero, which has no ratio.
func changeOf(total int, compared *int) *float64 {
	if compared == nil || *compared == 0 {
		return nil
	}
	change := float64(total-*compared) / float64(*compared)
	return &change
}
