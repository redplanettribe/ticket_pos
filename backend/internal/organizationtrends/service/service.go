// Package service implements Home's reading of an Organization (#858, ADR
// 0081): what stands now, and, beneath it, Organization Trends.
//
// It is the Organization's counterpart to Platform Trends and is modelled on
// it, with two differences ADR 0081 sets down. It reads one Organization, the
// Active Member's, and never the platform. And it reads live, the way Sales
// Trends does, freezing nothing: a sale counts on the platform day it was
// made, and a reversed one counts on no day. It owns no table; every figure
// is read through the module that owns its rows and composed here into the
// payload the page draws, so the page derives nothing.
//
// Every day here is the platform's day (platform.EcuadorTimeZone), never an
// Event's own, so the Events of an Organization add up to it.
//
// Money is withheld here and not by the page: an Event Staff Member is handed
// no Takings, and anyone but an Org Admin no Payable Balance. Absent is null,
// never zero.
package service

import (
	"context"
	"time"

	catalogsvc "github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// Member roles, as the Active Member carries them.
const (
	roleOrgAdmin   = "org_admin"
	roleEventOwner = "event_owner"
)

// TicketSales is what Home needs from sales: an Organization's Tickets Sold
// and Takings over a window, one Event's Tickets Sold so far, and the
// Organization's balances as the Payouts page reads them.
type TicketSales interface {
	OrganizationSalesBetween(ctx context.Context, orgID string, from, until time.Time) (salessvc.OrganizationSales, error)
	EventTicketsSold(ctx context.Context, orgID, eventID string) (int, error)
	OrganizationBalances(ctx context.Context, orgID string) (salessvc.OrganizationBalances, error)
}

// Events is what Home needs from catalog: how many Events and Active Events
// an Organization has, and which Event is next.
type Events interface {
	OrganizationEventCounts(ctx context.Context, orgID string, now time.Time) (catalogsvc.OrganizationEventCounts, error)
	OrganizationNextEvent(ctx context.Context, orgID string, now time.Time) (catalogsvc.NextEvent, bool, error)
}

// Reader is who is reading Home: the Active Member's Organization and role.
type Reader struct {
	OrganizationID string
	Role           string
}

// seesTakings says whether the reader may be handed Takings: an Org Admin or
// an Event Owner, the guard an Event's money carries everywhere else.
func (r Reader) seesTakings() bool {
	return r.Role == roleOrgAdmin || r.Role == roleEventOwner
}

// seesPayableBalance says whether the reader may be handed the Payable
// Balance: an Org Admin alone, the guard of Payouts.
func (r Reader) seesPayableBalance() bool {
	return r.Role == roleOrgAdmin
}

// Service reads Home.
type Service struct {
	sales  TicketSales
	events Events
	now    func() time.Time
}

// New returns a Home service reading through the given modules.
func New(sales TicketSales, events Events) *Service {
	return &Service{sales: sales, events: events, now: time.Now}
}

// WithClock overrides the clock now is read from (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// HomeNow is what stands now across the Organization: the top of Home.
type HomeNow struct {
	// Timezone names the platform clock today is read on.
	Timezone string `json:"timezone" example:"America/Guayaquil"`
	// Today is the platform day the reading was taken on, YYYY-MM-DD.
	Today string `json:"today" example:"2026-07-07"`
	// Currency is the Organization's, which every money figure is stated in.
	Currency string `json:"currency" example:"USD"`
	// HasEvents is false for an Organization that has no Event at all, in
	// any status.
	HasEvents bool `json:"has_events"`
	// ActiveEvents is how many of the Organization's Events are Active
	// Events: neither cancelled nor over, drafts included.
	ActiveEvents int `json:"active_events"`
	// NextEvent is the published, non-cancelled Event with the earliest
	// start that is not over, an Ongoing one included. Null when there is
	// none.
	NextEvent *HomeNextEvent `json:"next_event"`
	// TodaySoFar is today's sales so far, which today being unfinished makes
	// partial.
	TodaySoFar HomeTodaySoFar `json:"today_so_far"`
	// PayableBalanceCents is the Payable Balance, as the Payouts page states
	// it, in the Organization's minor units and signed. Null for anyone but
	// an Org Admin.
	PayableBalanceCents *int `json:"payable_balance_cents"`
}

// HomeNextEvent is the Event Home names as next.
type HomeNextEvent struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	// Timezone is the Event's own, which its start is printed in. Empty
	// where the Event names none.
	Timezone string `json:"timezone" example:"America/Guayaquil"`
	// TicketsSold is the Event's Tickets Sold so far, on every Sales
	// Channel, reversed sales left out.
	TicketsSold int `json:"tickets_sold"`
	// Ongoing is true while the Event has started and not yet ended.
	Ongoing bool `json:"ongoing"`
}

// HomeTodaySoFar is the Organization's sales made today on the platform
// clock so far.
type HomeTodaySoFar struct {
	// Partial is always true: today has not ended.
	Partial bool `json:"partial"`
	// TicketsSold is the quantity over today's active Ticket Sales.
	TicketsSold int `json:"tickets_sold"`
	// TakingsCents is the Takings of today's active Ticket Sales, in the
	// Organization's minor units. Null for Event Staff.
	TakingsCents *int `json:"takings_cents"`
}

// Now reads what stands now across the reader's Organization.
func (s *Service) Now(ctx context.Context, reader Reader) (*HomeNow, error) {
	now := s.now()
	dayStart := platform.StartOfEcuadorDay(now)
	dayEnd := platform.StartOfEcuadorDay(dayStart.Add(36 * time.Hour))

	counts, err := s.events.OrganizationEventCounts(ctx, reader.OrganizationID, now)
	if err != nil {
		return nil, err
	}
	today, err := s.sales.OrganizationSalesBetween(ctx, reader.OrganizationID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}

	out := &HomeNow{
		Timezone:     platform.EcuadorTimeZone,
		Today:        platform.EcuadorDate(dayStart),
		Currency:     today.Currency,
		HasEvents:    counts.Events > 0,
		ActiveEvents: counts.ActiveEvents,
		TodaySoFar:   HomeTodaySoFar{Partial: true, TicketsSold: today.TicketsSold},
	}
	if reader.seesTakings() {
		takings := today.TakingsCents
		out.TodaySoFar.TakingsCents = &takings
	}

	next, ok, err := s.events.OrganizationNextEvent(ctx, reader.OrganizationID, now)
	if err != nil {
		return nil, err
	}
	if ok {
		sold, err := s.sales.EventTicketsSold(ctx, reader.OrganizationID, next.ID)
		if err != nil {
			return nil, err
		}
		out.NextEvent = &HomeNextEvent{
			ID: next.ID, Name: next.Name, StartsAt: next.StartsAt.UTC(), Timezone: next.Timezone,
			TicketsSold: sold, Ongoing: next.Ongoing,
		}
	}

	if reader.seesPayableBalance() {
		balances, err := s.sales.OrganizationBalances(ctx, reader.OrganizationID)
		if err != nil {
			return nil, err
		}
		payable := balances.PayableBalanceCents
		out.PayableBalanceCents = &payable
	}
	return out, nil
}
