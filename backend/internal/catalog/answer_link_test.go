package catalog_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Answer Link's token format (#312, ADR 0044).
//
// This link is an unauthenticated URL that answers for a person, so the format
// itself carries security properties rather than merely encoding an id: one
// Ticket's token must never open another's, a tampered or truncated one must be
// refused, and a Confirmation Link must never verify as an Answer Link.

const (
	ticketA = "11111111-1111-4111-8111-111111111111"
	ticketB = "22222222-2222-4222-8222-222222222222"
)

func testSigner() catalog.AnswerLinkSigner {
	return catalog.NewAnswerLinkSigner([]byte("a-deployment-link-secret"))
}

func TestAnswerLinkRoundTripsTheTicketItNames(t *testing.T) {
	signer := testSigner()

	token, ok := signer.Sign(ticketA)
	if !ok {
		t.Fatal("a configured signer refused to sign")
	}

	got, ok := signer.Parse(token)
	if !ok {
		t.Fatal("a token this signer minted did not verify")
	}
	if got != ticketA {
		t.Fatalf("token opened %q, want %q", got, ticketA)
	}
}

// ONE TICKET'S LINK NEVER OPENS ANOTHER'S — the first acceptance criterion, and
// the reason the payload names a Ticket rather than a Sale or an Event.
func TestAnswerLinkOpensOnlyTheTicketItNames(t *testing.T) {
	signer := testSigner()

	tokenA, _ := signer.Sign(ticketA)
	tokenB, _ := signer.Sign(ticketB)

	if tokenA == tokenB {
		t.Fatal("two Tickets minted the same token")
	}

	opened, ok := signer.Parse(tokenB)
	if !ok {
		t.Fatal("B's token did not verify")
	}
	if opened == ticketA {
		t.Fatal("B's token opened A's Ticket")
	}
	if opened != ticketB {
		t.Fatalf("B's token opened %q, want %q", opened, ticketB)
	}
}

// A TAMPERED LINK IS REFUSED. Every one of these is somebody editing the token
// to point at a Ticket that is not theirs, which is the whole attack the
// signature exists to stop.
func TestAnswerLinkRefusesATamperedToken(t *testing.T) {
	signer := testSigner()
	token, _ := signer.Sign(ticketA)
	payload, mac, _ := strings.Cut(token, ".")

	// A's payload swapped for B's, keeping A's signature — the naive forgery.
	forgedPayload, _ := signer.Sign(ticketB)
	swapped, _, _ := strings.Cut(forgedPayload, ".")

	for name, tampered := range map[string]string{
		"payload swapped for another ticket's": swapped + "." + mac,
		"signature swapped":                    payload + "." + strings.Repeat("A", len(mac)),
		"one byte flipped in the payload":      flipLastRune(payload) + "." + mac,
		"one byte flipped in the signature":    payload + "." + flipLastRune(mac),
		"no dot at all":                        payload + mac,
		"empty":                                "",
		"not base64":                           "not-a-token.!!!!",
		"invented":                             "abcdefgh.ijklmnop",
	} {
		if _, ok := signer.Parse(tampered); ok {
			t.Errorf("%s: accepted a tampered token", name)
		}
	}
}

// A TRUNCATED LINK IS REFUSED — the failure a copy-paste out of a group chat
// actually produces, where a link detector stops at a character it did not like.
// Every prefix of a valid token must fail, not merely the interesting ones.
func TestAnswerLinkRefusesEveryTruncation(t *testing.T) {
	signer := testSigner()
	token, _ := signer.Sign(ticketA)

	for cut := 0; cut < len(token); cut++ {
		if _, ok := signer.Parse(token[:cut]); ok {
			t.Fatalf("accepted a token truncated to %d of %d bytes: %q", cut, len(token), token[:cut])
		}
	}
	// And the whole thing still works, so the loop above was not passing by
	// accident on a signer that refuses everything.
	if _, ok := signer.Parse(token); !ok {
		t.Fatal("the untruncated token did not verify")
	}
}

// A TOKEN FROM ANOTHER DEPLOYMENT'S SECRET IS REFUSED. Nothing about the format
// is secret; the key is the whole of the security.
func TestAnswerLinkRefusesAnotherSecretsToken(t *testing.T) {
	minted, _ := testSigner().Sign(ticketA)

	stranger := catalog.NewAnswerLinkSigner([]byte("some other deployment's secret"))
	if _, ok := stranger.Parse(minted); ok {
		t.Fatal("a token signed with another secret verified")
	}
}

// THREE TOKENS, THREE PURPOSES, AND NONE OPENS WHAT THE OTHERS DO (CONTEXT.md).
//
// The Confirmation Link is signed with the deployment's link secret DIRECTLY,
// in the same `base64(payload).base64(mac)` shape. This mints one by hand from
// the SAME secret an Answer Link signer was built on and asserts it does not
// verify — which is what the purpose label mixed into the derived key buys.
// Without that derivation both would hold one key, and only the payload's
// spelling would stand between a receipt link and a Ticket's questions.
func TestAnswerLinkRefusesAConfirmationLinkFromTheSameSecret(t *testing.T) {
	secret := []byte("a-deployment-link-secret")

	// Exactly how customers/service signs a Confirmation Link: the raw secret,
	// and a payload of "<ticket sale id>:<unix expiry>".
	payload := ticketA + ":9999999999"
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	confirmationLink := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if _, ok := catalog.NewAnswerLinkSigner(secret).Parse(confirmationLink); ok {
		t.Fatal("a Confirmation Link signed with the same secret opened a Ticket's questions")
	}
}

// AN UNCONFIGURED DEPLOYMENT MINTS NOTHING AND OPENS NOTHING, rather than
// signing with a zero key that anybody holding a copy of the source could forge.
func TestAnswerLinkWithoutASecretNeitherSignsNorParses(t *testing.T) {
	signer := catalog.NewAnswerLinkSigner(nil)

	if signer.Configured() {
		t.Fatal("a signer with no secret reported itself configured")
	}
	if _, ok := signer.Sign(ticketA); ok {
		t.Fatal("a signer with no secret minted a token")
	}
	// Including a token that is otherwise perfectly good.
	valid, _ := testSigner().Sign(ticketA)
	if _, ok := signer.Parse(valid); ok {
		t.Fatal("a signer with no secret verified a token")
	}
}

// A payload signed by this key that is not a Ticket id is refused rather than
// handed to a uuid column. It is our own bug that this guards against, not an
// attacker — see catalog.IsUUID.
func TestAnswerLinkRefusesASignedPayloadThatIsNotATicketID(t *testing.T) {
	signer := testSigner()

	for _, notAUUID := range []string{"", "1", "not-a-uuid", strings.Repeat("z", 36)} {
		token, ok := signer.Sign(notAUUID)
		if !ok {
			t.Fatal("signing refused")
		}
		if _, ok := signer.Parse(token); ok {
			t.Errorf("a signed payload %q verified as a Ticket id", notAUUID)
		}
	}
}

func flipLastRune(s string) string {
	if s == "" {
		return "A"
	}
	last := s[len(s)-1]
	if last == 'A' {
		return s[:len(s)-1] + "B"
	}
	return s[:len(s)-1] + "A"
}
