package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Answer Reminder's half that belongs to the catalog (#317, ADR 0044; #328,
// parent #322, ADR 0046; #347, parent #342, ADR 0049): which Tickets may be
// chased, WHO for, and the record of who has been told.
//
// THE SPLIT, STATED ONCE. The reminder is a mail, so the sales module composes
// and sends it — that is where the Mail Locale chain and the transactional
// sender already are. What this module owns is the DEBT, the RATIONING and the
// RECIPIENT: what counts as an Outstanding Answer (#313), whether a Ticket may
// be chased now (catalog.MayRemind), who the chasing is addressed to
// (catalog.AnswerReminderRecipient), and the ledger that makes "at most two
// ever" a fact rather than a hope. Sales asks; the catalog decides who.
//
// HOLDER OR NOBODY SINCE ADR 0049. The reminder goes to the address that
// accepted a Ticket — the buyer for their Self-held Ticket, a named Holder for
// an accepted one — and an `unassigned`, `assigned` or purged Ticket is simply
// not chased: nobody can answer for it. There is no buyer mail, no "mail the
// buyer instead" while Ticket Assignment is dark, and no batch deferral, all of
// which existed to address a purchase rather than a Ticket.
//
// TWO GATES, BOTH HERE. The sweep's SQL rations so that a batch cannot be
// starved by Tickets it may not chase, and catalog.MayRemind is applied again to
// every row that comes back before the sales module ever sees it. That is not
// belt and braces for its own sake: the Go rule is the authoritative statement,
// the SQL exists so the database can skip, and a disagreement between them must
// resolve as SILENCE. A candidate refused here costs one wasted row; a candidate
// that slipped through would be a mail somebody was rationed out of receiving.
//
// AND ONE PIECE OF ASSEMBLY, FROM #335. The query returns TICKETS and the sweep
// sends MAILS, and the fan-in between them is here: every owed Ticket one
// HOLDER holds becomes one message to that Holder — one mail per Holder per
// sweep, listing each owed Ticket. It is done in Go rather than in SQL because
// it is a product rule about inboxes — "whoever is chased is one person with
// one inbox, however many Tickets are owed" — and a rule expressed as an
// array_agg is a rule no unit test can reach.

// AnswerRemindersDue returns the MAILS that may be sent now, oldest sale first,
// built from at most limit candidate Tickets.
//
// THE FLAG IS READ HERE, and a dark deployment returns nothing at all (ADR
// 0045). It is the same gate TicketSaleHasOutstandingAnswers applies to the Sale
// Confirmation's one sentence, and it matters for the same reason with more
// force: this mail is entirely about Ticket Questions, and a whole message about
// them landing in an inbox before the Privacy Policy describes the collection is
// precisely the failure ADR 0045 exists to prevent.
//
// THE TICKET ASSIGNMENT FLAG IS NOT READ. With it dark nothing is ever accepted
// (a Self-held Ticket is minted only while it is open, ADR 0048), so there is
// nothing to hide; and a deployment that closed it after Tickets had been
// accepted keeps chasing their Holders, who answer from the Customer Area
// either way. The debt is the debt.
//
// IT IS GATED WHERE THE PURGE IS NOT, which will look inconsistent to somebody
// reading the two jobs side by side and is deliberate. The purge is the promise
// the platform made about data it already collected, so gating it would mean a
// deployment that stopped asking questions kept every answer forever. This one
// SENDS. A send is the thing the flag exists to hold back, and a reminder about
// questions a closed deployment no longer asks anyone would be a mail with
// nothing behind it.
//
// The clock is this service's own and is never a caller's. The cooldown cutoff
// is derived from it here rather than accepted as an argument, on the standing
// rule of the internal namespace: a caller who could name the cutoff could lift
// the seven-day silence on demand and mail the platform's whole outstanding
// backlog twice in an afternoon.
//
// LIMIT BOUNDS TICKETS AND NOT MAILS, so a run may return fewer messages than
// the number it asked for — a Holder of four owed Tickets is four candidates
// and one mail. That is the right way round: what the database has to be
// stopped from returning is rows.
func (s *Service) AnswerRemindersDue(ctx context.Context, limit int) ([]catalog.DueAnswerReminder, error) {
	if !s.ticketQuestionsEnabled {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}

	now := s.now()
	candidates, err := s.repo.ListAnswerReminderCandidates(
		ctx, now, now.Add(-catalog.AnswerReminderInterval), limit,
	)
	if err != nil {
		return nil, err
	}

	// The second gate. Each candidate Ticket is re-decided through the Go
	// statement of the rule, against the same clock the query used, so that the
	// SQL is a bounded prefilter and never the last word on whether somebody is
	// written to.
	allowed := make([]catalog.AnswerReminderCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if catalog.MayRemind(candidate.Inputs(now)) {
			allowed = append(allowed, candidate)
		}
	}

	return groupAnswerReminders(allowed), nil
}

// groupAnswerReminders turns candidate Tickets into the messages the sweep will
// send: one per HOLDER (#335).
//
// ONE MAIL PER HOLDER PER SWEEP is the ruling #335 recorded, and Story 48's own
// clause — "chase two of us without mailing either twice" — stated as code.
// Mailing one address twice in one sweep is the shape spam filters punish. The
// grouping key is the HOLDER'S ADDRESS and nothing else: a person who holds
// Tickets on two Sales in one batch still gets ONE mail, because the promise is
// about their inbox and not about any Sale. Each listed Ticket burns its own
// allowance; only the envelope is shared.
//
// THE ORDER OF THE OUTPUT IS THE ORDER THE TICKETS ARRIVED IN, which is oldest
// sale first. Under a batch smaller than the backlog that is what decides who
// waits, and the person whose Ticket was bought in January and who has said
// nothing since is the one whose silence has run longest.
//
// NO LINK IS COMPOSED HERE. The mail points at the Customer Area, one address
// for the whole message, and the sales module knows the Storefront's origin;
// this function has no credential to mint and nothing that could fail to sign.
// A candidate that made it here is a mail, or part of one.
//
// A HOLDER'S MAIL CARRIES NO BUYER FACT, and the enforcement is that neither
// the candidate nor the message has a field for one. A reader of this function
// should be able to see the boundary without following it there: what is
// copied is an address, two public names and the Sale's locale fallback.
func groupAnswerReminders(candidates []catalog.AnswerReminderCandidate) []catalog.DueAnswerReminder {
	due := make([]catalog.DueAnswerReminder, 0, len(candidates))
	// byHolder indexes into `due` rather than holding values, so a Holder's
	// second Ticket appends to the message their first one created instead of
	// building a copy that is then thrown away.
	byHolder := make(map[string]int, len(candidates))

	for _, candidate := range candidates {
		ticket := catalog.HolderReminderTicket{
			EventName:      candidate.EventName,
			TicketTypeName: candidate.TicketTypeName,
		}
		if at, found := byHolder[candidate.HolderEmail]; found {
			due[at].TicketIDs = append(due[at].TicketIDs, candidate.TicketID)
			due[at].Tickets = append(due[at].Tickets, ticket)
			continue
		}
		byHolder[candidate.HolderEmail] = len(due)
		due = append(due, catalog.DueAnswerReminder{
			HolderEmail: candidate.HolderEmail,
			TicketIDs:   []string{candidate.TicketID},
			Tickets:     []catalog.HolderReminderTicket{ticket},
			// The Sale facts below are the FIRST listed Ticket's, and they are
			// carried for the log line and the locale fallback only: a mail
			// grouped per Holder may span Sales, and no Sale fact ever reaches
			// the message itself.
			TicketSaleID:  candidate.TicketSaleID,
			SaleStatus:    candidate.SaleStatus,
			EventStartsAt: candidate.EventStartsAt,
			EventName:     candidate.EventName,
			// The Sale Locale travels, and the sales module reads it LAST for
			// this reader: a Holder did not buy anything and was never on the
			// page that recorded it, so their own remembered Mail Locale
			// outranks it (#325's inversion, ADR 0033).
			SaleLocale: candidate.SaleLocale,
		})
	}
	return due
}

// CountAnswerRemindersDue is how many MAILS are due in all, ignoring any batch:
// the standing backlog an operator reads two runs apart to tell a sweep that is
// keeping up from one that is not.
//
// It counts messages rather than Tickets so that it can be compared against the
// `sent` a run reports; see the query for how the same fan-in is expressed in
// SQL.
//
// It is REPORTING and not the job, which is why a dark deployment answers zero
// rather than refusing: zero is the truthful backlog of a platform that will
// send nothing.
func (s *Service) CountAnswerRemindersDue(ctx context.Context) (int, error) {
	if !s.ticketQuestionsEnabled {
		return 0, nil
	}
	now := s.now()
	return s.repo.CountAnswerRemindersDue(ctx, now, now.Add(-catalog.AnswerReminderInterval))
}

// RecordAnswerRemindersSent writes the ledger rows that ration the next
// reminder: one per Ticket the mail that just went out covered.
//
// IT TAKES THE SET THE MAIL COVERED and never a Ticket Sale, which is the whole
// of what #328 moved. A message about three Tickets spends three allowances and
// leaves the fourth — answered, already chased twice, or added to the Sale
// later — with the allowance nobody spent on its behalf.
//
// It takes no moment: the clock is this service's, for the same reason the
// sweep's is. A caller that could name when a reminder "was" sent could date one
// eight days ago and mail the same person again tomorrow.
//
// THE CALLER MUST HAVE SENT ALREADY. This is the record of a message a provider
// has accepted, and writing it first would ration somebody out of a reminder
// they never received — permanently, since the cap counts for the life of the
// Ticket. See repository.RecordAnswerRemindersSent for why the other failure is
// the one worth having.
func (s *Service) RecordAnswerRemindersSent(ctx context.Context, ticketIDs []string) error {
	return s.repo.RecordAnswerRemindersSent(ctx, ticketIDs, s.now())
}
