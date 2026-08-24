package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// resendEndpoint is Resend's single transactional-send endpoint. The whole
// provider is one authenticated JSON POST, which is why this uses net/http
// directly rather than pulling in the Resend SDK for one call.
const resendEndpoint = "https://api.resend.com/emails"

// ResendEmailSender delivers transactional email through Resend's HTTP API.
//
// It is selected over LoggingEmailSender only when RESEND_API_KEY is set, so
// local development and tests — which set no key — keep logging to the console.
// GCP blocks outbound port 25 permanently, so SMTP is not an option from Cloud
// Run; HTTPS is the transport. See ADR 0009.
//
// Delivery is synchronous: a call returns only once Resend has accepted (or
// rejected) the message. Callers decide what a failure means — SendOTP failure
// hard-fails the login because the passcode is a credential, while Sale
// Confirmation and void notices are best-effort and their errors are discarded
// by the caller. This sender only reports the truth.
//
// A failure log names the message type and the provider's error, never the
// recipient. A Resend 429 on 2026-08-23 wrote customer addresses into Cloud
// Logging through these lines (#377); the log must not become a list of
// people. Correlation goes through the caller's neighbouring line, which
// carries the Sale or Ticket id. Successful sends are not logged here at all.
type ResendEmailSender struct {
	apiKey string
	from   string
	client *http.Client
	logger Logger
}

// NewResendEmailSender builds a sender. from is the full RFC 5322 From header,
// e.g. `Multiticketing <noreply@send.multiticketing.com>`; the address must live
// on a domain verified in Resend or delivery is rejected.
func NewResendEmailSender(apiKey, from string, logger Logger) *ResendEmailSender {
	return &ResendEmailSender{
		apiKey: apiKey,
		from:   from,
		// A bounded client: an unresponsive provider must not hang a login
		// request until the Cloud Run request timeout.
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// From reports the RFC 5322 From header every message from this sender carries
// — which is to say, which sending IDENTITY this object is.
//
// It is exported because the platform now runs two Resend senders at once, on
// two different domains (#225, ADR 0030), and "which of the two is this" stops
// being obvious the moment there is more than one. Startup logging and the
// wiring tests both ask.
func (s *ResendEmailSender) From() string { return s.from }

// resendRequest is the subset of Resend's send payload this system uses. Plain
// text only for now: the messages are short, and text/plain sidesteps an HTML
// templating dependency and the deliverability tuning HTML mail invites.
type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

// send performs the one POST every public method funnels through.
func (s *ResendEmailSender) send(ctx context.Context, to, subject, text string) error {
	payload, err := json.Marshal(resendRequest{
		From:    s.from,
		To:      []string{to},
		Subject: subject,
		Text:    text,
	})
	if err != nil {
		return fmt.Errorf("marshal resend request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEndpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read a bounded slice of the body so a provider error is diagnosable in
	// logs without risking an unbounded read on a misbehaving response.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("resend returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// SendOTP delivers a One-time Passcode in the named language. A failure here is
// returned to the caller, which fails the sign-in request rather than pretending
// a code was sent.
//
// The copy is the message's own (email_content.go) and no longer this file's:
// this was the last message whose words lived in the provider, which put them
// out of reach of every test that drives the API (#244).
func (s *ResendEmailSender) SendOTP(ctx context.Context, to string, code string, locale Locale) error {
	message := OTPMessage{Code: code, Locale: locale}
	if err := s.send(ctx, to, message.Subject(), message.Text()); err != nil {
		s.logger.Error("resend send otp failed", "locale", string(locale), "error", err)
		return err
	}
	return nil
}

// SendSaleConfirmation delivers a Customer's receipt. The caller treats this as
// best-effort, so a returned error is logged here for visibility and then
// discarded upstream — a delivery hiccup never reverses a recorded Ticket Sale.
func (s *ResendEmailSender) SendSaleConfirmation(ctx context.Context, c SaleConfirmation) error {
	if err := s.send(ctx, c.To, c.Subject(), c.Text()); err != nil {
		s.logger.Error("resend send sale confirmation failed", "reference", c.Reference, "error", err)
		return err
	}
	return nil
}

// SendSaleVoided delivers a cancellation notice referencing the original Sale
// Confirmation. Best-effort, as above.
func (s *ResendEmailSender) SendSaleVoided(ctx context.Context, v SaleVoided) error {
	if err := s.send(ctx, v.To, v.Subject(), v.Text()); err != nil {
		s.logger.Error("resend send sale voided failed", "reference", v.Reference, "error", err)
		return err
	}
	return nil
}

// SendSaleReversalRefused delivers the notice that a refund the Customer was
// told was being processed could not be made. Best-effort like the notices
// above, and the log line is what is left when it fails: this is the only
// message that ever corrects a promise, so a Customer who never hears is a
// Customer who still believes their refund is coming.
func (s *ResendEmailSender) SendSaleReversalRefused(ctx context.Context, r SaleReversalRefused) error {
	if err := s.send(ctx, r.To, r.Subject(), r.Text()); err != nil {
		s.logger.Error("resend send sale reversal refused failed", "reference", r.Reference, "error", err)
		return err
	}
	return nil
}

// SendConsentWithdrawalConfirmation delivers the confirmation that a Consent
// Withdrawal took something away (#267).
//
// It goes out on the TRANSACTIONAL identity, which is the whole reason this
// method is here rather than on the Digest half: the recipient has just
// withdrawn Marketing Consent, and sending the confirmation of that act from the
// marketing sending domain would be both the wrong domain and a bad joke.
//
// The failure is returned as well as logged, unlike the best-effort notices
// above, because this send has a caller that must know: a Consent Record's
// confirmation-sent stamp is written only where the provider actually took the
// message, and a swallowed error would stamp evidence that nobody was ever sent
// anything. Nothing about that failure reaches the withdrawal, which has already
// committed.
func (s *ResendEmailSender) SendConsentWithdrawalConfirmation(ctx context.Context, c ConsentWithdrawalConfirmation) error {
	if err := s.send(ctx, c.To, c.Subject(), c.Text()); err != nil {
		s.logger.Error("resend send consent withdrawal confirmation failed", "error", err)
		return err
	}
	return nil
}

// SendHolderAnswerReminder delivers the Answer Reminder to the Holder of a
// Ticket that still owes an Answer (#328, ADR 0046; ADR 0049).
//
// The error is returned rather than swallowed, and the sweep that calls this
// depends on it: a reminder is recorded in the ledger only once the provider
// has accepted it, because the ledger is what rations the next one. Reporting a
// failed send as a success would ration a Holder out of a reminder they never
// received, permanently — the cap counts for the life of the Ticket.
//
// It goes out on the TRANSACTIONAL identity and never the Digest one, which
// SplitEmailSender guarantees structurally by embedding this sender rather than
// listing its methods (ADR 0030). That is not a filing preference here: this
// reader accepted a ticket and consented to nothing, so the message must not be
// reachable from the identity that carries marketing at all.
//
// THE EVENT IS NOT LOGGED ON FAILURE, only the address and the error, exactly
// as for the Assignment mail below: a log aggregator is a wider audience than
// an inbox.
func (s *ResendEmailSender) SendHolderAnswerReminder(ctx context.Context, r HolderAnswerReminder) error {
	if err := s.send(ctx, r.To, r.Subject(), r.Text()); err != nil {
		s.logger.Error("resend send holder answer reminder failed", "error", err)
		return err
	}
	return nil
}

// SendAssignmentReminder delivers the Assignment Reminder to the buyer of a
// Ticket Sale with Tickets still nobody's (#362, ADR 0051).
//
// The error is returned rather than swallowed, and the sweep depends on it
// exactly as it does for the Answer Reminder: the ledger row is written only
// once the provider has accepted the mail, because the ledger is what rations
// the next one, and a failed send reported as success would ration a buyer out
// of a reminder they never received — permanently, since the cap is for the
// life of the Sale.
//
// Transactional identity only (ADR 0030). The address and the error are logged
// on failure; the Event and the link are not.
func (s *ResendEmailSender) SendAssignmentReminder(ctx context.Context, r AssignmentReminder) error {
	if err := s.send(ctx, r.To, r.Subject(), r.Text()); err != nil {
		s.logger.Error("resend send assignment reminder failed", "error", err)
		return err
	}
	return nil
}

// SendTicketAssignment delivers the Assignment mail carrying an Assignment Link
// (#325, ADR 0046).
//
// The error is returned rather than swallowed, but the CALLER treats it as best
// effort: a provider hiccup must not undo an assignment the buyer made, because
// the buyer's record of who they gave which ticket to is worth keeping even when
// the mail failed. What the caller does instead is log it — see the catalog
// service's mailTicketAssignment.
//
// It goes out on the TRANSACTIONAL identity and never the Digest one, which
// SplitEmailSender guarantees structurally (ADR 0030). That is not a filing
// preference here: this message is addressed to somebody who has consented to
// nothing, so it must not be reachable from the identity that carries marketing
// at all.
//
// NEITHER THE LINK NOR THE EVENT IS LOGGED ON FAILURE, only the address and the
// error. The link is a credential that mints an identity, and a log aggregator
// is a wider audience than an inbox.
func (s *ResendEmailSender) SendTicketAssignment(ctx context.Context, a TicketAssignment) error {
	if err := s.send(ctx, a.To, a.Subject(), a.Text()); err != nil {
		s.logger.Error("resend send ticket assignment failed", "error", err)
		return err
	}
	return nil
}

// SendNoLongerHolding delivers the notice that a Ticket a Holder accepted is no
// longer theirs (#327, ADR 0046).
//
// The error is returned rather than swallowed, and the CALLER treats it as best
// effort for the reason every other notice in this flow is: the Ticket changed
// hands, or the Sale was reversed, whether or not the mail landed, and re-running
// a reversal to retry an email would be far worse than a missing one. What the
// caller does instead is log it where an operator can count it.
//
// It goes out on the TRANSACTIONAL identity and never the Digest one, which
// SplitEmailSender guarantees structurally (ADR 0030). The recipient consented
// to nothing beyond holding a ticket, so this must not be reachable from the
// identity that carries marketing.
//
// THE EVENT IS NOT LOGGED, only the address and the error. That is stricter than
// the Assignment mail's line above, and deliberately: which Event somebody has
// stopped holding a ticket to is a fact about a person's plans, and a log
// aggregator is a wider audience than an inbox. What an operator needs here is
// that a Holder was not told.
func (s *ResendEmailSender) SendNoLongerHolding(ctx context.Context, n NoLongerHolding) error {
	if err := s.send(ctx, n.To, n.Subject(), n.Text()); err != nil {
		s.logger.Error("resend send no longer holding failed", "error", err)
		return err
	}
	return nil
}

// SendTicketQuestionRevoked delivers one Org Admin's notice that an approved
// Ticket Question was revoked (#410, ADR 0056). Best-effort like the Payout
// Request notices beside it: the question is retired whether or not anybody
// was told, and the staff editor carries the same reason. The reason itself is
// not logged.
func (s *ResendEmailSender) SendTicketQuestionRevoked(ctx context.Context, r TicketQuestionRevoked) error {
	if err := s.send(ctx, r.To, r.Subject(), r.Text()); err != nil {
		s.logger.Error("resend send ticket question revoked failed", "organization", r.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendPayoutRequestSubmitted delivers one Platform Operator's notice that an
// Organization asked to be paid. Best-effort: the request is recorded whether or
// not anybody was told, and the pending count on the operator navigation is the
// backstop when this fails.
//
// No field of the message is logged beyond the recipient and the Organization.
// The amount is not a secret, but the log line's job here is diagnosing a
// delivery failure, and a money email's contents are not part of that.
func (s *ResendEmailSender) SendPayoutRequestSubmitted(ctx context.Context, p PayoutRequestSubmitted) error {
	if err := s.send(ctx, p.To, p.Subject(), p.Text()); err != nil {
		s.logger.Error("resend send payout request submitted failed", "organization", p.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendQuestionReviewSubmitted delivers one Platform Operator's notice that an
// Event's questions are waiting for review (#406, ADR 0056). Best-effort: the
// Review is recorded whether or not anybody was told.
func (s *ResendEmailSender) SendQuestionReviewSubmitted(ctx context.Context, q QuestionReviewSubmitted) error {
	if err := s.send(ctx, q.To, q.Subject(), q.Text()); err != nil {
		s.logger.Error("resend send question review submitted failed", "organization", q.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendPayoutRequestPaid delivers the asker's notice that the transfer was made.
// Best-effort: the Payout is the fact, and a failure here leaves an organizer
// who finds out from their bank instead.
func (s *ResendEmailSender) SendPayoutRequestPaid(ctx context.Context, p PayoutRequestPaid) error {
	if err := s.send(ctx, p.To, p.Subject(), p.Text()); err != nil {
		s.logger.Error("resend send payout request paid failed", "organization", p.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendPayoutRequestDeclined delivers the asker's notice that the ask was
// refused, carrying the operator's reason.
//
// Best-effort like the rest, and the log line is what is left when it fails: a
// decline nobody hears about is a request that appears to have been ignored,
// which is the outcome the reason exists to prevent. The reason itself is not
// logged — it is a message to one Organization, not an operational fact.
func (s *ResendEmailSender) SendPayoutRequestDeclined(ctx context.Context, p PayoutRequestDeclined) error {
	if err := s.send(ctx, p.To, p.Subject(), p.Text()); err != nil {
		s.logger.Error("resend send payout request declined failed", "organization", p.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendPayoutRequestTransferSent delivers the asker's notice that the transfer
// has been submitted to the bank. Best-effort: the request is `processing`
// whether or not anybody was told, and the organizer's own payouts page carries
// the same date and the same expectation.
func (s *ResendEmailSender) SendPayoutRequestTransferSent(ctx context.Context, p PayoutRequestTransferSent) error {
	if err := s.send(ctx, p.To, p.Subject(), p.Text()); err != nil {
		s.logger.Error("resend send payout request transfer sent failed", "organization", p.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendPayoutRequestTransferFailed delivers the asker's notice that the bank sent
// the transfer back, carrying the operator's reason.
//
// Best-effort like the rest, and the log line matters more here than on any of
// the other four: this is the only notice the organizer must ACT on, and one
// that never arrives is an organizer waiting a week on money that is not coming.
// The reason itself is not logged — it is a message to one Organization, not an
// operational fact.
func (s *ResendEmailSender) SendPayoutRequestTransferFailed(ctx context.Context, p PayoutRequestTransferFailed) error {
	if err := s.send(ctx, p.To, p.Subject(), p.Text()); err != nil {
		s.logger.Error("resend send payout request transfer failed failed", "organization", p.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendFollowDigest delivers the weekly Follow Digest — the first
// non-transactional mail this platform sends, and the first whose language
// depends on its reader.
//
// Best-effort from this sender's point of view, but NOT from its caller's: the
// error is returned and the drain acts on it, because a Digest is the whole
// payload of a Follow and a failed one is retried rather than shrugged off
// (ADR 0030). That is the opposite of the notices above, where the money record
// is the fact and the mail is the courtesy.
//
// ADR 0030 requires this to send from a subdomain separate from transactional
// mail, so that complaints about a Digest cannot degrade the reputation the
// One-time Passcodes depend on. That separation is NOT made here and cannot be:
// this type holds one From and one key, and which of them it holds is decided by
// whoever constructed it. A production deployment builds TWO of these — see
// server.newEmailSender and SplitEmailSender — and the one carrying the Digest
// identity is the only one this method is ever reached on (#225).
func (s *ResendEmailSender) SendFollowDigest(ctx context.Context, d FollowDigest) error {
	if err := s.send(ctx, d.To, d.Subject(), d.Text()); err != nil {
		s.logger.Error("resend send follow digest failed", "locale", string(d.Locale), "new", len(d.New), "happening", len(d.Happening), "error", err)
		return err
	}
	return nil
}
