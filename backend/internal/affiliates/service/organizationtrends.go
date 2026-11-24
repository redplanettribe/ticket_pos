package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// OrganizationPageViewsByDay is one Organization's Page Views per day over
// the hourly buckets in [from, until), every Event page of the Organization
// summed, read for Organization Trends (#863). A load that came through an
// Affiliate Link is one Page View here, not two. Each hour is read on its day
// in the named timezone, which must be a whole number of hours from UTC, as
// the platform clock is; days with no Page View have no entry.
func (s *Service) OrganizationPageViewsByDay(ctx context.Context, orgID string, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	return s.repo.OrganizationPageViewsByDay(ctx, orgID, from, until, timezone)
}
