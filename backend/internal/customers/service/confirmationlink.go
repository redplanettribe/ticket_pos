package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/repository"
)

// The Confirmation Link: the credential carried in every Sale Confirmation that
// opens that one Ticket Sale without signing in (PRD #55 decision 5).
//
// Its lifetime is the reverse of a password-reset link's, and getting that round
// the right way is the whole point. A reset link is a one-shot recovery of a
// credential and expires in minutes; this is a durable pointer to something the
// person already owns, and its most important moment is at the gate months after
// the purchase. So:
//
//   - The TOKEN stays valid until the Event has ended plus a grace window. A
//     ticket bought in March opens in June, in a queue, from an old email.
//   - Each redemption mints only a SHORT session scoped to that one Ticket Sale,
//     re-mintable by tapping the link again.
//
// The durability is paid for with narrowness. Confirmation emails get forwarded,
// so the session a link mints reaches exactly one sale and re-narrows within a
// day; seeing everything else requires a passcode.
const (
	// confirmationLinkGrace is how long after an Event ends its Confirmation
	// Links keep working. Long enough that a dispute, a late question, or a
	// misremembered date after the Event still resolves.
	confirmationLinkGrace = 30 * 24 * time.Hour

	// confirmationLinkFloor is the shortest life any link gets, measured from the
	// moment it is issued. It matters for back-dated sales: a Sale Import of an
	// Event that already happened would otherwise mint a link that was dead
	// before the email left the building.
	confirmationLinkFloor = 30 * 24 * time.Hour

	// confirmationLinkUndatedHorizon covers an Event with no schedule at all.
	// There is no moment to hang the expiry on, so the link gets a year — long
	// enough to be useful once the date is announced, short enough to still be a
	// bound.
	confirmationLinkUndatedHorizon = 365 * 24 * time.Hour

	// confirmationLinkSessionDuration is the session one redemption mints. It is
	// short by design and does not slide: a forwarded email's blast radius closes
	// within a day, while the link that opened it stays good for months.
	confirmationLinkSessionDuration = 24 * time.Hour

	// confirmationLinkPath is the Storefront route the link points at. It is a
	// Storefront URL, never an API one: the Customer must land on a page, and no
	// browser may address the Go API directly (ADR 0008).
	confirmationLinkPath = "/tickets/confirm"
)

// ConfirmationLinkConfig is what minting a Confirmation Link needs: the HMAC key
// the token is signed with, and the Storefront origin the link points at. Both
// come from configuration; there is no default for the secret.
type ConfirmationLinkConfig struct {
	Secret            []byte
	StorefrontBaseURL string
}

// ConfirmationLinkURL returns the Confirmation Link for one Ticket Sale, to be
// carried in its Sale Confirmation.
//
// eventEnd is the moment the sale's Event finishes, or the zero time when the
// Event has no schedule; the grace window and the undated fallback are applied
// here rather than by the caller, so every Sales Channel gets the same lifetime.
// The expiry is baked into the signed token, which is why redemption needs no
// Event lookup and no stored token table.
func (s *Service) ConfirmationLinkURL(ticketSaleID string, eventEnd time.Time) (string, error) {
	if len(s.links.Secret) == 0 {
		// Refusing beats emitting an unsigned or default-signed link that would
		// open a stranger's Ticket Sale.
		return "", customers.ErrConfirmationLinkUnavailable()
	}

	token := s.signConfirmationLink(ticketSaleID, s.confirmationLinkExpiry(eventEnd, s.now()))
	return s.links.StorefrontBaseURL + confirmationLinkPath + "?token=" + token, nil
}

// confirmationLinkExpiry is the lifetime policy in one place: the Event's end
// plus a grace window, a year for an Event with no date at all, and never less
// than the floor measured from now.
func (s *Service) confirmationLinkExpiry(eventEnd, now time.Time) time.Time {
	expiry := now.Add(confirmationLinkUndatedHorizon)
	if !eventEnd.IsZero() {
		expiry = eventEnd.Add(confirmationLinkGrace)
	}
	if floor := now.Add(confirmationLinkFloor); expiry.Before(floor) {
		return floor
	}
	return expiry
}

// RedeemConfirmationLink turns a Confirmation Link token into a Customer Session
// scoped to the one Ticket Sale the link names.
//
// existingSessionToken is whatever Customer Session the caller already holds, if
// any. Three properties this function exists to hold, each of which is easy to
// get wrong:
//
//  1. It never sets verified_at. Possession of a forwarded email is not proof of
//     owning the address; only a passcode makes someone a Verified Customer, and
//     that is what gates the profile-name rule and, later, whether we may email
//     them at all. Nothing here can reach the one write that verifies.
//  2. The session it mints reaches exactly one Ticket Sale. Not the Customer's
//     other sales, not any sale from the same Organization — one.
//  3. It never narrows an existing full Customer Session. Someone already signed
//     in who taps a link keeps everything: the wider credential wins, and a link
//     must not downgrade the person holding it.
func (s *Service) RedeemConfirmationLink(ctx context.Context, token, existingSessionToken string) (*CustomerSessionView, string, error) {
	// Property 3, checked before the token is even looked at. A full session is
	// strictly wider than anything a link can mint, so the redemption becomes a
	// no-op that returns the credential the caller already has.
	if existingSessionToken != "" {
		if session, customer, err := s.authenticate(ctx, existingSessionToken); err == nil && !session.TicketSaleID.Valid {
			return s.sessionView(customer, session), existingSessionToken, nil
		}
		// Anything else — expired, destroyed, or itself scoped to a single sale —
		// is not wider than what this link grants, so the link is redeemed
		// normally below.
	}

	ticketSaleID, err := s.parseConfirmationLink(token, s.now())
	if err != nil {
		return nil, "", err
	}

	customerID, ok, err := s.repo.GetTicketSaleCustomer(ctx, ticketSaleID)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		// A validly signed token for a sale that no longer exists is spent. It is
		// reported as an invalid link rather than a missing sale: the holder is
		// not entitled to learn which of the two it was.
		return nil, "", customers.ErrConfirmationLinkInvalid()
	}

	customer, err := s.repo.GetCustomerByID(ctx, customerID)
	if err != nil {
		return nil, "", err
	}
	if customer == nil {
		return nil, "", customers.ErrConfirmationLinkInvalid()
	}

	sessionToken, err := newSessionToken()
	if err != nil {
		return nil, "", err
	}

	now := s.now()
	// Property 2: the scope is written onto the session row itself, so every read
	// behind it is narrowed by the database query rather than by a check some
	// future handler might forget.
	session := repository.CustomerSession{
		ID:           sessionToken,
		CustomerID:   customer.ID,
		TicketSaleID: nullableString(ticketSaleID),
		ExpiresAt:    now.Add(confirmationLinkSessionDuration),
		CreatedAt:    now,
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, "", err
	}

	// Property 1: the Customer is returned exactly as stored. Nothing above
	// touched verified_at, and the view reports it honestly — an unverified
	// Customer who arrives by link is still unverified afterwards.
	return s.sessionView(customer, &session), sessionToken, nil
}

// signConfirmationLink produces the token: the payload the link asserts, plus an
// HMAC over it keyed by a server-held secret.
//
// A signed, stateless token beats a stored token table here. The expiry is
// derivable from the Event, so the only thing a table would add is a row to
// create, index, and eventually sweep — plus a migration — for a credential
// whose entire content is "this sale, until then".
func (s *Service) signConfirmationLink(ticketSaleID string, expires time.Time) string {
	payload := confirmationLinkPayload(ticketSaleID, expires)
	return encodeSegment([]byte(payload)) + "." + encodeSegment(s.confirmationLinkMAC(payload))
}

// parseConfirmationLink validates a token and returns the Ticket Sale it names.
//
// The signature is checked before anything in the payload is trusted or acted
// on, and compared in constant time. A token that was edited, truncated, or
// simply made up therefore fails here and never reaches a database lookup.
func (s *Service) parseConfirmationLink(token string, now time.Time) (string, error) {
	if len(s.links.Secret) == 0 {
		return "", customers.ErrConfirmationLinkUnavailable()
	}

	encodedPayload, encodedMAC, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found {
		return "", customers.ErrConfirmationLinkInvalid()
	}
	payload, err := decodeSegment(encodedPayload)
	if err != nil {
		return "", customers.ErrConfirmationLinkInvalid()
	}
	mac, err := decodeSegment(encodedMAC)
	if err != nil {
		return "", customers.ErrConfirmationLinkInvalid()
	}
	if !hmac.Equal(mac, s.confirmationLinkMAC(string(payload))) {
		return "", customers.ErrConfirmationLinkInvalid()
	}

	ticketSaleID, rawExpiry, found := strings.Cut(string(payload), ":")
	if !found || !isUUID(ticketSaleID) {
		return "", customers.ErrConfirmationLinkInvalid()
	}
	expiryUnix, err := strconv.ParseInt(rawExpiry, 10, 64)
	if err != nil {
		return "", customers.ErrConfirmationLinkInvalid()
	}
	// Expiry is a separate outcome from invalidity: the holder of a genuine but
	// spent link is told their link has run out, not that it was forged.
	if now.After(time.Unix(expiryUnix, 0)) {
		return "", customers.ErrConfirmationLinkExpired()
	}
	return ticketSaleID, nil
}

// confirmationLinkPayload is what the token asserts and the signature covers:
// which Ticket Sale, and until when.
func confirmationLinkPayload(ticketSaleID string, expires time.Time) string {
	return ticketSaleID + ":" + strconv.FormatInt(expires.Unix(), 10)
}

func (s *Service) confirmationLinkMAC(payload string) []byte {
	mac := hmac.New(sha256.New, s.links.Secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// encodeSegment/decodeSegment keep the token URL-safe and unpadded so it survives
// an email client, a copy-paste, and a query string intact.
func encodeSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// Strict decoding rejects encodings with non-zero trailing bits, so one token
// string maps to one credential and a byte-identical payload cannot be spelled
// two ways.
func decodeSegment(s string) ([]byte, error) {
	return base64.RawURLEncoding.Strict().DecodeString(s)
}

// nullableString maps "" to SQL NULL, which for a Customer Session's
// ticket_sale_id is the difference between "spans everything this Customer owns"
// and "this one sale".
func nullableString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// isUUID checks the shape of the Ticket Sale id carried in a token before it
// reaches a uuid-typed column. The signature has already been verified by the
// time this runs, so it guards against our own malformed payload rather than an
// attacker — a database type error is a worse answer than "invalid link".
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
