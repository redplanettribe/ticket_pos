package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The catalog's half of the Event Recap (spec #884, #891): which Events are
// due one, and the record of which have had theirs. The sales module's sweep
// resolves the readers, composes and sends; this module, which owns the Event
// and its dates, says which Events qualify - the split the Assignment Reminder
// has in assignment_reminders.go.

// EventRecapsDue returns the Events due their Event Recap at the run this
// service's clock is in, the one over longest first, up to limit - each
// already re-checked against catalog.EventRecapDue.
func (s *Service) EventRecapsDue(ctx context.Context, limit int) ([]catalog.EventRecapCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	now := s.now()
	runAt := catalog.EventRecapRunAt(now)
	candidates, err := s.repo.ListEventRecapCandidates(
		ctx, platform.EventRecapWentLiveAt, runAt.Add(-catalog.EventRecapSettleTime), now, limit,
	)
	if err != nil {
		return nil, err
	}
	due := make([]catalog.EventRecapCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if catalog.EventRecapDue(candidate.Inputs(runAt)) {
			due = append(due, candidate)
		}
	}
	return due, nil
}

// CountEventRecapsDue is how many Events are due their Event Recap at the run
// this service's clock is in, batch or no batch: the standing backlog,
// reported and never acted on.
func (s *Service) CountEventRecapsDue(ctx context.Context) (int, error) {
	now := s.now()
	runAt := catalog.EventRecapRunAt(now)
	return s.repo.CountEventRecapCandidates(ctx, platform.EventRecapWentLiveAt, runAt.Add(-catalog.EventRecapSettleTime), now)
}

// ClaimEventRecap takes one Event for the sweep about to write its recap, for
// catalog.EventRecapClaimLease, and reports whether it was taken: false when
// the Event was recapped or another sweep is writing its recap now.
func (s *Service) ClaimEventRecap(ctx context.Context, eventID string) (bool, error) {
	now := s.now()
	return s.repo.ClaimEventRecap(ctx, eventID, now, now.Add(catalog.EventRecapClaimLease))
}

// ReleaseEventRecap gives back a claim no mail went out under: the Event is
// due again at once.
func (s *Service) ReleaseEventRecap(ctx context.Context, eventID string) error {
	return s.repo.ReleaseEventRecap(ctx, eventID)
}

// RecordEventRecapSent records one claimed Event as recapped, stamped with
// this service's clock. From then on the Event is never due again.
func (s *Service) RecordEventRecapSent(ctx context.Context, eventID string) error {
	return s.repo.RecordEventRecapSent(ctx, eventID, s.now())
}
