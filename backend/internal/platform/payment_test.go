package platform

import (
	"context"
	"errors"
	"net/url"
	"testing"
)

// TestStubPaymentProviderInitiateBuildsTheInterstitialContract pins the stub
// redirect-URL contract the Storefront interstitial (ticket #84) is built
// against: path and query param names are load-bearing.
func TestStubPaymentProviderInitiateBuildsTheInterstitialContract(t *testing.T) {
	p := NewStubPaymentProvider("http://storefront.example")

	init, err := p.Initiate(context.Background(), PaymentInitiateInput{
		AmountCents:         3500,
		Currency:            "USD",
		ClientTransactionID: "ctid-123",
		Reference:           "Summer Concert",
		ResponseURL:         "http://storefront.example/checkout/return",
	})
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}

	parsed, err := url.Parse(init.RedirectURL)
	if err != nil {
		t.Fatalf("parse redirect url %q: %v", init.RedirectURL, err)
	}
	if parsed.Scheme != "http" || parsed.Host != "storefront.example" {
		t.Fatalf("redirect url origin = %s://%s, want the storefront origin", parsed.Scheme, parsed.Host)
	}
	if parsed.Path != StubPaymentPath {
		t.Fatalf("redirect url path = %q, want %q", parsed.Path, StubPaymentPath)
	}
	q := parsed.Query()
	if got := q.Get("client_transaction_id"); got != "ctid-123" {
		t.Fatalf("client_transaction_id = %q", got)
	}
	if got := q.Get("amount_cents"); got != "3500" {
		t.Fatalf("amount_cents = %q", got)
	}
	if got := q.Get("currency"); got != "USD" {
		t.Fatalf("currency = %q", got)
	}
	if got := q.Get("response_url"); got != "http://storefront.example/checkout/return" {
		t.Fatalf("response_url = %q", got)
	}
	if init.ProviderPaymentID == "" {
		t.Fatal("expected a provider payment id from the stub")
	}
}

// TestStubPaymentProviderConfirmFailsClosed pins the decline-by-default rule: a
// missing or unknown outcome is never an approval.
func TestStubPaymentProviderConfirmFailsClosed(t *testing.T) {
	p := NewStubPaymentProvider("http://storefront.example")
	ctx := context.Background()

	for name, params := range map[string]map[string]string{
		"approved": {StubPaymentOutcomeParam: StubPaymentOutcomeApproved},
		"declined": {StubPaymentOutcomeParam: StubPaymentOutcomeDeclined},
		"missing":  nil,
		"unknown":  {StubPaymentOutcomeParam: "yes-please"},
	} {
		conf, err := p.Confirm(ctx, PaymentConfirmInput{ClientTransactionID: "ctid-123", Params: params})
		if err != nil {
			t.Fatalf("%s: confirm: %v", name, err)
		}
		wantApproved := name == "approved"
		if conf.Approved != wantApproved {
			t.Fatalf("%s: approved = %v, want %v", name, conf.Approved, wantApproved)
		}
		if conf.ProviderTransactionID == "" {
			t.Fatalf("%s: expected a provider transaction id", name)
		}
	}
}

// TestStubPaymentProviderReverseSucceeds: the stub agrees to every reversal,
// which is what makes Customer-initiated Sale Reversal exercisable with no
// PayPhone credentials configured (ADR 0018). There is no service to ask and no
// money to give back, so yes is the honest answer.
func TestStubPaymentProviderReverseSucceeds(t *testing.T) {
	p := NewStubPaymentProvider("http://storefront.example")
	if err := p.Reverse(context.Background(), "ctid-123"); err != nil {
		t.Fatalf("reverse: %v, want the stub to agree", err)
	}
	if !p.SupportsReverse() {
		t.Fatal("SupportsReverse is false while Reverse succeeds; the pair moves together or the Undo is withheld from a sale it would have honoured")
	}
}

// TestStubPaymentProviderReverseDrivesBothFailureKinds: the stub can produce a
// definite refusal and an unknown outcome on demand, because a Reversal Request
// behaves completely differently on each (ADR 0024) and both paths have to be
// exercisable on a deployment with no PayPhone credentials — the same reason the
// stub agrees by default.
//
// Success stays the zero value, so nothing that merely constructs a stub can
// have changed behaviour by adding this.
func TestStubPaymentProviderReverseDrivesBothFailureKinds(t *testing.T) {
	p := NewStubPaymentProvider("http://storefront.example")

	p.SetReverseOutcome(StubReverseRefuses)
	err := p.Reverse(context.Background(), "ctid-123")
	if err == nil {
		t.Fatal("reverse succeeded while driven to refuse")
	}
	if !errors.Is(err, ErrPaymentReverseRefused) {
		t.Fatalf("refusal = %v; a caller cannot tell it apart from silence, which is the whole distinction", err)
	}
	if PaymentReverseOutcomeUnknown(err) {
		t.Fatal("a driven refusal reads as an unknown outcome; the Reversal Request would stay open on an answer that was definite")
	}

	p.SetReverseOutcome(StubReverseOutcomeUnknown)
	err = p.Reverse(context.Background(), "ctid-123")
	if err == nil {
		t.Fatal("reverse succeeded while driven to an unknown outcome")
	}
	if !PaymentReverseOutcomeUnknown(err) {
		t.Fatalf("unknown outcome = %v reads as a definite refusal; the buyer would be told nothing happened to money that may have moved", err)
	}

	p.SetReverseOutcome(StubReverseSucceeds)
	if err := p.Reverse(context.Background(), "ctid-123"); err != nil {
		t.Fatalf("reverse: %v, want the stub back to agreeing", err)
	}
}

// TestPaymentReverseNotSupportedIsADefiniteRefusal: a provider with no reversal
// API did not reverse anything, and never could. That is the strongest definite
// answer the boundary has, so it must never be read as an unknown outcome — a
// Reversal Request against such a provider would otherwise stay open forever,
// re-asking a provider that has nothing to ask.
func TestPaymentReverseNotSupportedIsADefiniteRefusal(t *testing.T) {
	err := unreversibleProvider{}.Reverse(context.Background(), "ctid-123")
	if !errors.Is(err, ErrPaymentReverseRefused) {
		t.Fatalf("%v is not a definite refusal; nothing happened and nothing ever could", err)
	}
	if PaymentReverseOutcomeUnknown(err) {
		t.Fatal("an unsupported reversal reads as an unknown outcome; there is nothing to find out")
	}
	// And it keeps its own identity: SupportsReverse and the Undo offer are still
	// decided by this sentinel, not by the classification above.
	if !errors.Is(err, ErrPaymentReverseNotSupported) {
		t.Fatal("ErrPaymentReverseNotSupported no longer matches itself")
	}
}

// TestPaymentReversalSupports pins the one rule that decides both whether the
// Customer Area offers an Undo and whether the endpoint goes ahead.
func TestPaymentReversalSupports(t *testing.T) {
	t.Run("a free claim needs no provider at all", func(t *testing.T) {
		// Including on a deployment that never wired one in: the zero value must
		// still honour the sale nobody had to collect money for.
		if !(PaymentReversal{}).Supports(FreePaymentMethod) {
			t.Fatal("a free Online Sale is not reversible without a provider; there is nothing to ask anybody")
		}
		if (PaymentReversal{}).Supports(PayPhoneProviderName) {
			t.Fatal("a paid sale is reversible with no provider configured; there is nobody to reverse it")
		}
	})

	t.Run("the stub stands in for the launch provider", func(t *testing.T) {
		// An Online Sale the stub settles records Payment Method "payphone", never
		// "stub" (ADR 0012). Comparing names alone would decide a dev deployment's
		// every paid sale was collected by somebody else.
		reversal := NewPaymentReversal(NewStubPaymentProvider("http://storefront.example"))
		if !reversal.Supports(PayPhoneProviderName) {
			t.Fatal("a paid sale the stub settled is not reversible; the whole flow is then unreachable in development")
		}
		if reversal.Supports("some-other-provider") {
			t.Fatal("a sale settled by another provider is reversible; nobody here can reverse it")
		}
	})

	t.Run("the real provider answers for its own name", func(t *testing.T) {
		reversal := NewPaymentReversal(NewPayPhoneProvider("token", "store-1", "http://payphone.invalid", discardLogger()))
		if !reversal.Supports(PayPhoneProviderName) {
			t.Fatal("a PayPhone sale is not reversible while the PayPhone provider supports reversal")
		}
		if reversal.Supports("some-other-provider") {
			t.Fatal("a sale settled by another provider was handed to PayPhone, which never saw it")
		}
	})

	t.Run("a provider that cannot reverse is never offered", func(t *testing.T) {
		reversal := NewPaymentReversal(unreversibleProvider{})
		if reversal.Supports("unreversible") {
			t.Fatal("a provider that refuses Reverse is offered anyway; the button would fail when pressed")
		}
		if !reversal.Supports(FreePaymentMethod) {
			t.Fatal("a free claim was refused because some provider cannot reverse; no provider was involved in it")
		}
	})
}

// unreversibleProvider is the future provider the boundary keeps
// ErrPaymentReverseNotSupported for: one with no reversal API at all.
type unreversibleProvider struct{}

func (unreversibleProvider) Name() string { return "unreversible" }
func (unreversibleProvider) Initiate(context.Context, PaymentInitiateInput) (*PaymentInitiation, error) {
	return nil, errors.New("not used")
}
func (unreversibleProvider) Confirm(context.Context, PaymentConfirmInput) (*PaymentConfirmation, error) {
	return nil, errors.New("not used")
}
func (unreversibleProvider) Reverse(context.Context, string) error {
	return ErrPaymentReverseNotSupported
}
func (unreversibleProvider) SupportsReverse() bool { return false }
