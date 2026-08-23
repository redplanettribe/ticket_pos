package catalog

import "time"

// This file holds the RATIONING of the Assignment Reminder — the rules deciding
// whether the buyer of one TICKET SALE may be written to right now about the
// Tickets on it that still have nobody (#362, parent #361, ADR 0051).
//
// It is the counterpart of answer_reminder.go and deliberately NOT a second
// case inside it. ADR 0049 ruled that the Answer Reminder chases only the
// Holder, about an Answer, and left "an Assignment Reminder" as a separate
// decision; ADR 0051 took it. The two mails ration different recipients about
// different debts: the Answer Reminder's unit is the TICKET and its reader is
// whoever holds it; this one's unit is the SALE and its reader is the buyer,
// because an unassigned Ticket has no Holder and the only person who can give
// it one is the person who bought it. A four-Ticket Sale with three Tickets
// unassigned is ONE person with ONE inbox and produces ONE mail listing a
// count — which is exactly the reasoning migration 079 gave for the Answer
// Reminder before ADR 0046 moved that one's unit down.
//
// THE DEBT AND THE MAIL ARE STILL TWO QUESTIONS. Whether a Ticket is
// `unassigned` is AssignmentState's answer and stays true however many times
// the buyer has been written to; whether the platform may say so in somebody's
// inbox is this file's, and it has a clock, a cap and a cooldown in it.
//
// THE SAME RULE LIVES TWICE, IN GO AND IN SQL, for the reason answer_reminder.go
// states: the sweep must be able to SKIP inside the database what it may not
// send, or a backlog whose oldest hundred Sales had all been reminded twice
// would hand the job the same hundred unmailable candidates every tick and
// never reach the hundred-and-first. repository.assignmentReminderRation is
// these clauses in SQL, this is the statement of them, and the service applies
// THIS one to every candidate the SQL returned before a message is composed. A
// disagreement resolves as silence, which costs a wasted row and mails nobody.

const (
	// AssignmentReminderMinSaleAge is how old a Ticket Sale must be before its
	// buyer is reminded at all.
	//
	// TWENTY-FOUR HOURS, because the receipt already said it. A buyer who
	// checked out an hour ago has a Sale Confirmation in the same inbox with a
	// Confirmation Link to the same page, and a reminder that arrived before
	// they had slept on it would be the receipt again with a sterner subject.
	// A day is long enough for the first mail to have been read and the page
	// to have been visited or not.
	//
	// It is measured from the Sale's CREATION and not from the Event, so a
	// buyer for an Event next week and one for an Event next year are treated
	// alike: the first reminder is about the choice existing, not about the
	// doors approaching.
	AssignmentReminderMinSaleAge = 24 * time.Hour

	// AssignmentReminderInterval is the shortest gap between two Assignment
	// Reminders about one Ticket Sale: seven days, for AnswerReminderInterval's
	// reason — the unit that matters is the WEEK. Somebody who has not decided
	// who three Tickets are for over a weekend has not forgotten; they are
	// asking their friends. Measured from the LAST SEND, never from a calendar
	// week, so no Monday carries the platform's whole backlog at once.
	AssignmentReminderInterval = 7 * 24 * time.Hour

	// MaxAssignmentReminders is how many Assignment Reminders one TICKET SALE
	// may ever produce, over its whole life.
	//
	// TWO, FOR MaxAnswerReminders' REASON AND ONE MORE. The first is
	// information — for the pre-feature buyer (ADR 0051's catch-up) it is the
	// only time they are told the choice exists at all; for everyone else it is
	// the day-after nudge. The second is a last word before the doors. A third
	// would be nagging somebody about Tickets they have decided to keep in
	// their own hand, on a transactional mail with no unsubscribe, so this
	// constant is the only thing between a reader and an unbounded chase.
	//
	// THE CAP IS PER SALE. Assigning one of three Tickets buys no fresh
	// allowance, and neither does a reassignment: the rows are the Sale's and
	// are spent however the Tickets on it have moved since.
	MaxAssignmentReminders = 2
)

// AssignmentReminderInputs are the facts — and the only facts — that decide
// whether one TICKET SALE may produce an Assignment Reminder now.
//
// A struct rather than positional arguments for AnswerReminderInputs' reason:
// three timestamps and three integers, and a caller that transposed any pair
// would compile and be wrong in a way no test of this function could catch.
//
// WHAT IS DELIBERATELY ABSENT IS MARKETING CONSENT, on the Answer Reminder's
// terms exactly: this is transactional mail about the reader's own purchase,
// on the same footing as the Sale Confirmation that carried the same link, so
// it is not gated by consent and there is no field to gate it with. Anyone
// adding one here is making the reminder marketing, with an unsubscribe
// footer and a majority of readers it could no longer reach (ADR 0034).
//
// ALSO ABSENT IS THE FEATURE FLAG. TICKET_ASSIGNMENT_ENABLED is read by the
// catalog service before the query runs — a closed flag is "nobody is a
// candidate", not "every candidate is refused" — so this rule never sees it.
type AssignmentReminderInputs struct {
	// SaleChannel is the Sales Channel the Ticket Sale was recorded on. AN
	// ONLINE SALE AND AN IMPORTED SALE ARE REMINDED; AN `in_person` SALE IS
	// NOT.
	//
	// ADR 0055 widened this past `online` (#395). The exclusion 0051 inherited
	// said an import Sale's buyer is a name somebody else typed who never used
	// the platform — but a Sale Import transcribes a transaction that already
	// happened, its buyer now holds Ticket 1 of it, and on a multi-Ticket Sale
	// nobody is named for the rest. That is exactly this mail's debt. It is a
	// deliberate exception to ADR 0050's "an imported buyer is mailed nothing
	// by default", which is about transactional mail they did not ask about;
	// asking somebody holding Tickets to name who is coming is the one thing
	// only they can do.
	//
	// `in_person` stays out because a door sale has no buyer surface: Ticket
	// Assignment refuses the channel, so a reminder would be an instruction its
	// reader cannot follow. Refused here as well as in the query, for
	// SaleStatus's reason below.
	SaleChannel string

	// SaleStatus is the Ticket Sale's status. A REVERSED SALE IS NEVER
	// REMINDED: its Tickets have ceased to exist and a mail pointing at them
	// would point at nothing. Stated here as well as in the query because a
	// reversal can land between the sweep reading its candidates and the job
	// reaching one of them, and because a rule this consequential should be
	// legible where the send is decided.
	SaleStatus string

	// SaleCreatedAt is when the Sale was recorded — ticket_sales.created_at,
	// the moment the buyer's receipt went, never sold_at, which an import can
	// backdate. The ZERO VALUE REFUSES: a Sale with no creation time is a row
	// this rule does not understand, not one that is old enough.
	SaleCreatedAt time.Time

	// TicketCount is how many Tickets the Sale has, across its lines. A
	// SINGLE-TICKET SALE IS NEVER REMINDED: that Ticket is the buyer's own
	// Self-held Ticket (ADR 0048) and there is nobody else to name.
	TicketCount int
	// UnassignedTickets is how many of them have no holder address — the
	// `unassigned` state, which a purged Ticket reads as again and a
	// reassigned Self-held Ticket does not. It is the reason the mail exists,
	// and zero is the buyer having acted.
	UnassignedTickets int

	// EventStartsAt is when the doors open, and the ZERO VALUE MEANS THE EVENT
	// HAS NO SCHEDULE — which refuses the reminder, for AnswerReminderInputs'
	// reason: silence is the safe direction for an Event nobody has placed in
	// time, and an undated Event is far more likely a draft than a show.
	EventStartsAt time.Time

	// Now is the moment the decision is made, taken from the service's clock
	// and never from a caller: a caller who could name the moment could name
	// one seven days after the last send and lift the cooldown on demand.
	Now time.Time

	// RemindersSent is how many Assignment Reminders this SALE has already
	// produced, counted from the ledger and never from a counter column.
	RemindersSent int
	// LastRemindedAt is when the most recent one went, ZERO IF NEVER — a case
	// this rule names rather than one arithmetic happens to allow.
	LastRemindedAt time.Time
}

// MayRemindAssignment reports whether one TICKET SALE may produce an
// Assignment Reminder now.
//
// EIGHT CLAUSES, ALL CONJUNCTIVE, and repository.assignmentReminderRation is
// the same clauses in the same order. Neither may be changed alone.
//
// IT ANSWERS ABOUT A SALE AND NEVER ABOUT A TICKET. Who is addressed is not a
// question here at all: the buyer is the only reader this mail has, and their
// address travels on the candidate.
func MayRemindAssignment(in AssignmentReminderInputs) bool {
	return remindableAssignmentChannel(in.SaleChannel) &&
		in.SaleStatus == TicketSaleStatusActive &&
		!in.SaleCreatedAt.IsZero() && !in.Now.Before(in.SaleCreatedAt.Add(AssignmentReminderMinSaleAge)) &&
		in.TicketCount > 1 &&
		in.UnassignedTickets > 0 &&
		!in.EventStartsAt.IsZero() && in.Now.Before(in.EventStartsAt) &&
		in.RemindersSent < MaxAssignmentReminders &&
		(in.LastRemindedAt.IsZero() || !in.Now.Before(in.LastRemindedAt.Add(AssignmentReminderInterval)))
}

// remindableAssignmentChannel is the channel clause of MayRemindAssignment,
// named so the SQL beside it (repository.assignmentReminderRation's
// `s.channel IN ('online', 'import')`) has something to be the same as.
func remindableAssignmentChannel(channel string) bool {
	return channel == SalesChannelOnline || channel == SalesChannelImport
}

// AssignmentReminderCandidate is one TICKET SALE the sweep found: the row
// MayRemindAssignment decides about, the unit the ledger is written in, and —
// unlike an AnswerReminderCandidate — also the mail, since one Sale is one
// buyer and nothing is grouped.
//
// IT CARRIES THE BUYER'S FACTS BECAUSE THE BUYER IS THE READER: their address
// and first name, which their own receipt already printed. It carries NO
// price, NO Tax ID and NO Sale Confirmation reference, because the message
// prints none of them (ADR 0044's disclosure rule, kept by ADR 0051 for a
// message that may be forwarded) and a field that is not here cannot be
// printed by mistake.
type AssignmentReminderCandidate struct {
	// TicketSaleID is what the ledger row is written against, and what the
	// Confirmation Link is minted for.
	TicketSaleID string
	// BuyerEmail is the Sale's customer_email — the recipient.
	BuyerEmail string
	// BuyerFirstName is what the greeting uses, as the receipt did.
	BuyerFirstName string

	// SaleChannel, SaleStatus and SaleCreatedAt are the facts
	// MayRemindAssignment is applied to. They travel with the candidate rather
	// than being re-read, so the decision is about the row the query returned.
	SaleChannel   string
	SaleStatus    string
	SaleCreatedAt time.Time
	// TicketCount and UnassignedTickets are the tally the mail prints — "{n} of
	// {total}" — and two of the clauses. They are the Customer Area's own
	// tally, so mail and page agree.
	TicketCount       int
	UnassignedTickets int

	// EventStartsAt, EventEndsAt, EventTimezone and EventName are the Event's
	// facts: the first is a clause, the second bounds the Confirmation Link's
	// life, the third and fourth are what the mail prints.
	EventStartsAt time.Time
	EventEndsAt   time.Time
	EventTimezone string
	EventName     string

	// SaleLocale is the Sale Locale AS STORED — empty for a sale no page
	// produced — read FIRST for this reader, who bought on the page that
	// recorded it (ADR 0033): this mail is about their own purchase, exactly
	// as the Sale Confirmation was.
	SaleLocale string

	// RemindersSent and LastRemindedAt are THIS SALE'S ledger, as the sweep
	// read it.
	RemindersSent  int
	LastRemindedAt time.Time
}

// Inputs is the candidate as MayRemindAssignment reads it, at one moment.
func (c AssignmentReminderCandidate) Inputs(now time.Time) AssignmentReminderInputs {
	return AssignmentReminderInputs{
		SaleChannel:       c.SaleChannel,
		SaleStatus:        c.SaleStatus,
		SaleCreatedAt:     c.SaleCreatedAt,
		TicketCount:       c.TicketCount,
		UnassignedTickets: c.UnassignedTickets,
		EventStartsAt:     c.EventStartsAt,
		Now:               now,
		RemindersSent:     c.RemindersSent,
		LastRemindedAt:    c.LastRemindedAt,
	}
}
