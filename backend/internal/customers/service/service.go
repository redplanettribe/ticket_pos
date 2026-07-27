// Package service implements the customers business rules: the platform-global
// Customer record created or reused by every Ticket Sale on every Sales Channel
// (ADR 0010). A Customer created this way is inert — verified_at stays null until
// a person proves they own the address, which nothing here does.
package service

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/googleauth"
	"github.com/peter/ticket_pos/backend/internal/platform/otp"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
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
	// google redeems the authorization codes the Storefront relays, against the
	// Storefront's own Google OAuth client. It is the second Proof of Email
	// Ownership this service accepts, alongside the passcode; see
	// googlesignin.go.
	google *googleauth.Client
	// storage holds Customer Avatars. Optional: a deployment without object
	// storage still signs Customers in and edits profiles; only the Avatar
	// endpoints refuse, and Google seeding quietly does nothing (avatar.go).
	storage storage.ObjectStorage
	// avatarHTTP fetches a Google Sign-In picture for re-hosting. Overridable in
	// tests; defaults to a client with a bounded timeout.
	avatarHTTP *http.Client
}

// New returns a customers service.
//
// links is required rather than optional: this module owns Confirmation Link
// issuing and redemption, and a service that could not sign one would fail at
// the moment a Sale Confirmation is sent rather than at startup.
//
// google is required for the same shape of reason and behaves differently: a
// deployment holding no Google credentials still gets a client, which refuses
// every exchange with the ordinary generic error. Google Sign-In is one of two
// doors, so its absence must not stop the other from opening.
func New(repo *repository.Repository, otpService *otp.Service, logger platform.Logger, links ConfirmationLinkConfig, google *googleauth.Client) *Service {
	return &Service{
		repo:       repo,
		otp:        otpService,
		logger:     logger,
		now:        time.Now,
		links:      links,
		google:     google,
		avatarHTTP: &http.Client{Timeout: avatarFetchTimeout},
	}
}

// WithObjectStorage attaches the object storage Customer Avatars live in. Same
// chaining shape as WithClock; a service without it refuses Avatar writes.
func (s *Service) WithObjectStorage(store storage.ObjectStorage) *Service {
	s.storage = store
	return s
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
//
// taxID is the Tax ID the sale was transacted under, unset on a channel that
// carries none. Its write-back follows the same fill/refresh shape as the name
// plus one override — a Tax ID asserted under the Customer's own session
// replaces a verified one — and the rule itself is stated at repository.Upsert
// (ADR 0016). As with the name, the sale's own snapshot is written by the sales
// module and never touched here.
func (s *Service) UpsertForSale(ctx context.Context, tx *sql.Tx, email, firstName, lastName string, taxID platform.SaleTaxID, now time.Time) (string, error) {
	return s.repo.Upsert(ctx, tx, repository.UpsertInput{
		Email:     platform.NormalizeEmail(email),
		FirstName: firstName,
		LastName:  lastName,
		TaxID:     taxID,
		Now:       now,
	})
}
