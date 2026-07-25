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

// TestStubPaymentProviderReverseIsReserved pins that reversal is a documented
// no-op until a provider supports it (ADR 0012).
func TestStubPaymentProviderReverseIsReserved(t *testing.T) {
	p := NewStubPaymentProvider("http://storefront.example")
	if err := p.Reverse(context.Background(), "ctid-123"); !errors.Is(err, ErrPaymentReverseNotSupported) {
		t.Fatalf("reverse error = %v, want ErrPaymentReverseNotSupported", err)
	}
}
