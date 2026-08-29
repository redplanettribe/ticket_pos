package integration

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"testing"
	"time"
)

// The Uninvoiced House Sales (#507, parent #506, ADR 0064): a Platform
// Operator's platform-wide list of every paid Online Sale that stands, owes
// no Sale Invoice of any status, and belongs to an Organization that is
// House NOW — the backlog a Sale Invoice Backfill works from. Oldest sale
// first, pages of 50 with a total, and a count that agrees with the list.
// Read-only here: the act is #508.
//
// The two ways a sale ends up here are the two the ADR names: paid while
// SALE_INVOICING_ENABLED was closed (the closed app of
// sale_invoicing_flag_test.go), and paid before its Organization was
// designated House (the shared, open app against an Organization designated
// afterwards). Both are driven through the public checkout; only sold_at is
// moved by SQL, because the fixed clock stamps every sale the same instant
// and ordering needs three that differ.

const uninvoicedSalesPath = "/api/v1/operator/invoicing/uninvoiced-sales"

type uninvoicedHouseSaleView struct {
	TicketSaleID     string  `json:"ticket_sale_id"`
	ConfirmationRef  string  `json:"confirmation_ref"`
	SoldAt           string  `json:"sold_at"`
	OrganizationID   string  `json:"organization_id"`
	OrganizationName string  `json:"organization_name"`
	EventID          string  `json:"event_id"`
	EventName        string  `json:"event_name"`
	BuyerName        string  `json:"buyer_name"`
	BuyerTaxIDType   *string `json:"buyer_tax_id_type"`
	BuyerTaxIDNumber *string `json:"buyer_tax_id_number"`
	TotalCents       int64   `json:"total_cents"`
	Currency         string  `json:"currency"`
}

type uninvoicedHouseSaleListView struct {
	Data       []uninvoicedHouseSaleView `json:"data"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
}

func getUninvoicedHouseSales(t *testing.T, env *testEnv, sessionID, query string) uninvoicedHouseSaleListView {
	t.Helper()
	resp, body := env.get(t, uninvoicedSalesPath+query, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uninvoiced sales status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil || body.RequestID == "" {
		t.Fatalf("uninvoiced sales envelope = error %+v request_id %q; want null error and a request id", body.Error, body.RequestID)
	}
	var list uninvoicedHouseSaleListView
	if err := json.Unmarshal(body.Data, &list); err != nil {
		t.Fatalf("decode uninvoiced sales: %v", err)
	}
	return list
}

func getUninvoicedHouseSaleCount(t *testing.T, env *testEnv, sessionID string) int {
	t.Helper()
	resp, body := env.get(t, uninvoicedSalesPath+"/count", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uninvoiced count status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out struct {
		Count int `json:"uninvoiced_house_sale_count"`
	}
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode uninvoiced count: %v", err)
	}
	return out.Count
}

// assertUninvoicedRefs asserts the list holds exactly these Sale Confirmation
// references, in any order — the fixed clock stamps every sale the same
// sold_at, so order among them is the id's and not the test's — and that
// the count agrees. The ordering test moves sold_at and asserts order.
func assertUninvoicedRefs(t *testing.T, operatorSessionID, where string, refs ...string) {
	t.Helper()
	list := getUninvoicedHouseSales(t, sriEnv, operatorSessionID, "")
	got := make([]string, 0, len(list.Data))
	for _, row := range list.Data {
		got = append(got, row.ConfirmationRef)
	}
	want := append([]string(nil), refs...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) || list.Pagination.Total != len(want) || !slices.Equal(got, want) {
		t.Fatalf("%s: uninvoiced sales = %v (total %d); want %v", where, got, list.Pagination.Total, want)
	}
	if n := getUninvoicedHouseSaleCount(t, sriEnv, operatorSessionID); n != len(refs) {
		t.Fatalf("%s: count = %d; want %d, the same number the list shows", where, n, len(refs))
	}
}

// paidCheckoutWhileClosed makes one paid checkout through the app whose
// SALE_INVOICING_ENABLED is closed: approved, recorded, and no Sale Invoice
// owed — a sale from before the platform invoiced House sales.
func paidCheckoutWhileClosed(t *testing.T, closed *testEnv, eventSlug string, body map[string]any) string {
	t.Helper()
	begin := beginCheckoutOK(t, closed, "test-org", eventSlug, body)
	resp, envBody := confirmCheckoutParams(t, closed, begin.ClientTransactionID, payphoneReturnParams(begin.ClientTransactionID))
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

// TestUninvoicedHouseSalesListsSalesPaidBeforeInvoicingReachedThem: a sale
// paid while the flag was closed is listed once it opens, with its own sale
// date and every column the page shows; a sale paid before its Organization
// was designated House is listed once designated, and undesignating removes
// it. The count agrees at every step.
func TestUninvoicedHouseSalesListsSalesPaidBeforeInvoicingReachedThem(t *testing.T) {
	env := setupTest(t)
	closed := startAppWithSaleInvoicingClosed(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	eventID, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	// Paid while closed: nothing owed, and the backlog shows it.
	ref := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("setup: %d invoices after a checkout with the flag closed; want none", list.Pagination.Total)
	}
	list := getUninvoicedHouseSales(t, sriEnv, operatorSessionID, "")
	if len(list.Data) != 1 || list.Pagination.Total != 1 {
		t.Fatalf("uninvoiced sales = %+v (total %d); want the one sale", list.Data, list.Pagination.Total)
	}
	if p := list.Pagination; p.Page != 1 || p.PageSize != 50 || p.TotalPages != 1 {
		t.Fatalf("pagination = %+v; want page 1 of 1 at the default size of 50", p)
	}
	row := list.Data[0]
	if row.ConfirmationRef != ref || row.TicketSaleID != saleIDByRef(t, env, ref) {
		t.Fatalf("row = %+v; want the sale %s", row, ref)
	}
	if row.SoldAt != env.fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("sold_at = %q; want the sale's own instant %s", row.SoldAt, env.fixedClock.UTC().Format(time.RFC3339))
	}
	if row.OrganizationID != orgID || row.OrganizationName != "Test Org" || row.EventID != eventID || row.EventName != "House Fest" {
		t.Fatalf("row names = %+v; want Test Org / House Fest", row)
	}
	if row.BuyerName != "Ana Lopez" {
		t.Fatalf("buyer_name = %q; want Ana Lopez", row.BuyerName)
	}
	if row.BuyerTaxIDType == nil || *row.BuyerTaxIDType != "cedula" || row.BuyerTaxIDNumber == nil || *row.BuyerTaxIDNumber != validCedula {
		t.Fatalf("buyer tax id = %v %v; want cedula %s as the checkout was transacted", row.BuyerTaxIDType, row.BuyerTaxIDNumber, validCedula)
	}
	// 2 × the 1115 buyer price: what the Payment was approved for.
	if row.TotalCents != 2230 || row.Currency != "USD" {
		t.Fatalf("total = %d %s; want 2230 USD", row.TotalCents, row.Currency)
	}
	if n := getUninvoicedHouseSaleCount(t, sriEnv, operatorSessionID); n != 1 {
		t.Fatalf("count = %d; want 1", n)
	}

	// Paid before the designation: undesignate, sell on the open app (a
	// non-House sale owes nothing), designate again — the sale is a
	// candidate now, its date untouched.
	resp, body := env.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear house designation status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertUninvoicedRefs(t, operatorSessionID, "after undesignation")
	earlier := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertUninvoicedRefs(t, operatorSessionID, "non-House sale")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	assertUninvoicedRefs(t, operatorSessionID, "designated after both sales", ref, earlier)
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("%d invoices after designation; want none — the list is a backlog, not an act", list.Pagination.Total)
	}

	// Undesignating removes both: the Organization is not House now.
	resp, body = env.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear house designation status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertUninvoicedRefs(t, operatorSessionID, "undesignated again")
}

// TestUninvoicedHouseSalesExcludeSalesThatOweNothingOrAlreadyHaveADocument:
// a reversed sale, one with a Reversal Request in flight or parked, a free
// sale, an imported and a Manually Recorded sale, a non-House sale and a
// sale with a Sale Invoice row in any status — owed, annulled, withdrawn —
// are not candidates. One paid-while-closed sale stays listed throughout as
// the control.
func TestUninvoicedHouseSalesExcludeSalesThatOweNothingOrAlreadyHaveADocument(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	closed := startAppWithSaleInvoicingClosed(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 20)
	freeEventID, freeID := publishCheckoutEvent(t, env, adminSessionID, "Free Fest", "free-fest", 0, 20)

	control := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertUninvoicedRefs(t, operatorSessionID, "control", control)

	// Reversed by the operator: the sale no longer stands.
	reversedRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertUninvoicedRefs(t, operatorSessionID, "before the reversal", control, reversedRef)
	if result := operatorReverseOK(t, payphoneEnv, operatorSessionID, reversedRef, operatorReversalBody{
		RefundedAmountCents: intPtr(1115),
		PlatformFeeKept:     boolPtr(false),
	}); result.Status != "reversed" {
		t.Fatalf("operator reversal = %+v; want reversed", result)
	}
	assertUninvoicedRefs(t, operatorSessionID, "reversed sale", control)

	// A Reversal Request in flight, and one parked needs_attention. Staged
	// by SQL: reaching either state through the API takes a provider that
	// hangs or a day of failed commits (customer_sale_reversal_pending_test),
	// and what matters here is the row's status, not how it got there.
	inFlightRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	parkedRef := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertUninvoicedRefs(t, operatorSessionID, "before the requests", control, inFlightRef, parkedRef)
	for ref, status := range map[string]string{inFlightRef: "in_flight", parkedRef: "needs_attention"} {
		if _, err := env.db.Exec(`
			INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
			VALUES ($1, $2, NOW(), $3, NOW())
		`, saleIDByRef(t, env, ref), "tx-"+ref, status); err != nil {
			t.Fatalf("stage a %s Reversal Request: %v", status, err)
		}
	}
	assertUninvoicedRefs(t, operatorSessionID, "reversal requests open", control)
	// A refused request leaves the sale standing, and a candidate.
	if _, err := env.db.Exec(`UPDATE sale_reversals SET status = 'refused' WHERE ticket_sale_id = $1`, saleIDByRef(t, env, inFlightRef)); err != nil {
		t.Fatalf("refuse the request: %v", err)
	}
	assertUninvoicedRefs(t, operatorSessionID, "reversal request refused", control, inFlightRef)

	// Free: nothing was paid.
	approvedRef(t, beginCheckoutSettled(t, env, "test-org", "free-fest", "", checkoutBody("free@example.com", "Bea", "Mora", cartLine(freeID, 2))))
	// Imported: the money never touched the platform.
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
	// Manually Recorded: the same, typed one at a time.
	recordManualSaleOK(t, env, adminSessionID, freeEventID, manualSaleBody("hand@example.com", "Hugo", "Paz", freeID, 1, "cash", "2026-07-02T10:00:00Z"))
	assertUninvoicedRefs(t, operatorSessionID, "free, imported and manual sales", control, inFlightRef)

	// Already invoiced: a paid House checkout on the open app owes a Sale
	// Invoice, and that row — owed, then refused by the SRI and annulled
	// by the operator, then withdrawn — keeps the sale out whatever its
	// status. Withdrawn is set by SQL: the only route there is a reversal,
	// which would also reverse the sale.
	owedRef := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	invoices := getSaleInvoiceList(t, operatorSessionID)
	if invoices.Pagination.Total != 1 || invoices.Data[0].Status != "owed" || *invoices.Data[0].SaleConfirmationRef != owedRef {
		t.Fatalf("setup: invoices = %+v; want one owed Sale Invoice for %s", invoices.Data, owedRef)
	}
	assertUninvoicedRefs(t, operatorSessionID, "sale invoice owed", control, inFlightRef)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
	})
	if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
		t.Fatalf("drain = %+v; want the document parked", result)
	}
	if annulled := annulOK(t, operatorSessionID, invoices.Data[0].ID); annulled.Status != "annulled" {
		t.Fatalf("annul = %s; want annulled", annulled.Status)
	}
	assertUninvoicedRefs(t, operatorSessionID, "sale invoice annulled", control, inFlightRef)
	// SQL: only a Sale Reversal withdraws an owed document, and this one is
	// already annulled; no endpoint moves it to withdrawn from here.
	if _, err := env.db.Exec(`UPDATE invoicing_invoices SET status = 'withdrawn', annulled_by = NULL, annulled_at = NULL WHERE id = $1`, invoices.Data[0].ID); err != nil {
		t.Fatalf("set the document withdrawn: %v", err)
	}
	assertUninvoicedRefs(t, operatorSessionID, "sale invoice withdrawn", control, inFlightRef)

	// Non-House: a second Organization, never designated, sells the same way.
	otherAdmin := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherAdmin, "Other Org", "other-org")
	_, otherGA := publishCheckoutEvent(t, env, otherAdmin, "Other Fest", "other-fest", 1000, 10)
	begin := beginCheckoutOK(t, payphoneEnv, "other-org", "other-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(otherGA, 1)))
	if resp, body := confirmCheckoutParams(t, payphoneEnv, begin.ClientTransactionID, payphoneReturnParams(begin.ClientTransactionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("other-org confirm status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertUninvoicedRefs(t, operatorSessionID, "non-House sale", control, inFlightRef)
}

// TestUninvoicedHouseSalesAreOrderedOldestFirstAndPaged: three sales with
// three different sale dates come back oldest first whatever order they
// were made in, in pages whose total and total_pages are the whole
// backlog's, and page_size is bounded above at 100.
func TestUninvoicedHouseSalesAreOrderedOldestFirstAndPaged(t *testing.T) {
	env := setupTest(t)
	closed := startAppWithSaleInvoicingClosed(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	// The fixed clock stamps every sale the same instant; the dates are
	// moved by SQL because nothing else can make three sales differ, and
	// the newest-made sale is given the oldest date so creation order
	// cannot pass for sale order.
	refs := make([]string, 3)
	for i := range refs {
		refs[i] = paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	}
	for i, ref := range refs {
		if _, err := env.db.Exec(`UPDATE ticket_sales SET sold_at = $1 WHERE confirmation_ref = $2`, env.fixedClock.Add(-time.Duration(i+1)*24*time.Hour), ref); err != nil {
			t.Fatalf("move sold_at: %v", err)
		}
	}
	oldestFirst := []string{refs[2], refs[1], refs[0]}
	assertUninvoicedRefs(t, operatorSessionID, "oldest first", oldestFirst...)

	page1 := getUninvoicedHouseSales(t, sriEnv, operatorSessionID, "?page=1&page_size=2")
	if len(page1.Data) != 2 || page1.Data[0].ConfirmationRef != oldestFirst[0] || page1.Data[1].ConfirmationRef != oldestFirst[1] {
		t.Fatalf("page 1 = %+v; want the two oldest sales %v", page1.Data, oldestFirst[:2])
	}
	if p := page1.Pagination; p.Page != 1 || p.PageSize != 2 || p.Total != 3 || p.TotalPages != 2 {
		t.Fatalf("page 1 pagination = %+v; want page 1 of 2, 2 per page, 3 in all", p)
	}
	if page1.Data[0].SoldAt != env.fixedClock.Add(-72*time.Hour).UTC().Format(time.RFC3339) {
		t.Fatalf("oldest sold_at = %q; want three days before the clock", page1.Data[0].SoldAt)
	}
	page2 := getUninvoicedHouseSales(t, sriEnv, operatorSessionID, "?page=2&page_size=2")
	if len(page2.Data) != 1 || page2.Data[0].ConfirmationRef != oldestFirst[2] {
		t.Fatalf("page 2 = %+v; want the newest sale %s alone", page2.Data, oldestFirst[2])
	}
	if p := page2.Pagination; p.Page != 2 || p.Total != 3 || p.TotalPages != 2 {
		t.Fatalf("page 2 pagination = %+v; want page 2 of 2, 3 in all", p)
	}
	if capped := getUninvoicedHouseSales(t, sriEnv, operatorSessionID, "?page_size=1000"); capped.Pagination.PageSize != 100 {
		t.Fatalf("page_size=1000 answered page_size %d; want capped at 100", capped.Pagination.PageSize)
	}
	if n := getUninvoicedHouseSaleCount(t, sriEnv, operatorSessionID); n != 3 {
		t.Fatalf("count = %d; want 3", n)
	}
}

// TestUninvoicedHouseSalesAreBehindTheFlagAndOperatorOnly: with the flag
// closed both endpoints answer 404 SALE_INVOICING_UNAVAILABLE, whatever the
// backlog holds; unauthenticated is 401 and an Organization Admin is 403.
func TestUninvoicedHouseSalesAreBehindTheFlagAndOperatorOnly(t *testing.T) {
	env := setupTest(t)
	closed := startAppWithSaleInvoicingClosed(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)
	ref := paidCheckoutWhileClosed(t, closed, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertUninvoicedRefs(t, operatorSessionID, "open app", ref)

	for _, path := range []string{uninvoicedSalesPath, uninvoicedSalesPath + "/count"} {
		resp, body := closed.get(t, path, authHeader(operatorSessionID))
		if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
			t.Fatalf("GET %s while closed: status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", path, resp.StatusCode, body.Error)
		}
		resp, body = sriEnv.get(t, path, nil)
		expectRefusal(t, "GET "+path+" unauthenticated", resp, body, http.StatusUnauthorized, "UNAUTHORIZED")
		resp, body = sriEnv.get(t, path, authHeader(adminSessionID))
		expectRefusal(t, "GET "+path+" as org_admin", resp, body, http.StatusForbidden, "FORBIDDEN")
	}
}
