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
//
// Neither provider in this system returns it any more — PayPhone reverses
// through its own API (ADR 0018) and the stub always agrees — and it stays on
// the boundary for the next one, which may well not. Removing it would mean the
// interface had no way to say "cannot", and a provider without a reversal API
// would have to invent a failure instead of declaring an absence.
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
	// Reverse undoes a collected payment, identified by our client transaction
	// id. It must return nil only when the money is genuinely on its way back:
	// an ambiguous answer — an unrecognized body, a transport failure, a
	// timeout — is an error, because the caller reverses the Ticket Sale on nil
	// and a buyer must never be told their money was returned when it was not.
	//
	// It must not retry. Re-posting a reversal into silence risks returning the
	// money twice, the same reasoning ADR 0012 applies to Confirm.
	//
	// A provider that cannot reverse programmatically returns
	// ErrPaymentReverseNotSupported and says so through SupportsReverse below.
	Reverse(ctx context.Context, clientTransactionID string) error
	// SupportsReverse reports whether Reverse does anything but refuse.
	//
	// It exists because Customer-initiated Sale Reversal has to be OFFERED
	// before it is attempted (ADR 0018): the Customer Area shows an Undo action
	// only on a sale it can actually undo, and finding out by calling Reverse
	// and reading the error would mean attempting a refund to answer a question
	// about a button. The answer is the provider's own — the same knowledge that
	// makes Reverse work or refuse — so it is declared here beside it rather
	// than guessed from a provider name anywhere else in the system.
	//
	// A provider whose Reverse returns ErrPaymentReverseNotSupported MUST return
	// false here. Returning true and then refusing would put an Undo button in
	// front of a buyer that cannot work.
	SupportsReverse() bool
}

// FreePaymentMethod is the Payment Method recorded on an Online Sale that had
// nothing to collect: a cart of Free Ticket Types is settled by the platform
// itself, with no Payment Provider involved at all (ADR 0017).
//
// It sits beside the Payment Provider boundary and not inside it precisely
// because it names the absence of one. A provider's Name() can never be this
// value, so nothing here can collide with a real integration.
const FreePaymentMethod = "free"

// PaymentReversal answers one question, and it is the question the whole
// Customer-initiated Sale Reversal flow turns on: can THIS Ticket Sale's Payment
// actually be undone?
//
// The answer is a property of how the sale was settled, derived from the
// Payment Provider that settled it, and it is deliberately computed in one place
// for two very different callers. The Customer Area reads it to decide whether
// to report a sale `reversible`, and the reversal endpoint reads it to decide
// whether to go ahead. Two copies of this rule would eventually disagree, and
// the shape of that disagreement is an Undo button that fails when pressed.
//
// The zero value is usable and refuses everything a provider would have to
// handle, which is the safe default for a deployment that never wired one in.
type PaymentReversal struct {
	provider PaymentProvider
}

// NewPaymentReversal binds the rule to the Payment Provider this deployment
// actually sells through.
func NewPaymentReversal(provider PaymentProvider) PaymentReversal {
	return PaymentReversal{provider: provider}
}

// Supports reports whether a Payment settled under paymentMethod can be
// reversed.
//
// A free Online Sale is always reversible: there is no money to return and
// nobody to ask, so voiding the Ticket Sale is the entire reversal (ADR 0017).
// Every other Payment Method names a Payment Provider, and only the provider
// this deployment is wired to can be asked to reverse anything — a sale settled
// through some other provider (a historical one, or another deployment's) is
// refused rather than handed to a provider that never saw it.
func (r PaymentReversal) Supports(paymentMethod string) bool {
	if paymentMethod == FreePaymentMethod {
		return true
	}
	if r.provider == nil || !settledBy(r.provider, paymentMethod) {
		return false
	}
	return r.provider.SupportsReverse()
}

// paymentMethodStandIn is a Payment Provider that settles Online Sales under a
// Payment Method other than its own Name. Exactly one implementation exists and
// exactly one can: the development stub, which stands in for the launch provider
// so a dev deployment's Sales list reads the same as production's (ADR 0012).
//
// It is an optional interface rather than a field on PaymentProvider because a
// real provider never has an answer to give — its Name IS the Payment Method its
// sales record.
type paymentMethodStandIn interface {
	// StandsInFor is the Payment Method recorded on the Online Sales this
	// provider settles.
	StandsInFor() string
}

// settledBy reports whether paymentMethod names this provider — the question
// "did the provider we can actually ask collect this money?".
//
// Name equality answers it for a real provider. The stub needs the second arm:
// an Online Sale it settles records the launch provider's Payment Method, not
// "stub", so comparing names alone would decide that a dev deployment's every
// paid sale was collected by somebody else and quietly withhold the Undo the
// stub exists to make exercisable.
func settledBy(provider PaymentProvider, paymentMethod string) bool {
	if provider.Name() == paymentMethod {
		return true
	}
	standIn, ok := provider.(paymentMethodStandIn)
	return ok && standIn.StandsInFor() == paymentMethod
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

// Reverse succeeds, unconditionally. There is no external service to ask and no
// money to give back, so the stub's honest answer to "was the payment reversed?"
// is yes — the same answer its Confirm gives the interstitial's Approve.
//
// It succeeds rather than refusing because the alternative makes the whole
// Customer-initiated Sale Reversal flow unreachable without PayPhone
// credentials: nobody could press Undo on a paid sale locally, and the first
// place the feature would be exercised end to end is production.
func (p *StubPaymentProvider) Reverse(_ context.Context, _ string) error {
	return nil
}

// SupportsReverse is true, in step with Reverse above (see the interface). The
// pair is what makes a paid Online Sale offer its Undo in development.
func (p *StubPaymentProvider) SupportsReverse() bool { return true }

// StandsInFor is the Payment Method the Online Sales this stub settles actually
// record: the launch provider's, never "stub" (ADR 0012). Without it the
// reversal rule would compare "payphone" against "stub" and conclude that every
// dev sale was collected by a provider this deployment cannot ask.
func (p *StubPaymentProvider) StandsInFor() string { return PayPhoneProviderName }
