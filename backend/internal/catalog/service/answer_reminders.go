package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Answer Reminder's half that belongs to the catalog (#317, ADR 0044; #328,
// parent #322, ADR 0046): which Tickets may be chased, WHO for, and the record
// of who has been told.
//
// THE SPLIT, STATED ONCE. The reminder is a mail, so the sales module composes
// and sends it — that is where the Confirmation Link, the Mail Locale chain and
// the transactional sender all already are. What this module owns is the DEBT,
// the RATIONING and the RECIPIENT: what counts as an Outstanding Answer (#313),
// whether a Ticket may be chased now (catalog.MayRemind), who the chasing is
// addressed to (catalog.AssignmentState), and the ledger that makes "at most two
// ever" a fact rather than a hope. Sales asks; the catalog decides who.
//
// TWO GATES, BOTH HERE. The sweep's SQL rations so that a batch cannot be
// starved by Tickets it may not chase, and catalog.MayRemind is applied again to
// every row that comes back before the sales module ever sees it. That is not
// belt and braces for its own sake: the Go rule is the authoritative statement,
// the SQL exists so the database can skip, and a disagreement between them must
// resolve as SILENCE. A candidate refused here costs one wasted row; a candidate
// that slipped through would be a mail somebody was rationed out of receiving.
//
// AND ONE PIECE OF ASSEMBLY, WHICH IS NEW IN #328 AND FINISHED BY #335. The
// query returns TICKETS and the sweep sends MAILS, and the fan-in between them
// is here: every buyer-addressed Ticket of one Sale becomes one message, and
// every Ticket one HOLDER accepted becomes one message to that Holder — one
// mail per Holder per sweep, listing each owed Ticket with its own Assignment
// Link. It is done in Go rather than in SQL because it is a product rule about
// inboxes — "whoever is chased is one person with one inbox, however many
// Tickets are owed" — and a rule expressed as an array_agg over a CASE is a
// rule no unit test can reach.

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
// THE SECOND FLAG ONLY MOVES THE ADDRESS. ticketAssignmentEnabled is a separate
// switch from ticketQuestionsEnabled and must stay separate (ADR 0046): closed,
// every Ticket is chased through its buyer, which is exactly the behaviour that
// shipped under ADR 0044. It never changes whether somebody is due — a
// deployment that closed the flag after Tickets had been accepted must not
// silently stop chasing them, and the debt is the debt either way.
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
// the number it asked for — usually far fewer, since a four-Ticket sale with
// nothing accepted is four candidates and one mail. That is the right way round:
// what the database has to be stopped from returning is rows.
func (s *Service) AnswerRemindersDue(ctx context.Context, limit int) ([]catalog.DueAnswerReminder, error) {
	if !s.ticketQuestionsEnabled {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}

	now := s.now()
	candidates, err := s.repo.ListAnswerReminderCandidates(
		ctx, now, now.Add(-catalog.AnswerReminderInterval), s.ticketAssignmentEnabled, limit,
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

	return s.groupAnswerReminders(allowed, trailingSaleToDefer(candidates, limit)), nil
}

// trailingSaleToDefer names the Ticket Sale whose buyer must NOT be mailed this
// run, because the batch cut its Tickets in half.
//
// THE PROBLEM IT SOLVES IS A DUPLICATE, NOT A DELAY. A buyer's mail covers every
// Ticket of their Sale that is still theirs to chase, and spends one ledger row
// for each. If a batch boundary falls inside a Sale, this run mails them about
// two Tickets, spends two allowances, and TOMORROW'S run — finding the other two
// still owed and never chased — mails the same person again. Two messages in two
// days, from a job whose whole promise is one a week. The rationing cannot catch
// it, because per-Ticket rationing is exactly what makes the second mail
// legitimate. SINCE #335 THE SAME SHAPE THREATENS A HOLDER — their mail covers
// every owed Ticket they accepted, so a boundary between two of them inside one
// Sale would split their envelope in two — which is why the deferral now holds
// for both recipients rather than only the buyer's half. A Holder whose Tickets
// span two SALES, one beyond the batch, can still be written to on two runs;
// that split is invisible from inside one batch, each mail respects every
// per-Ticket cap, and a rarity is the right price for a bound the query can
// actually promise.
//
// SO THE TRAILING SALE WAITS A DAY, which costs nothing: nothing about the
// rationing depends on a Ticket being reached on any particular day, and the
// Sale is whole in the next run's batch. This is only possible because the query
// orders by sale and then by ticket, so a truncated result can only ever have
// cut the LAST one.
//
// IT DOES NOT FIRE UNLESS THE BATCH WAS FULL. A short result was not truncated,
// so its last Sale is whole and there is nothing to defer.
//
// AND IT REFUSES TO STARVE. A Sale with more Tickets than the whole batch would
// be deferred by every run forever, so when the batch holds only one Sale it is
// mailed as it stands — a duplicate on a 50-Ticket order being the lesser fault
// against never chasing it at all.
//
// It returns "" when there is nothing to defer, which is the ordinary case.
func trailingSaleToDefer(candidates []catalog.AnswerReminderCandidate, limit int) string {
	if len(candidates) < limit {
		return ""
	}
	trailing := candidates[len(candidates)-1].TicketSaleID
	for _, candidate := range candidates {
		if candidate.TicketSaleID != trailing {
			return trailing
		}
	}
	// Every candidate in a full batch belongs to one Sale: deferring it would
	// defer it forever.
	return ""
}

// groupAnswerReminders turns candidate Tickets into the messages the sweep will
// send: one per Sale for each buyer, one per HOLDER for the accepted (#335).
//
// ONE MAIL PER HOLDER PER SWEEP is the ruling #335 recorded, and Story 48's own
// clause — "chase two of us without mailing either twice" — stated as code. The
// buyer's side already fanned a Sale's Tickets into one message, so per-Ticket
// envelopes to a Holder were an inconsistency as well as a volume problem, and
// mailing one address twice in one sweep is the shape spam filters punish. The
// grouping key is the HOLDER'S ADDRESS and nothing else: a person who accepted
// Tickets on two Sales in one batch still gets ONE mail, because the promise is
// about their inbox and not about any Sale. Each listed Ticket keeps its own
// Assignment Link — a link still opens exactly one Ticket — and burns its own
// allowance; only the envelope is shared.
//
// THE ORDER OF THE OUTPUT IS THE ORDER THE TICKETS ARRIVED IN, which is oldest
// sale first. Under a batch smaller than the backlog that is what decides who
// waits, and the buyer who paid in January and has said nothing since is the one
// whose silence has run longest.
//
// A HOLDER'S MAIL IS COMPOSED HERE AND NOWHERE ELSE, because it carries
// Assignment Links and this module is the only one that may mint them (ADR
// 0046). A Ticket whose link cannot be signed is DROPPED rather than listed
// linkless — the rest of that Holder's mail still goes — and nothing is
// recorded for it, so it is due again as soon as the deployment has a link
// secret. The only way to reach that is a service NewApp refuses to build in
// production.
//
// A BUYER'S MAIL CARRIES NO HOLDER FACT AND A HOLDER'S CARRIES NO BUYER FACT,
// and the fields are assigned in two separate literals rather than one shared
// one so that stays visible. The stronger enforcement is downstream — the two
// platform mail types have no field for the other's facts — but a reader of this
// function should be able to see the boundary without following it there.
func (s *Service) groupAnswerReminders(
	candidates []catalog.AnswerReminderCandidate,
	deferSale string,
) []catalog.DueAnswerReminder {
	due := make([]catalog.DueAnswerReminder, 0, len(candidates))
	// bySale and byHolder index into `due` rather than holding values, so a
	// Sale's — or a Holder's — second Ticket appends to the message its first
	// one created instead of building a copy that is then thrown away.
	bySale := make(map[string]int, len(candidates))
	byHolder := make(map[string]int, len(candidates))

	for _, candidate := range candidates {
		// A Sale the batch cut in half waits for the next run rather than having
		// anybody on it mailed twice in two days — see trailingSaleToDefer. It
		// holds for BOTH recipients since #335: a Holder whose two Tickets
		// straddled the boundary would otherwise get this sweep's mail about one
		// and tomorrow's about the other.
		if candidate.TicketSaleID == deferSale {
			continue
		}

		if candidate.Recipient == catalog.RemindTheHolder {
			link := s.assignmentLinkURL(candidate.TicketID, candidate.AssignedAt, candidate.HolderEmail)
			if link == "" {
				if s.logger != nil {
					s.logger.Warn("could not sign the Assignment Link for a Holder's Answer Reminder; the Ticket was left off the mail and stays due",
						"ticket_sale_id", candidate.TicketSaleID)
				}
				continue
			}
			ticket := catalog.HolderReminderTicket{
				EventName:      candidate.EventName,
				TicketTypeName: candidate.TicketTypeName,
				AssignmentLink: link,
			}
			if at, found := byHolder[candidate.HolderEmail]; found {
				due[at].TicketIDs = append(due[at].TicketIDs, candidate.TicketID)
				due[at].HolderTickets = append(due[at].HolderTickets, ticket)
				continue
			}
			byHolder[candidate.HolderEmail] = len(due)
			due = append(due, catalog.DueAnswerReminder{
				Recipient: catalog.RemindTheHolder,
				TicketIDs: []string{candidate.TicketID},
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
				SaleLocale:    candidate.SaleLocale,
				HolderEmail:   candidate.HolderEmail,
				HolderTickets: []catalog.HolderReminderTicket{ticket},
			})
			continue
		}

		// The buyer's half.
		if at, found := bySale[candidate.TicketSaleID]; found {
			due[at].TicketIDs = append(due[at].TicketIDs, candidate.TicketID)
			continue
		}
		bySale[candidate.TicketSaleID] = len(due)
		due = append(due, catalog.DueAnswerReminder{
			Recipient:         catalog.RemindTheBuyer,
			TicketIDs:         []string{candidate.TicketID},
			TicketSaleID:      candidate.TicketSaleID,
			SaleStatus:        candidate.SaleStatus,
			EventStartsAt:     candidate.EventStartsAt,
			EventEnd:          candidate.EventEnd,
			EventName:         candidate.EventName,
			SaleLocale:        candidate.SaleLocale,
			ConfirmationRef:   candidate.ConfirmationRef,
			CustomerEmail:     candidate.CustomerEmail,
			CustomerFirstName: candidate.CustomerFirstName,
			CustomerLastName:  candidate.CustomerLastName,
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
	return s.repo.CountAnswerRemindersDue(
		ctx, now, now.Add(-catalog.AnswerReminderInterval), s.ticketAssignmentEnabled,
	)
}

// RecordAnswerRemindersSent writes the ledger rows that ration the next
// reminder: one per Ticket the mail that just went out covered.
//
// IT TAKES THE SET THE MAIL COVERED and never a Ticket Sale, which is the whole
// of what #328 moved. A buyer's message about three Tickets spends three
// allowances and leaves the fourth — answered, already chased twice, or added to
// the Sale later — with the allowance nobody spent on its behalf.
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
