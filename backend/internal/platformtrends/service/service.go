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
// Sold per day and Sales Channel, its money sold and Platform Fee earned per
// day and currency, the currencies it sells in, and the first day it sold
// anything.
type TicketSales interface {
	PlatformTicketsSoldByDay(ctx context.Context, from, until time.Time, timezone string) ([]salessvc.PlatformTicketsSoldDay, error)
	PlatformMoneyByDay(ctx context.Context, from, until time.Time, timezone string) ([]salessvc.PlatformMoneyDay, error)
	PlatformCurrencies(ctx context.Context) ([]string, error)
	PlatformFirstTicketSoldDay(ctx context.Context, timezone string) (string, bool, error)
}

// Customers is what Platform Trends needs from customers: how many Customers
// became Verified Customers per day, and the first day any did.
type Customers interface {
	PlatformNewVerifiedCustomersByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error)
	PlatformFirstVerifiedCustomerDay(ctx context.Context, timezone string) (string, bool, error)
}

// Organizations is what Platform Trends needs from identity: how many
// Organizations were created per day, and the first day any was.
type Organizations interface {
	PlatformNewOrganizationsByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error)
	PlatformFirstOrganizationDay(ctx context.Context, timezone string) (string, bool, error)
}

// PageViews is what Platform Trends needs from affiliates, which keeps the
// hourly Page View buckets: the platform's Page Views per day, and the first
// day any was counted.
type PageViews interface {
	PlatformPageViewsByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error)
	PlatformFirstPageViewDay(ctx context.Context, timezone string) (string, bool, error)
}

// salesChannels is every Sales Channel, in the order the series are stated;
// the ticket_sales CHECK constraint admits no other.
// Each is always present, zero or not, so the page's legend never changes
// shape with the data.
var salesChannels = []string{"online", "in_person", "import"}

// countSeriesKey keys the one series of a figure that is a plain count with
// nothing to split it by.
const countSeriesKey = "all"

// Service reads Platform Trends.
type Service struct {
	sales         TicketSales
	customers     Customers
	organizations Organizations
	pageViews     PageViews
	now           func() time.Time
}

// New returns a Platform Trends service reading through the given modules.
func New(sales TicketSales, customers Customers, organizations Organizations, pageViews PageViews) *Service {
	return &Service{sales: sales, customers: customers, organizations: organizations, pageViews: pageViews, now: time.Now}
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
	// MoneySold is the buyer price times the quantity, before any fee is
	// withheld, over active Ticket Sales on every Sales Channel, by the instant sold, in
	// minor units, one series per currency. Currencies are never converted or
	// added together, so the figure has no total of its own.
	MoneySold PlatformTrendFigure `json:"money_sold"`
	// PlatformFeeEarned is the Platform Fee, excluding Fee IVA, on active
	// Online Sales and on reversed ones whose fee the platform kept, by the
	// instant sold, in minor units, one series per currency. Over every
	// day up to today, today's bucket included, it is the Platform Fee the
	// operator summary states. It has no total of
	// its own, as MoneySold has none.
	PlatformFeeEarned PlatformTrendFigure `json:"platform_fee_earned"`
	// NewVerifiedCustomers is the Customers who became Verified Customers,
	// by the instant they first proved their email and never the instant
	// their record was created, in one series, "all". A Customer never
	// verified is in no bucket.
	NewVerifiedCustomers PlatformTrendFigure `json:"new_verified_customers"`
	// NewOrganizations is the Organizations created, by the instant of
	// creation, House Organizations included, in one series, "all".
	NewOrganizations PlatformTrendFigure `json:"new_organizations"`
	// PageViews is the loads of every Event's storefront page, in one
	// series, "all". It counts loads and not people, and a load that came
	// through an Affiliate Link is counted once. Nothing here is divided
	// into it: Platform Trends states no rate built on Page Views.
	PageViews PlatformTrendFigure `json:"page_views"`
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

// PlatformTrendSeries is one line of a figure: a Sales Channel for Tickets
// Sold, a currency for a money figure, the whole of a plain count.
type PlatformTrendSeries struct {
	// Key names the series: for Tickets Sold, the Sales Channel; for a money
	// figure, the currency code, whose minor units every value of the series
	// is in; "all" for a figure that is one count with nothing to split it
	// by.
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
	// Value is a count, or for a money figure an amount in the minor units
	// of the series' currency.
	Value int `json:"value"`
	// Partial is true on today's bucket alone: a day still running.
	Partial bool `json:"partial"`
}

// Trends reads every figure of Platform Trends for a resolved span and its
// comparison.
func (s *Service) Trends(ctx context.Context, span Span) (*PlatformTrends, error) {
	salesSince, err := s.salesSince(ctx)
	if err != nil {
		return nil, err
	}
	ticketsSold, err := s.ticketsSold(ctx, span, salesSince)
	if err != nil {
		return nil, err
	}
	moneySold, platformFeeEarned, err := s.money(ctx, span, salesSince)
	if err != nil {
		return nil, err
	}
	newVerifiedCustomers, err := count(ctx, span, s.customers.PlatformFirstVerifiedCustomerDay, s.customers.PlatformNewVerifiedCustomersByDay)
	if err != nil {
		return nil, err
	}
	newOrganizations, err := count(ctx, span, s.organizations.PlatformFirstOrganizationDay, s.organizations.PlatformNewOrganizationsByDay)
	if err != nil {
		return nil, err
	}
	pageViews, err := count(ctx, span, s.pageViews.PlatformFirstPageViewDay, s.pageViews.PlatformPageViewsByDay)
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
		Figures: PlatformTrendFigures{
			TicketsSold:          ticketsSold,
			MoneySold:            moneySold,
			PlatformFeeEarned:    platformFeeEarned,
			NewVerifiedCustomers: newVerifiedCustomers,
			NewOrganizations:     newOrganizations,
			PageViews:            pageViews,
		},
	}, nil
}

// salesSince is the platform day the earliest Ticket Sale, reversed or not,
// was sold, and nil when there is none: the first day every figure read off
// Ticket Sales has data, the counts and the money alike.
func (s *Service) salesSince(ctx context.Context) (*time.Time, error) {
	return firstDay(ctx, s.sales.PlatformFirstTicketSoldDay)
}

// firstDay reads the first platform day a figure has data, through the
// owning module's read of it, and is nil when the figure has none.
func firstDay(ctx context.Context, read func(ctx context.Context, timezone string) (string, bool, error)) (*time.Time, error) {
	first, ok, err := read(ctx, platform.EcuadorTimeZone)
	if err != nil || !ok {
		return nil, err
	}
	day, err := time.Parse(dayFormat, first)
	if err != nil {
		return nil, err
	}
	return &day, nil
}

// count reads a figure that is one plain count per platform day, in one
// series: new Verified Customers, new Organizations, Page Views. Each is read
// off rows the platform has kept since the figure's first day, so it is
// always exact and has no recording-began day; first names that first day
// and byDay the counts over a window.
func count(
	ctx context.Context,
	span Span,
	first func(ctx context.Context, timezone string) (string, bool, error),
	byDay func(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error),
) (PlatformTrendFigure, error) {
	dataSince, err := firstDay(ctx, first)
	if err != nil {
		return PlatformTrendFigure{}, err
	}
	perDay := map[string]int{}
	for _, window := range readWindows(span, dataSince) {
		days, err := byDay(ctx, window.from, window.until, platform.EcuadorTimeZone)
		if err != nil {
			return PlatformTrendFigure{}, err
		}
		for _, day := range days {
			perDay[day.Day] += day.Count
		}
	}
	return figureOf(span, dataSince, []string{countSeriesKey}, map[string]map[string]int{countSeriesKey: perDay}, true), nil
}

// ticketsSold reads Tickets Sold, one series per Sales Channel.
func (s *Service) ticketsSold(ctx context.Context, span Span, dataSince *time.Time) (PlatformTrendFigure, error) {
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

// money reads the two money figures, money sold and Platform Fee earned, one
// series per currency each. Every currency an Organization is in has a series
// in both, sold in or not, so the two figures and the operator summary list
// the same currencies; their series are never summed, since no currency is
// converted into another.
func (s *Service) money(ctx context.Context, span Span, dataSince *time.Time) (sold, feeEarned PlatformTrendFigure, err error) {
	currencies, err := s.sales.PlatformCurrencies(ctx)
	if err != nil {
		return PlatformTrendFigure{}, PlatformTrendFigure{}, err
	}

	soldByCurrency := make(map[string]map[string]int, len(currencies))
	feeByCurrency := make(map[string]map[string]int, len(currencies))
	for _, currency := range currencies {
		soldByCurrency[currency] = map[string]int{}
		feeByCurrency[currency] = map[string]int{}
	}
	for _, window := range readWindows(span, dataSince) {
		days, err := s.sales.PlatformMoneyByDay(ctx, window.from, window.until, platform.EcuadorTimeZone)
		if err != nil {
			return PlatformTrendFigure{}, PlatformTrendFigure{}, err
		}
		for _, day := range days {
			// A sale's currency is its Organization's, so it is one of
			// currencies; an Organization created between the two reads is
			// stated from the next read on.
			if soldByCurrency[day.Currency] == nil {
				continue
			}
			soldByCurrency[day.Currency][day.Day] += day.SoldCents
			feeByCurrency[day.Currency][day.Day] += day.PlatformFeeCents
		}
	}
	return figureOf(span, dataSince, currencies, soldByCurrency, false),
		figureOf(span, dataSince, currencies, feeByCurrency, false), nil
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

	total, comparisonTotal, hasComparison := 0, 0, false
	for _, key := range keys {
		series := seriesOf(key, span, dataSince, perSeries[key])
		total += series.Total
		if series.ComparisonTotal != nil {
			comparisonTotal += *series.ComparisonTotal
			hasComparison = true
		}
		figure.Series = append(figure.Series, series)
	}
	if summable {
		figure.Total = &total
		if hasComparison {
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
		series.Buckets = append(series.Buckets, bucket.stated(value))
	}
	if span.endsYesterday() {
		today := span.Today.Format(dayFormat)
		series.Today = &PlatformTrendBucket{FirstDay: today, LastDay: today, Value: perDay[today], Partial: true}
	}

	comparisonRanges := span.compared().buckets()
	series.ComparisonBuckets = make([]*PlatformTrendBucket, 0, len(comparisonRanges))
	comparisonTotal, hasComparison := 0, false
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
		hasComparison = true
		stated := bucket.stated(value)
		series.ComparisonBuckets = append(series.ComparisonBuckets, &stated)
	}
	if hasComparison {
		series.ComparisonTotal = &comparisonTotal
		series.Change = change(series.Total, comparisonTotal)
	}
	return series
}

// stated is a whole bucket as the response states it, carrying value.
func (b bucketRange) stated(value int) PlatformTrendBucket {
	return PlatformTrendBucket{FirstDay: b.first.Format(dayFormat), LastDay: b.last.Format(dayFormat), Value: value}
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
