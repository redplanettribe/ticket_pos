package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Event Recap sweep (spec #884, #891): the one email that tells the people
// answerable for an Event how it went, sent at the first 09:00 of the platform
// clock at least six hours after the Event is over.
//
// SWEPT, NOT TRIGGERED, like the Reminder sweeps beside it: a daily job that
// sees the state cannot be made to mail twice by an Event's dates moving back
// and forth, and a run that was missed is made good by the next one, which
// sends late and drops nothing.
//
// IT LIVES IN THE SALES MODULE because what the recap states is the Event's
// sales, which are this module's, and because the pacer the mail sweeps share
// is here. Which Events are DUE is the catalog's, since the Event and its dates
// are; who ANSWERS for an Event is identity's, since Event assignments are.
// Those are the two seams below.

// EventRecapSource is what the sweep needs from the catalog: which Events are
// due, and the record that one was sent. Implemented by the catalog service.
type EventRecapSource interface {
	// EventRecapsDue returns up to limit Events due their Event Recap at this
	// run, the one over longest first, each already passed through
	// catalog.EventRecapDue.
	EventRecapsDue(ctx context.Context, limit int) ([]catalog.EventRecapCandidate, error)
	// RecordEventRecapSent records one Event as recapped, after the send. The
	// Event is never due again.
	RecordEventRecapSent(ctx context.Context, eventID string) error
}

// EventRecapRecipients is what the sweep needs from identity: who the Event
// Owners of an Event are right now. The Staff Locale each is written in comes
// through the StaffLocales seam the Payout Request notices already use.
type EventRecapRecipients interface {
	EventOwnerEmails(ctx context.Context, eventID string) ([]string, error)
}

const (
	// eventRecapBatch is the most Events one run recaps. The Reminder sweeps'
	// figure: more Events than this over in one day is a backlog the next run
	// continues, the one over longest first.
	eventRecapBatch = 50
	// eventRecapBudget bounds one run in wall-clock time, below Cloud
	// Scheduler's attempt deadline, as the Reminder sweeps' budget does.
	eventRecapBudget = 60 * time.Second
)

// EventRecapSweepResult is what one run reports: COUNTS ONLY. No Event, no
// Organization, no address.
type EventRecapSweepResult struct {
	// Due is how many Events this run found due in its batch.
	Due int `json:"due"`
	// Recapped is how many of them were mailed to at least one reader and are
	// recorded, never due again.
	Recapped int `json:"recapped"`
	// Sent is how many mails the provider accepted, one per reader per Event.
	Sent int `json:"sent"`
	// Failed is how many mails the provider refused. An Event none of whose
	// readers could be written to stays due and is retried by the next run.
	Failed int `json:"failed"`
	// Skipped is how many Events were due but had nobody to write to or whose
	// readers could not be read; they stay due.
	Skipped int `json:"skipped"`
	// Unrecorded is how many Events were mailed but could not be recorded:
	// counted in Recapped too, and the one case that can send a second recap.
	Unrecorded int `json:"unrecorded"`
}

// SweepEventRecaps runs one sweep: reads the batch of due Events, writes to
// each one's readers, records each Event that reached somebody, reports
// counts.
func (s *Service) SweepEventRecaps(ctx context.Context) (*EventRecapSweepResult, error) {
	result := &EventRecapSweepResult{}
	if s.eventRecaps == nil || s.eventRecapRecipients == nil {
		return result, nil
	}

	due, err := s.eventRecaps.EventRecapsDue(ctx, eventRecapBatch)
	if err != nil {
		return nil, err
	}
	result.Due = len(due)

	deadline := s.now().Add(eventRecapBudget)
	pacer := &reminderPacer{service: s, deadline: deadline}
	for i, candidate := range due {
		if !s.recapEvent(ctx, candidate, pacer, deadline, result) {
			s.logger.Info("event recap sweep stopped on its budget; the rest are due on the next run",
				"recapped", result.Recapped, "remaining", len(due)-i)
			break
		}
	}

	s.logger.Info("event recaps swept",
		"due", result.Due,
		"recapped", result.Recapped,
		"sent", result.Sent,
		"failed", result.Failed,
		"skipped", result.Skipped,
		"unrecorded", result.Unrecorded,
	)
	return result, nil
}

// recapEvent writes ONE Event's recap to each of its readers and records the
// Event. It reports false when the run's budget ran out before this Event's
// readers were all written to, which ends the run.
//
// THE READERS ARE RESOLVED HERE, AT SEND TIME, never when the Event ended:
// somebody made an Event Owner this morning gets it, and somebody whose
// assignment was removed yesterday does not.
//
// THE EVENT IS RECORDED AFTER THE SENDS AND ONLY IF ONE WAS ACCEPTED, the
// rule the Certificate Expiry Warning keeps for a mail with several readers. An
// Event that reached nobody stays due and the next run retries it. One that
// reached some readers and not others is recorded: the recap goes once per
// Event, and retrying it would write a second time to those it reached.
//
// AN EVENT WITH NO EVENT OWNER IS SKIPPED AND STAYS DUE in this ticket. Who is
// written to then, and what becomes of an Event that resolves to nobody, is
// #885's to decide.
func (s *Service) recapEvent(ctx context.Context, c catalog.EventRecapCandidate, pacer *reminderPacer, deadline time.Time, result *EventRecapSweepResult) bool {
	owners, err := s.eventRecapRecipients.EventOwnerEmails(ctx, c.EventID)
	if err != nil {
		result.Skipped++
		s.logger.Error(ctx, "event recap: the Event's Event Owners could not be read; the Event stays due", "event_id", c.EventID, "error", err)
		return true
	}
	recipients := distinctEmails(owners)
	if len(recipients) == 0 {
		result.Skipped++
		s.logger.Info("event recap: the Event has no Event Owner; nobody was written to and the Event stays due", "event_id", c.EventID)
		return true
	}

	sentAt := s.now()
	eventURL := s.staffBaseURL + "/events/" + c.EventID
	accepted, withinBudget := 0, true
	for _, to := range recipients {
		// The gap comes first and the budget check second, as in the Reminder
		// sweeps: a pause that spends the last of the budget ends the run
		// here, unsent, rather than one send past it.
		pacer.pauseBefore(ctx, result.Sent+result.Failed)
		if !s.now().Before(deadline) {
			withinBudget = false
			break
		}
		err := s.email.SendEventRecap(ctx, platform.EventRecap{
			To:             to,
			Locale:         s.staffLocale(ctx, to),
			EventName:      c.EventName,
			SentAt:         sentAt,
			SalesTrendsURL: eventURL + "/sales/trends",
			ReachURL:       eventURL + "/reach",
			SalesURL:       eventURL + "/sales",
		})
		if err != nil {
			// The address is not logged: the log counts readers and names
			// the Event, where an operator has to find the row.
			result.Failed++
			s.logger.Error(ctx, "event recap send failed", "event_id", c.EventID, "error", err)
			continue
		}
		result.Sent++
		accepted++
	}
	if accepted == 0 {
		return withinBudget
	}
	result.Recapped++
	if err := s.eventRecaps.RecordEventRecapSent(ctx, c.EventID); err != nil {
		result.Unrecorded++
		s.logger.Error(ctx, "an Event Recap was sent but could not be recorded; the next run will send it again",
			"event_id", c.EventID, "error", err)
	}
	return withinBudget
}

// distinctEmails is the addresses with repeats dropped, compared normalized,
// in the order given: one copy per person.
func distinctEmails(emails []string) []string {
	seen := make(map[string]bool, len(emails))
	out := make([]string, 0, len(emails))
	for _, email := range emails {
		key := platform.NormalizeEmail(email)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, email)
	}
	return out
}
