package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
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
// due, the claim on one a sweep is writing to, and the record that one was
// sent. Implemented by the catalog service.
type EventRecapSource interface {
	// EventRecapsDue returns up to limit Events due their Event Recap at this
	// run, the one over longest first, each already passed through
	// catalog.EventRecapDue.
	EventRecapsDue(ctx context.Context, limit int) ([]catalog.EventRecapCandidate, error)
	// CountEventRecapsDue is the standing backlog, reported and never acted
	// on.
	CountEventRecapsDue(ctx context.Context) (int, error)
	// ClaimEventRecap takes one Event before its first send, and reports
	// false when it was recapped or another sweep holds it.
	ClaimEventRecap(ctx context.Context, eventID string) (bool, error)
	// ReleaseEventRecap gives back a claim no mail went out under. The Event
	// is due again.
	ReleaseEventRecap(ctx context.Context, eventID string) error
	// RecordEventRecapSent records one claimed Event as recapped, after the
	// send. The Event is never due again.
	RecordEventRecapSent(ctx context.Context, eventID string) error
}

// EventRecapRecipients is what the sweep needs from identity: who the Event
// Owners of an Event are right now, and who the Org Admins of its Organization
// are, for an Event with none (#885). Neither list has Event Staff in it, and
// neither has anybody for being a Platform Operator: the allowlist is not
// Membership. The Staff Locale each is written in comes through the
// StaffLocales seam the Payout Request notices already use.
type EventRecapRecipients interface {
	EventOwnerEmails(ctx context.Context, eventID string) ([]string, error)
	OrgAdminEmails(ctx context.Context, orgID string) ([]string, error)
}

// EventRecapReachSource is what the sweep needs from affiliates: the attention
// an Event's page drew before the Event was over (#887), its Page Views and
// its busiest Affiliate Links. The hourly buckets and the links are that
// module's, so the cut at the over instant is made there.
type EventRecapReachSource interface {
	EventRecapReach(ctx context.Context, organizationID, eventID string, overAt time.Time) (*platform.EventRecapReach, error)
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
	// Skipped is how many Events were due but whose readers or figures, sales
	// or reach, could not be read, or which could not be claimed; they stay
	// due.
	Skipped int `json:"skipped"`
	// Overlapped is how many Events were due but held by another sweep running
	// at the same time, or recapped by one since this run read its batch: left
	// to it, and written to by nobody here.
	Overlapped int `json:"overlapped"`
	// Unrecorded is how many Events were mailed but could not be recorded:
	// counted in Recapped too, and the one case that can send a second recap.
	Unrecorded int `json:"unrecorded"`
	// Unaddressed is how many Events had no Event Owner and no Org Admin to
	// write to and were recorded: nothing was sent, and they are never due
	// again. Not counted in Recapped. One that could not be recorded is not
	// counted here and stays due.
	Unaddressed int `json:"unaddressed"`
	// Backlog is how many Events are due after this run, batch or no batch:
	// the number an Operator watches drain during a catch-up.
	Backlog int `json:"backlog"`
}

// SweepEventRecaps runs one sweep: reads the batch of due Events, writes to
// each one's readers, records each Event that reached somebody, reports
// counts.
func (s *Service) SweepEventRecaps(ctx context.Context) (*EventRecapSweepResult, error) {
	result := &EventRecapSweepResult{}
	if s.eventRecaps == nil || s.eventRecapRecipients == nil || s.eventRecapReach == nil {
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

	if total, err := s.eventRecaps.CountEventRecapsDue(ctx); err != nil {
		s.logger.Error(ctx, "event recap backlog read failed", "error", err)
	} else {
		result.Backlog = total
	}

	s.logger.Info("event recaps swept",
		"due", result.Due,
		"recapped", result.Recapped,
		"sent", result.Sent,
		"failed", result.Failed,
		"skipped", result.Skipped,
		"overlapped", result.Overlapped,
		"unrecorded", result.Unrecorded,
		"unaddressed", result.Unaddressed,
		"backlog", result.Backlog,
	)
	return result, nil
}

// recapEvent writes ONE Event's recap to each of its readers and records the
// Event. It reports false when the run's budget ran out before this Event was
// begun, which ends the run with the Event untouched and still due.
//
// THE READERS ARE RESOLVED HERE, AT SEND TIME, never when the Event ended:
// somebody made an Event Owner this morning gets it, and somebody whose
// assignment was removed yesterday does not.
//
// THE BUDGET IS CHECKED ONCE PER EVENT, BEFORE ITS FIRST SEND, and an Event
// once begun is finished. The recap goes once per Event, so a run that stopped
// between two readers of one Event would leave the second without it for
// good. The overrun is bounded by one Event's readers at the pacer's gap.
//
// THE EVENT IS CLAIMED BEFORE ITS FIRST SEND, so two sweeps that overlap, the
// scheduled run and one made by hand, never both write to its readers: the
// one that loses the claim leaves the Event to the other. A claim whose sweep
// died mid-Event lapses after catalog.EventRecapClaimLease, and the next run
// writes again, to every reader: as with a recap that could not be recorded,
// the one way a reader can get a second copy.
//
// THE EVENT IS RECORDED AFTER THE SENDS AND ONLY IF ONE WAS ACCEPTED, the
// rule the Certificate Expiry Warning keeps for a mail with several readers. An
// Event that reached nobody has its claim released, stays due and the next run
// retries it. One whose provider refused some readers and accepted others is
// recorded: retrying it would write a second time to those it reached.
//
// AN EVENT THAT RESOLVES TO NOBODY IS RECORDED (#885): no Event Owner and no
// Org Admin is a state no later run is likely to change, and an Event left
// due would come back every morning and sit at the front of the batch, the
// one over longest first, until enough of them starved the rest. It is
// logged, so an operator can find it, and counted as Unaddressed.
func (s *Service) recapEvent(ctx context.Context, c catalog.EventRecapCandidate, pacer *reminderPacer, deadline time.Time, result *EventRecapSweepResult) bool {
	readers, toOrgAdmins, err := s.eventRecapReaders(ctx, c)
	if err != nil {
		result.Skipped++
		s.logger.Error(ctx, "event recap: the Event's readers could not be read; the Event stays due", "event_id", c.EventID, "error", err)
		return true
	}
	if len(readers) == 0 {
		if !s.claimEventRecap(ctx, c, result) {
			return true
		}
		s.logger.Info("event recap: the Event has no Event Owner and its Organization no Org Admin; nothing was sent and the Event is recorded as recapped",
			"event_id", c.EventID, "organization_id", c.OrganizationID)
		if err := s.eventRecaps.RecordEventRecapSent(ctx, c.EventID); err != nil {
			s.logger.Error(ctx, "event recap: an Event with nobody to write to could not be recorded; the next run will look at it again",
				"event_id", c.EventID, "error", err)
			return true
		}
		result.Unaddressed++
		return true
	}

	// The gap comes first and the budget check second, as in the Reminder
	// sweeps: a pause that spends the last of the budget ends the run here,
	// with this Event unsent, rather than one Event past it.
	pacer.pauseBefore(ctx, result.Sent+result.Failed)
	if !s.now().Before(deadline) {
		return false
	}
	if !s.claimEventRecap(ctx, c, result) {
		return true
	}

	// The figures are read once per Event, at the instant the mail says they
	// are as of, so every reader of one Event is told the same numbers. The
	// one exception is the Registration Link clicks of an Event with External
	// Registration (#888), which came with the candidate when the run began,
	// at most the run's budget earlier. An
	// Event whose figures cannot be read is not mailed without them: it stays
	// due, and the next run reads them again.
	sentAt := s.now()
	figures, err := s.eventRecapSales(ctx, c)
	if err != nil {
		result.Skipped++
		s.logger.Error(ctx, "event recap: the Event's sales figures could not be read; the Event stays due", "event_id", c.EventID, "error", err)
		s.releaseEventRecap(ctx, c)
		return true
	}
	// The reach figures stop at the instant the Event was over, whenever the
	// recap is sent, so loads of a finished Event's page are left out (#887).
	reach, err := s.eventRecapReach.EventRecapReach(ctx, c.OrganizationID, c.EventID, catalog.EventOverAt(c.StartsAt, c.EndsAt))
	if err != nil {
		result.Skipped++
		s.logger.Error(ctx, "event recap: the Event's reach figures could not be read; the Event stays due", "event_id", c.EventID, "error", err)
		s.releaseEventRecap(ctx, c)
		return true
	}
	// An Event with External Registration states its Registration Link clicks
	// in place of every sales figure and links to Reach only (#888). The mail
	// keeps that rule, in one place: it prints neither of the other two URLs
	// for a recap that carries the clicks.
	eventURL := s.staffBaseURL + "/events/" + c.EventID
	recap := platform.EventRecap{
		EventName:              c.EventName,
		SentAt:                 sentAt,
		SalesTrendsURL:         eventURL + "/sales/trends",
		ReachURL:               eventURL + "/reach",
		SalesURL:               eventURL + "/sales",
		Sales:                  figures,
		Reach:                  reach,
		RegistrationLinkClicks: c.RegistrationLinkClicks,
	}
	accepted := 0
	for _, to := range readers {
		pacer.pauseBefore(ctx, result.Sent+result.Failed)
		recap.To, recap.Locale, recap.ToOrgAdmin = to, s.staffLocale(ctx, to), toOrgAdmins
		err := s.email.SendEventRecap(ctx, recap)
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
		s.releaseEventRecap(ctx, c)
		return true
	}
	result.Recapped++
	if err := s.eventRecaps.RecordEventRecapSent(ctx, c.EventID); err != nil {
		result.Unrecorded++
		s.logger.Error(ctx, "an Event Recap was sent but could not be recorded; the next run will send it again",
			"event_id", c.EventID, "error", err)
	}
	return true
}

// claimEventRecap takes one Event for this sweep and reports whether it may
// write to the Event's readers. An Event another sweep holds or has recapped
// is counted as Overlapped, and one whose claim could not be written as
// Skipped: neither is written to, and the second stays due.
func (s *Service) claimEventRecap(ctx context.Context, c catalog.EventRecapCandidate, result *EventRecapSweepResult) bool {
	claimed, err := s.eventRecaps.ClaimEventRecap(ctx, c.EventID)
	if err != nil {
		result.Skipped++
		s.logger.Error(ctx, "event recap: the Event could not be claimed; the Event stays due", "event_id", c.EventID, "error", err)
		return false
	}
	if !claimed {
		result.Overlapped++
		s.logger.Info("event recap: the Event is held or was recapped by an overlapping sweep; left to it", "event_id", c.EventID)
		return false
	}
	return true
}

// releaseEventRecap gives back the claim on an Event no mail went out for, so
// the next run retries it. A release that fails is logged and costs nothing
// but time: the claim lapses after its lease.
func (s *Service) releaseEventRecap(ctx context.Context, c catalog.EventRecapCandidate) {
	if err := s.eventRecaps.ReleaseEventRecap(ctx, c.EventID); err != nil {
		s.logger.Error(ctx, "event recap: the claim on an Event nothing was sent for could not be released; it lapses after its lease",
			"event_id", c.EventID, "error", err)
	}
}

// eventRecapReaders is who one Event's recap goes to, resolved now: every
// Event Owner of the Event by its Event assignments, or, when it has none,
// every Org Admin of its Organization (#885). The Org Admins are the fallback
// and never an addition, so an Event with an Event Owner does not mail every
// Org Admin of a busy Organization. One copy per person: the addresses are
// compared normalized, so a Member who holds an Event Owner assignment and was
// later made Org Admin is written to once.
//
// toOrgAdmins says which of the two lists it is, which the mail's footer
// states as the reason it was sent.
func (s *Service) eventRecapReaders(ctx context.Context, c catalog.EventRecapCandidate) (readers []string, toOrgAdmins bool, err error) {
	owners, err := s.eventRecapRecipients.EventOwnerEmails(ctx, c.EventID)
	if err != nil {
		return nil, false, err
	}
	if readers := distinctEmails(owners); len(readers) > 0 {
		return readers, false, nil
	}
	admins, err := s.eventRecapRecipients.OrgAdminEmails(ctx, c.OrganizationID)
	if err != nil {
		return nil, false, err
	}
	return distinctEmails(admins), true, nil
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

// eventRecapChannels is the Sales Channels in the order the recap lists them,
// each stated even where it sold nothing, so a reader sees that the door or
// the imports moved no ticket rather than wondering whether they were counted.
var eventRecapChannels = []string{catalog.SalesChannelOnline, catalog.SalesChannelInPerson, catalog.SalesChannelImport}

// eventRecapSales is one Event's sales figures as they stand now, for its
// Event Recap (#886), or nil for an Event with External Registration, which
// produces no Ticket Sale and so has none.
//
// NO NEW ARITHMETIC. Each figure is read from the read model that already
// states it on the staff surface the mail links to, so the two agree:
//
//   - Tickets Sold, Ticket Sales and Net Proceeds for the Event entire are
//     the Sales summary's (repo.SalesSummary).
//   - Takings, and Tickets Sold by Ticket Type, are Sales Trends' cells
//     (repo.SalesTrends) added over the Event's days, as its charts add
//     them, with the catalog its legend lists (repo.ListEventTicketTypes),
//     a Ticket Type that sold nothing included.
//   - The count of reversed Sales is the one both surfaces state
//     (repo.ReversedSalesCount).
//   - Tickets Sold and Ticket Sales by Sales Channel are
//     repo.EventSalesByChannel, the one read this adds: the Sales summary's
//     two counts grouped by channel, as Organization Trends groups them.
//
// Every one reads ACTIVE Ticket Sales only, so a Sale Reversal drops its
// Sale whole from all of them.
//
// The reads are separate statements, not one snapshot. A sale recorded
// between two of them would be in one figure and not another; the recap goes
// the morning after the Event is over, and says its figures are a reading at
// an instant, so the reads are not wrapped in a transaction for it.
func (s *Service) eventRecapSales(ctx context.Context, c catalog.EventRecapCandidate) (*platform.EventRecapSales, error) {
	// The candidate says which kind of Event this is, once for the whole
	// recap: one with External Registration states its Registration Link
	// clicks in place of these figures (#888).
	if c.ExternalRegistration() {
		return nil, nil
	}
	event, ok, err := s.repo.GetEventImportContext(ctx, c.OrganizationID, c.EventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	summary, err := s.repo.SalesSummary(ctx, c.OrganizationID, c.EventID)
	if err != nil {
		return nil, err
	}
	buckets, err := s.repo.SalesTrends(ctx, c.OrganizationID, c.EventID, resolveEventLocation(event.Timezone).String())
	if err != nil {
		return nil, err
	}
	ticketTypes, err := s.repo.ListEventTicketTypes(ctx, c.OrganizationID, c.EventID)
	if err != nil {
		return nil, err
	}
	channels, err := s.repo.EventSalesByChannel(ctx, c.OrganizationID, c.EventID)
	if err != nil {
		return nil, err
	}
	reversed, err := s.repo.ReversedSalesCount(ctx, c.OrganizationID, c.EventID)
	if err != nil {
		return nil, err
	}

	figures := &platform.EventRecapSales{
		Currency:         event.Currency,
		TicketsSold:      summary.TicketsSold,
		TicketSales:      summary.SalesCount,
		NetProceedsCents: summary.NetProceedsCents,
		ReversedSales:    reversed,
	}

	soldByType := make(map[string]int, len(ticketTypes))
	for _, bucket := range buckets {
		figures.TakingsCents += bucket.TakingsCents
		soldByType[bucket.TicketTypeID] += bucket.Quantity
	}
	for _, ticketType := range ticketTypes {
		figures.TicketTypes = append(figures.TicketTypes, platform.EventRecapTicketType{
			Name: ticketType.Name, TicketsSold: soldByType[ticketType.ID],
		})
	}

	// The channel column admits these three and no other (migration 010), so
	// the rows add up to the totals above.
	byChannel := make(map[string]repository.EventChannelSales, len(channels))
	for _, channel := range channels {
		byChannel[channel.Channel] = channel
	}
	for _, name := range eventRecapChannels {
		sold := byChannel[name]
		figures.Channels = append(figures.Channels, platform.EventRecapChannel{
			Channel: name, TicketsSold: sold.TicketsSold, TicketSales: sold.TicketSales,
		})
	}
	return figures, nil
}
