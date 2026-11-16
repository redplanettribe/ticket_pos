package integration

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// Check status on an annulled factura is one ask of the SRI's
// ConsultaComprobante (#744, spec #742, ADR 0078), asserted at the HTTP seam
// against the fake SRI.
//
// An ask is up to three calls, stopping at the first real reading (ANULADO
// or AUTORIZADO); every call is a `status` row on the attempts ledger whose
// outcome is its reading, and a call with no reading - a refusal, a 302, an
// HTML body, a reset - never fails the press. The document itself is left
// exactly as it was: no verdict is reached yet, and autorización is never
// asked, since its stored authorization record may keep saying AUTORIZADO
// after an annulment.

// annulledStatusRow is one attempts row as the annulled check reads it.
type annulledStatusRow struct {
	ID        int64  `json:"id"`
	Operation string `json:"operation"`
	Outcome   string `json:"outcome"`
	Error     string `json:"error"`
	Messages  []struct {
		Identifier string `json:"identifier"`
		Message    string `json:"message"`
	} `json:"messages"`
	StartedAt  string `json:"started_at"`
	DurationMS *int64 `json:"duration_ms"`
}

type annulledCheckView struct {
	Status     string              `json:"status"`
	AnnulledBy *string             `json:"annulled_by"`
	AttemptID  *int64              `json:"attempt_id"`
	Attempts   []annulledStatusRow `json:"attempts"`
}

// annulledSaleInvoice parks a Sale Invoice pending at the SRI and has the
// operator mark it annulled, returning the operator session and its id.
func annulledSaleInvoice(t *testing.T) (operatorSessionID, invoiceID string) {
	t.Helper()
	env := setupTest(t)
	operatorSessionID, invoiceID = houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("setup: drain = %+v; want pending", result)
	}
	if annulled := annulOK(t, operatorSessionID, invoiceID); annulled.Status != "annulled" {
		t.Fatalf("setup: status = %s; want annulled", annulled.Status)
	}
	return operatorSessionID, invoiceID
}

// checkAnnulledOK presses Check status and decodes the answer.
func checkAnnulledOK(t *testing.T, sessionID, id string) annulledCheckView {
	t.Helper()
	resp, body := checkInvoice(t, sessionID, id)
	if resp.StatusCode != http.StatusOK || body.Error != nil {
		t.Fatalf("check an annulled factura: status=%d error=%+v; want 200 with the document", resp.StatusCode, body.Error)
	}
	var view annulledCheckView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	return view
}

// statusRows is the ledger's `status` rows, in order.
func statusRows(view annulledCheckView) []annulledStatusRow {
	var out []annulledStatusRow
	for _, a := range view.Attempts {
		if a.Operation == "status" {
			out = append(out, a)
		}
	}
	return out
}

func readingsOf(rows []annulledStatusRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Outcome)
	}
	return out
}

// TestCheckOnAnAnnulledFacturaAsksConsultaComprobante: the SRI's wrong
// negative, then its edge's redirect, then AUTORIZADO. The press answers 200
// with the document, still annulled, its three calls on the ledger in order
// with the refusal's message kept, and `attempt_id` naming the last.
// Nothing is sent and autorización is never asked.
func TestCheckOnAnAnnulledFacturaAsksConsultaComprobante(t *testing.T) {
	operatorSessionID, invoiceID := annulledSaleInvoice(t)
	before := getDrainedInvoice(t, operatorSessionID, invoiceID)
	queriesBefore, receivedBefore := sriStub.authorizationQueryCount(), sriStub.receptionCount()
	sriStub.setConsulta(consultaSequence(consultaRefused, consultaRedirected, consultaAuthorized))

	view := checkAnnulledOK(t, operatorSessionID, invoiceID)

	if view.Status != "annulled" || view.AnnulledBy == nil || *view.AnnulledBy != "operator@example.com" {
		t.Fatalf("after the check: status %s annulled_by %v; want the annulment untouched", view.Status, view.AnnulledBy)
	}
	rows := statusRows(view)
	if got := readingsOf(rows); !equalStrings(got, []string{"none", "none", "authorized"}) {
		t.Fatalf("status rows = %v; want none, none, authorized", got)
	}
	if len(rows[0].Messages) != 1 || rows[0].Messages[0].Identifier != "99" || rows[0].Error != "" {
		t.Fatalf("the refusal's row = %+v; want the SRI's 99 kept and no error", rows[0])
	}
	if rows[1].Error == "" {
		t.Fatalf("the redirect's row = %+v; want the error saying why there was no reading", rows[1])
	}
	for _, r := range rows {
		if r.StartedAt == "" || r.DurationMS == nil {
			t.Fatalf("row %+v; want its timing", r)
		}
	}
	if view.AttemptID == nil || *view.AttemptID != rows[2].ID {
		t.Fatalf("attempt_id = %v; want the ask's last call %d", view.AttemptID, rows[2].ID)
	}
	if calls := sriStub.consultaCalls(); len(calls) != 3 || calls[0] != before.EcuadorFull.AccessKey {
		t.Fatalf("consultas = %v; want three, about clave %s", calls, before.EcuadorFull.AccessKey)
	}
	if sriStub.authorizationQueryCount() != queriesBefore || sriStub.receptionCount() != receivedBefore {
		t.Fatal("an annulled factura's Check status asked autorización or sent something")
	}
	after := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if after.Status != "annulled" || after.NextAttemptAt != nil {
		t.Fatalf("after: status %s next %v; want still annulled with nothing due", after.Status, after.NextAttemptAt)
	}

	// Resend stays refused: only Check status changed.
	resp, body := resendInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "resend an annulled document", resp, body, http.StatusConflict, "INVOICE_ANNULLED")
}

// TestAnnulledCheckStopsAtTheFirstReading: ANULADO and AUTORIZADO each end
// the ask on the call that read them.
func TestAnnulledCheckStopsAtTheFirstReading(t *testing.T) {
	operatorSessionID, invoiceID := annulledSaleInvoice(t)

	sriStub.setConsulta(consultaSequence(consultaAnnulled, consultaAuthorized))
	view := checkAnnulledOK(t, operatorSessionID, invoiceID)
	if got := readingsOf(statusRows(view)); !equalStrings(got, []string{"annulled"}) {
		t.Fatalf("first ask = %v; want one call reading annulled", got)
	}

	sriStub.setConsulta(consultaSequence(consultaHTML, consultaAuthorized, consultaAnnulled))
	view = checkAnnulledOK(t, operatorSessionID, invoiceID)
	if got := readingsOf(statusRows(view)); !equalStrings(got, []string{"annulled", "none", "authorized"}) {
		t.Fatalf("both asks = %v; want the first ask's annulled, then none and authorized", got)
	}
	if n := len(sriStub.consultaCalls()); n != 3 {
		t.Fatalf("consultas = %d; want 1 + 2", n)
	}
}

// TestAnnulledCheckWithNoReadingIsAnAnswer: three calls with no reading - a
// redirect, an HTML page, a reset - are the most one ask makes, and the
// press still answers 200 with the document.
func TestAnnulledCheckWithNoReadingIsAnAnswer(t *testing.T) {
	operatorSessionID, invoiceID := annulledSaleInvoice(t)
	sriStub.setConsulta(consultaSequence(consultaRedirected, consultaHTML, consultaDropped, consultaAnnulled))

	view := checkAnnulledOK(t, operatorSessionID, invoiceID)
	rows := statusRows(view)
	if got := readingsOf(rows); !equalStrings(got, []string{"none", "none", "none"}) {
		t.Fatalf("status rows = %v; want three calls with no reading, and no fourth", got)
	}
	for _, r := range rows {
		if r.Error == "" {
			t.Fatalf("row %+v; want the error saying why there was no reading", r)
		}
	}
	if n := len(sriStub.consultaCalls()); n != 3 {
		t.Fatalf("consultas = %d; want at most 3 in one ask", n)
	}
	if view.Status != "annulled" {
		t.Fatalf("status = %s; want annulled", view.Status)
	}
}

// TestAnnulledCheckAsksTheFacturasEnvironment: a test-environment factura
// is asked of the adapter built for `test` (celcer), a production one of the
// one built for `production` (cel) - sri's TestQueryStatusAsksTheEnvironmentsHost
// pins which host each one is. Manual Tax Invoices, which Mark annulled takes
// from pending as it takes a Sale Invoice.
func TestAnnulledCheckAsksTheFacturasEnvironment(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, operatorSessionID)
	testDoc := issuePending(t, operatorSessionID)
	flip := validEcuadorIssuerBody()
	flip["environment"] = "production"
	putEcuadorIssuer(t, sriEnv, operatorSessionID, flip)
	prodDoc := issuePending(t, operatorSessionID)
	if prodDoc.Environment != "production" || testDoc.Environment != "test" {
		t.Fatalf("setup: environments %s / %s; want test / production", testDoc.Environment, prodDoc.Environment)
	}
	annulOK(t, operatorSessionID, testDoc.ID)
	annulOK(t, operatorSessionID, prodDoc.ID)

	var mu sync.Mutex
	var asked []invoicing.Environment
	sriApp.InvoicingService.WithTaxAuthority(func(e invoicing.Environment) invoicing.TaxAuthority {
		mu.Lock()
		asked = append(asked, e)
		mu.Unlock()
		return sri.NewAuthority(e, sri.WithBaseURL(sriStub.server.URL))
	})
	t.Cleanup(func() {
		sriApp.InvoicingService.WithTaxAuthority(sri.AuthorityFactory(sri.WithBaseURL(sriStub.server.URL)))
	})

	for _, c := range []struct {
		id   string
		want invoicing.Environment
	}{{testDoc.ID, invoicing.EnvironmentTest}, {prodDoc.ID, invoicing.EnvironmentProduction}} {
		mu.Lock()
		asked = nil
		mu.Unlock()
		view := checkAnnulledOK(t, operatorSessionID, c.id)
		if got := readingsOf(statusRows(view)); !equalStrings(got, []string{"authorized"}) {
			t.Fatalf("%s: status rows = %v; want one AUTORIZADO", c.want, got)
		}
		mu.Lock()
		got := append([]invoicing.Environment(nil), asked...)
		mu.Unlock()
		if len(got) != 1 || got[0] != c.want {
			t.Fatalf("adapters built = %v; want the factura's own %s", got, c.want)
		}
	}
}
