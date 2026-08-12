package service

import (
	"context"
	"crypto/hmac"
	"sort"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers"
)

// The confirmation link that resolves a Pending Confirmation (#255, parent
// #249, ADR 0035).
//
// THIS IS THE UNSUBSCRIBE TOKEN INVERTED, and the inversion is the whole design.
// That link says No from an inbox and this one says Yes, so everything about the
// shape carries over — an HMAC under the same key, a purpose baked into the
// signed payload, no expiry, a Storefront page that confirms with a POST — and
// the one thing that does not carry over is what a leaked token costs, which is
// why the argument is restated here rather than assumed.
//
// WHY IT DOES NOT EXPIRE. ADR 0035 answers this directly, and it is the same
// answer ADR 0030 gave for the opt-out: a Sale Confirmation sits in an inbox for
// months, and the likeliest moment somebody presses this is on a receipt they
// have just rediscovered. A link gone stale by then would send a person who
// wants to confirm their own opt-in to a sign-in page, which is not a double
// opt-in but an obstacle course. What the token can do is bounded instead of
// dated: it names one Customer, it can only ever move a state the platform is
// ALREADY treating as a guest's unproven tick, and everything it can grant is
// visible and reversible in that person's own Customer Area.
//
// WHY A LEAKED ONE IS SURVIVABLE, which is the honest difference from the
// unsubscribe link — that one can only silence mail, and this one can authorize
// it. Three things bound it. The token reaches nothing that is not already
// pending, so it can never manufacture consent out of nothing: the only state it
// can change is one where somebody typed this address into a checkout and ticked
// a box. Its scope is fixed at minting, so it cannot pick up a later pending it
// was not written for. And it travelled only to the address it is about. The
// worst it achieves is a marketing opt-in its owner can see and switch off from
// their own Area — which is exactly the trade ADR 0035 priced in.

// consentConfirmationLinkPath is the STOREFRONT route the link points at, never
// an API one (ADR 0008), and it is a page rather than an endpoint for the reason
// the unsubscribe link is: the page confirms with a POST, so a mail scanner that
// merely opened the address has confirmed nothing. A GET that acted would let a
// prefetch grant consent that was never given, which is the exact hazard
// routes.go documents for the unsubscribe endpoint.
const consentConfirmationLinkPath = "/confirm-consent"

// consentConfirmationTokenPurpose is this token's domain separator, and it is
// its OWN string rather than a reuse of the unsubscribe one on purpose.
//
// All three signed tokens in this service — Confirmation Link, unsubscribe,
// confirmation of consent — are HMACs under the SAME key, so the purpose is the
// only thing that stops them being one credential. Without it a token minted to
// switch a Digest OFF would be a valid token to switch a Marketing Consent ON,
// which is precisely the substitution the domain separator exists to refuse: an
// unsubscribe link and a consent confirmation are opposite acts and their tokens
// must not be interchangeable strings. The purpose sits INSIDE the signed
// payload rather than beside it, so it cannot be edited off a genuine token.
const consentConfirmationTokenPurpose = "consent-confirm"

// The scope letters carried in the payload: which boxes this link was minted to
// confirm. Single characters because they ride in a URL somebody may read aloud
// or a mail client may wrap, and sorted into a canonical order so one scope has
// exactly one spelling.
const (
	consentScopeMarketing  = "m"
	consentScopeNetworking = "n"
)

// ConsentConfirmationView is what a press did, as the confirmation page reports
// it.
//
// It says what THIS press confirmed and what is now true, which are different
// facts and both worth telling: a second press confirms nothing and the page
// must not congratulate somebody on an act that did not happen. AlreadyResolved
// is what it says instead.
type ConsentConfirmationView struct {
	// MarketingConsent and NetworkingConsent are true where this press flipped
	// that box from Pending Confirmation to granted.
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
	// AlreadyResolved is true when there was nothing left for this link to
	// confirm — a second press, or an answer the owner has since given
	// themselves. Not an error, and deliberately not distinguished further: the
	// page has nothing useful to say about WHICH of those it was, and the person
	// can see their own answers in their Customer Area.
	AlreadyResolved bool `json:"already_resolved"`
	// DigestEnabled is the Follow Digest as it now stands, so a person who has
	// just confirmed a marketing opt-in is told what they will actually receive
	// (ADR 0034 — one switch).
	DigestEnabled bool `json:"digest_enabled"`
}

// ConsentConfirmationLinkURL returns the signed confirmation link for one
// Customer, or "" when they have nothing pending.
//
// THE EMPTY STRING IS THE WHOLE OF "sales that left nothing pending are
// unchanged". A receipt for a signed-in buyer, for somebody who ticked nothing,
// for a box office sale or an import gets no line at all, because there is
// nothing to confirm — and the caller renders the line only when there is a link
// to put in it. Deciding it here rather than at the send site keeps the rule in
// one place and out of the sales module, which has no business knowing what
// Pending Confirmation is.
//
// It is minted here, like the unsubscribe link, because the signing key is this
// service's (ConfirmationLinkConfig) and the Customer is this module's subject.
//
// THE SCOPE IS BAKED IN at minting. What pends when the mail is written is what
// the mail offers, and a press months later confirms nothing beyond it: the
// person read a line about the boxes THEY had ticked, and a later guest checkout
// pending a different box was never on that page.
func (s *Service) ConsentConfirmationLinkURL(ctx context.Context, customerID string) (string, error) {
	if len(s.links.Secret) == 0 {
		// Refusing beats emitting an unsigned or default-signed link, which would
		// be an unauthenticated way to grant consent for any Customer whose id
		// somebody could guess.
		return "", customers.ErrConsentConfirmationLinkUnavailable()
	}

	pending, err := s.consent.PendingConfirmations(ctx, customerID)
	if err != nil {
		return "", err
	}
	if !pending.Any() {
		return "", nil
	}
	return s.links.StorefrontBaseURL + consentConfirmationLinkPath + "?token=" + s.signConsentConfirmationLink(customerID, pending), nil
}

// ConfirmConsent resolves whatever of a signed link's scope is still pending.
//
// NO SESSION, BY CONTRACT, exactly as for the unsubscribe link and for the same
// reason: this is read in a mail client by somebody who may have no account at
// all — a guest checkout creates a Customer nobody has ever signed in as — and a
// double opt-in gated behind a passcode is not a one-click confirmation. The
// token is the whole authority, and what it can reach is bounded to the point
// where being unauthenticated costs nothing worth having (see the file comment).
//
// What a press MEANS is not decided here. This resolves the token to a Customer
// and hands the act to the consent module, which owns the staleness rule, the
// evidence and the digest lockstep (consent/service.ConfirmPending). Nothing in
// this module writes consent state.
func (s *Service) ConfirmConsent(ctx context.Context, token string, evidence consent.Evidence) (*ConsentConfirmationView, error) {
	customerID, scope, err := s.parseConsentConfirmationLink(token)
	if err != nil {
		return nil, err
	}

	customer, err := s.repo.GetCustomerByID(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		// A validly signed token for a Customer who no longer exists is spent. It
		// is reported as an invalid link rather than as a missing Customer: this
		// endpoint takes no credential, so distinguishing the two would make it an
		// oracle for whether a Customer id exists.
		return nil, customers.ErrConsentConfirmationLinkInvalid()
	}

	result, err := s.consent.ConfirmPending(ctx, consent.Confirmation{
		CustomerID: customer.ID,
		// The Customer's own stored address, because that is the only address this
		// surface can be about: the link was mailed to it, and nobody typed
		// anything. It is deliberately not the address a guest asserted at
		// checkout — the two differ exactly when somebody typed a stranger's, and
		// the record must say which inbox actually presented the token.
		Email: customer.Email,
		Scope: scope,
		// No SessionID: this route is session-less by contract, and an empty
		// evidence field is recorded as "not collected" rather than as a blank.
		Evidence: evidence,
	})
	if err != nil {
		return nil, err
	}

	return &ConsentConfirmationView{
		MarketingConsent:  result.Confirmed.MarketingConsent,
		NetworkingConsent: result.Confirmed.NetworkingConsent,
		AlreadyResolved:   !result.Confirmed.Any(),
		DigestEnabled:     result.MarketingConsent == consent.StateGranted,
	}, nil
}

// signConsentConfirmationLink produces the token: which Customer, for which
// purpose, over which boxes, plus an HMAC over all three.
//
// The same two-segment `payload.mac` shape and URL-safe encoding as a
// Confirmation Link and an unsubscribe token, because it survives a mail client,
// a copy-paste and a query string for exactly the same reasons — and because one
// token format in this service is one format to get right.
func (s *Service) signConsentConfirmationLink(customerID string, scope consent.Pending) string {
	payload := consentConfirmationPayload(customerID, scope)
	return encodeSegment([]byte(payload)) + "." + encodeSegment(s.confirmationLinkMAC(payload))
}

// parseConsentConfirmationLink validates a token and returns the Customer and
// the scope it names.
//
// THE SIGNATURE IS CHECKED BEFORE ANYTHING IN THE PAYLOAD IS BELIEVED, in
// constant time, so a token that was edited, truncated or made up fails here and
// never reaches a write. Without it this endpoint would be an unauthenticated
// way to grant a pending consent for any Customer whose id somebody could guess
// — and, worse than the unsubscribe link's equivalent, to widen the scope of a
// genuine token by editing a letter.
func (s *Service) parseConsentConfirmationLink(token string) (string, consent.Pending, error) {
	if len(s.links.Secret) == 0 {
		return "", consent.Pending{}, customers.ErrConsentConfirmationLinkUnavailable()
	}

	encodedPayload, encodedMAC, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found {
		return "", consent.Pending{}, customers.ErrConsentConfirmationLinkInvalid()
	}
	payload, err := decodeSegment(encodedPayload)
	if err != nil {
		return "", consent.Pending{}, customers.ErrConsentConfirmationLinkInvalid()
	}
	mac, err := decodeSegment(encodedMAC)
	if err != nil {
		return "", consent.Pending{}, customers.ErrConsentConfirmationLinkInvalid()
	}
	if !hmac.Equal(mac, s.confirmationLinkMAC(string(payload))) {
		return "", consent.Pending{}, customers.ErrConsentConfirmationLinkInvalid()
	}

	parts := strings.Split(string(payload), ":")
	// The purpose is checked as strictly as the signature. A genuinely signed
	// unsubscribe link or Confirmation Link presented here is a valid HMAC over a
	// payload that means something else, and only this comparison stops it being
	// spent as a consent confirmation — which, between two tokens that mean
	// opposite things, is the substitution that matters most.
	if len(parts) != 3 || parts[0] != consentConfirmationTokenPurpose || !isUUID(parts[1]) {
		return "", consent.Pending{}, customers.ErrConsentConfirmationLinkInvalid()
	}
	scope, ok := parseConsentScope(parts[2])
	if !ok {
		return "", consent.Pending{}, customers.ErrConsentConfirmationLinkInvalid()
	}
	return parts[1], scope, nil
}

// consentConfirmationPayload is what the token asserts and the signature covers:
// that this is a consent confirmation, for whom, and over which boxes.
//
// No expiry, unlike a Confirmation Link's — see the file comment for why a dated
// double opt-in would be no double opt-in.
func consentConfirmationPayload(customerID string, scope consent.Pending) string {
	return consentConfirmationTokenPurpose + ":" + customerID + ":" + formatConsentScope(scope)
}

// formatConsentScope writes the scope in its one canonical spelling. Sorted, so
// that the same two boxes cannot produce two different valid tokens.
func formatConsentScope(scope consent.Pending) string {
	var letters []string
	if scope.MarketingConsent {
		letters = append(letters, consentScopeMarketing)
	}
	if scope.NetworkingConsent {
		letters = append(letters, consentScopeNetworking)
	}
	sort.Strings(letters)
	return strings.Join(letters, "")
}

// parseConsentScope reads the letters back, and refuses anything it does not
// recognise rather than ignoring it.
//
// AN EMPTY SCOPE IS REFUSED. A link is only ever minted when something pends, so
// a token naming no box is not one this platform wrote — and treating it as
// "confirm nothing" would give a forger a shape that validates.
func parseConsentScope(raw string) (consent.Pending, bool) {
	var scope consent.Pending
	for _, letter := range strings.Split(raw, "") {
		switch letter {
		case consentScopeMarketing:
			if scope.MarketingConsent {
				return consent.Pending{}, false
			}
			scope.MarketingConsent = true
		case consentScopeNetworking:
			if scope.NetworkingConsent {
				return consent.Pending{}, false
			}
			scope.NetworkingConsent = true
		default:
			return consent.Pending{}, false
		}
	}
	if !scope.Any() {
		return consent.Pending{}, false
	}
	return scope, true
}
