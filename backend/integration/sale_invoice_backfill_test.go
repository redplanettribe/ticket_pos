package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The Sale Invoice Backfill (#508 and #509, parent #506, ADR 0064): a
// Platform Operator selects Uninvoiced House Sales and owes each an ordinary
// Sale Invoice, dated the day of the act and never the day of the sale,
// stamped with who owed it and when. Each sale is worked in its own
// transaction — a refused sale blocks nothing, the same sale twice is
// refused the second time — and one Drainer kick follows. From then on
// the document is any Sale Invoice: signed and authorized by the fake SRI,
// delivered to the buyer with the standard mail (XML + RIDE), credited on
// reversal. The detail exposes the trail; a checkout-born document carries
// none.
//
// The sales are paid through the app whose SALE_INVOICING_ENABLED is
// closed (sale_invoicing_flag_test.go), which is how a paid House sale ends
// up owing nothing; the act, the list, the drain and the detail are asked
// of the SRI app. Everything is asserted through the API, the drain
// response, the captured mail and what the fake SRI received; SQL moves
// sold_at, reprices one line, and counts rows where no surface states
// "exactly one" or "no number consumed".

const backfillPath = uninvoicedSalesPath + "/backfill"

type backfillResultView struct {
	Owed []struct {
		TicketSaleID string `json:"ticket_sale_id"`
		InvoiceID    string `json:"invoice_id"`
	} `json:"owed"`
	Refused []struct {
		TicketSaleID string `json:"ticket_sale_id"`
		Code         string `json:"code"`
	} `json:"refused"`
}

// backfilledInvoiceView is the detail as the backfill's tests read it: the
// trail (#509) beside the Drainer's facts.
type backfilledInvoiceView struct {
	drainedInvoiceView
	BackfilledBy *string `json:"backfilled_by"`
	BackfilledAt *string `json:"backfilled_at"`
}

func backfillSales(t *testing.T, env *testEnv, sessionID string, ids []string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if sessionID != "" {
		headers = authHeader(sessionID)
	}
	return env.post(t, backfillPath, map[string]any{"ticket_sale_ids": ids}, headers)
}

func backfillOK(t *testing.T, sessionID string, ids []string) backfillResultView {
	t.Helper()
	resp, body := backfillSales(t, sriEnv, sessionID, ids)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backfill status=%d error=%+v; want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil || body.RequestID == "" {
		t.Fatalf("backfill envelope = error %+v request_id %q; want null error and a request id", body.Error, body.RequestID)
	}
	var result backfillResultView
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode backfill result: %v", err)
	}
	return result
}

func getBackfilledInvoice(t *testing.T, sessionID, id string) backfilledInvoiceView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view backfilledInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// invoiceRowsOfSale counts the invoicing rows naming a Ticket Sale: no
// surface states "exactly one document for this sale".
func invoiceRowsOfSale(t *testing.T, env *testEnv, saleID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM invoicing_invoices WHERE ticket_sale_id = $1`, saleID).Scan(&n); err != nil {
		t.Fatalf("count invoices of sale: %v", err)
	}
	return n
}

// lastFacturaSecuencial reads where the 01 sequence stands: the one fact
// that says how many numbers were consumed, whatever the row count.
func lastFacturaSecuencial(t *testing.T, env *testEnv) int64 {
	t.Helper()
	var last int64
	if err := env.db.QueryRow(`SELECT COALESCE(MAX(last_secuencial), 0) FROM invoicing_sequences_ec WHERE cod_doc = '01'`).Scan(&last); err != nil {
		t.Fatalf("read the factura sequence: %v", err)
	}
	return last
}

// houseBacklog designates the test Organization, publishes a House Event
// and returns the closed app to pay through, the operator session, the
// Organization Admin's session and the Ticket Type to buy — the setup every
// backfill test starts from.
func houseBacklog(t *testing.T, env *testEnv) (closed *testEnv, operatorSessionID, adminSessionID, gaID string) {
	t.Helper()
	closed = startAppWithSaleInvoicingClosed(t)
	adminSessionID = orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID = operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID = publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 20)
	return closed, operatorSessionID, adminSessionID, gaID
}

// TestSaleInvoiceBackfillOwesDrainsAndDeliversTheSelectedSales: two sales
// paid while the flag was closed — a month ago, by two buyers — are
// backfilled: both owed, in request order, stamped with the operator and
// the clock's now and visible so on the detail; both gone from the backlog
// and its count; both in the invoicing list. A drain authorizes both with
// issued_on = the clock's today and not the sale's date, and each buyer
// gets one standard delivery mail with the XML and the RIDE attached.
func TestSaleInvoiceBackfillOwesDrainsAndDeliversTheSelectedSales(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	closed, operatorSessionID, _, gaID := houseBacklog(t, env)

	anaRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	beaRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("bea@example.com", "Bea", "Mora", cartLine(gaID, 1)))
	anaID, beaID := saleIDByRef(t, env, anaRef), saleIDByRef(t, env, beaRef)
	// Sold a month ago: the factura must still be dated today.
	if _, err := env.db.Exec(`UPDATE ticket_sales SET sold_at = $1 WHERE id IN ($2, $3)`, env.fixedClock.Add(-30*24*time.Hour), anaID, beaID); err != nil {
		t.Fatalf("move sold_at: %v", err)
	}
	assertUninvoicedRefs(t, operatorSessionID, "before the backfill", anaRef, beaRef)
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("setup: %d invoices before the backfill; want none", list.Pagination.Total)
	}

	result := backfillOK(t, operatorSessionID, []string{beaID, anaID})
	if len(result.Owed) != 2 || len(result.Refused) != 0 {
		t.Fatalf("backfill = %+v; want both owed and none refused", result)
	}
	if result.Owed[0].TicketSaleID != beaID || result.Owed[1].TicketSaleID != anaID {
		t.Fatalf("owed = %+v; want request order: %s then %s", result.Owed, beaID, anaID)
	}
	beaInvoiceID, anaInvoiceID := result.Owed[0].InvoiceID, result.Owed[1].InvoiceID
	if beaInvoiceID == "" || anaInvoiceID == "" || beaInvoiceID == anaInvoiceID {
		t.Fatalf("owed invoice ids = %q, %q; want two distinct documents", beaInvoiceID, anaInvoiceID)
	}

	// Owed, stamped, dated nothing yet, and no longer in the backlog.
	wantAt := env.fixedClock.UTC().Format(time.RFC3339)
	for _, tc := range []struct{ invoiceID, ref, saleID string }{{anaInvoiceID, anaRef, anaID}, {beaInvoiceID, beaRef, beaID}} {
		detail := getBackfilledInvoice(t, operatorSessionID, tc.invoiceID)
		if detail.Kind != "sale" || detail.Status != "owed" || detail.Number != nil || detail.IssuedOn != nil || detail.NextAttemptAt == nil {
			t.Fatalf("%s right after the backfill = %s %s number %v issued_on %v next %v; want an owed, unsigned, due Sale Invoice", tc.ref, detail.Kind, detail.Status, detail.Number, detail.IssuedOn, detail.NextAttemptAt)
		}
		if detail.SaleConfirmationRef == nil || *detail.SaleConfirmationRef != tc.ref {
			t.Fatalf("%s names sale %v; want %s", tc.invoiceID, detail.SaleConfirmationRef, tc.ref)
		}
		if detail.BackfilledBy == nil || *detail.BackfilledBy != "operator@example.com" {
			t.Fatalf("%s backfilled_by = %v; want the operator who acted", tc.ref, detail.BackfilledBy)
		}
		if detail.BackfilledAt == nil || *detail.BackfilledAt != wantAt {
			t.Fatalf("%s backfilled_at = %v; want the clock's now %s", tc.ref, detail.BackfilledAt, wantAt)
		}
		if n := invoiceRowsOfSale(t, env, tc.saleID); n != 1 {
			t.Fatalf("%s has %d invoice rows; want exactly one", tc.ref, n)
		}
	}
	assertUninvoicedRefs(t, operatorSessionID, "after the backfill")
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 2 {
		t.Fatalf("invoicing list holds %d documents; want the two owed", list.Pagination.Total)
	}
	if n := sequenceRows(t, env); n != 0 {
		t.Fatalf("sequence rows = %d after owing; want none — nothing is signed until a drain", n)
	}

	// Drained: signed today, authorized, delivered — like any Sale Invoice.
	issuerReady(t, operatorSessionID)
	drain := drainSaleInvoices(t)
	if drain.Claimed != 2 || drain.Authorized != 2 || drain.Delivered != 2 || drain.NeedsAttention != 0 {
		t.Fatalf("drain = %+v; want both documents authorized and delivered", drain)
	}
	if last := lastFacturaSecuencial(t, env); last != 2 {
		t.Fatalf("the factura sequence stands at %d; want 2", last)
	}
	sent := deliveriesSent(t, env)
	if len(sent) != 2 {
		t.Fatalf("%d delivery mails were sent; want one per buyer", len(sent))
	}
	for _, tc := range []struct{ invoiceID, ref, to string }{{anaInvoiceID, anaRef, "guest@example.com"}, {beaInvoiceID, beaRef, "bea@example.com"}} {
		detail := getBackfilledInvoice(t, operatorSessionID, tc.invoiceID)
		if detail.Status != "authorized" || detail.DeliveredAt == nil || !detail.HasAuthorizationXML {
			t.Fatalf("%s after the drain = %s delivered %v xml %v; want authorized and delivered", tc.ref, detail.Status, detail.DeliveredAt, detail.HasAuthorizationXML)
		}
		if detail.IssuedOn == nil || *detail.IssuedOn != "2026-07-07" {
			t.Fatalf("%s issued_on = %v; want 2026-07-07, the day of the act in Guayaquil and never the sale's date", tc.ref, detail.IssuedOn)
		}
		if detail.IssuedBy == nil || *detail.IssuedBy != "sale-invoice-drainer" || detail.BackfilledBy == nil || *detail.BackfilledBy != "operator@example.com" {
			t.Fatalf("%s issued_by %v backfilled_by %v; want the Drainer and the operator", tc.ref, detail.IssuedBy, detail.BackfilledBy)
		}
		var mails []int
		for i, m := range sent {
			if m.Reference == tc.ref {
				mails = append(mails, i)
			}
		}
		if len(mails) != 1 {
			t.Fatalf("%d delivery mails name %s; want exactly one", len(mails), tc.ref)
		}
		mail := sent[mails[0]]
		if mail.To != tc.to || mail.Kind != "sale" || mail.EventName != "House Fest" {
			t.Fatalf("delivery for %s = to %s kind %s event %s; want %s, a sale document, House Fest", tc.ref, mail.To, mail.Kind, mail.EventName, tc.to)
		}
		if len(mail.Attachments) != 2 {
			t.Fatalf("delivery for %s carries %v; want the XML and the RIDE", tc.ref, attachmentNames(mail))
		}
		accessKey := detail.EcuadorFull.AccessKey
		xml, pdf := mail.Attachments[0], mail.Attachments[1]
		if xml.Filename != accessKey+".xml" || !bytes.Contains(xml.Body, []byte("<factura")) {
			t.Fatalf("delivery for %s first attachment = %s; want the signed factura under <clave>.xml", tc.ref, xml.Filename)
		}
		if pdf.Filename != accessKey+".pdf" || !bytes.HasPrefix(pdf.Body, []byte("%PDF-")) {
			t.Fatalf("delivery for %s second attachment = %s; want the RIDE under <clave>.pdf", tc.ref, pdf.Filename)
		}
		if !bytes.Equal(pdf.Body, operatorRIDE(t, operatorSessionID, tc.invoiceID)) {
			t.Fatalf("delivery for %s attached a RIDE that differs from the operator's download", tc.ref)
		}
		if text := mail.Text(); !strings.Contains(text, "House Fest") || !strings.Contains(text, tc.ref) {
			t.Fatalf("delivery text for %s lacks the Event or the reference:\n%s", tc.ref, text)
		}
	}
	// The last document the SRI received is dated the day of the act.
	if got := xmlText(t, receivedFactura(t), "/factura/infoFactura/fechaEmision"); got != "07/07/2026" {
		t.Fatalf("fechaEmision = %s; want 07/07/2026, the day of the act", got)
	}
	list := getSaleInvoiceList(t, operatorSessionID)
	if list.Pagination.Total != 2 || list.Data[0].Status != "authorized" || list.Data[1].Status != "authorized" {
		t.Fatalf("invoicing list = %+v; want both documents authorized", list.Data)
	}
	assertUninvoicedRefs(t, operatorSessionID, "after the drain")
}

// TestSaleInvoiceBackfillRefusesPerSaleAndProceedsWithTheRest: a selection
// naming one sale twice, one the builder cannot invoice — a line repriced
// so the lines no longer sum to the Payment — an id nobody has and two
// good sales: the good sales are owed, the repeat is refused
// not_a_candidate the second time with exactly one document for it, the
// mismatched sale is refused unsupported_sale with no row written and no
// number ever consumed, and the unknown id is not_a_candidate. Arrays keep
// request order. The refused candidate stays in the backlog; a drain
// numbers only the owed documents.
func TestSaleInvoiceBackfillRefusesPerSaleAndProceedsWithTheRest(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	closed, operatorSessionID, _, gaID := houseBacklog(t, env)

	aRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	brokenRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	bRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	aID, brokenID, bID := saleIDByRef(t, env, aRef), saleIDByRef(t, env, brokenRef), saleIDByRef(t, env, bRef)
	// A sale the platform cannot invoice as it was transacted: its line
	// says one price and its Payment another. Nothing through the API
	// produces this; it is the builder's refusal that is under test.
	if _, err := env.db.Exec(`UPDATE ticket_sale_lines SET unit_price_cents = unit_price_cents + 1 WHERE ticket_sale_id = $1`, brokenID); err != nil {
		t.Fatalf("reprice the line: %v", err)
	}
	unknown := uuid.NewString()
	assertUninvoicedRefs(t, operatorSessionID, "before the backfill", aRef, brokenRef, bRef)

	result := backfillOK(t, operatorSessionID, []string{aID, aID, brokenID, unknown, bID})
	if len(result.Owed) != 2 || result.Owed[0].TicketSaleID != aID || result.Owed[1].TicketSaleID != bID {
		t.Fatalf("owed = %+v; want %s then %s", result.Owed, aID, bID)
	}
	if len(result.Refused) != 3 {
		t.Fatalf("refused = %+v; want three refusals", result.Refused)
	}
	for i, want := range []struct{ id, code string }{{aID, "not_a_candidate"}, {brokenID, "unsupported_sale"}, {unknown, "not_a_candidate"}} {
		if got := result.Refused[i]; got.TicketSaleID != want.id || got.Code != want.code {
			t.Fatalf("refused[%d] = %+v; want %s %s", i, got, want.id, want.code)
		}
	}
	if n := invoiceRowsOfSale(t, env, aID); n != 1 {
		t.Fatalf("the sale named twice has %d invoice rows; want exactly one", n)
	}
	if n := invoiceRowsOfSale(t, env, brokenID); n != 0 {
		t.Fatalf("the unsupported sale has %d invoice rows; want none — the transaction rolled back", n)
	}
	assertUninvoicedRefs(t, operatorSessionID, "after the backfill", brokenRef)
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 2 {
		t.Fatalf("invoicing list holds %d documents; want the two owed", list.Pagination.Total)
	}

	// A second request for the same selection owes nothing more.
	again := backfillOK(t, operatorSessionID, []string{aID, bID})
	if len(again.Owed) != 0 || len(again.Refused) != 2 || again.Refused[0].Code != "not_a_candidate" || again.Refused[1].Code != "not_a_candidate" {
		t.Fatalf("second backfill = %+v; want both refused not_a_candidate", again)
	}

	// Drained: two numbers for two documents, none for the refused sale.
	issuerReady(t, operatorSessionID)
	if drain := drainSaleInvoices(t); drain.Claimed != 2 || drain.Authorized != 2 {
		t.Fatalf("drain = %+v; want the two owed documents authorized", drain)
	}
	if last := lastFacturaSecuencial(t, env); last != 2 {
		t.Fatalf("the factura sequence stands at %d; want 2 — no number for the unsupported sale", last)
	}
	if n := invoiceRowsOfSale(t, env, brokenID); n != 0 {
		t.Fatalf("the unsupported sale has %d invoice rows after the drain; want none", n)
	}
	if n := len(deliveriesSent(t, env)); n != 2 {
		t.Fatalf("%d delivery mails; want two", n)
	}
}

// TestSaleInvoiceBackfillReversalCreditsABackfilledSale: a backfilled and
// authorized Sale Invoice is credited on an Operator Reversal exactly as a
// checkout-born one — a Credit Note for the whole amount, owed in the
// reversal, authorized and delivered on the next drain — and the Credit
// Note itself carries no backfill trail.
func TestSaleInvoiceBackfillReversalCreditsABackfilledSale(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	closed, operatorSessionID, _, gaID := houseBacklog(t, env)
	ref := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	saleID := saleIDByRef(t, env, ref)

	result := backfillOK(t, operatorSessionID, []string{saleID})
	if len(result.Owed) != 1 {
		t.Fatalf("backfill = %+v; want the sale owed", result)
	}
	facturaID := result.Owed[0].InvoiceID
	issuerReady(t, operatorSessionID)
	if drain := drainSaleInvoices(t); drain.Authorized != 1 || drain.Delivered != 1 {
		t.Fatalf("drain = %+v; want the factura authorized and delivered", drain)
	}
	factura := getBackfilledInvoice(t, operatorSessionID, facturaID)
	if factura.Status != "authorized" || factura.CreditedByInvoiceID != nil {
		t.Fatalf("factura before the reversal = %s credited by %v; want authorized and uncredited", factura.Status, factura.CreditedByInvoiceID)
	}

	if rev := operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(1115),
		PlatformFeeKept:     boolPtr(false),
	}); rev.Status != "reversed" {
		t.Fatalf("operator reversal = %+v; want reversed", rev)
	}
	noteID := creditNoteOf(t, operatorSessionID)
	note := getBackfilledInvoice(t, operatorSessionID, noteID)
	if note.Status != "owed" || note.CreditsInvoiceID == nil || *note.CreditsInvoiceID != facturaID || note.CreditNoteReason == nil || *note.CreditNoteReason != "platform" {
		t.Fatalf("credit note = %s credits %v for %v; want owed against %s for platform", note.Status, note.CreditsInvoiceID, note.CreditNoteReason, facturaID)
	}
	if note.Totals.TotalCents != factura.Totals.TotalCents || note.Totals.TotalCents != 1115 {
		t.Fatalf("credit note total = %d; want the factura's whole %d", note.Totals.TotalCents, factura.Totals.TotalCents)
	}
	if note.BackfilledBy != nil || note.BackfilledAt != nil {
		t.Fatalf("credit note carries a backfill trail %v %v; want none — only the factura was backfilled", note.BackfilledBy, note.BackfilledAt)
	}
	factura = getBackfilledInvoice(t, operatorSessionID, facturaID)
	if factura.CreditedByInvoiceID == nil || *factura.CreditedByInvoiceID != noteID || factura.BackfilledBy == nil {
		t.Fatalf("factura after the reversal credited by %v backfilled_by %v; want the Credit Note and the trail kept", factura.CreditedByInvoiceID, factura.BackfilledBy)
	}
	if drain := drainSaleInvoices(t); drain.Claimed != 1 || drain.Authorized != 1 || drain.Delivered != 1 {
		t.Fatalf("drain = %+v; want the Credit Note authorized and delivered", drain)
	}
	note = getBackfilledInvoice(t, operatorSessionID, noteID)
	if note.Status != "authorized" || note.Kind != "credit_note" {
		t.Fatalf("credit note after the drain = %s %s; want an authorized Credit Note", note.Kind, note.Status)
	}
	if doc := receivedNotaCredito(t); xmlText(t, doc, "/notaCredito/infoNotaCredito/numDocModificado") != "001-001-000000001" {
		t.Fatalf("the nota de crédito names %s; want the backfilled factura 001-001-000000001", xmlText(t, doc, "/notaCredito/infoNotaCredito/numDocModificado"))
	}
	assertUninvoicedRefs(t, operatorSessionID, "reversed sale")
}

// TestSaleInvoiceBackfillIsValidatedGatedAndOperatorOnly: an empty or
// missing selection, more than 200 ids and a non-UUID are 400
// VALIDATION_FAILED; unauthenticated is 401 and an Organization Admin 403;
// with the flag closed the act answers 404 SALE_INVOICING_UNAVAILABLE. None
// of it owes anything: the sale stays in the backlog.
func TestSaleInvoiceBackfillIsValidatedGatedAndOperatorOnly(t *testing.T) {
	env := setupTest(t)
	closed, operatorSessionID, adminSessionID, gaID := houseBacklog(t, env)
	ref := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	saleID := saleIDByRef(t, env, ref)

	resp, body := backfillSales(t, sriEnv, operatorSessionID, []string{})
	expectRefusal(t, "empty selection", resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
	resp, body = sriEnv.post(t, backfillPath, map[string]any{}, authHeader(operatorSessionID))
	expectRefusal(t, "missing selection", resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
	tooMany := make([]string, 201)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	resp, body = backfillSales(t, sriEnv, operatorSessionID, tooMany)
	expectRefusal(t, "201 ids", resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
	resp, body = backfillSales(t, sriEnv, operatorSessionID, []string{saleID, "not-a-uuid"})
	expectRefusal(t, "non-UUID", resp, body, http.StatusBadRequest, "VALIDATION_FAILED")

	resp, body = backfillSales(t, sriEnv, "", []string{saleID})
	expectRefusal(t, "unauthenticated", resp, body, http.StatusUnauthorized, "UNAUTHORIZED")
	resp, body = backfillSales(t, sriEnv, adminSessionID, []string{saleID})
	expectRefusal(t, "org_admin", resp, body, http.StatusForbidden, "FORBIDDEN")

	resp, body = backfillSales(t, closed, operatorSessionID, []string{saleID})
	expectRefusal(t, "flag closed", resp, body, http.StatusNotFound, "SALE_INVOICING_UNAVAILABLE")

	assertUninvoicedRefs(t, operatorSessionID, "after every refusal", ref)
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("%d invoices after the refusals; want none", list.Pagination.Total)
	}
}

// TestSaleInvoiceDetailCarriesNoBackfillTrailForACheckoutBornDocument
// (#509): a Sale Invoice owed by the checkout's own transaction answers
// backfilled_by and backfilled_at as null — present and null, never absent
// — so the detail page can tell it from a backfilled one.
func TestSaleInvoiceDetailCarriesNoBackfillTrailForACheckoutBornDocument(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)

	resp, body := sriEnv.get(t, invoicesPath+"/"+invoiceID, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &raw); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	for _, key := range []string{"backfilled_by", "backfilled_at"} {
		v, ok := raw[key]
		if !ok {
			t.Fatalf("detail lacks %s; want it present and null on a checkout-born document", key)
		}
		if string(v) != "null" {
			t.Fatalf("%s = %s; want null on a checkout-born document", key, v)
		}
	}
	detail := getBackfilledInvoice(t, operatorSessionID, invoiceID)
	if detail.BackfilledBy != nil || detail.BackfilledAt != nil {
		t.Fatalf("checkout-born detail = backfilled_by %v backfilled_at %v; want both null", detail.BackfilledBy, detail.BackfilledAt)
	}
}
