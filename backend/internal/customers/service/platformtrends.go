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
