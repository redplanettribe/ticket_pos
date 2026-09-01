package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
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
	// ConsentBoxes is which consent boxes a capture surface must show the person
	// holding THIS session, in the same shape and with the same vocabulary the
	// sign-in door's consent-required outcome uses (#254, parent #249).
	//
	// It rides on the session read rather than on an endpoint of its own for the
	// reason the Tax ID and the phone above do: the Storefront checkout dialog
	// asks this one question of the API when it opens, and the answer to "who is
	// buying" and the answer to "what may I still ask them" have to come from ONE
	// snapshot of ONE session. Two reads could disagree — a dialog prefilled with
	// somebody's email while drawing boxes computed for nobody — and the box that
	// gets drawn wrongly is one whose tick would churn a standing answer.
	//
	// IT IS NOT AN ORACLE. It is behind a Customer Session, so it only ever tells
	// a Customer about themselves; nothing here is reachable before Proof of Email
	// Ownership, which is the discipline ADR 0035 sets for every consent surface.
	//
	// A SALE-SCOPED SESSION IS SHOWN EVERY BOX. A Confirmation Link session is
	// minted from a token in a forwarded email rather than from proof, so a
	// checkout under one is captured as a guest's (selfAssertedCheckout refuses
	// it) — and what is shown must be exactly what the write side will honour.
	// Its answers cannot churn anything either: unproven answers are only ever
	// written where the owner has not answered.
	//
	// Nothing here says what to PRE-TICK, and nothing ever should: stored state
	// decides whether to ASK, never what to show as already agreed.
	ConsentBoxes ConsentBoxesView `json:"consent_boxes"`
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
//
// locale is the language of the Storefront page the passcode was asked from, or
// empty from a caller with no page to name one. It WORDS THIS ONE EMAIL AND
// NOTHING ELSE (ADR 0033): nothing here writes the Customer's Mail Locale, and
// nothing may, because this request is anonymous — an unauthenticated caller
// naming a stranger's address must not be able to rewrite a stored property of
// their record and change what language their receipts arrive in. Only a
// completed sign-in writes it (see signInProvenEmail).
//
// An unserved or malformed language is ignored rather than refused, exactly as
// on the sign-in doors, and the email goes out in English. A passcode is how a
// person gets in; it must never fail over the words it is written in.
func (s *Service) RequestOTP(ctx context.Context, email, clientIP, locale string) (*CustomerOTPRequestResult, error) {
	// The requesting page's language, then English. The remembered Mail Locale is
	// deliberately not a candidate here — see ResolveMailLocale.
	mailLocale := platform.ResolveMailLocale(locale, "")

	if err := s.otp.Issue(ctx, otpPurpose, email, clientIP, mailLocale); err != nil {
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
// locale is the Locale of the Storefront page the passcode was redeemed on, or
// empty from any caller that has no page to name one from. It is remembered on
// the Customer, never checked: see signInProvenEmail.
//
// The outcome is a session OR a consent step (#251): a Customer with no Policy
// Acceptance of the current Policy Version has proven their address and earned
// nothing else yet. Failing the passcode still looks exactly as it did — the
// consent-required outcome is only ever reached past a correct code.
func (s *Service) VerifyOTP(ctx context.Context, email, code, locale string) (*SignInOutcome, error) {
	email = platform.NormalizeEmail(email)
	now := s.now()

	if err := s.otp.Verify(ctx, otpPurpose, email, code); err != nil {
		return nil, err
	}

	return s.signInProvenEmail(ctx, email, now, "", locale)
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
// locale is the second thing both doors carry alike: the language of the
// Storefront page the sign-in happened on, remembered as the Customer's Mail
// Locale because mail has no address to carry a Locale of its own (ADR 0030,
// ADR 0033).
// It is a preference and not a credential, so an unserved language is dropped
// rather than refused — a sign-in is Proof of Email Ownership and must not fail
// over the words a later email will be written in. A caller that names no
// Locale at all leaves what was remembered exactly as it was.
//
// THE CONSENT GATE LIVES HERE, at the convergence, and that placement is the
// point of it (#251, parent #249). Both doors prove the same fact, so both owe
// the same question afterwards: has this Customer accepted the Policy Version
// that is current now? A gate written into VerifyOTP would have left Google
// Sign-In as an unguarded way past it, and the two doors drifting apart is
// exactly the failure a shared convergence exists to prevent. The check runs
// AFTER the proof and never before it — see RequestOTP on why nothing about a
// known Customer may be observable earlier.
//
// A Customer with consent outstanding is minted NO SESSION and gets a
// consent-required outcome instead. Everything upstream of the session still
// happens: the record is created or reused, verified_at is stamped, the Mail
// Locale is remembered, an Avatar is seeded. Only the credential is withheld,
// which is what makes abandoning the step cost the person nothing they had.
//
// The email must already be normalised and proven by the caller.
func (s *Service) signInProvenEmail(ctx context.Context, email string, now time.Time, seedAvatarURL, locale string) (*SignInOutcome, error) {
	mailLocale := ""
	if parsed, ok := platform.ParseLocale(locale); ok {
		mailLocale = string(parsed)
	}

	customer, err := s.repo.VerifyCustomer(ctx, email, now, mailLocale)
	if err != nil {
		return nil, err
	}
	customer = s.seedAvatarFromGoogle(ctx, customer, seedAvatarURL)

	// Asked ONCE and used twice: it decides whether this proof earns a session at
	// all, and — when it does — which boxes the session it earns still owes. Two
	// reads could answer differently under a concurrent capture, and the door
	// would then mint a session claiming a box was answered that it had just
	// gated on.
	outstanding, err := s.consent.Outstanding(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	required, err := s.gateOnConsent(ctx, customer, outstanding, now)
	if err != nil {
		return nil, err
	}
	if required != nil {
		return &SignInOutcome{ConsentRequired: required}, nil
	}

	session, view, err := s.mintSession(ctx, customer, now, outstanding)
	if err != nil {
		return nil, err
	}
	return &SignInOutcome{Session: view, SessionID: session.ID}, nil
}

// mintSession issues the full Customer Session a proven email earns, and is the
// one place that does.
//
// Both doors reach it through signInProvenEmail, and the consent step reaches
// it directly when a submission finishes a sign-in that was held. That is
// deliberate: the session a consent submission produces must be THE SAME
// session the sign-in would have produced — same scope, same window, same view
// on the wire — and the only way to guarantee that is for there to be one
// statement that mints it.
//
// A full Customer Session: no Ticket Sale scope, so it spans every sale this
// Customer owns across every Organization.
//
// `outstanding` is what this Customer is still owed AT THE MOMENT THE SESSION
// COMES INTO EXISTENCE, which each caller knows and this one does not: the
// sign-in door has just read it to decide whether to mint at all, and the
// consent step has just answered every box that was outstanding. Recomputing it
// here would be a third read of a question already asked.
func (s *Service) mintSession(ctx context.Context, customer *repository.Customer, now time.Time, outstanding consent.Outstanding) (repository.CustomerSession, *CustomerSessionView, error) {
	token, err := newSessionToken()
	if err != nil {
		return repository.CustomerSession{}, nil, err
	}

	session := repository.CustomerSession{
		ID:         token,
		CustomerID: customer.ID,
		ExpiresAt:  now.Add(sessionDuration),
		CreatedAt:  now,
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return repository.CustomerSession{}, nil, err
	}
	return session, s.sessionView(customer, &session, outstanding), nil
}

// GetSession loads a Customer Session — extending it if it is a full one —
// returning which email the caller is signed in as and which consent boxes the
// holder still owes an answer to.
func (s *Service) GetSession(ctx context.Context, token string) (*CustomerSessionView, error) {
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}

	// Read only for a full session. A sale-scoped one is shown every box whatever
	// the state says (see CustomerSessionView.ConsentBoxes), so asking would be a
	// query whose answer is discarded — and one that could fail a session read
	// over a question that session never gets to ask.
	var outstanding consent.Outstanding
	if !session.TicketSaleID.Valid {
		if outstanding, err = s.consent.Outstanding(ctx, customer.ID); err != nil {
			return nil, err
		}
	}
	return s.sessionView(customer, session, outstanding), nil
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

func (s *Service) sessionView(customer *repository.Customer, session *repository.CustomerSession, outstanding consent.Outstanding) *CustomerSessionView {
	view := &CustomerSessionView{
		Email:     customer.Email,
		FirstName: customer.FirstName,
		LastName:  customer.LastName,
		AvatarURL: s.avatarURL(customer),
		ConsentBoxes: ConsentBoxesView{
			PolicyAcceptance:  outstanding.PolicyAcceptance,
			MarketingConsent:  outstanding.MarketingConsent,
			NetworkingConsent: outstanding.NetworkingConsent,
			// The Terms box reaches the checkout dialog through this same read
			// (#537): a session minted before the current edition took effect —
			// the one-time re-gate, or any later bump — owes the box at the
			// dialog, which is the backstop that lets no Online Sale complete
			// unaccepted. Ordinarily false, because a sign-in since #536 cannot
			// finish without settling it.
			TermsAcceptance: outstanding.TermsAcceptance,
			// And the 18+ box beside it (#586, ADR 0069), from the same
			// Outstanding and never recomputed here. It reaches the checkout
			// dialog by the same road the Terms box does, and it is false
			// wherever the Terms box is — it tracks that box, and under an
			// edition carrying no Adulthood Declaration Artifact it is simply
			// always false.
			AdulthoodDeclaration: outstanding.AdulthoodDeclaration,
		},
	}
	// A Confirmation Link session proves nothing about who is holding it, so a
	// capture surface under one asks everything — the same verdict the checkout's
	// write side reaches by refusing to treat a sale-scoped session as the
	// buyer's own assertion. Applied HERE rather than at each caller so that no
	// future caller can mint one of these views and forget it.
	if session.TicketSaleID.Valid {
		view.ConsentBoxes = ConsentBoxesView{
			PolicyAcceptance: true, MarketingConsent: true, NetworkingConsent: true, TermsAcceptance: true,
			// The Adulthood Declaration is NOT forced on with its neighbours,
			// and that is the one considered exception here (#586, ADR 0069).
			// The three above are re-asked because a Confirmation Link session
			// proves nothing about who holds it, so nothing it answers may be
			// trusted as the owner's; the declaration is different in that a
			// person who is owed nothing has already made it — a refusal writes
			// no row, so an acceptance of an Artifact-carrying edition
			// necessarily carried one. Forcing it on would draw a box this
			// person has already answered, and — where the edition carries no
			// Artifact at all — one with no words beside it.
			AdulthoodDeclaration: outstanding.AdulthoodDeclaration,
		}
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
