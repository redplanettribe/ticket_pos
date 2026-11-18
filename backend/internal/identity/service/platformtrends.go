package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformNewOrganizationsByDay is how many Organizations were created per
// day over the creations in [from, until), House Organizations included, read
// for Platform Trends (#813). The days are read in the named timezone; days
// with no new Organization have no entry.
func (s *Service) PlatformNewOrganizationsByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	return s.repo.PlatformNewOrganizationsByDay(ctx, from, until, timezone)
}

// PlatformFirstOrganizationDay is the day, YYYY-MM-DD on the named clock, the
// platform's first Organization was created, and false when there is none.
func (s *Service) PlatformFirstOrganizationDay(ctx context.Context, timezone string) (string, bool, error) {
	return s.repo.PlatformFirstOrganizationDay(ctx, timezone)
}
