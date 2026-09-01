package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// The Staff Digest: the stand-in for a staff person's email address on any
// surface that must name one without disclosing it (#565, parent #556,
// ADR 0067, ADR 0046).
//
// A CUSTOMER HAS AN ID AND A STAFF PERSON DOES NOT. Customer identity is UUID
// -keyed, so the acceptance browser's customer half links to a person by their
// row id and nobody's address ever leaves the response body. The person key of
// the Staff platform is the EMAIL a session names — migrations 067, 069 and 107
// each argued it, and each was right: a Platform Operator may hold no `members`
// row at all, a person with three Organizations holds three, and the acceptance
// belongs to the person. There is nothing that IS a staff person to reference.
//
// So the staff acceptance browser has a row per address and needs a link out of
// it, and #565's rule is absolute: A DATA SUBJECT'S EMAIL ADDRESS MUST NEVER
// APPEAR IN A URL, A QUERY STRING OR A REFERER. Reading about somebody must not
// leak them into an access log, a proxy log, a browser history or the Referer
// header the next request sends. This digest is what a link carries instead.
//
// NO NEW SECRET AND NO NEW ENVIRONMENT VARIABLE, so shipping the Legal Center
// needs no `terraform apply`: the key is derived from the deployment's existing
// CONFIRMATION_LINK_SECRET under a purpose label, which is ADR 0046's whole
// convention — one deployment secret, a distinct key per purpose, so no token
// or digest minted for one purpose is meaningful under another.

const (
	// staffDigestPurpose is the label that makes this digest's key its own.
	//
	// Derived as k = HMAC(link secret, purpose), exactly as the Assignment
	// Link's and the Re-addressing Link's keys are, so a digest computed here
	// is unrelated to every token those mint even though all three descend
	// from one deployment secret.
	//
	// THE `.v1` IS THE ROTATION HANDLE, and it is the reason the version lives
	// in the purpose string rather than nowhere. Bumping it to `.v2` changes
	// every digest this platform computes without touching the master secret
	// and without invalidating a single Assignment Link, Re-addressing Link or
	// Confirmation Link. That is only cheap because a digest is NEVER STORED
	// (see below): rotating it invalidates some bookmarks and nothing else.
	staffDigestPurpose = "legal-staff-digest.v1"

	// staffDigestSubjectPrefix opens the preimage: "staff:" + the normalised
	// address.
	//
	// It is not decoration. The key is this purpose's alone, so nothing else
	// signs under it today — but a second kind of subject reached by digest on
	// a later screen (an Organization, a Customer, a support thread) would
	// otherwise share a preimage space with this one, and two subjects that
	// could collide are two subjects one link could confuse. Naming the kind
	// costs six bytes and closes that off before it opens.
	staffDigestSubjectPrefix = "staff:"

	// staffDigestHexLength is how much of the MAC a digest carries: 32 hex
	// chars, which is 16 bytes.
	//
	// TRUNCATED BECAUSE THIS IS A LOOKUP KEY AND NOT A CREDENTIAL. Holding a
	// digest authorises nothing — every screen that accepts one is already
	// behind the operator allowlist — so its only job is to name one person
	// unambiguously and to say nothing about them to a reader. 128 bits is far
	// beyond collision range for a population of dozens, and a 64-character
	// URL segment would be an eyesore for no gain.
	//
	// It is NOT reversible by anybody without the key, and it is NOT reversible
	// by dictionary attack from anybody with a list of addresses UNLESS they
	// also hold the key — which is the same posture the Assignment Link's
	// address fingerprint takes.
	staffDigestHexLength = 32
)

// StaffDigester turns a staff person's address into the 32 hex characters that
// stand for them in a URL and on a screen.
//
// A VALUE AND NOT A SERVICE, following NewAssignmentLinkSigner and
// NewReAddressingLinkSigner: computing a digest is a pure function of the
// deployment secret and the address, so the whole of it is testable with no
// database, no clock and no wiring.
//
// It is deliberately ONE-WAY AND ONE-WAY ONLY. There is no Parse, no Reverse and
// no lookup table, because there must not be: a screen resolves a digest by
// digesting the population it is already entitled to read and matching, which
// takes a query over dozens of rows and leaves no reverse index for anybody to
// find. Adding an inverse would mean storing the pairs, and see below.
//
// ============================================================================
// STANDING RULE: THE DIGEST IS A URL KEY AND A SCREEN LABEL, AND IS WRITTEN TO
// NO ROW, NO LOG, NO FILE AND NO EXPORT.
// ============================================================================
//
// It is computed on the way out of a read and discarded. Persisting it would
// turn a key rotation — the `.v1` above, the cheapest safety valve this design
// has — into a data migration over append-only consent evidence, which is
// exactly the kind of table nothing may rewrite. Logging it would defeat the
// point of having it: a log line pairing a digest with a request that also
// carries the operator's session is a log line that has stored the mapping the
// digest exists to avoid storing.
type StaffDigester struct {
	// key is the DERIVED key and never the deployment's raw secret. The
	// mistake to avoid is customers/service/confirmationlink.go's, which HMACs
	// under the master directly.
	key []byte
}

// NewStaffDigester derives this purpose's key from the deployment's link
// secret.
//
// An empty secret produces an UNCONFIGURED digester, which computes nothing.
// The staff acceptance browser then REFUSES TO SERVE rather than fall back to a
// zero key: digests under a constant key are digests anybody holding a copy of
// this source can compute, which would turn the one identifier standing between
// an access log and a staff roster into a reversible one.
//
// The absence of the master secret is caught ONCE, in server.NewApp, where
// every other signed-link key is resolved (server.confirmationLinkSecret). It
// is NOT caught in platform.LoadConfig, and must never be: cmd/migrate calls
// LoadConfig with APP_ENV=production and no signing key of any kind, and moving
// the check there has failed every production deploy that tried it.
func NewStaffDigester(secret []byte) StaffDigester {
	if len(secret) == 0 {
		return StaffDigester{}
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(staffDigestPurpose))
	return StaffDigester{key: mac.Sum(nil)}
}

// Configured reports whether this digester holds a key. False means the
// deployment set no link secret — a deployment fault, not the operator's, and
// the browser says so rather than listing anybody.
func (d StaffDigester) Configured() bool { return len(d.key) > 0 }

// Digest returns the 32 hex characters that stand for one staff address.
//
// THE ADDRESS IS NORMALISED HERE, through the platform's one fold, so that
// `Ana@Example.com` and `ana@example.com` are one person and not two. That is
// belt and braces: migration 114 adds CHECK (email = lower(btrim(email))) to
// all four staff tables precisely so no unnormalised address can be stored to
// begin with. Both exist because the tables are written from two directions —
// Go paths that normalise, and hand-typed psql INSERTs into
// `platform_operators` that do not.
//
// Returns "" and false when unconfigured. A caller that ignored the bool would
// print an empty string where a person's key belongs, which is why every state
// this digester has is reported rather than defaulted.
func (d StaffDigester) Digest(email string) (string, bool) {
	if !d.Configured() {
		return "", false
	}
	mac := hmac.New(sha256.New, d.key)
	mac.Write([]byte(staffDigestSubjectPrefix + platform.NormalizeEmail(email)))
	return hex.EncodeToString(mac.Sum(nil))[:staffDigestHexLength], true
}

// Matches reports whether a digest presented in a URL names this address.
//
// CONSTANT TIME, through hmac.Equal, and not `==`. The comparison itself leaks
// nothing worth having here — the caller is already an authenticated operator —
// but a digest comparison written with `==` is the one a later reader copies to
// somewhere it does matter, and the two spellings cost the same.
//
// An unconfigured digester matches nothing, so a deployment with no secret
// cannot be walked into by presenting the empty digest.
func (d StaffDigester) Matches(digest, email string) bool {
	computed, ok := d.Digest(email)
	if !ok {
		return false
	}
	return hmac.Equal([]byte(computed), []byte(digest))
}

// ErrStaffDigestUnavailable is returned when a staff surface that must name
// people by digest is asked to serve on a deployment that configured no link
// secret.
//
// THE SCREEN REFUSES RATHER THAN FALLING BACK. There is no unkeyed digest, no
// empty-string digest and no "just show the email" mode: the first would be
// forgeable by anybody with a copy of this source, the second would make every
// row on the page name the same person, and the third would put a data
// subject's address in the URL that this whole mechanism exists to keep it out
// of.
//
// 503 and not 500: nothing is broken and no request was malformed — the
// deployment is missing a value, and the honest sentence is "this screen is not
// available here". It is unreachable in production, where server.NewApp refuses
// to start without CONFIRMATION_LINK_SECRET; it is reachable in a local stack
// that has been started deliberately without one.
func ErrStaffDigestUnavailable() apperror.DomainError {
	return apperror.New(
		"STAFF_DIGEST_UNAVAILABLE",
		"This screen is unavailable because the deployment has no link secret configured.",
		nil,
	)
}
