package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The catalog's half of the swept Assignment mail (#671, parent #665, ADR
// 0076): one owed mail claimed, judged, sent through the ordinary composer and
// recorded. The sales module's sweep loops over this, paced by the pacer the
// Reminder sweeps share - the Assignment Reminder's split, for its reason: the
// Ticket, its assignment, the Assignment Link and the ledger are this module's,
// and the pacing of a run of mails is that module's.
//
// THE LINK IS COMPOSED HERE AND NOWHERE ELSE. assignmentLinkURL may be called
// only from this module (assignment_link.go), so the sweep's loop can never
// hold an Assignment Link: it hears only what became of each mail.

// SendNextOwedAssignmentMail claims the oldest due owed Assignment mail and
// sends it, drops it, or backs it off, reporting which. NoneDue means the run
// should stop: nothing is due, or nothing may be sent.
//
// HELD, NOT DROPPED, WHILE TICKET ASSIGNMENT IS CLOSED. The accept route refuses
// every link while the flag is off, so a mail sent now would be an instruction
// its reader cannot follow; but the assignment stands, and the buyer paid for
// it, so the mail waits for the flag to reopen. The Event starting is what
// drops it, if the flag stays closed that long. Held the same way when no mailer
// or no link secret is wired: a deployment fault, logged, and the owed mail
// waits for the fix rather than being spent on it.
//
// AN ERROR IS A DATABASE FAILURE ON THE CLAIM, which means the run cannot see
// what is owed and must say so. Every failure after the claim is one mail's and
// is reported as its outcome.
func (s *Service) SendNextOwedAssignmentMail(ctx context.Context) (catalog.OwedAssignmentMailOutcome, error) {
	if !s.ticketAssignmentEnabled {
		return catalog.OwedAssignmentMailNoneDue, nil
	}
	if s.mailer == nil || !s.assignmentLinks.Configured() {
		s.logAssignmentMailFailure("owed assignment mails held: no mailer or no assignment link secret is configured", nil)
		return catalog.OwedAssignmentMailNoneDue, nil
	}

	now := s.now()
	owed, err := s.repo.ClaimDueOwedAssignmentMail(ctx, now, now.Add(catalog.OwedAssignmentMailClaimLease))
	if err != nil {
		return catalog.OwedAssignmentMailNoneDue, err
	}
	if owed == nil {
		return catalog.OwedAssignmentMailNoneDue, nil
	}

	// THE ACCEPT FLOW'S OWN READ, live: the assignment the Ticket carries now,
	// its Sale and its Event, and everything the mail is composed from. A Ticket
	// gone since the claim took its owed row with it, by the cascade.
	ticket, err := s.repo.GetAssignmentLinkTicket(ctx, owed.TicketID)
	if err != nil {
		s.backOffOwedAssignmentMail(ctx, owed.TicketID, owed.AttemptCount, "owed assignment mail not composed: ticket unreadable", err)
		return catalog.OwedAssignmentMailFailed, nil
	}
	if ticket == nil {
		return catalog.OwedAssignmentMailDropped, nil
	}

	fate := catalog.DecideOwedAssignmentMail(catalog.OwedAssignmentMailInputs{
		OwedFor:       owed.OwedFor,
		HolderEmail:   ticket.HolderEmail.String,
		AssignedAt:    nullTimeOrNil(ticket.AssignedAt),
		AcceptedAt:    nullTimeOrNil(ticket.AcceptedAt),
		Channel:       owed.Channel,
		SaleStatus:    ticket.SaleStatus,
		EventStartsAt: nullTimeOrNil(ticket.EventStartsAt),
		Now:           now,
	})
	if fate == catalog.OwedAssignmentMailSend && !owed.BuyerCustomerID.Valid {
		// Unreachable: a Storefront checkout always has a Customer (ADR 0054),
		// and the ledger row a send writes must name one. Dropped rather than
		// sent unrecorded, which would be a mail outside every allowance.
		s.logger.Warn("an owed assignment mail's Sale has no buyer Customer; dropped unsent", "ticket_id", owed.TicketID)
		fate = catalog.OwedAssignmentMailDropStale
	}
	if fate != catalog.OwedAssignmentMailSend {
		if err := s.repo.DropOwedAssignmentMail(ctx, owed.TicketID); err != nil {
			// The lease brings it back and the next run judges it again, the same
			// way: nothing is lost but a statement.
			s.logger.Error("an owed assignment mail owed no more could not be dropped; it is judged again after its lease",
				"ticket_id", owed.TicketID, "reason", fate.String(), "error", err)
		} else {
			s.logger.Info("owed assignment mail dropped unsent", "ticket_id", owed.TicketID, "reason", fate.String())
		}
		return catalog.OwedAssignmentMailDropped, nil
	}

	// THE ORDINARY ASSIGNMENT MAIL, through the one composer, signed over the
	// assigned_at the Ticket carries - which the judgement above has just found
	// equal to the one the mail was owed for - and addressed to the address on
	// the Ticket, exactly as the inline path signs and addresses it.
	if !s.mailTicketAssignment(ctx, ticket, ticket.HolderEmail.String, ticket.AssignedAt.Time) {
		s.backOffOwedAssignmentMail(ctx, owed.TicketID, owed.AttemptCount, "owed assignment mail failed; it stays owed", nil)
		return catalog.OwedAssignmentMailFailed, nil
	}

	if err := s.repo.RecordOwedAssignmentMailSent(ctx, owed.TicketID, owed.BuyerCustomerID.String, s.now()); err != nil {
		// The Holder has the mail and the platform has forgotten it sent it.
		// Clearing the owed mark is the half worth retrying on its own: left
		// standing, the lease would bring the same mail round again, and a
		// duplicate to a stranger is worse than a Ticket whose lifetime
		// allowance under-counts by one.
		s.logger.Error("an owed assignment mail was sent but could not be recorded; this Ticket's allowance under-counts by one",
			"ticket_id", owed.TicketID, "error", err)
		if dropErr := s.repo.DropOwedAssignmentMail(ctx, owed.TicketID); dropErr != nil {
			s.logger.Error("a sent owed assignment mail is still marked owed; it may be sent again after its lease",
				"ticket_id", owed.TicketID, "error", dropErr)
		}
		return catalog.OwedAssignmentMailSentUnrecorded, nil
	}
	return catalog.OwedAssignmentMailSent, nil
}

// backOffOwedAssignmentMail keeps a mail that failed owed, due again after the
// backoff its attempts have earned. A failure to write the backoff leaves the
// lease to bring it back, which is the same outcome a little sooner.
func (s *Service) backOffOwedAssignmentMail(ctx context.Context, ticketID string, attempts int, message string, cause error) {
	if cause != nil {
		s.logger.Error(message, "ticket_id", ticketID, "attempt_count", attempts, "error", cause)
	} else {
		s.logger.Warn(message, "ticket_id", ticketID, "attempt_count", attempts)
	}
	next := s.now().Add(catalog.OwedAssignmentMailRetryDelay(attempts))
	if err := s.repo.RescheduleOwedAssignmentMail(ctx, ticketID, next); err != nil {
		s.logger.Error("a failed owed assignment mail could not be backed off; it is due again after its lease",
			"ticket_id", ticketID, "error", err)
	}
}

// CountOwedAssignmentMailsDue is the standing backlog: owed mails due now and
// held by no claim. Reported even while the sender is held, because a backlog
// growing behind a closed flag is what an Operator most needs to see.
func (s *Service) CountOwedAssignmentMailsDue(ctx context.Context) (int, error) {
	return s.repo.CountOwedAssignmentMailsDue(ctx, s.now())
}
