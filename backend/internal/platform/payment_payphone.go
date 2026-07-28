package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PayPhone implementation of the Payment Provider boundary (ADR 0012): the
// redirect flow ("Botón de pago por redirección", docs.payphone.app/boton-de-pago).
// Initiate calls Prepare server-side and returns the hosted card-payment URL;
// Confirm calls V2/Confirm after the Customer returns and reports the verdict.
// Raw net/http, no vendor SDK — the whole provider is two authenticated JSON
// POSTs, mirroring the Resend sender.
//
// Two PayPhone facts shape the error handling here:
//
//   - PayPhone auto-reverses any charge not confirmed within 5 minutes of
//     payment, so Confirm is one prompt, bounded attempt — no retries that
//     could silently outlast the window. A Confirm that errors leaves the
//     Payment pending upstream, and the return page's own refresh is the retry.
//   - Only a definitive verdict may settle a Payment. statusCode 3 approves,
//     statusCode 2 declines; everything else — HTTP failure, malformed JSON, an
//     unknown status — is an ERROR, never a decline, because the charge might
//     have succeeded and marking it failed would strand a paid Customer.

// payPhonePreparePath and payPhoneConfirmPath are PayPhone's two button
// endpoints, relative to the base URL (PayPhoneBaseURL in production, the test
// override elsewhere).
const (
	payPhonePreparePath = "/api/button/Prepare"
	payPhoneConfirmPath = "/api/button/V2/Confirm"
)

// payPhoneStatus values in the V2/Confirm response. PayPhone documents exactly
// two settled verdicts; anything else is treated as unknown-outcome.
const (
	payPhoneStatusCanceled = 2
	payPhoneStatusApproved = 3
)

// payPhoneTimeout bounds each call to PayPhone. Prepare blocks a checkout
// request and Confirm blocks the Customer's return page, so neither may hang
// until the Cloud Run request timeout; 10s is generous for a single JSON POST
// while staying far inside the 5-minute confirm window.
//
// It was 15s until Prepare gained its one prefill-stripping retry (#104): the
// worst case is now two calls on a request that blocks the buyer's browser, and
// 10s keeps that worst case at 20s rather than 30s.
//
// One client serves both operations, so this tightened Confirm as well, which
// has no retry and so simply gained five seconds less headroom. That is the
// right side to err on: a Confirm that runs out of time leaves the Payment
// pending and the return page's own refresh retries it, well inside PayPhone's
// 5-minute window — whereas a Confirm still waiting is a paid Customer staring
// at a spinner.
const payPhoneTimeout = 10 * time.Second

// PayPhoneProvider collects money through the platform's single PayPhone
// merchant account (ADR 0012). Selected by server.NewApp when PAYPHONE_*
// credentials are configured; otherwise the stub serves.
type PayPhoneProvider struct {
	apiToken string
	storeID  string
	baseURL  string
	client   *http.Client
	logger   Logger
}

// NewPayPhoneProvider builds the provider. baseURL is normally empty, selecting
// PayPhone's production origin; configuration may override it outside
// production only (the integration suite's fake server — LoadConfig refuses the
// override in production, like the Google token endpoint).
func NewPayPhoneProvider(apiToken, storeID, baseURL string, logger Logger) *PayPhoneProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = PayPhoneBaseURL
	}
	return &PayPhoneProvider{
		apiToken: apiToken,
		storeID:  storeID,
		baseURL:  strings.TrimRight(baseURL, "/"),
		client:   &http.Client{Timeout: payPhoneTimeout},
		logger:   logger,
	}
}

// Name identifies the implementation; recorded on every Payment it handles.
func (p *PayPhoneProvider) Name() string { return "payphone" }

// payPhonePrepareRequest is the subset of Prepare's payload this system uses.
// PayPhone requires amount to equal the sum of its monetary components; with no
// tax breakdown in the catalog, the whole amount rides as amountWithoutTax and
// the tax/service/tip fields are omitted.
//
// Email, DocumentID and PhoneNumber are PayPhone's optional prefills, which its
// docs describe with "se solicitará si no se proporciona" — it will be asked for
// if not provided. They carry omitempty because absent and blank are different
// requests to PayPhone: an omitted key means "ask the buyer", an empty string is
// a value we asserted and may be refused. That distinction is also what lets the
// prefill-stripping retry below reproduce today's payload byte for byte.
//
// omitempty carries a second, harder duty on PhoneNumber (#106). The checkout
// phone field is optional and is never given a default, a placeholder or a
// filler value, because PayPhone's documentation prohibits "datos quemados o
// estáticos" — hardcoded or static cardholder data — on pain of transaction
// rejection and account blocking. A buyer who left the field blank must produce
// a Prepare with no phoneNumber key AT ALL, not an empty string, and their
// checkout must behave exactly as it did before this field existed.
type payPhonePrepareRequest struct {
	Amount              int    `json:"amount"`
	AmountWithoutTax    int    `json:"amountWithoutTax"`
	ClientTransactionID string `json:"clientTransactionId"`
	Currency            string `json:"currency"`
	StoreID             string `json:"storeId"`
	Reference           string `json:"reference"`
	ResponseURL         string `json:"responseUrl"`
	Email               string `json:"email,omitempty"`
	DocumentID          string `json:"documentId,omitempty"`
	PhoneNumber         string `json:"phoneNumber,omitempty"`
}

// stripPrefills returns the request as it would have been sent before the
// prefills existed — the payload the retry falls back to. Every prefill goes,
// the phone number included: the retry exists to reproduce a payload PayPhone
// has accepted for as long as this integration has run, and a field kept back
// "because it is probably fine" would defeat the whole point of the fallback.
func (r payPhonePrepareRequest) stripPrefills() payPhonePrepareRequest {
	r.Email = ""
	r.DocumentID = ""
	r.PhoneNumber = ""
	return r
}

// hasPrefills reports whether there is anything for the retry to strip. Without
// it a 4xx on a bare payload would be re-sent identically: a second charge
// request that can only be refused the same way.
func (r payPhonePrepareRequest) hasPrefills() bool {
	return r.Email != "" || r.DocumentID != "" || r.PhoneNumber != ""
}

// payPhoneDocumentID maps a Tax ID onto PayPhone's documentId prefill, and is
// the one place that mapping lives (ADR 0012: provider-specific knowledge stays
// inside the provider).
//
// A cédula and a RUC pass through; a passport is withheld entirely. documentId
// is a field built around Ecuadorian identifiers, while the Tax ID validator is
// deliberately permissive for passports — any 6-20 alphanumerics, any country's
// scheme (ADR 0016). Sending one risks a refused Prepare, and a refused Prepare
// is a failed checkout: strictly worse for that buyer than typing the number on
// PayPhone's form as they do today.
//
// The Tax ID Type itself is never sent under any mapping. The redirect
// integration ("Botón de pago por redirección") has no parameter for it; the
// field exists only in PayPhone's Cajita de Pagos widget, a different
// integration this system does not use.
func payPhoneDocumentID(taxID SaleTaxID) string {
	if !taxID.Set() {
		return ""
	}
	switch taxID.Type {
	case TaxIDTypeCedula, TaxIDTypeRUC:
		return strings.TrimSpace(taxID.Number)
	default:
		return ""
	}
}

// payPhonePrepareResponse carries PayPhone's two payment URLs and its id for
// the attempt. payWithCard is the hosted web form any guest can complete in a
// browser; payWithPayPhone is the app flow, which the redirect checkout does
// not use (one RedirectURL per attempt, and the card form asks nothing of the
// Customer beyond a card).
type payPhonePrepareResponse struct {
	PaymentID       payPhoneNumberOrString `json:"paymentId"`
	PayWithCard     string                 `json:"payWithCard"`
	PayWithPayPhone string                 `json:"payWithPayPhone"`
}

// Initiate calls Prepare and returns the hosted card-payment URL. Amounts are
// integer cents end to end — PayPhone's own unit. PayPhone charges USD only, so
// any other currency is refused here rather than mis-charged there.
//
// The prefills (#104) get one second chance and no more. A 4xx is PayPhone
// blaming the payload, and the only part of the payload that is new is what this
// system has never sent before, so the request is re-sent stripped back to
// exactly the payload that worked before the prefills existed: a systematic
// rejection then degrades to today's checkout rather than breaking it for every
// affected buyer.
//
// Nothing else is retried, deliberately. A 5xx or a timeout says nothing about
// our fields, and re-posting a charge request into that silence risks a double
// charge — a far worse outcome for the buyer than the failed checkout they get
// instead (parent spec #103, "Failure handling").
func (p *PayPhoneProvider) Initiate(ctx context.Context, in PaymentInitiateInput) (*PaymentInitiation, error) {
	if in.Currency != "USD" {
		return nil, fmt.Errorf("payphone: unsupported currency %q: PayPhone charges USD only", in.Currency)
	}

	req := payPhonePrepareRequest{
		Amount:              in.AmountCents,
		AmountWithoutTax:    in.AmountCents,
		ClientTransactionID: in.ClientTransactionID,
		Currency:            in.Currency,
		StoreID:             p.storeID,
		Reference:           in.Reference,
		ResponseURL:         in.ResponseURL,
		Email:               strings.TrimSpace(in.Customer.Email),
		DocumentID:          payPhoneDocumentID(in.Customer.TaxID),
		// Already canonical E.164 by the time it reaches here — the exact form
		// PayPhone's phoneNumber parameter documents, "Símbolo(+) + Código País +
		// número". Blank when the buyer gave none, which omitempty turns into an
		// absent key rather than a fabricated one (#106).
		PhoneNumber: strings.TrimSpace(in.Customer.Phone),
	}

	var out payPhonePrepareResponse
	err := p.post(ctx, payPhonePreparePath, req, &out)
	if status, rejected := payPhoneClientErrorStatus(err); err != nil && rejected && req.hasPrefills() {
		// Presence booleans, never values: which prefill was populated is the whole
		// question a human debugging a systematic rejection needs answered, and no
		// email address, phone number or identification number may reach a log line
		// to answer it. The fields are independent, so booleans answer it exactly.
		//
		// The status is named but the error is NOT logged here, deliberately: its
		// message carries a bounded slice of PayPhone's response body, and this is
		// the one request that certainly held all three values. A provider that
		// echoes the field it objected to would put the buyer's own data in the log
		// through the very line written to keep it out.
		//
		// had_phone_number is the one this log most exists for: whether PayPhone
		// accepts a NON-Ecuadorian dialling code could not be established from its
		// documentation (#103, "Further Notes"), and a run of rejections that all
		// carried a phone is what would show it.
		p.logger.Warn("payphone prepare rejected our request; retrying once without the prefills",
			"client_transaction_id", in.ClientTransactionID,
			"had_email", req.Email != "",
			"had_document_id", req.DocumentID != "",
			"had_phone_number", req.PhoneNumber != "",
			"status_code", status,
		)
		out = payPhonePrepareResponse{}
		err = p.post(ctx, payPhonePreparePath, req.stripPrefills(), &out)
	}
	if err != nil {
		p.logger.Error("payphone prepare failed", "client_transaction_id", in.ClientTransactionID, "error", err)
		return nil, fmt.Errorf("payphone prepare: %w", err)
	}
	if strings.TrimSpace(out.PayWithCard) == "" {
		p.logger.Error("payphone prepare returned no payment url", "client_transaction_id", in.ClientTransactionID)
		return nil, fmt.Errorf("payphone prepare: response carries no payWithCard url")
	}

	return &PaymentInitiation{
		RedirectURL:       out.PayWithCard,
		ProviderPaymentID: string(out.PaymentID),
	}, nil
}

// payPhoneConfirmRequest is V2/Confirm's payload: PayPhone's id for the
// transaction (the integer id query param it appended to the responseUrl) and
// our client transaction id, which it names clientTxId here.
type payPhoneConfirmRequest struct {
	ID         int64  `json:"id"`
	ClientTxID string `json:"clientTxId"`
}

// payPhoneConfirmResponse is the subset of V2/Confirm's response this system
// reads: the verdict, PayPhone's transaction id for support cross-referencing,
// and the instrument details worth showing a human.
type payPhoneConfirmResponse struct {
	StatusCode        int                    `json:"statusCode"`
	TransactionStatus string                 `json:"transactionStatus"`
	TransactionID     payPhoneNumberOrString `json:"transactionId"`
	CardBrand         string                 `json:"cardBrand"`
	LastDigits        payPhoneNumberOrString `json:"lastDigits"`
}

// Confirm calls V2/Confirm with the id PayPhone's return redirect carried and
// reports the verdict. Only statusCode 3 approves and only statusCode 2
// declines; a missing or malformed id, an HTTP failure, malformed JSON, or an
// unrecognized status is an error — the outcome is unknown, the Payment must
// stay pending upstream, and a page refresh retries the confirm well inside
// PayPhone's 5-minute window.
func (p *PayPhoneProvider) Confirm(ctx context.Context, in PaymentConfirmInput) (*PaymentConfirmation, error) {
	rawID := strings.TrimSpace(in.Params["id"])
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		p.logger.Error("payphone confirm: missing or malformed id param", "client_transaction_id", in.ClientTransactionID, "id", rawID)
		return nil, fmt.Errorf("payphone confirm: missing or malformed id param %q", rawID)
	}

	var out payPhoneConfirmResponse
	if err := p.post(ctx, payPhoneConfirmPath, payPhoneConfirmRequest{
		ID:         id,
		ClientTxID: in.ClientTransactionID,
	}, &out); err != nil {
		p.logger.Error("payphone confirm failed", "client_transaction_id", in.ClientTransactionID, "error", err)
		return nil, fmt.Errorf("payphone confirm: %w", err)
	}

	// PayPhone assigns the transaction id it appended to the redirect; the
	// response echoes it. Prefer the echo, fall back to the param — either way
	// the Payment records an id the operator can find on the PayPhone dashboard.
	providerTransactionID := string(out.TransactionID)
	if providerTransactionID == "" {
		providerTransactionID = rawID
	}

	switch out.StatusCode {
	case payPhoneStatusApproved:
		return &PaymentConfirmation{
			Approved:              true,
			ProviderTransactionID: providerTransactionID,
			Instrument:            payPhoneInstrument(out.CardBrand, string(out.LastDigits)),
		}, nil
	case payPhoneStatusCanceled:
		return &PaymentConfirmation{
			Approved:              false,
			ProviderTransactionID: providerTransactionID,
		}, nil
	default:
		// Not a verdict PayPhone documents. Declining here could strand a paid
		// Customer, so this is unknown-outcome: error, Payment stays pending.
		p.logger.Error("payphone confirm returned an unrecognized status",
			"client_transaction_id", in.ClientTransactionID,
			"status_code", out.StatusCode,
			"transaction_status", out.TransactionStatus,
		)
		return nil, fmt.Errorf("payphone confirm: unrecognized statusCode %d (transactionStatus %q)", out.StatusCode, out.TransactionStatus)
	}
}

// Reverse is reserved (ADR 0012): reversals are manual on the PayPhone
// dashboard until a reversal flow is built.
func (p *PayPhoneProvider) Reverse(_ context.Context, _ string) error {
	return ErrPaymentReverseNotSupported
}

// payPhoneStatusError is a non-2xx answer from PayPhone, carrying the status
// alongside the bounded slice of the body that goes into the log. It exists so
// callers can tell "PayPhone refused what we sent" (4xx) from "PayPhone is
// unwell" (5xx) — a distinction only Initiate's prefill fallback needs, and one
// a flat error string could only be recovered from by parsing it.
//
// Transport failures and timeouts are NOT this error: they never reached
// PayPhone at all, and Initiate treats them like a 5xx.
type payPhoneStatusError struct {
	statusCode int
	body       string
}

func (e *payPhoneStatusError) Error() string {
	return fmt.Sprintf("payphone returned %d: %s", e.statusCode, e.body)
}

// payPhoneClientErrorStatus reports whether an error is PayPhone rejecting the
// request itself — the only failure the prefills can be to blame for — and, when
// it is, the status it rejected with.
//
// The status comes back separately from the error so the retry can name it in a
// log line without logging the error itself: payPhoneStatusError's message
// carries a bounded slice of PayPhone's response body, and the request that
// provoked it is the one request that certainly held the buyer's email,
// identification number and phone. If PayPhone echoes the field it objected to,
// that body is PII, and the retry line is the last place it may appear (#104).
func payPhoneClientErrorStatus(err error) (int, bool) {
	var statusErr *payPhoneStatusError
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	if statusErr.statusCode < 400 || statusErr.statusCode >= 500 {
		return 0, false
	}
	return statusErr.statusCode, true
}

// post performs the one authenticated JSON POST both operations funnel through,
// decoding a 2xx response into out.
func (p *PayPhoneProvider) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// A bounded slice of an error body keeps PayPhone's diagnostics in the log
	// without risking an unbounded read on a misbehaving response.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &payPhoneStatusError{statusCode: resp.StatusCode, body: string(errBody)}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// payPhoneInstrument renders the card details as the boundary's human-readable
// instrument description (e.g. "visa ····1234"); empty when PayPhone sent none.
func payPhoneInstrument(cardBrand, lastDigits string) string {
	brand := strings.ToLower(strings.TrimSpace(cardBrand))
	digits := strings.TrimSpace(lastDigits)
	switch {
	case brand != "" && digits != "":
		return brand + " ····" + digits
	case brand != "":
		return brand
	case digits != "":
		return "····" + digits
	default:
		return ""
	}
}

// payPhoneNumberOrString reads a field PayPhone may send as a JSON number or a
// string — ids and card last-digits alike. Its docs show numbers, but the
// values are opaque here and treating a genuine string as a decode failure
// would fail a confirm whose outcome is otherwise perfectly known.
type payPhoneNumberOrString string

func (v *payPhoneNumberOrString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*v = ""
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*v = payPhoneNumberOrString(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*v = payPhoneNumberOrString(n.String())
	return nil
}
