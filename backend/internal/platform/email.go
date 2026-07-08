package platform

import "context"

// EmailSender delivers one-time passcodes to staff email addresses.
type EmailSender interface {
	SendOTP(ctx context.Context, to string, code string) error
}

// LoggingEmailSender logs OTP codes to the configured logger (development use).
type LoggingEmailSender struct {
	Logger Logger
}

// SendOTP logs the OTP code for local development and testing.
func (s *LoggingEmailSender) SendOTP(_ context.Context, to string, code string) error {
	s.Logger.Info("otp sent", "email", to, "code", code)
	return nil
}

// NoopEmailSender discards OTP delivery (tests).
type NoopEmailSender struct{}

func (NoopEmailSender) SendOTP(_ context.Context, _ string, _ string) error {
	return nil
}

// CaptureEmailSender stores the last OTP for integration tests.
type CaptureEmailSender struct {
	LastTo   string
	LastCode string
}

func (s *CaptureEmailSender) SendOTP(_ context.Context, to string, code string) error {
	s.LastTo = to
	s.LastCode = code
	return nil
}
