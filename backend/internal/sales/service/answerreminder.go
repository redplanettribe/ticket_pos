package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Answer Reminder sweep: the mail telling somebody that a Ticket still owes
// an Answer, and pointing them at the surface where they can give it (#317,
// ADR 0044; #328, parent #322, ADR 0046).
//
// IT WRITES TO WHOEVER CAN ACTUALLY ANSWER, which is the change #328 made and
// the only one. ADR 0044 addressed this mail to the buyer "because there is
// nobody else to address"; ADR 0046 gave a Ticket a Holder who accepts by mail,
// so there is now, and chasing the buyer for a Ticket somebody else accepted
// nags the one person this feature established does not know their friend's
// t-shirt size. The Holder for an `accepted` Ticket, the buyer for every other
// Ticket on the Sale, and a Sale with a mix produces both.
//
// SWEPT, NOT TRIGGERED BY THE EDIT, and that is the original ticket's central
// decision rather than an implementation choice. A reminder raised when a Ticket
// Question is authored would mail the same people four times in the ten minutes
// an Organization spends drafting four questions — and would mail them again the
// next morning when somebody fixed a typo in one. A job that sweeps sees the
// state, not the events, so no amount of authoring produces more than the
// rationing allows. #328 makes that stronger rather than weaker: there are more
// inboxes to get this wrong in now.
//
// IT IS A JOB IN THE SHAPE THIS BACKEND ALREADY HAS ONE — an internal endpoint a
// scheduler calls and a human can curl — for the reason answerpurge.go gives and
// ADR 0024 recorded: nothing in this process outlives a request, since the API
// runs with min_instances 0 and cpu_idle, so a ticker would fire only while
// somebody happened to be browsing.
//
// WHERE THE PIECES LIVE. The debt, the rationing, the RECIPIENT and the ledger
// are the catalog module's (catalog.MayRemind, catalog.AssignmentState,
// catalog/service/answer_reminders.go): what counts as an Outstanding Answer is
// #313's rule, who holds a Ticket is ADR 0046's, and there may not be a second
// copy of either. What is here is the MAIL — the links, the Mail Locales and the
// transactional sender. This file decides nothing about who is owed a reminder
// or who it is addressed to; it asks, and then writes to whoever comes back.
//
// IT SHIPS PAUSED. The Cloud Scheduler job is created with paused = true
// (answer_reminder_enabled defaults false), exactly as the Follow Digest's two
// schedulers and #316's purge did, and the catalog's side reads
// TICKET_QUESTIONS_ENABLED on top of that — so a deployment has to make two
// deliberate decisions before anybody is written to. #328 does not change that
// and does not unpause anything.

// AnswerReminderSource is what sales needs from catalog to send Answer
// Reminders: who is due one, and the record that one was sent.
//
// THREE METHODS, AND EVERY JUDGEMENT IS ON THE FAR SIDE OF THEM. This module
// does not know what a Ticket Question is, that a retired one owes nothing, that
// a reversed Sale's Tickets have ceased to exist, that the reminder falls silent
// when the doors open, that two is the lifetime cap, or — since #328 — what it
// means for a Ticket to have been accepted. It knows that some messages are due
// and that sending one has to be written down.
//
// It is the same arrangement OutstandingAnswerReporter has for the receipt's one
// sentence, widened rather than duplicated in spirit: one module owns the debt
// and the assignment, the other owns the mail. The alternative — sales counting
// unanswered rows, reading assignment columns and keeping its own ledger — would
// be a second definition of the Outstanding Answer AND a second reader of the
// three assignment states, which is two rules this platform keeps in one place
// each.
//
// OPTIONAL, like OutstandingAnswerReporter. A deployment that never wires it
// sweeps nothing and reports zeros, which is the correct behaviour while the
// feature is dark and the correct behaviour if somebody forgets. Nobody is
// mailed by accident; the failure mode of an unwired seam is silence.
type AnswerReminderSource interface {
	// AnswerRemindersDue returns the MAILS that may be sent now, oldest sale
	// first, built from at most limit candidate Tickets. It reads both feature
	// flags, derives each Ticket's recipient and applies the whole of
	// catalog.MayRemind on its own side, so a dark deployment — and a Ticket that
	// has had its two, or whose Event has started, or whose Sale was reversed —
	// never reaches this module at all.
	//
	// THE LIMIT COUNTS TICKETS AND THE RESULT COUNTS MAILS, so this returns fewer
	// values than it was asked for whenever a Sale has more than one Ticket still
	// owing. That is the right way round: what a batch has to bound is rows read.
	AnswerRemindersDue(ctx context.Context, limit int) ([]catalog.DueAnswerReminder, error)
	// CountAnswerRemindersDue is the standing backlog in MAILS, ignoring any
	// batch: reporting, not the job.
	CountAnswerRemindersDue(ctx context.Context) (int, error)
	// RecordAnswerRemindersSent appends the ledger rows that ration the next
	// reminder — one per Ticket the message covered. Called only after the
	// provider has accepted it.
	RecordAnswerRemindersSent(ctx context.Context, ticketIDs []string) error
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
// accepted — which costs somebody a duplicate reminder on a later tick.
//
// THE BATCH COUNTS TICKETS AND NOT MAILS SINCE #328, and it is unchanged at 50
// deliberately rather than by omission. Fifty candidate Tickets is at most fifty
// messages — the case where every one of them is a separate accepted Holder —
// and usually far fewer, because a Sale's buyer-addressed Tickets collapse into
// one. The old number therefore still bounds the same worst case it always did,
// and raising it to keep the mail count constant would raise the worst case
// instead.
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
// on a Ticket being reached on any particular day.
const (
	answerReminderBatch  = 50
	answerReminderBudget = 60 * time.Second
)

// AnswerReminderSweepResult is what one run did, and what is still waiting.
//
// A tally per OUTCOME rather than a bare 200, on the Reversal Reconciler's and
// the purge's terms: this endpoint is the runbook as much as it is the
// automation's entry point, and "we mailed forty people" says nothing an
// operator can act on where "thirty-eight sent, two had no link" says all of it.
//
// EVERY FIGURE HERE COUNTS MESSAGES, never Tickets and never people. One mail
// covering four of a buyer's Tickets is one Sent and four ledger rows; a Sale
// with two accepted Holders and two unaccepted Tickets is three Sent. The
// distinction is not pedantry — it is what makes `sent` comparable against
// `due_total` two runs apart, which is the only thing an operator reads this for.
//
// AND IT NAMES NOBODY. There is no address, no Ticket, no Sale and no Event
// anywhere in this struct, deliberately: a response listing who had just been
// chased about unanswered questions would publish, to anything holding the
// scheduler's token, a list of people being chased — and this feature's whole
// premise is that an Answer may be health data.
type AnswerReminderSweepResult struct {
	// Due is how many MAILS this run found to send, after grouping and bounded by
	// the batch. It is what the run had to work with, and DueTotal below is what
	// there was.
	Due int `json:"due"`
	// Sent is reminders the provider accepted and the ledger recorded, to buyers
	// and Holders alike. On a platform where the feature ships dark and the job
	// ships paused, zero is the only answer.
	//
	// IT DOES NOT SAY WHICH KIND, and adding a breakdown was considered and
	// refused: on a platform with one Organization, "two of today's reminders
	// went to Holders" is close enough to naming somebody, and an operator
	// diagnosing this job needs to know that mail moved rather than who read it.
	Sent int `json:"sent"`
	// Skipped is candidates this run deliberately did not mail: a link that could
	// not be signed, or an address the row does not carry. NOTHING WAS SENT and
	// nothing was recorded, so they are due again on the next tick — which is
	// right, because the fault is the deployment's rather than the reader's.
	//
	// A number that stays high is a misconfiguration, not a backlog: the only way
	// to fail to sign a Confirmation Link or an Assignment Link is to have no
	// link secret.
	Skipped int `json:"skipped"`
	// Failed is reminders the provider refused. They are NOT recorded in the
	// ledger, so the reader is due again on the next tick and has lost nothing.
	// This is the number that says a provider is unwell.
	Failed int `json:"failed"`
	// Unrecorded is sends the provider accepted whose ledger rows could not be
	// written. It is its own number rather than folded into Failed because it
	// means the opposite thing: the reader HAS the mail, and the platform has
	// forgotten it sent it, so they may receive one more than the cap intended.
	//
	// It should always be zero. A non-zero value is the one outcome of this job
	// that is worth waking somebody for, because the rationing is only as true as
	// that table.
	Unrecorded int `json:"unrecorded"`
	// DueTotal is how many reminders are due across the platform, ignoring the
	// batch — the standing backlog, in the sense the purge's answers_held is. Two
	// curls a day apart say whether the sweep is keeping up.
	DueTotal int `json:"due_total"`
}

// SweepAnswerReminders mails the people who can answer what an active Ticket
// Sale's Tickets still owe, and nobody else.
//
// WHAT IT DOES NOT DO is decide who. Every rule — the debt, the seven days, the
// cap of two, the silence once the Event has started, the reversed Sale, and
// since #328 whether a Ticket's mail is addressed to its Holder or to the buyer
// — is applied by the catalog module before a candidate is returned, and this
// loop composes a message for each one it is handed. That separation is what
// stops the rationing from being restated here, where it would be a third copy
// after the SQL and catalog.MayRemind.
//
// THE ORDER OF SEND-THEN-RECORD IS LOAD-BEARING and is the one thing in this
// file worth reading twice. Recording first and failing to send would ration
// somebody out of a reminder they never received, permanently, since the cap
// counts for the life of the Ticket. Sending first and failing to record costs
// at most one duplicate on a later tick, reported as Unrecorded so nobody has to
// guess which happened.
//
// A FAILED SEND IS NOT A FAILED RUN. One provider refusal must not abandon the
// forty readers behind it in the batch — the reason the Reversal Reconciler
// works item by item — so failures are counted and the loop continues. What DOES
// stop the run is the read that produced the candidates, because a run that
// could not see who was owed has done nothing and must not report a quiet
// success.
func (s *Service) SweepAnswerReminders(ctx context.Context) (*AnswerReminderSweepResult, error) {
	result := &AnswerReminderSweepResult{}

	// An unwired seam sweeps nothing, exactly as it reports no Outstanding
	// Answers on a receipt. Silence is what an unconfigured deployment gets.
	if s.answerReminders == nil {
		return result, nil
	}

	due, err := s.answerReminders.AnswerRemindersDue(ctx, answerReminderBatch)
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
	if total, err := s.answerReminders.CountAnswerRemindersDue(ctx); err != nil {
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

// sendAnswerReminder writes to one reader and records that it did, tallying the
// outcome. It never returns an error: every way this can go wrong is one row of
// a batch, and the run continues.
//
// IT SWITCHES ON THE RECIPIENT AND NOT ON A FIELD BEING EMPTY. There are exactly
// two kinds of message and the catalog said which this is; sniffing for a
// non-empty HolderEmail would turn a future third case, or a bug that left a
// field unset, into a mail sent to the wrong person rather than into a compile
// error or a loud default.
func (s *Service) sendAnswerReminder(ctx context.Context, due catalog.DueAnswerReminder, result *AnswerReminderSweepResult) {
	sent := false
	switch due.Recipient {
	case catalog.RemindTheHolder:
		sent = s.sendHolderAnswerReminder(ctx, due, result)
	default:
		sent = s.sendBuyerAnswerReminder(ctx, due, result)
	}
	if !sent {
		return
	}

	if err := s.answerReminders.RecordAnswerRemindersSent(ctx, due.TicketIDs); err != nil {
		// The mail is in somebody's inbox and the platform has forgotten it sent
		// it. Counted apart from a failure because it means the opposite thing:
		// this reader may receive one reminder more than the cap intended, and the
		// only way anybody learns that is this line and the Unrecorded tally.
		//
		// THE LOG NAMES A TICKET SALE AND NOT A PERSON, which holds for a Holder's
		// mail too even though the Sale is not a fact that reader is ever told: an
		// operator has to be able to find the row, and the row is what they can
		// find it by.
		result.Unrecorded++
		result.Sent++
		s.logger.Error("an Answer Reminder was sent but could not be recorded; this recipient may receive one more than the cap allows",
			"ticket_sale_id", due.TicketSaleID, "tickets", len(due.TicketIDs), "error", err)
		return
	}

	result.Sent++
}

// sendBuyerAnswerReminder writes to the buyer about the Tickets of their Sale
// that are still theirs to chase — every one that is `unassigned` or `assigned`.
// It reports whether a provider accepted the message.
//
// THIS IS THE MAIL AS IT ALWAYS WAS, pointing at the buyer's own sale page where
// they answer what they know and copy each Ticket's Answer Link out for whoever
// will be using it. What changed under it is which Tickets it is about: an
// accepted one is the Holder's now, and the buyer is not chased about it.
func (s *Service) sendBuyerAnswerReminder(
	ctx context.Context, due catalog.DueAnswerReminder, result *AnswerReminderSweepResult,
) bool {
	// A Sale with no address cannot be written to. It should be unreachable — a
	// Ticket Sale records its buyer's email on every channel — and it is checked
	// because the alternative is handing an empty To to the provider and counting
	// the refusal as a provider fault.
	if due.CustomerEmail == "" {
		result.Skipped++
		s.logger.Warn("a Ticket Sale due an Answer Reminder carries no buyer address",
			"ticket_sale_id", due.TicketSaleID)
		return false
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
		return false
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
		// case ADR 0033 put the Sale Locale first for — and this reader IS the
		// party to that sale, which is what keeps the order this way round while
		// the Holder's is inverted below.
		Locale: s.mailLocale(ctx, due.TicketSaleID, due.SaleLocale, due.CustomerEmail),
	}

	if err := s.email.SendAnswerReminder(ctx, reminder); err != nil {
		// Nothing is recorded, so the buyer is due again on the next tick and has
		// lost none of their allowance to a provider outage.
		result.Failed++
		s.logger.Error("answer reminder send failed",
			"ticket_sale_id", due.TicketSaleID, "error", err)
		return false
	}
	return true
}

// sendHolderAnswerReminder writes to the person who accepted one Ticket, about
// that Ticket. It reports whether a provider accepted the message.
//
// IT NAMES NO BUYER AND NO PURCHASE, and the enforcement is that
// platform.HolderAnswerReminder has no field for either — see that type. What is
// composed here is an Event name, a Ticket Type name and a link, all three of
// which this reader was already shown when they accepted.
//
// THE LINK ARRIVES ALREADY SIGNED, by the catalog module, which is the only
// place on this platform an Assignment Link is composed (ADR 0046). This module
// puts it in a message addressed to the Holder and nowhere else: it is not
// logged, not returned, and not rendered on any surface. An empty one is a
// deployment with no link secret and is skipped by the catalog before it gets
// here, so reaching this function with one would be a bug — checked anyway,
// because the cost of being wrong is a mail telling somebody to open nothing.
func (s *Service) sendHolderAnswerReminder(
	ctx context.Context, due catalog.DueAnswerReminder, result *AnswerReminderSweepResult,
) bool {
	if due.HolderEmail == "" || due.AssignmentLink == "" {
		result.Skipped++
		s.logger.Warn("a Ticket due a Holder's Answer Reminder carries no address or no link; nobody was written to and it stays due",
			"ticket_sale_id", due.TicketSaleID)
		return false
	}

	reminder := platform.HolderAnswerReminder{
		To:             due.HolderEmail,
		EventName:      due.EventName,
		TicketTypeName: due.TicketTypeName,
		AnswerURL:      due.AssignmentLink,
		// THE CHAIN IS READ IN THE OTHER ORDER FOR THIS READER, which is the one
		// thing about this function that differs from its buyer-addressed sibling
		// beyond the disclosure. A Holder is not party to the sale: they did not
		// buy anything, were never on the page that recorded a Sale Locale, and
		// may not share the buyer's language at all — a Spanish-speaking Holder
		// whose friend paid on the English site is exactly the case this feature
		// exists to serve. So their own remembered Mail Locale outranks the sale's,
		// and the sale's is the better-than-nothing fallback: a friend who bought
		// in Spanish is more likely than chance to have Spanish-speaking friends.
		// English is the floor, as always.
		//
		// #325 made this inversion for the Assignment mail
		// (service.assignmentMailLocale) and the reasoning is identical here. It
		// does NOT extend to the buyer's reminder, which is about their own
		// purchase and keeps ADR 0033's ordinary order.
		Locale: s.holderMailLocale(ctx, due.TicketSaleID, due.SaleLocale, due.HolderEmail),
	}

	if err := s.email.SendHolderAnswerReminder(ctx, reminder); err != nil {
		// Nothing is recorded, so the Holder is due again on the next tick and has
		// lost none of their allowance to a provider outage. The link is not
		// logged: it is a credential that mints an identity, and a log aggregator
		// is a wider audience than an inbox.
		result.Failed++
		s.logger.Error("holder answer reminder send failed",
			"ticket_sale_id", due.TicketSaleID, "error", err)
		return false
	}
	return true
}

// holderMailLocale resolves the language of a mail whose reader is NOT the party
// to the sale: the recipient's own remembered Mail Locale first, the Sale Locale
// second, English underneath both.
//
// IT IS mailLocale WITH THE TWO CANDIDATES SWAPPED, and it is a separate
// function rather than a boolean argument to that one because the order is a
// decision about who the reader is, not a mode. A call site passing `true` for
// "recipient first" is a call site where the reason has been lost; a call site
// naming this function is one where somebody chose it.
//
// It is the sales-module twin of catalog service's assignmentMailLocale, which
// made the same inversion for the same reason (#325, ADR 0033, ADR 0046). The
// two exist separately because each module reads a Mail Locale through its own
// seam — this one through the customers service it already holds — and neither
// may reach into the other's.
//
// READING THE RECIPIENT'S RECORD HERE IS NOT AN ORACLE, on assignmentMailLocale's
// terms: nothing about the answer reaches any response body. This is a scheduled
// job with no caller to tell, and what changes with the value is the language of
// a message sent to the address itself.
func (s *Service) holderMailLocale(ctx context.Context, saleID, saleLocale, recipientEmail string) platform.Locale {
	remembered, err := s.customers.MailLocale(ctx, recipientEmail)
	if err != nil {
		// A language is not worth failing a reminder over, and both fallbacks are
		// honest answers. Logged rather than swallowed for the reason mailLocale
		// logs it: a persistent failure here is a feature quietly writing to
		// everybody in English.
		s.logger.Warn("could not read a Holder's remembered language for their Answer Reminder; falling back",
			"ticket_sale_id", saleID, "error", err)
	}
	return platform.ResolveMailLocale(remembered, saleLocale)
}
