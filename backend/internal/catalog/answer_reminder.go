package catalog

import "time"

// This file holds the RATIONING of the Answer Reminder — the rules deciding
// whether one TICKET may be chased right now, and who the chasing is addressed
// to (#317, ADR 0044; #328, parent #322, ADR 0046).
//
// It sits beside outstanding_answer.go and is deliberately NOT part of it. The
// debt and the mail are two different questions:
//
//   - IsOutstandingAnswer answers "does this Ticket owe this question an
//     Answer", which stays true after the doors open, stays true however many
//     times anybody has been chased, and is what the Organization's list and
//     the Sales Export's blank cell are made of.
//
//   - MayRemind answers "may the platform put a message in somebody's inbox
//     about it", which is a rule about MAILING and has a clock, a cap and a
//     cooldown in it.
//
// Pushing these clauses down into the derivation was considered and refused in
// the note at the foot of repository/outstanding_answer_repository.go: an
// Outstanding Answer that vanished from #313's list because the event started,
// or because somebody had already been mailed twice, would be a list that
// disagreed with itself about what "required" means. The debt is the debt. The
// silence is the reminder's.
//
// WHO IS ADDRESSED, STATED ONCE AND AT THE TOP. ADR 0044 addressed this mail to
// the buyer "because there is nobody else to address". ADR 0046 gave a Ticket a
// Holder who accepts by mail, and ADR 0049 finished the thought: only the Holder
// answers, so only the Holder is chased. The reminder goes to the address that
// ACCEPTED the Ticket — the buyer for their own Self-held Ticket (ADR 0048), a
// named Holder for an accepted one — and an `unassigned`, `assigned` or purged
// Ticket has NO recipient at all. Nobody can answer for it, so nobody is
// nagged about it: an unassigned Ticket is an assignment debt, not an Answer
// debt, and an Assignment Reminder is a separate decision ADR 0049 deferred.
// The rationing follows the recipient, from the Sale down to the TICKET. A
// per-Sale allowance could not ration four Holders at all: the first one
// mailed would spend it and the other three would never be written to.
//
// THE CAPS THEMSELVES ARE UNCHANGED. At most one per 7 days, at most two ever,
// silence once the Event has started, nothing at all for a reversed Sale, and
// still swept rather than triggered by an edit. Only the unit moved.
//
// THE SAME RULE LIVES TWICE, IN GO AND IN SQL, exactly as the derivation's does
// and for a weaker reason honestly stated: the sweep has to be able to SKIP the
// Tickets it may not chase inside the database, or a platform whose oldest
// hundred Tickets had all been chased twice would hand the job the same hundred
// unmailable candidates every night and never reach the hundred-and-first. So
// repository.answerReminderRation is these clauses in SQL, this is the statement
// of them, and the service applies THIS one to every candidate the SQL returned
// before a single message is composed. A disagreement between the two is a
// candidate that survives the query and is refused here, which costs a wasted
// row and mails nobody — the safe direction, and the integration tests hold the
// two together anyway.

const (
	// AnswerReminderInterval is the shortest gap between two Answer Reminders
	// about one Ticket.
	//
	// SEVEN DAYS, and the unit that matters is the WEEK rather than the number:
	// somebody who has not chased three friends for their t-shirt sizes in a
	// weekend has not forgotten, they are waiting for a reply. A reminder that
	// arrived a day later would be the platform hurrying a conversation it is not
	// part of, and the second one would be read as a fault in the first. It reads
	// the same way to a Holder, who is on the other end of exactly that
	// conversation.
	//
	// It is measured from the LAST SEND and not from a calendar week, so two
	// Ticket Sales made on different days are chased on different days and no
	// Monday morning ever carries the platform's whole outstanding debt at once.
	AnswerReminderInterval = 7 * 24 * time.Hour

	// MaxAnswerReminders is how many Answer Reminders one TICKET may ever
	// produce, over its whole life.
	//
	// TWO, WHICH IS A PRODUCT DECISION AND NOT A ROUND NUMBER. The first reminder
	// is information: an Organization added a question after the sale, or the
	// buyer skipped the checkout form, and the person holding the Ticket may
	// genuinely not know it is waiting on them. The second is a last word before
	// the doors. A third would be nagging somebody about a t-shirt size they have
	// decided not to give, on a mail they cannot turn off — this is transactional
	// and carries no unsubscribe (ADR 0034 keeps that footer for the one message
	// it belongs on), so the only thing standing between a reader and an
	// unbounded chase is this constant.
	//
	// THE CAP IS PER TICKET SINCE #328, and per Ticket is not per question and
	// not per recipient. An Organization authoring a fifth question buys itself
	// no fresh allowance, and a buyer who reassigns a Ticket buys none either:
	// the row is the Ticket's and it is spent whoever the mail went to.
	//
	// WHAT THIS COSTS, SAID PLAINLY, because moving the unit down made the
	// feature louder and ADR 0046 says so. A sale with four accepted Holders can
	// produce eight mails in all rather than two, to four different people. No
	// ONE person is written to more than twice about any one Ticket, which is
	// the promise this constant makes and the only promise it ever made — and
	// since #335 it holds for a Holder of several Tickets too, whose owed
	// Tickets share ONE envelope per sweep instead of one each, each still
	// burning its own allowance.
	MaxAnswerReminders = 2
)

// AnswerReminderInputs are the facts — and the only facts — that decide whether
// one TICKET may produce an Answer Reminder now.
//
// A struct rather than positional arguments for OutstandingAnswerInputs' reason:
// two of these are booleans and two are timestamps, and a caller that transposed
// either pair would compile and be wrong in a way no test of this function could
// catch.
//
// IT HAS NO FIELD FOR THE RECIPIENT, and that is deliberate rather than an
// omission. Who a Ticket's reminder is addressed to — its Holder, or nobody —
// is decided by its assignment state (AnswerReminderRecipient, migration 080)
// and changes nothing about WHETHER a held Ticket may be chased: the rule has
// a clock, a cap and a cooldown, and a Ticket inherits whatever allowance it
// had already spent under a previous Holder.
//
// WHAT IS DELIBERATELY ABSENT IS MARKETING CONSENT. An Answer Reminder is
// transactional — it is about tickets somebody bought or accepted, on the same
// footing as the Sale Confirmation that carries the same sentence — so it is not
// gated by consent and this rule has no field to gate it with. That matters
// twice as much since #328: a Holder accepted a ticket and opted into nothing,
// and accepting grants no consent of any kind (ADR 0046). Anyone adding a
// consent field here is making the reminder marketing, which would also oblige
// it to carry an unsubscribe footer and to stop being sendable to the majority
// of people who decline (ADR 0034).
type AnswerReminderInputs struct {
	// HasOutstandingAnswers is whether this Ticket still owes a required Ticket
	// Question an Answer — IsOutstandingAnswer, aggregated over the Ticket's
	// questions. It is the reason the mail exists, and the only clause here that
	// is about the debt at all.
	HasOutstandingAnswers bool

	// SaleStatus is the Ticket Sale's status. A REVERSED SALE IS NEVER CHASED:
	// its tickets have ceased to exist and its money has gone back, so there is
	// no shirt to order and nobody to order it for. Mailing anybody about
	// answering questions for tickets that no longer exist is the worst message
	// this feature could send — and worse still to a Holder, who is told by a
	// separate mail that they are no longer holding anything.
	//
	// It is stated here as well as inside the derivation — where `s.status =
	// 'active'` already excludes it — because a reversal can land in the gap
	// between the sweep reading its candidates and the job reaching one of them,
	// and because a rule this consequential should be legible in the function
	// that decides the send rather than only in the query that fed it.
	SaleStatus string

	// EventStartsAt is when the doors open, and the ZERO VALUE MEANS THE EVENT
	// HAS NO SCHEDULE — which refuses the reminder rather than allowing it.
	//
	// SILENCE IS THE SAFE DIRECTION for an Event nobody has placed in time. The
	// whole justification of this mail is "answer before the doors open"; an
	// Event with no start has no doors to be before, both the Answer Link a buyer
	// would be sent to hand out and the Assignment Link a Holder would be sent to
	// answer through expire at a start that does not exist, and a draft with no
	// date is far more likely to be an Organization still setting up than a real
	// event about to happen.
	EventStartsAt time.Time

	// Now is the moment the decision is made, taken from the service's clock and
	// never from a caller — the same rule the Abandoned Answer Purge's cutoff
	// obeys. A caller who could name the moment could name one seven days after
	// the last send and lift the cooldown on demand.
	Now time.Time

	// RemindersSent is how many Answer Reminders this TICKET has already
	// produced, counted from the ledger and never from a counter column. Rows,
	// because "how many" and "when was the last" have to agree, and two columns
	// maintained by hand eventually do not.
	RemindersSent int

	// LastRemindedAt is when the most recent one went, ZERO IF NEVER. Never is
	// not "a very long time ago" by accident of arithmetic — it is a case this
	// rule names, because the first reminder must be sendable on the day a debt
	// appears and a zero timestamp compared against a seven-day window would
	// happen to allow it for the wrong reason.
	LastRemindedAt time.Time
}

// MayRemind reports whether one TICKET may produce an Answer Reminder now.
//
// FIVE CLAUSES, ALL CONJUNCTIVE, and repository.answerReminderRation is the
// same five in the same order. Neither may be changed alone.
//
// IT ANSWERS ABOUT A TICKET AND NEVER ABOUT A TICKET SALE — the meaning of
// RemindersSent moved down a level in #328 and the signature did not change.
// WHO is addressed is a separate question with a separate function,
// AnswerReminderRecipient, and this one has no opinion about it.
func MayRemind(in AnswerReminderInputs) bool {
	return in.HasOutstandingAnswers &&
		in.SaleStatus == TicketSaleStatusActive &&
		!in.EventStartsAt.IsZero() && in.Now.Before(in.EventStartsAt) &&
		in.RemindersSent < MaxAnswerReminders &&
		(in.LastRemindedAt.IsZero() || !in.Now.Before(in.LastRemindedAt.Add(AnswerReminderInterval)))
}

// AnswerReminderRecipient is the address one Ticket's Answer Reminder goes to,
// and "" when the Ticket has nobody to chase.
//
// HOLDER OR NOBODY (ADR 0049). Only an `accepted` Ticket has a recipient, and
// it is the address that accepted it — a named Holder, or the buyer for their
// own Self-held Ticket, which is `accepted` by paying (ADR 0048) and is
// indistinguishable here from any other. An `unassigned` Ticket has nobody
// who can answer for it; an `assigned` one has an address that has agreed to
// nothing (ADR 0046: ignoring the Assignment mail IS the decline); and a
// Ticket the Holder Address Purge has been through reads `unassigned` again.
// None of the three is chased, deferred or counted as due: the buyer's debt on
// such a Ticket is an assignment, not an Answer, and nudging that is a
// different mail ADR 0049 explicitly left unbuilt.
//
// IT GOES THROUGH AssignmentState AND NEVER TESTS accepted_at ITSELF, which is
// the rule migration 080 states and every other reader of the three columns
// obeys. The pointers are that function's own signature: nil for a column that
// is NULL.
//
// THE SAME CLAUSE LIVES IN SQL, as repository.answerReminderRation's
// `tk.accepted_at IS NOT NULL`, so the sweep can skip what it may not chase
// inside the database; the repository applies THIS one to every row anyway, and
// a disagreement resolves as silence.
func AnswerReminderRecipient(holderEmail string, assignedAt, acceptedAt *time.Time) string {
	if AssignmentState(holderEmail, assignedAt, acceptedAt) != TicketAccepted {
		return ""
	}
	return holderEmail
}

// AnswerReminderCandidate is one held TICKET the sweep found: the row MayRemind
// decides about, and the unit the ledger is written in.
//
// IT IS NOT A MAIL. Several of these become one DueAnswerReminder when they
// share a Holder (#335). Keeping the two types apart is what stops the
// rationing from being reasoned about in terms of messages: the caps count
// Tickets chased, and a mail covering four Tickets spends four allowances.
//
// IT CARRIES NO BUYER FACT. The buyer's name, address and Sale Confirmation
// reference were selected while the buyer was a recipient; since ADR 0049 no
// reminder is about a purchase, so there is nothing here for a template to
// print by mistake.
type AnswerReminderCandidate struct {
	// TicketID is what the ledger row is written against.
	TicketID string

	// HolderEmail is the address that accepted this Ticket — the recipient, and
	// since #335 the GROUPING KEY: every owed Ticket one address holds in one
	// sweep's batch becomes one mail. Never empty on a candidate: a Ticket with
	// no recipient is not one.
	HolderEmail string
	// TicketTypeName is what the Holder recognises their ticket by, one of the
	// two public facts their surfaces are allowed to show (ADR 0046).
	TicketTypeName string

	// TicketSaleID is the Sale this Ticket belongs to, carried for the LOG LINE
	// and the locale fallback only: it never reaches the message.
	TicketSaleID string
	// SaleStatus and EventStartsAt are the facts MayRemind is applied to. They
	// travel with the candidate rather than being re-read, so the decision the
	// service makes is about the same row the query returned.
	SaleStatus    string
	EventStartsAt time.Time
	// EventName is the other public fact the mail prints.
	EventName string
	// SaleLocale is the Sale Locale AS STORED — empty for a sale no page produced
	// — read LAST for this reader: a Holder did not buy anything and was never
	// on the page that recorded it, so their own remembered Mail Locale comes
	// first and this is the better-than-nothing fallback (#325's inversion, ADR
	// 0033). It is no different for the buyer's own Self-held Ticket, whose
	// record remembers the same language the sale did in every ordinary case.
	SaleLocale string

	// RemindersSent and LastRemindedAt are THIS TICKET'S ledger, as the sweep read
	// it. See AnswerReminderInputs for why both travel.
	RemindersSent  int
	LastRemindedAt time.Time
}

// Inputs is the candidate as MayRemind reads it, at one moment.
//
// HasOutstandingAnswers IS TRUE BY CONSTRUCTION: the query that produced this
// candidate is the Outstanding Answer derivation with the ration appended, so a
// row that came back owes something. It is set explicitly rather than left to
// the zero value because MayRemind must refuse a zero-valued input, and a
// candidate that silently satisfied the first clause would be a candidate that
// could not be told from one that had been checked.
func (c AnswerReminderCandidate) Inputs(now time.Time) AnswerReminderInputs {
	return AnswerReminderInputs{
		HasOutstandingAnswers: true,
		SaleStatus:            c.SaleStatus,
		EventStartsAt:         c.EventStartsAt,
		Now:                   now,
		RemindersSent:         c.RemindersSent,
		LastRemindedAt:        c.LastRemindedAt,
	}
}

// DueAnswerReminder is ONE MAIL the sweep will send: one Holder, every owed
// Ticket they hold that this batch reached.
//
// ONE PER HOLDER PER SWEEP (#335). Per-Ticket rationing is unchanged: each
// listed Ticket burns its own allowance; only the envelope is shared.
type DueAnswerReminder struct {
	// HolderEmail is where the mail goes, and the key the candidates were
	// grouped under.
	HolderEmail string
	// TicketIDs are the Tickets THIS MAIL COVERS, and the ledger rows it spends
	// once a provider has accepted it — in the order the query returned them.
	//
	// THE LEDGER IS WRITTEN FROM THIS AND NEVER FROM THE SALE. A mail covering
	// three Tickets writes three rows, so the fourth — answered, already chased
	// twice, or added later — keeps the allowance nobody spent on its behalf.
	TicketIDs []string
	// Tickets are what the message prints, one entry per TicketIDs element.
	Tickets []HolderReminderTicket

	// TicketSaleID is the FIRST listed Ticket's Sale, carried for the log line
	// and the locale fallback only; a mail grouped per Holder may span Sales and
	// no Sale fact ever reaches the message. SaleStatus, EventStartsAt and
	// EventName are the same Ticket's, kept so the send site can log or
	// re-reason about the row the query returned.
	TicketSaleID  string
	SaleStatus    string
	EventStartsAt time.Time
	EventName     string
	// SaleLocale is read LAST for this reader; see
	// AnswerReminderCandidate.SaleLocale.
	SaleLocale string
}

// HolderReminderTicket is one owed Ticket as the Answer Reminder lists it: the
// two public facts the reader recognises it by, and nothing else.
//
// EventName travels PER TICKET, not once per mail, because the envelope is per
// Holder per sweep (#335) and nothing guarantees every Ticket one address holds
// belongs to one Event — the mail must be able to name each honestly.
//
// IT CARRIES NO LINK. Since ADR 0049 the reader answers from their own Customer
// Area, behind a sign-in to the address this mail is sent to, and the sales
// module composes that one address for the whole message. A credential per
// Ticket — the Assignment Link this mail used to carry — is no longer minted
// for a reminder, which is one fewer place a credential travels.
type HolderReminderTicket struct {
	EventName      string
	TicketTypeName string
}
