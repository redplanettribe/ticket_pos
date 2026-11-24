package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// OrganizationEventCounts is how many Events an Organization has in any
// status, and how many of them are Active Events.
type OrganizationEventCounts struct {
	Events       int
	ActiveEvents int
}

// OrganizationEventCounts counts one Organization's Events and Active Events
// at the instant now, read for Home (#859).
func (s *Service) OrganizationEventCounts(ctx context.Context, orgID string, now time.Time) (OrganizationEventCounts, error) {
	counts, err := s.repo.OrganizationEventCounts(ctx, orgID, now)
	if err != nil {
		return OrganizationEventCounts{}, err
	}
	return OrganizationEventCounts{Events: counts.Events, ActiveEvents: counts.ActiveEvents}, nil
}

// NextEvent is the Event Home names as next.
type NextEvent = repository.NextEventRow

// OrganizationNextEvent is the published, non-cancelled Event of one
// Organization with the earliest start that is not over at now, an Ongoing
// one included, and false when there is none (#859). A draft is never next.
func (s *Service) OrganizationNextEvent(ctx context.Context, orgID string, now time.Time) (NextEvent, bool, error) {
	return s.repo.OrganizationNextEvent(ctx, orgID, now)
}

// OrganizationEventsHeldByDay is one Organization's Events held per day over
// the starts in [from, until), read for Organization Trends (#863): each
// published Event once, on the day of the named timezone its start falls in.
// A draft and a cancelled Event are never held; an External Registration
// Event is. Days on which no Event was held have no entry.
func (s *Service) OrganizationEventsHeldByDay(ctx context.Context, orgID string, from, until time.Time, timezone string) ([]platform.DayCount, error) {
	return s.repo.OrganizationEventsHeldByDay(ctx, orgID, from, until, timezone)
}

// OrganizationEventsHeld is every Event of one Organization held over the
// starts in [from, until), each counting one, earliest start first, read for
// the breakdown of Organization Trends (#864): the Events
// OrganizationEventsHeldByDay counts over the same window.
func (s *Service) OrganizationEventsHeld(ctx context.Context, orgID string, from, until time.Time) ([]platform.EventCount, error) {
	return s.repo.OrganizationEventsHeld(ctx, orgID, from, until)
}

// EventStart is one Event's name, start and timezone.
type EventStart = repository.EventStartRow

// OrganizationEventStarts is the name, start and timezone of each of the
// given Events of one Organization, keyed by id, read for the Event table of
// Organization Trends (#865). An id no Event of the Organization has is
// absent.
func (s *Service) OrganizationEventStarts(ctx context.Context, orgID string, eventIDs []string) (map[string]EventStart, error) {
	return s.repo.OrganizationEventStarts(ctx, orgID, eventIDs)
}
