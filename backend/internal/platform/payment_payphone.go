package platform

import (
	"bytes"
	"context"
	"encoding/json"
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
// until the Cloud Run request timeout; 15s is generous for two JSON POSTs while
// staying far inside the 5-minute confirm window.
const payPhoneTimeout = 15 * time.Second

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
type payPhonePrepareRequest struct {
	Amount              int    `json:"amount"`
	AmountWithoutTax    int    `json:"amountWithoutTax"`
	ClientTransactionID string `json:"clientTransactionId"`
	Currency            string `json:"currency"`
	StoreID             string `json:"storeId"`
	Reference           string `json:"reference"`
	ResponseURL         string `json:"responseUrl"`
}

// payPhonePrepareResponse carries PayPhone's two payment URLs and its id for
// the attempt. payWithCard is the hosted web form any guest can complete in a
// browser; payWithPayPhone is the app flow, which the redirect checkout does
// not use (one RedirectURL per attempt, and the card form asks nothing of the
// Customer beyond a card).
type payPhonePrepareResponse struct {
	PaymentID       payPhoneID `json:"paymentId"`
	PayWithCard     string     `json:"payWithCard"`
	PayWithPayPhone string     `json:"payWithPayPhone"`
}

// Initiate calls Prepare and returns the hosted card-payment URL. Amounts are
// integer cents end to end — PayPhone's own unit. PayPhone charges USD only, so
// any other currency is refused here rather than mis-charged there.
func (p *PayPhoneProvider) Initiate(ctx context.Context, in PaymentInitiateInput) (*PaymentInitiation, error) {
	if in.Currency != "USD" {
		return nil, fmt.Errorf("payphone: unsupported currency %q: PayPhone charges USD only", in.Currency)
	}

	var out payPhonePrepareResponse
	err := p.post(ctx, payPhonePreparePath, payPhonePrepareRequest{
		Amount:              in.AmountCents,
		AmountWithoutTax:    in.AmountCents,
		ClientTransactionID: in.ClientTransactionID,
		Currency:            in.Currency,
		StoreID:             p.storeID,
		Reference:           in.Reference,
		ResponseURL:         in.ResponseURL,
	}, &out)
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
	StatusCode        int        `json:"statusCode"`
	TransactionStatus string     `json:"transactionStatus"`
	TransactionID     payPhoneID `json:"transactionId"`
	CardBrand         string     `json:"cardBrand"`
	LastDigits        payPhoneID `json:"lastDigits"`
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
		return fmt.Errorf("payphone returned %d: %s", resp.StatusCode, string(errBody))
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

// payPhoneID reads an identifier field whether PayPhone sends it as a JSON
// number or a string — its docs show numbers, but ids are opaque here and
// treating a genuine string id as a decode failure would fail a confirm whose
// outcome is otherwise perfectly known.
type payPhoneID string

func (v *payPhoneID) UnmarshalJSON(data []byte) error {
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
		*v = payPhoneID(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*v = payPhoneID(n.String())
	return nil
}
