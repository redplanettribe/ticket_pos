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
