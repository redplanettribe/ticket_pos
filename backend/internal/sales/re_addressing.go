package sales

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Sale Re-addressing: the Operator's act of moving an Online Sale to the address
// its buyer meant, completed only when that address accepts by Re-addressing
// Link (#420, parent #419, ADR 0058).
//
// THE VERB IS ACCEPT AND THE ACT IS RE-ADDRESS. "Claim" and "transfer" are
// barred by the glossary — a Holder accepts a Ticket Assignment, a corrected
// address accepts a re-addressing — and nothing in this file, its neighbours or
// the mail is spelled otherwise.

// ReAddressingState is the derived state of one Sale Re-addressing record. It is
// never stored (migration 093): it is read off the record's own timestamps and
// the Sale and Event beside it.
type ReAddressingState string

const (
	// ReAddressingPending: recorded, mailed, and nothing has happened since —
	// and the Sale is still active with its Event still ahead, so the link
	// opens.
	ReAddressingPending ReAddressingState = "pending"
	// ReAddressingAccepted: the corrected address clicked and the Sale moved.
	ReAddressingAccepted ReAddressingState = "accepted"
	// ReAddressingWithdrawn: the Operator withdrew it, or replaced it.
	ReAddressingWithdrawn ReAddressingState = "withdrawn"
	// ReAddressingExpired: nobody ended it, but the Sale was reversed or the
	// Event started while it was pending, so the link refuses and nothing can
	// complete it. Read from OTHER tables, which is why the state is derived.
	ReAddressingExpired ReAddressingState = "expired"
)

// DeriveReAddressingState reads the state off a record's ends and the world
// around it. The two ends win first — an accepted record stays accepted whatever
// later happens to the Sale, and a withdrawn one is history — and only an
// unended record asks whether the Sale is still active and the Event still
// ahead.
//
// eventStartsAt is nil for an Event with no schedule, which cannot have started.
// The Event's timezone is already inside the instant: `events.starts_at` is a
// TIMESTAMPTZ fixed when the Organization set the schedule in its own zone, so
// comparing instants IS reading it in the Event's timezone — the same reading
// the Holder Address Purge makes.
func DeriveReAddressingState(acceptedAt, withdrawnAt *time.Time, saleStatus string, eventStartsAt *time.Time, now time.Time) ReAddressingState {
	if acceptedAt != nil {
		return ReAddressingAccepted
	}
	if withdrawnAt != nil {
		return ReAddressingWithdrawn
	}
	if saleStatus != platform.ActiveSaleStatus || EventHasStarted(eventStartsAt, now) {
		return ReAddressingExpired
	}
	return ReAddressingPending
}

// EventHasStarted reports whether an Event's doors have opened by now. An Event
// with no start (nil) has not started — but no Online Sale belongs to one,
// since publishing requires a schedule.
func EventHasStarted(eventStartsAt *time.Time, now time.Time) bool {
	return eventStartsAt != nil && !now.Before(*eventStartsAt)
}

// MaxReAddressingNoteLength bounds the Operator's note, matching the schema's
// CHECK and the Operator Reversal's note for the same reason: one sentence for
// a human.
const MaxReAddressingNoteLength = 500

// MaxCorrectedEmailLength is the RFC 5321 bound every other address on this
// platform is held to.
const MaxCorrectedEmailLength = 254

// ParseCorrectedEmail turns what the Operator typed into the address that will
// be stored and mailed, or reports that it is not one.
//
// NORMALISED FIRST, as every Customer address is, so the Verified Customer #421
// mints or matches from the click is the person the Operator named and not a
// second record one capital letter away. The shape check is mail.ParseAddress,
// as at every other door — a weak check by design, since an address is only
// really validated by mail arriving at it, and here the mail IS the validation.
// It refuses the display-name form (`Ana <ana@example.com>`) that ParseAddress
// otherwise accepts, on the Holder address's rule: what is stored must be an
// address and nothing else, because it travels into a mail header.
func ParseCorrectedEmail(raw string) (string, bool) {
	email := platform.NormalizeEmail(raw)
	if email == "" || len(email) > MaxCorrectedEmailLength {
		return "", false
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil {
		return "", false
	}
	if platform.NormalizeEmail(parsed.Address) != email {
		return "", false
	}
	return email, true
}

// The Re-addressing Link: the signed token mailed to the corrected address,
// whose click accepts one Sale Re-addressing (ADR 0058).
//
// IT IS IN THE ASSIGNMENT LINK'S FAMILY AND IS THE SECOND TOKEN THAT MINTS AN
// IDENTITY. Clicking it is Proof of Email Ownership (ADR 0035): #421 mints or
// matches a Verified Customer from it exactly as accepting an Assignment Link
// does. That is why it derives its own key under its own purpose label and
// carries its own payload prefix — an Assignment Link, a Confirmation Link and
// this must never open what the others do, and the separation is cryptographic
// rather than a check somebody could forget.
//
// DELIVERED TO THE CORRECTED ADDRESS ALONE, NEVER TO THE OPERATOR. It appears in
// no Operator or staff response, so the Operator cannot complete an acceptance
// on the buyer's behalf and the click stays a proof. The only place one is
// composed is the mail, and the integration suite reads it from the captured
// sender the way a person reads it from an inbox.
//
// BOUND TO THE RECORD'S ID AND ITS requested_at. A withdrawal ends the record,
// and a replacement ends it and opens a new one with its own id and instant, so
// every earlier link stops verifying against the row #421 reads — with no
// revocation list and nothing to remember. The record id alone would already be
// unique; the instant is carried so that even a row somebody managed to reopen
// with the same id would not resurrect an old link. NO EXPIRY IS BAKED IN: the
// open must read the Sale and its Event anyway (for the name, the status and the
// start), and a baked deadline could disagree with an Event an Organization
// moved.

const (
	// reAddressingLinkPurpose is the label that makes this token's key its own:
	// k = HMAC(link secret, purpose), as the Assignment Link derives its key, so
	// that no token from another purpose verifies here whatever its payload
	// spells.
	reAddressingLinkPurpose = "sale-re-addressing-link.v1"

	// reAddressingLinkPayloadPrefix opens every payload this signer produces —
	// redundant with the key derivation on purpose, so a decoded payload is
	// visibly what it is to a person reading it.
	reAddressingLinkPayloadPrefix = "srl1:"
)

// ReAddressingLinkSigner mints and verifies Re-addressing Links. A value and not
// a service: signing is a pure function of the secret, the record and its
// instant, which is what lets the format be tested with no database and no
// clock.
type ReAddressingLinkSigner struct {
	// key is the DERIVED key and never the deployment's raw secret.
	key []byte
}

// NewReAddressingLinkSigner derives this purpose's key from the deployment's
// link secret. An empty secret produces an UNCONFIGURED signer, which mints
// nothing: refusing beats signing with a zero key anybody holding this source
// could forge into an acceptance of somebody else's Sale.
func NewReAddressingLinkSigner(secret []byte) ReAddressingLinkSigner {
	if len(secret) == 0 {
		return ReAddressingLinkSigner{}
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(reAddressingLinkPurpose))
	return ReAddressingLinkSigner{key: mac.Sum(nil)}
}

// Configured reports whether this signer holds a key. False means the
// deployment set no link secret — a deployment fault, not the Operator's.
func (s ReAddressingLinkSigner) Configured() bool { return len(s.key) > 0 }

// Sign returns the token naming one Sale Re-addressing record and the instant
// it was recorded. MICROSECONDS, because that is the resolution a TIMESTAMPTZ
// keeps; a nanosecond value would mint a token that never matched the row it
// was read back from.
func (s ReAddressingLinkSigner) Sign(recordID string, requestedAt time.Time) (string, bool) {
	if !s.Configured() {
		return "", false
	}
	payload := reAddressingLinkPayloadPrefix + recordID + ":" +
		strconv.FormatInt(requestedAt.UnixMicro(), 10)
	return encodeLinkSegment([]byte(payload)) + "." +
		encodeLinkSegment(s.mac(payload)), true
}

// Parse verifies a token and returns what it names: the record and the instant
// it was minted for. THE SIGNATURE IS CHECKED BEFORE ANY OF THE PAYLOAD IS
// TRUSTED, in constant time, so a token that was edited, truncated, retyped or
// invented never reaches a database lookup. It returns only a bool: whoever
// holds a link that does not work is entitled to learn nothing beyond that.
func (s ReAddressingLinkSigner) Parse(token string) (recordID string, requestedAtMicros int64, ok bool) {
	if !s.Configured() {
		return "", 0, false
	}
	encodedPayload, encodedMAC, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found {
		return "", 0, false
	}
	payload, err := decodeLinkSegment(encodedPayload)
	if err != nil {
		return "", 0, false
	}
	mac, err := decodeLinkSegment(encodedMAC)
	if err != nil {
		return "", 0, false
	}
	if !hmac.Equal(mac, s.mac(string(payload))) {
		return "", 0, false
	}
	body, cut := strings.CutPrefix(string(payload), reAddressingLinkPayloadPrefix)
	if !cut {
		return "", 0, false
	}
	id, micros, split := strings.Cut(body, ":")
	if !split || !isUUID(id) {
		return "", 0, false
	}
	stamp, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return id, stamp, true
}

// NamesRecord reports whether a verified token still names the recording a row
// carries: the same instant, at the precision the column keeps. A function
// rather than a comparison at the call site because the rounding is the subtle
// part — the row comes back in microseconds and the signed value came from a
// Go clock that keeps nanoseconds.
func (s ReAddressingLinkSigner) NamesRecord(signedMicros int64, requestedAt time.Time) bool {
	return requestedAt.UnixMicro() == signedMicros
}

func (s ReAddressingLinkSigner) mac(payload string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// isUUID checks the shape of an id carried inside a signed payload before it
// reaches a uuid-typed column. The signature has already been verified, so this
// guards against our own malformed payload rather than an attacker.
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

// encodeLinkSegment/decodeLinkSegment keep a signed token URL-safe and
// unpadded, so it survives a query string, a mail client and a copy-paste
// intact. Strict decoding refuses encodings with non-zero trailing bits, so one
// token string maps to one credential.
func encodeLinkSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeLinkSegment(s string) ([]byte, error) {
	return base64.RawURLEncoding.Strict().DecodeString(s)
}
