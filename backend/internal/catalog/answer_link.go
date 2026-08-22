package catalog

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// The Answer Link: a signed, stateless link opening ONE Ticket's Ticket
// Questions, for the buyer to pass to whoever will hold that ticket (#312,
// ADR 0044).
//
// It ANSWERS; IT DOES NOT TRANSFER. The Ticket Sale, the Sale Confirmation, the
// Customer Area entry and the Reversal Window all stay with the buyer, and
// holding one of these makes nobody a Customer. Nothing here mints a session,
// creates a Customer, or writes an identity anywhere — that is the decision, not
// an omission, and ADR 0044 records why: requiring proof of identity would mean
// collecting an address from someone who never came to this platform, which is
// the third-party-collection problem the whole feature was shaped to avoid.
//
// ITS SAFETY RESTS ON TWO THINGS AND NOTHING ELSE, because there is no
// authentication behind it. The first is that the PAGE DISCLOSES NOTHING about
// the purchase — see service.AnswerLinkView, which is a separate type from the
// staff view for exactly that reason. The second is that it stops opening at
// Event start and on a reversed Sale, which is catalog.AnswerWindow and is read
// LIVE rather than baked in here (see below).
//
// THREE SIGNED-LINK CONCEPTS NOW TRAVEL IN THE SAME FLOW — Confirmation Link,
// Consent Confirmation Link, Answer Link — and CONTEXT.md is explicit that none
// of them opens what the others do. That separation is enforced twice below: by
// a purpose label mixed into the signing key, and by a purpose prefix inside the
// signed payload. Either alone would do; both together mean a cross-purpose
// replay fails cryptographically AND fails on inspection.

const (
	// answerLinkPurpose is the label that makes this token's key its own.
	//
	// The signing key is DERIVED from the deployment's link secret rather than
	// used directly: k = HMAC(secret, purpose). That is one HMAC and it buys the
	// property CONTEXT.md asks for — a Confirmation Link token can never verify
	// as an Answer Link, whatever its payload happens to spell, because it was
	// signed under a different key. The alternative, a second environment
	// variable, would make a misconfigured deployment silently linkless, and the
	// derivation is standard practice (an HKDF-Expand with a fixed info label in
	// all but name).
	answerLinkPurpose = "ticket-answer-link.v1"

	// answerLinkPayloadPrefix opens every payload this signer produces.
	//
	// Redundant with the key derivation above ON PURPOSE. The derivation makes a
	// cross-purpose token fail to verify; this makes a cross-purpose token
	// visibly wrong to anybody reading a decoded payload in a log or a debugger,
	// and it gives the format a version number for the day the payload has to
	// carry a second field.
	answerLinkPayloadPrefix = "al1:"
)

// AnswerLinkSigner mints and verifies Answer Links.
//
// It is a value and not a service: signing is a pure function of the secret and
// the Ticket id, and keeping it that way is what lets the whole token format be
// unit-tested with no database, no clock and no wiring.
type AnswerLinkSigner struct {
	// key is the DERIVED key, never the deployment's raw secret. Derived once at
	// construction so that no caller can accidentally sign with the raw one.
	key []byte
}

// NewAnswerLinkSigner derives this purpose's key from the deployment's link
// secret.
//
// An empty secret produces an UNCONFIGURED signer rather than one signing with a
// zero key. Refusing to mint beats emitting a link signed with a constant that
// anybody holding a copy of the source could forge — the same call
// ConfirmationLinkURL makes.
func NewAnswerLinkSigner(secret []byte) AnswerLinkSigner {
	if len(secret) == 0 {
		return AnswerLinkSigner{}
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(answerLinkPurpose))
	return AnswerLinkSigner{key: mac.Sum(nil)}
}

// Configured reports whether this signer holds a key. False means the deployment
// set no link secret, which is a deployment fault and not a caller's: the
// service turns it into a 500 rather than into "your link is invalid", because
// telling somebody their link is broken when it is the server that is broken
// sends them to the buyer to ask for a new one that would fail identically.
func (s AnswerLinkSigner) Configured() bool { return len(s.key) > 0 }

// Sign returns the token naming one Ticket.
//
// WHAT THE TOKEN ENCODES IS THE TICKET AND NOTHING ELSE — no Ticket Sale, no
// Event, no Organization, no buyer, and NO EXPIRY. Each of those is deliberate.
//
// One Ticket, because "one Ticket's link must never open another's" is the
// acceptance criterion, and a token that named a Sale or an Event would open
// several by construction. The Ticket id is the narrowest thing there is to name.
//
// NO EXPIRY IS BAKED IN, which is the one place this deliberately departs from
// the Confirmation Link. That token bakes its expiry because redemption is
// meant to need no Event lookup; this one MUST read the Event anyway — the page
// shows the Event's name, and the Sale's status decides whether the link opens
// at all — so a baked expiry would buy nothing and cost the thing a second copy
// of a rule always costs: disagreement. An Organization that MOVES its Event a
// week later would otherwise have every Answer Link it ever handed out die on
// the old date, silently, with no way to reissue them into the group chats they
// were forwarded to. The expiry is catalog.AnswerWindow read against the Event
// as it stands now, which is the same rule Event Staff and the checkout capture
// are held to, evaluated in one place.
//
// The secret being absent is reported rather than papered over.
func (s AnswerLinkSigner) Sign(ticketID string) (string, bool) {
	if !s.Configured() {
		return "", false
	}
	payload := answerLinkPayloadPrefix + ticketID
	return encodeAnswerLinkSegment([]byte(payload)) + "." +
		encodeAnswerLinkSegment(s.mac(payload)), true
}

// Parse verifies a token and returns the Ticket id it names.
//
// THE SIGNATURE IS CHECKED BEFORE ANYTHING IN THE PAYLOAD IS TRUSTED, and
// compared in constant time. A token that was edited, truncated, retyped by hand
// or simply invented therefore fails here and never reaches a database lookup —
// which is what makes "a tampered or truncated link is refused" a property of
// the format rather than of whatever the query happens to do with a bad id.
//
// Truncation is refused by every one of three independent checks, and it is
// worth naming them because "truncated" is the failure a copy-paste out of a
// chat app actually produces: a token cut before the dot has no MAC segment and
// fails the Cut; one cut inside either segment fails strict base64 decoding or
// the MAC comparison; one cut after the dot has an empty MAC that cannot equal a
// 32-byte one.
//
// It returns only a bool. There is no "expired" outcome here because there is no
// expiry in the token — the window is the service's read of AnswerWindow — and
// no "which part was wrong" detail, because a holder is entitled to learn
// nothing beyond that the link did not work.
func (s AnswerLinkSigner) Parse(token string) (string, bool) {
	if !s.Configured() {
		return "", false
	}

	encodedPayload, encodedMAC, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found {
		return "", false
	}
	payload, err := decodeAnswerLinkSegment(encodedPayload)
	if err != nil {
		return "", false
	}
	mac, err := decodeAnswerLinkSegment(encodedMAC)
	if err != nil {
		return "", false
	}
	if !hmac.Equal(mac, s.mac(string(payload))) {
		return "", false
	}

	ticketID, ok := strings.CutPrefix(string(payload), answerLinkPayloadPrefix)
	if !ok || !IsUUID(ticketID) {
		// Signed by this key and still not a Ticket id: either a payload from a
		// future version of this format, or our own bug. Refusing beats handing
		// a database a value of the wrong shape and turning a refusal into a 500.
		return "", false
	}
	return ticketID, true
}

func (s AnswerLinkSigner) mac(payload string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// encodeAnswerLinkSegment/decodeAnswerLinkSegment keep the token URL-safe and
// unpadded, so it survives a query string, a copy-paste and a chat app's link
// detector intact — this token's whole life is being pasted into WhatsApp.
//
// Strict decoding refuses encodings with non-zero trailing bits, so one token
// string maps to one credential and a byte-identical payload cannot be spelled
// two ways.
func encodeAnswerLinkSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeAnswerLinkSegment(s string) ([]byte, error) {
	return base64.RawURLEncoding.Strict().DecodeString(s)
}

// IsUUID checks the shape of an id carried inside a signed payload before it
// reaches a uuid-typed column.
//
// The signature has already been verified by the time this runs, so it guards
// against our own malformed payload rather than against an attacker — a database
// type error is a worse answer than "this link is not valid".
func IsUUID(s string) bool {
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
