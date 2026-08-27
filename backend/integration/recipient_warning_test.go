package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/peter/ticket_pos/backend/migrations"
)

// The Recipient Warning (#482, parent #478, ADR 0061): the SRI's word, on a
// Sale Invoice it nevertheless AUTHORIZED, that the Recipient's Tax ID does
// not exist (advertencia 59) or is incorrect (62). A stored fact beside the
// status, which stays `authorized`: the Drainer settled the document, it is
// delivered, it is never parked, and a reversal credits it as any other. Set
// wherever an authorization outcome is recorded — the Drainer, Check status,
// Resend — and never by advertencia 60 alone, which every test-environment
// authorization carries. Documents authorized before the fact existed are
// marked from their attempts ledger by the migration's backfill.
//
// Everything is asserted through the operator API — the detail, the list and
// its filter, the dashboard's count — the drain response and the fake SRI.
// The backfill test inserts the attempt row directly, because no API writes
// the ledger, and re-applies the migration statement against the shared
// database, since the harness applies migrations once at start.

const recipientWarningCountPath = "/api/v1/operator/invoicing/recipient-warnings/count"

// recipientWarningView is what #482 adds to the list row and the detail.
type recipientWarningView struct {
	drainedInvoiceView
	RecipientWarning bool `json:"recipient_warning"`
}

type recipientWarningListView struct {
	Data []struct {
		ID               string `json:"id"`
		Kind             string `json:"kind"`
		Status           string `json:"status"`
		RecipientWarning bool   `json:"recipient_warning"`
	} `json:"data"`
	Pagination struct {
		Total int `json:"total"`
	} `json:"pagination"`
}

func getRecipientWarningDetail(t *testing.T, env *testEnv, sessionID, id string) recipientWarningView {
	t.Helper()
	resp, body := env.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view recipientWarningView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// listInvoicesWith lists the invoices under the given query string.
func listInvoicesWith(t *testing.T, env *testEnv, sessionID, query string) recipientWarningListView {
	t.Helper()
	resp, body := env.get(t, invoicesPath+query, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list %q status=%d error=%+v", query, resp.StatusCode, body.Error)
	}
	var view recipientWarningListView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return view
}

func getRecipientWarningCount(t *testing.T, env *testEnv, sessionID string) int {
	t.Helper()
	resp, body := env.get(t, recipientWarningCountPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view struct {
		RecipientWarningCount int `json:"recipient_warning_count"`
	}
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	return view.RecipientWarningCount
}

// authorizeWithAdvertencia makes the fake SRI authorize every document with
// the given advertencia beside the test environment's 60.
func authorizeWithAdvertencia(identifier, message string) {
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, authorizedSOAPWithMessages(accessKey,
			sriMessage(identifier, message, "", "ADVERTENCIA")+testEnvironmentAdvertencia)
	})
}

// assertNotParked: the warning never parks a document — the queue does not
// list it and the count does not include it.
func assertNotParked(t *testing.T, operatorSessionID, invoiceID string) {
	t.Helper()
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("needs_attention count = %d; want 0 — a Recipient Warning is not needs_attention", n)
	}
	for _, row := range getNeedsAttentionQueue(t, operatorSessionID).Data {
		if row.ID == invoiceID {
			t.Fatalf("the warned document is in the needs_attention queue")
		}
	}
}

// TestRecipientWarningIsSetByTheDrainerOn59: the Drainer authorizes a Sale
// Invoice the SRI answered with advertencia 59. The document is authorized
// and delivered as usual — never parked — and carries the warning; the
// detail shows the SRI's own message; the list marks it, the filter finds
// it, and the dashboard's count says one.
func TestRecipientWarningIsSetByTheDrainerOn59(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 0 {
		t.Fatalf("count before anything is authorized = %d; want 0", n)
	}
	authorizeWithAdvertencia("59", "IDENTIFICACION NO EXISTE")

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Authorized != 1 || result.Delivered != 1 || result.NeedsAttention != 0 {
		t.Fatalf("drain = %+v; want the document authorized and delivered, not parked", result)
	}

	detail := getRecipientWarningDetail(t, sriEnv, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || !detail.RecipientWarning {
		t.Fatalf("detail = %s warning %v; want authorized with a Recipient Warning", detail.Status, detail.RecipientWarning)
	}
	if detail.DeliveredAt == nil || detail.NextAttemptAt != nil {
		t.Fatalf("delivered_at %v next_attempt_at %v; want delivered and nothing due — the warning changes nothing about the document", detail.DeliveredAt, detail.NextAttemptAt)
	}
	// The SRI's own words are on the detail, verbatim.
	var sawWarning bool
	for _, m := range detail.Messages {
		if m.Identifier == "59" && m.Message == "IDENTIFICACION NO EXISTE" && m.Type == "ADVERTENCIA" {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Fatalf("messages = %+v; want the SRI's 59 verbatim", detail.Messages)
	}
	assertNotParked(t, operatorSessionID, invoiceID)

	// The list marks it, and the filter narrows the list to it.
	list := listInvoicesWith(t, sriEnv, operatorSessionID, "")
	if len(list.Data) != 1 || list.Data[0].ID != invoiceID || !list.Data[0].RecipientWarning {
		t.Fatalf("list = %+v; want the one document marked", list.Data)
	}
	filtered := listInvoicesWith(t, sriEnv, operatorSessionID, "?recipient_warning=true")
	if filtered.Pagination.Total != 1 || len(filtered.Data) != 1 || filtered.Data[0].ID != invoiceID {
		t.Fatalf("filtered list = %+v total %d; want the warned document alone", filtered.Data, filtered.Pagination.Total)
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("count = %d; want 1", n)
	}

	// A second drain — nothing due — changes nothing: the warning is not
	// cleared by delivery or by time.
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("second drain = %+v; want nothing claimed", again)
	}
	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, invoiceID); !d.RecipientWarning {
		t.Fatal("the warning was cleared by a later drain")
	}
}

// TestRecipientWarningIsNotSetBy60Alone: the ordinary test-environment
// authorization — advertencia 60 and nothing else — raises no warning; the
// filter finds nothing and the count is zero. Nor does a manual Tax
// Invoice the SRI warns about: the warning is a Sale Invoice's fact, the
// one document whose Recipient the platform copied from a Sale and can
// correct by reissue.
func TestRecipientWarningIsNotSetBy60Alone(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)

	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want authorized", result)
	}
	detail := getRecipientWarningDetail(t, sriEnv, operatorSessionID, invoiceID)
	if detail.Status != "authorized" || detail.RecipientWarning {
		t.Fatalf("detail = %s warning %v; want authorized without a warning", detail.Status, detail.RecipientWarning)
	}
	if len(detail.Messages) != 1 || detail.Messages[0].Identifier != "60" {
		t.Fatalf("messages = %+v; want the 60 alone", detail.Messages)
	}
	list := listInvoicesWith(t, sriEnv, operatorSessionID, "")
	if len(list.Data) != 1 || list.Data[0].RecipientWarning {
		t.Fatalf("list = %+v; want the document unmarked", list.Data)
	}
	if filtered := listInvoicesWith(t, sriEnv, operatorSessionID, "?recipient_warning=true"); filtered.Pagination.Total != 0 || len(filtered.Data) != 0 {
		t.Fatalf("filtered list = %+v; want nothing", filtered.Data)
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 0 {
		t.Fatalf("count = %d; want 0", n)
	}

	// A manual Tax Invoice warned about with 62 is authorized and unmarked.
	authorizeWithAdvertencia("62", "IDENTIFICACION INCORRECTA")
	manual := issueOK(t, operatorSessionID, validInvoiceBody())
	if manual.Status != "authorized" {
		t.Fatalf("manual = %s; want authorized", manual.Status)
	}
	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, manual.ID); d.RecipientWarning {
		t.Fatal("a manual Tax Invoice carries a Recipient Warning")
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 0 {
		t.Fatalf("count after the manual document = %d; want 0", n)
	}
	if filtered := listInvoicesWith(t, sriEnv, operatorSessionID, "?recipient_warning=true"); filtered.Pagination.Total != 0 {
		t.Fatalf("filtered list total = %d; want 0", filtered.Pagination.Total)
	}
}

// TestRecipientWarningIsSetByCheckStatusOn62: a Sale Invoice the SRI held
// EN PROCESAMIENTO is pending; the operator's Check status finds it
// AUTORIZADO with advertencia 62, and the document is authorized with the
// warning.
func TestRecipientWarningIsSetByCheckStatusOn62(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("drain = %+v; want pending", result)
	}

	authorizeWithAdvertencia("62", "IDENTIFICACION INCORRECTA")
	resp, body := checkInvoice(t, operatorSessionID, invoiceID)
	view := actionOK(t, "check", resp, body)
	if view.Status != "authorized" {
		t.Fatalf("check = %s; want authorized", view.Status)
	}
	detail := getRecipientWarningDetail(t, sriEnv, operatorSessionID, invoiceID)
	if !detail.RecipientWarning {
		t.Fatal("Check status authorized the document with a 62 and set no Recipient Warning")
	}
	assertNotParked(t, operatorSessionID, invoiceID)
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("count = %d; want 1", n)
	}
}

// TestRecipientWarningIsSetByResendOn59: a Sale Invoice the SRI returned is
// parked; the operator's Resend gets it AUTORIZADO with advertencia 59, and
// the document leaves the queue authorized with the warning.
func TestRecipientWarningIsSetByResendOn59(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setReception(func(accessKey string) (int, string) {
		return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want parked", result)
	}

	sriStub.answerAsUsual()
	authorizeWithAdvertencia("59", "IDENTIFICACION NO EXISTE")
	resp, body := resendInvoice(t, operatorSessionID, invoiceID)
	view := actionOK(t, "resend", resp, body)
	if view.Status != "authorized" {
		t.Fatalf("resend = %s; want authorized", view.Status)
	}
	detail := getRecipientWarningDetail(t, sriEnv, operatorSessionID, invoiceID)
	if !detail.RecipientWarning {
		t.Fatal("Resend authorized the document with a 59 and set no Recipient Warning")
	}
	assertNotParked(t, operatorSessionID, invoiceID)
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("count = %d; want 1", n)
	}
}

// TestRecipientWarningNeverBlocksTheCreditNote: a warned factura is
// credited on reversal exactly as an unwarned one — the Credit Note is
// owed, drained and authorized — and the factura keeps its warning; the
// Credit Note, to the same wrong Recipient, gets none of its own.
func TestRecipientWarningNeverBlocksTheCreditNote(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	authorizeWithAdvertencia("59", "IDENTIFICACION NO EXISTE")
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want authorized", result)
	}
	ref := lastConfirmation(t, env).Reference

	buyer := buyerSession(t, env, "guest@example.com")
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, buyer, ""), ref)
	if result := reverseSaleOK(t, payphoneEnv, buyer, sale.ID); result.ConfirmationRef != ref {
		t.Fatalf("undo = %+v; want %s reversed", result, ref)
	}
	noteID := creditNoteOf(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Authorized != 1 {
		t.Fatalf("drain of the Credit Note = %+v; want authorized", result)
	}
	note := getRecipientWarningDetail(t, sriEnv, operatorSessionID, noteID)
	if note.Kind != "credit_note" || note.Status != "authorized" || note.RecipientWarning {
		t.Fatalf("credit note = %s %s warning %v; want an authorized Credit Note without a warning of its own", note.Kind, note.Status, note.RecipientWarning)
	}
	// Credited, the factura declares nothing any more and can never be
	// reissued: its warning is cleared and it leaves the operator's count.
	factura := getRecipientWarningDetail(t, sriEnv, operatorSessionID, invoiceID)
	if factura.CreditedByInvoiceID == nil || *factura.CreditedByInvoiceID != noteID || factura.RecipientWarning {
		t.Fatalf("factura credited_by %v warning %v; want credited by %s with the warning cleared", factura.CreditedByInvoiceID, factura.RecipientWarning, noteID)
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 0 {
		t.Fatalf("count = %d; want 0 — a credited factura needs no decision", n)
	}
}

// TestRecipientWarningListFilterIsValidated: the filter takes `true` and
// nothing else; a mistyped value is refused by name rather than read as
// "every document".
func TestRecipientWarningListFilterIsValidated(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	resp, body := sriEnv.get(t, invoicesPath+"?recipient_warning=yes", authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("recipient_warning=yes status=%d error=%+v; want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
	}
	// Combined with the kind filter, both narrow.
	if list := listInvoicesWith(t, sriEnv, operatorSessionID, "?kind=sale&recipient_warning=true"); list.Pagination.Total != 0 {
		t.Fatalf("empty database lists %d", list.Pagination.Total)
	}
}

// TestRecipientWarningIsBackfilledFromTheAttemptsLedger: a Sale Invoice
// authorized before the fact existed — its 59 in the attempts ledger and
// nothing on the row — is marked when the migration's backfill runs; one
// whose ledger holds 60 alone is not.
func TestRecipientWarningIsBackfilledFromTheAttemptsLedger(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceIDs := houseSalesOwed(t, env, 2)
	issuerReady(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 2 {
		t.Fatalf("drain = %+v; want both authorized", result)
	}
	warnedID, plainID := invoiceIDs[0], invoiceIDs[1]
	for _, id := range invoiceIDs {
		if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, id); d.RecipientWarning {
			t.Fatalf("%s is warned before the backfill", id)
		}
	}

	// Written directly: the ledger is append-only and no API writes it, and
	// this is the shape a document authorized before #482 left behind — an
	// authorized query whose messages carry the 59 nobody read.
	if _, err := env.db.Exec(`
		INSERT INTO invoicing_attempts (invoice_id, operation, outcome, messages, error, started_at, duration_ms)
		VALUES ($1, 'query', 'authorized',
		        '[{"identifier":"59","message":"IDENTIFICACION NO EXISTE","additional_info":"","type":"ADVERTENCIA"},
		          {"identifier":"60","message":"ESTE PROCESO FUE REALIZADO EN EL AMBIENTE DE PRUEBAS","additional_info":"","type":"ADVERTENCIA"}]'::jsonb,
		        '', NOW(), 12)
	`, warnedID); err != nil {
		t.Fatalf("seed attempt: %v", err)
	}

	// The harness applied the migrations once at start; the file is written
	// to be re-applied, so the backfill is run exactly as it ships.
	body, err := migrations.Files.ReadFile("101_recipient_warning.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := env.db.Exec(string(body)); err != nil {
		t.Fatalf("re-apply migration 101: %v", err)
	}

	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, warnedID); !d.RecipientWarning {
		t.Fatal("the document with a 59 in its ledger was not marked by the backfill")
	}
	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, plainID); d.RecipientWarning {
		t.Fatal("the document with a 60 alone in its ledger was marked by the backfill")
	}
	if n := getRecipientWarningCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("count after the backfill = %d; want 1", n)
	}
}

// TestRecipientWarningIsHiddenWhileSaleInvoicingIsClosed: with the flag
// closed the count answers 404 as the Drainer does, the filter is refused
// the same way, and a document marked while the flag was open reads
// unmarked on the list and the detail — no marker, no filter, no count.
func TestRecipientWarningIsHiddenWhileSaleInvoicingIsClosed(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	closed := startAppWithSaleInvoicingClosed(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	authorizeWithAdvertencia("59", "IDENTIFICACION NO EXISTE")
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want authorized", result)
	}
	if d := getRecipientWarningDetail(t, sriEnv, operatorSessionID, invoiceID); !d.RecipientWarning {
		t.Fatal("setup: the open app shows no warning")
	}

	resp, body := closed.get(t, recipientWarningCountPath, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("count while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	resp, body = closed.get(t, invoicesPath+"?recipient_warning=true", authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("filter while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	if list := listInvoicesWith(t, closed, operatorSessionID, ""); len(list.Data) != 1 || list.Data[0].RecipientWarning {
		t.Fatalf("closed list = %+v; want the document listed unmarked", list.Data)
	}
	if d := getRecipientWarningDetail(t, closed, operatorSessionID, invoiceID); d.RecipientWarning {
		t.Fatal("closed detail shows the warning")
	}
	// The count still says nothing while closed: the needs_attention count
	// answers as before, since it is not behind the flag.
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("needs_attention count = %d; want 0", n)
	}
}
