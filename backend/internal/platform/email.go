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

// AnswerReminder is the mail telling a buyer that Tickets on their Ticket Sale
// still owe Answers, and pointing them back at their sale to give them or to
// pass the per-Ticket links on (#317, ADR 0044).
//
// IT IS ADDRESSED TO THE BUYER BECAUSE THERE IS NOBODY ELSE TO ADDRESS. The
// platform holds no address for a holder and asks for none, which is the
// decision the whole feature is built around: collecting three friends'
// addresses so the platform could write to them is the third-party collection
// problem ADR 0044 exists to avoid. So a Ticket Question added after a sale
// reaches its holder only if the buyer forwards it, and this is the message that
// asks them to.
//
// IT IS TRANSACTIONAL, and this type's place on EmailSender's transactional half
// is what makes that structural rather than remembered. It is about tickets
// somebody bought, on the same footing as the Sale Confirmation that carries the
// same sentence, so nothing anywhere reads Marketing Consent before sending it
// and it is not even reachable from the marketing sending identity (ADR 0030,
// ADR 0034). What bounds it instead is catalog.MayRemind: at most one per Ticket
// Sale per week, at most two ever, and silence once the Event has started. Those
// are the only brakes this message has, because a transactional mail carries no
// unsubscribe footer.
//
// IT IS SWEPT RATHER THAN TRIGGERED. Nothing composes one of these when a
// question is authored — an Organization drafting four questions in ten minutes
// would otherwise mail the same people four times — so the only caller is the
// scheduled job in the sales module.
type AnswerReminder struct {
	// To is the buyer's address, as the Ticket Sale recorded it. Never a
	// holder's: there is no such column and there must never be one.
	To           string
	CustomerName string
	EventName    string
	// Reference is the Sale Confirmation reference, printed so a buyer holding
	// two sales for one Event can tell which of them this is about. It is their
	// own reference for their own purchase, in a mail already addressed to them —
	// unlike the Answer Link page, which must never show it, because that page is
	// built to be forwarded into a group chat and this mail is not.
	Reference string
	// ConfirmationLink opens this one Ticket Sale on the Storefront without
	// signing in — the page where the buyer answers what they know and copies out
	// each Ticket's own Answer Link for whoever will be using it (#315).
	//
	// IT IS THE WHOLE MESSAGE. Unlike the Sale Confirmation, which is worth
	// sending without its link because it carries the reference and the total, a
	// reminder with no link is an instruction its reader cannot follow. The job
	// refuses to send one rather than composing it — see the sales module's
	// SweepAnswerReminders, where a Sale whose link could not be signed is skipped
	// and counted.
	//
	// WHAT IS DELIBERATELY ABSENT IS ANY ANSWER LINK. This mail introduces no URL
	// of its own beyond this one, for the reason the receipt's sentence does not
	// either: an Answer Link is meant to be forwarded and this mail is not.
	// Distribution happens on the page, where the buyer copies one link at a time
	// and decides who gets which.
	ConfirmationLink string
	// Locale is the language this reminder is written in, ALREADY RESOLVED by the
	// caller through platform.ResolveMailLocale (ADR 0033): the Sale Locale, then
	// the Customer's Mail Locale, then English.
	//
	// It is resolved from the SALE first for the reason every mail about a sale
	// is: the language the buyer chose at the moment they bought outranks what
	// their record remembers, and it governs every mail about that sale however
	// long afterwards it is sent. This one is sent longest afterwards of any —
	// weeks, by a job nobody is watching, from no page at all.
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
	// SendConsentWithdrawalConfirmation delivers the confirmation of a Consent
	// Withdrawal that actually took something away (#267). It is on the
	// transactional half of this interface deliberately: it is sent to somebody
	// who has just asked to stop receiving marketing, and is the one message that
	// must arrive anyway.
	SendConsentWithdrawalConfirmation(ctx context.Context, confirmation ConsentWithdrawalConfirmation) error
	// SendAnswerReminder delivers an Answer Reminder to the buyer of a Ticket
	// Sale whose Tickets still owe Answers (#317, ADR 0044).
	//
	// It is on the TRANSACTIONAL half of this interface, beside the receipt whose
	// one extra sentence it repeats at length, and that placement is the decision
	// rather than a filing choice: it means the message is not reachable from the
	// marketing sending identity at all (ADR 0030), and that no consent state is
	// anywhere near the code that sends it. What bounds it is catalog.MayRemind,
	// which is the only thing that does.
	SendAnswerReminder(ctx context.Context, reminder AnswerReminder) error
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
	SendPayoutRequestSubmitted(ctx context.Context, submitted PayoutRequestSubmitted) error
	SendPayoutRequestPaid(ctx context.Context, paid PayoutRequestPaid) error
	SendPayoutRequestDeclined(ctx context.Context, declined PayoutRequestDeclined) error
	SendPayoutRequestTransferSent(ctx context.Context, sent PayoutRequestTransferSent) error
	SendPayoutRequestTransferFailed(ctx context.Context, failed PayoutRequestTransferFailed) error
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

// SendAnswerReminder logs the Answer Reminder for local development. The
// address, the sale's reference and the language are logged and the body is
// not: what a local developer needs to see is that a buyer was chased, about
// which purchase, and in which language.
func (s *LoggingEmailSender) SendAnswerReminder(_ context.Context, r AnswerReminder) error {
	s.Logger.Info("answer reminder sent", "email", r.To, "reference", r.Reference, "locale", string(r.Locale))
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

// SendPayoutRequestSubmitted logs the operator's notice that an Organization
// asked to be paid. The amount and the asker are logged; nothing about the bank
// is, here or anywhere else (ADR 0026).
func (s *LoggingEmailSender) SendPayoutRequestSubmitted(_ context.Context, p PayoutRequestSubmitted) error {
	s.Logger.Info("payout request submitted notice sent", "email", p.To, "organization", p.OrganizationName, "amount_cents", p.AmountCents, "requested_by", p.RequestedBy)
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

// SendConsentWithdrawalConfirmation discards the Consent Withdrawal confirmation.
func (NoopEmailSender) SendConsentWithdrawalConfirmation(_ context.Context, _ ConsentWithdrawalConfirmation) error {
	return nil
}

// SendAnswerReminder discards the Answer Reminder.
func (NoopEmailSender) SendAnswerReminder(_ context.Context, _ AnswerReminder) error {
	return nil
}

// SendTicketAssignment discards the Assignment mail.
func (NoopEmailSender) SendTicketAssignment(_ context.Context, _ TicketAssignment) error {
	return nil
}

// SendPayoutRequestSubmitted discards the operator's submission notice.
func (NoopEmailSender) SendPayoutRequestSubmitted(_ context.Context, _ PayoutRequestSubmitted) error {
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
	// The Consent Withdrawal confirmations (#267), kept whole so a test can
	// render Subject() and Text() and assert on the words the recipient reads —
	// which is the only way the language, and the promise the copy is forbidden
	// from making, are visible at all.
	//
	// Tests assert on the LENGTH as much as on the contents: the criterion that
	// an act which moved nothing sends nothing cannot be told from a message's
	// contents, only from there being none.
	WithdrawalConfirmations []ConsentWithdrawalConfirmation
	// The Answer Reminders delivered (#317). Kept whole rather than as rendered
	// strings, so a test can call Subject() and Text() itself — which is the only
	// way the Mail Locale is visible at all — and can assert on WHO was written
	// to, which is what every rationing test is really about.
	//
	// Tests assert on the LENGTH as much as on the contents: "this buyer was not
	// mailed a second time inside the week" cannot be told from any message's
	// contents, only from there being none.
	AnswerReminders []AnswerReminder
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
	// The five Payout Request notices (#179, #188).
	SubmittedPayoutRequests    []PayoutRequestSubmitted
	PaidPayoutRequests         []PayoutRequestPaid
	DeclinedPayoutRequests     []PayoutRequestDeclined
	TransferSentPayoutRequests []PayoutRequestTransferSent
	FailedPayoutRequests       []PayoutRequestTransferFailed
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

// SendAnswerReminder records a delivered Answer Reminder.
func (s *CaptureEmailSender) SendAnswerReminder(_ context.Context, r AnswerReminder) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AnswerReminders = append(s.AnswerReminders, r)
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

// AnswerRemindersSent returns the Answer Reminders delivered so far, in the
// order the sweep sent them — which is the order that matters, since the job
// works oldest sale first and a test asserting who got the one available slot
// is asserting on that order.
func (s *CaptureEmailSender) AnswerRemindersSent() []AnswerReminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AnswerReminder, len(s.AnswerReminders))
	copy(out, s.AnswerReminders)
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
	s.WithdrawalConfirmations = nil
	s.AnswerReminders = nil
	s.TicketAssignments = nil
	s.SubmittedPayoutRequests = nil
	s.PaidPayoutRequests = nil
	s.DeclinedPayoutRequests = nil
	s.TransferSentPayoutRequests = nil
	s.FailedPayoutRequests = nil
	s.FollowDigests = nil
	s.failure = nil
}
