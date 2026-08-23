package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Assignment Reminder sweep: the mail telling the buyer of a Ticket Sale
// that some of its Tickets still have nobody, and pointing them at the Sale's
// own page by a fresh Confirmation Link (#362, parent #361, ADR 0051).
//
// IT WRITES TO THE BUYER AND TO NOBODY ELSE. An unassigned Ticket has no
// Holder (ADR 0046), so the only person who can give it one is the person who
// bought it; the Answer Reminder beside this one chases the Holder about an
// Answer and, since ADR 0049, never the buyer about an assignment. ADR 0051
// took the decision ADR 0049 parked, and this is the counterpart sweep.
//
// IT IS ALSO THE CATCH-UP. Ticket Assignment went live after Sales for
// upcoming Events had been made, and nothing told those buyers the choice now
// exists. There is no separate announcement: the first sweep IS the
// announcement, and #364 adds the one sentence that makes it read as one for a
// Sale created before the go-live constant. Everything else about those
// buyers — who qualifies, how often, the link, the ledger — is this file's.
//
// SWEPT, NOT TRIGGERED, for the Answer Reminder's reason: a job that sees the
// state rather than the events cannot be made to mail somebody by assigning
// and unassigning in a loop, and a paused scheduler is a switch that mails
// nobody.
//
// IT LIVES IN THE SALES MODULE because the reader is the buyer and everything
// the mail says about them — their address, their name, their Sale Locale,
// the Confirmation Link — is a fact about the Sale, which is this module's.
// Who QUALIFIES is the catalog's, because the holder columns are the Ticket's
// (migration 080); that is the seam AssignmentReminderSource wraps.

// AssignmentReminderSource is what the sweep needs from the catalog: who is
// due, how many are due in all, and the ledger. Implemented by the catalog
// service; nil on a deployment that has not wired it, which sweeps nothing.
type AssignmentReminderSource interface {
	// AssignmentRemindersDue returns up to limit Sales due a reminder now,
	// oldest Sale first, each already passed through
	// catalog.MayRemindAssignment. Nothing while TICKET_ASSIGNMENT_ENABLED is
	// off.
	AssignmentRemindersDue(ctx context.Context, limit int) ([]catalog.AssignmentReminderCandidate, error)
	// CountAssignmentRemindersDue is the standing backlog, reported and never
	// acted on.
	CountAssignmentRemindersDue(ctx context.Context) (int, error)
	// RecordAssignmentReminderSent writes the ledger row for one Sale, after
	// the send.
	RecordAssignmentReminderSent(ctx context.Context, ticketSaleID string) error
}

const (
	// assignmentReminderBatch is the most Sales one sweep will write to. The
	// Answer Reminder's figure, for its reason: well inside a request's budget
	// at the provider's pace, and a backlog larger than it is drained a tick at
	// a time, oldest first, with the remainder reported as backlog.
	assignmentReminderBatch = 50
	// assignmentReminderBudget bounds one run in wall-clock time, below Cloud
	// Scheduler's attempt deadline, so a slow provider stops the run cleanly
	// rather than having it killed mid-send.
	assignmentReminderBudget = 60 * time.Second
)

// AssignmentReminderSweepResult is what one run reports: COUNTS ONLY. No
// buyer, no address, no Sale, no Event — a response that listed who had just
// been reminded would publish a list of people to anything holding the
// scheduler's token.
type AssignmentReminderSweepResult struct {
	// Due is how many Sales this run found in its batch.
	Due int `json:"due"`
	// Sent is how many mails the provider accepted.
	Sent int `json:"sent"`
	// Skipped is how many were due but could not be composed — no address, no
	// link — and stay due.
	Skipped int `json:"skipped"`
	// Failed is how many the provider refused; they stay due and are retried
	// next tick, because no ledger row was written.
	Failed int `json:"failed"`
	// Unrecorded is how many were sent but whose ledger row could not be
	// written: counted in Sent too, and the one case that can cost a buyer one
	// mail over the cap.
	Unrecorded int `json:"unrecorded"`
	// Backlog is how many Sales are due after this run, batch or no batch — the
	// number an Operator watches drain during a catch-up.
	Backlog int `json:"backlog"`
}

// SweepAssignmentReminders runs one sweep: reads the batch, sends each, records
// each success, reports counts.
func (s *Service) SweepAssignmentReminders(ctx context.Context) (*AssignmentReminderSweepResult, error) {
	result := &AssignmentReminderSweepResult{}
	if s.assignmentReminders == nil {
		return result, nil
	}

	due, err := s.assignmentReminders.AssignmentRemindersDue(ctx, assignmentReminderBatch)
	if err != nil {
		return nil, err
	}
	result.Due = len(due)

	deadline := s.now().Add(assignmentReminderBudget)
	pacer := &reminderPacer{service: s, deadline: deadline}
	for _, candidate := range due {
		// The gap comes first and the budget check second, so a pause that
		// spends the last of the budget ends the run here, unsent, rather than
		// one send past it. Sent and Failed together are the requests made.
		pacer.pauseBefore(ctx, result.Sent+result.Failed)
		if !s.now().Before(deadline) {
			s.logger.Info("assignment reminder sweep stopped on its budget; the rest are due on the next tick",
				"sent", result.Sent, "remaining", len(due)-result.Sent-result.Skipped-result.Failed)
			break
		}
		s.sendAssignmentReminder(ctx, candidate, result)
	}

	if total, err := s.assignmentReminders.CountAssignmentRemindersDue(ctx); err != nil {
		s.logger.Error("assignment reminder backlog read failed", "error", err)
	} else {
		result.Backlog = total
	}

	s.logger.Info("assignment reminders swept",
		"due", result.Due,
		"sent", result.Sent,
		"skipped", result.Skipped,
		"failed", result.Failed,
		"unrecorded", result.Unrecorded,
		"backlog", result.Backlog,
	)
	return result, nil
}

// sendAssignmentReminder composes, sends and records ONE Sale's reminder.
//
// THE LEDGER ROW IS WRITTEN AFTER THE SEND AND ONLY AFTER A SUCCESSFUL ONE
// (migration 087). A refused send leaves no row, so the buyer is retried next
// tick; a send whose row could not be written is reported as unrecorded, which
// is the one direction this can duplicate in, and the one worth failing in.
func (s *Service) sendAssignmentReminder(ctx context.Context, c catalog.AssignmentReminderCandidate, result *AssignmentReminderSweepResult) {
	if c.BuyerEmail == "" || c.UnassignedTickets == 0 {
		result.Skipped++
		s.logger.Warn("an Assignment Reminder carries no address or nothing unassigned; nobody was written to and the Sale stays due",
			"ticket_sale_id", c.TicketSaleID)
		return
	}
	// A FRESH Confirmation Link for every mail, expiring at the Event's end
	// exactly as the receipt's does; the sweep refuses to compose a reminder
	// without one, because a reminder with nowhere to go is an instruction its
	// reader cannot follow.
	link := s.confirmationLink(c.TicketSaleID, c.EventEndsAt)
	if link == "" {
		result.Skipped++
		s.logger.Warn("no Confirmation Link could be minted for an Assignment Reminder; nobody was written to and the Sale stays due",
			"ticket_sale_id", c.TicketSaleID)
		return
	}

	reminder := platform.AssignmentReminder{
		To:                c.BuyerEmail,
		FirstName:         c.BuyerFirstName,
		EventName:         c.EventName,
		EventStartsAt:     c.EventStartsAt,
		EventTimezone:     c.EventTimezone,
		UnassignedTickets: c.UnassignedTickets,
		TotalTickets:      c.TicketCount,
		ConfirmationLink:  link,
		SaleCreatedAt:     c.SaleCreatedAt,
		// Sale Locale first, then the buyer's remembered Mail Locale, then
		// English: this is the buyer's own purchase, so the chain is the Sale
		// Confirmation's and not the Holder's inversion (ADR 0033).
		Locale: s.mailLocale(ctx, c.TicketSaleID, c.SaleLocale, c.BuyerEmail),
	}
	if err := s.email.SendAssignmentReminder(ctx, reminder); err != nil {
		result.Failed++
		s.logger.Error("assignment reminder send failed", "ticket_sale_id", c.TicketSaleID, "error", err)
		return
	}
	if err := s.assignmentReminders.RecordAssignmentReminderSent(ctx, c.TicketSaleID); err != nil {
		result.Unrecorded++
		result.Sent++
		s.logger.Error("an Assignment Reminder was sent but could not be recorded; this buyer may receive one more than the cap allows",
			"ticket_sale_id", c.TicketSaleID, "error", err)
		return
	}
	result.Sent++
}
