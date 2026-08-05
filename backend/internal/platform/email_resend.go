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

// SendOTP delivers a staff one-time passcode. A failure here is returned to the
// caller, which fails the login request rather than pretending a code was sent.
func (s *ResendEmailSender) SendOTP(ctx context.Context, to string, code string) error {
	subject := "Your Multiticketing passcode"
	text := fmt.Sprintf("Your one-time passcode is %s.\n\nIt expires shortly. If you did not request it, ignore this email.", code)
	if err := s.send(ctx, to, subject, text); err != nil {
		s.logger.Error("resend send otp failed", "email", to, "error", err)
		return err
	}
	return nil
}

// SendSaleConfirmation delivers a Customer's receipt. The caller treats this as
// best-effort, so a returned error is logged here for visibility and then
// discarded upstream — a delivery hiccup never reverses a recorded Ticket Sale.
func (s *ResendEmailSender) SendSaleConfirmation(ctx context.Context, c SaleConfirmation) error {
	if err := s.send(ctx, c.To, c.Subject(), c.Text()); err != nil {
		s.logger.Error("resend send sale confirmation failed", "email", c.To, "reference", c.Reference, "error", err)
		return err
	}
	return nil
}

// SendSaleVoided delivers a cancellation notice referencing the original Sale
// Confirmation. Best-effort, as above.
func (s *ResendEmailSender) SendSaleVoided(ctx context.Context, v SaleVoided) error {
	if err := s.send(ctx, v.To, v.Subject(), v.Text()); err != nil {
		s.logger.Error("resend send sale voided failed", "email", v.To, "reference", v.Reference, "error", err)
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
		s.logger.Error("resend send sale reversal refused failed", "email", r.To, "reference", r.Reference, "error", err)
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
		s.logger.Error("resend send payout request submitted failed", "email", p.To, "organization", p.OrganizationName, "error", err)
		return err
	}
	return nil
}

// SendPayoutRequestPaid delivers the asker's notice that the transfer was made.
// Best-effort: the Payout is the fact, and a failure here leaves an organizer
// who finds out from their bank instead.
func (s *ResendEmailSender) SendPayoutRequestPaid(ctx context.Context, p PayoutRequestPaid) error {
	if err := s.send(ctx, p.To, p.Subject(), p.Text()); err != nil {
		s.logger.Error("resend send payout request paid failed", "email", p.To, "organization", p.OrganizationName, "error", err)
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
		s.logger.Error("resend send payout request declined failed", "email", p.To, "organization", p.OrganizationName, "error", err)
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
		s.logger.Error("resend send payout request transfer sent failed", "email", p.To, "organization", p.OrganizationName, "error", err)
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
		s.logger.Error("resend send payout request transfer failed failed", "email", p.To, "organization", p.OrganizationName, "error", err)
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
		s.logger.Error("resend send follow digest failed", "email", d.To, "locale", string(d.Locale), "events", len(d.Events), "error", err)
		return err
	}
	return nil
}
