package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// Mark annulled admits an authorized factura (#745, spec #742, ADR 0078):
// one act, whether the operator annuls at the SRI portal after reading the
// page or records an annulment done earlier, with one set of gates and one
// confirm in every state a factura may be annulled from - the number typed
// in full, an optional note, and past the Annulment Window an
// acknowledgement that the portal took it. Asserted at the HTTP seam, as
// invoicing_abandon_test.go asserts the Abandon: the response and its
// refusal codes, and the detail's `annulment` and `annulability`.
//
// The invoicing clock stands at fixedClock, 2026-07-07 07:00 in Guayaquil,
// unless a test moves it: a factura issued then closes its window at the
// end of 7 August, and warns from 1 August.

type annulmentRecordView struct {
	FromStatus       string  `json:"from_status"`
	AnnulledBy       string  `json:"annulled_by"`
	AnnulledAt       string  `json:"annulled_at"`
	Note             *string `json:"note"`
	LateAcknowledged bool    `json:"late_acknowledged"`
	ReinstatedAt     *string `json:"reinstated_at"`
}

type annulmentDetailView struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Status      string  `json:"status"`
	Number      *string `json:"number"`
	Environment *string `json:"environment"`
	AnnulledBy  *string `json:"annulled_by"`
	AnnulledAt  *string `json:"annulled_at"`
	Recipient   struct {
		TaxID string `json:"tax_id"`
		Email string `json:"email"`
	} `json:"recipient"`
	Annulment *struct {
		Current *annulmentRecordView  `json:"current"`
		Past    []annulmentRecordView `json:"past"`
	} `json:"annulment"`
	Annulability *struct {
		Admitted bool `json:"admitted"`
		Refusal  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"refusal"`
		Window *struct {
			IssuedOn    string `json:"issued_on"`
			Deadline    string `json:"deadline"`
			WarningFrom string `json:"warning_from"`
			Today       string `json:"today"`
			ClosesAt    string `json:"closes_at"`
			Open        bool   `json:"open"`
			Warning     bool   `json:"warning"`
			DaysLeft    int    `json:"days_left"`
			DaysTotal   int    `json:"days_total"`
		} `json:"window"`
		Portal *struct {
			AccessKey         string  `json:"access_key"`
			AuthorizationDate *string `json:"authorization_date"`
			ReceptorID        string  `json:"receptor_id"`
			ReceptorEmail     string  `json:"receptor_email"`
			Environment       string  `json:"environment"`
		} `json:"portal"`
	} `json:"annulability"`
}

func getAnnulmentDetail(t *testing.T, sessionID, id string) annulmentDetailView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view annulmentDetailView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// pressAnnul presses Mark annulled with exactly the body given.
func pressAnnul(t *testing.T, sessionID, id string, body any) (*http.Response, envelope) {
	t.Helper()
	return sriEnv.post(t, invoicesPath+"/"+id+"/annul", body, authHeader(sessionID))
}

// annulPressOK presses Mark annulled with the body given and decodes the
// document it answers with.
func annulPressOK(t *testing.T, sessionID, id string, body any) annulmentDetailView {
	t.Helper()
	resp, env := pressAnnul(t, sessionID, id, body)
	return annulledDetail(t, resp, env)
}

func annulledDetail(t *testing.T, resp *http.Response, env envelope) annulmentDetailView {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("annul status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view annulmentDetailView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// authorizedManualInvoice issues a manual Tax Invoice the fake SRI
// authorizes at once.
func authorizedManualInvoice(t *testing.T, env *testEnv) (operatorSessionID, invoiceID string) {
	t.Helper()
	operatorSessionID = operatorSession(t, env, "operator@example.com")
	issuerReady(t, operatorSessionID)
	issued := issueOK(t, operatorSessionID, validInvoiceBody())
	if issued.Status != "authorized" {
		t.Fatalf("setup: manual invoice %s; want authorized", issued.Status)
	}
	return operatorSessionID, issued.ID
}

func numberOf(t *testing.T, view annulmentDetailView) string {
	t.Helper()
	if view.Number == nil {
		t.Fatalf("document %s has no number", view.ID)
	}
	return *view.Number
}

// TestMarkAnnulledOnAnAuthorizedSaleInvoice: the detail offers the act with
// the window and the portal's fields, the typed number records it with the
// note, and the response and the detail both show who, when, the note and
// that no late acknowledgement was given. Afterwards the act is refused as
// on any annulled document.
func TestMarkAnnulledOnAnAuthorizedSaleInvoice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID, _ := houseSaleAuthorized(t, env)

	before := getAnnulmentDetail(t, operatorSessionID, invoiceID)
	a := before.Annulability
	if a == nil || !a.Admitted || a.Refusal != nil {
		t.Fatalf("annulability = %+v; want admitted on an authorized Sale Invoice", a)
	}
	w := a.Window
	if w == nil || w.IssuedOn != "2026-07-07" || w.Deadline != "2026-08-07" || w.WarningFrom != "2026-08-01" ||
		w.Today != "2026-07-07" || !w.Open || w.Warning || w.DaysLeft != 31 || w.DaysTotal != 31 {
		t.Fatalf("window = %+v; want issued 2026-07-07, deadline 2026-08-07, warning from 2026-08-01, open with 31 days left", w)
	}
	if closes, err := time.Parse(time.RFC3339, w.ClosesAt); err != nil || !closes.Equal(time.Date(2026, 8, 8, 0, 0, 0, 0, sri.Guayaquil)) {
		t.Fatalf("closes_at = %s; want 2026-08-08 00:00 in Guayaquil", w.ClosesAt)
	}
	p := a.Portal
	if p == nil || p.AccessKey == "" || p.AuthorizationDate == nil || p.ReceptorID != before.Recipient.TaxID ||
		p.ReceptorEmail != before.Recipient.Email || p.Environment != "test" {
		t.Fatalf("portal = %+v; want the clave, the authorization date, the receptor as signed (%s, %s) and the test environment", p, before.Recipient.TaxID, before.Recipient.Email)
	}
	if before.Annulment == nil || before.Annulment.Current != nil || len(before.Annulment.Past) != 0 {
		t.Fatalf("annulment = %+v; want an empty record before the act", before.Annulment)
	}

	resp, body := pressAnnul(t, operatorSessionID, invoiceID, map[string]any{
		"number": numberOf(t, before), "note": "  wrong Recipient, the buyer asked by phone  ",
	})
	annulled := annulledDetail(t, resp, body)
	for _, view := range []annulmentDetailView{annulled, getAnnulmentDetail(t, operatorSessionID, invoiceID)} {
		if view.Status != "annulled" || view.AnnulledBy == nil || *view.AnnulledBy != "operator@example.com" || view.AnnulledAt == nil {
			t.Fatalf("detail = %s by %v at %v; want annulled by the operator", view.Status, view.AnnulledBy, view.AnnulledAt)
		}
		c := view.Annulment.Current
		if c == nil || c.FromStatus != "authorized" || c.AnnulledBy != "operator@example.com" || c.AnnulledAt == "" ||
			c.Note == nil || *c.Note != "wrong Recipient, the buyer asked by phone" || c.LateAcknowledged || c.ReinstatedAt != nil {
			t.Fatalf("annulment = %+v; want from authorized, by the operator, with the trimmed note and no late acknowledgement", c)
		}
		if view.Annulability.Admitted || view.Annulability.Refusal == nil || view.Annulability.Refusal.Code != "INVOICE_NOT_ANNULLABLE" {
			t.Fatalf("annulability after the act = %+v; want refused INVOICE_NOT_ANNULLABLE", view.Annulability)
		}
	}

	resp, body = pressAnnul(t, operatorSessionID, invoiceID, map[string]any{"number": numberOf(t, before)})
	expectRefusal(t, "annul twice", resp, body, http.StatusConflict, "INVOICE_NOT_ANNULLABLE")
}

// TestMarkAnnulledOnAnAuthorizedManualTaxInvoiceThroughItsWindow: the same
// act on a manual Tax Invoice, walked through its window: open and quiet in
// July, warning from 1 August Guayaquil time, still open on the last minute
// of 7 August, closed at 00:00 on 8 August. Past the deadline the press
// needs the acknowledgement and records it; before it, the acknowledgement
// is refused. With no note the record carries none.
func TestMarkAnnulledOnAnAuthorizedManualTaxInvoiceThroughItsWindow(t *testing.T) {
	operatorSessionID, invoiceID := authorizedManualInvoice(t, setupTest(t))
	number := numberOf(t, getAnnulmentDetail(t, operatorSessionID, invoiceID))

	// 04:00 UTC on 1 August is still 31 July in Guayaquil.
	atInvoicingClock(t, time.Date(2026, 8, 1, 4, 0, 0, 0, time.UTC))
	if w := getAnnulmentDetail(t, operatorSessionID, invoiceID).Annulability.Window; !w.Open || w.Warning || w.DaysLeft != 7 || w.Today != "2026-07-31" {
		t.Fatalf("window on 31 July = %+v; want open, no warning, 7 days left", w)
	}
	atInvoicingClock(t, time.Date(2026, 8, 1, 0, 0, 0, 0, sri.Guayaquil))
	if w := getAnnulmentDetail(t, operatorSessionID, invoiceID).Annulability.Window; !w.Open || !w.Warning || w.DaysLeft != 6 {
		t.Fatalf("window on 1 August = %+v; want open in its warning stretch, 6 days left", w)
	}
	atInvoicingClock(t, time.Date(2026, 8, 7, 23, 59, 0, 0, sri.Guayaquil))
	if w := getAnnulmentDetail(t, operatorSessionID, invoiceID).Annulability.Window; !w.Open || !w.Warning || w.DaysLeft != 0 {
		t.Fatalf("window on the last minute = %+v; want open, warning, the last day", w)
	}
	resp, body := pressAnnul(t, operatorSessionID, invoiceID, map[string]any{"number": number, "late_acknowledged": true})
	expectRefusal(t, "acknowledge a late annulment inside the window", resp, body, http.StatusConflict, "INVOICE_ANNULMENT_NOT_LATE")

	atInvoicingClock(t, time.Date(2026, 8, 8, 0, 0, 0, 0, sri.Guayaquil))
	view := getAnnulmentDetail(t, operatorSessionID, invoiceID)
	if w := view.Annulability.Window; w.Open || w.Warning || w.DaysLeft != 0 {
		t.Fatalf("window on 8 August = %+v; want closed", w)
	}
	if !view.Annulability.Admitted {
		t.Fatalf("annulability past the deadline = %+v; want still admitted, with the acknowledgement", view.Annulability)
	}
	resp, body = pressAnnul(t, operatorSessionID, invoiceID, map[string]any{"number": number})
	expectRefusal(t, "annul past the deadline without acknowledging", resp, body, http.StatusConflict, "INVOICE_ANNULMENT_LATE")
	if body.Error != nil {
		if details, ok := body.Error.Details.(map[string]any); !ok || details["deadline"] != "2026-08-07" {
			t.Fatalf("details = %+v; want the deadline named", body.Error.Details)
		}
	}
	if got := getAnnulmentDetail(t, operatorSessionID, invoiceID).Status; got != "authorized" {
		t.Fatalf("status after the refusal = %s; want authorized", got)
	}

	annulled := annulPressOK(t, operatorSessionID, invoiceID, map[string]any{"number": number, "note": "   ", "late_acknowledged": true})
	c := annulled.Annulment.Current
	if annulled.Kind != "manual" || annulled.Status != "annulled" || c == nil || c.FromStatus != "authorized" || !c.LateAcknowledged || c.Note != nil {
		t.Fatalf("annulled manual = %s %s %+v; want annulled from authorized, late acknowledged, no note", annulled.Kind, annulled.Status, c)
	}
}

// TestMarkAnnulledOnAPendingAndAParkedFacturaTakesTheSameConfirm: a
// pending factura and a parked one take the typed number too: none, or a
// wrong one, is INVOICE_NUMBER_MISMATCH and changes nothing; the right one
// records the state it was annulled from.
func TestMarkAnnulledOnAPendingAndAParkedFacturaTakesTheSameConfirm(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		issuerReady(t, operatorSessionID)
		sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
		if result := drainSaleInvoices(t); result.Pending != 1 {
			t.Fatalf("setup: drain = %+v; want pending", result)
		}
		assertTypedNumberGatesThenAnnuls(t, operatorSessionID, invoiceID, "pending")
	})
	t.Run("needs_attention", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		issuerReady(t, operatorSessionID)
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
		})
		if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
			t.Fatalf("setup: drain = %+v; want parked", result)
		}
		assertTypedNumberGatesThenAnnuls(t, operatorSessionID, invoiceID, "needs_attention")
	})
}

func assertTypedNumberGatesThenAnnuls(t *testing.T, operatorSessionID, invoiceID, fromStatus string) {
	t.Helper()
	view := getAnnulmentDetail(t, operatorSessionID, invoiceID)
	if !view.Annulability.Admitted || view.Annulability.Portal == nil || view.Annulability.Portal.AuthorizationDate != nil {
		t.Fatalf("annulability on %s = %+v; want admitted, with no authorization date to copy", fromStatus, view.Annulability)
	}
	for name, body := range map[string]any{
		"no body":       nil,
		"no number":     map[string]any{"note": "x"},
		"a wrong one":   map[string]any{"number": "001-001-999999999"},
		"a partial one": map[string]any{"number": "000000001"},
	} {
		resp, env := pressAnnul(t, operatorSessionID, invoiceID, body)
		expectRefusal(t, "annul with "+name, resp, env, http.StatusConflict, "INVOICE_NUMBER_MISMATCH")
	}
	if got := getAnnulmentDetail(t, operatorSessionID, invoiceID).Status; got != fromStatus {
		t.Fatalf("status after the refusals = %s; want %s untouched", got, fromStatus)
	}
	annulled := annulPressOK(t, operatorSessionID, invoiceID, map[string]any{"number": " " + numberOf(t, view) + " "})
	if c := annulled.Annulment.Current; annulled.Status != "annulled" || c == nil || c.FromStatus != fromStatus || c.LateAcknowledged {
		t.Fatalf("annulled = %s %+v; want annulled from %s", annulled.Status, c, fromStatus)
	}
}

// TestMarkAnnulledIsRefusedWhileALiveCreditNoteStands: a reversal of an
// authorized Sale Invoice owes a Credit Note, which refuses the annulment
// whether owed or parked by the SRI - the factura is being undone another
// way, and the facts are judged before the typed number. Once the operator
// marks that Credit Note annulled (its own Mark annulled, unchanged), it is
// dead and the factura can be annulled: the cure for a reversed Sale whose
// Credit Note was refused (flows doc U6).
func TestMarkAnnulledIsRefusedWhileALiveCreditNoteStands(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	number := numberOf(t, getAnnulmentDetail(t, operatorSessionID, facturaID))

	reversalRoutes[1].reverse(t, env, operatorSessionID, lastConfirmation(t, env).Reference)
	noteID := creditNoteOf(t, operatorSessionID)

	view := getAnnulmentDetail(t, operatorSessionID, facturaID)
	if view.Annulability.Admitted || view.Annulability.Refusal == nil || view.Annulability.Refusal.Code != "INVOICE_CREDIT_NOTE_STANDS" {
		t.Fatalf("annulability with an owed Credit Note = %+v; want INVOICE_CREDIT_NOTE_STANDS", view.Annulability)
	}
	resp, body := pressAnnul(t, operatorSessionID, facturaID, map[string]any{"number": "wrong"})
	expectRefusal(t, "annul under an owed Credit Note", resp, body, http.StatusConflict, "INVOICE_CREDIT_NOTE_STANDS")

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	atInvoicingClock(t, fixedClock.Add(time.Hour))
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the Credit Note parked", result)
	}
	resp, body = pressAnnul(t, operatorSessionID, facturaID, map[string]any{"number": number})
	expectRefusal(t, "annul under a parked Credit Note", resp, body, http.StatusConflict, "INVOICE_CREDIT_NOTE_STANDS")

	// The Credit Note's own Mark annulled takes no body and keeps no record.
	if note := annulOK(t, operatorSessionID, noteID); note.Status != "annulled" {
		t.Fatalf("credit note = %s; want annulled", note.Status)
	}
	if note := getAnnulmentDetail(t, operatorSessionID, noteID); note.Annulment != nil || note.Annulability != nil {
		t.Fatalf("credit note annulment = %+v / %+v; want none on a Credit Note", note.Annulment, note.Annulability)
	}

	if a := getAnnulmentDetail(t, operatorSessionID, facturaID).Annulability; !a.Admitted {
		t.Fatalf("annulability with a dead Credit Note = %+v; want admitted", a)
	}
	annulled := annulPressOK(t, operatorSessionID, facturaID, map[string]any{"number": number})
	if annulled.Status != "annulled" || annulled.Annulment.Current == nil || annulled.Annulment.Current.FromStatus != "authorized" {
		t.Fatalf("factura = %s %+v; want annulled from authorized", annulled.Status, annulled.Annulment.Current)
	}
}

// TestMarkAnnulledIsRefusedWhileALiveReissueSuccessorStands: a reissued
// factura has a corrected factura standing in its place (and the reissue's
// Credit Note); the reissue is the reason named.
func TestMarkAnnulledIsRefusedWhileALiveReissueSuccessorStands(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	number := numberOf(t, getAnnulmentDetail(t, operatorSessionID, facturaID))
	reissueOK(t, operatorSessionID, facturaID, companyRecipient())

	if a := getAnnulmentDetail(t, operatorSessionID, facturaID).Annulability; a.Admitted || a.Refusal == nil || a.Refusal.Code != "INVOICE_REISSUE_STANDS" {
		t.Fatalf("annulability of a reissued factura = %+v; want INVOICE_REISSUE_STANDS", a)
	}
	resp, body := pressAnnul(t, operatorSessionID, facturaID, map[string]any{"number": number})
	expectRefusal(t, "annul a reissued factura", resp, body, http.StatusConflict, "INVOICE_REISSUE_STANDS")
}

// TestMarkAnnulledStillHandsANumberTheSRIRefusesToAbandon: a factura the
// SRI refuses for its number answers INVOICE_ABANDON_INSTEAD whatever is
// typed, its panel shrinks to that sentence, and nothing changes.
func TestMarkAnnulledStillHandsANumberTheSRIRefusesToAbandon(t *testing.T) {
	operatorSessionID, invoiceID := refusedByNumberSaleInvoice(t)
	view := getAnnulmentDetail(t, operatorSessionID, invoiceID)
	if a := view.Annulability; a.Admitted || a.Refusal == nil || a.Refusal.Code != "INVOICE_ABANDON_INSTEAD" {
		t.Fatalf("annulability = %+v; want INVOICE_ABANDON_INSTEAD", a)
	}
	for _, typed := range []string{numberOf(t, view), "wrong"} {
		resp, body := pressAnnul(t, operatorSessionID, invoiceID, map[string]any{"number": typed})
		expectRefusal(t, "annul a number the SRI refuses", resp, body, http.StatusConflict, "INVOICE_ABANDON_INSTEAD")
	}
	if got := getAnnulmentDetail(t, operatorSessionID, invoiceID).Status; got != "needs_attention" {
		t.Fatalf("status = %s; want needs_attention untouched", got)
	}
}

// TestMarkAnnulledRefusesALongNote: the note is bounded at 500 characters
// as the Abandon's is.
func TestMarkAnnulledRefusesALongNote(t *testing.T) {
	operatorSessionID, invoiceID := authorizedManualInvoice(t, setupTest(t))
	number := numberOf(t, getAnnulmentDetail(t, operatorSessionID, invoiceID))
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	resp, body := pressAnnul(t, operatorSessionID, invoiceID, map[string]any{"number": number, "note": string(long)})
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" || !fieldNamed(body.Error.Details, "note") {
		t.Fatalf("long note: status=%d error=%+v; want 400 VALIDATION_FAILED on note", resp.StatusCode, body.Error)
	}
}

// TestASecondOpenAnnulmentIsImpossibleAtTheDatabase: the annulments ledger
// holds at most one annulment without a reinstatement per factura, whatever
// writes to it; a reinstated one leaves room for the next.
func TestASecondOpenAnnulmentIsImpossibleAtTheDatabase(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := authorizedManualInvoice(t, env)
	number := numberOf(t, getAnnulmentDetail(t, operatorSessionID, invoiceID))
	annulPressOK(t, operatorSessionID, invoiceID, map[string]any{"number": number})

	insert := `INSERT INTO invoicing_annulments (invoice_id, from_status, annulled_by, annulled_at, reinstated_at)
		VALUES ($1, 'authorized', 'someone@example.com', $2, $3)`
	if _, err := env.db.Exec(insert, invoiceID, fixedClock, nil); err == nil {
		t.Fatal("a second open annulment was stored; want the partial unique index to refuse it")
	}
	if _, err := env.db.Exec(insert, invoiceID, fixedClock, fixedClock.Add(time.Hour)); err != nil {
		t.Fatalf("a reinstated annulment beside the open one: %v; want it stored", err)
	}
}
