package integration

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// The documents that need an operator (#477, parent #471, ADR 0060): the
// Operator Dashboard's needs_attention queue, counted and listed oldest
// first; Mark annulled on a document the operator annulled by hand at the
// SRI portal; the invoicing list's filter by kind; and the Sale lookup's
// walk from a Sale to its documents. Everything is asserted through the
// operator API — the queue, the detail, the list, the lookup — and the drain
// response where "the Drainer no longer claims it" has no other surface.
//
// Only Sale Invoices reach the queue on this branch: a Credit Note is owed
// by a reversal a later ticket wires, and the queue reads every kind by
// status alone, so the day one is parked it lists here with no change.

const needsAttentionPath = "/api/v1/operator/invoicing/needs-attention"

// needsAttentionQueueView is the queue as the API returns it: the list row
// plus when the document was parked and what the SRI last said about it.
type needsAttentionQueueView struct {
	Data []struct {
		ID                  string  `json:"id"`
		Kind                string  `json:"kind"`
		Status              string  `json:"status"`
		Number              *string `json:"number"`
		SaleConfirmationRef *string `json:"sale_confirmation_ref"`
		AttentionSince      *string `json:"attention_since"`
		// AbandonedAt is the other instant a row can be waiting since
		// (#581): null on a parked document, and on an abandoned one the
		// moment it entered the queue by the second door.
		AbandonedAt *string `json:"abandoned_at"`
		Messages    []struct {
			Identifier string `json:"identifier"`
			Message    string `json:"message"`
			Type       string `json:"type"`
		} `json:"messages"`
	} `json:"data"`
	Pagination struct {
		Total int `json:"total"`
	} `json:"pagination"`
}

type needsAttentionCountView struct {
	NeedsAttentionCount int `json:"needs_attention_count"`
}

// annulledInvoiceView is the detail as Mark annulled's tests read it.
type annulledInvoiceView struct {
	drainedInvoiceView
	AnnulledBy *string `json:"annulled_by"`
	AnnulledAt *string `json:"annulled_at"`
}

func getNeedsAttentionQueue(t *testing.T, sessionID string) needsAttentionQueueView {
	t.Helper()
	resp, env := sriEnv.get(t, needsAttentionPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("queue status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view needsAttentionQueueView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	return view
}

func getNeedsAttentionCount(t *testing.T, sessionID string) int {
	t.Helper()
	resp, env := sriEnv.get(t, needsAttentionPath+"/count", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view needsAttentionCountView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	return view.NeedsAttentionCount
}

func annulInvoice(t *testing.T, sessionID, id string) (*http.Response, envelope) {
	t.Helper()
	return sriEnv.post(t, invoicesPath+"/"+id+"/annul", nil, authHeader(sessionID))
}

func annulOK(t *testing.T, sessionID, id string) annulledInvoiceView {
	t.Helper()
	resp, env := annulInvoice(t, sessionID, id)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("annul status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view annulledInvoiceView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode annulled invoice: %v", err)
	}
	return view
}

func expectRefusal(t *testing.T, what string, resp *http.Response, env envelope, status int, code string) {
	t.Helper()
	if resp.StatusCode != status || env.Error == nil || env.Error.Code != code {
		t.Fatalf("%s: status=%d error=%+v; want %d %s", what, resp.StatusCode, env.Error, status, code)
	}
}

// houseSalesOwed designates the test Organization, publishes a House Event
// and makes `count` paid checkouts, returning the operator session and the
// owed Sale Invoices' ids in checkout order.
func houseSalesOwed(t *testing.T, env *testEnv, count int) (operatorSessionID string, invoiceIDs []string) {
	t.Helper()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID = operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)
	seen := map[string]bool{}
	for i := 0; i < count; i++ {
		paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
		// The list is newest first; the one not seen yet is the checkout
		// that just committed.
		for _, row := range getSaleInvoiceList(t, operatorSessionID).Data {
			if !seen[row.ID] {
				seen[row.ID] = true
				invoiceIDs = append(invoiceIDs, row.ID)
			}
		}
	}
	if len(invoiceIDs) != count {
		t.Fatalf("setup: %d owed Sale Invoices; want %d", len(invoiceIDs), count)
	}
	return operatorSessionID, invoiceIDs
}

// TestNeedsAttentionQueueIsEmptyUntilSomethingIsParked: the ordinary answer
// is zero and an empty list — never an error and never null — and a
// document that is owed, pending or authorized is not in it.
func TestNeedsAttentionQueueIsEmptyUntilSomethingIsParked(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)

	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count with an owed document = %d; want 0", n)
	}
	queue := getNeedsAttentionQueue(t, operatorSessionID)
	if queue.Pagination.Total != 0 || queue.Data == nil || len(queue.Data) != 0 {
		t.Fatalf("queue with an owed document = %+v; want an empty list", queue)
	}

	issuerReady(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want authorized", result)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count with an authorized document = %d; want 0", n)
	}
	if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.Status != "authorized" {
		t.Fatalf("detail = %s", detail.Status)
	}
}

// TestNeedsAttentionQueueListsOldestFirstWithTheSRIsMessages: two Sale
// Invoices the SRI refuses, parked an hour apart, are counted and listed
// with the earlier one first — kind, Sale Confirmation reference, state,
// since when, and the SRI's messages verbatim — and each row opens the
// document detail. A pending document beside them is not listed.
func TestNeedsAttentionQueueListsOldestFirstWithTheSRIsMessages(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceIDs := houseSalesOwed(t, env, 3)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})

	// The Drainer works one document per round when narrowed to a batch of
	// one, so each is parked at a clock of its own.
	sriApp.InvoicingService.WithSaleInvoiceDrainBatch(1)
	t.Cleanup(func() { sriApp.InvoicingService.WithSaleInvoiceDrainBatch(0) })
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.NeedsAttention != 1 {
		t.Fatalf("first drain = %+v; want one document parked", result)
	}
	// The three owed documents fell due together, so which one the first
	// round took is the queue's to say.
	parkedFirst := getNeedsAttentionQueue(t, operatorSessionID)
	if len(parkedFirst.Data) != 1 {
		t.Fatalf("queue after the first drain = %+v; want one document", parkedFirst.Data)
	}
	firstParkedID := parkedFirst.Data[0].ID
	atInvoicingClock(t, fixedClock.Add(time.Hour))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.NeedsAttention != 1 {
		t.Fatalf("second drain = %+v; want one document parked", result)
	}
	// The third stays pending: the SRI holds it and has not decided.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("third drain = %+v; want one document pending", result)
	}

	if n := getNeedsAttentionCount(t, operatorSessionID); n != 2 {
		t.Fatalf("count = %d; want 2", n)
	}
	queue := getNeedsAttentionQueue(t, operatorSessionID)
	if queue.Pagination.Total != 2 || len(queue.Data) != 2 {
		t.Fatalf("queue = %+v; want the two parked documents", queue)
	}
	first, second := queue.Data[0], queue.Data[1]
	if first.ID != firstParkedID || second.ID == firstParkedID {
		t.Fatalf("queue order = [%s, %s]; want the first parked (%s) first", first.ID, second.ID, firstParkedID)
	}
	for _, id := range invoiceIDs {
		if id != first.ID && id != second.ID {
			if detail := getDrainedInvoice(t, operatorSessionID, id); detail.Status != "pending" {
				t.Fatalf("the third document = %s; want pending and off the queue", detail.Status)
			}
		}
	}
	for _, row := range queue.Data {
		if row.Kind != "sale" || row.Status != "needs_attention" || row.SaleConfirmationRef == nil || row.Number == nil {
			t.Fatalf("row = %+v; want a signed Sale Invoice needing attention with its reference", row)
		}
		if len(row.Messages) != 1 || row.Messages[0].Identifier != "65" || row.Messages[0].Message != "FECHA EMISION EXTEMPORANEA" {
			t.Fatalf("messages = %+v; want the SRI's 65 verbatim", row.Messages)
		}
	}
	if first.AttentionSince == nil || *first.AttentionSince != fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("first attention_since = %v; want the moment it was parked (%s)", first.AttentionSince, fixedClock.UTC().Format(time.RFC3339))
	}
	if second.AttentionSince == nil || *second.AttentionSince != fixedClock.Add(time.Hour).UTC().Format(time.RFC3339) {
		t.Fatalf("second attention_since = %v; want an hour on", second.AttentionSince)
	}

	// Each row opens the document.
	for _, row := range queue.Data {
		if detail := getDrainedInvoice(t, operatorSessionID, row.ID); detail.ID != row.ID || detail.Status != "needs_attention" {
			t.Fatalf("detail for %s = %+v", row.ID, detail)
		}
	}

	// Healed by the operator, the document leaves the queue.
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	resp, body := checkInvoice(t, operatorSessionID, firstParkedID)
	if view := actionOK(t, "check", resp, body); view.Status != "authorized" {
		t.Fatalf("check = %s; want authorized", view.Status)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count after healing one = %d; want 1", n)
	}
	if queue := getNeedsAttentionQueue(t, operatorSessionID); len(queue.Data) != 1 || queue.Data[0].ID != second.ID {
		t.Fatalf("queue after healing one = %+v; want the other document alone", queue.Data)
	}
}

// TestNeedsAttentionQueueListsAnUnsignableDocument: a document parked
// because it could not be signed — no Issuer — is in the queue too, with
// the platform's own message and no number, since it is exactly the kind of
// stuck document the queue exists for.
func TestNeedsAttentionQueueListsAnUnsignableDocument(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)

	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want parked", result)
	}
	queue := getNeedsAttentionQueue(t, operatorSessionID)
	if len(queue.Data) != 1 || queue.Data[0].ID != invoiceID || queue.Data[0].Number != nil {
		t.Fatalf("queue = %+v; want the unsigned document", queue.Data)
	}
	if msgs := queue.Data[0].Messages; len(msgs) != 1 || msgs[0].Identifier != "ISSUER_NOT_FOUND" || msgs[0].Type != "PLATFORM" {
		t.Fatalf("messages = %+v; want the platform's ISSUER_NOT_FOUND", msgs)
	}
	if queue.Data[0].AttentionSince == nil {
		t.Fatal("attention_since is null on a parked document")
	}
}

// TestOperatorMarksARefusedDocumentAnnulled: the operator annulled the
// document by hand at the SRI portal and records so. The document is
// annulled with who and when, keeps its number and its signed XML, leaves
// the queue, and is never claimed by the Drainer again; nothing about it
// can be checked, resent or annulled a second time.
func TestOperatorMarksARefusedDocumentAnnulled(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want parked", result)
	}
	before := getDrainedInvoice(t, operatorSessionID, invoiceID)
	xmlBefore := storedSignedXML(t, operatorSessionID, invoiceID)

	atInvoicingClock(t, fixedClock.Add(3*time.Hour))
	annulled := annulOK(t, operatorSessionID, invoiceID)
	if annulled.Status != "annulled" {
		t.Fatalf("status = %s; want annulled", annulled.Status)
	}
	if annulled.AnnulledBy == nil || *annulled.AnnulledBy != "operator@example.com" {
		t.Fatalf("annulled_by = %v; want the operator's email from the session", annulled.AnnulledBy)
	}
	if annulled.AnnulledAt == nil || *annulled.AnnulledAt != fixedClock.Add(3*time.Hour).UTC().Format(time.RFC3339) {
		t.Fatalf("annulled_at = %v; want the moment it was marked", annulled.AnnulledAt)
	}
	if annulled.NextAttemptAt != nil {
		t.Fatalf("next_attempt_at = %v; want nothing due", *annulled.NextAttemptAt)
	}
	if annulled.Number == nil || *annulled.Number != *before.Number || annulled.EcuadorFull == nil || annulled.EcuadorFull.AccessKey != before.EcuadorFull.AccessKey {
		t.Fatalf("annulled document = number %v clave %+v; want its number and clave kept", annulled.Number, annulled.EcuadorFull)
	}
	if len(annulled.Messages) != 1 || annulled.Messages[0].Identifier != "65" {
		t.Fatalf("messages = %+v; want the SRI's last messages kept", annulled.Messages)
	}
	if got := storedSignedXML(t, operatorSessionID, invoiceID); string(got) != string(xmlBefore) {
		t.Fatal("the signed XML on file changed on annulment")
	}

	// Gone from the queue and from the Drainer's claim set.
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count after annulment = %d; want 0", n)
	}
	if queue := getNeedsAttentionQueue(t, operatorSessionID); len(queue.Data) != 0 {
		t.Fatalf("queue after annulment = %+v; want empty", queue.Data)
	}
	atInvoicingClock(t, fixedClock.Add(48*time.Hour))
	if result := drainSaleInvoices(t); result.Claimed != 0 || result.Standing["annulled"] != 1 {
		t.Fatalf("drain after annulment = %+v; want nothing claimed and the document standing annulled", result)
	}
	list := getSaleInvoiceList(t, operatorSessionID)
	if len(list.Data) != 1 || list.Data[0].Status != "annulled" {
		t.Fatalf("list = %+v; want the annulled document still listed", list.Data)
	}

	// Irreversible: nothing is asked or sent for it again, and it cannot be
	// annulled twice.
	resp, body := annulInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "annul twice", resp, body, http.StatusConflict, "INVOICE_NOT_ANNULLABLE")
	resp, body = checkInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "check an annulled document", resp, body, http.StatusConflict, "INVOICE_ANNULLED")
	resp, body = resendInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "resend an annulled document", resp, body, http.StatusConflict, "INVOICE_ANNULLED")
	if n := sriStub.receptionCount(); n != 1 {
		t.Fatalf("the SRI received %d documents; want the one submission", n)
	}
	if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.Status != "annulled" {
		t.Fatalf("after the refusals: %s; want still annulled", detail.Status)
	}
}

// TestOperatorMarksAPendingDocumentAnnulled: a document the SRI holds and
// has not decided may be annulled too — the operator may have annulled it
// at the portal before the platform heard — and the Drainer stops polling it.
func TestOperatorMarksAPendingDocumentAnnulled(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("drain = %+v; want pending", result)
	}

	annulled := annulOK(t, operatorSessionID, invoiceID)
	if annulled.Status != "annulled" || annulled.NextAttemptAt != nil {
		t.Fatalf("annulled = %s next %v; want annulled with nothing due", annulled.Status, annulled.NextAttemptAt)
	}
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("drain after annulment = %+v; want nothing claimed", result)
	}
}

// TestMarkAnnulledIsRefusedWhereNothingWasAnnulled: an authorized document is
// a legal artifact the Credit Note handles; an owed one, and a document
// parked unsigned, have no number at the SRI to have been annulled; a
// document that does not exist is 404.
func TestMarkAnnulledIsRefusedWhereNothingWasAnnulled(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)

	resp, body := annulInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "annul an owed document", resp, body, http.StatusConflict, "INVOICE_NOT_ISSUED")

	// Parked unsignable: needs_attention, but never at the SRI.
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want parked unsignable", result)
	}
	resp, body = annulInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "annul an unsigned document", resp, body, http.StatusConflict, "INVOICE_NOT_ISSUED")

	issuerReady(t, operatorSessionID)
	atInvoicingClock(t, fixedClock.Add(time.Hour))
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain once fixed = %+v; want authorized", result)
	}
	resp, body = annulInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "annul an authorized document", resp, body, http.StatusConflict, "INVOICE_NOT_ANNULLABLE")
	if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.Status != "authorized" {
		t.Fatalf("after the refusal: %s; want still authorized", detail.Status)
	}

	resp, body = annulInvoice(t, operatorSessionID, "00000000-0000-0000-0000-000000000000")
	expectRefusal(t, "annul an unknown document", resp, body, http.StatusNotFound, "INVOICE_NOT_FOUND")
}

// TestDocumentAttentionSurfaceIsOperatorsOnly: the queue, its count and Mark
// annulled refuse a missing session with 401 and an Org Admin with 403 —
// org roles grant nothing platform-wide — and the refused annulment changed
// nothing.
func TestDocumentAttentionSurfaceIsOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	// The Org Admin of the very House Organization whose sale this is: a
	// second session for the address houseSaleOwed created the Organization
	// under.
	adminSessionID := verifyOTP(t, env, "admin@example.com")
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want parked", result)
	}

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, needsAttentionPath},
		{http.MethodGet, needsAttentionPath + "/count"},
		{http.MethodPost, invoicesPath + "/" + invoiceID + "/annul"},
	} {
		resp, body := sriEnv.doJSON(t, route.method, route.path, nil, nil)
		expectRefusal(t, route.method+" "+route.path+" unauthenticated", resp, body, http.StatusUnauthorized, "UNAUTHORIZED")
		resp, body = sriEnv.doJSON(t, route.method, route.path, nil, authHeader(adminSessionID))
		expectRefusal(t, route.method+" "+route.path+" as org_admin", resp, body, http.StatusForbidden, "FORBIDDEN")
	}
	if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.Status != "needs_attention" {
		t.Fatalf("after the refused annulments: %s; want still needs_attention", detail.Status)
	}
}

// TestInvoiceListFiltersByKind: a manual Tax Invoice beside an owed Sale
// Invoice; `kind` narrows the list to one or the other, an absent kind lists
// both, and a kind that is not one of the three is refused by name.
func TestInvoiceListFiltersByKind(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, saleInvoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	manual := issueOK(t, operatorSessionID, validInvoiceBody())

	listKind := func(kind string) saleInvoiceListView {
		t.Helper()
		resp, env := sriEnv.get(t, invoicesPath+"?kind="+kind, authHeader(operatorSessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list kind=%s status=%d error=%+v", kind, resp.StatusCode, env.Error)
		}
		var list saleInvoiceListView
		if err := json.Unmarshal(env.Data, &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return list
	}

	if all := getSaleInvoiceList(t, operatorSessionID); all.Pagination.Total != 2 || len(all.Data) != 2 {
		t.Fatalf("unfiltered list = %+v; want both documents", all.Data)
	}
	sales := listKind("sale")
	if sales.Pagination.Total != 1 || len(sales.Data) != 1 || sales.Data[0].ID != saleInvoiceID || sales.Data[0].Kind != "sale" {
		t.Fatalf("kind=sale = %+v; want the Sale Invoice alone", sales.Data)
	}
	manuals := listKind("manual")
	if manuals.Pagination.Total != 1 || len(manuals.Data) != 1 || manuals.Data[0].ID != manual.ID || manuals.Data[0].Kind != "manual" {
		t.Fatalf("kind=manual = %+v; want the manual Tax Invoice alone", manuals.Data)
	}
	if credits := listKind("credit_note"); credits.Pagination.Total != 0 || len(credits.Data) != 0 {
		t.Fatalf("kind=credit_note = %+v; want none", credits.Data)
	}

	resp, body := sriEnv.get(t, invoicesPath+"?kind=receipt", authHeader(operatorSessionID))
	expectRefusal(t, "an unknown kind", resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
	assertFieldError(t, body.Error.Details, "kind")
}

// TestSaleLookupShowsTheSalesDocuments: the operator walks from a buyer's
// reference to the Sale's documents — each with its kind, state and id, which
// is the link — and a Sale of an ordinary Organization shows none.
func TestSaleLookupShowsTheSalesDocuments(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	list := getSaleInvoiceList(t, operatorSessionID)
	ref := *list.Data[0].SaleConfirmationRef

	type document struct {
		ID         string  `json:"id"`
		Kind       string  `json:"kind"`
		Status     string  `json:"status"`
		Number     *string `json:"number"`
		TotalCents int64   `json:"total_cents"`
	}
	var found struct {
		Sale      operatorSale `json:"sale"`
		Documents []document   `json:"documents"`
	}
	operatorGetOK(t, env, operatorSessionID, operatorSaleLookupPath(ref), &found)
	if found.Sale.ConfirmationRef != ref {
		t.Fatalf("lookup = %+v", found.Sale)
	}
	if len(found.Documents) != 1 || found.Documents[0].ID != invoiceID || found.Documents[0].Kind != "sale" || found.Documents[0].Status != "owed" || found.Documents[0].Number != nil {
		t.Fatalf("documents = %+v; want the owed Sale Invoice", found.Documents)
	}
	if found.Documents[0].TotalCents != int64(found.Sale.AmountCents) {
		t.Fatalf("document total %d; want the Sale's %d", found.Documents[0].TotalCents, found.Sale.AmountCents)
	}

	// Issued, the same walk shows its state and number.
	issuerReady(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v", result)
	}
	operatorGetOK(t, env, operatorSessionID, operatorSaleLookupPath(ref), &found)
	if len(found.Documents) != 1 || found.Documents[0].Status != "authorized" || found.Documents[0].Number == nil {
		t.Fatalf("documents after the drain = %+v; want authorized with a number", found.Documents)
	}

	// An ordinary Organization's sale has no documents, and says so with an
	// empty list rather than null.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	_, ga := publishCheckoutEvent(t, env, otherSessionID, "Plain Fest", "plain-fest", feeTestBaseCents, 10)
	begun := beginCheckoutOK(t, env, "other-org", "plain-fest", checkoutBody("bea@example.com", "Bea", "Ruiz", cartLine(ga, 1)))
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	var plain struct {
		Documents []document `json:"documents"`
	}
	operatorGetOK(t, env, operatorSessionID, operatorSaleLookupPath(settled.ConfirmationRef), &plain)
	if plain.Documents == nil || len(plain.Documents) != 0 {
		t.Fatalf("documents on an ordinary sale = %+v; want an empty list", plain.Documents)
	}
}

// TestMarkAnnulledWinsAgainstADrainersLateAuthorization: the document is
// pending and held by the SRI; a round is asking autorización — and the
// SRI is about to say AUTORIZADO — when the operator marks it annulled.
// The round's answer finds the row no longer pending and leaves it as the
// operator recorded it: annulled, with the trail, off the queue, nothing
// mailed. The SRI's late answer is in the attempts ledger and nowhere
// else. Before this held, the answer overwrote the annulment.
func TestMarkAnnulledWinsAgainstADrainersLateAuthorization(t *testing.T) {
	env := setupTest(t)
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("setup drain = %+v; want pending", result)
	}
	attempts := len(getDrainedInvoice(t, operatorSessionID, invoiceID).AttemptRows)

	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	arrived, release := sriStub.holdAuthorization()
	atInvoicingClock(t, fixedClock.Add(time.Minute))

	var wg sync.WaitGroup
	var result saleInvoiceDrainResult
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = drainSaleInvoices(t)
	}()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the Drainer never asked the SRI")
	}
	view := annulOK(t, operatorSessionID, invoiceID)
	if view.Status != "annulled" || view.AnnulledBy == nil || *view.AnnulledBy != "operator@example.com" {
		t.Fatalf("annul under the Drainer's claim = %s by %v; want annulled by the operator", view.Status, view.AnnulledBy)
	}
	release()
	wg.Wait()

	if result.Claimed != 1 || result.Authorized != 0 || result.Delivered != 0 {
		t.Fatalf("drain that lost the row to the annulment = %+v; want it claimed and neither authorized nor delivered", result)
	}
	after := getDrainedInvoice(t, operatorSessionID, invoiceID)
	if after.Status != "annulled" || after.NextAttemptAt != nil || after.DeliveredAt != nil || after.HasAuthorizationXML {
		t.Fatalf("after the round: status %s next %v delivered %v xml %v; want annulled, nothing due, nothing delivered, no authorization on file", after.Status, after.NextAttemptAt, after.DeliveredAt, after.HasAuthorizationXML)
	}
	if len(after.AttemptRows) != attempts+1 || after.AttemptRows[len(after.AttemptRows)-1].Outcome != "authorized" {
		t.Fatalf("attempts = %+v; want the late AUTORIZADO in the ledger and nowhere else", after.AttemptRows)
	}
	if n := len(deliveriesSent(t, env)); n != 0 {
		t.Fatalf("%d delivery mails; want none for an annulled document", n)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("needs_attention count = %d; want 0", n)
	}
	atInvoicingClock(t, fixedClock.Add(2*time.Hour))
	if again := drainSaleInvoices(t); again.Claimed != 0 {
		t.Fatalf("drain afterwards = %+v; want nothing claimed", again)
	}
}
