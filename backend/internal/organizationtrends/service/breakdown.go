package service

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/trendspan"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// The breakdown of Organization Trends (#864): one bucket of one figure,
// opened into the Events that made it. Each figure is read by the definition
// the reading counts it by, over the instants of the bucket's days, so the
// Events add up to what the reading states for the bucket.

// OrganizationTrendFigureName names a figure of Organization Trends as the breakdown
// takes it: its name in OrganizationTrendFigures.
type OrganizationTrendFigureName string

const (
	FigureTakings     OrganizationTrendFigureName = "takings"
	FigureTicketsSold OrganizationTrendFigureName = "tickets_sold"
	FigureTicketSales OrganizationTrendFigureName = "ticket_sales"
	FigurePageViews   OrganizationTrendFigureName = "page_views"
	FigureEventsHeld  OrganizationTrendFigureName = "events_held"
)

// OrganizationTrendFigureNames is every figure a caller may ask the breakdown of, in the
// order the page draws them.
var OrganizationTrendFigureNames = []OrganizationTrendFigureName{FigureTakings, FigureTicketsSold, FigureTicketSales, FigurePageViews, FigureEventsHeld}

// BucketQuery is a bucket as the caller named it, already checked for shape
// by the handler: its first day and, for a month, its last, no earlier than
// the first and in the same calendar month. A nil LastDay, or one equal to
// FirstDay, names a day.
type BucketQuery struct {
	FirstDay time.Time
	LastDay  *time.Time
}

// Bucket is a resolved bucket: its whole days on the platform clock, its
// length, and whether it is today, a day still running.
type Bucket struct {
	FirstDay    time.Time
	LastDay     time.Time
	Granularity OrganizationTrendGranularity
	Partial     bool
}

// ResolveBucket resolves a bucket query against today on the service clock,
// and reports false for a bucket no reading states.
func (s *Service) ResolveBucket(query BucketQuery) (Bucket, bool) {
	return ResolveBucket(query, s.now())
}

// ResolveBucket turns a query into the bucket it names on the platform
// clock. A day is itself, and today is allowed, as the partial today the
// reading sets beside a span. A month bucket is the days named, the whole
// month or the part of it a span cuts, and must end before today: a span ends
// at the close of yesterday, so no month bucket of a reading holds today.
func ResolveBucket(query BucketQuery, now time.Time) (Bucket, bool) {
	today := trendspan.CivilDate(now)
	if query.LastDay == nil || query.LastDay.Equal(query.FirstDay) {
		if query.FirstDay.After(today) {
			return Bucket{}, false
		}
		return Bucket{FirstDay: query.FirstDay, LastDay: query.FirstDay, Granularity: GranularityDay, Partial: query.FirstDay.Equal(today)}, true
	}
	if !query.LastDay.Before(today) {
		return Bucket{}, false
	}
	return Bucket{FirstDay: query.FirstDay, LastDay: *query.LastDay, Granularity: GranularityMonth}, true
}

// window is the instants the bucket covers, [from, until): the days the
// reading counts the bucket over.
func (b Bucket) window() (from, until time.Time) {
	return platform.StartOfEcuadorDate(b.FirstDay), platform.StartOfEcuadorDate(b.LastDay.AddDate(0, 0, 1))
}

// OrganizationTrendBreakdown is what one bucket of one figure was made of.
type OrganizationTrendBreakdown struct {
	// Timezone names the platform clock the bucket's days are read on.
	Timezone string `json:"timezone" example:"America/Guayaquil"`
	// Figure is the figure broken down.
	Figure OrganizationTrendFigureName `json:"figure" enums:"takings,tickets_sold,ticket_sales,page_views,events_held"`
	// Granularity is the length of a bucket: a day, or a calendar month,
	// whole or cut.
	Granularity OrganizationTrendGranularity `json:"granularity"`
	// FirstDay and LastDay are the days the bucket covers, both included.
	FirstDay string `json:"first_day" example:"2026-07-01"`
	LastDay  string `json:"last_day" example:"2026-07-06"`
	// Partial is true when the bucket is today, a day still running.
	Partial bool `json:"partial"`
	// Total is what the reading states for the bucket, every Sales Channel
	// summed: the Events add up to it. Takings is in the Organization's
	// minor units.
	Total int `json:"total"`
	// Events is the Events that contributed, largest first, then by name.
	// An Event with nothing for the figure is left out. For Events held each
	// counts one and they come earliest start first.
	Events []OrganizationTrendBreakdownEvent `json:"events"`
}

// OrganizationTrendBreakdownEvent is one Event's part of a bucket.
type OrganizationTrendBreakdownEvent struct {
	// EventID is what the Event's page is linked on.
	EventID string `json:"event_id"`
	Name    string `json:"name"`
	Value   int    `json:"value"`
}

// Breakdown reads what one bucket of one figure was made of, for the
// reader's Organization. Takings is refused to a reader who is not handed
// Takings in the reading, Event Staff, with the Forbidden error: they never
// obtain money through it.
func (s *Service) Breakdown(ctx context.Context, reader Reader, figure OrganizationTrendFigureName, bucket Bucket) (*OrganizationTrendBreakdown, error) {
	if figure == FigureTakings && !reader.seesTakings() {
		return nil, identity.ErrForbidden()
	}
	from, until := bucket.window()
	var events []OrganizationTrendBreakdownEvent
	switch figure {
	case FigureTakings, FigureTicketsSold, FigureTicketSales:
		sold, err := s.sales.OrganizationSalesByEvent(ctx, reader.OrganizationID, from, until)
		if err != nil {
			return nil, err
		}
		value := salesValue(figure)
		for _, event := range sold {
			events = append(events, OrganizationTrendBreakdownEvent{EventID: event.EventID, Name: event.EventName, Value: value(event)})
		}
	case FigurePageViews:
		loaded, err := s.pageViews.OrganizationPageViewsByEvent(ctx, reader.OrganizationID, from, until)
		if err != nil {
			return nil, err
		}
		events = eventParts(loaded)
	case FigureEventsHeld:
		held, err := s.events.OrganizationEventsHeld(ctx, reader.OrganizationID, from, until)
		if err != nil {
			return nil, err
		}
		events = eventParts(held)
	}

	out := &OrganizationTrendBreakdown{
		Timezone:    platform.EcuadorTimeZone,
		Figure:      figure,
		Granularity: bucket.Granularity,
		FirstDay:    bucket.FirstDay.Format(trendspan.DayFormat),
		LastDay:     bucket.LastDay.Format(trendspan.DayFormat),
		Partial:     bucket.Partial,
		Events:      make([]OrganizationTrendBreakdownEvent, 0, len(events)),
	}
	for _, event := range events {
		if event.Value == 0 {
			continue
		}
		out.Total += event.Value
		out.Events = append(out.Events, event)
	}
	if figure != FigureEventsHeld {
		slices.SortFunc(out.Events, func(a, b OrganizationTrendBreakdownEvent) int {
			return cmp.Or(cmp.Compare(b.Value, a.Value), cmp.Compare(a.Name, b.Name), cmp.Compare(a.EventID, b.EventID))
		})
	}
	return out, nil
}

// salesValue is what a figure read off Ticket Sales takes from an Event.
func salesValue(figure OrganizationTrendFigureName) func(salessvc.OrganizationEventSales) int {
	switch figure {
	case FigureTakings:
		return func(event salessvc.OrganizationEventSales) int { return event.TakingsCents }
	case FigureTicketSales:
		return func(event salessvc.OrganizationEventSales) int { return event.TicketSales }
	default:
		return func(event salessvc.OrganizationEventSales) int { return event.TicketsSold }
	}
}

// eventParts states each Event a module counted as a part of a bucket, in
// the order the module returned them.
func eventParts(counted []platform.EventCount) []OrganizationTrendBreakdownEvent {
	out := make([]OrganizationTrendBreakdownEvent, 0, len(counted))
	for _, event := range counted {
		out = append(out, OrganizationTrendBreakdownEvent{EventID: event.EventID, Name: event.EventName, Value: event.Count})
	}
	return out
}
