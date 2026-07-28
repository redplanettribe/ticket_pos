package platform

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// The Payment Provider boundary (ADR 0012): an external service that collects
// money from a Customer on the platform's behalf during an Online Sale. The
// integration shape is a full-page redirect — the API initiates a payment
// server-side and hands the Storefront a hosted payment URL; the provider's
// return redirect lands on a Storefront route handler (nothing external may
// call the Go API, ADR 0008), which asks the API to confirm.
//
// Selection mirrors the transactional-email pattern (ADR 0009): the real
// provider is chosen by credential presence in server.NewApp; absent
// credentials select the stub below, so the full checkout works locally with
// zero setup.

// PaymentInitiateInput is what starting a payment attempt needs: how much, in
// what currency, under which of OUR ids, a human-readable reference for the
// provider's own records, and where the provider must send the Customer back.
type PaymentInitiateInput struct {
	AmountCents int
	Currency    string
	// ClientTransactionID is our id for the attempt; the provider echoes it on
	// the return redirect and Confirm is keyed on it.
	ClientTransactionID string
	// Reference is a short human-readable description (e.g. the Event name)
	// shown on the provider's payment page and dashboard.
	Reference string
	// ResponseURL is the Storefront route the provider redirects the Customer to
	// once the payment page is done with them, whatever the outcome.
	ResponseURL string
	// Customer is what the platform already knows about the buyer, offered to
	// the provider so its hosted payment page can arrive with those fields
	// filled in rather than asking a second time (#103).
	Customer PaymentCustomer
}

// PaymentCustomer is the buyer, as much of them as the platform holds when a
// payment attempt starts. Nothing here is collected for the provider's benefit:
// every value is already on hand at Initiate, and every one is optional to a
// provider that has no use for it.
//
// It is deliberately NOT SaleCustomer, though the checkout builds it from one.
// This is a prefill offered across a third-party boundary and holds only what a
// hosted payment page could ask the buyer for; the buyer's name and the
// provenance of their own assertions (SaleCustomer.SelfAsserted) are the
// platform's business and do not cross it (#111).
//
// The Tax ID travels as the PAIR — Type and Number — never as a pre-resolved
// document string. Which Tax ID Types a provider's identification field can
// accept is that provider's knowledge, and it lives in that provider's
// implementation (ADR 0012): PayPhone maps cédula and RUC onto its documentId
// and withholds a passport, and a second provider must be free to decide
// otherwise about exactly the same pair without this boundary moving.
type PaymentCustomer struct {
	// Email is the address the Ticket Sale will be confirmed to, as snapshotted
	// on the Payment.
	Email string
	// Phone is the buyer's phone number in canonical E.164 form
	// ("+593987654321"), or empty when they gave none (#106).
	//
	// Empty is the ordinary case and means exactly nothing was collected: the
	// checkout field is optional and its value is never invented. A provider
	// must therefore send nothing at all rather than a blank or a placeholder —
	// PayPhone's rules prohibit static or hardcoded cardholder data, and the
	// buyer who skipped the field is entitled to today's behaviour, where the
	// provider asks them on its own form.
	Phone string
	// TaxID is the Tax ID the Online Sale is to be declared under (ADR 0016).
	TaxID SaleTaxID
}

// PaymentInitiation is the provider's answer to Initiate: where to send the
// Customer, and — when the provider assigns one this early — its own id for the
// payment, worth recording for support cross-referencing.
type PaymentInitiation struct {
	RedirectURL       string
	ProviderPaymentID string
}

// PaymentConfirmInput identifies the attempt to settle: our client transaction
// id plus whatever params the provider's return redirect carried, relayed
// verbatim by the Storefront.
type PaymentConfirmInput struct {
	ClientTransactionID string
	Params              map[string]string
}

// PaymentConfirmation is the provider's verdict: approved or declined, the
// provider's transaction id, and optionally a human-readable instrument
// description (e.g. "visa ····1234"), kept on the Payment for support; it is
// part of the boundary so a future payment-detail surface needs no interface
// change.
type PaymentConfirmation struct {
	Approved              bool
	ProviderTransactionID string
	Instrument            string
}

// ErrPaymentReverseNotSupported is returned by providers that cannot reverse a
// payment programmatically; reversal is then a manual operation on the
// provider's dashboard (ADR 0012).
var ErrPaymentReverseNotSupported = errors.New("payment provider: reverse is not supported")

// PaymentProvider collects money for Online Sales behind a provider-agnostic
// seam. PayPhone is the launch provider; a second provider requires only a new
// implementation of this interface, never a domain change.
type PaymentProvider interface {
	// Name identifies the implementation (e.g. "payphone", "stub"); it is
	// recorded on every Payment the provider handles.
	Name() string
	// Initiate starts a payment attempt and returns the hosted payment URL to
	// redirect the Customer to.
	Initiate(ctx context.Context, in PaymentInitiateInput) (*PaymentInitiation, error)
	// Confirm settles a payment attempt after the Customer returns, reporting
	// approved or declined. It must be safe to call once per attempt; callers
	// guarantee idempotency across retries by consulting their own Payment
	// record first.
	Confirm(ctx context.Context, in PaymentConfirmInput) (*PaymentConfirmation, error)
	// Reverse undoes a collected payment. Reserved: no current implementation
	// supports it (refunds are manual via the provider dashboard, ADR 0012);
	// implementations without support return ErrPaymentReverseNotSupported.
	Reverse(ctx context.Context, clientTransactionID string) error
}

// Stub payment page contract, honoured by the Storefront's dev-only
// interstitial (built by ticket #84):
//
//	Initiate redirects the Customer to
//	    {STOREFRONT_BASE_URL}/checkout/stub
//	        ?client_transaction_id={our id}
//	        &amount_cents={integer amount}
//	        &currency={ISO code, e.g. USD}
//	        &response_url={URL-encoded ResponseURL}
//
//	The interstitial shows the amount with Approve and Decline actions. Either
//	action navigates to {response_url} with two query params appended:
//	    client_transaction_id={our id}
//	    outcome=approved | declined
//
//	The Storefront return handler then confirms with the API, relaying the
//	outcome param; Confirm approves exactly when Params["outcome"] == "approved"
//	and declines otherwise — a missing or unknown outcome is a decline, never an
//	approval.
const (
	StubPaymentPath            = "/checkout/stub"
	StubPaymentOutcomeParam    = "outcome"
	StubPaymentOutcomeApproved = "approved"
	StubPaymentOutcomeDeclined = "declined"
)

// StubPaymentProvider is the development Payment Provider: its "hosted payment
// page" is a Storefront interstitial with Approve and Decline actions, driving
// the same redirect legs the real provider does. Selected when no PayPhone
// credentials are configured. It never talks to any external service.
type StubPaymentProvider struct {
	storefrontBaseURL string
}

// NewStubPaymentProvider returns the stub provider, pointing its interstitial
// at the given Storefront origin.
func NewStubPaymentProvider(storefrontBaseURL string) *StubPaymentProvider {
	return &StubPaymentProvider{storefrontBaseURL: storefrontBaseURL}
}

// Name identifies the stub implementation.
func (p *StubPaymentProvider) Name() string { return "stub" }

// Initiate builds the interstitial URL per the stub contract above. It cannot
// fail: there is no external service to refuse.
//
// The Customer block is ignored in full: the interstitial has no card form and
// therefore nothing to prefill, and putting a Customer's email, phone number or
// Tax ID number in a query string the browser then displays would be a needless
// leak of the buyer's identity into their own URL bar and history.
func (p *StubPaymentProvider) Initiate(_ context.Context, in PaymentInitiateInput) (*PaymentInitiation, error) {
	q := url.Values{}
	q.Set("client_transaction_id", in.ClientTransactionID)
	q.Set("amount_cents", strconv.Itoa(in.AmountCents))
	q.Set("currency", in.Currency)
	q.Set("response_url", in.ResponseURL)
	return &PaymentInitiation{
		RedirectURL: p.storefrontBaseURL + StubPaymentPath + "?" + q.Encode(),
		// The stub assigns its provider id at initiation, exercising the same
		// "store the provider's id" path the real provider needs.
		ProviderPaymentID: "stub-" + in.ClientTransactionID,
	}, nil
}

// Confirm approves exactly when the relayed outcome param says "approved";
// anything else — declined, missing, or unrecognized — is a decline. Failing
// closed here mirrors a real provider, where only a positive confirmation is
// ever an approval.
func (p *StubPaymentProvider) Confirm(_ context.Context, in PaymentConfirmInput) (*PaymentConfirmation, error) {
	return &PaymentConfirmation{
		Approved:              in.Params[StubPaymentOutcomeParam] == StubPaymentOutcomeApproved,
		ProviderTransactionID: "stub-" + in.ClientTransactionID,
	}, nil
}

// Reverse is reserved (ADR 0012): the stub, like the launch PayPhone
// integration, has no programmatic reversal.
func (p *StubPaymentProvider) Reverse(_ context.Context, _ string) error {
	return ErrPaymentReverseNotSupported
}
