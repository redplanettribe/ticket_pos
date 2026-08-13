package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// dayFormat is the calendar-date layout the Sales Trends span speaks in. A day
// is a civil date in the Event's timezone, never an instant, so it travels as a
// string and is parsed back only to step to the next one.
const dayFormat = "2006-01-02"

// SalesTrends is the whole Sales Trends surface in one payload: the Event's
// selling life as a contiguous run of days, each split by Ticket Type into
// tickets sold and Takings earned (ADR 0040).
//
// It is deliberately one response rather than several. The tab draws two charts
// over the same matrix and toggles Ticket Types client-side, so the whole
// day × Ticket Type grid arrives at once and no interaction waits on the
// network. There is no pagination: this is a bounded aggregate, not a list.
type SalesTrends struct {
	// Timezone is the zone the days are counted in — the Event's own, or UTC
	// where it carries none — stated so the reader knows whose day a day is.
	Timezone string `json:"timezone"`
	// Currency is the Organization's currency, which Takings is denominated in.
	Currency string `json:"currency"`
	// TicketTypes is the Event's WHOLE catalog in display order, including types
	// that have sold nothing, so a legend built from it is stable and its chip
	// set does not change shape as sales arrive.
	TicketTypes []TrendsTicketType `json:"ticket_types"`
	// Days is every calendar day from the first sale to the end of the span,
	// contiguous and zero-filled: a silent day is present carrying no lines, so
	// a quiet fortnight reads as a quiet fortnight rather than as two adjacent
	// bars. Empty on an Event that has sold nothing.
	Days []TrendsDay `json:"days"`
	// ReversedCount is how many of the Event's Ticket Sales are reversed, across
	// the whole Event. Reversed sales are excluded from every figure above, so
	// this is what tells an organizer a bar shrank rather than letting it shrink
	// silently (ADR 0018).
	ReversedCount int `json:"reversed_count"`
}

// TrendsTicketType is one Ticket Type of the Event's catalog as the chart's
// legend needs it: what to call it and where it sits in display order.
type TrendsTicketType struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

// TrendsDay is one calendar day of the span, in the Event's timezone. Lines
// carries only the Ticket Types that sold that day — a type that sold nothing is
// omitted rather than sent as a zero — and is empty, never null, on a silent
// day.
type TrendsDay struct {
	Date  string       `json:"date"`
	Lines []TrendsLine `json:"lines"`
}

// TrendsLine is what one Ticket Type did on one day: tickets moved, and the
// Takings they earned the Organization.
type TrendsLine struct {
	TicketTypeID string `json:"ticket_type_id"`
	Quantity     int    `json:"quantity"`
	TakingsCents int    `json:"takings_cents"`
}

// EventSalesTrends returns the Event's per-day tickets and Takings by Ticket
// Type. It is read-only and scoped to the acting Member's Organization; the
// caller's role is gated at the route (Org Admin and Event Owner only — the
// Event's money is not for hired door staff, which is the Sales summary's guard
// rather than the Sales list's).
//
// The aggregate is one query; the gaps between selling days are filled here,
// because a day that sold nothing has no row to return and inventing one in SQL
// (generate_series over a span the query would have to compute twice) buys
// nothing a loop does not.
func (s *Service) EventSalesTrends(ctx context.Context, actor ActorContext, eventID string) (*SalesTrends, error) {
	event, ok, err := s.repo.GetEventImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	// The Sales Export's own helper, so the day a sale falls on is the same day
	// on every surface that buckets one. It defaults to UTC where the Event
	// carries no timezone or an unrecognised one, and its resolved name is what
	// the query buckets by, so Go and Postgres cannot part company over which
	// day a sale is on.
	loc := resolveEventLocation(event.Timezone)

	buckets, err := s.repo.SalesTrends(ctx, actor.OrganizationID, eventID, loc.String())
	if err != nil {
		return nil, err
	}

	catalog, err := s.repo.ListEventTicketTypes(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	types := make([]TrendsTicketType, 0, len(catalog))
	for _, tt := range catalog {
		types = append(types, TrendsTicketType{ID: tt.ID, Name: tt.Name, SortOrder: tt.SortOrder})
	}

	// Read independently of everything above, and across the whole Event: the
	// question it answers is whether anything here was reversed, which a figure
	// derived from the days on screen could not answer.
	reversedCount, err := s.repo.ReversedSalesCount(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}

	return &SalesTrends{
		Timezone:      loc.String(),
		Currency:      event.Currency,
		TicketTypes:   types,
		Days:          fillTrendDays(buckets, s.now(), event.End(), loc),
		ReversedCount: reversedCount,
	}, nil
}

// fillTrendDays turns the aggregate's sparse (day, Ticket Type) buckets into the
// contiguous run of days the chart draws.
//
// The span starts on the first sale's day — an Event's selling life begins when
// it first sold, not when it was created — and ends on the earlier of today and
// the Event's last day, both read in the Event's timezone: an Event still on
// sale runs to today, and a finished one stops when it finished rather than
// being padded with empty days forever. An Event with no schedule has no end to
// stop at and runs to today.
//
// The upper bound never truncates away a day that has sales. A sale recorded
// with a sold_at after the Event finished is unusual — a Sale Import typed in
// afterwards — but dropping its day would be losing recorded money off a chart,
// which is worse than a span a few days longer than the schedule.
//
// Buckets arrive ordered by day, so the first is the first selling day.
func fillTrendDays(buckets []repository.SalesTrendBucket, now, eventEnd time.Time, loc *time.Location) []TrendsDay {
	days := make([]TrendsDay, 0, len(buckets))
	if len(buckets) == 0 {
		return days
	}

	first, err := time.ParseInLocation(dayFormat, buckets[0].Day, time.UTC)
	if err != nil {
		return days
	}
	last := dayOf(now, loc)
	if !eventEnd.IsZero() {
		if end := dayOf(eventEnd, loc); end.Before(last) {
			last = end
		}
	}
	if lastSold, err := time.ParseInLocation(dayFormat, buckets[len(buckets)-1].Day, time.UTC); err == nil && lastSold.After(last) {
		last = lastSold
	}

	byDay := make(map[string][]TrendsLine, len(buckets))
	for _, bucket := range buckets {
		byDay[bucket.Day] = append(byDay[bucket.Day], TrendsLine{
			TicketTypeID: bucket.TicketTypeID,
			Quantity:     bucket.Quantity,
			TakingsCents: bucket.TakingsCents,
		})
	}

	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		date := day.Format(dayFormat)
		lines := byDay[date]
		if lines == nil {
			// A silent day carries an empty array rather than a null: the client
			// draws an empty slot, and has nothing to defend itself against.
			lines = []TrendsLine{}
		}
		days = append(days, TrendsDay{Date: date, Lines: lines})
	}
	return days
}

// dayOf is the calendar day an instant falls on in the Event's timezone, as a
// bare date. It is compared against and stepped through by whole days, so the
// zone is dropped once the date is known — a civil date has no offset to carry
// and stepping one in a zone with a DST transition would be the classic way to
// produce a day twice or lose one.
func dayOf(instant time.Time, loc *time.Location) time.Time {
	local := instant.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}
