package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The catalog's half of the Assignment Reminder (#362, parent #361, ADR 0051):
// who is due one, and the ledger of who has had one. The sales module's sweep
// composes and sends; this module, which owns the Ticket and its holder
// columns (migration 080), says who qualifies — the same split the Answer
// Reminder has in answer_reminders.go, for the same reason.

// AssignmentRemindersDue returns the Sales due an Assignment Reminder now,
// oldest first, up to limit — each already re-checked against
// catalog.MayRemindAssignment at this service's clock.
//
// NOTHING WHILE TICKET ASSIGNMENT IS DARK. With the flag off there is no page
// on which to assign, so a reminder would be an instruction its reader cannot
// follow; the answer is "nobody is due", not an error, so a paused feature and
// an empty backlog look the same to the scheduler. The flag is read here and
// not in the domain rule because a closed flag is the absence of candidates,
// not the refusal of each.
func (s *Service) AssignmentRemindersDue(ctx context.Context, limit int) ([]catalog.AssignmentReminderCandidate, error) {
	if !s.ticketAssignmentEnabled || limit <= 0 {
		return nil, nil
	}
	now := s.now()
	candidates, err := s.repo.ListAssignmentReminderCandidates(
		ctx, now, now.Add(-catalog.AssignmentReminderMinSaleAge), now.Add(-catalog.AssignmentReminderInterval), limit,
	)
	if err != nil {
		return nil, err
	}
	allowed := make([]catalog.AssignmentReminderCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if catalog.MayRemindAssignment(candidate.Inputs(now)) {
			allowed = append(allowed, candidate)
		}
	}
	return allowed, nil
}

// CountAssignmentRemindersDue is the standing backlog at this moment, and zero
// while the feature is dark.
func (s *Service) CountAssignmentRemindersDue(ctx context.Context) (int, error) {
	if !s.ticketAssignmentEnabled {
		return 0, nil
	}
	now := s.now()
	return s.repo.CountAssignmentRemindersDue(
		ctx, now, now.Add(-catalog.AssignmentReminderMinSaleAge), now.Add(-catalog.AssignmentReminderInterval),
	)
}

// RecordAssignmentReminderSent writes the ledger row for one Sale, stamped
// with this service's clock.
func (s *Service) RecordAssignmentReminderSent(ctx context.Context, ticketSaleID string) error {
	return s.repo.RecordAssignmentReminderSent(ctx, ticketSaleID, s.now())
}
