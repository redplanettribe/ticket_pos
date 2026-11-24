package service

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform/trendspan"
)

// The Event table of Organization Trends (#865): every Event held, loaded or
// sold in the span, ranked, beneath the charts. Each figure is read per Event
// over the span's days, by the breakdown's reads, so the rows add up to the
// span's totals; today, in no total, is in no row either.

// OrganizationTrendEvent is one row of the Event table: one Event's part of
// the span.
type OrganizationTrendEvent struct {
	// EventID is what the Event's Sales Trends and Reach are linked on.
	EventID string `json:"event_id"`
	Name    string `json:"name"`
	// StartsAt is the Event's start, null for an Event that names none.
	StartsAt *time.Time `json:"starts_at"`
	// Timezone is the Event's own, which its start is printed in. Empty
	// where the Event names none.
	Timezone string `json:"timezone" example:"America/Guayaquil"`
	// TicketsSold is the quantity over the Event's active Ticket Sales made in
	// the span, every Sales Channel.
	TicketsSold int `json:"tickets_sold"`
	// TakingsCents is the Takings of those sales (ADR 0040), in the
	// Organization's minor units. Null for Event Staff, never zero in its
	// place.
	TakingsCents *int `json:"takings_cents"`
	// PageViews is how many times the Event's page was loaded in the span.
	PageViews int `json:"page_views"`
}

// eventTable reads the Event table over the span's days, today left out:
// every Event that sold or was loaded in them, or was held in them, ranked by
// Takings for a reader who sees Takings and by Tickets Sold for one who does
// not, then by Page Views and name.
func (s *Service) eventTable(ctx context.Context, reader Reader, span trendspan.Span) ([]OrganizationTrendEvent, error) {
	from, until := Bucket{FirstDay: span.FirstDay, LastDay: span.LastDay}.window()

	sold, err := s.sales.OrganizationSalesByEvent(ctx, reader.OrganizationID, from, until)
	if err != nil {
		return nil, err
	}
	loaded, err := s.pageViews.OrganizationPageViewsByEvent(ctx, reader.OrganizationID, from, until)
	if err != nil {
		return nil, err
	}
	held, err := s.events.OrganizationEventsHeld(ctx, reader.OrganizationID, from, until)
	if err != nil {
		return nil, err
	}

	type row struct {
		ticketsSold, takings, pageViews int
	}
	rows := map[string]*row{}
	var ids []string
	rowOf := func(eventID string) *row {
		if r, ok := rows[eventID]; ok {
			return r
		}
		r := &row{}
		rows[eventID] = r
		ids = append(ids, eventID)
		return r
	}
	for _, event := range sold {
		r := rowOf(event.EventID)
		r.ticketsSold, r.takings = event.TicketsSold, event.TakingsCents
	}
	for _, event := range loaded {
		rowOf(event.EventID).pageViews = event.Count
	}
	for _, event := range held {
		rowOf(event.EventID)
	}

	starts, err := s.events.OrganizationEventStarts(ctx, reader.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]OrganizationTrendEvent, 0, len(ids))
	for _, id := range ids {
		r := rows[id]
		start := starts[id]
		event := OrganizationTrendEvent{
			EventID: id, Name: start.Name, StartsAt: start.StartsAt, Timezone: start.Timezone,
			TicketsSold: r.ticketsSold, PageViews: r.pageViews,
		}
		if reader.seesTakings() {
			takings := r.takings
			event.TakingsCents = &takings
		}
		out = append(out, event)
	}
	seesTakings := reader.seesTakings()
	slices.SortFunc(out, func(a, b OrganizationTrendEvent) int {
		byTakings := 0
		if seesTakings {
			byTakings = cmp.Compare(*b.TakingsCents, *a.TakingsCents)
		}
		return cmp.Or(byTakings,
			cmp.Compare(b.TicketsSold, a.TicketsSold),
			cmp.Compare(b.PageViews, a.PageViews),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.EventID, b.EventID))
	})
	return out, nil
}
