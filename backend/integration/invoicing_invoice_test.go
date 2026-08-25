package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// Issuing a Tax Invoice (#454): the platform's factura to the SRI, built and
// signed and carried to the fake SRI, through the HTTP seam the ticket names.
// A test never asserts the module's tables or its layering — only what the
// operator sees and what the SRI received.

const invoicesPath = "/api/v1/operator/invoicing/invoices"

// invoiceDetailView is one Tax Invoice as the API returns it.
type invoiceDetailView struct {
	ID          string `json:"id"`
	Country     string `json:"country"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Number      string `json:"number"`
	IssuedOn    string `json:"issued_on"`
	IssuedBy    string `json:"issued_by"`
	Recipient   struct {
		TaxIDType string `json:"tax_id_type"`
		TaxID     string `json:"tax_id"`
		LegalName string `json:"legal_name"`
		Address   string `json:"address"`
		Email     string `json:"email"`
	} `json:"recipient"`
	TotalCents int64 `json:"total_cents"`
	Issuer     struct {
		RUC         string `json:"ruc"`
		RazonSocial string `json:"razon_social"`
	} `json:"issuer"`
	Lines []struct {
		Position    int    `json:"position"`
		Description string `json:"description"`
		Quantity    string `json:"quantity"`
		IVARate     string `json:"iva_rate"`
		RatePercent int    `json:"rate_percent"`
		BaseCents   int64  `json:"base_cents"`
		IVACents    int64  `json:"iva_cents"`
	} `json:"lines"`
	AdditionalFields []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"additional_fields"`
	PaymentMethod string `json:"payment_method"`
	Totals        struct {
		SubtotalCents int64 `json:"subtotal_cents"`
		IVACents      int64 `json:"iva_cents"`
		TotalCents    int64 `json:"total_cents"`
		ByRate        []struct {
			IVARate     string `json:"iva_rate"`
			RatePercent int    `json:"rate_percent"`
			BaseCents   int64  `json:"base_cents"`
			IVACents    int64  `json:"iva_cents"`
		} `json:"by_rate"`
	} `json:"totals"`
	Messages []struct {
		Identifier     string `json:"identifier"`
		Message        string `json:"message"`
		AdditionalInfo string `json:"additional_info"`
		Type           string `json:"type"`
	} `json:"messages"`
	Ecuador struct {
		AccessKey           string  `json:"access_key"`
		Secuencial          int64   `json:"secuencial"`
		Ambiente            string  `json:"ambiente"`
		AuthorizationNumber *string `json:"authorization_number"`
		AuthorizationDate   *string `json:"authorization_date"`
	} `json:"ecuador"`
	Attempts []struct {
		Operation  string `json:"operation"`
		Outcome    string `json:"outcome"`
		DurationMS int64  `json:"duration_ms"`
		Messages   []struct {
			Identifier string `json:"identifier"`
		} `json:"messages"`
	} `json:"attempts"`
	HasAuthorizationXML bool `json:"has_authorization_xml"`
}

// issuerReady saves the Ecuador Issuer and uploads a matching certificate, so
// the platform can sign — everything an issue needs and nothing it must not.
// Done through the SRI app, which shares the shared app's database and key.
func issuerReady(t *testing.T, sessionID string) {
	t.Helper()
	putEcuadorIssuer(t, sriEnv, sessionID, validEcuadorIssuerBody())
	uploadCertificate(t, sriEnv, sessionID, rsaP12(t, "1790012345001", "s3cret"), "s3cret")
}

// validInvoiceBody is a one-line factura to a company Recipient.
func validInvoiceBody() map[string]any {
	return map[string]any{
		"recipient": map[string]any{
			"tax_id_type": "ruc",
			"tax_id":      "1790012345001",
			"legal_name":  "ORGANIZACION EJEMPLO S.A.",
			"address":     "Av. República del Salvador, Quito",
			"email":       "billing@example.com",
		},
		"lines": []map[string]any{
			{"description": "Platform Fee - July 2026", "quantity": "1", "unit_price_cents": 10000, "iva_rate": "15"},
		},
		"payment_method":    "20",
		"additional_fields": []map[string]any{{"name": "Periodo", "value": "2026-07"}},
	}
}

func issueInvoice(t *testing.T, sessionID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return sriEnv.post(t, invoicesPath, body, authHeader(sessionID))
}

func issueOK(t *testing.T, sessionID string, body map[string]any) invoiceDetailView {
	t.Helper()
	resp, env := issueInvoice(t, sessionID, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view invoiceDetailView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// TestIssueInvoiceAuthorizedInOneRequest: the usual case. The fake receives a
// signed factura that validates against the vendored XSD, carries the clave
// the detail shows, and passes the ticket-3 verifier; the operator is looking
// at an authorized invoice with its number and date.
func TestIssueInvoiceAuthorizedInOneRequest(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	view := issueOK(t, sessionID, validInvoiceBody())

	if view.Status != "authorized" {
		t.Fatalf("status=%q, want authorized (messages %+v)", view.Status, view.Messages)
	}
	if view.Number != "001-001-000000001" {
		t.Fatalf("number=%q, want 001-001-000000001", view.Number)
	}
	if view.Ecuador.Secuencial != 1 {
		t.Fatalf("secuencial=%d, want 1", view.Ecuador.Secuencial)
	}
	if view.Ecuador.AuthorizationNumber == nil || *view.Ecuador.AuthorizationNumber != view.Ecuador.AccessKey {
		t.Fatalf("authorization number=%v, want the clave %q", view.Ecuador.AuthorizationNumber, view.Ecuador.AccessKey)
	}
	if view.Ecuador.AuthorizationDate == nil {
		t.Fatal("authorized invoice has no authorization date")
	}
	if !view.HasAuthorizationXML {
		t.Fatal("authorized invoice kept no authorization XML")
	}
	if view.IssuedBy != "operator@example.com" {
		t.Fatalf("issued_by=%q", view.IssuedBy)
	}
	// Totals are the server's: 100.00 base, 15.00 IVA, 115.00 total.
	if view.Totals.SubtotalCents != 10000 || view.Totals.IVACents != 1500 || view.Totals.TotalCents != 11500 {
		t.Fatalf("totals = %+v", view.Totals)
	}

	// The document the fake received is the legal artifact: it carries the
	// detail's clave, verifies, and validates against the official XSD.
	got, ok := sriStub.lastReceived()
	if !ok {
		t.Fatal("the fake SRI received nothing")
	}
	if got.accessKey != view.Ecuador.AccessKey {
		t.Fatalf("received clave %q, detail shows %q", got.accessKey, view.Ecuador.AccessKey)
	}
	if err := sri.ValidateAccessKey(got.accessKey); err != nil {
		t.Fatalf("received clave is malformed: %v", err)
	}
	if _, err := sri.Verify(got.signedXML); err != nil {
		t.Fatalf("received document does not verify: %v", err)
	}
	validateReceivedAgainstXSD(t, got.signedXML)

	// One attempts row per SRI call, with a duration, and the authorized query
	// carried the SRI's warning 60 verbatim.
	if len(view.Attempts) < 2 {
		t.Fatalf("attempts = %d, want at least the submit and one query", len(view.Attempts))
	}
	if view.Attempts[0].Operation != "submit" || view.Attempts[0].Outcome != "received" {
		t.Fatalf("first attempt = %+v, want submit/received", view.Attempts[0])
	}
	last := view.Attempts[len(view.Attempts)-1]
	if last.Operation != "query" || last.Outcome != "authorized" {
		t.Fatalf("last attempt = %+v, want query/authorized", last)
	}
	foundWarning := false
	for _, m := range view.Messages {
		if m.Identifier == "60" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("authorized invoice did not keep the SRI's warning 60: %+v", view.Messages)
	}
}

// TestIssueInvoiceRejectedShowsMessages: a DEVUELTA answer becomes a rejected
// invoice whose messages are the SRI's, verbatim.
func TestIssueInvoiceRejectedShowsMessages(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
	})

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "rejected" {
		t.Fatalf("status=%q, want rejected", view.Status)
	}
	if len(view.Messages) != 1 || view.Messages[0].Identifier != "35" || view.Messages[0].AdditionalInfo != "El XML no cumple el esquema" {
		t.Fatalf("messages = %+v, want the SRI's 35 verbatim", view.Messages)
	}
	// The number is kept even when rejected (Ficha §5.10).
	if view.Number != "001-001-000000001" {
		t.Fatalf("rejected invoice number=%q", view.Number)
	}
}

// TestIssueInvoiceNotAuthorizedShowsMessages: NO AUTORIZADO becomes a
// not_authorized invoice carrying the SRI's messages.
func TestIssueInvoiceNotAuthorizedShowsMessages(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("39", "FIRMA INVALIDA", "Error en la firma", "ERROR"))
	})

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "not_authorized" {
		t.Fatalf("status=%q, want not_authorized", view.Status)
	}
	if len(view.Messages) != 1 || view.Messages[0].Identifier != "39" {
		t.Fatalf("messages = %+v, want the SRI's 39", view.Messages)
	}
	if view.HasAuthorizationXML {
		t.Fatal("a not_authorized invoice must hold no authorization XML")
	}
}

// TestIssueInvoicePendingOnInProcessing: the SRI keeps saying EN
// PROCESAMIENTO; the budget runs out and the invoice is pending with its
// number kept.
func TestIssueInvoicePendingOnInProcessing(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, inProcessingSOAP(accessKey)
	})

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "pending" {
		t.Fatalf("status=%q, want pending", view.Status)
	}
	if view.Number != "001-001-000000001" {
		t.Fatalf("pending invoice number=%q", view.Number)
	}
}

// TestIssueInvoicePendingOnTransportFailure: the fake hangs past the budget;
// the submit fails and the invoice is pending, its number kept, the failed
// call on the ledger.
func TestIssueInvoicePendingOnTransportFailure(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	sriStub.setSleep(testPollBudget * 3)

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "pending" {
		t.Fatalf("status=%q, want pending", view.Status)
	}
	if len(view.Attempts) == 0 || view.Attempts[0].Outcome != "error" {
		t.Fatalf("attempts = %+v, want a recorded error", view.Attempts)
	}
}

// TestIssueInvoicePendingOnHTTP500: a 500 from the SRI is a transport failure,
// so the invoice is pending.
func TestIssueInvoicePendingOnHTTP500(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusInternalServerError, ""
	})

	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Status != "pending" {
		t.Fatalf("status=%q, want pending", view.Status)
	}
}

// TestIssueInvoiceConcurrentGetsDistinctConsecutiveNumbers: two operators
// pressing Issue at the same moment get two distinct, consecutive secuenciales
// — the sequence starts at 1 and the row lock serialises them.
func TestIssueInvoiceConcurrentGetsDistinctConsecutiveNumbers(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	const n = 2
	var wg sync.WaitGroup
	numbers := make([]int64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			view := issueOK(t, sessionID, validInvoiceBody())
			numbers[i] = view.Ecuador.Secuencial
		}(i)
	}
	wg.Wait()

	if numbers[0] == numbers[1] {
		t.Fatalf("two concurrent issues got the same secuencial %d", numbers[0])
	}
	if (numbers[0] == 1 && numbers[1] == 2) || (numbers[0] == 2 && numbers[1] == 1) {
		return
	}
	t.Fatalf("secuenciales = %v, want {1, 2}", numbers)
}

// TestIssueInvoiceSequencePerEnvironment: flipping the Issuer's environment
// does not disturb the other environment's sequence — each starts at 1 and
// counts on independently.
func TestIssueInvoiceSequencePerEnvironment(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	testEnvView := issueOK(t, sessionID, validInvoiceBody())
	if testEnvView.Ecuador.Secuencial != 1 || testEnvView.Environment != "test" {
		t.Fatalf("first test issue = seq %d env %q", testEnvView.Ecuador.Secuencial, testEnvView.Environment)
	}

	flip := validEcuadorIssuerBody()
	flip["environment"] = "production"
	putEcuadorIssuer(t, sriEnv, sessionID, flip)
	prodView := issueOK(t, sessionID, validInvoiceBody())
	if prodView.Ecuador.Secuencial != 1 || prodView.Environment != "production" {
		t.Fatalf("first production issue = seq %d env %q, want 1/production", prodView.Ecuador.Secuencial, prodView.Environment)
	}

	putEcuadorIssuer(t, sriEnv, sessionID, validEcuadorIssuerBody())
	secondTest := issueOK(t, sessionID, validInvoiceBody())
	if secondTest.Ecuador.Secuencial != 2 || secondTest.Environment != "test" {
		t.Fatalf("second test issue = seq %d env %q, want 2/test", secondTest.Ecuador.Secuencial, secondTest.Environment)
	}
}

// TestIssueInvoiceSnapshotsIssuer: editing the Issuer after issuing changes
// nothing on the invoice.
func TestIssueInvoiceSnapshotsIssuer(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	view := issueOK(t, sessionID, validInvoiceBody())
	originalName := view.Issuer.RazonSocial

	edited := validEcuadorIssuerBody()
	edited["razon_social"] = "A Completely Different Name S.A."
	putEcuadorIssuer(t, sriEnv, sessionID, edited)

	reread := getInvoice(t, sessionID, view.ID)
	if reread.Issuer.RazonSocial != originalName {
		t.Fatalf("invoice issuer name changed to %q after the Issuer was edited", reread.Issuer.RazonSocial)
	}
}

func getInvoice(t *testing.T, sessionID, id string) invoiceDetailView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view invoiceDetailView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// validateReceivedAgainstXSD runs xmllint against the vendored official
// factura_V1.1.0.xsd, skipping when xmllint is absent (as the kit's own test
// does). The XSD admits the trailing ds:Signature, so the signed document
// validates as sent.
func validateReceivedAgainstXSD(t *testing.T, signed []byte) {
	t.Helper()
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Skip("xmllint not installed; XSD validation skipped")
	}
	path := filepath.Join(t.TempDir(), "received.xml")
	if err := os.WriteFile(path, signed, 0o644); err != nil {
		t.Fatal(err)
	}
	xsd := filepath.Join("..", "internal", "invoicing", "sri", "testdata", "factura_V1.1.0.xsd")
	cmd := exec.Command(xmllint, "--noout", "--nonet", "--schema", xsd, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("received document does not validate against factura_V1.1.0.xsd:\n%s", out)
	}
}
