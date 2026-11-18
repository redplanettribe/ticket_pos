package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PlatformEventsPublishedByDay is the platform's Events Published per day
// over the publications in [from, until), every Organization included, read
// for Platform Trends (#814): each Event once, on the day it first became
// `published`, cancelled since or not. The days are read in the named
// timezone; days on which no Event was published have no entry.
func (s *Service) PlatformEventsPublishedByDay(ctx context.Context, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	return s.repo.PlatformEventsPublishedByDay(ctx, from, until, timezone)
}

// PlatformFirstEventPublishedDay is the day, YYYY-MM-DD on the named clock,
// the platform's earliest Event was first published, and false when none
// ever was.
func (s *Service) PlatformFirstEventPublishedDay(ctx context.Context, timezone string) (string, bool, error) {
	return s.repo.PlatformFirstEventPublishedDay(ctx, timezone)
}

// PlatformEventsPublishedExactSince is the platform day, YYYY-MM-DD, exact
// recording of Events Published began, and false when none was recorded.
// An Event first published before it is counted on its creation day.
func (s *Service) PlatformEventsPublishedExactSince(ctx context.Context) (string, bool, error) {
	return s.repo.PlatformEventsPublishedExactSince(ctx)
}

// PlatformEventsPublished is every Event first published in [from, until),
// oldest publication first, every Organization included, read for the
// breakdown of Platform Trends (#818): the Events
// PlatformEventsPublishedByDay counts, each once.
func (s *Service) PlatformEventsPublished(ctx context.Context, from, until time.Time) ([]platform.EventCount, error) {
	return s.repo.PlatformEventsPublished(ctx, from, until)
}
