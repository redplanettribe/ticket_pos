package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/otp"
)

// sessionDuration is the Customer Session's sliding window, extended on every
// authenticated use. It is an order of magnitude longer than a Staff Session's
// fourteen days on purpose: Customers buy months ahead and come back at the
// door, and a staff-length window would turn a sign-in they never think about
// into a form met at every visit. Sessions are server-side rows, so a long
// window does not cost the ability to revoke.
const sessionDuration = 180 * 24 * time.Hour

// otpPurpose scopes every passcode this service issues and verifies to Customer
// sign-in. A passcode minted for any other surface must never open a Customer
// Session, so this constant is the only purpose customers ever names.
const otpPurpose = otp.PurposeCustomer

// CustomerSessionView is the public representation of a Customer Session: which
// email the caller is signed in as, and what the session is scoped to.
type CustomerSessionView struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	// The Customer's stored Tax ID, both halves null until they have one. It is
	// here so the Storefront checkout dialog can prefill it beside the email and
	// name, which is the whole point of storing one (ADR 0016). Unmasked: it is
	// the person's own Tax ID being shown back to the person, behind their own
	// session.
	TaxIDType   *string `json:"tax_id_type"`
	TaxIDNumber *string `json:"tax_id_number"`
	// The Customer's stored phone number in canonical E.164 form, null until they
	// have one. It rides on the session for exactly the reason the Tax ID does: the
	// checkout dialog prefills the field from here, which is what closes the "give
	// it once, never again" loop the phone exists to close (#103, #108). The
	// Storefront splits it back into a country selection and a national number for
	// display; nothing below the form ever sees the halves.
	Phone *string `json:"phone"`
	// The Customer's Avatar as a browser-loadable URL, null when they have none
	// (the Storefront renders initials instead). A URL rather than an object key
	// because no client of this view writes Avatars — the header only shows one.
	AvatarURL  *string `json:"avatar_url"`
	VerifiedAt *string `json:"verified_at"`
	// TicketSaleID is null for a full Customer Session, which spans every Ticket
	// Sale the Customer owns. A Confirmation Link session names one sale here.
	TicketSaleID *string `json:"ticket_sale_id"`
}

// CustomerOTPRequestResult is returned after requesting a Customer passcode.
type CustomerOTPRequestResult struct {
	Message string `json:"message"`
}

// requestOTPMessage is returned verbatim for every email, known or not. Whether
// the platform holds a Customer for an address is not something an anonymous
// caller may learn, so this response carries no signal at all.
const requestOTPMessage = "If this email can be signed in to, a passcode has been sent."

// RequestOTP delivers a Customer-purpose one-time passcode.
//
// The result is identical whether or not a Customer exists for the email: this
// endpoint is public and unauthenticated, and a difference in response, timing
// branch, or status code would turn it into an oracle for who the platform's
// customers are. The passcode is therefore issued for any well-formed address,
// and what it is worth is decided at verification.
func (s *Service) RequestOTP(ctx context.Context, email, clientIP string) (*CustomerOTPRequestResult, error) {
	if err := s.otp.Issue(ctx, otpPurpose, email, clientIP); err != nil {
		return nil, err
	}
	return &CustomerOTPRequestResult{Message: requestOTPMessage}, nil
}

// VerifyOTP validates a Customer-purpose passcode and issues a Customer Session.
// A passcode issued for any other purpose is not accepted here.
//
// A successful verification is what makes someone a Verified Customer: it is the
// only path in the system that sets verified_at. Nobody registers, so the record
// is created or reused from the proven email — a person who signs in before ever
// buying gets the same record their first Ticket Sale would have reused anyway.
func (s *Service) VerifyOTP(ctx context.Context, email, code string) (*CustomerSessionView, string, error) {
	email = platform.NormalizeEmail(email)
	now := s.now()

	if err := s.otp.Verify(ctx, otpPurpose, email, code); err != nil {
		return nil, "", err
	}

	return s.signInProvenEmail(ctx, email, now, "")
}

// signInProvenEmail is what every Proof of Email Ownership converges on: the
// create-or-reuse-and-stamp step that makes a Verified Customer, and the full
// Customer Session it earns.
//
// Both doors end here — a One-time Passcode and a Google Sign-In — because both
// assert the same fact and neither is worth more than the other (ADR 0011).
// Nothing recorded here says which one was used: no column, no field on the
// session view. A person who used a passcode on Monday and Google on Tuesday
// lands on one record with one history.
//
// seedAvatarURL is the one asymmetry between the doors: Google offers a profile
// picture and a passcode has none to offer, so the Google path passes the URL
// and the passcode path passes "". It seeds an Avatar only into an empty slot
// (see seedAvatarFromGoogle) and can fail without failing the sign-in — which is
// why it is a parameter here rather than a divergence: the session minted below
// must be identical either way.
//
// The email must already be normalised and proven by the caller.
func (s *Service) signInProvenEmail(ctx context.Context, email string, now time.Time, seedAvatarURL string) (*CustomerSessionView, string, error) {
	customer, err := s.repo.VerifyCustomer(ctx, email, now)
	if err != nil {
		return nil, "", err
	}
	customer = s.seedAvatarFromGoogle(ctx, customer, seedAvatarURL)

	token, err := newSessionToken()
	if err != nil {
		return nil, "", err
	}

	// A full Customer Session: no Ticket Sale scope, so it spans every sale this
	// Customer owns across every Organization.
	session := repository.CustomerSession{
		ID:         token,
		CustomerID: customer.ID,
		ExpiresAt:  now.Add(sessionDuration),
		CreatedAt:  now,
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, "", err
	}

	return s.sessionView(customer, &session), token, nil
}

// GetSession loads a Customer Session — extending it if it is a full one —
// returning which email the caller is signed in as.
func (s *Service) GetSession(ctx context.Context, token string) (*CustomerSessionView, error) {
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	return s.sessionView(customer, session), nil
}

// Logout destroys a Customer Session. Because sessions are server-side rows, the
// token is worthless the moment this returns.
func (s *Service) Logout(ctx context.Context, token string) error {
	session, err := s.repo.GetSession(ctx, token)
	if err != nil {
		return err
	}
	if session == nil {
		return customers.ErrCustomerSessionNotFound()
	}
	return s.repo.DeleteSession(ctx, token)
}

// AuthenticatedCustomer is the identity a validated Customer Session carries.
// TicketSaleID is empty for a full session and names one Ticket Sale for a
// Confirmation Link session.
type AuthenticatedCustomer struct {
	SessionID    string
	CustomerID   string
	Email        string
	TicketSaleID string
}

// Authenticate validates a Customer Session token and returns the Customer it
// belongs to, sliding the expiry only for a full session. It is the single entry
// point through which any Customer-scoped request establishes who the caller is.
func (s *Service) Authenticate(ctx context.Context, token string) (*AuthenticatedCustomer, error) {
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	out := &AuthenticatedCustomer{
		SessionID:  session.ID,
		CustomerID: customer.ID,
		Email:      customer.Email,
	}
	if session.TicketSaleID.Valid {
		out.TicketSaleID = session.TicketSaleID.String
	}
	return out, nil
}

// authenticate resolves a session token to its live session and Customer,
// extending the sliding window of a full session and leaving a sale-scoped one's
// fixed expiry exactly where it was. An expired session is destroyed rather than
// left to linger.
func (s *Service) authenticate(ctx context.Context, token string) (*repository.CustomerSession, *repository.Customer, error) {
	if token == "" {
		return nil, nil, customers.ErrCustomerSessionNotFound()
	}

	session, err := s.repo.GetSession(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, customers.ErrCustomerSessionNotFound()
	}

	now := s.now()
	if now.After(session.ExpiresAt) {
		_ = s.repo.DeleteSession(ctx, token)
		return nil, nil, customers.ErrCustomerSessionExpired()
	}

	customer, err := s.repo.GetCustomerByID(ctx, session.CustomerID)
	if err != nil {
		return nil, nil, err
	}
	if customer == nil {
		// The Customer is gone; the session it anchored cannot mean anything.
		_ = s.repo.DeleteSession(ctx, token)
		return nil, nil, customers.ErrCustomerSessionNotFound()
	}

	// Only a full Customer Session slides. A sale-scoped session is a Confirmation
	// Link redemption, and its shortness is what pays for the link token's
	// durability: the token stays good until the Event ends plus a grace window
	// precisely because each redemption grants only a day. Sliding one here would
	// let the first authenticated read behind a forwarded Sale Confirmation turn a
	// 24-hour credential into a 180-day one. Re-minting is by tapping the link
	// again, never by using the session.
	if !session.TicketSaleID.Valid {
		newExpiry := now.Add(sessionDuration)
		if err := s.repo.ExtendSession(ctx, token, newExpiry); err != nil {
			return nil, nil, err
		}
		session.ExpiresAt = newExpiry
	}

	return session, customer, nil
}

func (s *Service) sessionView(customer *repository.Customer, session *repository.CustomerSession) *CustomerSessionView {
	view := &CustomerSessionView{
		Email:     customer.Email,
		FirstName: customer.FirstName,
		LastName:  customer.LastName,
		AvatarURL: s.avatarURL(customer),
	}
	if customer.TaxIDType.Valid && customer.TaxIDNumber.Valid {
		taxIDType, taxIDNumber := customer.TaxIDType.String, customer.TaxIDNumber.String
		view.TaxIDType, view.TaxIDNumber = &taxIDType, &taxIDNumber
	}
	if customer.Phone.Valid {
		phone := customer.Phone.String
		view.Phone = &phone
	}
	if customer.VerifiedAt.Valid {
		verified := customer.VerifiedAt.Time.UTC().Format(time.RFC3339)
		view.VerifiedAt = &verified
	}
	if session.TicketSaleID.Valid {
		saleID := session.TicketSaleID.String
		view.TicketSaleID = &saleID
	}
	return view
}

// newSessionToken generates an opaque 256-bit session token. It is derived from
// nothing about the Customer, so it cannot be guessed from an email.
func newSessionToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
