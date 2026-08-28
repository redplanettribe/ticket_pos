package platform

import (
	"context"
	"sync"
	"time"
)

// OTPMessage is the One-time Passcode email, as the recipient reads it (#244).
//
// It is a value rather than a pair of loose strings for the reason every other
// message here is one: the copy lives on the message (email_content.go), so a
// test holding one can render exactly what was delivered. The Locale is what
// decides that rendering, and it is the ONLY per-message state a passcode has —
// the same two sentences go through both doors, and which door was used is not
// something this type records.
type OTPMessage struct {
	// Code is the passcode itself. It is never logged by anything that renders
	// this message; the logging sender prints it deliberately, for local
	// development where there is no mailbox to read.
	Code string
	// Locale is the language this passcode is written in: the Storefront page a
	// visitor asked from, or — at the staff door — the Staff Locale stored
	// against the address it is going to, English underneath either (ADR 0033,
	// ADR 0041).
	//
	// NEITHER DOOR REMEMBERS ANYTHING BY SENDING ONE. A Customer's request
	// reports a page's language and writes no Mail Locale; the staff door only
	// READS a row a sign-in or a switcher wrote. Asking for a passcode proves
	// nothing about who is asking, so it may not record anything about them.
	Locale Locale
}

// SaleConfirmation is the receipt emailed to a Customer when a Ticket Sale is
// recorded. It carries the reference code the customer can present, plus enough
// context to render a human-readable confirmation.
type SaleConfirmation struct {
	To           string
	CustomerName string
	EventName    string
	Reference    string
	// AmountCents and Currency are what the Customer actually paid, so the
	// receipt reconciles against their card statement. A comp sale is 0.
	AmountCents int
	Currency    string
	// ConfirmationLink opens this one Ticket Sale on the Storefront without
	// signing in. It is the path most Customers will ever take back to their
	// purchase: no form, no passcode, no typing. Empty only if the link could not
	// be signed, in which case the receipt still goes out — a missing link is
	// worth less than no email at all.
	ConfirmationLink string
	// ConsentConfirmationLink resolves the Pending Confirmations this buyer's
	// address is carrying: the double opt-in's one-click half (#255, ADR 0035).
	//
	// EMPTY IS THE COMMON CASE AND CHANGES NOTHING. A receipt for a signed-in
	// buyer, for a guest who ticked nothing, for a box office sale or an import
	// carries no such line at all, because there is nothing to confirm. The
	// caller decides — the consent module holds the only view of what is pending
	// — and passes "" when the answer is "nothing", exactly as it does for a
	// Confirmation Link it could not sign.
	//
	// It is a SECOND link in one email, which is a real cost and an accepted one:
	// the alternative is a marketing opt-in that either sends on a stranger's
	// word or never resolves at all. It sits below the Confirmation Link, because
	// the receipt's job is the tickets and this is an aside.
	ConsentConfirmationLink string
	// HasOutstandingAnswers says whether any Ticket on this Sale still owes a
	// required Ticket Question an Answer (#315, ADR 0044). True adds ONE sentence
	// pointing back at the ConfirmationLink above; false — the zero value, and
	// what every caller written before this feature passes — changes nothing at
	// all, and there is a test freezing the rendered receipt against a literal to
	// keep that true.
	//
	// A BOOLEAN AND NOT A COUNT, deliberately. "Three tickets still need answers"
	// reads as more precise and is a promise the mail cannot keep: the debt is
	// derived live and the buyer may answer two of them between the send and the
	// read, at which point the receipt in their inbox is wrong forever. The page
	// behind the Confirmation Link states the real figure at the moment it is
	// looked at, which is the only moment it is true.
	//
	// WHAT IS DELIBERATELY ABSENT IS ANY ANSWER LINK. The sentence points at the
	// Confirmation Link this mail already carries and introduces no URL of its
	// own. An Answer Link is meant to be forwarded and this receipt is meant not
	// to be — it holds the reference, the total and the Tax ID — so a per-Ticket
	// link in the body would make "send my friend the t-shirt question" and "send
	// my friend my receipt" the same gesture. Distribution happens on the page,
	// where the buyer copies one link at a time.
	HasOutstandingAnswers bool
	// TaxID is the Tax ID this Ticket Sale was transacted under, printed on the
	// receipt so the buyer can file it against their own expense records
	// (ADR 0016). It is the sale's immutable snapshot, never the Customer's
	// current stored assertion — a profile edit after the fact changes nothing
	// about a receipt already sent. Unset on sales recorded before the feature
	// and on imported sales that never carried one, in which case the receipt
	// simply has no such line.
	TaxID SaleTaxID
	// SaleInvoiceFollows says a Sale Invoice was owed for this sale — a paid
	// Online Sale of a House Organization's Event (#473, ADR 0060) — and adds
	// ONE sentence telling the buyer a factura will arrive by a separate
	// email. False, the zero value and what every other sale passes, changes
	// nothing, and the byte-identical test keeps that true.
	//
	// It is set from the commit's own answer and never re-derived at send
	// time, so a receipt can only ever promise a document that exists as
	// owed. It says nothing about WHEN: the document is issued by a Drainer
	// on the Tax Authority's timetable, and a receipt that named a delay
	// would be wrong one way or the other.
	SaleInvoiceFollows bool
	// Locale is the language this receipt is written in, ALREADY RESOLVED by the
	// caller through platform.ResolveMailLocale (#245, ADR 0033): the Sale
	// Locale, then the Customer's Mail Locale, then English.
	//
	// It arrives resolved rather than as the two raw values because the chain is
	// one decision and this type is not where it is made — a message that
	// resolved its own language would be a second copy of the ordering, and the
	// void notice and the digest would each need a third.
	//
	// The zero value renders English, which is what every sale recorded before
	// this feature — box office, import, and every Online Sale that predates the
	// column — is written in, exactly as before.
	Locale Locale
}

// SaleVoided is the cancellation notice emailed to a Customer when a Ticket Sale
// is reversed (e.g. an undone Sale Import). It references the original Sale
// Confirmation so the Customer can reconcile the record they were given. This is
// the void/notify path future refunds reuse.
type SaleVoided struct {
	To           string
	CustomerName string
	EventName    string
	Reference    string
	// Locale is the language this notice is written in, ALREADY RESOLVED by the
	// caller through platform.ResolveMailLocale (#246, ADR 0033): the Sale
	// Locale, then the Customer's Mail Locale, then English.
	//
	// This is the message the ordering in ADR 0033 was decided for. A void notice
	// is sent days after the sale, sometimes by a Platform Operator and sometimes
	// by a drain nobody is watching, from NO PAGE AT ALL — so there is nothing at
	// that moment to read a language off except the sale itself. The sale
	// remembers, and this field is what it remembered.
	//
	// The zero value renders English, which is what every sale recorded before
	// this feature — box office, import, and every Online Sale that predates the
	// column — is voided in, exactly as before.
	Locale Locale
}

// SaleReversalRefused is the notice emailed to a Customer whose Reversal Request
// the Payment Provider definitively refused after the platform had already told
// them it was being processed (ADR 0024, #161).
//
// It exists because that is the one path where this system knowingly breaks a
// promise it made, and the person most owed the correction is the one who closed
// the tab: no page will ever reach them again. Which reversal endings send this,
// which send SaleVoided and which send nothing is decided at the one site that
// resolves a pending request — see service.resolveReversalRequest.
//
// It carries no reason and no provider code, deliberately, and the wording that
// enacts that lives on Text().
type SaleReversalRefused struct {
	To           string
	CustomerName string
	EventName    string
	Reference    string
	// Locale is the language this correction is written in, resolved from the
	// sale exactly as SaleVoided.Locale is (#246, ADR 0033).
	//
	// It is the message with the least context available to it of any this
	// platform sends: it is raised by the reversal DRAIN rather than by a request
	// in flight, so no page, no header and no session is anywhere near the code
	// that composes it. That is precisely why the language has to be a property
	// of the sale — the only fact still standing when this is written.
	Locale Locale
}

// HolderAnswerReminderTicket is one owed Ticket as an Answer Reminder lists
// it: what the reader recognises it by, and nothing else.
//
// EventName and TicketTypeName are already public — rows on a Storefront page
// anybody can read — and are the whole of what the mail says about the
// purchase. EventName travels PER TICKET because the envelope is per Holder
// per sweep (#335) and nothing guarantees every Ticket one person holds
// belongs to one Event.
//
// IT CARRIES NO LINK, since ADR 0049. The Assignment Link this entry used to
// hold is a credential that mints an identity, and a reminder no longer needs
// one: the reader answers from their own Customer Area, whose one address the
// message carries once (HolderAnswerReminder.CustomerAreaURL) behind a sign-in
// to the very address the mail is sent to.
type HolderAnswerReminderTicket struct {
	EventName      string
	TicketTypeName string
}

// HolderAnswerReminder is the mail telling the Holder of a Ticket that it still
// owes an Answer, and pointing them at the Customer Area where they give it
// (#328, parent #322, ADR 0046; #347, parent #342, ADR 0049).
//
// IT IS THE ONLY ANSWER REMINDER. ADR 0044 addressed the reminder to the buyer
// "because there is nobody else to address"; ADR 0046 gave a Ticket a Holder
// who proves their address by clicking; ADR 0049 ruled that only the Holder
// answers, so only the Holder is chased. The buyer receives one of these about
// their own Self-held Ticket (ADR 0048) and about nothing else: a Ticket they
// have not handed on has nobody to answer for it and is not chased at all.
// The separate buyer-addressed reminder, with its Sale Confirmation reference
// and Confirmation Link, is gone.
//
// IT NAMES NOTHING ABOUT THE PURCHASE, and the type is the enforcement. A
// Holder is told the Event, the Ticket Type and where to sign in, and NEVER a
// buyer's name, the price, the Tax ID, the Sale Confirmation reference or the
// Sale's other Tickets — ADR 0044's disclosure rule, carried over unchanged by
// ADR 0046 and applied to an inbox. There is no field for any of it, so a
// template cannot print it by mistake. That the buyer reads the same message
// about their own Ticket costs them nothing: their receipt already told them
// everything this one withholds.
//
// IT IS TRANSACTIONAL, and its place on EmailSender's transactional half is what
// makes that structural rather than remembered. It matters more here than for
// any other message on that half: a Holder accepted a ticket and opted into
// NOTHING — accepting grants no consent of any kind (ADR 0046) — so there is no
// Marketing Consent to read and, being about a ticket they hold, none is
// required. Nothing anywhere reads a consent state before sending it and it is
// not reachable from the marketing sending identity at all (ADR 0030, ADR 0034).
// What bounds it instead is catalog.MayRemind, per TICKET: at most one a week,
// at most two ever, silence once the Event has started.
//
// ONE PER HOLDER PER SWEEP SINCE #335. A person holding two owed Tickets gets
// one message listing both; the per-Ticket caps did not move — each listed
// Ticket burns its own allowance, only the envelope is shared.
//
// IT IS SWEPT RATHER THAN TRIGGERED. Nothing composes one when a question is
// authored — an Organization drafting four questions in ten minutes would
// otherwise mail every Holder four times — so the only caller is the scheduled
// job in the sales module.
type HolderAnswerReminder struct {
	// To is the address that holds the Ticket: `tickets.holder_email`,
	// normalised — the address that accepted it, or the buyer's own for a
	// Self-held Ticket. It is the address the Customer Area sign-in below will
	// ask for.
	To string
	// Tickets are the owed Tickets this ONE mail lists — at least one, usually
	// exactly one.
	Tickets []HolderAnswerReminderTicket
	// CustomerAreaURL is the Storefront's Customer Area, where the reader signs
	// in with the address this mail reached and answers from the held-ticket
	// panel (ADR 0049). ONE address for the whole message, however many Tickets
	// it lists: the panel shows every Ticket the reader holds.
	//
	// IT IS THE WHOLE MESSAGE. A reminder with nowhere to go is an instruction
	// its reader cannot follow, so the sweep refuses to compose one without it
	// rather than rendering a shorter mail. It is NOT a credential: nothing in
	// it signs anything, and a forwarded copy opens nothing.
	CustomerAreaURL string
	// Locale is the language this is written in, ALREADY RESOLVED by the caller
	// (ADR 0033).
	//
	// THE CHAIN IS READ RECIPIENT-FIRST, as the Assignment mail's is and unlike
	// every other message about a sale. This reader is not party to the sale:
	// they did not buy anything, were not on the page that recorded a Sale
	// Locale, and may not share the buyer's language at all — a Spanish-speaking
	// Holder whose friend paid on the English site is exactly the case the
	// feature exists to serve. So their own remembered Mail Locale outranks the
	// sale's, and the sale's is kept as the better-than-nothing fallback. For
	// the buyer's own Self-held Ticket the two agree in every ordinary case;
	// see #325 and service.assignmentMailLocale for the reasoning this borrows.
	Locale Locale
}

// AssignmentReminder is the mail telling the buyer of a Ticket Sale that some
// of its Tickets still have nobody, and pointing them at the Sale's own page by
// a fresh Confirmation Link (#362, parent #361, ADR 0051).
//
// IT IS ADDRESSED TO THE BUYER, AND ONLY EVER TO THE BUYER. An unassigned Ticket
// has no Holder (ADR 0046), so there is nobody else who could be told and
// nobody else who could act; the Answer Reminder beside this one chases the
// Holder about an Answer, and ADR 0049 kept the two apart on purpose. One Sale
// is one inbox for this reader, so one Sale is one mail, carrying a count.
//
// IT IS THE RECEIPT'S SENTENCE, SAID AGAIN LATER. The Sale Confirmation already
// carried a Confirmation Link to the same page; this repeats that link days or
// weeks on, with the one fact the receipt could not have known — how many
// Tickets are still nobody's. Nothing else the receipt printed is repeated:
// NO price, NO Tax ID, NO Sale Confirmation reference, NO list of Tickets. The
// type has no field for any of them, which is the enforcement; mail is
// forwarded, and a forwarded reminder should give away nothing a forwarded
// link does not already.
//
// IT DISCLOSES WHAT ASSIGNING DOES, which ADR 0047 requires of every surface
// that invites an address: the Holder is emailed, and the Organization will see
// the address beside the Ticket. A reader deciding whether to type a friend's
// address is entitled to both halves before they do.
//
// IT IS TRANSACTIONAL, and its place on EmailSender's transactional half is
// what makes that structural. It is about the reader's own purchase, on the
// same footing as the Sale Confirmation, so it is not gated by Marketing
// Consent and nothing reads a consent state before sending it; it is not
// reachable from the marketing identity at all (ADR 0030, ADR 0034). What bounds
// it is catalog.MayRemindAssignment: not before a day after the Sale, at most
// one a week, at most two ever, silence once the Event starts.
//
// IT IS SWEPT RATHER THAN TRIGGERED: the only caller is the scheduled job in
// the sales module, so a buyer assigning and unassigning three times in an
// evening is written to at most as the rationing allows.
type AssignmentReminder struct {
	// To is the Sale's buyer address, as recorded on the Ticket Sale.
	To string
	// FirstName is the buyer's first name, for the greeting — the same fact the
	// receipt greeted them with.
	FirstName string
	// EventName, EventStartsAt and EventTimezone are the Event's own name,
	// start and zone. The date is rendered in the Event's zone for the Follow
	// Digest's reason: a buyer is told the day the doors open where the doors
	// are. An unloadable zone drops the date line rather than lying about it.
	EventName     string
	EventStartsAt time.Time
	EventTimezone string
	// UnassignedTickets of TotalTickets is the tally the mail prints — the
	// Customer Area's own, so mail and page agree. UnassignedTickets is at
	// least one on any reminder the sweep composes.
	UnassignedTickets int
	TotalTickets      int
	// ConfirmationLink opens this one Ticket Sale on the Storefront without a
	// sign-in, freshly minted for this mail and expiring at the Event's end. IT
	// IS THE WHOLE MESSAGE: a reminder with nowhere to go is an instruction its
	// reader cannot follow, so the sweep refuses to compose one without it.
	ConfirmationLink string
	// SaleCreatedAt is when the Ticket Sale was recorded. It is never printed;
	// Text() compares it against TicketAssignmentWentLiveAt and, for a Sale
	// older than that, adds ADR 0051's one extra sentence — "when you bought,
	// tickets could not yet be assigned; now they can" (#364).
	SaleCreatedAt time.Time
	// Locale is the language the mail is written in: the Sale Locale first,
	// because this is the buyer's own purchase, then their remembered Mail
	// Locale, then English (ADR 0033). Resolved by the sweep, never here.
	Locale Locale
}

// TicketAssignment is the mail telling somebody that a friend bought them a
// ticket, and carrying the Assignment Link whose click accepts it (#325, parent
// #322, ADR 0046).
//
// IT IS THE ONE MESSAGE THIS PLATFORM SENDS TO SOMEBODY WHO NEVER CAME HERE, at
// an address supplied by a person with no authority to supply it. That is the
// price of the feature and ADR 0046 is where it is paid; everything odd about
// the shape below is that price being paid in fields.
//
// IT NAMES NO BUYER. There is no CustomerName here and there must never be one:
// the reader is owed the fact that somebody bought them a ticket, not that
// person's identity, and a mail that named them would disclose a fact about the
// purchase to whoever the mail was forwarded to. For the same reason there is no
// price, no Tax ID and no Sale Confirmation reference — the Answer Link's
// disclosure rule (ADR 0044), carried over unchanged and applied to an inbox.
//
// IT IS TRANSACTIONAL, and its place on EmailSender's transactional half is what
// makes that structural rather than remembered. Nothing reads Marketing Consent
// before sending it and it is not reachable from the marketing sending identity
// (ADR 0030, ADR 0034). What bounds it instead is that only a buyer naming a NEW
// address sends one at all: re-submitting the address a Ticket already carries
// writes nothing and mails nobody.
type TicketAssignment struct {
	// To is the address the buyer named, normalised by NormalizeEmail before it
	// was stored (migration 080). It belongs to a third party who has agreed to
	// nothing, which is why #322's purge takes it when the Event starts.
	To string
	// EventName and TicketTypeName are what the reader has to recognise this by,
	// and both are already public: they are rows on a Storefront page anybody can
	// read. They are the whole of what this mail says about the purchase.
	EventName      string
	TicketTypeName string
	// AcceptURL is the Assignment Link: the Storefront address whose click
	// accepts, carrying a token distinct from the Answer Link and delivered ONLY
	// here.
	//
	// IT IS THE WHOLE MESSAGE, like the Answer Reminder's link and unlike the
	// receipt's: a message telling somebody they have a ticket and giving them no
	// way to claim it would be an instruction its reader cannot follow. The
	// caller refuses to compose one rather than sending it linkless.
	//
	// THIS FIELD IS WHY THIS TYPE EXISTS AT ALL. The link must never appear on a
	// buyer surface or in any API response to the buyer — the Answer Link is
	// copyable off the buyer's own sale page, so a design that showed this one
	// there too would mean the click proves nothing and the Verified Customer
	// minted from it is a fiction (ADR 0046). Mail is the only carrier.
	AcceptURL string
	// Locale is the language this is written in, ALREADY RESOLVED by the caller
	// (ADR 0033).
	//
	// IT IS THE ONE MAIL WHOSE READER IS NOT PARTY TO THE SALE, so the usual
	// chain is read in the other order: the recipient's own remembered Mail
	// Locale outranks the language the BUYER was reading when they paid. See
	// service.assignmentMailLocale.
	Locale Locale
}

// SaleReAddressing is the mail telling the address a Sale Re-addressing names
// that a purchase is being re-addressed to them, carrying the Re-addressing
// Link whose click accepts it (#420, parent #419, ADR 0058).
//
// ITS READER MAY BE NOBODY, THE BUYER, OR A STRANGER. The Operator typed this
// address from a support thread; if it is right, the buyer finally reaches the
// tickets they paid for; if it is wrong again, the mail lands on nobody or on
// somebody who never bought anything. The copy is written for the third case
// as carefully as the second: it says plainly that accepting takes on somebody's
// purchase, so that ignoring it is the obvious move and costs nothing.
//
// IT IS TRANSACTIONAL, beside the Assignment mail, and its recipient has
// consented to nothing — so it must not be reachable from the identity that
// carries marketing (ADR 0030). What bounds it is that only an Operator's act
// sends one, against one active Online Sale, one pending at a time.
type SaleReAddressing struct {
	// To is the corrected address, normalised by NormalizeEmail before it was
	// stored (migration 093). Until accepted it belongs to somebody who is not
	// here to confirm anything, which is why #424's purge takes it at the
	// Event's start.
	To string
	// EventName and Reference are what the reader recognises the purchase by.
	// The reference is the Sale's own `TP-` code — the thing the buyer quoted
	// to support, so the buyer knows which of their Sales this is (a buyer with
	// two stranded Sales gets two mails, each naming its own), and a thing that
	// is not a credential, so a stranger gains nothing by reading it.
	EventName string
	Reference string
	// AcceptURL is the Re-addressing Link: the Storefront address whose click
	// accepts, carrying a token delivered ONLY here. It never appears in any
	// Operator or staff response — that is what keeps the Operator from
	// completing the acceptance themself (ADR 0058). The caller refuses to
	// compose a linkless message rather than send one.
	AcceptURL string
	// Locale is the language this is written in, ALREADY RESOLVED by the
	// caller: the Sale's own locale first (ADR 0058: "mail follows the Sale's
	// own locale, as the original Sale Confirmation did"), then the recipient's
	// remembered Mail Locale, then English (ADR 0033).
	Locale Locale
}

// NoLongerHolding is the mail telling an accepted Holder that a Ticket they held
// is no longer theirs (#327, parent #322, ADR 0046).
//
// ONE MESSAGE FOR TWO CAUSES, AND THE SHAPE IS WHERE THAT RULE IS ENFORCED. A
// Holder stops holding a Ticket for exactly two reasons — the buyer reassigned
// it, or the Ticket Sale was reversed — and from where the Holder sits the
// outcome is identical: they had a ticket and now they do not. So there is one
// type, with NO field naming the cause and no room to add one. A platform
// talkative about a reassignment and silent about a reversal would be teaching
// its readers to infer the cause from the silence.
//
// IT NAMES NO BUYER AND GIVES NO CAUSE. There is no CustomerName here, no
// `reason`, no `reversed_at`, no Sale Confirmation reference and no price. Who
// bought the ticket and what they decided to do with it are facts about somebody
// else's purchase, and ADR 0044's disclosure rule — carried over unchanged by
// ADR 0046 — binds this message exactly as it binds the Assignment mail. Mail
// gets forwarded; "your friend cancelled" is not this platform's to say.
//
// IT CARRIES NO LINK, and that absence is deliberate rather than unfinished.
// There is nothing left for the reader to do: the Ticket is not theirs, no page
// would show it, and an Assignment Link for it has already stopped opening. A
// message with a button on it would be inviting somebody to press their way back
// to something that is gone.
//
// IT IS OWED ONLY TO SOMEBODY WHO ACCEPTED. An address that was typed and never
// accepted is never sent this, because the platform never told that person they
// had anything — so telling them now that they have lost it would be the
// platform's first and only word to a stranger, about a ticket they never knew
// existed. The caller enforces that; this type simply never gets built for them.
//
// TRANSACTIONAL, on EmailSender's transactional half with the receipts, so no
// consent state is read before sending it and it is not reachable from the
// marketing sending identity at all (ADR 0030, ADR 0034). A Holder consented to
// nothing by accepting a ticket, and being told the ticket is gone is not
// marketing.
type NoLongerHolding struct {
	// To is the Holder's own address — proven by their click, which is what makes
	// this the one mail in this flow whose recipient is certainly a real person
	// who chose to be here.
	To string
	// EventName is the whole of what this message says about the ticket, and it
	// is already public: the Event has a Storefront page anybody can read. There
	// is deliberately no Ticket Type here either — "which kind of ticket you no
	// longer have" is a distinction with nothing behind it for a reader who has
	// none.
	EventName string
	// Locale is the language this is written in, ALREADY RESOLVED by the caller
	// (ADR 0033), and resolved RECIPIENT-FIRST for the reason the Assignment
	// mail's is: this reader is not party to the sale. Unlike that mail's reader,
	// though, this one is certainly a Customer — they accepted — so the
	// remembered Mail Locale is usually there to be found.
	Locale Locale
}

// ConsentWithdrawalConfirmation is the message a Customer receives when a
// Consent Withdrawal actually took something away (#267, parent #265).
//
// IT IS TRANSACTIONAL MAIL, and that is the decision this type exists to make
// structural rather than remembered. It is sent to somebody who has just
// withdrawn Marketing Consent, on the same footing as a Sale Confirmation or a
// One-time Passcode: nothing about it reads `digest_enabled` or a consent state,
// because suppressing it would make the one act that must be confirmed the one
// act met with silence. It sits on EmailSender's transactional half with the
// receipts, never beside the Follow Digest, so it is not even reachable from the
// marketing sending identity (ADR 0030).
//
// IT IS SENT ONLY WHEN SOMETHING MOVED. The decision is the consent module's —
// a capture reports what it withdrew (consent.Receipt.Withdrawn) — and an act
// that changed nothing composes no message at all. Nobody is written to about a
// change that did not happen.
//
// IT NAMES WHAT WAS ACTUALLY WITHDRAWN, which is why the two flags below are
// here (#270). #267 shipped with Marketing Consent alone because the two
// surfaces that could withdraw anything withdrew nothing else — the unsubscribe
// link at the foot of a Follow Digest and the Customer Area's digest toggle (ADR
// 0034, one switch) — and said in as many words that a later surface able to
// withdraw Networking Consent must say what IT took away, by a field here and a
// sentence beside that one, never by this message being sent for an act it does
// not describe. The passcode-only withdrawal surface is that surface, and this
// is that field: the copy is chosen from what moved, and the marketing-only
// wording is unchanged for the acts that produce it.
type ConsentWithdrawalConfirmation struct {
	// To is the Customer's own stored address, which is the only address a
	// withdrawal confirmation can be about: the act named a Customer, and the
	// person entitled to learn that somebody acted on their behalf is whoever
	// holds that inbox.
	To string
	// Locale is the language this is written in, ALREADY RESOLVED by the caller
	// through platform.ResolveMailLocale (ADR 0033): no sale is involved, so it
	// is the Customer's remembered Mail Locale, then English.
	//
	// A message about somebody's legal rights is the last one that may arrive in
	// a language they cannot read, which is why this is resolved rather than
	// defaulted at the send site.
	Locale Locale
	// MarketingConsent and NetworkingConsent are WHAT THIS ACT TOOK AWAY, and
	// they decide which of the three wordings the reader gets. They are the
	// receipt's Withdrawn (consent.Receipt), never the answers a surface
	// submitted: a person who switched off a consent that was already off is not
	// written to at all, so a message that named it would be reporting a change
	// that did not happen.
	//
	// At least one is true wherever this message is composed, because the caller
	// sends nothing when nothing moved.
	MarketingConsent  bool
	NetworkingConsent bool
}

// The five Payout Request notices (#179 and #188, ADR 0026), and the platform's
// first organizer-facing email: every message above this line is a Customer's
// receipt or a staff sign-in code.
//
// Two things are true of all five and are enforced by their Text() methods
// rather than by their callers. NONE of them carries a bank detail — the
// database now holds account numbers, and an email is the one place they could
// leak into a mail-server log, a phone notification and a screenshot at once, so
// they say "the account on your Payout Profile" and never which one. And all
// five are BEST-EFFORT: the money record is the fact and the email is the
// courtesy, so a delivery failure is swallowed by the caller exactly as every
// other notice on this path is (ADR 0019).
//
// ALL FIVE CARRY A LOCALE (#285, ADR 0041), and it is the RECIPIENT'S Staff
// Locale, resolved by the caller from the address the notice is going to — the
// operator allowlist entry for the submission, the request's own `requested_by`
// for the four answers — with English as the floor when nobody at that address
// has stated a language. That address is precisely the record ADR 0033 said did
// not exist, which is why these notices were English until it did.
//
// The locale decides words and marks and NOTHING ELSE: the amount stays in the
// Organization's currency and the transfer's date stays the day it was in
// Ecuador, in either language.

// PayoutRequestSubmitted tells one Platform Operator that an Organization has
// asked to be paid. One is sent per address on the operator allowlist, which is
// the whole of operator authority (ADR 0015) — there is no role to check and no
// subscription to consult.
//
// It exists because the pending-count badge on the operator navigation only
// works for somebody who already decided to look, and a Friday-evening request
// otherwise waits until Monday.
// QuestionReviewSubmitted tells one Platform Operator that an Organization has
// submitted an Event's Ticket Questions for review (#406, ADR 0056). One is
// sent per address on the operator allowlist, on the terms the Payout Request's
// submission notice is: the allowlist is the whole of operator authority, and
// each address is written to in its own Staff Locale.
//
// It carries no question text. What was asked is read on the Operator
// Dashboard, where the verdict is given; the notice's job is to say that
// something is waiting, whose it is, and how much of it there is.
type QuestionReviewSubmitted struct {
	To               string
	Locale           Locale
	OrganizationName string
	EventName        string
	// SubmittedBy is the submitting Member's email, recorded on the Review so
	// it outlives their Membership, and in the notice so an Operator can reply
	// to a person.
	SubmittedBy string
	// QuestionCount is how many questions the Review carries; Options are not
	// counted, because "3 questions" is what an Operator budgets time for.
	QuestionCount int
	// Note is the Organization's own words, empty when they wrote none.
	Note string
}

// QuestionReviewAnswered tells the Member who submitted a Question Review
// what the Platform Operator ruled, item by item (#407, ADR 0056). One is
// sent, to submitted_by, in their Staff Locale: the record names one person,
// and the verdict is theirs to act on.
//
// Unlike the submission notice this one DOES carry question text: the reader
// wrote the questions, and a refusal without the words it refused would send
// them to the editor to guess.
type QuestionReviewAnswered struct {
	To               string
	Locale           Locale
	OrganizationName string
	EventName        string
	// AnsweredBy is the Operator's email, so the Organization can reply to a
	// person about a refusal.
	AnsweredBy string
	Items      []QuestionReviewAnsweredItem
}

// QuestionReviewAnsweredItem is one ruling: a question, or an Option of one
// when OptionLabel is set. Verdict is `approved` or `refused`; Reason travels
// on a refusal and is empty otherwise.
type QuestionReviewAnsweredItem struct {
	QuestionLabel string
	OptionLabel   string
	Verdict       string
	Reason        string
}

type PayoutRequestSubmitted struct {
	To string
	// Locale is the language this operator reads, resolved from the Staff Locale
	// stored against THIS address rather than against the Organization that asked
	// — one submission fans out to the whole allowlist, and each of them is
	// written to in their own language.
	Locale           Locale
	OrganizationName string
	// AmountCents and Currency are what was asked for, in the Organization's own
	// currency — the only currency any of its money is ever stated in.
	AmountCents int
	Currency    string
	// RequestedBy is the asking Member's email, recorded on the request itself so
	// it outlives their Membership. It is in the notice because an operator
	// deciding whether to act tonight often wants to reply to a person.
	RequestedBy string
	// Note is the organizer's own message with the ask, and is usually the reason
	// it is urgent ("we owe the venue on Monday"). Empty when they wrote none, in
	// which case the notice simply has no such line.
	Note string
}

// PayoutRequestPaid tells the asking Member that the transfer has been made, so
// they know to look at their bank. It goes to the one address recorded on the
// request and not to every Org Admin: one request, one asker, one reply.
type PayoutRequestPaid struct {
	To string
	// Locale is the asker's Staff Locale, read off the address on the request.
	Locale           Locale
	OrganizationName string
	// AmountCents is what ACTUALLY moved, which is not required to equal what was
	// asked — an operator may transfer less, and partial fulfilment is
	// deliberately not modelled (ADR 0026).
	AmountCents int
	// RequestedCents is what was asked for. It is carried so the notice can name
	// the gap when the two differ: this email is the only place an organizer is
	// ever told that a transfer fell short of their ask, and finding out from a
	// bank statement instead is the support thread this feature exists to remove.
	RequestedCents int
	Currency       string
}

// PayoutRequestDeclined tells the asking Member that the ask was refused, and
// why.
//
// The reason is the whole point of the message. It is required by the handler,
// by the service and by a CHECK constraint under both (ADR 0026), and all three
// are wasted if it never reaches the person who has to decide what to do next.
type PayoutRequestDeclined struct {
	To string
	// Locale is the asker's Staff Locale, read off the address on the request.
	// The REASON is not translated by anything and never could be: it is the
	// operator's own sentence to this Organization, and quoting it verbatim is
	// what the whole notice is for.
	Locale           Locale
	OrganizationName string
	AmountCents      int
	Currency         string
	Reason           string
}

// PayoutRequestTransferSent tells the asking Member that an operator has
// submitted the transfer to the bank, and that the money is not there yet
// (#188, ADR 0026 amendment).
//
// This is the notice that stops the "where is my money" message on day one. A
// request that has been read and acted on and one nobody has opened both read as
// waiting from the organizer's side, and only one of them deserves patience.
//
// SubmittedAt is the instant the transfer was submitted, and the notice states
// it as a DATE. It is carried rather than left implicit because the 48-hour
// expectation is only checkable against a day the organizer can count from; a
// message saying "up to 48 hours" with no starting point is a message they
// cannot act on, which is what "we sent it recently" already is.
type PayoutRequestTransferSent struct {
	To string
	// Locale is the asker's Staff Locale, read off the address on the request. It
	// spells the month of SubmittedAt below and moves nothing else: the date is
	// the day the transfer left in Ecuador in either language.
	Locale           Locale
	OrganizationName string
	// AmountCents is what was ASKED for. Nothing has moved yet, so there is no
	// second figure to reconcile against: what the transfer actually settles at
	// belongs to the paid notice, which is written when the money lands.
	AmountCents int
	Currency    string
	SubmittedAt time.Time
}

// TicketQuestionRevoked tells one Org Admin that a Platform Operator has
// revoked an approved Ticket Question, and why (#410, ADR 0056).
//
// The second use of the organizer-facing channel ADR 0026 opened, which
// ADR 0056 named as a decision. One notice per Org Admin of the Organization,
// each in their own Staff Locale, because a Revocation has no single asker the
// way a Payout Request has: the question was the Organization's, and every
// person accountable for the Organization is told.
//
// THE REASON IS NEVER TRANSLATED. It is the Operator's own sentence to this
// Organization, quoted verbatim in either language. The question's label and
// the Event's name are data too, and read as coined.
type TicketQuestionRevoked struct {
	To               string
	Locale           Locale
	OrganizationName string
	EventName        string
	QuestionLabel    string
	Reason           string
}

// PayoutRequestTransferFailed tells the asking Member that the bank sent the
// transfer back, and why (#188, ADR 0026 amendment).
//
// This is the one notice of the five that is ACTIONABLE, and the only one whose
// absence costs the organizer money: the commonest failure is a wrong account
// number on their own Payout Profile, and a failure that only appears on a page
// they would have to think to visit is a week of waiting followed by the support
// thread this feature exists to prevent.
//
// Reason is the operator's free text and is rendered verbatim by Text(). It is a
// BANK's answer, not a judgement, and `failed` and `declined` share a column
// precisely so that they must never come out sharing a sentence — nothing here
// prefixes, softens or reframes it into a refusal.
type PayoutRequestTransferFailed struct {
	To string
	// Locale is the asker's Staff Locale, read off the address on the request.
	// The bank's Reason is rendered verbatim in either language, for the reason
	// above: nothing translates, prefixes or softens it.
	Locale           Locale
	OrganizationName string
	AmountCents      int
	Currency         string
	Reason           string
}

// FollowDigest is the one weekly email carrying everything a Customer Follows
// (#220, parent #215, ADR 0030), and it is unlike every message above it in two
// ways that shape this type.
//
// It is the platform's FIRST NON-TRANSACTIONAL mail. Everything else here
// answers something the reader just did — a passcode they asked for, a receipt
// for a purchase they made, a notice about money they are owed. This one arrives
// unbidden, which is why it is the only mail a Customer can turn off, and why
// nothing in this system may ever send it to somebody who did not press Follow.
//
// It WAS THE FIRST MESSAGE THAT BRANCHED ON LANGUAGE, and Locale was once a
// field here and on nothing else; the passcode, the receipt and the two sale
// notices all carry one now (ADR 0033). A Locale is otherwise a property of a
// page's address (ADR 0027) and mail has no address, so this one is the
// Customer's Mail Locale, remembered at sign-in (#216) and carried here.
//
// It is NEVER SENT EMPTY. A Digest with no Events is not composed into a message
// at all — see digest/service.deliverDigest, which records the Digest as `empty`
// and sends nothing. The Digest is the whole payload of a Follow, and one that
// says nothing teaches its reader to ignore the next one.
type FollowDigest struct {
	To string
	// CustomerName is the reader's first name, for the greeting. A Customer
	// record always has one — it is required on every path that creates one — so
	// there is no absent case to render around.
	CustomerName string
	// Locale is the Customer's Mail Locale and decides which language every
	// word of this message is written in, including the Tag names already
	// resolved into Events below. It is never empty: DefaultLocale is what a
	// Customer who has never been on a localized surface reads in.
	Locale Locale
	// New and Happening are the Digest's TWO SECTIONS (#221), each soonest first,
	// and they answer two different questions.
	//
	// New is what this reader has never been shown — novelty as ADR 0030 defines
	// it, a fact about the reader rather than about the Event. Happening is what
	// they were already told about and which now starts within seven days, which
	// is the week-before reminder the Digest absorbed rather than sending as a
	// second kind of mail.
	//
	// AN EVENT IS NEVER IN BOTH. One qualifying for each is news, once: a reader
	// who meets the same Event twice in one email learns that this mail repeats
	// itself. The rule is enforced where the two lists are built
	// (digest/repository.DigestCandidates) rather than here, because it is a
	// property of the sets and not of the rendering.
	//
	// EITHER MAY BE EMPTY, and an empty one prints no heading at all — a Digest
	// with nothing new must not announce a "New this week" with nothing under it.
	// Both empty is impossible: a caller with nothing to say sends nothing.
	New       []FollowDigestEvent
	Happening []FollowDigestEvent
	// NewOverflow and HappeningOverflow are what each section could not carry
	// (#222): how many more matched, and where the reader can see them.
	//
	// A CAP THE READER CANNOT SEE IS A LIE ABOUT WHAT THEY FOLLOW. Ten Events
	// under a heading look identical whether ten matched or three hundred did,
	// and a reader who cannot tell the two apart has been told that Following
	// that Tag is worth less than it is. This is the section admitting to its own
	// edge.
	//
	// Zero on a section that fitted, which is the ordinary case and prints
	// nothing at all.
	NewOverflow       FollowDigestOverflow
	HappeningOverflow FollowDigestOverflow
	// UnsubscribeURL is the signed link that turns this Digest off, carried in
	// the footer of every one (#224, ADR 0030).
	//
	// EVERY DIGEST CARRIES IT, which is the first acceptance criterion and the
	// one every other one depends on — there is no opt-out at all if the message
	// does not carry it. It is minted per Customer by the customers module, which
	// owns the signing key, and it names that one Customer and nothing else.
	//
	// It points at a STOREFRONT PAGE and not at an API endpoint, and the page
	// confirms with a POST. Mail security scanners prefetch every link in every
	// message before a human sees one, so a link that acted on being fetched
	// would let a corporate scanner silence everybody it protects, silently and
	// permanently.
	//
	// Empty only when the platform holds no signing key, in which case the
	// footer degrades to the closing line alone rather than the Digest failing to
	// send — see digest/service.unsubscribeURL.
	UnsubscribeURL string
}

// FollowDigestEvent is one Event as a Follow Digest lists it.
//
// Every string here is ALREADY RENDERED in the reader's Mail Locale by the
// time it arrives. In particular the Tag names have already been resolved
// through catalog's LocalizedTagNames, which is the one place the rule lives:
// a Preset Tag is named in the reader's language, a Custom Tag exactly as the
// Organization coined it (ADR 0027 as amended by ADR 0030). This type does no
// language work of its own beyond choosing its own sentences, because doing it
// twice is how the two copies come to disagree.
// FollowDigestOverflow is what one capped section of a Digest did not carry
// (#222): how many more Events matched, and the one address that shows them.
//
// IT POINTS AT A SURFACE THAT ALREADY EXISTS, and that is ADR 0030's decision
// rather than this type's convenience: the global explorer filtered by a Tag's
// canonical key, or an Organization's public page. No Following feed is built,
// because the whole payload of a Follow is this Digest and a second browsable
// stream of the same Events would compete with the explorer for a job the
// explorer already does.
//
// The URL is chosen from the Follows the SHED Events actually matched, so the
// link is true about what is behind it — see digest/service.sectionOverflow.
type FollowDigestOverflow struct {
	// Count is how many matched Events this section could not carry. Zero means
	// the section fitted and nothing is printed.
	Count int
	// URL is where the rest can be seen.
	//
	// Empty only when the platform has no Storefront origin configured, in which
	// case the count is still printed without a link: "there are eleven more"
	// with nowhere to go is thin, and saying nothing at all would be a silent
	// truncation, which is the one outcome this feature exists to prevent.
	URL string
}

type FollowDigestEvent struct {
	Name string
	// StartsAt and Timezone are the Event's own start and its own zone. The date
	// is rendered in the EVENT's zone rather than in the reader's or the
	// platform's, for the reason every other Event-facing surface does it: an
	// Event starting at 20:00 in Guayaquil starts at 20:00 for everybody reading
	// about it, and a date shifted into somebody else's zone is a date they will
	// turn up on the wrong day for.
	StartsAt time.Time
	Timezone string
	// OrganizationName is who is putting it on — the fact a reader uses to place
	// an Event they have not heard of.
	OrganizationName string
	// Venue is where it is. It is here because an entry has to carry enough to
	// decide WITHOUT CLICKING (#221): what it is, when, where and who is putting
	// it on is what the decision is actually made from, and a listing that gives
	// only a name makes the reader open a page to find out whether they care.
	//
	// Empty when the Event names no venue, in which case the line is omitted
	// rather than rendered blank.
	Venue string
	// URL opens the Event on the Storefront. Built in the same shape an Affiliate
	// Link is (service.storefrontURL), because an Event listing with nothing to
	// press is a list of things the reader now has to go and search for.
	URL string
	// MatchedOrganization and MatchedTagNames are WHY this Event is in this
	// person's Digest: the Follows of theirs it matched.
	//
	// An Event can match both, and both are carried rather than one being picked:
	// the reason a Digest gives has to be the true one, and "because you follow
	// Music" is a strange thing to read about an Event by an Organization you
	// deliberately subscribed to. ADR 0030 also asks that which Follow matched be
	// recorded, so that Tag stuffing can be measured before anything is
	// legislated against it.
	MatchedOrganization bool
	MatchedTagNames     []string
	// Attending is whether the reader already holds a live Ticket Sale for this
	// Event (#223), which changes what the entry says about itself in two ways:
	// it is marked as one they are going to, and it carries no purchase call to
	// action at all.
	//
	// It is the difference between a Digest that is useful and one that is
	// embarrassing. Telling somebody who bought tickets three weeks ago to "get
	// tickets" for the show they are going to on Saturday is the single most
	// visible way this mail could be wrong, because the reader knows the answer
	// better than the sender does.
	//
	// NEVER TRUE ALONGSIDE ExternallyRegistered. Registration for such an Event
	// happens on somebody else's site and this platform never learns whether it
	// happened — the Registration Link shows its clicks and nothing more — so
	// there is no honest way to mark one as attended. The rule is held where the
	// flag is computed (digest/repository.DigestCandidates); this comment is what
	// stops a future renderer inventing a case for it.
	Attending bool
	// ExternallyRegistered is whether this Event sends its audience elsewhere to
	// sign up rather than selling Tickets here (ADR 0028).
	//
	// It chooses the call to action, and the choice is exclusive because the
	// modes are: such an Event has NO Ticket Types, so a purchase line would
	// point at a page with no checkout on it. Register is not a softer way of
	// saying buy — it is the only way in.
	ExternallyRegistered bool
	// TicketSaleURL is where an attending reader finds the Ticket Sale they
	// already hold, and it is read only when Attending is true.
	//
	// It is the CUSTOMER AREA and not a per-sale address, because the Storefront
	// has no per-sale page: a Ticket Sale is opened either from the Area behind a
	// Customer Session or through the Confirmation Link carried in its own Sale
	// Confirmation. The Confirmation Link is deliberately NOT used here — it is a
	// bearer credential that opens one sale without signing in, and minting one
	// into a weekly marketing message would put a credential in an email nobody
	// asked for, forwarded and scanned like any other.
	//
	// Empty when the platform knows no Storefront origin, in which case the entry
	// still says the reader is going and simply offers nowhere to press.
	TicketSaleURL string
}

// EmailAttachment is one file carried by a message: the bytes exactly as
// stored, the filename the reader saves them under, and their media type.
//
// It exists for the Tax Document delivery below and for nothing before it:
// every earlier message is prose with links in it, and the platform's first
// attachment is the one the SRI obliges the emisor to hand the buyer (ADR
// 0060). Nothing here is rendered or logged; the bytes are a legal artifact
// and the logging sender prints their length alone.
type EmailAttachment struct {
	Filename    string
	ContentType string
	Body        []byte
}

// The kinds of Tax Document a buyer can be handed, as the delivery mail
// names them. They are the invoicing module's document kinds spelled here
// so this package need not import it; the mail's copy is keyed on them.
const (
	// TaxDocumentKindSaleInvoice is a Sale Invoice: the factura for a paid
	// House sale (ADR 0060).
	TaxDocumentKindSaleInvoice = "sale"
	// TaxDocumentKindCreditNote is a Credit Note: the nota de crédito for a
	// reversed one.
	TaxDocumentKindCreditNote = "credit_note"
	// TaxDocumentReasonReissue is the Credit Note reason that is not a Sale
	// Reversal's route (#481, ADR 0061): the earlier factura is cancelled
	// for a correction of its Recipient and a corrected one follows. Any
	// other reason on a Credit Note is a reversal route, and the copy says
	// the purchase was reversed.
	TaxDocumentReasonReissue = "reissue"
)

// TaxDocumentDelivery is the mail that hands a buyer an authorized Tax
// Document (#475, ADR 0060): the second mail about a paid House sale, after
// the receipt that promised it.
//
// ONE MESSAGE TYPE FOR EVERY KIND OF DOCUMENT, on purpose. A Sale Invoice
// and a Credit Note differ in what the reader is holding and in nothing
// about how it reaches them — same attachments, same link, same Locale — so
// Kind is a field the copy is keyed on rather than a second message that
// would drift. The Sale Invoice Drainer sends it for whichever document it
// has just seen authorized, and a later ticket's Credit Note rides it
// unchanged.
//
// IT CARRIES THE DOCUMENT ITSELF, which no earlier message does: the SRI's
// rule is that the emisor delivers the XML and the RIDE to the buyer's
// email (#496, ADR 0062), and a link alone would not be delivery. The link
// is the buyer's way back to the same files once the mail is gone.
type TaxDocumentDelivery struct {
	// To is the Recipient's email as the document was issued to it — the
	// Sale's snapshot, never the Customer's current address.
	To string
	// Kind is one of the TaxDocumentKind constants and decides the words.
	Kind string
	// Reason is, on a Credit Note, why it was owed — a reversal route, or
	// TaxDocumentReasonReissue — and decides whether the words say the
	// purchase was reversed or that a corrected factura follows (#481). ""
	// on a Sale Invoice.
	Reason string
	// Supersedes is, on a Sale Invoice, whether a Sale Invoice Reissue
	// produced it (#485, ADR 0061): the ordinary factura words then carry
	// one more line saying it replaces the earlier factura the Credit Note
	// cancelled. False on a first factura and on every Credit Note.
	Supersedes bool
	// CustomerName is the Recipient's legal name as printed on the document.
	CustomerName string
	// EventName and Reference name the purchase the document is about, so
	// the reader can match this mail to the receipt they already hold.
	EventName string
	Reference string
	// CustomerAreaURL points at the Sale's own card in the Customer Area,
	// behind a sign-in. Never a Confirmation Link: that is a bearer
	// credential, and this mail is a document a reader will forward to an
	// accountant.
	CustomerAreaURL string
	// Attachments are the document in both forms the SRI obliges the emisor
	// to hand over (ADR 0062): the signed XML the authority authorized
	// first, then its RIDE, the same document rendered as a PDF. Always
	// both — the Drainer renders the RIDE before it sends and never mails
	// the XML alone (#496).
	Attachments []EmailAttachment
	// Locale is the Sale's language, resolved by the caller through
	// ResolveMailLocale exactly as the receipt's was.
	Locale Locale
}

// CertificateExpiryWarning tells one Platform Operator that the Issuer's
// signing certificate is about to lapse, or has (#502, ADR 0063). One is sent
// per address on the operator allowlist, in that address's Staff Locale, on
// the Payout Request's terms: the allowlist is the whole of operator
// authority (ADR 0015), and the people told are the people who can re-upload.
//
// It is the platform's first mail about its own machinery rather than about a
// sale, and it carries nothing but the machinery: no document counts, no
// buyer, no Sale. What the reader needs is the date, whose certificate it is,
// what lapsing costs, and where the remedy is — and the remedy is one link.
type CertificateExpiryWarning struct {
	To     string
	Locale Locale
	// Threshold is the rung of the ladder this mail is: 30, 7, 1 or 0 days
	// before NotAfter, counted in Ecuadorian calendar days by the Drainer's
	// tick, which sends each rung once per certificate. It decides the
	// subject and the tense: 0 is the day the certificate has lapsed.
	Threshold int
	// NotAfter is the certificate's own expiry instant. It is rendered as the
	// Ecuadorian date it falls on, in either language — the reader counts
	// days from it, and the UTC day can be a day late.
	NotAfter time.Time
	// RUC is the Issuer's Tax ID, so a reader with more than one .p12 on a
	// desk knows which one this is about.
	RUC string
	// IssuerURL is the Issuer page on the staff application, where the
	// renewed .p12 is uploaded.
	IssuerURL string
}

// EmailSender delivers transactional email: staff one-time passcodes,
// Customer Sale Confirmations, Sale void/cancellation notices, the notice
// that a refund the Customer was told was being processed could not be made, and
// the five Payout Request notices. A real provider is deferred; development and
// tests use the logging and capture implementations below.
type EmailSender interface {
	// SendOTP delivers a One-time Passcode in the named language.
	//
	// One method serves both doors rather than two serving one each: the message
	// is identical and only its language differs, and one argument states that
	// policy more plainly than two copies of the same two sentences would.
	//
	// The two doors resolve that language differently, and the argument is what
	// lets them: a Customer's comes from the Storefront page they asked on, and a
	// Member's from the Staff Locale stored against the address the code is going
	// to (#285, ADR 0041). Neither is this interface's business.
	SendOTP(ctx context.Context, to string, code string, locale Locale) error
	SendSaleConfirmation(ctx context.Context, confirmation SaleConfirmation) error
	SendSaleVoided(ctx context.Context, voided SaleVoided) error
	SendSaleReversalRefused(ctx context.Context, refused SaleReversalRefused) error
	// SendTaxDocumentDelivery hands a buyer an authorized Tax Document
	// (#475, ADR 0060): the signed XML and its RIDE attached (#496, ADR
	// 0062), and a link to the Sale.
	//
	// TRANSACTIONAL, beside the receipt that promised it: it is the document
	// the law obliges the seller to deliver, sent to somebody who bought
	// something, and no consent state is anywhere near it. The Drainer
	// records delivered_at only after this returns nil, and retries on its
	// own ladder when it does not.
	SendTaxDocumentDelivery(ctx context.Context, delivery TaxDocumentDelivery) error
	// SendConsentWithdrawalConfirmation delivers the confirmation of a Consent
	// Withdrawal that actually took something away (#267). It is on the
	// transactional half of this interface deliberately: it is sent to somebody
	// who has just asked to stop receiving marketing, and is the one message that
	// must arrive anyway.
	SendConsentWithdrawalConfirmation(ctx context.Context, confirmation ConsentWithdrawalConfirmation) error
	// SendHolderAnswerReminder delivers the Answer Reminder to the Holder of a
	// Ticket that still owes an Answer (#328, ADR 0046; ADR 0049).
	//
	// It is on the TRANSACTIONAL half of this interface, beside the receipt whose
	// one extra sentence it repeats at length, and that placement is the decision
	// rather than a filing choice: it means the message is not reachable from the
	// marketing sending identity at all (ADR 0030), and that no consent state is
	// anywhere near the code that sends it — this reader accepted a ticket and
	// consented to nothing. What bounds it is catalog.MayRemind, which is the
	// only thing that does.
	SendHolderAnswerReminder(ctx context.Context, reminder HolderAnswerReminder) error
	// SendAssignmentReminder delivers the Assignment Reminder to the buyer of a
	// Ticket Sale with Tickets still nobody's (#362, ADR 0051).
	//
	// On the TRANSACTIONAL half for the Answer Reminder's reason: it is about
	// the reader's own purchase, no consent state is anywhere near the code
	// that sends it, and it is not reachable from the marketing identity. What
	// bounds it is catalog.MayRemindAssignment, and the ledger the sweep writes
	// only after this returns nil.
	SendAssignmentReminder(ctx context.Context, reminder AssignmentReminder) error
	// SendTicketAssignment delivers the Assignment mail carrying an Assignment
	// Link (#325, ADR 0046).
	//
	// TRANSACTIONAL, beside the Answer Reminder, and the placement is load
	// bearing twice over. It means no consent state is anywhere near the code
	// that sends it — which is right, because the recipient has consented to
	// nothing and could not have, having never been here — and it means the
	// message cannot be reached from the marketing sending identity at all.
	//
	// It is also the only method on this interface that carries a credential
	// capable of MINTING AN IDENTITY. Anything that widens where TicketAssignment
	// values travel is widening where that credential travels.
	SendTicketAssignment(ctx context.Context, assignment TicketAssignment) error
	// SendNoLongerHolding delivers the one mail an accepted Holder gets when a
	// Ticket stops being theirs (#327, ADR 0046).
	//
	// TRANSACTIONAL, beside the Assignment mail whose reader it is written to
	// second. The recipient granted no consent by accepting a ticket, so there is
	// none to consult and none is consulted; and being told that a ticket is gone
	// must not be suppressible by a marketing preference.
	//
	// ONE METHOD FOR BOTH CAUSES, which is this interface's share of the rule.
	// Two methods — one for a reassignment, one for a reversal — would be two
	// places for the copy to drift apart, and the whole point is that the reader
	// cannot tell which happened.
	SendNoLongerHolding(ctx context.Context, notice NoLongerHolding) error
	// SendSaleReAddressing delivers the mail carrying a Re-addressing Link to
	// the corrected address of a Sale Re-addressing (#420, ADR 0058).
	//
	// TRANSACTIONAL, beside the Assignment mail whose reader it resembles: a
	// person who may never have been here, who consented to nothing, and who is
	// being handed the one link that lets them prove an address. Its placement
	// on this half is what makes that structural.
	SendSaleReAddressing(ctx context.Context, reAddressing SaleReAddressing) error
	// SendTicketQuestionRevoked tells one Org Admin that an approved Ticket
	// Question was revoked, and why (#410, ADR 0056). Transactional, on the
	// staff-to-organizer channel beside the Payout Request notices.
	SendTicketQuestionRevoked(ctx context.Context, revoked TicketQuestionRevoked) error
	SendPayoutRequestSubmitted(ctx context.Context, submitted PayoutRequestSubmitted) error
	// SendQuestionReviewSubmitted tells one Platform Operator that an Event's
	// questions are waiting for review (#406, ADR 0056).
	SendQuestionReviewSubmitted(ctx context.Context, submitted QuestionReviewSubmitted) error
	// SendQuestionReviewAnswered tells the submitter of a Question Review the
	// Operator's verdict on each item (#407, ADR 0056).
	SendQuestionReviewAnswered(ctx context.Context, answered QuestionReviewAnswered) error
	SendPayoutRequestPaid(ctx context.Context, paid PayoutRequestPaid) error
	SendPayoutRequestDeclined(ctx context.Context, declined PayoutRequestDeclined) error
	SendPayoutRequestTransferSent(ctx context.Context, sent PayoutRequestTransferSent) error
	SendPayoutRequestTransferFailed(ctx context.Context, failed PayoutRequestTransferFailed) error
	// SendCertificateExpiryWarning tells one Platform Operator that the
	// Issuer's signing certificate is about to lapse, or has (#502, ADR 0063).
	//
	// TRANSACTIONAL, beside the Payout Request notices on the staff channel
	// and never the Digest's: it is about the platform's own ability to sign
	// what the law obliges it to sign, and no consent state is anywhere near
	// it. What bounds it is the Drainer's ledger of sends, written only after
	// at least one address accepted the mail.
	SendCertificateExpiryWarning(ctx context.Context, warning CertificateExpiryWarning) error
	// SendFollowDigest delivers the one weekly Follow Digest. The only
	// non-transactional message on this interface, and the only one whose
	// language depends on its reader.
	SendFollowDigest(ctx context.Context, digest FollowDigest) error
}

// LoggingEmailSender logs email delivery to the configured logger (development use).
type LoggingEmailSender struct {
	Logger Logger
}

// SendOTP logs the OTP code for local development and testing. The language is
// logged beside it because it is the one thing about a passcode that can now be
// wrong without the code being wrong.
func (s *LoggingEmailSender) SendOTP(_ context.Context, to string, code string, locale Locale) error {
	s.Logger.Info("otp sent", "email", to, "code", code, "locale", string(locale))
	return nil
}

// SendSaleConfirmation logs the Sale Confirmation for local development and testing.
// The Confirmation Link is logged alongside the reference for the same reason
// the OTP code is: locally there is no mailbox, and the link is the whole point
// of the email.
func (s *LoggingEmailSender) SendSaleConfirmation(_ context.Context, c SaleConfirmation) error {
	s.Logger.Info("sale confirmation sent", "email", c.To, "reference", c.Reference, "event", c.EventName, "amount_cents", c.AmountCents, "confirmation_link", c.ConfirmationLink)
	return nil
}

// SendSaleVoided logs the Sale void notice for local development and testing.
func (s *LoggingEmailSender) SendSaleVoided(_ context.Context, v SaleVoided) error {
	s.Logger.Info("sale voided notice sent", "email", v.To, "reference", v.Reference, "event", v.EventName)
	return nil
}

// SendTaxDocumentDelivery logs the Tax Document delivery for local
// development: the reference, the kind and each attachment's filename and
// size, never its bytes.
func (s *LoggingEmailSender) SendTaxDocumentDelivery(_ context.Context, d TaxDocumentDelivery) error {
	attrs := []any{"email", d.To, "kind", d.Kind, "reference", d.Reference, "event", d.EventName}
	for _, a := range d.Attachments {
		attrs = append(attrs, "attachment", a.Filename, "attachment_bytes", len(a.Body))
	}
	attrs = append(attrs, "customer_area", d.CustomerAreaURL)
	s.Logger.Info("tax document delivery sent", attrs...)
	return nil
}

// SendSaleReversalRefused logs the refused-reversal notice for local development
// and testing.
func (s *LoggingEmailSender) SendSaleReversalRefused(_ context.Context, r SaleReversalRefused) error {
	s.Logger.Info("sale reversal refused notice sent", "email", r.To, "reference", r.Reference, "event", r.EventName)
	return nil
}

// SendConsentWithdrawalConfirmation logs the Consent Withdrawal confirmation for
// local development. The address and the language are logged and nothing else:
// what a local developer needs from this line is that somebody was told, and in
// which language — the rest of the message is the same two paragraphs every
// time.
func (s *LoggingEmailSender) SendConsentWithdrawalConfirmation(_ context.Context, c ConsentWithdrawalConfirmation) error {
	s.Logger.Info("consent withdrawal confirmation sent", "email", c.To, "locale", string(c.Locale))
	return nil
}

// SendHolderAnswerReminder logs the Answer Reminder for local development. The
// address, how many Tickets and the language are logged and the body is not:
// what a local developer needs to see is that a Holder was chased, about how
// much, and in which language.
func (s *LoggingEmailSender) SendHolderAnswerReminder(_ context.Context, r HolderAnswerReminder) error {
	s.Logger.Info("holder answer reminder sent", "email", r.To, "tickets", len(r.Tickets), "locale", string(r.Locale))
	return nil
}

// SendAssignmentReminder logs the Assignment Reminder for local development:
// the address, the tally and the language, and not the body or the link — the
// Confirmation Link opens the Sale without a sign-in and the receipt's own
// logging keeps it out of the log for the same reason.
func (s *LoggingEmailSender) SendAssignmentReminder(_ context.Context, r AssignmentReminder) error {
	s.Logger.Info("assignment reminder sent", "email", r.To,
		"unassigned", r.UnassignedTickets, "total", r.TotalTickets, "locale", string(r.Locale))
	return nil
}

// SendTicketAssignment logs the Assignment mail for local development. The
// address and the link are logged, because locally there is no mailbox and the
// link is the whole point of the message — the same reason the OTP code is
// logged. Nothing about the buyer is logged, because nothing about the buyer is
// in the message.
func (s *LoggingEmailSender) SendTicketAssignment(_ context.Context, a TicketAssignment) error {
	s.Logger.Info("ticket assignment sent", "email", a.To, "event", a.EventName, "accept_url", a.AcceptURL, "locale", string(a.Locale))
	return nil
}

// SendNoLongerHolding logs the No Longer Holding notice for local development.
// The address and the Event are logged and there is nothing else in the message
// to log: it carries no link, no cause and nothing about the buyer.
func (s *LoggingEmailSender) SendNoLongerHolding(_ context.Context, n NoLongerHolding) error {
	s.Logger.Info("no longer holding notice sent", "email", n.To, "event", n.EventName, "locale", string(n.Locale))
	return nil
}

// SendSaleReAddressing logs the Re-addressing mail for local development. The
// address and the link are logged, as the Assignment mail's are, because locally
// there is no mailbox and the link is the whole point of the message.
func (s *LoggingEmailSender) SendSaleReAddressing(_ context.Context, r SaleReAddressing) error {
	s.Logger.Info("sale re-addressing sent", "email", r.To, "event", r.EventName, "reference", r.Reference, "accept_url", r.AcceptURL, "locale", string(r.Locale))
	return nil
}

// SendTicketQuestionRevoked logs the Revocation notice. The reason is not
// logged: it is the Operator's message to one Organization.
func (s *LoggingEmailSender) SendTicketQuestionRevoked(_ context.Context, r TicketQuestionRevoked) error {
	s.Logger.Info("ticket question revoked notice sent", "email", r.To, "organization", r.OrganizationName)
	return nil
}

// SendPayoutRequestSubmitted logs the operator's notice that an Organization
// asked to be paid. The amount and the asker are logged; nothing about the bank
// is, here or anywhere else (ADR 0026).
func (s *LoggingEmailSender) SendPayoutRequestSubmitted(_ context.Context, p PayoutRequestSubmitted) error {
	s.Logger.Info("payout request submitted notice sent", "email", p.To, "organization", p.OrganizationName, "amount_cents", p.AmountCents, "requested_by", p.RequestedBy)
	return nil
}

// SendQuestionReviewSubmitted logs the Operator's notice that an Event's
// questions are waiting for review. The count is logged; no question is.
func (s *LoggingEmailSender) SendQuestionReviewSubmitted(_ context.Context, q QuestionReviewSubmitted) error {
	s.Logger.Info("question review submitted notice sent", "email", q.To, "organization", q.OrganizationName, "event", q.EventName, "question_count", q.QuestionCount, "submitted_by", q.SubmittedBy)
	return nil
}

// SendQuestionReviewAnswered logs the submitter's notice of the verdicts. The
// count is logged; no question is.
func (s *LoggingEmailSender) SendQuestionReviewAnswered(_ context.Context, q QuestionReviewAnswered) error {
	s.Logger.Info("question review answered notice sent", "email", q.To, "organization", q.OrganizationName, "event", q.EventName, "item_count", len(q.Items), "answered_by", q.AnsweredBy)
	return nil
}

// SendPayoutRequestPaid logs the asker's notice that the transfer was made.
func (s *LoggingEmailSender) SendPayoutRequestPaid(_ context.Context, p PayoutRequestPaid) error {
	s.Logger.Info("payout request paid notice sent", "email", p.To, "organization", p.OrganizationName, "amount_cents", p.AmountCents, "requested_cents", p.RequestedCents)
	return nil
}

// SendPayoutRequestDeclined logs the asker's notice that the ask was refused.
// The reason is not logged: it is the operator's message to one Organization,
// and a log is the wrong audience for it.
func (s *LoggingEmailSender) SendPayoutRequestDeclined(_ context.Context, p PayoutRequestDeclined) error {
	s.Logger.Info("payout request declined notice sent", "email", p.To, "organization", p.OrganizationName, "amount_cents", p.AmountCents)
	return nil
}

// SendPayoutRequestTransferSent logs the asker's notice that the transfer has
// been submitted to the bank.
func (s *LoggingEmailSender) SendPayoutRequestTransferSent(_ context.Context, p PayoutRequestTransferSent) error {
	s.Logger.Info("payout request transfer sent notice sent", "email", p.To, "organization", p.OrganizationName, "amount_cents", p.AmountCents)
	return nil
}

// SendPayoutRequestTransferFailed logs the asker's notice that the bank sent the
// transfer back. The reason is not logged, for the reason the decline's is not:
// it is the operator's message to one Organization.
func (s *LoggingEmailSender) SendPayoutRequestTransferFailed(_ context.Context, p PayoutRequestTransferFailed) error {
	s.Logger.Info("payout request transfer failed notice sent", "email", p.To, "organization", p.OrganizationName, "amount_cents", p.AmountCents)
	return nil
}

// SendCertificateExpiryWarning logs the Operator's notice that the signing
// certificate is about to lapse, or has. The threshold, the RUC and the
// date are logged: locally there is no mailbox, and the ladder's rung is
// the whole of what a developer watching a closed-flag tick wants to see.
func (s *LoggingEmailSender) SendCertificateExpiryWarning(_ context.Context, w CertificateExpiryWarning) error {
	s.Logger.Info("certificate expiry warning sent", "email", w.To, "threshold_days", w.Threshold, "ruc", w.RUC, "not_after", w.NotAfter, "locale", string(w.Locale))
	return nil
}

// SendFollowDigest logs the weekly Follow Digest for local development. The
// Event count and the Locale are logged rather than the Events themselves: what
// a local developer needs from this line is that a Digest went out, to whom, in
// which language, and that it was not empty.
func (s *LoggingEmailSender) SendFollowDigest(_ context.Context, d FollowDigest) error {
	s.Logger.Info("follow digest sent", "email", d.To, "locale", string(d.Locale), "new", len(d.New), "happening", len(d.Happening))
	return nil
}

// NoopEmailSender discards delivery (tests).
type NoopEmailSender struct{}

// SendOTP discards the OTP.
func (NoopEmailSender) SendOTP(_ context.Context, _ string, _ string, _ Locale) error {
	return nil
}

// SendSaleConfirmation discards the confirmation.
func (NoopEmailSender) SendSaleConfirmation(_ context.Context, _ SaleConfirmation) error {
	return nil
}

// SendSaleVoided discards the void notice.
func (NoopEmailSender) SendSaleVoided(_ context.Context, _ SaleVoided) error {
	return nil
}

// SendSaleReversalRefused discards the refused-reversal notice.
func (NoopEmailSender) SendSaleReversalRefused(_ context.Context, _ SaleReversalRefused) error {
	return nil
}

// SendTaxDocumentDelivery does nothing.
func (NoopEmailSender) SendTaxDocumentDelivery(_ context.Context, _ TaxDocumentDelivery) error {
	return nil
}

// SendConsentWithdrawalConfirmation discards the Consent Withdrawal confirmation.
func (NoopEmailSender) SendConsentWithdrawalConfirmation(_ context.Context, _ ConsentWithdrawalConfirmation) error {
	return nil
}

// SendHolderAnswerReminder discards the Holder's Answer Reminder.
func (NoopEmailSender) SendHolderAnswerReminder(_ context.Context, _ HolderAnswerReminder) error {
	return nil
}

// SendTicketAssignment discards the Assignment mail.
// SendAssignmentReminder discards the Assignment Reminder.
func (NoopEmailSender) SendAssignmentReminder(_ context.Context, _ AssignmentReminder) error {
	return nil
}

func (NoopEmailSender) SendTicketAssignment(_ context.Context, _ TicketAssignment) error {
	return nil
}

// SendNoLongerHolding discards the No Longer Holding notice.
func (NoopEmailSender) SendNoLongerHolding(_ context.Context, _ NoLongerHolding) error {
	return nil
}

// SendSaleReAddressing discards the Re-addressing mail.
func (NoopEmailSender) SendSaleReAddressing(_ context.Context, _ SaleReAddressing) error {
	return nil
}

// SendTicketQuestionRevoked discards the Revocation notice.
func (NoopEmailSender) SendTicketQuestionRevoked(_ context.Context, _ TicketQuestionRevoked) error {
	return nil
}

// SendPayoutRequestSubmitted discards the operator's submission notice.
func (NoopEmailSender) SendPayoutRequestSubmitted(_ context.Context, _ PayoutRequestSubmitted) error {
	return nil
}

// SendQuestionReviewSubmitted discards the Operator's review notice.
func (NoopEmailSender) SendQuestionReviewSubmitted(_ context.Context, _ QuestionReviewSubmitted) error {
	return nil
}

// SendQuestionReviewAnswered discards the submitter's verdict notice.
func (NoopEmailSender) SendQuestionReviewAnswered(_ context.Context, _ QuestionReviewAnswered) error {
	return nil
}

// SendPayoutRequestPaid discards the asker's paid notice.
func (NoopEmailSender) SendPayoutRequestPaid(_ context.Context, _ PayoutRequestPaid) error {
	return nil
}

// SendPayoutRequestDeclined discards the asker's decline notice.
func (NoopEmailSender) SendPayoutRequestDeclined(_ context.Context, _ PayoutRequestDeclined) error {
	return nil
}

// SendPayoutRequestTransferSent discards the asker's transfer-sent notice.
func (NoopEmailSender) SendPayoutRequestTransferSent(_ context.Context, _ PayoutRequestTransferSent) error {
	return nil
}

// SendPayoutRequestTransferFailed discards the asker's transfer-failed notice.
func (NoopEmailSender) SendPayoutRequestTransferFailed(_ context.Context, _ PayoutRequestTransferFailed) error {
	return nil
}

// SendCertificateExpiryWarning discards the Operator's certificate notice.
func (NoopEmailSender) SendCertificateExpiryWarning(_ context.Context, _ CertificateExpiryWarning) error {
	return nil
}

// SendFollowDigest discards the weekly Follow Digest.
func (NoopEmailSender) SendFollowDigest(_ context.Context, _ FollowDigest) error {
	return nil
}

// CaptureEmailSender records delivered email for integration tests. It is safe
// for concurrent use so tests can exercise concurrent sales.
type CaptureEmailSender struct {
	mu       sync.Mutex
	LastTo   string
	LastCode string
	otpSends int
	// The One-time Passcodes delivered (#244), kept whole so a test can render
	// Subject() and Text() and assert on the words a recipient reads. LastTo and
	// LastCode above answer "did a passcode go out, and what was it"; this
	// answers "what did it say", which is the only way the language is visible
	// at all.
	OTPMessages       []OTPMessage
	SaleConfirmations []SaleConfirmation
	VoidedSales       []SaleVoided
	RefusedReversals  []SaleReversalRefused
	// The Tax Document deliveries (#475, ADR 0060). Kept whole so a test can
	// render Subject() and Text() in the Sale Locale and read the attached
	// bytes against what the fake SRI received; asserted on by LENGTH as much
	// as by contents, since "sent exactly once" and "nothing was sent while
	// the sender was down" are facts about how many there are.
	TaxDocumentDeliveries []TaxDocumentDelivery
	// The Consent Withdrawal confirmations (#267), kept whole so a test can
	// render Subject() and Text() and assert on the words the recipient reads —
	// which is the only way the language, and the promise the copy is forbidden
	// from making, are visible at all.
	//
	// Tests assert on the LENGTH as much as on the contents: the criterion that
	// an act which moved nothing sends nothing cannot be told from a message's
	// contents, only from there being none.
	WithdrawalConfirmations []ConsentWithdrawalConfirmation
	// The Answer Reminders delivered (#317, #328, ADR 0049). Kept whole rather
	// than as rendered strings, so a test can call Subject() and Text() itself —
	// which is the only way the Mail Locale is visible at all — and can assert
	// on WHO was written to, which is what every rationing test is really about.
	//
	// Tests assert on the LENGTH as much as on the contents: "this Holder was
	// not mailed a second time inside the week", and "the buyer was not chased
	// about a Ticket they do not hold", cannot be told from any message's
	// contents, only from there being none.
	HolderAnswerReminders []HolderAnswerReminder
	// The Assignment mails delivered (#325). Kept whole rather than as rendered
	// strings, for the reason the Answer Reminders above are: a test calls
	// Subject() and Text() itself, which is the only way the Mail Locale and the
	// copy's promises are visible at all.
	//
	// It is also the ONLY way an integration test can see an Assignment Link. The
	// token is never on a buyer surface and never in an API response — that is
	// the security property of the whole feature — so the captured mail is the
	// one place a test can get one, exactly as a Holder's inbox is the one place
	// a person can.
	TicketAssignments []TicketAssignment
	// The Re-addressing mails delivered (#420, ADR 0058). Kept whole so a test
	// can render what the corrected address read, and — as with the Assignment
	// mail — the ONLY way an integration test can see a Re-addressing Link: the
	// token is in no Operator response, so the captured mail is the one place a
	// test can get one, exactly as an inbox is the one place a person can.
	SaleReAddressings []SaleReAddressing
	// The Assignment Reminders delivered (#362, ADR 0051). Kept whole so a test
	// can render what the buyer read; asserted on by LENGTH as much as by
	// contents, since "nobody was reminded" has no message to inspect.
	AssignmentReminders []AssignmentReminder
	// The No Longer Holding notices delivered (#327). Kept whole, so a test can
	// render Subject() and Text() itself and read what the Holder read — which is
	// the only way "it gives no cause and names no buyer" can be asserted at all.
	//
	// Its LENGTH carries as much of this ticket as its contents do: "an assigned
	// Holder who never accepted is mailed in NEITHER case" and "the Holder is
	// told exactly once" are both facts about how many of these exist, and
	// neither is visible in any message's words.
	NoLongerHoldings []NoLongerHolding
	// The Revocation notices delivered (#410, ADR 0056). Kept whole so a test
	// can render Subject() and Text() itself, which is the only way the Mail
	// Locale is visible; asserted on by LENGTH as much as by contents, since
	// "every Org Admin and nobody else" is a fact about how many there are.
	RevokedTicketQuestions []TicketQuestionRevoked
	// The five Payout Request notices (#179, #188).
	SubmittedPayoutRequests []PayoutRequestSubmitted
	// The Question Review submission notices (#406, ADR 0056), one per
	// allowlisted Operator, kept whole so a test can render each in its
	// recipient's Staff Locale.
	SubmittedQuestionReviews []QuestionReviewSubmitted
	// The Question Review verdict notices (#407, ADR 0056), one per answered
	// Review, to its submitter.
	AnsweredQuestionReviews    []QuestionReviewAnswered
	PaidPayoutRequests         []PayoutRequestPaid
	DeclinedPayoutRequests     []PayoutRequestDeclined
	TransferSentPayoutRequests []PayoutRequestTransferSent
	FailedPayoutRequests       []PayoutRequestTransferFailed
	// The Certificate Expiry Warnings (#502, ADR 0063), one per allowlisted
	// Operator per rung of the ladder, kept whole so a test can render each
	// in its recipient's Staff Locale; asserted on by LENGTH as much as by
	// contents, since "once per threshold" is a fact about how many there are.
	CertificateExpiryWarnings []CertificateExpiryWarning
	// The weekly Follow Digests (#220). Kept as the whole value rather than as a
	// rendered string, so a test can assert on the message a Customer would read
	// by calling Subject() and Text() itself — which is what makes the Digest
	// Locale assertable at all: the language is only visible once the message is
	// rendered.
	FollowDigests []FollowDigest
	// failure, when set, makes every send fail with it and record nothing —
	// a provider outage, as the calling code would meet one.
	//
	// It exists for the one property that cannot be observed any other way: a
	// notice whose delivery fails must never block or roll back the money record
	// it accompanies (ADR 0026). Asserting that with a sender that always
	// succeeds would assert nothing. Recording nothing is the honest model of a
	// failed send, and it is what lets a test say "nobody was told, and the
	// Payout stands anyway".
	//
	// It applies to EVERY message, including the staff passcode, which is the one
	// email in this system that is not best-effort — a test that switches this on
	// must already hold the sessions it needs. Cleared by Reset, so a failure
	// never leaks into the next test.
	failure error
	// delay, when set, makes the Tax Document delivery take that long before
	// it is recorded — a slow provider, as the calling code would meet one.
	//
	// It exists for one property: that a document is mailed ONCE when two
	// Sale Invoice Drainer rounds overlap (#475). The window in which a
	// second round could claim a document the first is still mailing is a
	// few milliseconds wide against a sender that answers at once, and a
	// test that cannot hold it open cannot prove it is closed. Cleared by
	// Reset.
	delay time.Duration
}

// SlowTaxDocumentDeliveryBy makes every subsequent Tax Document delivery
// take d before it is recorded, or restores instant delivery when d is 0.
func (s *CaptureEmailSender) SlowTaxDocumentDeliveryBy(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

// FailWith makes every subsequent send fail with err, or restores ordinary
// delivery when err is nil.
func (s *CaptureEmailSender) FailWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = err
}

// failed reports the injected failure, if any, and is the first line of every
// send below.
func (s *CaptureEmailSender) failed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failure
}

// SendOTP records the last OTP delivered, and the message it was delivered in.
func (s *CaptureEmailSender) SendOTP(_ context.Context, to string, code string, locale Locale) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastTo = to
	s.LastCode = code
	s.otpSends++
	s.OTPMessages = append(s.OTPMessages, OTPMessage{Code: code, Locale: locale})
	return nil
}

// SendSaleConfirmation records a delivered Sale Confirmation.
func (s *CaptureEmailSender) SendSaleConfirmation(_ context.Context, c SaleConfirmation) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SaleConfirmations = append(s.SaleConfirmations, c)
	return nil
}

// SendSaleVoided records a delivered Sale void/cancellation notice.
func (s *CaptureEmailSender) SendSaleVoided(_ context.Context, v SaleVoided) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VoidedSales = append(s.VoidedSales, v)
	return nil
}

// SendTaxDocumentDelivery records a delivered Tax Document.
func (s *CaptureEmailSender) SendTaxDocumentDelivery(_ context.Context, d TaxDocumentDelivery) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	delay := s.delay
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TaxDocumentDeliveries = append(s.TaxDocumentDeliveries, d)
	return nil
}

// SendSaleReversalRefused records a delivered refused-reversal notice.
func (s *CaptureEmailSender) SendSaleReversalRefused(_ context.Context, r SaleReversalRefused) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RefusedReversals = append(s.RefusedReversals, r)
	return nil
}

// SendConsentWithdrawalConfirmation records a delivered Consent Withdrawal
// confirmation.
func (s *CaptureEmailSender) SendConsentWithdrawalConfirmation(_ context.Context, c ConsentWithdrawalConfirmation) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.WithdrawalConfirmations = append(s.WithdrawalConfirmations, c)
	return nil
}

// SendHolderAnswerReminder records a delivered Answer Reminder.
func (s *CaptureEmailSender) SendHolderAnswerReminder(_ context.Context, r HolderAnswerReminder) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.HolderAnswerReminders = append(s.HolderAnswerReminders, r)
	return nil
}

// SendAssignmentReminder records a delivered Assignment Reminder.
func (s *CaptureEmailSender) SendAssignmentReminder(_ context.Context, r AssignmentReminder) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AssignmentReminders = append(s.AssignmentReminders, r)
	return nil
}

// SendTicketAssignment records a delivered Assignment mail.
func (s *CaptureEmailSender) SendTicketAssignment(_ context.Context, a TicketAssignment) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TicketAssignments = append(s.TicketAssignments, a)
	return nil
}

// SendNoLongerHolding records a delivered No Longer Holding notice.
func (s *CaptureEmailSender) SendNoLongerHolding(_ context.Context, n NoLongerHolding) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NoLongerHoldings = append(s.NoLongerHoldings, n)
	return nil
}

// SendSaleReAddressing records a delivered Re-addressing mail.
func (s *CaptureEmailSender) SendSaleReAddressing(_ context.Context, r SaleReAddressing) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SaleReAddressings = append(s.SaleReAddressings, r)
	return nil
}

// SendPayoutRequestSubmitted records a delivered operator submission notice.
func (s *CaptureEmailSender) SendPayoutRequestSubmitted(_ context.Context, p PayoutRequestSubmitted) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SubmittedPayoutRequests = append(s.SubmittedPayoutRequests, p)
	return nil
}

// SendPayoutRequestPaid records a delivered paid notice.
func (s *CaptureEmailSender) SendPayoutRequestPaid(_ context.Context, p PayoutRequestPaid) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PaidPayoutRequests = append(s.PaidPayoutRequests, p)
	return nil
}

// SendTicketQuestionRevoked records a delivered Revocation notice.
func (s *CaptureEmailSender) SendTicketQuestionRevoked(_ context.Context, r TicketQuestionRevoked) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RevokedTicketQuestions = append(s.RevokedTicketQuestions, r)
	return nil
}

// SendPayoutRequestDeclined records a delivered decline notice.
func (s *CaptureEmailSender) SendPayoutRequestDeclined(_ context.Context, p PayoutRequestDeclined) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DeclinedPayoutRequests = append(s.DeclinedPayoutRequests, p)
	return nil
}

// SendPayoutRequestTransferSent records a delivered transfer-sent notice.
func (s *CaptureEmailSender) SendPayoutRequestTransferSent(_ context.Context, p PayoutRequestTransferSent) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TransferSentPayoutRequests = append(s.TransferSentPayoutRequests, p)
	return nil
}

// SendPayoutRequestTransferFailed records a delivered transfer-failed notice.
func (s *CaptureEmailSender) SendPayoutRequestTransferFailed(_ context.Context, p PayoutRequestTransferFailed) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FailedPayoutRequests = append(s.FailedPayoutRequests, p)
	return nil
}

// SendCertificateExpiryWarning records a delivered Certificate Expiry Warning.
func (s *CaptureEmailSender) SendCertificateExpiryWarning(_ context.Context, w CertificateExpiryWarning) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CertificateExpiryWarnings = append(s.CertificateExpiryWarnings, w)
	return nil
}

// SendFollowDigest records a delivered weekly Follow Digest.
func (s *CaptureEmailSender) SendFollowDigest(_ context.Context, d FollowDigest) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FollowDigests = append(s.FollowDigests, d)
	return nil
}

// Confirmations returns a copy of the captured Sale Confirmations.
func (s *CaptureEmailSender) Confirmations() []SaleConfirmation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SaleConfirmation, len(s.SaleConfirmations))
	copy(out, s.SaleConfirmations)
	return out
}

// Voided returns a copy of the captured Sale void/cancellation notices.
func (s *CaptureEmailSender) Voided() []SaleVoided {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SaleVoided, len(s.VoidedSales))
	copy(out, s.VoidedSales)
	return out
}

// RefusedReversalNotices returns a copy of the captured refused-reversal
// notices. Tests assert on its LENGTH as much as on its contents: the notice is
// sent at most once per Reversal Request, and two actors can resolve one.
func (s *CaptureEmailSender) RefusedReversalNotices() []SaleReversalRefused {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SaleReversalRefused, len(s.RefusedReversals))
	copy(out, s.RefusedReversals)
	return out
}

// ConsentWithdrawalConfirmations returns a copy of the captured Consent
// Withdrawal confirmations.
func (s *CaptureEmailSender) ConsentWithdrawalConfirmations() []ConsentWithdrawalConfirmation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConsentWithdrawalConfirmation, len(s.WithdrawalConfirmations))
	copy(out, s.WithdrawalConfirmations)
	return out
}

// HolderAnswerRemindersSent returns the Answer Reminders delivered so far, in
// the order the sweep sent them — which is the order that matters, since the
// job works oldest sale first and a test asserting who got the one available
// slot is asserting on that order.
//
// Tests assert on its LENGTH as much as on its contents: "nobody was chased
// about an unassigned Ticket" cannot be told from any message's contents at
// all.
func (s *CaptureEmailSender) HolderAnswerRemindersSent() []HolderAnswerReminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HolderAnswerReminder, len(s.HolderAnswerReminders))
	copy(out, s.HolderAnswerReminders)
	return out
}

// AssignmentRemindersSent returns the Assignment Reminders delivered so far, in
// the order the sweep sent them — oldest Sale first, which is the order a test
// of the batch limit asserts on.
func (s *CaptureEmailSender) AssignmentRemindersSent() []AssignmentReminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AssignmentReminder, len(s.AssignmentReminders))
	copy(out, s.AssignmentReminders)
	return out
}

// TicketAssignmentsSent returns the Assignment mails delivered so far, in the
// order they were sent.
//
// Tests assert on the LENGTH as much as on the contents. "Re-submitting the same
// address mailed nobody" and "assigning does not mail the buyer" cannot be told
// from any message's contents, only from there being none — and the second of
// those is the acceptance criterion the whole feature rests on.
func (s *CaptureEmailSender) TicketAssignmentsSent() []TicketAssignment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TicketAssignment, len(s.TicketAssignments))
	copy(out, s.TicketAssignments)
	return out
}

// TaxDocumentDeliveriesSent returns the Tax Document deliveries so far, in
// the order they were sent.
func (s *CaptureEmailSender) TaxDocumentDeliveriesSent() []TaxDocumentDelivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TaxDocumentDelivery, len(s.TaxDocumentDeliveries))
	copy(out, s.TaxDocumentDeliveries)
	return out
}

// CertificateExpiryWarningsSent returns the Certificate Expiry Warnings
// delivered so far, in the order they were sent.
func (s *CaptureEmailSender) CertificateExpiryWarningsSent() []CertificateExpiryWarning {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CertificateExpiryWarning, len(s.CertificateExpiryWarnings))
	copy(out, s.CertificateExpiryWarnings)
	return out
}

// SaleReAddressingsSent returns the Re-addressing mails delivered so far, in
// the order they were sent. Tests assert on the LENGTH as much as on the
// contents: "the wrong address was told nothing" and "a refused recording
// mailed nobody" are facts about there being none.
func (s *CaptureEmailSender) SaleReAddressingsSent() []SaleReAddressing {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SaleReAddressing, len(s.SaleReAddressings))
	copy(out, s.SaleReAddressings)
	return out
}

// NoLongerHoldingsSent returns the No Longer Holding notices delivered so far,
// in the order they were sent.
//
// Tests assert on the LENGTH first and the contents second, because the sharpest
// rules in #327 are rules about counting: exactly one notice per displaced
// Holder however the displacement happened, none at all to somebody who was
// merely assigned, and none to the buyer, who keeps their reversed sale.
func (s *CaptureEmailSender) NoLongerHoldingsSent() []NoLongerHolding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]NoLongerHolding, len(s.NoLongerHoldings))
	copy(out, s.NoLongerHoldings)
	return out
}

// PayoutRequestsSubmitted returns a copy of the captured operator submission
// notices. Tests assert on its LENGTH as much as on its contents: one notice per
// allowlisted operator, and none at all when a repeated submission is handed the
// outstanding request back rather than recording a new ask.
func (s *CaptureEmailSender) PayoutRequestsSubmitted() []PayoutRequestSubmitted {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PayoutRequestSubmitted, len(s.SubmittedPayoutRequests))
	copy(out, s.SubmittedPayoutRequests)
	return out
}

// QuestionReviewsSubmitted returns a copy of the captured Question Review
// submission notices, in the order they were sent.
func (s *CaptureEmailSender) QuestionReviewsSubmitted() []QuestionReviewSubmitted {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]QuestionReviewSubmitted, len(s.SubmittedQuestionReviews))
	copy(out, s.SubmittedQuestionReviews)
	return out
}

// SendQuestionReviewSubmitted records a delivered Question Review notice.
func (s *CaptureEmailSender) SendQuestionReviewSubmitted(_ context.Context, q QuestionReviewSubmitted) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SubmittedQuestionReviews = append(s.SubmittedQuestionReviews, q)
	return nil
}

// QuestionReviewsAnswered returns a copy of the captured Question Review
// verdict notices, in the order they were sent.
func (s *CaptureEmailSender) QuestionReviewsAnswered() []QuestionReviewAnswered {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]QuestionReviewAnswered, len(s.AnsweredQuestionReviews))
	copy(out, s.AnsweredQuestionReviews)
	return out
}

// SendQuestionReviewAnswered records a delivered verdict notice.
func (s *CaptureEmailSender) SendQuestionReviewAnswered(_ context.Context, q QuestionReviewAnswered) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AnsweredQuestionReviews = append(s.AnsweredQuestionReviews, q)
	return nil
}

// PayoutRequestsPaid returns a copy of the captured paid notices.
func (s *CaptureEmailSender) PayoutRequestsPaid() []PayoutRequestPaid {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PayoutRequestPaid, len(s.PaidPayoutRequests))
	copy(out, s.PaidPayoutRequests)
	return out
}

// PayoutRequestsDeclined returns a copy of the captured decline notices.
func (s *CaptureEmailSender) PayoutRequestsDeclined() []PayoutRequestDeclined {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PayoutRequestDeclined, len(s.DeclinedPayoutRequests))
	copy(out, s.DeclinedPayoutRequests)
	return out
}

// PayoutRequestTransfersSent returns a copy of the captured transfer-sent
// notices. Tests assert on its LENGTH as much as on its contents: the notice
// belongs to the transition and not to the button, so a compare-and-swap that
// lost its race must add nothing here.
func (s *CaptureEmailSender) PayoutRequestTransfersSent() []PayoutRequestTransferSent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PayoutRequestTransferSent, len(s.TransferSentPayoutRequests))
	copy(out, s.TransferSentPayoutRequests)
	return out
}

// PayoutRequestTransfersFailed returns a copy of the captured transfer-failed
// notices, on the same terms.
func (s *CaptureEmailSender) PayoutRequestTransfersFailed() []PayoutRequestTransferFailed {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PayoutRequestTransferFailed, len(s.FailedPayoutRequests))
	copy(out, s.FailedPayoutRequests)
	return out
}

// FollowDigestsSent returns a copy of the captured weekly Follow Digests.
//
// Tests assert on its LENGTH more than on anything else in this file. Almost
// every rule in the Digest pipeline is a rule about how many emails one person
// gets — exactly one per week, none at all when nothing matched, and still
// exactly one after a failed send and a retry — and none of those can be told
// apart by looking at a message's contents.
func (s *CaptureEmailSender) FollowDigestsSent() []FollowDigest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FollowDigest, len(s.FollowDigests))
	copy(out, s.FollowDigests)
	return out
}

// OTPsSent returns a copy of the captured passcode messages, so a test can
// render one and assert on the language a recipient was written to in.
func (s *CaptureEmailSender) OTPsSent() []OTPMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OTPMessage, len(s.OTPMessages))
	copy(out, s.OTPMessages)
	return out
}

// OTPSendCount returns how many passcode emails were delivered. Tests that care
// about a send being suppressed assert on this rather than on the last code,
// which a refused request leaves untouched either way.
func (s *CaptureEmailSender) OTPSendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.otpSends
}

// Reset clears captured email between tests.
func (s *CaptureEmailSender) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastTo = ""
	s.LastCode = ""
	s.otpSends = 0
	s.OTPMessages = nil
	s.SaleConfirmations = nil
	s.VoidedSales = nil
	s.RefusedReversals = nil
	s.TaxDocumentDeliveries = nil
	s.delay = 0
	s.WithdrawalConfirmations = nil
	s.HolderAnswerReminders = nil
	s.TicketAssignments = nil
	s.AssignmentReminders = nil
	s.SaleReAddressings = nil
	s.NoLongerHoldings = nil
	s.SubmittedPayoutRequests = nil
	s.SubmittedQuestionReviews = nil
	s.AnsweredQuestionReviews = nil
	s.PaidPayoutRequests = nil
	s.DeclinedPayoutRequests = nil
	s.RevokedTicketQuestions = nil
	s.TransferSentPayoutRequests = nil
	s.FailedPayoutRequests = nil
	s.FollowDigests = nil
	s.failure = nil
}
