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
	// reversal answers whether a given Ticket Sale's Payment can actually be
	// undone, which is half of what makes a sale `reversible` in the Customer
	// Area (ADR 0018). The other half — the Reversal Window — is this module's
	// own arithmetic; this half belongs to whoever settled the money, so it
	// arrives from the Payment Provider boundary rather than being decided here.
	//
	// Its zero value refuses everything a Payment Provider would have to handle
	// and still allows a free claim, so a deployment that never wired one in
	// under-offers rather than over-promises.
	reversal platform.PaymentReversal
	// reversals pursues a Reversal Request this Customer left in flight, and is
	// consulted only when they load their own Area (ADR 0024). Optional: a
	// service without one renders pending reversals as pending and resolves
	// nothing, which is the correct behaviour for every surface that is not the
	// Customer Area.
	reversals ReversalRequestResolver
}

// ReversalRequestResolver asks the Payment Provider again about the Reversal
// Requests one Customer has left in flight, and applies whatever it answers.
//
// It is declared here, on the side that calls it, and implemented by the sales
// service — which owns Ticket Sales, the Payment Provider and the reversal
// primitive. The customers module owns who is asking and when; it does not own
// what is being pursued, and this interface is deliberately the narrowest
// statement of that: one Customer's own asks, no return value to render, and no
// way to name a sale or reach anybody else's.
type ReversalRequestResolver interface {
	ResolveInFlightReversalRequests(ctx context.Context, customerID string) error
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

// WithPaymentReversal attaches the rule that says whether a settled Payment can
// be undone. Same chaining shape as WithObjectStorage.
//
// It is wiring rather than a constructor argument because this module does not
// depend on payments for anything else: a Customer signs in, reads their Area
// and edits their profile without one. What it buys is that the Customer Area's
// offer and the reversal endpoint's own check are computed from one rule, so the
// button and the API can never disagree about a given sale.
func (s *Service) WithPaymentReversal(reversal platform.PaymentReversal) *Service {
	s.reversal = reversal
	return s
}

// WithReversalRequests attaches the resolver that pursues a Customer's own
// in-flight Reversal Requests when they load their Area (ADR 0024). Same
// chaining shape as WithPaymentReversal.
//
// Wired after construction because the sales service is built after this one and
// takes this one as a dependency, and because it is additive: unwired, the Area
// still renders a pending reversal as pending — it simply never resolves one,
// which is what every deployment did before #157.
func (s *Service) WithReversalRequests(resolver ReversalRequestResolver) *Service {
	s.reversals = resolver
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
// It takes the buyer whole — platform.SaleCustomer, the Sales Channel's account
// of one person — rather than their facts one positional argument at a time
// (#111). Which of those facts may be written back is decided in one place, by
// customer.SelfAsserted and the Customer's own verified state; a new guarded
// buyer fact therefore arrives on the bundle and changes no signature between
// the checkout form and the SQL.
//
// The email is normalised here, through platform.NormalizeEmail — the one rule
// every Customer entry path shares. The name is the one recorded on this sale; it
// refreshes the Customer's profile name only while the Customer is unverified.
// The sale's own recorded name is never touched by this call.
//
// The Tax ID is what the sale was transacted under, unset on a channel that
// carries none. Its write-back follows the same fill/refresh shape as the name
// plus one override — a Tax ID asserted under the Customer's own session
// replaces a verified one — and the rule itself is stated at repository.Upsert
// (ADR 0016). As with the name, the sale's own snapshot is written by the sales
// module and never touched here.
//
// The phone is the number the buyer typed at checkout, in canonical E.164 form
// and empty on every channel that collects none. It is what makes the same value
// prefill their NEXT purchase (#107, parent #103), and this seam is the only way
// it can reach the profile: a guest checkout proves nothing, so it cannot be
// written by the Customer Area, and the redirect brings nothing back but a
// transaction id, so it rides the Payment to get here. Its write-back is guarded
// exactly as the Tax ID's is — same three arms, same self-asserted flag, stated
// once at repository.Upsert — because the tampering vector is the same one.
//
// Unlike the Tax ID it is NOT snapshotted onto the Ticket Sale: ADR 0016 makes
// a Tax ID a fiscal fact of the sale, and a phone number is nothing of the kind.
func (s *Service) UpsertForSale(ctx context.Context, tx *sql.Tx, customer platform.SaleCustomer, now time.Time) (string, error) {
	// The copy is local: normalising here must not edit the caller's buyer, whose
	// email the sales module records on the Ticket Sale verbatim, as transacted.
	customer.Email = platform.NormalizeEmail(customer.Email)
	return s.repo.Upsert(ctx, tx, repository.UpsertInput{
		Customer: customer,
		Now:      now,
	})
}
