package catalog

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// The Assignment Link: the signed token mailed to a Holder's address, whose
// click accepts one Ticket Assignment (#325, parent #322, ADR 0046).
//
// IT IS THE FOURTH SIGNED LINK AND THE ONLY ONE THAT MINTS AN IDENTITY.
// Confirmation Link, Consent Confirmation Link and Answer Link open a purchase,
// resolve a consent and answer a question; this one turns a stranger into a
// Verified Customer, because clicking from the inbox is Proof of Email Ownership
// (ADR 0035). That makes conflating it with any of the other three worse than a
// naming slip, which is why it derives its own key under its own purpose label,
// carries its own payload prefix, and is spelled `accept` from end to end —
// `confirm` is taken four times over and is banned here by ADR 0046.
//
// IT IS DISTINCT FROM THE ANSWER LINK, AND THAT IS THE WHOLE SECURITY PROPERTY
// OF THE FEATURE. The Answer Link is copyable off the buyer's own sale page, so
// a design that reused it would let the buyer accept on their friend's behalf,
// and the Verified Customer minted from it would be a fiction. This token is
// delivered ONLY to the address, never to a buyer surface and never in an API
// response to the buyer. ADR 0046: an Assignment Link appearing on a buyer
// surface is a defect of the same severity as leaking the token itself.
//
// NO TICKET ID EVER APPEARS IN A URL PATH. The token names the Ticket. A path
// segment naming it would be a second, unsigned way to say which Ticket this is,
// and the two could disagree.

const (
	// assignmentLinkPurpose is the label that makes this token's key its own.
	//
	// Derived as k = HMAC(link secret, purpose), exactly as the Answer Link's is,
	// so that an Answer Link token can never verify as an Assignment Link
	// whatever its payload spells — it was signed under a different key. Here
	// that separation is not merely tidy: it is what stops a token the buyer can
	// copy from being replayed into an acceptance.
	assignmentLinkPurpose = "ticket-assignment-link.v1"

	// assignmentLinkPayloadPrefix opens every payload this signer produces.
	//
	// Redundant with the key derivation on purpose, as `al1:` is for the Answer
	// Link: the derivation makes a cross-purpose token fail to verify, and this
	// makes one visibly wrong to a person reading a decoded payload.
	assignmentLinkPayloadPrefix = "asl1:"
)

// AssignmentLinkSigner mints and verifies Assignment Links.
//
// A value and not a service, like AnswerLinkSigner: signing is a pure function
// of the secret, the Ticket and the moment the address was named, which is what
// lets the whole format be tested with no database, no clock and no wiring.
type AssignmentLinkSigner struct {
	// key is the DERIVED key and never the deployment's raw secret.
	key []byte
}

// NewAssignmentLinkSigner derives this purpose's key from the deployment's link
// secret. An empty secret produces an UNCONFIGURED signer, which mints nothing:
// refusing beats signing with a zero key that anybody holding this source could
// forge into an acceptance of somebody else's Ticket.
func NewAssignmentLinkSigner(secret []byte) AssignmentLinkSigner {
	if len(secret) == 0 {
		return AssignmentLinkSigner{}
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(assignmentLinkPurpose))
	return AssignmentLinkSigner{key: mac.Sum(nil)}
}

// Configured reports whether this signer holds a key. False means the deployment
// set no link secret, which is a deployment fault and not the Holder's.
func (s AssignmentLinkSigner) Configured() bool { return len(s.key) > 0 }

// Sign returns the token naming one Ticket and the exact assignment it was
// minted for.
//
// THREE FIELDS, AND EACH OF THE LAST TWO KILLS A DIFFERENT STALE LINK.
//
// The Ticket id, because "one Ticket's link must never open another's" is the
// narrowest thing there is to name — a token naming a Sale or an Event would
// open several by construction.
//
// The instant that Ticket's CURRENT address was named (assigned_at, migration
// 080), so that a buyer who reassigns kills every link mailed before. The open
// compares it against the row.
//
// A FINGERPRINT OF THE ADDRESS, which is what makes that property hold even when
// two assignments land in the same microsecond — a frozen clock in a test, a
// buyer correcting a typo the instant they made it, or a column that stores less
// precision than the clock that fed it. Without it, "the buyer reassigned this
// Ticket to somebody else" and "nothing changed" can look identical to a token,
// and the previous address would accept a Ticket it no longer holds. A timestamp
// alone is a race; the address is the fact.
//
// THE FINGERPRINT IS A MAC AND NOT THE ADDRESS ITSELF. A token carrying the
// address in the clear would disclose a third party's email to anybody who came
// to hold a copy of the link — a forward, a shared screen, a support ticket —
// and this token is already the most sensitive one the platform mints. What is
// carried instead is unforgeable without the key and says nothing to a reader.
//
// MICROSECONDS, because that is the resolution a TIMESTAMPTZ keeps. Signing a
// nanosecond value would mint a token that never matched the row it was read
// back from.
//
// NO EXPIRY IS BAKED IN, exactly as the Answer Link bakes none: the open must
// read the Event anyway — for its name, its start and the Sale's status — so a
// baked deadline would buy nothing and could disagree with the Event after an
// Organization moves it.
func (s AssignmentLinkSigner) Sign(ticketID string, assignedAt time.Time, holderEmail string) (string, bool) {
	if !s.Configured() {
		return "", false
	}
	payload := assignmentLinkPayloadPrefix + ticketID + ":" +
		strconv.FormatInt(assignedAt.UnixMicro(), 10) + ":" +
		s.Fingerprint(holderEmail)
	return encodeLinkSegment([]byte(payload)) + "." +
		encodeLinkSegment(s.mac(payload)), true
}

// Fingerprint is the unforgeable, non-disclosing stand-in for one address.
//
// Derived under this signer's own key with its own label, so it cannot be
// computed by anybody holding a link and cannot be replayed as anything else.
// Truncated because it is a comparison and not a credential: twelve bytes is far
// beyond what an attacker who must ALSO produce a valid signature could grind.
//
// The address must already be normalised — every address this platform stores
// has been through platform.NormalizeEmail — so the same person's address
// fingerprints the same way whether the buyer typed it shouting or whispering.
func (s AssignmentLinkSigner) Fingerprint(holderEmail string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("holder:" + holderEmail))
	return encodeLinkSegment(mac.Sum(nil)[:12])
}

// Parse verifies a token and returns what it names: the Ticket, the assignment
// instant it was minted for, and the fingerprint of the address it was mailed
// to.
//
// THE SIGNATURE IS CHECKED BEFORE ANY OF THE PAYLOAD IS TRUSTED, in constant
// time, so a token that was edited, truncated, retyped or invented never reaches
// a database lookup. It returns only a bool: whoever holds a link that does not
// work is entitled to learn nothing beyond that fact.
func (s AssignmentLinkSigner) Parse(token string) (ticketID string, assignedAtMicros int64, fingerprint string, ok bool) {
	if !s.Configured() {
		return "", 0, "", false
	}

	encodedPayload, encodedMAC, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found {
		return "", 0, "", false
	}
	payload, err := decodeLinkSegment(encodedPayload)
	if err != nil {
		return "", 0, "", false
	}
	mac, err := decodeLinkSegment(encodedMAC)
	if err != nil {
		return "", 0, "", false
	}
	if !hmac.Equal(mac, s.mac(string(payload))) {
		return "", 0, "", false
	}

	body, cut := strings.CutPrefix(string(payload), assignmentLinkPayloadPrefix)
	if !cut {
		return "", 0, "", false
	}
	id, rest, split := strings.Cut(body, ":")
	if !split || !IsUUID(id) {
		return "", 0, "", false
	}
	micros, print, split := strings.Cut(rest, ":")
	if !split || print == "" {
		return "", 0, "", false
	}
	stamp, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		// Signed by this key and still not the format we mint: a payload from a
		// future version, or our own bug. Refusing beats handing a comparison a
		// value of the wrong shape.
		return "", 0, "", false
	}
	return id, stamp, print, true
}

// NamesAssignment reports whether a verified token still names the assignment a
// Ticket currently carries: the same address, named at the same moment.
//
// BOTH HALVES, AND NEITHER IS REDUNDANT. The address is what a reassignment
// really changes; the timestamp is what changes when a Ticket is handed away and
// later handed back to the same person, whose old link must not resume working —
// they were mailed a new one.
//
// A FUNCTION RATHER THAN A COMPARISON AT THE CALL SITE, because the rounding is
// the subtle part: the row comes back from Postgres in microseconds and the
// value that was signed came from a Go clock that keeps nanoseconds. Comparing
// two time.Times directly would make every token look stale on a deployment
// whose clock ticks finer than the column stores.
func (s AssignmentLinkSigner) NamesAssignment(
	signedMicros int64,
	fingerprint string,
	assignedAt *time.Time,
	holderEmail string,
) bool {
	if assignedAt == nil || holderEmail == "" {
		return false
	}
	if assignedAt.UnixMicro() != signedMicros {
		return false
	}
	return hmac.Equal([]byte(fingerprint), []byte(s.Fingerprint(holderEmail)))
}

func (s AssignmentLinkSigner) mac(payload string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// MaxHolderNameLength caps each half of a Holder's name.
//
// The same 100 the Customer's own name fields are held to at checkout, because
// it IS the Customer's name: accepting writes what the Holder types onto their
// Customer record as their current asserted name (ADR 0005, stored separately).
const MaxHolderNameLength = 100

// ParseHolderName turns what the Holder typed into the two halves that will be
// stored, or reports that it is not a name.
//
// BOTH HALVES ARE REQUIRED AND STORED SEPARATELY, per ADR 0005 and #325's
// acceptance criteria. A single "full name" field would have to be split by
// guesswork later, and this platform's market has two of each.
//
// WHAT IS NOT ASKED FOR HERE IS THE POINT. There is no Tax ID, no phone, no
// password and no passcode: a Holder is a person doing the platform a favour by
// saying what size t-shirt they wear, and a Tax ID is a fact about the SALE'S
// BUYER that no attendee is ever asked for (ADR 0046).
func ParseHolderName(firstName, lastName string) (string, string, bool) {
	first := strings.TrimSpace(firstName)
	last := strings.TrimSpace(lastName)
	if first == "" || last == "" {
		return "", "", false
	}
	if len(first) > MaxHolderNameLength || len(last) > MaxHolderNameLength {
		return "", "", false
	}
	return first, last, true
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

// encodeLinkSegment/decodeLinkSegment keep a signed token URL-safe and
// unpadded, so it survives a query string, a mail client and a copy-paste
// intact.
//
// Strict decoding refuses encodings with non-zero trailing bits, so one token
// string maps to one credential and a byte-identical payload cannot be spelled
// two ways.
func encodeLinkSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeLinkSegment(s string) ([]byte, error) {
	return base64.RawURLEncoding.Strict().DecodeString(s)
}
