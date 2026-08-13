package openapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCustomerOpenAPIContract checks that the committed spec actually documents
// the Customer surface: the sign-in pair, the Confirmation Link redemption, the
// session read, sign-out, the Customer Area, the "My info" profile write, and
// the Follows, each with a typed success
// envelope rather than a bare one. It fails when handlers are added or renamed
// without regenerating the spec.
func TestCustomerOpenAPIContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(openAPISpecPath(t))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	for _, path := range []string{
		"/api/v1/customer/auth/otp/request",
		"/api/v1/customer/auth/otp/verify",
		"/api/v1/customer/auth/google/verify",
		"/api/v1/customer/auth/confirmation-link",
		// The consent submission (#251, parent #249): the only route that turns a
		// held sign-in into a Customer Session. It is asserted here because both
		// verify routes above can now answer with a consent-required outcome, and
		// an outcome with no documented endpoint to resolve it is a sign-in a
		// client cannot finish.
		"/api/v1/customer/auth/consent",
		// The confirmation link's endpoint (#255, parent #249), asserted for the
		// same reason: every Sale Confirmation for a checkout that pended
		// something now carries a link only this route can resolve, and those
		// mails live in inboxes for years. A path that vanished from the spec
		// would be a Pending Confirmation nobody can ever clear.
		"/api/v1/customer/consent/confirm",
		// The Consent Withdrawal surface's passcode door (#270, parent #265,
		// ADR 0039), asserted because it is the one door that must never mint a
		// Customer Session: a route that quietly disappeared from the spec would
		// leave the withdrawal-without-signing-in flow reachable only through the
		// sign-in door, which is the situation the ADR exists to end.
		"/api/v1/customer/consent/withdrawal/passcode/verify",
		"/api/v1/customer/auth/session",
		"/api/v1/customer/auth/logout",
		"/api/v1/customer/ticket-sales",
		"/api/v1/customer/profile",
		"/api/v1/customer/profile/avatar-upload-url",
		"/api/v1/customer/profile/avatar",
		// The Follows (#217, #218). ONE listing endpoint carrying every kind of
		// Follow, and a follow/unfollow pair per kind, each naming its subject by
		// the identifier that subject is addressed by everywhere else — the
		// Organization by slug, the Tag by canonical key. A second listing path
		// appearing here would be the contract breaking, not growing.
		"/api/v1/customer/follows",
		"/api/v1/customer/follows/organizations/{slug}",
		"/api/v1/customer/follows/tags/{canonicalKey}",
		// Suggested Follows (#231, ADR 0031): a SECOND READ beside the listing
		// above, never a field on it. It is here so that folding it back into
		// `/customer/follows` — which the explorer and every Event and
		// Organization page call — cannot happen without this test noticing.
		"/api/v1/customer/follow-suggestions",
		// The guest-facing Reversal Window read (#121). Unauthenticated, because
		// checkout is: the buyer who most wants to undo may have no account yet.
		"/api/v1/public/checkout/{clientTransactionId}/reversal",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %s — regenerate the spec with `make swagger`", path)
		}
	}

	for _, schema := range []string{
		"openapi.EnvelopeCustomerVerifyOTP",
		"openapi.EnvelopeCustomerVerifyGoogle",
		"openapi.EnvelopeCustomerSession",
		"openapi.EnvelopeCustomerArea",
		"openapi.EnvelopeCustomerProfile",
		"openapi.EnvelopeCustomerAvatarUpload",
		"openapi.EnvelopeCheckoutReversal",
		"openapi.EnvelopeCustomerFollows",
		"openapi.EnvelopeCustomerFollow",
		"openapi.EnvelopeCustomerFollowSuggestions",
		"openapi.EnvelopeCustomerConsentConfirmation",
		"openapi.EnvelopeCustomerConsentSubmission",
		"openapi.EnvelopeCustomerConsentWithdrawalProof",
	} {
		if _, ok := doc.Components.Schemas[schema]; !ok {
			t.Fatalf("missing typed envelope schema %s", schema)
		}
	}
}

func openAPISpecPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	return filepath.Join(root, "openapi", "openapi.yaml")
}
