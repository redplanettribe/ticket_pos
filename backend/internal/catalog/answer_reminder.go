package catalog

import "time"

// This file holds the RATIONING of the Answer Reminder — the rules deciding
// whether one Ticket Sale's buyer may be written to right now (#317, ADR 0044).
//
// It sits beside outstanding_answer.go and is deliberately NOT part of it. The
// debt and the mail are two different questions:
//
//   - IsOutstandingAnswer answers "does this Ticket owe this question an
//     Answer", which stays true after the doors open, stays true however many
//     times the buyer has been chased, and is what the Organization's list and
//     the Sales Export's blank cell are made of.
//
//   - MayRemind answers "may the platform put a message in this buyer's inbox
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
// THE SAME RULE LIVES TWICE, IN GO AND IN SQL, exactly as the derivation's does
// and for a weaker reason honestly stated: the sweep has to be able to SKIP the
// sales it may not mail inside the database, or a platform whose oldest hundred
// sales have all been reminded twice would hand the job the same hundred
// unmailable candidates every night and never reach the hundred-and-first. So
// repository.answerReminderRation is these clauses in SQL, this is the statement
// of them, and the service applies THIS one to every candidate the SQL returned
// before a single message is composed. A disagreement between the two is a
// candidate that survives the query and is refused here, which costs a wasted
// row and mails nobody — the safe direction, and the integration tests hold the
// two together anyway.

const (
	// AnswerReminderInterval is the shortest gap between two Answer Reminders
	// about one Ticket Sale.
	//
	// SEVEN DAYS, and the unit that matters is the WEEK rather than the number:
	// somebody who has not chased three friends for their t-shirt sizes in a
	// weekend has not forgotten, they are waiting for a reply. A reminder that
	// arrived a day later would be the platform hurrying a conversation it is not
	// part of, and the second one would be read as a fault in the first.
	//
	// It is measured from the LAST SEND and not from a calendar week, so two
	// Ticket Sales made on different days are chased on different days and no
	// Monday morning ever carries the platform's whole outstanding debt at once.
	AnswerReminderInterval = 7 * 24 * time.Hour

	// MaxAnswerReminders is how many Answer Reminders one Ticket Sale may ever
	// receive, over its whole life.
	//
	// TWO, WHICH IS A PRODUCT DECISION AND NOT A ROUND NUMBER. The first reminder
	// is information: an Organization added a question after the sale, or the
	// buyer skipped the checkout form, and they may genuinely not know their
	// tickets are waiting on them. The second is a nudge before the doors. A
	// third would be nagging somebody about a t-shirt size they have decided not
	// to give, on a mail they cannot turn off — this is transactional and carries
	// no unsubscribe (ADR 0034 keeps that footer for the one message it belongs
	// on), so the only thing standing between a buyer and an unbounded chase is
	// this constant.
	//
	// The cap is per TICKET SALE and not per Ticket or per question. A buyer with
	// four unanswered Tickets is one person with one inbox, and an Organization
	// authoring a fifth question does not buy itself a fresh allowance to write
	// to them.
	MaxAnswerReminders = 2
)

// AnswerReminderInputs are the facts — and the only facts — that decide whether
// one Ticket Sale's buyer may be sent an Answer Reminder now.
//
// A struct rather than positional arguments for OutstandingAnswerInputs' reason:
// two of these are booleans and two are timestamps, and a caller that transposed
// either pair would compile and be wrong in a way no test of this function could
// catch.
//
// WHAT IS DELIBERATELY ABSENT IS MARKETING CONSENT. An Answer Reminder is
// transactional — it is about tickets somebody bought, on the same footing as
// the Sale Confirmation that carries the same sentence — so it is not gated by
// consent and this rule has no field to gate it with. Anyone adding one is
// making the reminder marketing, which would also oblige it to carry an
// unsubscribe footer and to stop being sendable to the majority of buyers who
// decline (ADR 0034).
type AnswerReminderInputs struct {
	// HasOutstandingAnswers is whether ANY Ticket on this Sale still owes a
	// required Ticket Question an Answer — IsOutstandingAnswer, aggregated over
	// the Sale. It is the reason the mail exists, and the only clause here that
	// is about the debt at all.
	HasOutstandingAnswers bool

	// SaleStatus is the Ticket Sale's status. A REVERSED SALE IS NEVER CHASED:
	// its tickets have ceased to exist and its money has gone back, so there is
	// no shirt to order and nobody to order it for. Mailing a buyer about
	// answering questions for tickets they no longer hold is the worst message
	// this feature could send.
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
	// whole justification of this mail is "answer before the doors open, and pass
	// the links on to whoever is coming"; an Event with no start has no doors to
	// be before, the Answer Links a buyer would be sent to distribute expire at a
	// start that does not exist, and a draft with no date is far more likely to
	// be an Organization still setting up than a real event about to happen.
	EventStartsAt time.Time

	// Now is the moment the decision is made, taken from the service's clock and
	// never from a caller — the same rule the Abandoned Answer Purge's cutoff
	// obeys. A caller who could name the moment could name one seven days after
	// the last send and lift the cooldown on demand.
	Now time.Time

	// RemindersSent is how many Answer Reminders this Ticket Sale has already
	// received, counted from the ledger and never from a counter column. Rows,
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

// MayRemind reports whether one Ticket Sale's buyer may be sent an Answer
// Reminder now.
//
// FIVE CLAUSES, ALL CONJUNCTIVE, and repository.answerReminderRation is the
// same five in the same order. Neither may be changed alone.
//
// It answers about a TICKET SALE and never about a Ticket: the mail is addressed
// to the buyer, who is a property of the Sale, and there is nobody else on this
// platform to address (ADR 0044). The platform holds no address for a holder and
// must never collect one — so a "remind the holder" variant of this function is
// not a feature that is missing, it is the decision the whole feature is built
// around.
func MayRemind(in AnswerReminderInputs) bool {
	return in.HasOutstandingAnswers &&
		in.SaleStatus == TicketSaleStatusActive &&
		!in.EventStartsAt.IsZero() && in.Now.Before(in.EventStartsAt) &&
		in.RemindersSent < MaxAnswerReminders &&
		(in.LastRemindedAt.IsZero() || !in.Now.Before(in.LastRemindedAt.Add(AnswerReminderInterval)))
}

// DueAnswerReminder is one Ticket Sale the sweep found, with everything the mail
// needs and nothing it does not.
//
// IT LIVES IN THE CATALOG ROOT PACKAGE so that the sales module — which composes
// and sends the mail, because a Ticket Sale's mail is that module's and the
// Confirmation Link and Mail Locale it needs are already there — can name the
// type without importing catalog's service. Sales already imports this package
// for shared pricing types, so this closes no cycle.
//
// WHAT IT CARRIES ABOUT THE BUYER is what a receipt already carries: a name, an
// address and the language to write in. What it carries about the debt is a
// BOOLEAN, for the reason the Sale Confirmation's line names no figure — the
// debt is derived live and a count is wrong the moment somebody answers one.
type DueAnswerReminder struct {
	TicketSaleID string
	// SaleStatus and EventStartsAt are the facts MayRemind is applied to. They
	// travel with the candidate rather than being re-read, so the decision the
	// service makes is about the same row the query returned.
	SaleStatus    string
	EventStartsAt time.Time
	// EventEnd is when the Event finishes, or its start when it has no end. It is
	// here for one purpose: the Confirmation Link this mail points at is signed
	// with an expiry derived from it, exactly as the Sale Confirmation's is, so
	// the two links in a buyer's inbox live the same length of time.
	EventEnd  time.Time
	EventName string
	// SaleLocale is the Sale Locale AS STORED — empty for a sale no page produced
	// — and is handed to platform.ResolveMailLocale raw. This mail is about a
	// sale, so the language the buyer bought in outranks whatever their record
	// remembers, however long afterwards it is sent (ADR 0033).
	SaleLocale        string
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	// RemindersSent and LastRemindedAt are this Sale's ledger, as the sweep read
	// it. See AnswerReminderInputs for why both travel.
	RemindersSent  int
	LastRemindedAt time.Time
}

// Inputs turns a candidate into the facts MayRemind decides on, so that no
// caller assembles them by hand and no caller can quietly leave one out.
//
// HasOutstandingAnswers is TRUE by construction: the sweep's WHERE is the
// Outstanding Answer derivation itself, so a row that came back is a Sale with a
// debt. It is passed explicitly rather than defaulted, because the field exists
// to make the clause readable in one place and a struct literal that omitted it
// would silently say the opposite.
func (d DueAnswerReminder) Inputs(now time.Time) AnswerReminderInputs {
	return AnswerReminderInputs{
		HasOutstandingAnswers: true,
		SaleStatus:            d.SaleStatus,
		EventStartsAt:         d.EventStartsAt,
		Now:                   now,
		RemindersSent:         d.RemindersSent,
		LastRemindedAt:        d.LastRemindedAt,
	}
}
