package integration

import (
	"net/http"
	"testing"
)

// A TICKET SALE'S ORIGIN (#370, parent #366, ADR 0052): how the sale reached the
// platform, stated on every Sales list row so a row nobody recognises can be
// accounted for.
//
// Four answers, and the whole set is DERIVED — nothing on `ticket_sales` records
// a route, and this feature adds no column to make one (ADR 0052: "Its origin is
// derived, never stored"):
//
//   - `sale_import`           — it arrived in an uploaded Sale Import batch.
//   - `manually_recorded`     — somebody typed it: a Manually Recorded Sale.
//   - `correction_replacement`— it is a Sale Correction's replacement.
//   - `channel_sale`          — it sold on a Sales Channel of its own, with no
//     import behind it at all (an Online Sale today).
//
// The middle one is the fragile one, and it is why these tests exist as their
// own file. A Manually Recorded Sale is recognised by a THREE-WAY NEGATIVE —
// imported, no batch, no correction linkage — so the assertions that matter most
// here are the ones that prove the OTHER batchless imported sale, a correction's
// replacement, does not answer to that name, and that a batch's sale does not
// either. See sales.DeriveSaleOrigin, which is the single place the predicate
// is spelled out.
//
// Everything is asserted at the HTTP seam: the origin a Sales list row carries,
// for a reader who may read the list at all.

// originsByEmail reads a page of the Sales list and maps each row's buyer to the
// origin the API stated for it — the whole of what these tests assert.
func originsByEmail(t *testing.T, env *testEnv, sessionID, eventID, status string) map[string]string {
	t.Helper()
	resp, body := env.get(t,
		"/api/v1/staff/events/"+eventID+"/sales?status="+status+"&page_size=100", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	out := map[string]string{}
	for _, row := range salesList(t, body).Data {
		out[row.CustomerEmail] = row.Origin
	}
	return out
}

// EVERY ROUTE INTO THE PLATFORM NAMES ITSELF, AND THE TWO BATCHLESS ONES ARE
// TOLD APART. One Event, one of each: a sale from an uploaded file, a sale typed
// by hand, a Sale Correction's replacement, and an Online Sale nobody imported.
func TestSalesListStatesHowEachSaleReachedThePlatform(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Origin Fest", "origin-fest", 1000, 100)

	// The file: two sales in one uploaded Sale Import batch.
	commitBatch(t, env, sessionID, eventID, "the-file", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "eve@example.com", "customer_first_name": "Eve", "customer_last_name": "Ruiz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T11:00:00Z"},
	})
	eve := saleRowByEmail(t, env, sessionID, eventID, "eve@example.com", "active")

	// The form: one Manually Recorded Sale, batchless and linked to nothing.
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-02T10:00:00Z"))

	// The correction: Eve's sale reversed and replaced under a fixed email. The
	// replacement is ALSO an imported sale with no batch — the one shape that
	// could be mistaken for a hand-typed one, and is not, because it points at
	// the sale it stands in for.
	correctImportedSaleOK(t, env, sessionID, eventID, eve.ID,
		correctionBody("cara@example.com", "Cara", "Diaz", gaID, 1, "cash", "2026-07-01T11:00:00Z"))

	// The Sales Channel: an Online Sale, which no import ever touched.
	approved := beginCheckoutOK(t, env, "test-org", "origin-fest",
		checkoutBody("dora@example.com", "Dora", "Vega", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirm := confirmCheckoutOK(t, env, approved.ClientTransactionID, "approved"); confirm.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirm.Status)
	}

	active := originsByEmail(t, env, sessionID, eventID, "active")
	want := map[string]string{
		"ana@example.com":  "sale_import",
		"bob@example.com":  "manually_recorded",
		"cara@example.com": "correction_replacement",
		"dora@example.com": "channel_sale",
	}
	for email, origin := range want {
		if active[email] != origin {
			t.Errorf("%s origin = %q, want %q", email, active[email], origin)
		}
	}
	if len(active) != len(want) {
		t.Errorf("active rows = %v, want exactly the four seeded sales", active)
	}
}

// A SALE'S ORIGIN IS WHERE IT CAME FROM, NOT WHAT LATER HAPPENED TO IT. A
// reversal is not a route: the corrected original still arrived in its file, and
// a hand-typed sale reversed on its own was still typed. Both are read off the
// reversed view, where the status filter is the only way to reach them.
func TestASaleKeepsItsOriginAfterItIsReversedOrCorrected(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Undone Fest", "undone-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	commitBatch(t, env, sessionID, eventID, "the-file", []map[string]any{
		{"customer_email": "eve@example.com", "customer_first_name": "Eve", "customer_last_name": "Ruiz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	eve := saleRowByEmail(t, env, sessionID, eventID, "eve@example.com", "active")
	correctImportedSaleOK(t, env, sessionID, eventID, eve.ID,
		correctionBody("eva@example.com", "Eva", "Ruiz", gaID, 1, "cash", "2026-07-01T10:00:00Z"))

	typo := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-02T10:00:00Z"))
	reverseImportedSaleOK(t, env, sessionID, eventID, typo.SaleID)

	reversed := originsByEmail(t, env, sessionID, eventID, "reversed")
	if reversed["eve@example.com"] != "sale_import" {
		t.Errorf("the corrected original's origin = %q, want sale_import — being replaced is not a route in",
			reversed["eve@example.com"])
	}
	if reversed["bob@example.com"] != "manually_recorded" {
		t.Errorf("the reversed hand-typed sale's origin = %q, want manually_recorded — it was still typed",
			reversed["bob@example.com"])
	}
}

// THE ORIGIN REACHES EVERY MEMBER WHO MAY READ THE SALES LIST, AND IS NOTHING
// BUT A STATEMENT. Event Staff read the list and no more: the origin tells them
// where a row came from and hands them no lever the list did not already offer —
// recording a sale stays refused to them (manual_sale_test.go).
func TestTheOriginIsStatedToEveryMemberWhoCanReadTheSalesList(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Read Fest", "read-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "the-file", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-02T10:00:00Z"))

	resp, body := env.post(t, "/api/v1/staff/members",
		map[string]string{"email": "staff@example.com", "role": "event_staff"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSession := verifyOTP(t, env, "staff@example.com")

	seen := originsByEmail(t, env, staffSession, eventID, "active")
	if seen["ana@example.com"] != "sale_import" || seen["bob@example.com"] != "manually_recorded" {
		t.Errorf("Event Staff read origins %v, want the same account of the two routes an Org Admin gets", seen)
	}
}
