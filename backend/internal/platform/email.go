package platform

import (
	"context"
	"sync"
	"time"
)

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
	// TaxID is the Tax ID this Ticket Sale was transacted under, printed on the
	// receipt so the buyer can file it against their own expense records
	// (ADR 0016). It is the sale's immutable snapshot, never the Customer's
	// current stored assertion — a profile edit after the fact changes nothing
	// about a receipt already sent. Unset on sales recorded before the feature
	// and on imported sales that never carried one, in which case the receipt
	// simply has no such line.
	TaxID SaleTaxID
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

// PayoutRequestSubmitted tells one Platform Operator that an Organization has
// asked to be paid. One is sent per address on the operator allowlist, which is
// the whole of operator authority (ADR 0015) — there is no role to check and no
// subscription to consult.
//
// It exists because the pending-count badge on the operator navigation only
// works for somebody who already decided to look, and a Friday-evening request
// otherwise waits until Monday.
type PayoutRequestSubmitted struct {
	To               string
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
	To               string
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
	To               string
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
	To               string
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
	To               string
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
// It is also the FIRST MESSAGE THAT BRANCHES ON LANGUAGE, which is why Locale is
// a field here and on nothing else. A Locale is otherwise a property of a page's
// address (ADR 0027) and mail has no address, so the Customer's Digest Locale is
// remembered at sign-in (#216) and carried here.
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
	// Locale is the Customer's Digest Locale and decides which language every
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
// Every string here is ALREADY RENDERED in the reader's Digest Locale by the
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
}

// EmailSender delivers transactional email: staff one-time passcodes,
// Customer Sale Confirmations, Sale void/cancellation notices, the notice
// that a refund the Customer was told was being processed could not be made, and
// the five Payout Request notices. A real provider is deferred; development and
// tests use the logging and capture implementations below.
type EmailSender interface {
	SendOTP(ctx context.Context, to string, code string) error
	SendSaleConfirmation(ctx context.Context, confirmation SaleConfirmation) error
	SendSaleVoided(ctx context.Context, voided SaleVoided) error
	SendSaleReversalRefused(ctx context.Context, refused SaleReversalRefused) error
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

// SendOTP logs the OTP code for local development and testing.
func (s *LoggingEmailSender) SendOTP(_ context.Context, to string, code string) error {
	s.Logger.Info("otp sent", "email", to, "code", code)
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
func (NoopEmailSender) SendOTP(_ context.Context, _ string, _ string) error {
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
	mu                sync.Mutex
	LastTo            string
	LastCode          string
	otpSends          int
	SaleConfirmations []SaleConfirmation
	VoidedSales       []SaleVoided
	RefusedReversals  []SaleReversalRefused
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

// SendOTP records the last OTP delivered.
func (s *CaptureEmailSender) SendOTP(_ context.Context, to string, code string) error {
	if err := s.failed(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastTo = to
	s.LastCode = code
	s.otpSends++
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
	s.SaleConfirmations = nil
	s.VoidedSales = nil
	s.RefusedReversals = nil
	s.SubmittedPayoutRequests = nil
	s.PaidPayoutRequests = nil
	s.DeclinedPayoutRequests = nil
	s.TransferSentPayoutRequests = nil
	s.FailedPayoutRequests = nil
	s.FollowDigests = nil
	s.failure = nil
}
