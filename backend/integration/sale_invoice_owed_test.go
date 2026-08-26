package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A paid House checkout owes a Sale Invoice (#473, parent #471, ADR 0060):
// the moment a Payment for a House Event is approved and its Ticket Sale
// recorded, a Tax Invoice of kind `sale` exists in state `owed`, naming the
// Sale, copying the buyer as the Sale transacted them, and pricing one line
// per Ticket Sale Line as the buyer paid it. Nothing is signed, no sequence
// number is consumed and the SRI hears nothing: issuing is the Drainer's
// (later). Free, imported and non-House sales owe nothing, and the manual
// form issues exactly as before beside the owed documents.
//
// Everything is asserted through the operator invoicing list and detail, the
// captured Sale Confirmation and the fake SRI's count; the sequence table is
// read directly because no surface states "no number was consumed".

// saleInvoiceListView is the invoices list as widened by #473: the document
// kind and the Sale Confirmation reference beside what the list always
// showed. Number, environment and emission date are pointers because an owed
// document has none yet, and null must be distinguishable from blank.
type saleInvoiceListView struct {
	Data []struct {
		ID                  string  `json:"id"`
		Kind                string  `json:"kind"`
		Status              string  `json:"status"`
		Number              *string `json:"number"`
		Environment         *string `json:"environment"`
		IssuedOn            *string `json:"issued_on"`
		IssuedBy            *string `json:"issued_by"`
		Country             string  `json:"country"`
		TicketSaleID        *string `json:"ticket_sale_id"`
		SaleConfirmationRef *string `json:"sale_confirmation_ref"`
		Recipient           struct {
			TaxIDType string `json:"tax_id_type"`
			TaxID     string `json:"tax_id"`
			LegalName string `json:"legal_name"`
			Address   string `json:"address"`
			Email     string `json:"email"`
		} `json:"recipient"`
		TotalCents int64  `json:"total_cents"`
		Currency   string `json:"currency"`
	} `json:"data"`
	Pagination struct {
		Total int `json:"total"`
	} `json:"pagination"`
}

// saleInvoiceDetailView is one document as widened by #473.
type saleInvoiceDetailView struct {
	ID                  string  `json:"id"`
	Kind                string  `json:"kind"`
	Status              string  `json:"status"`
	Number              *string `json:"number"`
	SaleConfirmationRef *string `json:"sale_confirmation_ref"`
	IVARate             *string `json:"iva_rate"`
	DeliveredAt         *string `json:"delivered_at"`
	NextAttemptAt       *string `json:"next_attempt_at"`
	CreditsInvoiceID    *string `json:"credits_invoice_id"`
	ReversalReason      *string `json:"reversal_reason"`
	PaymentMethod       string  `json:"payment_method"`
	Issuer              *struct {
		RUC string `json:"ruc"`
	} `json:"issuer"`
	Ecuador *struct {
		AccessKey string `json:"access_key"`
	} `json:"ecuador"`
	Lines []struct {
		Position       int    `json:"position"`
		Description    string `json:"description"`
		Quantity       string `json:"quantity"`
		UnitPriceCents int64  `json:"unit_price_cents"`
		DiscountCents  int64  `json:"discount_cents"`
		IVARate        string `json:"iva_rate"`
		RatePercent    int    `json:"rate_percent"`
		BaseCents      int64  `json:"base_cents"`
		IVACents       int64  `json:"iva_cents"`
	} `json:"lines"`
	Totals struct {
		SubtotalCents int64 `json:"subtotal_cents"`
		DiscountCents int64 `json:"discount_cents"`
		IVACents      int64 `json:"iva_cents"`
		TotalCents    int64 `json:"total_cents"`
	} `json:"totals"`
	Attempts            []json.RawMessage `json:"attempts"`
	HasAuthorizationXML bool              `json:"has_authorization_xml"`
}

func getSaleInvoiceList(t *testing.T, sessionID string) saleInvoiceListView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var list saleInvoiceListView
	if err := json.Unmarshal(env.Data, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return list
}

func getSaleInvoiceDetail(t *testing.T, sessionID, id string) saleInvoiceDetailView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view saleInvoiceDetailView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// paidCheckoutApproved drives a paid checkout through the PayPhone stub to an
// approved Payment and returns the Sale Confirmation reference. The body may
// carry a Sale Locale.
func paidCheckoutApproved(t *testing.T, eventSlug string, body map[string]any) string {
	t.Helper()
	begin := beginCheckoutOK(t, payphoneEnv, "test-org", eventSlug, body)
	resp, envBody := confirmCheckoutParams(t, payphoneEnv, begin.ClientTransactionID, payphoneReturnParams(begin.ClientTransactionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	var confirm confirmCheckoutResult
	if err := json.Unmarshal(envBody.Data, &confirm); err != nil {
		t.Fatalf("decode confirm result: %v", err)
	}
	if confirm.Status != "approved" || confirm.ConfirmationRef == "" {
		t.Fatalf("confirm = %+v, want approved with a reference", confirm)
	}
	return confirm.ConfirmationRef
}

// lastConfirmation is the most recent Sale Confirmation the capture sender
// holds.
func lastConfirmation(t *testing.T, env *testEnv) platform.SaleConfirmation {
	t.Helper()
	confs := env.email.Confirmations()
	if len(confs) == 0 {
		t.Fatal("no Sale Confirmation was sent")
	}
	return confs[len(confs)-1]
}

const (
	saleInvoiceFollowsEN = "A tax invoice (factura) for this purchase will follow in a separate email."
	saleInvoiceFollowsES = "La factura de esta compra le llegará en un correo aparte."
)

func assertSaleInvoiceFollows(t *testing.T, c platform.SaleConfirmation, sentence string, where string) {
	t.Helper()
	if !c.SaleInvoiceFollows {
		t.Fatalf("%s: SaleInvoiceFollows is false; want the receipt to promise a factura", where)
	}
	if !strings.Contains(c.Text(), sentence) {
		t.Fatalf("%s: receipt lacks %q:\n%s", where, sentence, c.Text())
	}
}

func assertNoSaleInvoicePromised(t *testing.T, c platform.SaleConfirmation, where string) {
	t.Helper()
	if c.SaleInvoiceFollows {
		t.Fatalf("%s: SaleInvoiceFollows is true; want no factura promised", where)
	}
	text := c.Text()
	if strings.Contains(text, saleInvoiceFollowsEN) || strings.Contains(text, saleInvoiceFollowsES) {
		t.Fatalf("%s: receipt promises a factura:\n%s", where, text)
	}
}

func sequenceRows(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM invoicing_sequences_ec`).Scan(&n); err != nil {
		t.Fatalf("count sequences: %v", err)
	}
	return n
}

// TestPaidHouseCheckoutOwesASaleInvoice: the paid checkout of a House Event
// owes exactly one Sale Invoice — kind `sale`, state `owed`, its Recipient
// the buyer as the Sale transacted them, one line per Ticket Sale Line at the
// price paid with IVA backed out at 15% — with no number, no signature, no
// SRI call and no Issuer at all (none exists in this test). The receipt says
// a factura will follow, in the Sale's own language. Undesignating the
// Organization afterwards withdraws nothing already owed, and the next sale
// owes nothing.
func TestPaidHouseCheckoutOwesASaleInvoice(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	ref := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))

	list := getSaleInvoiceList(t, operatorSessionID)
	if list.Pagination.Total != 1 || len(list.Data) != 1 {
		t.Fatalf("invoices after a paid House checkout = %d rows (total %d); want exactly one", len(list.Data), list.Pagination.Total)
	}
	row := list.Data[0]
	if row.Kind != "sale" || row.Status != "owed" {
		t.Fatalf("row kind/status = %s/%s; want sale/owed", row.Kind, row.Status)
	}
	if row.Number != nil || row.Environment != nil || row.IssuedOn != nil || row.IssuedBy != nil {
		t.Fatalf("owed row carries issue facts: number=%v environment=%v issued_on=%v issued_by=%v; want all null", row.Number, row.Environment, row.IssuedOn, row.IssuedBy)
	}
	if row.SaleConfirmationRef == nil || *row.SaleConfirmationRef != ref {
		t.Fatalf("sale_confirmation_ref = %v; want %q", row.SaleConfirmationRef, ref)
	}
	if row.TicketSaleID == nil || *row.TicketSaleID == "" {
		t.Fatalf("ticket_sale_id = %v; want the Ticket Sale", row.TicketSaleID)
	}
	if row.Country != "ec" || row.Currency != "USD" || row.TotalCents != 2230 {
		t.Fatalf("row = %+v; want ec / USD / 2230 (2 × the 1115 buyer price)", row)
	}
	// The Recipient is the Sale's snapshot: "First Last", the Tax ID the
	// checkout was transacted under, the Sale's email, and no address.
	r := row.Recipient
	if r.LegalName != "Ana Lopez" || r.TaxIDType != "cedula" || r.TaxID != validCedula || r.Email != "guest@example.com" || r.Address != "" {
		t.Fatalf("recipient = %+v; want Ana Lopez / cedula %s / guest@example.com / no address", r, validCedula)
	}

	detail := getSaleInvoiceDetail(t, operatorSessionID, row.ID)
	if detail.Kind != "sale" || detail.Status != "owed" || detail.Number != nil {
		t.Fatalf("detail = kind %s status %s number %v", detail.Kind, detail.Status, detail.Number)
	}
	if detail.SaleConfirmationRef == nil || *detail.SaleConfirmationRef != ref {
		t.Fatalf("detail sale_confirmation_ref = %v; want %q", detail.SaleConfirmationRef, ref)
	}
	if detail.IVARate == nil || *detail.IVARate != "15" {
		t.Fatalf("iva_rate = %v; want 15 recorded on the document", detail.IVARate)
	}
	if detail.Ecuador != nil || detail.Issuer != nil {
		t.Fatalf("owed document has an Ecuador row or an Issuer snapshot: ecuador=%+v issuer=%+v; want neither before signing", detail.Ecuador, detail.Issuer)
	}
	if detail.DeliveredAt != nil || detail.CreditsInvoiceID != nil || detail.ReversalReason != nil {
		t.Fatalf("owed Sale Invoice carries delivery or credit facts: %+v", detail)
	}
	// Due at once: the Drainer's first attempt is the immediate one right
	// after checkout, and next_attempt_at is how it finds the document.
	if detail.NextAttemptAt == nil {
		t.Fatalf("owed Sale Invoice has no next_attempt_at; want it due now")
	} else if at, err := time.Parse(time.RFC3339, *detail.NextAttemptAt); err != nil || !at.Equal(env.fixedClock) {
		t.Fatalf("next_attempt_at = %s (%v); want the server's clock %s", *detail.NextAttemptAt, err, env.fixedClock)
	}
	if detail.PaymentMethod != "19" {
		t.Fatalf("payment_method = %q; want 19 (card), mapped from the Payment Provider", detail.PaymentMethod)
	}
	if len(detail.Attempts) != 0 || detail.HasAuthorizationXML {
		t.Fatalf("owed document has attempts (%d) or an authorization; want none", len(detail.Attempts))
	}
	// One line per Ticket Sale Line, priced as the buyer paid it, naming the
	// Ticket Type and the Event, with the IVA backed out of the price so
	// base + IVA is the cents paid: 2230 → 1939 + 291.
	if len(detail.Lines) != 1 {
		t.Fatalf("lines = %d; want one per Ticket Sale Line", len(detail.Lines))
	}
	line := detail.Lines[0]
	if !strings.Contains(line.Description, "GA") || !strings.Contains(line.Description, "House Fest") {
		t.Fatalf("line description %q; want the Ticket Type and the Event named", line.Description)
	}
	if line.Quantity != "2.00" || line.UnitPriceCents != 1115 || line.DiscountCents != 0 || line.IVARate != "15" || line.RatePercent != 15 {
		t.Fatalf("line = %+v; want 2 × 1115 at 15%%", line)
	}
	if line.BaseCents != 1939 || line.IVACents != 291 {
		t.Fatalf("line arithmetic = base %d + iva %d; want 1939 + 291 = 2230", line.BaseCents, line.IVACents)
	}
	if detail.Totals.SubtotalCents != 1939 || detail.Totals.IVACents != 291 || detail.Totals.TotalCents != 2230 || detail.Totals.DiscountCents != 0 {
		t.Fatalf("totals = %+v; want 1939 / 291 / 2230", detail.Totals)
	}

	// Nothing was sent, nothing was numbered.
	if n := sriStub.receptionCount(); n != 0 {
		t.Fatalf("the SRI received %d documents; want none at owing time", n)
	}
	if n := sequenceRows(t, env); n != 0 {
		t.Fatalf("sequence rows = %d; want no secuencial consumed", n)
	}

	// The operator's actions and downloads have nothing to work on yet.
	resp, body := checkInvoice(t, operatorSessionID, row.ID)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_NOT_ISSUED" {
		t.Fatalf("check status on an owed document: status=%d error=%+v; want 409 INVOICE_NOT_ISSUED", resp.StatusCode, body.Error)
	}
	resp, body = resendInvoice(t, operatorSessionID, row.ID)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "INVOICE_NOT_ISSUED" {
		t.Fatalf("resend an owed document: status=%d error=%+v; want 409 INVOICE_NOT_ISSUED", resp.StatusCode, body.Error)
	}
	rawResp, raw := sriEnv.getRaw(t, signedXMLPath(row.ID), authHeader(operatorSessionID))
	if rawResp.StatusCode != http.StatusNotFound || decodeErrorEnvelope(t, raw).Error.Code != "SIGNED_XML_NOT_FOUND" {
		t.Fatalf("signed XML of an owed document: status=%d body=%s; want 404 SIGNED_XML_NOT_FOUND", rawResp.StatusCode, raw)
	}

	// The receipt promises the factura, in the Sale's language.
	assertSaleInvoiceFollows(t, lastConfirmation(t, env), saleInvoiceFollowsEN, "English receipt")

	esBody := checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1))
	esBody["locale"] = "es"
	paidCheckoutApproved(t, "house-fest", esBody)
	assertSaleInvoiceFollows(t, lastConfirmation(t, env), saleInvoiceFollowsES, "Spanish receipt")
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 2 {
		t.Fatalf("invoices after two paid House checkouts = %d; want 2", list.Pagination.Total)
	}

	// Undesignation withdraws nothing and stops nothing already owed.
	resp, body = env.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear house designation status=%d error=%+v", resp.StatusCode, body.Error)
	}
	list = getSaleInvoiceList(t, operatorSessionID)
	if list.Pagination.Total != 2 {
		t.Fatalf("invoices after undesignation = %d; want the 2 already owed", list.Pagination.Total)
	}
	for _, row := range list.Data {
		if row.Status != "owed" {
			t.Fatalf("row %s after undesignation = %s; want still owed", row.ID, row.Status)
		}
	}
	paidCheckoutApproved(t, "house-fest", checkoutBody("late@example.com", "Luis", "Vega", cartLine(gaID, 1)))
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 2 {
		t.Fatalf("invoices after a sale following undesignation = %d; want still 2", list.Pagination.Total)
	}
	assertNoSaleInvoicePromised(t, lastConfirmation(t, env), "receipt after undesignation")
}

// TestSalesThatOweNoSaleInvoice: a paid sale of an Organization that is not a
// House Organization, a paid sale made BEFORE the designation, a free House
// checkout and a House Sale Import all owe nothing, and none of their
// receipts promises a factura. Designation is never retroactive.
func TestSalesThatOweNoSaleInvoice(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	_, paidID := publishCheckoutEvent(t, env, adminSessionID, "Paid Fest", "paid-fest", 1000, 10)
	freeEventID, freeID := publishCheckoutEvent(t, env, adminSessionID, "Free Fest", "free-fest", 0, 10)

	// Not a House Organization: nothing changes.
	paidCheckoutApproved(t, "paid-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(paidID, 1)))
	assertNoSaleInvoicePromised(t, lastConfirmation(t, env), "non-House paid receipt")
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("invoices after a non-House paid checkout = %d; want 0", list.Pagination.Total)
	}

	// Designated now: the sale above stays uninvoiced.
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("invoices after designation = %d; want 0, designation is never retroactive", list.Pagination.Total)
	}

	// A free House checkout has nothing to invoice.
	result := beginCheckoutSettled(t, env, "test-org", "free-fest", "", checkoutBody("free@example.com", "Bea", "Mora", cartLine(freeID, 2)))
	approvedRef(t, result)
	assertNoSaleInvoicePromised(t, lastConfirmation(t, env), "free House receipt")
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("invoices after a free House checkout = %d; want 0", list.Pagination.Total)
	}

	// An imported House sale's money never touched the platform.
	resp, body := env.post(t, "/api/v1/staff/events/"+freeEventID+"/sale-imports", map[string]any{
		"idempotency_key": "house-batch-1",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "imp@example.com", "customer_first_name": "Ivan", "customer_last_name": "Ruiz", "ticket_type_id": freeID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z", "amount_cents": 500},
		},
	}, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoSaleInvoicePromised(t, lastConfirmation(t, env), "imported House receipt")
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("invoices after a House import = %d; want 0", list.Pagination.Total)
	}

	// And a paid House checkout now owes one — the control that the list
	// above was empty for the right reason.
	paidCheckoutApproved(t, "paid-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(paidID, 1)))
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 1 || list.Data[0].Kind != "sale" {
		t.Fatalf("invoices after a paid House checkout = %+v; want one Sale Invoice", list.Data)
	}
}

// TestManualTaxInvoiceStillIssuesBesideSaleInvoices: the manual form is
// untouched by the widened table — it issues synchronously, is numbered and
// signed as before, shows kind `manual` and no Sale reference, and may leave
// the Recipient's address blank. Both kinds share one list.
func TestManualTaxInvoiceStillIssuesBesideSaleInvoices(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)
	paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))

	issuerReady(t, operatorSessionID)
	body := validInvoiceBody()
	body["recipient"].(map[string]any)["address"] = ""
	manual := issueOK(t, operatorSessionID, body)
	if manual.Status != "authorized" || manual.Number != "001-001-000000001" {
		t.Fatalf("manual invoice = %s %s; want authorized 001-001-000000001", manual.Status, manual.Number)
	}
	if manual.Recipient.Address != "" {
		t.Fatalf("manual recipient address = %q; want blank accepted", manual.Recipient.Address)
	}
	detail := getSaleInvoiceDetail(t, operatorSessionID, manual.ID)
	if detail.Kind != "manual" || detail.SaleConfirmationRef != nil || detail.IVARate != nil || detail.Ecuador == nil || detail.Issuer == nil {
		t.Fatalf("manual detail = kind %s ref %v iva_rate %v ecuador %v issuer %v; want manual with no Sale facts and its numbering", detail.Kind, detail.SaleConfirmationRef, detail.IVARate, detail.Ecuador, detail.Issuer)
	}

	list := getSaleInvoiceList(t, operatorSessionID)
	if list.Pagination.Total != 2 {
		t.Fatalf("invoices = %d; want the manual one beside the owed one", list.Pagination.Total)
	}
	kinds := map[string]string{}
	for _, row := range list.Data {
		kinds[row.Kind] = row.Status
	}
	if kinds["manual"] != "authorized" || kinds["sale"] != "owed" {
		t.Fatalf("list kinds/statuses = %v; want manual authorized and sale owed", kinds)
	}
	// One document went to the SRI: the manual one. The owed Sale Invoice
	// consumed no number either side of it.
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want the manual factura alone", n)
	}
}
