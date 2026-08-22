package catalog

import (
	"testing"
	"time"
)

// The Assignment Link's format (#325, ADR 0046), tested where it is a pure
// function of a secret, a Ticket, an instant and an address — no database, no
// clock, no wiring.
//
// The integration suite proves the flow; this proves the properties the flow
// rests on, including the two that are hard to stage over HTTP: that a token of
// another purpose cannot verify here, and that two assignments sharing an
// instant still produce links that refuse each other.

var (
	signerSecret  = []byte("a deployment's link secret")
	signedTicket  = "11111111-2222-4333-8444-555555555555"
	otherTicket   = "99999999-2222-4333-8444-555555555555"
	signedAtStamp = time.Date(2026, 8, 22, 12, 0, 0, 123456000, time.UTC)
)

func TestAssignmentLinkOpensExactlyWhatItNames(t *testing.T) {
	signer := NewAssignmentLinkSigner(signerSecret)
	token, ok := signer.Sign(signedTicket, signedAtStamp, "carla@example.com")
	if !ok {
		t.Fatal("a configured signer minted nothing")
	}

	ticketID, micros, fingerprint, ok := signer.Parse(token)
	if !ok || ticketID != signedTicket {
		t.Fatalf("parse gave (%q, %d, ok=%v), want the Ticket it named", ticketID, micros, ok)
	}
	if !signer.NamesAssignment(micros, fingerprint, &signedAtStamp, "carla@example.com") {
		t.Fatal("a freshly minted token does not name the assignment it was minted for")
	}

	// THE ADDRESS IS NOT IN THE TOKEN. Whoever comes to hold this link — a mail
	// forward, a shared screen — learns no third party's email from it.
	if contains(token, "carla") || contains(token, "example.com") {
		t.Error("the token carries the Holder's address in a readable form")
	}
}

// A REASSIGNMENT KILLS THE OLD LINK EVEN WHEN BOTH ASSIGNMENTS SHARE AN INSTANT.
//
// This is the case a timestamp alone cannot see: a buyer correcting a typo the
// moment they made it, or any clock coarser than the two writes. Without the
// address fingerprint, the previous address would accept a Ticket it no longer
// holds — a Verified Customer minted for somebody who was handed nothing, which
// is the one failure this feature cannot have.
func TestAnAssignmentLinkDoesNotOpenAnotherAddressesAssignment(t *testing.T) {
	signer := NewAssignmentLinkSigner(signerSecret)
	carla, _ := signer.Sign(signedTicket, signedAtStamp, "carla@example.com")

	_, micros, fingerprint, ok := signer.Parse(carla)
	if !ok {
		t.Fatal("the token does not parse")
	}
	// The same Ticket, the same instant, a different Holder.
	if signer.NamesAssignment(micros, fingerprint, &signedAtStamp, "elena@example.com") {
		t.Fatal("Carla's link names Elena's assignment; a token that only knew WHEN would let a previous address accept")
	}
	// The same Holder, handed the Ticket back later: they were mailed a new link,
	// and the old one stays dead.
	later := signedAtStamp.Add(time.Hour)
	if signer.NamesAssignment(micros, fingerprint, &later, "carla@example.com") {
		t.Fatal("an old link resumed working after the Ticket came back to the same address")
	}
	// And an unassigned Ticket names nobody's assignment.
	if signer.NamesAssignment(micros, fingerprint, nil, "") {
		t.Fatal("a link opened a Ticket with no assignment at all")
	}
}

// AN UNCONFIGURED SIGNER MINTS NOTHING AND OPENS NOTHING. Refusing beats signing
// with a zero key that anybody holding a copy of this source could forge into an
// acceptance of somebody else's Ticket.
func TestAnUnconfiguredAssignmentSignerRefuses(t *testing.T) {
	signer := NewAssignmentLinkSigner(nil)
	if signer.Configured() {
		t.Fatal("a signer with no secret reports itself configured")
	}
	if _, ok := signer.Sign(signedTicket, signedAtStamp, "carla@example.com"); ok {
		t.Fatal("a signer with no secret minted a token")
	}
	real := NewAssignmentLinkSigner(signerSecret)
	token, _ := real.Sign(signedTicket, signedAtStamp, "carla@example.com")
	if _, _, _, ok := signer.Parse(token); ok {
		t.Fatal("a signer with no secret opened a real token")
	}
}

// A TOKEN SIGNED BY ANOTHER DEPLOYMENT, TAMPERED WITH, OR TRUNCATED IS REFUSED
// BEFORE ANY OF ITS PAYLOAD IS TRUSTED.
func TestAssignmentLinkRefusesForgedAndTruncatedTokens(t *testing.T) {
	signer := NewAssignmentLinkSigner(signerSecret)
	elsewhere := NewAssignmentLinkSigner([]byte("another deployment"))

	foreign, _ := elsewhere.Sign(otherTicket, signedAtStamp, "carla@example.com")
	if _, _, _, ok := signer.Parse(foreign); ok {
		t.Fatal("a token from another deployment verified here")
	}

	token, _ := signer.Sign(signedTicket, signedAtStamp, "carla@example.com")
	for name, bad := range map[string]string{
		"cut before the dot": token[:4],
		"cut after the dot":  token[:len(token)-8],
		"no MAC at all":      token[:index(token, '.')],
		"empty":              "",
		"not a token":        "hello there",
	} {
		if _, _, _, ok := signer.Parse(bad); ok {
			t.Errorf("%s verified", name)
		}
	}
}

// A NAME IS TWO NON-BLANK HALVES, TRIMMED AND BOUNDED, and never a Tax ID.
func TestParseHolderName(t *testing.T) {
	first, last, ok := ParseHolderName("  Carla ", "Ruiz  ")
	if !ok || first != "Carla" || last != "Ruiz" {
		t.Fatalf("ParseHolderName gave (%q, %q, %v)", first, last, ok)
	}
	for name, pair := range map[string][2]string{
		"no first name": {"   ", "Ruiz"},
		"no last name":  {"Carla", ""},
		"neither":       {"", ""},
		"too long":      {"Carla", repeat("z", MaxHolderNameLength+1)},
	} {
		if _, _, ok := ParseHolderName(pair[0], pair[1]); ok {
			t.Errorf("%s was accepted as a name", name)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func index(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return len(s)
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
