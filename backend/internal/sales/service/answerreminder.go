package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Answer Reminder sweep: the mail telling a buyer that Tickets on their
// Ticket Sale still owe Answers, and pointing them back at their sale to give
// them or pass the Answer Links on (#317, ADR 0044).
//
// SWEPT, NOT TRIGGERED BY THE EDIT, and that is the ticket's central decision
// rather than an implementation choice. A reminder raised when a Ticket Question
// is authored would mail the same buyer four times in the ten minutes an
// Organization spends drafting four questions — and would mail them again the
// next morning when somebody fixed a typo in one. A job that sweeps sees the
// state, not the events, so no amount of authoring produces more than the
// rationing allows.
//
// IT IS A JOB IN THE SHAPE THIS BACKEND ALREADY HAS ONE — an internal endpoint a
// scheduler calls and a human can curl — for the reason answerpurge.go gives and
// ADR 0024 recorded: nothing in this process outlives a request, since the API
// runs with min_instances 0 and cpu_idle, so a ticker would fire only while
// somebody happened to be browsing.
//
// WHERE THE PIECES LIVE. The debt, the rationing and the ledger are the catalog
// module's (catalog.MayRemind, catalog/service/answer_reminders.go): what counts
// as an Outstanding Answer is #313's rule and there may not be a second copy of
// it. What is here is the MAIL — the Confirmation Link it points at, the Mail
// Locale it is written in, and the transactional sender it goes out on, all
// three of which this module already owns because the Sale Confirmation needed
// them first. This file decides nothing about who is owed a reminder; it asks,
// and then writes to whoever comes back.
//
// IT SHIPS PAUSED. The Cloud Scheduler job is created with paused = true
// (answer_reminder_enabled defaults false), exactly as the Follow Digest's two
// schedulers and #316's purge did, and the catalog's side reads
// TICKET_QUESTIONS_ENABLED on top of that — so a deployment has to make two
// deliberate decisions before any buyer is written to.

// AnswerReminderSource is what sales needs from catalog to send Answer
// Reminders: who is due one, and the record that one was sent.
//
// THREE METHODS, AND EVERY JUDGEMENT IS ON THE FAR SIDE OF THEM. This module
// does not know what a Ticket Question is, that a retired one owes nothing, that
// a reversed Sale's Tickets have ceased to exist, that the reminder falls silent
// when the doors open, or that two is the lifetime cap. It knows that some
// buyers are owed a message and that sending one has to be written down.
//
// It is the same arrangement OutstandingAnswerReporter has for the receipt's one
// sentence, widened rather than duplicated in spirit: one module owns the debt,
// the other owns the buyer's mail. The alternative — sales counting unanswered
// rows and keeping its own ledger — would be a second definition of the
// Outstanding Answer, which #313 exists to prevent.
//
// OPTIONAL, like OutstandingAnswerReporter. A deployment that never wires it
// sweeps nothing and reports zeros, which is the correct behaviour while the
// feature is dark and the correct behaviour if somebody forgets. Nobody is
// mailed by accident; the failure mode of an unwired seam is silence.
type AnswerReminderSource interface {
	// TicketSalesDueAnswerReminder returns the Ticket Sales whose buyers may be
	// written to now, oldest sale first, at most limit of them. It reads the
	// Ticket Question feature flag and applies the whole of catalog.MayRemind on
	// its own side, so a dark deployment — and a Sale that has had its two, or
	// whose Event has started, or that was reversed — never reaches this module
	// at all.
	TicketSalesDueAnswerReminder(ctx context.Context, limit int) ([]catalog.DueAnswerReminder, error)
	// CountTicketSalesDueAnswerReminder is the standing backlog, ignoring any
	// batch: reporting, not the job.
	CountTicketSalesDueAnswerReminder(ctx context.Context) (int, error)
	// RecordAnswerReminderSent appends the ledger row that rations the next one.
	// Called only after the provider has accepted the message.
	RecordAnswerReminderSent(ctx context.Context, ticketSaleID string) error
}

// answerReminderBatch and answerReminderBudget bound one run.
//
// THE DEADLINE CHAIN, on the Digest drain's terms and with one fewer term than
// the reversal reconciler's:
//
//	answerReminderBudget  <  Cloud Scheduler's attempt_deadline  <  Cloud Run's
//	                                                                request timeout
//	        60s           <              120s                    <      300s
//
// Whichever term is smallest is what actually stops a run, and only the innermost
// one stops it politely. The other two abandon the request where it stands, and
// here that means losing the ledger write for a message the provider has already
// accepted — which costs a buyer a duplicate reminder on a later tick.
//
// THE BATCH IS NOT PACED, unlike the Digest's, and the difference is worth
// stating because the two jobs otherwise look alike. The Digest drains a whole
// platform's weekly mail through a per-minute tick and needs an explicit gap
// between sends to stay under the provider's roughly two-a-second (ADR 0009).
// This run is daily, sends at most a batch, and sends SEQUENTIALLY AND
// SYNCHRONOUSLY — each send is a full HTTPS round trip to the provider before
// the next begins, which is already well under that rate without a timer.
// Adding pacing here would slow a run that is not near the limit, in exchange
// for nothing.
//
// Neither bound limits how much backlog the sweep can work through: what a run
// does not reach is still due tomorrow, and nothing about the rationing depends
// on a Sale being reached on any particular day.
const (
	answerReminderBatch  = 50
	answerReminderBudget = 60 * time.Second
)

// AnswerReminderSweepResult is what one run did, and what is still waiting.
//
// A tally per OUTCOME rather than a bare 200, on the Reversal Reconciler's and
// the purge's terms: this endpoint is the runbook as much as it is the
// automation's entry point, and "we mailed forty buyers" says nothing an
// operator can act on where "thirty-eight sent, two had no link" says all of it.
type AnswerReminderSweepResult struct {
	// Due is how many Ticket Sales this run found waiting, bounded by the batch.
	// It is what the run had to work with, and DueTotal below is what there was.
	Due int `json:"due"`
	// Sent is reminders the provider accepted and the ledger recorded. On a
	// platform where the feature ships dark and the job ships paused, zero is the
	// only answer.
	Sent int `json:"sent"`
	// Skipped is candidates this run deliberately did not mail: a Confirmation
	// Link that could not be signed, or an address the Sale does not carry.
	// NOTHING WAS SENT and nothing was recorded, so they are due again on the
	// next tick — which is right, because the fault is the deployment's rather
	// than the buyer's.
	//
	// A number that stays high is a misconfiguration, not a backlog: the only way
	// to fail to sign a Confirmation Link is to have no link secret.
	Skipped int `json:"skipped"`
	// Failed is reminders the provider refused. They are NOT recorded in the
	// ledger, so the buyer is due again on the next tick and has lost nothing.
	// This is the number that says a provider is unwell.
	Failed int `json:"failed"`
	// Unrecorded is sends the provider accepted whose ledger row could not be
	// written. It is its own number rather than folded into Failed because it
	// means the opposite thing: the buyer HAS the mail, and the platform has
	// forgotten it sent it, so they may receive one more than the cap intended.
	//
	// It should always be zero. A non-zero value is the one outcome of this job
	// that is worth waking somebody for, because the rationing is only as true as
	// this table.
	Unrecorded int `json:"unrecorded"`
	// DueTotal is how many Ticket Sales are due a reminder across the platform,
	// ignoring the batch — the standing backlog, in the sense the purge's
	// answers_held is. Two curls a day apart say whether the sweep is keeping up.
	DueTotal int `json:"due_total"`
}

// SweepAnswerReminders mails the buyers of active Ticket Sales that still owe
// Answers, and nobody else.
//
// WHAT IT DOES NOT DO is decide who. Every rule — the debt, the seven days, the
// cap of two, the silence once the Event has started, the reversed Sale — is
// applied by the catalog module before a candidate is returned, and this loop
// composes a message for each row it is handed. That separation is what stops
// the rationing from being restated here, where it would be a third copy after
// the SQL and catalog.MayRemind.
//
// THE ORDER OF SEND-THEN-RECORD IS LOAD-BEARING and is the one thing in this
// file worth reading twice. Recording first and failing to send would ration a
// buyer out of a reminder they never received, permanently, since the cap counts
// for the life of the Sale. Sending first and failing to record costs at most
// one duplicate on a later tick, reported as Unrecorded so nobody has to guess
// which happened.
//
// A FAILED SEND IS NOT A FAILED RUN. One provider refusal must not abandon the
// forty buyers behind it in the batch — the reason the Reversal Reconciler works
// item by item — so failures are counted and the loop continues. What DOES stop
// the run is the read that produced the candidates, because a run that could not
// see who was owed has done nothing and must not report a quiet success.
func (s *Service) SweepAnswerReminders(ctx context.Context) (*AnswerReminderSweepResult, error) {
	result := &AnswerReminderSweepResult{}

	// An unwired seam sweeps nothing, exactly as it reports no Outstanding
	// Answers on a receipt. Silence is what an unconfigured deployment gets.
	if s.answerReminders == nil {
		return result, nil
	}

	due, err := s.answerReminders.TicketSalesDueAnswerReminder(ctx, answerReminderBatch)
	if err != nil {
		return nil, err
	}
	result.Due = len(due)

	deadline := s.now().Add(answerReminderBudget)
	for _, candidate := range due {
		// The budget is checked BEFORE each send rather than after, so a run stops
		// with a message unsent rather than with one sent and unrecorded. What is
		// left is due again on the next tick, having lost nothing.
		if !s.now().Before(deadline) {
			s.logger.Info("answer reminder sweep stopped on its budget; the rest are due on the next tick",
				"sent", result.Sent, "remaining", len(due)-result.Sent-result.Skipped-result.Failed)
			break
		}
		s.sendAnswerReminder(ctx, candidate, result)
	}

	// Reporting, and deliberately not part of the job: a run that mailed
	// correctly must not be reported as failed because a COUNT did not come back.
	// Same arrangement the purge's backlog read has.
	if total, err := s.answerReminders.CountTicketSalesDueAnswerReminder(ctx); err != nil {
		s.logger.Error("answer reminder backlog read failed", "error", err)
	} else {
		result.DueTotal = total
	}

	// One line per run, and the only record that a run happened at all: a sweep
	// that sent nothing writes nothing to the database, so without this an
	// operator asking "is the job running" cannot tell it from a paused
	// scheduler.
	s.logger.Info("answer reminders swept",
		"due", result.Due,
		"sent", result.Sent,
		"skipped", result.Skipped,
		"failed", result.Failed,
		"unrecorded", result.Unrecorded,
		"due_total", result.DueTotal,
	)
	return result, nil
}

// sendAnswerReminder writes to one buyer and records that it did, tallying the
// outcome. It never returns an error: every way this can go wrong is one row of
// a batch, and the run continues.
func (s *Service) sendAnswerReminder(ctx context.Context, due catalog.DueAnswerReminder, result *AnswerReminderSweepResult) {
	// A Sale with no address cannot be written to. It should be unreachable — a
	// Ticket Sale records its buyer's email on every channel — and it is checked
	// because the alternative is handing an empty To to the provider and counting
	// the refusal as a provider fault.
	if due.CustomerEmail == "" {
		result.Skipped++
		s.logger.Warn("a Ticket Sale due an Answer Reminder carries no buyer address",
			"ticket_sale_id", due.TicketSaleID)
		return
	}

	// THE LINK IS THE MESSAGE, so a Sale whose link cannot be signed is skipped
	// rather than mailed without one. This is the opposite of the Sale
	// Confirmation's ruling — a receipt without its link still carries the
	// reference and the total and is worth far more than no email — and the
	// difference is that this mail has nothing else to say. "Open your purchase"
	// with no link to open is worse than silence: the reader goes looking for
	// something that is not there.
	//
	// Nothing is recorded, so the Sale is due again as soon as the deployment is
	// fixed. The only way to reach here is a service with no link secret, which
	// NewApp refuses to build in production.
	link := s.confirmationLink(due.TicketSaleID, due.EventEnd)
	if link == "" {
		result.Skipped++
		s.logger.Warn("could not sign the Confirmation Link for an Answer Reminder; nobody was written to and the Sale stays due",
			"ticket_sale_id", due.TicketSaleID)
		return
	}

	reminder := platform.AnswerReminder{
		To:               due.CustomerEmail,
		CustomerName:     displayName(due.CustomerFirstName, due.CustomerLastName),
		EventName:        due.EventName,
		Reference:        due.ConfirmationRef,
		ConfirmationLink: link,
		// The same chain every other mail about a sale goes through: the Sale
		// Locale, then what the recipient's record remembers, then English. This
		// mail is sent longer after the sale than any other, which is exactly the
		// case ADR 0033 put the Sale Locale first for.
		Locale: s.mailLocale(ctx, due.TicketSaleID, due.SaleLocale, due.CustomerEmail),
	}

	if err := s.email.SendAnswerReminder(ctx, reminder); err != nil {
		// Nothing is recorded, so the buyer is due again on the next tick and has
		// lost none of their allowance to a provider outage.
		result.Failed++
		s.logger.Error("answer reminder send failed",
			"ticket_sale_id", due.TicketSaleID, "error", err)
		return
	}

	if err := s.answerReminders.RecordAnswerReminderSent(ctx, due.TicketSaleID); err != nil {
		// The mail is in the buyer's inbox and the platform has forgotten it sent
		// it. Counted apart from a failure because it means the opposite thing:
		// this buyer may receive one reminder more than the cap intended, and the
		// only way anybody learns that is this line and the Unrecorded tally.
		result.Unrecorded++
		result.Sent++
		s.logger.Error("an Answer Reminder was sent but could not be recorded; this buyer may receive one more than the cap allows",
			"ticket_sale_id", due.TicketSaleID, "error", err)
		return
	}

	result.Sent++
}
