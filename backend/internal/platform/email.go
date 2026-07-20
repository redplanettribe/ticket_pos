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
}

// EmailSender delivers transactional email: staff one-time passcodes and
// Customer Sale Confirmations. A real provider is deferred; development and
// tests use the logging and capture implementations below.
type EmailSender interface {
	SendOTP(ctx context.Context, to string, code string) error
	SendSaleConfirmation(ctx context.Context, confirmation SaleConfirmation) error
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
func (s *LoggingEmailSender) SendSaleConfirmation(_ context.Context, c SaleConfirmation) error {
	s.Logger.Info("sale confirmation sent", "email", c.To, "reference", c.Reference, "event", c.EventName)
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

// CaptureEmailSender records delivered email for integration tests. It is safe
// for concurrent use so tests can exercise concurrent sales.
type CaptureEmailSender struct {
	mu                sync.Mutex
	LastTo            string
	LastCode          string
	SaleConfirmations []SaleConfirmation
}

// SendOTP records the last OTP delivered.
func (s *CaptureEmailSender) SendOTP(_ context.Context, to string, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastTo = to
	s.LastCode = code
	return nil
}

// SendSaleConfirmation records a delivered Sale Confirmation.
func (s *CaptureEmailSender) SendSaleConfirmation(_ context.Context, c SaleConfirmation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SaleConfirmations = append(s.SaleConfirmations, c)
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

// Reset clears captured email between tests.
func (s *CaptureEmailSender) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastTo = ""
	s.LastCode = ""
	s.SaleConfirmations = nil
}
