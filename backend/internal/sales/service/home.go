package service

import (
	"context"
	"time"
)

// OrganizationSales is what one Organization's active Ticket Sales sold over
// a window: Tickets Sold, and Takings in Currency, the Organization's.
type OrganizationSales struct {
	Currency     string
	TicketsSold  int
	TakingsCents int
}

// OrganizationSalesBetween is one Organization's Tickets Sold and Takings over
// the active Ticket Sales sold in [from, until), read for Home (#859): by the
// instant each sale was made, reversed sales left out, Takings as Sales
// Trends computes it (ADR 0040).
func (s *Service) OrganizationSalesBetween(ctx context.Context, orgID string, from, until time.Time) (OrganizationSales, error) {
	row, err := s.repo.OrganizationSalesBetween(ctx, orgID, from, until)
	if err != nil {
		return OrganizationSales{}, err
	}
	return OrganizationSales{Currency: row.Currency, TicketsSold: row.TicketsSold, TakingsCents: row.TakingsCents}, nil
}

// EventTicketsSold is one of the Organization's Events' Tickets Sold so far,
// on every Sales Channel, reversed sales left out.
func (s *Service) EventTicketsSold(ctx context.Context, orgID, eventID string) (int, error) {
	return s.repo.EventTicketsSold(ctx, orgID, eventID)
}

// OrganizationSalesDay is how many tickets, in how many Ticket Sales, one
// Organization sold on one Sales Channel on one day, the day being a
// YYYY-MM-DD calendar date on the clock the caller asked for.
type OrganizationSalesDay struct {
	Day         string
	Channel     string
	TicketsSold int
	TicketSales int
}

// OrganizationSalesByDay is one Organization's Tickets Sold and Ticket Sales
// per day and Sales Channel over the active Ticket Sales sold in [from,
// until), read for Organization Trends (#861): by the instant each sale was
// made, on the named timezone, reversed sales left out. Days that sold
// nothing have no entry, and filling them is the caller's.
func (s *Service) OrganizationSalesByDay(ctx context.Context, orgID string, from, until time.Time, timezone string) ([]OrganizationSalesDay, error) {
	rows, err := s.repo.OrganizationSalesByDay(ctx, orgID, from, until, timezone)
	if err != nil {
		return nil, err
	}
	out := make([]OrganizationSalesDay, 0, len(rows))
	for _, row := range rows {
		out = append(out, OrganizationSalesDay{Day: row.Day, Channel: row.Channel, TicketsSold: row.TicketsSold, TicketSales: row.TicketSales})
	}
	return out, nil
}

// OrganizationOrigin is where an Organization's history begins: its
// creation, the instant its earliest Ticket Sale was made, reversed or not
// (nil when it has none), and its currency.
type OrganizationOrigin struct {
	Currency    string
	CreatedAt   time.Time
	FirstSoldAt *time.Time
}

// OrganizationOrigin reads where one Organization's history begins, which
// Organization Trends' all time span starts from (#861).
func (s *Service) OrganizationOrigin(ctx context.Context, orgID string) (OrganizationOrigin, error) {
	row, err := s.repo.OrganizationOrigin(ctx, orgID)
	if err != nil {
		return OrganizationOrigin{}, err
	}
	return OrganizationOrigin{Currency: row.Currency, CreatedAt: row.CreatedAt, FirstSoldAt: row.FirstSoldAt}, nil
}
