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
