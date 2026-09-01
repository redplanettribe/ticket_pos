package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// WHAT THESE PIN (#565, ADR 0046, ADR 0067).
//
// The Staff Digest is the only thing standing between an access log and a
// roster of everybody who signs into the Staff platform, so every property it
// has is asserted here rather than inferred from the implementation — including
// the exact preimage, because a digest whose preimage drifts is a digest that
// silently stops naming the people the last release's links named.
//
// NO DATABASE AND NO CLOCK, which is the whole reason this is a value and not a
// service (NewAssignmentLinkSigner's shape, and NewReAddressingLinkSigner's).

const testSecret = "a-deployment-link-secret"

func TestStaffDigestIsThirtyTwoHexCharacters(t *testing.T) {
	digester := NewStaffDigester([]byte(testSecret))

	digest, ok := digester.Digest("ana@example.com")
	if !ok {
		t.Fatal("a configured digester must digest")
	}
	if len(digest) != 32 {
		t.Fatalf("digest length = %d, want 32: %q", len(digest), digest)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		t.Fatalf("digest is not hex: %q", digest)
	}
	if strings.ToLower(digest) != digest {
		t.Fatalf("digest must be lowercase hex, so one address has one spelling: %q", digest)
	}
}

// The preimage, spelled out independently of the implementation.
//
// A KEY DERIVED FROM THE MASTER, NEVER THE MASTER ITSELF. ADR 0046's whole
// convention is one deployment secret and a distinct key per purpose; HMACing
// under the raw secret — which customers/service/confirmationlink.go does, and
// which this deliberately does not copy — would make a digest and a link token
// share a key.
func TestStaffDigestIsTheDocumentedPreimageUnderADerivedKey(t *testing.T) {
	derive := hmac.New(sha256.New, []byte(testSecret))
	derive.Write([]byte("legal-staff-digest.v1"))
	key := derive.Sum(nil)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("staff:ana@example.com"))
	want := hex.EncodeToString(mac.Sum(nil))[:32]

	got, ok := NewStaffDigester([]byte(testSecret)).Digest("ana@example.com")
	if !ok || got != want {
		t.Fatalf("digest = %q, want %q — 32 hex chars of HMAC-SHA256(subkey, \"staff:\"+email)", got, want)
	}
}

// The `.v1` is the rotation handle: bumping it must change every digest without
// touching the master secret. Pinned by showing that a different purpose label
// over the same secret produces a different key and so a different digest.
func TestADifferentPurposeLabelProducesADifferentDigest(t *testing.T) {
	derive := hmac.New(sha256.New, []byte(testSecret))
	derive.Write([]byte("legal-staff-digest.v2"))
	rotated := hmac.New(sha256.New, derive.Sum(nil))
	rotated.Write([]byte("staff:ana@example.com"))
	v2 := hex.EncodeToString(rotated.Sum(nil))[:32]

	v1, _ := NewStaffDigester([]byte(testSecret)).Digest("ana@example.com")
	if v1 == v2 {
		t.Fatal("rotating the purpose label must rotate the digests; it did not")
	}
}

// The digest is derived from the NORMALISED address, so two spellings of one
// person are one person. Migration 114's CHECK stops an unnormalised address
// being stored in the first place; this is the second half of the belt and
// braces, and it is the half that covers a search box.
func TestStaffDigestFoldsTheAddressTheWayThePlatformStoresIt(t *testing.T) {
	digester := NewStaffDigester([]byte(testSecret))

	canonical, _ := digester.Digest("ana@example.com")
	for _, spelling := range []string{"Ana@Example.com", "  ana@example.com  ", "ANA@EXAMPLE.COM"} {
		got, _ := digester.Digest(spelling)
		if got != canonical {
			t.Fatalf("%q digested to %q, want %q — one person is one digest", spelling, got, canonical)
		}
	}
}

func TestTwoAddressesDigestDifferently(t *testing.T) {
	digester := NewStaffDigester([]byte(testSecret))
	first, _ := digester.Digest("ana@example.com")
	second, _ := digester.Digest("bruno@example.com")
	if first == second {
		t.Fatal("two people must not share a digest")
	}
}

// Two deployments must not agree on a digest: a digest computed under a leaked
// staging secret must not name anybody in production.
func TestTwoSecretsDigestDifferently(t *testing.T) {
	first, _ := NewStaffDigester([]byte("secret-one")).Digest("ana@example.com")
	second, _ := NewStaffDigester([]byte("secret-two")).Digest("ana@example.com")
	if first == second {
		t.Fatal("the digest must depend on the deployment secret")
	}
}

// AN UNCONFIGURED DIGESTER COMPUTES NOTHING, which is what lets the screens
// refuse to serve rather than fall back to an empty key: digests under a
// constant key are digests anybody with a copy of this source could reproduce,
// and an empty digest would make every row on a page name the same person.
func TestAnUnconfiguredDigesterDigestsNothingAndMatchesNothing(t *testing.T) {
	digester := NewStaffDigester(nil)

	if digester.Configured() {
		t.Fatal("no secret must mean not configured")
	}
	if digest, ok := digester.Digest("ana@example.com"); ok || digest != "" {
		t.Fatalf("unconfigured Digest = (%q, %v), want (\"\", false)", digest, ok)
	}
	// And the empty digest must not be a skeleton key into the empty address.
	if digester.Matches("", "") {
		t.Fatal("an unconfigured digester must match nothing at all")
	}
}

func TestMatchesRecognisesOnlyTheAddressThatProducedTheDigest(t *testing.T) {
	digester := NewStaffDigester([]byte(testSecret))
	digest, _ := digester.Digest("ana@example.com")

	if !digester.Matches(digest, "ana@example.com") {
		t.Fatal("a digest must match the address it was minted from")
	}
	if !digester.Matches(digest, "  Ana@Example.com ") {
		t.Fatal("a digest must match that address however it is spelled")
	}
	if digester.Matches(digest, "bruno@example.com") {
		t.Fatal("a digest must not match somebody else")
	}
	if digester.Matches("", "ana@example.com") {
		t.Fatal("the empty digest must match nobody")
	}
}
