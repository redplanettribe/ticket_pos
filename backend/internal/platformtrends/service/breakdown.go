package service

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// BreakdownFigure names a figure of Platform Trends as the breakdown read
// takes it: the name the figure has in PlatformTrendFigures.
type BreakdownFigure string

const (
	FigureTicketsSold          BreakdownFigure = "tickets_sold"
	FigureMoneySold            BreakdownFigure = "money_sold"
	FigurePlatformFeeEarned    BreakdownFigure = "platform_fee_earned"
	FigurePageViews            BreakdownFigure = "page_views"
	FigureEventsPublished      BreakdownFigure = "events_published"
	FigureNewOrganizations     BreakdownFigure = "new_organizations"
	FigureNewVerifiedCustomers BreakdownFigure = "new_verified_customers"
	FigureActiveOrganizations  BreakdownFigure = "active_organizations"
	FigureActiveCustomers      BreakdownFigure = "active_customers"
)

// BreakdownFigures is every figure a caller may ask the breakdown of, in the
// order PlatformTrendFigures states them.
var BreakdownFigures = []BreakdownFigure{
	FigureTicketsSold, FigureMoneySold, FigurePlatformFeeEarned,
	FigureNewVerifiedCustomers, FigureNewOrganizations, FigurePageViews, FigureEventsPublished,
	FigureActiveOrganizations, FigureActiveCustomers,
}

// Granularities is every bucket length a caller may name, the default first.
var Granularities = []PlatformTrendGranularity{GranularityDay, GranularityWeek}

// WeekDays is how many days a weekly bucket covers unless it is the stub a
// span may begin with.
const WeekDays = 7

// BucketQuery is a bucket as the caller named it, already checked for shape
// by the handler: its first day and its length. LastDay is set only for a
// weekly bucket shorter than seven days, the stub a custom range may begin
// with, and is then no earlier than FirstDay and within the week.
type BucketQuery struct {
	FirstDay    time.Time
	LastDay     *time.Time
	Granularity PlatformTrendGranularity
}

// Bucket is a resolved bucket: its whole days on the platform clock, and
// whether it is today, a day still running.
type Bucket struct {
	FirstDay    time.Time
	LastDay     time.Time
	Granularity PlatformTrendGranularity
	Partial     bool
}

// ResolveBucket resolves a bucket query against today on the service clock,
// and reports false for a bucket the first read could not have stated: a day
// after today, or a week that does not end before today.
func (s *Service) ResolveBucket(query BucketQuery) (Bucket, bool) {
	return ResolveBucket(query, s.now())
}

// ResolveBucket turns a query into the bucket it names on the platform
// clock. A day is itself, and today is allowed, as the partial bucket the
// first read sets beside a span. A week is the seven days from its first
// day, or the days up to LastDay when the query names one, and must end
// before today: a span ends at the close of yesterday, so no weekly bucket
// of the first read holds today.
func ResolveBucket(query BucketQuery, now time.Time) (Bucket, bool) {
	today := civilDate(now)
	if query.Granularity != GranularityWeek {
		if query.FirstDay.After(today) {
			return Bucket{}, false
		}
		return Bucket{FirstDay: query.FirstDay, LastDay: query.FirstDay, Granularity: GranularityDay, Partial: query.FirstDay.Equal(today)}, true
	}
	last := query.FirstDay.AddDate(0, 0, WeekDays-1)
	if query.LastDay != nil {
		last = *query.LastDay
	}
	if !last.Before(today) {
		return Bucket{}, false
	}
	return Bucket{FirstDay: query.FirstDay, LastDay: last, Granularity: GranularityWeek}, true
}

// window is the instants the bucket covers, [from, until): the read every
// figure's breakdown is bounded by, which is the window the first read
// counts the same days over.
func (b Bucket) window() (from, until time.Time) {
	return platform.StartOfEcuadorDate(b.FirstDay), platform.StartOfEcuadorDate(b.LastDay.AddDate(0, 0, 1))
}

// PlatformTrendBreakdown is what one bucket of one figure was made of.
type PlatformTrendBreakdown struct {
	// Timezone names the platform clock the bucket's days are read on.
	Timezone string `json:"timezone" example:"America/Guayaquil"`
	// Figure is the figure broken down.
	Figure BreakdownFigure `json:"figure" enums:"tickets_sold,money_sold,platform_fee_earned,new_verified_customers,new_organizations,page_views,events_published,active_organizations,active_customers"`
	// Granularity is the bucket's length.
	Granularity PlatformTrendGranularity `json:"granularity"`
	// FirstDay and LastDay are the days the bucket covers, both included.
	FirstDay string `json:"first_day" example:"2026-07-06"`
	LastDay  string `json:"last_day" example:"2026-07-06"`
	// Partial is true when the bucket is today, a day still running.
	Partial bool `json:"partial"`
	// Available is false for a figure whose breakdown is not available yet,
	// the two activity figures. Totals, Organizations and Events are then
	// empty, which says nothing about the bucket.
	Available bool `json:"available"`
	// Totals is what the first read states for the bucket: one total keyed
	// "all" for a count, summed across Sales Channels for Tickets Sold, and
	// one per currency an Organization is in for a money figure, in the
	// currency's minor units, zero or not. The contributors keyed the same
	// add up to it.
	Totals []PlatformTrendBreakdownTotal `json:"totals"`
	// Organizations is the Organizations that contributed, ranked, largest
	// first within each key. It is stated for Tickets Sold and the money
	// figures, and for new Organizations, where it is the list of the
	// Organizations created, oldest first, each counting one. Empty for
	// every other figure.
	Organizations []PlatformTrendBreakdownOrganization `json:"organizations"`
	// Events is the Events that contributed, ranked, largest first within
	// each key. It is stated for Tickets Sold, the money figures and Page
	// Views, and for Events Published, where it is the list of the Events
	// published, oldest first, each counting one. Empty for every other
	// figure. New Verified Customers states its total alone: nothing here
	// names a Customer.
	Events []PlatformTrendBreakdownEvent `json:"events"`
}

// PlatformTrendBreakdownTotal is one total of a bucket.
type PlatformTrendBreakdownTotal struct {
	// Key is "all" for a count and the currency code for a money figure.
	Key string `json:"key" example:"USD"`
	// Value is a count, or an amount in the minor units of the currency.
	Value int `json:"value"`
}

// PlatformTrendBreakdownOrganization is one Organization's part of a bucket.
type PlatformTrendBreakdownOrganization struct {
	// OrganizationID is what the Organization's operator page is linked on.
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	// Key names the total this part counts toward: "all", or the
	// Organization's currency for a money figure.
	Key   string `json:"key" example:"USD"`
	Value int    `json:"value"`
}

// PlatformTrendBreakdownEvent is one Event's part of a bucket.
type PlatformTrendBreakdownEvent struct {
	EventID string `json:"event_id"`
	Name    string `json:"name"`
	// OrganizationID is the Event's Organization, whose operator page an
	// Event links to: there is no operator page for an Event.
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	// Key names the total this part counts toward: "all", or the currency
	// of the Event's Organization for a money figure.
	Key   string `json:"key" example:"USD"`
	Value int    `json:"value"`
}

// Breakdown reads what one bucket of one figure was made of. Each figure is
// read by the definition the first read counts it by, over the instants of
// the bucket's days, so its contributors add up to what Trends states for
// the bucket.
func (s *Service) Breakdown(ctx context.Context, figure BreakdownFigure, bucket Bucket) (*PlatformTrendBreakdown, error) {
	breakdown := &PlatformTrendBreakdown{
		Timezone:      platform.EcuadorTimeZone,
		Figure:        figure,
		Granularity:   bucket.Granularity,
		FirstDay:      bucket.FirstDay.Format(dayFormat),
		LastDay:       bucket.LastDay.Format(dayFormat),
		Partial:       bucket.Partial,
		Available:     true,
		Totals:        []PlatformTrendBreakdownTotal{},
		Organizations: []PlatformTrendBreakdownOrganization{},
		Events:        []PlatformTrendBreakdownEvent{},
	}
	from, until := bucket.window()

	var err error
	switch figure {
	case FigureTicketsSold:
		err = s.breakDownSales(ctx, breakdown, from, until, []string{countSeriesKey},
			func(salessvc.PlatformEventSales) string { return countSeriesKey },
			func(event salessvc.PlatformEventSales) int { return event.Quantity })
	case FigureMoneySold, FigurePlatformFeeEarned:
		err = s.breakDownMoney(ctx, breakdown, from, until)
	case FigurePageViews:
		var events []platform.EventCount
		if events, err = s.pageViews.PlatformPageViewsByEvent(ctx, from, until); err == nil {
			breakdown.Events = rankEvents(eventParts(events))
			breakdown.Totals = totalsOf([]string{countSeriesKey}, breakdown.Events)
		}
	case FigureEventsPublished:
		var events []platform.EventCount
		if events, err = s.events.PlatformEventsPublished(ctx, from, until); err == nil {
			breakdown.Events = eventParts(events)
			breakdown.Totals = totalsOf([]string{countSeriesKey}, breakdown.Events)
		}
	case FigureNewOrganizations:
		var organizations []platform.OrganizationCount
		if organizations, err = s.organizations.PlatformNewOrganizations(ctx, from, until); err == nil {
			total := 0
			for _, organization := range organizations {
				total += organization.Count
				breakdown.Organizations = append(breakdown.Organizations, PlatformTrendBreakdownOrganization{
					OrganizationID: organization.OrganizationID, Name: organization.OrganizationName,
					Key: countSeriesKey, Value: organization.Count,
				})
			}
			breakdown.Totals = []PlatformTrendBreakdownTotal{{Key: countSeriesKey, Value: total}}
		}
	case FigureNewVerifiedCustomers:
		// The count alone, read through the same per-day count the figure
		// is: no read that names a Customer is reachable from a request.
		var days []platform.DayCount
		if days, err = s.customers.PlatformNewVerifiedCustomersByDay(ctx, from, until, platform.EcuadorTimeZone); err == nil {
			total := 0
			for _, day := range days {
				total += day.Count
			}
			breakdown.Totals = []PlatformTrendBreakdownTotal{{Key: countSeriesKey, Value: total}}
		}
	default:
		// Active Organizations and Active Customers: not available yet.
		breakdown.Available = false
	}
	if err != nil {
		return nil, err
	}
	return breakdown, nil
}

// breakDownMoney states a money figure's bucket, one total per currency an
// Organization is in, as the first read states one series per currency.
func (s *Service) breakDownMoney(ctx context.Context, breakdown *PlatformTrendBreakdown, from, until time.Time) error {
	currencies, err := s.sales.PlatformCurrencies(ctx)
	if err != nil {
		return err
	}
	value := func(event salessvc.PlatformEventSales) int { return event.SoldCents }
	if breakdown.Figure == FigurePlatformFeeEarned {
		value = func(event salessvc.PlatformEventSales) int { return event.PlatformFeeCents }
	}
	return s.breakDownSales(ctx, breakdown, from, until, currencies,
		func(event salessvc.PlatformEventSales) string { return event.Currency }, value)
}

// breakDownSales states a figure read off Ticket Sales: the Events that sold
// in the window, each with the value the figure takes from it under the key
// it counts toward, and the same rolled up by Organization. An Event with
// nothing for this figure is left out, which is how a reversed sale is
// absent from Tickets Sold and money sold while its kept fee stays in
// Platform Fee earned.
func (s *Service) breakDownSales(
	ctx context.Context, breakdown *PlatformTrendBreakdown, from, until time.Time, keys []string,
	key func(salessvc.PlatformEventSales) string, value func(salessvc.PlatformEventSales) int,
) error {
	sold, err := s.sales.PlatformSalesByEvent(ctx, from, until)
	if err != nil {
		return err
	}
	byOrganization := map[string]*PlatformTrendBreakdownOrganization{}
	for _, event := range sold {
		// A sale's currency is its Organization's, so its key is one of
		// keys; an Organization created between the two reads is stated
		// from the next read on, as in money.
		if value(event) == 0 || !slices.Contains(keys, key(event)) {
			continue
		}
		breakdown.Events = append(breakdown.Events, PlatformTrendBreakdownEvent{
			EventID: event.EventID, Name: event.EventName,
			OrganizationID: event.OrganizationID, OrganizationName: event.OrganizationName,
			Key: key(event), Value: value(event),
		})
		organization := byOrganization[event.OrganizationID]
		if organization == nil {
			organization = &PlatformTrendBreakdownOrganization{OrganizationID: event.OrganizationID, Name: event.OrganizationName, Key: key(event)}
			byOrganization[event.OrganizationID] = organization
		}
		organization.Value += value(event)
	}
	for _, organization := range byOrganization {
		breakdown.Organizations = append(breakdown.Organizations, *organization)
	}
	slices.SortFunc(breakdown.Organizations, func(a, b PlatformTrendBreakdownOrganization) int {
		return rank(a.Key, b.Key, a.Value, b.Value, a.Name, b.Name, a.OrganizationID, b.OrganizationID)
	})
	breakdown.Events = rankEvents(breakdown.Events)
	breakdown.Totals = totalsOf(keys, breakdown.Events)
	return nil
}

// eventParts states each Event a module counted as a part of a count's
// bucket, in the order the module returned them.
func eventParts(events []platform.EventCount) []PlatformTrendBreakdownEvent {
	parts := make([]PlatformTrendBreakdownEvent, 0, len(events))
	for _, event := range events {
		parts = append(parts, PlatformTrendBreakdownEvent{
			EventID: event.EventID, Name: event.EventName,
			OrganizationID: event.OrganizationID, OrganizationName: event.OrganizationName,
			Key: countSeriesKey, Value: event.Count,
		})
	}
	return parts
}

// rankEvents orders Events as a ranking: by key, then largest first.
func rankEvents(events []PlatformTrendBreakdownEvent) []PlatformTrendBreakdownEvent {
	slices.SortFunc(events, func(a, b PlatformTrendBreakdownEvent) int {
		return rank(a.Key, b.Key, a.Value, b.Value, a.Name, b.Name, a.EventID, b.EventID)
	})
	return events
}

// rank orders two contributors: by key, so the parts of one total sit
// together, then the larger value first, then by name and id, so equal
// parts keep one order from one read to the next.
func rank(keyA, keyB string, valueA, valueB int, nameA, nameB, idA, idB string) int {
	return cmp.Or(cmp.Compare(keyA, keyB), cmp.Compare(valueB, valueA), cmp.Compare(nameA, nameB), cmp.Compare(idA, idB))
}

// totalsOf adds the Events of a bucket up into one total per key, in the
// order keys gives, each always present. Every figure broken down by Event
// is the sum of its Events, so the totals are what the first read states.
func totalsOf(keys []string, events []PlatformTrendBreakdownEvent) []PlatformTrendBreakdownTotal {
	sums := make(map[string]int, len(keys))
	for _, event := range events {
		sums[event.Key] += event.Value
	}
	totals := make([]PlatformTrendBreakdownTotal, 0, len(keys))
	for _, key := range keys {
		totals = append(totals, PlatformTrendBreakdownTotal{Key: key, Value: sums[key]})
	}
	return totals
}
