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
// WHAT #328 CHANGED, STATED ONCE AND AT THE TOP. ADR 0044 addressed this mail to
// the buyer "because there is nobody else to address", and rationed it per
// Ticket Sale on the strength of that: one buyer, one inbox. ADR 0046 gave a
// Ticket a Holder who accepts by mail, and both halves of that sentence stopped
// being true. The reminder now follows the ANSWER — to the Holder for a Ticket
// that is `accepted`, to the buyer for every other Ticket on the Sale — and the
// rationing follows the recipient, from the Sale down to the TICKET. A per-Sale
// allowance could not ration four Holders at all: the first one mailed would
// spend it and the other three would never be written to.
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
	// feature louder and ADR 0046 says so. A four-Ticket sale with nothing
	// accepted still produces the two mails it always did — the sweep groups
	// every buyer-addressed Ticket of one Sale into ONE message and spends all
	// four allowances on it — but a sale with four accepted Holders can now
	// produce eight mails in all rather than two, to five different people. No
	// ONE person is written to more than twice, which is the promise this
	// constant makes and the only promise it ever made — and since #335 it holds
	// for a Holder of several Tickets too, whose owed Tickets share ONE envelope
	// per sweep instead of one each, each still burning its own allowance.
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
// omission. Who a Ticket's reminder is addressed to is decided by its assignment
// state (AssignmentState, migration 080) and changes nothing about WHETHER one
// may be sent: a Holder's ticket falls silent when the Event starts on the same
// terms a buyer's does, and inherits whatever allowance the Ticket had already
// spent under its previous recipient. A rule that could say yes to one reader
// and no to another about the same Ticket would be two rules.
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
// IT ANSWERS ABOUT A TICKET AND NEVER ABOUT A TICKET SALE, which is the whole of
// what #328 changed here — the signature is identical and the meaning of
// RemindersSent moved down a level. ADR 0044's note that "a remind the holder
// variant of this function is not a feature that is missing, it is the decision
// the whole feature is built around" is retired by ADR 0046, and there is still
// no such variant: WHO is addressed is read off the Ticket's assignment state by
// the caller, and this function is the same rule for both.
func MayRemind(in AnswerReminderInputs) bool {
	return in.HasOutstandingAnswers &&
		in.SaleStatus == TicketSaleStatusActive &&
		!in.EventStartsAt.IsZero() && in.Now.Before(in.EventStartsAt) &&
		in.RemindersSent < MaxAnswerReminders &&
		(in.LastRemindedAt.IsZero() || !in.Now.Before(in.LastRemindedAt.Add(AnswerReminderInterval)))
}

// AnswerReminderRecipient is who one Ticket's reminder is addressed to.
//
// TWO VALUES AND NOT THREE, though a Ticket has three assignment states. An
// `assigned` Ticket — an address typed and never accepted — is the buyer's to
// chase exactly as an `unassigned` one is, because nobody at that address has
// agreed to hear from this platform about anything. ADR 0046: ignoring the
// Assignment mail IS the decline, and a platform that started chasing a
// non-answer would be reading silence as consent.
//
// A STRING RATHER THAN A BOOL, beside TicketAssignmentState and for a weaker
// version of its reason: nothing puts this on the wire, but a struct field
// reading `Recipient: RemindTheHolder` is legible at the send site in a way
// `ToHolder: true` is not, and the send site is where a mistake reaches an inbox.
type AnswerReminderRecipient string

const (
	// RemindTheBuyer: the Ticket is `unassigned` or `assigned`, so the person who
	// bought it is the person who can act. Exactly today's behaviour, and still
	// the answer for most Tickets on this platform.
	//
	// IT IS ALSO WHAT A PURGED TICKET GETS. The Holder Address Purge (#331,
	// migration 081) takes an address nobody accepted when the Event starts, and
	// a purged Ticket reads `unassigned` through AssignmentState — so it falls
	// back here, which is the truth: nobody holds it. In practice such a Ticket
	// is past its Event's start and MayRemind has already silenced it.
	RemindTheBuyer AnswerReminderRecipient = "buyer"
	// RemindTheHolder: the Ticket is `accepted`, so the person who will use it
	// proved their address, is a Verified Customer, and answers their own Ticket
	// Questions through their own Assignment Link (#326).
	//
	// This is the case ADR 0044 said did not exist and ADR 0046 created. It is
	// the whole point of #328: chasing the buyer here nags the one person the
	// feature has just established does not know their friend's t-shirt size.
	RemindTheHolder AnswerReminderRecipient = "holder"
)

// AnswerReminderCandidate is one TICKET the sweep found: the row MayRemind
// decides about, and the unit the ledger is written in.
//
// IT IS NOT A MAIL. Several of these become one DueAnswerReminder when they
// share a buyer, and several become one when they share a Holder (#335).
// Keeping the two types apart is what stops the rationing from being reasoned
// about in terms of messages: the caps count Tickets chased, and a mail
// covering four Tickets spends four allowances.
//
// EVERY FIELD ABOUT THE SALE IS REPEATED ON EVERY TICKET OF IT, which is a
// denormalisation the query produces for free and the grouping then collapses.
// The alternative — a Sale struct holding a slice of Tickets — would need the
// repository to build a tree out of a flat result set, and every consumer to
// walk it to ask the one question that matters, which is whether this Ticket may
// be chased.
type AnswerReminderCandidate struct {
	// TicketID is what the ledger row is written against, and the only field
	// here that is not either a fact about the Sale or a fact about the reader.
	TicketID string

	// Recipient is who this Ticket's mail is addressed to, derived by the
	// repository from AssignmentState and never stored. See
	// AnswerReminderRecipient.
	Recipient AnswerReminderRecipient

	// TicketSaleID is the Sale this Ticket belongs to. It is the GROUPING KEY for
	// buyer-addressed Tickets — that is its whole job — and it is also what a
	// failure log names so an operator can find the row.
	TicketSaleID string
	// SaleStatus and EventStartsAt are the facts MayRemind is applied to. They
	// travel with the candidate rather than being re-read, so the decision the
	// service makes is about the same row the query returned.
	SaleStatus    string
	EventStartsAt time.Time
	// EventEnd is when the Event finishes, or its start when it has no end. It is
	// here for one purpose: the Confirmation Link the BUYER'S mail points at is
	// signed with an expiry derived from it, exactly as the Sale Confirmation's
	// is, so the two links in a buyer's inbox live the same length of time. The
	// Holder's Assignment Link bakes in no expiry at all and reads the Event
	// live, so it does not use this.
	EventEnd  time.Time
	EventName string
	// SaleLocale is the Sale Locale AS STORED — empty for a sale no page produced
	// — and is handed to platform.ResolveMailLocale raw.
	//
	// THE TWO MAILS READ IT IN OPPOSITE ORDERS, which is the one thing about this
	// field worth knowing. The buyer's reminder is about their own sale, so the
	// language they bought in outranks whatever their record remembers (ADR
	// 0033). The Holder's is not: they did not buy anything and were never on
	// that page, so their own remembered Mail Locale comes first and this is only
	// the better-than-nothing fallback — the same inversion #325 made for the
	// Assignment mail, for the same reason and no other.
	SaleLocale string

	// ConfirmationRef, CustomerEmail, CustomerFirstName and CustomerLastName are
	// the BUYER, and are what a receipt already carries: a reference, a name, an
	// address.
	//
	// THEY ARE SELECTED FOR EVERY CANDIDATE AND USED ONLY BY BUYER-ADDRESSED
	// ONES. That is not sloppiness — the grouping needs the buyer's address to
	// key on and the query would fetch it anyway — but nothing about the buyer
	// may reach a Holder's mail, and the way that is enforced is that
	// platform.HolderAnswerReminder HAS NO FIELD FOR ANY OF IT. A fact with
	// nowhere to go cannot be leaked by somebody adding a line to a template.
	ConfirmationRef   string
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string

	// HolderEmail is the address that accepted this Ticket, empty unless
	// Recipient is RemindTheHolder.
	//
	// IT IS `tickets.holder_email` AND NOT THE CUSTOMER'S CURRENT ADDRESS. The
	// two are the same address today — accepting mints or matches a Customer on
	// this normalised value — and this is the one the Assignment Link's
	// fingerprint was signed against, so a link composed for any other address
	// would not open.
	HolderEmail string
	// TicketTypeName is what the Holder recognises their ticket by, and is one of
	// the two public facts their surfaces are allowed to show (ADR 0046). Empty
	// on a buyer-addressed candidate, whose mail is about a whole purchase rather
	// than about one Ticket.
	TicketTypeName string
	// AssignedAt is when this Ticket's current address was named. It exists for
	// exactly one purpose: signing the Assignment Link, whose token names the
	// Ticket and the instant, so that a reassignment kills every link minted
	// before it. Nothing else reads it and nothing renders it.
	AssignedAt time.Time

	// RemindersSent and LastRemindedAt are THIS TICKET'S ledger, as the sweep read
	// it. See AnswerReminderInputs for why both travel.
	RemindersSent  int
	LastRemindedAt time.Time
}

// Inputs turns a candidate into the facts MayRemind decides on, so that no
// caller assembles them by hand and no caller can quietly leave one out.
//
// HasOutstandingAnswers is TRUE by construction: the sweep's WHERE is the
// Outstanding Answer derivation itself, so a row that came back is a Ticket with
// a debt. It is passed explicitly rather than defaulted, because the field
// exists to make the clause readable in one place and a struct literal that
// omitted it would silently say the opposite.
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

// DueAnswerReminder is ONE MAIL the sweep is to send, with everything the
// message needs and nothing it does not.
//
// IT LIVES IN THE CATALOG ROOT PACKAGE so that the sales module — which composes
// and sends both mails, because that is where the Confirmation Link, the Mail
// Locale chain and the transactional sender already live — can name the type
// without importing catalog's service. Sales already imports this package for
// shared pricing types, so this closes no cycle.
//
// ONE VALUE, TWO SHAPES, AND THE RECIPIENT SAYS WHICH. A buyer's mail covers
// every Ticket of one Sale that is still theirs to chase; a Holder's covers
// every Ticket they accepted that the sweep found owing. The fields the other
// shape does not use are zero, and the send site switches on Recipient rather
// than sniffing for an empty string.
//
// BOTH ARE PER PERSON NOW, AND THE LINKS ARE STILL PER TICKET (#335). The buyer
// has one surface for the whole purchase — the Confirmation Link page — so four
// Tickets are one message with one link. The Holder's surface IS a Ticket: the
// Assignment Link opens exactly one, by design and as the whole security
// property of the feature, and there is still no single address that would open
// two — inventing one would be inventing a surface that discloses a Sale to
// somebody entitled to see one Ticket. What #335 ruled is that the ENVELOPE
// follows the inbox anyway: one mail per Holder per sweep, listing each owed
// Ticket with its own Assignment Link, because the buyer's side already fans
// Tickets into one message per Sale, per-Ticket envelopes to a Holder were an
// inconsistency as well as a volume problem, and mailing one address twice in
// one sweep is the shape spam filters punish. Per-Ticket rationing is
// unchanged: each listed Ticket burns its own allowance; only the envelope is
// shared.
type DueAnswerReminder struct {
	// Recipient decides which half of this struct is meaningful, and which of the
	// two messages is composed. There is no third case.
	Recipient AnswerReminderRecipient

	// TicketIDs are the Tickets THIS MAIL COVERS, and the ledger rows it spends
	// once a provider has accepted it. One to many for either recipient since
	// #335, in the order the query returned them.
	//
	// THE LEDGER IS WRITTEN FROM THIS AND NEVER FROM THE SALE. A mail covering
	// three Tickets writes three rows, so the fourth Ticket — answered, or
	// already chased twice, or added to the Sale later — keeps the allowance
	// nobody spent on its behalf.
	TicketIDs []string

	// TicketSaleID is the Sale this mail is about. On a Holder's mail it is
	// carried for the LOG LINE ONLY, so an operator diagnosing a failure can find
	// the row; it never reaches the message, because a Holder is never told which
	// purchase they came from.
	TicketSaleID string
	// SaleStatus and EventStartsAt are MayRemind's facts, kept so the send site
	// can log or re-reason about the same row the query returned.
	SaleStatus    string
	EventStartsAt time.Time
	// EventEnd signs the buyer's Confirmation Link expiry. Unused by a Holder's
	// mail, whose link bakes in no expiry and reads the Event live.
	EventEnd time.Time
	// EventName is the one fact both mails print, and the only fact about the
	// purchase a Holder's mail is allowed to name: an Event is already public on
	// its own Storefront page.
	EventName string
	// SaleLocale is the Sale Locale as stored, read FIRST for a buyer and LAST
	// for a Holder. See AnswerReminderCandidate.SaleLocale.
	SaleLocale string

	// ConfirmationRef, CustomerEmail, CustomerFirstName and CustomerLastName are
	// the buyer's, and are set ONLY when Recipient is RemindTheBuyer.
	//
	// ON A HOLDER'S MAIL THEY ARE EMPTY, and that is a disclosure rule rather
	// than an optimisation: a Sale Confirmation reference and a buyer's name are
	// facts about somebody else's purchase, and ADR 0044's rule — carried over
	// unchanged by ADR 0046 and applied to an inbox — says a Holder is told
	// neither. The mail type they would be composed into has no field for them
	// either, which is the enforcement that survives somebody editing this file.
	ConfirmationRef   string
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string

	// HolderEmail is where a Holder's mail goes, set ONLY when Recipient is
	// RemindTheHolder. Since #335 it is also the GROUPING KEY for
	// holder-addressed candidates: every owed Ticket this address accepted in
	// one sweep's batch becomes one entry of HolderTickets below.
	HolderEmail string
	// HolderTickets are the owed Tickets a Holder's ONE mail lists, in the order
	// the query returned them, each with its own Assignment Link (#335). Empty
	// on a buyer's mail. Its length always equals len(TicketIDs) on a Holder's:
	// the entry is what the message prints, the id is what the ledger spends.
	HolderTickets []HolderReminderTicket
}

// HolderReminderTicket is one owed Ticket as a Holder's Answer Reminder lists
// it: the two public facts the reader recognises it by, and the link they
// answer through.
//
// EventName travels PER TICKET, not once per mail, because the envelope is per
// Holder per sweep (#335) and nothing guarantees every Ticket one address
// accepted belongs to one Event — the mail must be able to name each honestly.
//
// AssignmentLink is the composed Assignment Link URL this Holder answers
// through — the same link their Assignment mail carried, minted fresh because
// the platform stores no tokens.
//
// IT IS A CREDENTIAL AND THE ONLY ONE THAT MINTS AN IDENTITY (ADR 0046), so it
// is worth stating exactly how far it travels and why that is allowed. It is
// composed inside the catalog service, by the one function that composes
// Assignment Links, and handed to the sales module for the single purpose of
// putting it in a message addressed to the HolderEmail it groups under — the
// same address it was already mailed to. It never reaches a buyer surface, an
// API response or a log line: the sweep's 200 body is counts and names nobody,
// and the failure logs name a Ticket Sale and never a link. Anything that
// widens where this field travels is widening where that credential travels,
// and is a change ADR 0046 rates as severely as leaking the signing key.
//
// A TICKET WHOSE LINK COULD NOT BE SIGNED — a deployment with no link secret —
// IS NEVER LISTED: the sweep DROPS that candidate rather than listing it
// linkless, exactly as it skips a Confirmation Link it could not sign. Nothing
// is recorded, so the Ticket is due again as soon as the deployment is fixed.
type HolderReminderTicket struct {
	EventName      string
	TicketTypeName string
	AssignmentLink string
}
