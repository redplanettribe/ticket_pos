package platform

import (
	"context"
	"sync"
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

// EmailSender delivers transactional email: staff one-time passcodes,
// Customer Sale Confirmations, Sale void/cancellation notices, and the notice
// that a refund the Customer was told was being processed could not be made. A
// real provider is deferred; development and tests use the logging and capture
// implementations below.
type EmailSender interface {
	SendOTP(ctx context.Context, to string, code string) error
	SendSaleConfirmation(ctx context.Context, confirmation SaleConfirmation) error
	SendSaleVoided(ctx context.Context, voided SaleVoided) error
	SendSaleReversalRefused(ctx context.Context, refused SaleReversalRefused) error
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
}

// SendOTP records the last OTP delivered.
func (s *CaptureEmailSender) SendOTP(_ context.Context, to string, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastTo = to
	s.LastCode = code
	s.otpSends++
	return nil
}

// SendSaleConfirmation records a delivered Sale Confirmation.
func (s *CaptureEmailSender) SendSaleConfirmation(_ context.Context, c SaleConfirmation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SaleConfirmations = append(s.SaleConfirmations, c)
	return nil
}

// SendSaleVoided records a delivered Sale void/cancellation notice.
func (s *CaptureEmailSender) SendSaleVoided(_ context.Context, v SaleVoided) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VoidedSales = append(s.VoidedSales, v)
	return nil
}

// SendSaleReversalRefused records a delivered refused-reversal notice.
func (s *CaptureEmailSender) SendSaleReversalRefused(_ context.Context, r SaleReversalRefused) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RefusedReversals = append(s.RefusedReversals, r)
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
}
