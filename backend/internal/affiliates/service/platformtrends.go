package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformPageViewsByDay is the platform's Page Views per day over the hourly
// buckets in [from, until), every Event of every Organization included, read
// for Platform Trends (#813). A load that came through an Affiliate Link is
// one Page View here, not two. The days are read in the named timezone, which
// must be a whole number of hours from UTC, as the platform clock is; days
// with no Page View have no entry.
func (s *Service) PlatformPageViewsByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	return s.repo.PlatformPageViewsByDay(ctx, from, until, timezone)
}

// PlatformFirstPageViewDay is the day, YYYY-MM-DD on the named clock, the
// platform's earliest Page View was counted, and false when it has none.
func (s *Service) PlatformFirstPageViewDay(ctx context.Context, timezone string) (string, bool, error) {
	return s.repo.PlatformFirstPageViewDay(ctx, timezone)
}
