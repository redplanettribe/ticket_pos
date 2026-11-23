package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
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
