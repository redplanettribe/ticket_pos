package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Sale Invoice Reissue (#483, parent #478, ADR 0061): a Platform
// Operator corrects an authorized Sale Invoice's Recipient by owing, in one
// transaction, a Credit Note for the full amount against it (reason
// `reissue`, the same Recipient as the factura) and a corrected Sale Invoice
// to the Recipient as entered — email from the Sale as it stands, the
// original lines, the stored rate — linked to the factura it supersedes. The
// Drainer works the Credit Note first and signs the corrected factura only
// once that Credit Note is authorized, so a Sale never holds two authorized
// facturas. The Sale, its buyer snapshot, its Tickets and the Customer's
// stored Tax ID are never written.
//
// Everything is asserted through the operator API (the reissue response, the
// detail, the list), the drain response, what the fake SRI received and in
// which order, and the captured mail; the Sale's rows are read directly
// where "byte-for-byte unchanged" has no surface.

func reissuePath(id string) string {
	return invoicesPath + "/" + id + "/reissue"
}

// reissueRecipientBody is the corrected Recipient as the operator states it.
// There is no email: it is the Sale's, never the caller's.
type reissueRecipientBody struct {
	TaxIDType string `json:"tax_id_type"`
	TaxID     string `json:"tax_id"`
	LegalName string `json:"legal_name"`
	Address   string `json:"address,omitempty"`
}

type reissueBody struct {
	Recipient reissueRecipientBody `json:"recipient"`
	Note      *string              `json:"note,omitempty"`
}

// reissuedInvoiceView is the detail as the reissue's tests read it: the
// chain links and the reissue's trail beside the Drainer's facts.
type reissuedInvoiceView struct {
	drainedInvoiceView
	Recipient             recipientView `json:"recipient"`
	SupersedesInvoiceID   *string       `json:"supersedes_invoice_id"`
	SupersededByInvoiceID *string       `json:"superseded_by_invoice_id"`
	ReissuedBy            *string       `json:"reissued_by"`
	ReissuedAt            *string       `json:"reissued_at"`
	ReissueNote           *string       `json:"reissue_note"`
}

// recipientView is the Recipient as every document shows it, comparable so
// "the same Recipient as the factura" is one equality.
type recipientView struct {
	TaxIDType string `json:"tax_id_type"`
	TaxID     string `json:"tax_id"`
	LegalName string `json:"legal_name"`
	Address   string `json:"address"`
	Email     string `json:"email"`
}

func reissueInvoice(t *testing.T, sessionID, id string, body reissueBody) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if sessionID != "" {
		headers = authHeader(sessionID)
	}
	return sriEnv.post(t, reissuePath(id), body, headers)
}

func reissueOK(t *testing.T, sessionID, id string, body reissueBody) reissuedInvoiceView {
	t.Helper()
	resp, env := reissueInvoice(t, sessionID, id, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reissue status=%d error=%+v; want 201", resp.StatusCode, env.Error)
	}
	if env.Error != nil {
		t.Fatalf("reissue error=%+v; want none", env.Error)
	}
	var view reissuedInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode reissued invoice: %v", err)
	}
	return view
}

func getReissuedInvoice(t *testing.T, sessionID, id string) reissuedInvoiceView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view reissuedInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// companyRecipient is the corrected Recipient every success test enters: a
// company's RUC and razón social with an address, where the Sale carried a
// person's cédula and "First Last" with none.
func companyRecipient() reissueBody {
	return reissueBody{
		Recipient: reissueRecipientBody{
			TaxIDType: "ruc",
			TaxID:     companyRUC,
			LegalName: "ORGANIZACION EJEMPLO S.A.",
			Address:   "Av. República del Salvador, Quito",
		},
		Note: strPtr("buyer wrote in: the factura goes to the company"),
	}
}

// saleSnapshot reads the Sale row, its lines, its Tickets and the buyer's
// stored Tax ID as one JSON text, directly: "byte-for-byte unchanged" has no
// API surface, and a reissue that rewrote any of them would be the bug.
func saleSnapshot(t *testing.T, env *testEnv, saleID string) string {
	t.Helper()
	var snapshot string
	if err := env.db.QueryRow(`
		SELECT jsonb_build_object(
			'sale', (SELECT to_jsonb(ts) FROM ticket_sales ts WHERE ts.id = $1),
			'lines', (SELECT jsonb_agg(to_jsonb(l) ORDER BY l.id) FROM ticket_sale_lines l WHERE l.ticket_sale_id = $1),
			'tickets', (SELECT jsonb_agg(to_jsonb(tk) ORDER BY tk.id) FROM tickets tk
			            JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id WHERE l.ticket_sale_id = $1),
			'customer', (SELECT jsonb_build_object('tax_id_type', c.tax_id_type, 'tax_id_number', c.tax_id_number)
			             FROM customers c JOIN ticket_sales ts ON ts.customer_id = c.id WHERE ts.id = $1)
		)::text
	`, saleID).Scan(&snapshot); err != nil {
		t.Fatalf("read the sale snapshot: %v", err)
	}
	return snapshot
}

// documentsOf reads the Sale's documents through the operator's Sale lookup
// surface — the invoicing list narrowed to the Sale — oldest first, by kind.
func documentsOfSale(t *testing.T, operatorSessionID, ref string) []saleDocumentRow {
	t.Helper()
	var out []saleDocumentRow
	list := getSaleInvoiceList(t, operatorSessionID)
	for i := len(list.Data) - 1; i >= 0; i-- {
		if row := list.Data[i]; row.SaleConfirmationRef != nil && *row.SaleConfirmationRef == ref {
			out = append(out, saleDocumentRow{ID: row.ID, Kind: row.Kind, Status: row.Status})
		}
	}
	return out
}

type saleDocumentRow struct {
	ID, Kind, Status string
}

// TestReissueOwesACreditNoteAndACorrectedSaleInvoice: the success path. The
// operator reissues an authorized factura to a company. The response is the
// corrected document — owed, unsigned, due at once, the Recipient as entered
// with the Sale's email, the original lines and totals at the stored rate,
// superseding the factura, stamped with who, when and the note — and the
// list holds three documents: the factura, now superseded and credited by
// an owed reissue Credit Note naming the factura's own Recipient, and the
// corrected one. The Sale is byte-for-byte what it was.
func TestReissueOwesACreditNoteAndACorrectedSaleInvoice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, saleID := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	before := saleSnapshot(t, env, saleID)
	factura := getReissuedInvoice(t, operatorSessionID, facturaID)
	if factura.SupersededByInvoiceID != nil || factura.SupersedesInvoiceID != nil || factura.ReissuedBy != nil {
		t.Fatalf("an unreissued factura carries chain facts: %+v", factura)
	}

	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())

	if corrected.ID == facturaID || corrected.Kind != "sale" || corrected.Status != "owed" || corrected.Number != nil || corrected.EcuadorFull != nil || corrected.NextAttemptAt == nil {
		t.Fatalf("corrected document = %s %s number %v ecuador %v next %v; want a new owed Sale Invoice, unsigned, due", corrected.Kind, corrected.Status, corrected.Number, corrected.EcuadorFull, corrected.NextAttemptAt)
	}
	if corrected.SupersedesInvoiceID == nil || *corrected.SupersedesInvoiceID != facturaID || corrected.SupersededByInvoiceID != nil {
		t.Fatalf("corrected document supersedes %v, superseded by %v; want the factura and nobody", corrected.SupersedesInvoiceID, corrected.SupersededByInvoiceID)
	}
	r := corrected.Recipient
	if r.TaxIDType != "ruc" || r.TaxID != companyRUC || r.LegalName != "ORGANIZACION EJEMPLO S.A." || r.Address != "Av. República del Salvador, Quito" || r.Email != "guest@example.com" {
		t.Fatalf("corrected recipient = %+v; want the company as entered with the Sale's email", r)
	}
	if corrected.SaleConfirmationRef == nil || *corrected.SaleConfirmationRef != ref || corrected.IVARate == nil || *corrected.IVARate != "15" {
		t.Fatalf("corrected document sale ref %v rate %v; want %s at 15", corrected.SaleConfirmationRef, corrected.IVARate, ref)
	}
	if corrected.Totals != factura.Totals || corrected.Totals.TotalCents != 1115 {
		t.Fatalf("corrected totals = %+v; want the factura's %+v", corrected.Totals, factura.Totals)
	}
	if len(corrected.Lines) != len(factura.Lines) || corrected.Lines[0].Description != factura.Lines[0].Description || corrected.Lines[0].UnitPriceCents != factura.Lines[0].UnitPriceCents || corrected.Lines[0].BaseCents != factura.Lines[0].BaseCents {
		t.Fatalf("corrected lines = %+v; want the factura's %+v", corrected.Lines, factura.Lines)
	}
	if corrected.ReissuedBy == nil || *corrected.ReissuedBy != "operator@example.com" || corrected.ReissuedAt == nil || *corrected.ReissuedAt != fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("reissued by %v at %v; want the operator's session email at the platform's clock", corrected.ReissuedBy, corrected.ReissuedAt)
	}
	if corrected.ReissueNote == nil || *corrected.ReissueNote != "buyer wrote in: the factura goes to the company" {
		t.Fatalf("reissue note = %v; want the note as stated", corrected.ReissueNote)
	}
	if corrected.CreditsInvoiceID != nil || corrected.CreditedByInvoiceID != nil || corrected.CreditNoteReason != nil {
		t.Fatalf("a corrected factura carries credit facts: %+v", corrected)
	}

	// The list: three documents on the Sale.
	docs := documentsOfSale(t, operatorSessionID, ref)
	if len(docs) != 3 {
		t.Fatalf("the Sale's documents = %+v; want the factura, the Credit Note and the corrected factura", docs)
	}
	noteID := creditNoteOf(t, operatorSessionID)
	note := getReissuedInvoice(t, operatorSessionID, noteID)
	if note.Status != "owed" || note.Number != nil || note.NextAttemptAt == nil || note.CreditsInvoiceID == nil || *note.CreditsInvoiceID != facturaID || note.CreditNoteReason == nil || *note.CreditNoteReason != "reissue" {
		t.Fatalf("credit note = %s number %v next %v credits %v for %v; want owed, unsigned, due, crediting the factura for reissue", note.Status, note.Number, note.NextAttemptAt, note.CreditsInvoiceID, note.CreditNoteReason)
	}
	if note.Recipient != factura.Recipient || note.Recipient.TaxID != validCedula || note.Recipient.LegalName != "Ana Lopez" {
		t.Fatalf("credit note recipient = %+v; want the superseded factura's %+v, never the corrected one", note.Recipient, factura.Recipient)
	}
	if note.Totals != factura.Totals || len(note.Lines) != len(factura.Lines) {
		t.Fatalf("credit note totals %+v lines %d; want the factura's %+v and %d", note.Totals, len(note.Lines), factura.Totals, len(factura.Lines))
	}
	// The reissue's trail is read beside the Credit Note too.
	if note.ReissuedBy == nil || *note.ReissuedBy != "operator@example.com" || note.ReissueNote == nil {
		t.Fatalf("credit note reissue trail = %v / %v; want the operator and the note", note.ReissuedBy, note.ReissueNote)
	}

	// The superseded factura: still authorized, linked both ways.
	factura = getReissuedInvoice(t, operatorSessionID, facturaID)
	if factura.Status != "authorized" || factura.SupersededByInvoiceID == nil || *factura.SupersededByInvoiceID != corrected.ID || factura.CreditedByInvoiceID == nil || *factura.CreditedByInvoiceID != noteID {
		t.Fatalf("superseded factura = %s superseded by %v credited by %v; want still authorized, superseded by the corrected one, credited by the note", factura.Status, factura.SupersededByInvoiceID, factura.CreditedByInvoiceID)
	}
	if factura.ReissuedBy == nil || *factura.ReissuedBy != "operator@example.com" || factura.ReissueNote == nil || *factura.ReissueNote != "buyer wrote in: the factura goes to the company" {
		t.Fatalf("superseded factura reissue trail = %v / %v; want the operator and the note", factura.ReissuedBy, factura.ReissueNote)
	}
	if factura.Recipient.TaxID != validCedula || factura.Recipient.LegalName != "Ana Lopez" {
		t.Fatalf("the superseded factura's Recipient was rewritten: %+v", factura.Recipient)
	}

	// Nothing was sent, nothing was mailed, no number consumed yet.
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want still the factura alone", n)
	}
	if n := sequenceRows(t, env); n != 1 {
		t.Fatalf("sequence rows = %d; want the factura's alone", n)
	}

	// The Sale is untouched: row, lines, Tickets, the Customer's Tax ID.
	if after := saleSnapshot(t, env, saleID); after != before {
		t.Fatalf("the Sale changed under the reissue:\nbefore %s\nafter  %s", before, after)
	}
	if status, _, _ := saleProvenance(t, env, ref); status != "active" {
		t.Fatalf("sale status = %s; want active, a reissue reverses nothing", status)
	}
}

// TestReissueTakesTheEmailTheSaleCarriesNow: the Sale was re-addressed
// before the reissue (ADR 0058). The superseded factura keeps the address it
// was issued to; the corrected one carries the address the Sale carries
// now, and nobody typed it.
func TestReissueTakesTheEmailTheSaleCarriesNow(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	env.email.Reset()
	reAddressOK(t, payphoneEnv, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	token := reAddressingTokenFrom(t, reAddressingMailFor(t, env, "ana.lopez@example.com"))
	acceptAndSignIn(t, env, token)

	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())

	if corrected.Recipient.Email != "ana.lopez@example.com" {
		t.Fatalf("corrected recipient email = %q; want the re-addressed Sale's", corrected.Recipient.Email)
	}
	if factura := getReissuedInvoice(t, operatorSessionID, facturaID); factura.Recipient.Email != "guest@example.com" {
		t.Fatalf("superseded factura email = %q; want the one it was issued to", factura.Recipient.Email)
	}
	if note := getReissuedInvoice(t, operatorSessionID, creditNoteOf(t, operatorSessionID)); note.Recipient.Email != "guest@example.com" {
		t.Fatalf("credit note email = %q; want the superseded factura's", note.Recipient.Email)
	}
}

// TestReissueDrainsTheCreditNoteBeforeTheCorrectedSaleInvoice: the ordinary
// case. One drain claims the Credit Note first — the corrected factura is
// not claimable until that Credit Note is authorized — signs it under 04
// with the correction motivo, authorizes and mails it, then claims the
// corrected factura, signs it under 01 as secuencial 2 to the company and
// authorizes and mails it. The SRI receives the nota de crédito before the
// corrected factura. A second reissue then corrects the corrected factura;
// the superseded one refuses.
func TestReissueDrainsTheCreditNoteBeforeTheCorrectedSaleInvoice(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, saleID := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	env.email.Reset()
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	noteID := creditNoteOf(t, operatorSessionID)

	result := drainSaleInvoices(t)
	if result.Claimed != 2 || result.Authorized != 2 || result.Delivered != 2 || result.Withdrawn != 0 || result.Failed != 0 {
		t.Fatalf("drain = %+v; want the Credit Note and then the corrected factura authorized and delivered", result)
	}

	received := sriStub.allReceived()
	if len(received) != 3 {
		t.Fatalf("the SRI received %d documents; want the factura, the nota de crédito, the corrected factura", len(received))
	}
	if !strings.Contains(string(received[1].signedXML), "<notaCredito") || !strings.Contains(string(received[2].signedXML), "<factura") {
		t.Fatalf("the SRI received them out of order: second %.40s, third %.40s", received[1].signedXML, received[2].signedXML)
	}
	if !strings.Contains(string(received[1].signedXML), "<motivo>Corrección de los datos del receptor</motivo>") {
		t.Fatalf("the nota de crédito's motivo is not the correction text:\n%s", received[1].signedXML)
	}
	if !strings.Contains(string(received[2].signedXML), "<identificacionComprador>"+companyRUC+"</identificacionComprador>") ||
		!strings.Contains(string(received[2].signedXML), "<razonSocialComprador>ORGANIZACION EJEMPLO S.A.</razonSocialComprador>") ||
		!strings.Contains(string(received[2].signedXML), "<tipoIdentificacionComprador>04</tipoIdentificacionComprador>") ||
		!strings.Contains(string(received[2].signedXML), "<direccionComprador>Av. República del Salvador, Quito</direccionComprador>") {
		t.Fatalf("the corrected factura does not name the company:\n%s", received[2].signedXML)
	}
	validateReceivedAgainstXSD(t, received[1].signedXML)
	validateReceivedAgainstXSD(t, received[2].signedXML)

	note := getReissuedInvoice(t, operatorSessionID, noteID)
	if note.Status != "authorized" || note.Number == nil || *note.Number != "001-001-000000001" || note.DeliveredAt == nil {
		t.Fatalf("credit note = %s %v delivered %v; want authorized, numbered 1 in the 04 sequence, delivered", note.Status, note.Number, note.DeliveredAt)
	}
	after := getReissuedInvoice(t, operatorSessionID, corrected.ID)
	if after.Status != "authorized" || after.Number == nil || *after.Number != "001-001-000000002" || after.DeliveredAt == nil || after.NextAttemptAt != nil {
		t.Fatalf("corrected factura = %s %v delivered %v next %v; want authorized, the 01 sequence's second number, delivered, off the queue", after.Status, after.Number, after.DeliveredAt, after.NextAttemptAt)
	}
	if after.IssuedBy == nil || *after.IssuedBy != "sale-invoice-drainer" {
		t.Fatalf("corrected factura issued_by = %v; want the Drainer", after.IssuedBy)
	}

	// The buyer's two mails: a correction, then an ordinary factura.
	sent := deliveriesSent(t, env)
	if len(sent) != 2 || sent[0].Kind != "credit_note" || sent[0].Reason != "reissue" || sent[1].Kind != "sale" || sent[1].To != "guest@example.com" {
		t.Fatalf("deliveries = %+v; want the Credit Note's (reissue) then the corrected factura's", sent)
	}
	buyer := buyerSession(t, env, "guest@example.com")
	if docs, _ := listCustomerDocuments(t, buyer, saleID); len(docs) != 3 || docs[2].Status != "authorized" || docs[2].DownloadURL == nil {
		t.Fatalf("the buyer's documents = %+v; want three, the corrected factura authorized and downloadable", docs)
	}

	// Settled: nothing more is due.
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("second drain = %+v; want nothing claimed", again)
	}

	// The superseded factura refuses; the corrected one may be corrected.
	resp, body := reissueInvoice(t, operatorSessionID, facturaID, companyRecipient())
	expectRefusal(t, "reissue of the superseded factura", resp, body, http.StatusConflict, "INVOICE_SUPERSEDED")
	second := reissueOK(t, operatorSessionID, corrected.ID, reissueBody{Recipient: reissueRecipientBody{
		TaxIDType: "cedula", TaxID: otherCedula, LegalName: "Beatriz Mora",
	}})
	if second.SupersedesInvoiceID == nil || *second.SupersedesInvoiceID != corrected.ID || second.Recipient.TaxID != otherCedula || second.ReissueNote != nil {
		t.Fatalf("second reissue = supersedes %v recipient %+v note %v; want the corrected factura, Beatriz, no note", second.SupersedesInvoiceID, second.Recipient, second.ReissueNote)
	}
	if docs := documentsOfSale(t, operatorSessionID, ref); len(docs) != 5 {
		t.Fatalf("the Sale's documents = %d; want five: two facturas superseded, two Credit Notes, one current factura", len(docs))
	}
	if status, _, _ := saleProvenance(t, env, ref); status != "active" {
		t.Fatalf("sale status = %s; want active", status)
	}
}

// TestTheCorrectedSaleInvoiceWaitsUntilTheCreditNoteIsAuthorized: the SRI
// keeps the nota de crédito EN PROCESAMIENTO. The first drain signs and
// submits the Credit Note alone and leaves it pending; the corrected
// factura stays owed, unsigned, untouched — the SRI received nothing under
// its name and no 01 number was consumed. Once a late AUTORIZADO settles
// the Credit Note, the next drain issues the corrected factura in the same
// run.
func TestTheCorrectedSaleInvoiceWaitsUntilTheCreditNoteIsAuthorized(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	env.email.Reset()
	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
	noteID := creditNoteOf(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })

	result := drainSaleInvoices(t)
	if result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("first drain = %+v; want the Credit Note alone, pending", result)
	}
	if note := getReissuedInvoice(t, operatorSessionID, noteID); note.Status != "pending" || note.Number == nil {
		t.Fatalf("credit note after the first drain = %s %v; want pending and numbered", note.Status, note.Number)
	}
	waiting := getReissuedInvoice(t, operatorSessionID, corrected.ID)
	if waiting.Status != "owed" || waiting.Number != nil || waiting.EcuadorFull != nil || len(waiting.AttemptRows) != 0 {
		t.Fatalf("corrected factura after the first drain = %s number %v ecuador %v attempts %d; want owed, unsigned, untouched", waiting.Status, waiting.Number, waiting.EcuadorFull, len(waiting.AttemptRows))
	}
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want the factura and the nota de crédito alone", n)
	}
	var last int64
	if err := env.db.QueryRow(`SELECT last_secuencial FROM invoicing_sequences_ec WHERE cod_doc = '01'`).Scan(&last); err != nil || last != 1 {
		t.Fatalf("01 sequence = %d (%v); want still at 1, the corrected factura consumed no number", last, err)
	}

	// Still pending a minute later: the factura still waits.
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("drain with the Credit Note still pending = %+v; want the Credit Note alone polled", result)
	}
	if n := sriStub.receptionCount(); n != 2 {
		t.Fatalf("the SRI received %d documents; want still two", n)
	}

	// The Credit Note authorizes: the corrected factura follows in the same drain.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	atInvoicingClock(t, fixedClock.Add(6*time.Minute))
	result = drainSaleInvoices(t)
	if result.Claimed != 2 || result.Authorized != 2 || result.Delivered != 2 {
		t.Fatalf("drain with a late AUTORIZADO = %+v; want the Credit Note and then the corrected factura authorized and delivered", result)
	}
	received := sriStub.allReceived()
	if len(received) != 3 || !strings.Contains(string(received[1].signedXML), "<notaCredito") || !strings.Contains(string(received[2].signedXML), "<factura") {
		t.Fatalf("the SRI received %d documents, the nota de crédito must precede the corrected factura", len(received))
	}
	if after := getReissuedInvoice(t, operatorSessionID, corrected.ID); after.Status != "authorized" || after.Number == nil || *after.Number != "001-001-000000002" {
		t.Fatalf("corrected factura = %s %v; want authorized as the 01 sequence's second number", after.Status, after.Number)
	}
	if sent := deliveriesSent(t, env); len(sent) != 2 || sent[0].Kind != "credit_note" || sent[1].Kind != "sale" {
		t.Fatalf("deliveries = %+v; want the Credit Note's then the corrected factura's", sent)
	}
}

// TestReissueIsRefusedWhereItCannotCorrect: every refusal, each by its own
// code, and none of them writes a thing. A manual Tax Invoice and a Credit
// Note are refused by kind; a factura that is owed, pending, needs_attention,
// withdrawn or annulled by state; a reversed Sale's factura, a factura with
// a reissue in flight on its Sale, and a superseded factura by the chain.
func TestReissueIsRefusedWhereItCannotCorrect(t *testing.T) {
	countDocuments := func(t *testing.T, sessionID string) int {
		t.Helper()
		return getSaleInvoiceList(t, sessionID).Pagination.Total
	}
	refuse := func(t *testing.T, what, sessionID, id, code string) {
		t.Helper()
		before := countDocuments(t, sessionID)
		resp, body := reissueInvoice(t, sessionID, id, companyRecipient())
		expectRefusal(t, what, resp, body, http.StatusConflict, code)
		if after := countDocuments(t, sessionID); after != before {
			t.Fatalf("%s: %d documents before, %d after; a refusal writes nothing", what, before, after)
		}
	}

	t.Run("unknown id", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID := operatorSession(t, env, "operator@example.com")
		resp, body := reissueInvoice(t, operatorSessionID, "00000000-0000-0000-0000-000000000000", companyRecipient())
		expectRefusal(t, "reissue of an unknown id", resp, body, http.StatusNotFound, "INVOICE_NOT_FOUND")
		resp, body = reissueInvoice(t, operatorSessionID, "not-a-uuid", companyRecipient())
		expectRefusal(t, "reissue of a malformed id", resp, body, http.StatusNotFound, "INVOICE_NOT_FOUND")
	})
	t.Run("manual tax invoice", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID := operatorSession(t, env, "operator@example.com")
		issuerReady(t, operatorSessionID)
		manual := issueOK(t, operatorSessionID, validInvoiceBody())
		if manual.Status != "authorized" {
			t.Fatalf("setup: manual invoice is %s", manual.Status)
		}
		refuse(t, "reissue of a manual Tax Invoice", operatorSessionID, manual.ID, "INVOICE_MANUAL_NOT_REISSUABLE")
	})
	t.Run("credit note", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
		reissueOK(t, operatorSessionID, facturaID, companyRecipient())
		noteID := creditNoteOf(t, operatorSessionID)
		refuse(t, "reissue of a Credit Note", operatorSessionID, noteID, "CREDIT_NOTE_NOT_REISSUABLE")
		if result := drainSaleInvoices(t); result.Authorized != 2 {
			t.Fatalf("drain = %+v", result)
		}
		refuse(t, "reissue of an authorized Credit Note", operatorSessionID, noteID, "CREDIT_NOTE_NOT_REISSUABLE")
	})
	t.Run("owed", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		refuse(t, "reissue of an owed factura", operatorSessionID, invoiceID, "INVOICE_NOT_AUTHORIZED")
	})
	t.Run("pending", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		issuerReady(t, operatorSessionID)
		sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
		if result := drainSaleInvoices(t); result.Pending != 1 {
			t.Fatalf("setup drain = %+v", result)
		}
		refuse(t, "reissue of a pending factura", operatorSessionID, invoiceID, "INVOICE_NOT_AUTHORIZED")
	})
	t.Run("needs attention, then annulled", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		issuerReady(t, operatorSessionID)
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
		})
		if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
			t.Fatalf("setup drain = %+v", result)
		}
		refuse(t, "reissue of a parked factura", operatorSessionID, invoiceID, "INVOICE_NOT_AUTHORIZED")
		annulOK(t, operatorSessionID, invoiceID)
		refuse(t, "reissue of an annulled factura", operatorSessionID, invoiceID, "INVOICE_NOT_AUTHORIZED")
	})
	t.Run("withdrawn", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		reversalRoutes[0].reverse(t, env, operatorSessionID, lastConfirmation(t, env).Reference)
		if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.Status != "withdrawn" {
			t.Fatalf("setup: %s", detail.Status)
		}
		refuse(t, "reissue of a withdrawn factura", operatorSessionID, invoiceID, "INVOICE_NOT_AUTHORIZED")
	})
	t.Run("reversed sale", func(t *testing.T) {
		for _, route := range reversalRoutes {
			t.Run(route.name, func(t *testing.T) {
				env := setupTest(t)
				operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
				route.reverse(t, env, operatorSessionID, lastConfirmation(t, env).Reference)
				refuse(t, "reissue of a reversed Sale's factura", operatorSessionID, facturaID, "INVOICE_SALE_REVERSED")
			})
		}
	})
	t.Run("reissue in flight, then superseded", func(t *testing.T) {
		env := setupTest(t)
		operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
		corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())
		refuse(t, "second reissue while the first is in flight", operatorSessionID, facturaID, "REISSUE_IN_FLIGHT")
		refuse(t, "reissue of the owed corrected factura", operatorSessionID, corrected.ID, "INVOICE_NOT_AUTHORIZED")
		// The Credit Note authorized, the corrected factura still owed: the
		// chain is still in flight.
		sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
		if result := drainSaleInvoices(t); result.Pending != 1 {
			t.Fatalf("setup drain = %+v", result)
		}
		refuse(t, "second reissue with the Credit Note pending", operatorSessionID, facturaID, "REISSUE_IN_FLIGHT")
		sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
		atInvoicingClock(t, fixedClock.Add(6*time.Minute))
		if result := drainSaleInvoices(t); result.Authorized != 2 {
			t.Fatalf("settling drain = %+v", result)
		}
		refuse(t, "reissue of the superseded factura", operatorSessionID, facturaID, "INVOICE_SUPERSEDED")
	})
}

// TestReissueValidatesTheCorrectedRecipient: the Tax ID is validated with
// checkout's rules and refused on the same field-level codes; the legal
// name is required; the note is bounded as an Operator Reversal's is. Every
// failing field is named at once and nothing is written.
func TestReissueValidatesTheCorrectedRecipient(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)

	for _, tc := range []struct {
		name  string
		body  reissueBody
		field string
	}{
		{"a cédula with a wrong check digit", reissueBody{Recipient: reissueRecipientBody{TaxIDType: "cedula", TaxID: invalidCedula, LegalName: "Ana Lopez"}}, "recipient.tax_id"},
		{"an unknown Tax ID Type", reissueBody{Recipient: reissueRecipientBody{TaxIDType: "dni", TaxID: validCedula, LegalName: "Ana Lopez"}}, "recipient.tax_id_type"},
		{"no Tax ID", reissueBody{Recipient: reissueRecipientBody{TaxIDType: "cedula", LegalName: "Ana Lopez"}}, "recipient.tax_id"},
		{"no legal name", reissueBody{Recipient: reissueRecipientBody{TaxIDType: "ruc", TaxID: companyRUC, LegalName: "  "}}, "recipient.legal_name"},
		{"a note past the bound", reissueBody{Recipient: reissueRecipientBody{TaxIDType: "ruc", TaxID: companyRUC, LegalName: "ORGANIZACION EJEMPLO S.A."}, Note: strPtr(strings.Repeat("x", 501))}, "note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := reissueInvoice(t, operatorSessionID, facturaID, tc.body)
			if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
				t.Fatalf("status=%d error=%+v; want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
			}
			if fields := fieldErrors(t, body); fields[tc.field] == "" {
				t.Fatalf("field errors = %v; want %s named", fields, tc.field)
			}
		})
	}
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 1 {
		t.Fatalf("documents after the refusals = %d; want the factura alone", list.Pagination.Total)
	}
	// A passport is accepted on shape alone and stored upper-case, as
	// checkout stores it; a 500-character note is within the bound.
	corrected := reissueOK(t, operatorSessionID, facturaID, reissueBody{
		Recipient: reissueRecipientBody{TaxIDType: "passport", TaxID: lowercasePassprt, LegalName: "Ana Lopez"},
		Note:      strPtr(strings.Repeat("y", 500)),
	})
	if corrected.Recipient.TaxIDType != "passport" || corrected.Recipient.TaxID != strings.ToUpper(lowercasePassprt) {
		t.Fatalf("corrected recipient = %+v; want the passport normalised as checkout does", corrected.Recipient)
	}
}

// TestReissueIsOperatorsOnly: a missing session is 401 and an Org Admin of
// the very House Organization is 403, and nothing was written.
func TestReissueIsOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	adminSessionID := verifyOTP(t, env, "admin@example.com")

	resp, body := reissueInvoice(t, "", facturaID, companyRecipient())
	expectRefusal(t, "reissue unauthenticated", resp, body, http.StatusUnauthorized, "UNAUTHORIZED")
	resp, body = reissueInvoice(t, adminSessionID, facturaID, companyRecipient())
	expectRefusal(t, "reissue as org_admin", resp, body, http.StatusForbidden, "FORBIDDEN")
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 1 {
		t.Fatalf("documents after the refusals = %d; want the factura alone", list.Pagination.Total)
	}
}

// TestReissueIsHiddenWhileSaleInvoicingIsClosed: with SALE_INVOICING_ENABLED
// closed the endpoint answers 404 SALE_INVOICING_UNAVAILABLE before anything
// is read, on the flag's own terms — even for a factura that was authorized
// while the flag was open.
func TestReissueIsHiddenWhileSaleInvoicingIsClosed(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	closed := startAppWithSaleInvoicingClosed(t)

	resp, body := closed.post(t, reissuePath(facturaID), companyRecipient(), authHeader(operatorSessionID))
	expectRefusal(t, "reissue while closed", resp, body, http.StatusNotFound, "SALE_INVOICING_UNAVAILABLE")
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 1 {
		t.Fatalf("documents after the refusal = %d; want the factura alone", list.Pagination.Total)
	}
}

// TestReissueLosesToAReversalItRaces: the guarded write. The reissue reads
// an authorized, current factura of a Sale that stands, and between that
// read and its write the buyer's reversal commits. The write is conditioned
// on the same facts, under the Sale's own lock, so it finds the Sale
// reversed and refuses — one Credit Note exists, the reversal's, and no
// corrected factura.
//
// The two are queued behind a lock the test holds on the Sale row — FOR NO
// KEY UPDATE, which lets the reversal record its request (a foreign key to
// the Sale) and blocks only the FOR UPDATE each of them takes to act: the
// reversal's commit first, then the reissue, whose pre-read has already
// seen the factura eligible. Released, the reversal commits first and the
// reissue's transaction decides on what it then finds.
func TestReissueLosesToAReversalItRaces(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, saleID := houseSaleAuthorized(t, env)
	ref := lastConfirmation(t, env).Reference
	buyer := buyerSession(t, env, "guest@example.com")
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, buyer, ""), ref)

	hold, err := env.db.Begin()
	if err != nil {
		t.Fatalf("begin hold: %v", err)
	}
	t.Cleanup(func() { _ = hold.Rollback() })
	if _, err := hold.Exec(`SELECT id FROM ticket_sales WHERE id = $1 FOR NO KEY UPDATE`, saleID); err != nil {
		t.Fatalf("hold the Sale: %v", err)
	}
	waiters := func() int {
		var n int
		if err := env.db.QueryRow(`SELECT COUNT(*) FROM pg_locks WHERE NOT granted`).Scan(&n); err != nil {
			t.Fatalf("count lock waiters: %v", err)
		}
		return n
	}
	waitForWaiters := func(want int) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for waiters() < want {
			if time.Now().After(deadline) {
				t.Fatalf("never saw %d transactions waiting on the Sale", want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reverseSaleOK(t, payphoneEnv, buyer, sale.ID)
	}()
	waitForWaiters(1)

	var resp *http.Response
	var body envelope
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, body = reissueInvoice(t, operatorSessionID, facturaID, companyRecipient())
	}()
	waitForWaiters(2)

	_ = hold.Rollback()
	wg.Wait()

	expectRefusal(t, "reissue that raced a reversal", resp, body, http.StatusConflict, "INVOICE_SALE_REVERSED")
	if status, _, _ := saleProvenance(t, env, ref); status != "reversed" {
		t.Fatalf("sale status = %s; want reversed", status)
	}
	docs := documentsOfSale(t, operatorSessionID, ref)
	if len(docs) != 2 || docs[1].Kind != "credit_note" {
		t.Fatalf("the Sale's documents = %+v; want the factura and the reversal's Credit Note alone", docs)
	}
	noteID := creditNoteOf(t, operatorSessionID)
	if note := getReissuedInvoice(t, operatorSessionID, noteID); note.CreditNoteReason == nil || *note.CreditNoteReason != "customer" {
		t.Fatalf("credit note reason = %v; want the reversal's, never reissue", note.CreditNoteReason)
	}
	if factura := getReissuedInvoice(t, operatorSessionID, facturaID); factura.SupersededByInvoiceID != nil {
		t.Fatalf("the factura was superseded by a reissue that lost: %v", *factura.SupersededByInvoiceID)
	}
}

// TestReissueKicksTheDrainer: with the post-commit kick on, both documents
// are worked within seconds of the reissue, in order, with nothing pressed.
func TestReissueKicksTheDrainer(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, facturaID, _ := houseSaleAuthorized(t, env)
	sriApp.InvoicingService.WithSaleInvoiceKick(true)
	t.Cleanup(func() { sriApp.InvoicingService.WithSaleInvoiceKick(false) })

	corrected := reissueOK(t, operatorSessionID, facturaID, companyRecipient())

	deadline := time.Now().Add(10 * time.Second)
	for {
		after := getReissuedInvoice(t, operatorSessionID, corrected.ID)
		if after.Status == "authorized" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the kick never authorized the corrected factura: %s", after.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	received := sriStub.allReceived()
	if len(received) != 3 || !strings.Contains(string(received[1].signedXML), "<notaCredito") || !strings.Contains(string(received[2].signedXML), "<factura") {
		t.Fatalf("the SRI received %d documents; want the nota de crédito before the corrected factura", len(received))
	}
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("scheduled drain after the kick = %+v; want nothing due", result)
	}
}
