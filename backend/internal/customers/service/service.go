// Package service implements the customers business rules: the platform-global
// Customer record created or reused by every Ticket Sale on every Sales Channel
// (ADR 0010). A Customer created this way is inert — verified_at stays null until
// a person proves they own the address, which nothing here does.
package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/otp"
)

// Service implements customers business rules.
type Service struct {
	repo   *repository.Repository
	otp    *otp.Service
	logger platform.Logger
	now    func() time.Time
	// links is the Confirmation Link signing key and the Storefront origin those
	// links point at. Both are configuration; see confirmationlink.go.
	links ConfirmationLinkConfig
}

// New returns a customers service.
//
// links is required rather than optional: this module owns Confirmation Link
// issuing and redemption, and a service that could not sign one would fail at
// the moment a Sale Confirmation is sent rather than at startup.
func New(repo *repository.Repository, otpService *otp.Service, logger platform.Logger, links ConfirmationLinkConfig) *Service {
	return &Service{
		repo:   repo,
		otp:    otpService,
		logger: logger,
		now:    time.Now,
		links:  links,
	}
}

// WithClock overrides the clock (tests). Passcode expiry is measured by the OTP
// service's own clock, so both move together.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	s.otp.WithClock(now)
	return s
}

// UpsertForSale creates or reuses the Customer for a Ticket Sale and returns the
// Customer id. It is the seam other domain modules call: sales invokes it from
// its service, passing the transaction that records the sale, so the Customer and
// the sale referencing it commit or roll back together.
//
// The email is normalised here, through platform.NormalizeEmail — the one rule
// every Customer entry path shares. The name is the one recorded on this sale; it
// refreshes the Customer's profile name only while the Customer is unverified.
// The sale's own recorded name is never touched by this call.
func (s *Service) UpsertForSale(ctx context.Context, tx *sql.Tx, email, firstName, lastName string, now time.Time) (string, error) {
	return s.repo.Upsert(ctx, tx, repository.UpsertInput{
		Email:     platform.NormalizeEmail(email),
		FirstName: firstName,
		LastName:  lastName,
		Now:       now,
	})
}
