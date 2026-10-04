package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Owed Assignment Mail sweep: the Assignment mails a Named Tickets checkout
// owes, sent paced and retried after the Ticket Sale is recorded (#671, parent
// #665, ADR 0076).
//
// WHY A SWEEP. The commit that records the Sale assigns every Ticket the buyer
// named and marks one Assignment mail owed per address that is not the buyer's
// (migration 127). It cannot send them: a transaction cannot take a mail back,
// and a nine-Ticket checkout sending inline is nine requests in one burst, the
// shape that cost the Assignment Reminder's first run three mails to a 429.
// There is no outbox in this codebase; the owed mark is the only queue, and
// this sweep is its only reader.
//
// THE ASSIGNMENT REMINDER'S SHAPE, EXACTLY. An internal route a Cloud Scheduler
// job calls and a human can curl, a seam the catalog implements, and the pacer
// both Reminder sweeps share (reminderpacing.go). Unlike them it runs every
// minute rather than daily, because what it sends is not a reminder anybody
// could wait a day for: the Holder was named a minute ago and the buyer has
// just been told they will hear.
//
// EVERY JUDGEMENT IS THE CATALOG'S. Whether a mail is still owed - the Ticket
// reassigned since, its Sale reversed, its Event started - the composing of the
// mail and its Assignment Link, and the ledger, all live on the far side of
// OwedAssignmentMailSender. This loop decides how fast and for how long, and
// nothing about whom.
//
// ONLY CHECKOUT-NAMED ASSIGNMENTS COME THIS WAY. An assignment made after the
// sale still sends inline, with its rationing, as before.

// OwedAssignmentMailSender is what the sweep needs from the catalog.
// Implemented by the catalog service; nil on a deployment that has not wired
// it, which sends nothing.
type OwedAssignmentMailSender interface {
	// SendNextOwedAssignmentMail claims the oldest due owed mail and sends,
	// drops or backs it off, reporting which. NoneDue ends the run: nothing is
	// due, or the sender is held (Ticket Assignment closed, or unconfigured).
	SendNextOwedAssignmentMail(ctx context.Context) (catalog.OwedAssignmentMailOutcome, error)
	// CountOwedAssignmentMailsDue is the standing backlog, reported and never
	// acted on.
	CountOwedAssignmentMailsDue(ctx context.Context) (int, error)
}

// owedAssignmentMailBatch and owedAssignmentMailBudget bound one run.
//
// THE BATCH COUNTS OWED MAILS TOUCHED, dropped ones included, so a run reads at
// most fifty rows whatever became of them. At the pacer's gap fifty sends take
// about six seconds; a larger backlog drains a minute at a time, oldest first.
//
// THE BUDGET IS UNDER THE CADENCE, which is the term that matters for a job that
// ticks every minute. A run that stops inside forty-five seconds has finished
// before the next tick starts, so two scheduled runs never overlap and never
// pace their requests independently of each other into twice the rate. And it
// is the innermost term of the deadline chain the Reminder sweeps keep:
//
//	owedAssignmentMailBudget  <  attempt_deadline  <  api_request_timeout_seconds
//	          45s             <        90s         <            300s
//
// Overlap is still harmless when it happens - a curl during a tick - because
// the claim skips locked rows and leases what it takes: no mail goes twice.
const (
	owedAssignmentMailBatch  = 50
	owedAssignmentMailBudget = 45 * time.Second
)

// OwedAssignmentMailSweepResult is what one run reports: COUNTS ONLY. No
// address, no Ticket, no Sale, no Event: a response listing whom the platform
// had just written to would hand a list of people to anything holding the
// scheduler's token.
type OwedAssignmentMailSweepResult struct {
	// Sent is how many mails the provider accepted, recorded or not.
	Sent int `json:"sent"`
	// Dropped is how many were owed no longer - the Ticket reassigned since, its
	// Sale reversed, its Event started - and went unsent, never to be sent.
	Dropped int `json:"dropped"`
	// Failed is how many the provider refused or failed. They stay owed, backed
	// off, and a later run sends them.
	Failed int `json:"failed"`
	// Unrecorded is how many were sent but whose ledger row could not be
	// written: counted in Sent too. The Holder has the mail; the Ticket's
	// lifetime allowance under-counts by one. It should always be zero.
	Unrecorded int `json:"unrecorded"`
	// Backlog is how many owed mails are due after this run and held by no
	// claim. Non-zero with nothing sent means the sender is held - Ticket
	// Assignment closed, or no mailer or link secret configured.
	Backlog int `json:"backlog"`
}

// SweepOwedAssignmentMails runs one sweep: claims, sends and records owed
// Assignment mails one at a time, paced, until none is due, the batch is
// spent, or the budget runs out.
//
// THE GAP FIRST AND THE BUDGET SECOND, as in the Reminder sweeps: a pause that
// spends the last of the budget ends the run before the next claim, so nothing
// is claimed that will not be sent. A claim that is not sent - the process dying
// - comes due again when its lease lapses.
func (s *Service) SweepOwedAssignmentMails(ctx context.Context) (*OwedAssignmentMailSweepResult, error) {
	result := &OwedAssignmentMailSweepResult{}
	if s.owedAssignmentMails == nil {
		return result, nil
	}

	deadline := s.now().Add(owedAssignmentMailBudget)
	pacer := &reminderPacer{service: s, deadline: deadline}
	for touched := 0; touched < owedAssignmentMailBatch; touched++ {
		// Sent and Failed together are the requests the provider has seen from
		// this run; a drop made none and costs no pause.
		pacer.pauseBefore(ctx, result.Sent+result.Failed)
		if !s.now().Before(deadline) {
			s.logger.Info("owed assignment mail sweep stopped on its budget; the rest are due on the next tick",
				"sent", result.Sent)
			break
		}
		outcome, err := s.owedAssignmentMails.SendNextOwedAssignmentMail(ctx)
		if err != nil {
			// The claim failed: the run cannot see what is owed. What it already
			// sent is recorded; what it did not is still owed.
			return nil, err
		}
		if outcome == catalog.OwedAssignmentMailNoneDue {
			break
		}
		switch outcome {
		case catalog.OwedAssignmentMailSent:
			result.Sent++
		case catalog.OwedAssignmentMailSentUnrecorded:
			result.Sent++
			result.Unrecorded++
		case catalog.OwedAssignmentMailFailed:
			result.Failed++
		case catalog.OwedAssignmentMailDropped:
			result.Dropped++
		}
	}

	if total, err := s.owedAssignmentMails.CountOwedAssignmentMailsDue(ctx); err != nil {
		s.logger.Error("owed assignment mail backlog read failed", "error", err)
	} else {
		result.Backlog = total
	}

	s.logger.Info("owed assignment mails swept",
		"sent", result.Sent,
		"dropped", result.Dropped,
		"failed", result.Failed,
		"unrecorded", result.Unrecorded,
		"backlog", result.Backlog,
	)
	return result, nil
}
