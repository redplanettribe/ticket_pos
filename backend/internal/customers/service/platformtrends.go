package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformNewVerifiedCustomersByDay is how many Customers became Verified
// Customers per day over the verifications in [from, until), read for
// Platform Trends (#813). A Customer counts on the day they first proved
// their email, by One-time Passcode, Google Sign-In or an accepted link, and
// never on the day a sale minted their record. The days are read in the named
// timezone; days nobody verified on have no entry.
func (s *Service) PlatformNewVerifiedCustomersByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	return s.repo.PlatformNewVerifiedCustomersByDay(ctx, from, until, timezone)
}

// PlatformFirstVerifiedCustomerDay is the day, YYYY-MM-DD on the named clock,
// the platform's first Customer was verified, and false when none ever was.
func (s *Service) PlatformFirstVerifiedCustomerDay(ctx context.Context, timezone string) (string, bool, error) {
	return s.repo.PlatformFirstVerifiedCustomerDay(ctx, timezone)
}

// PlatformCustomerDay names a Customer and a day, YYYY-MM-DD on the clock the
// caller asked for.
type PlatformCustomerDay struct {
	Day        string
	CustomerID string
}

// PlatformCustomersVerifiedByDay is every Customer who became a Verified
// Customer in [from, until) with the day they did, in the named timezone. It
// is for the daily close, which splits the Customers active on a day into
// new and returning with it (#817) and freezes counts alone; nothing that
// answers a request may read it, since no route returns a Customer for a
// day (ADR 0080).
func (s *Service) PlatformCustomersVerifiedByDay(ctx context.Context, from, until time.Time, timezone string) ([]PlatformCustomerDay, error) {
	rows, err := s.repo.PlatformCustomersVerifiedByDay(ctx, from, until, timezone)
	if err != nil {
		return nil, err
	}
	out := make([]PlatformCustomerDay, 0, len(rows))
	for _, row := range rows {
		out = append(out, PlatformCustomerDay{Day: row.Day, CustomerID: row.CustomerID})
	}
	return out, nil
}
