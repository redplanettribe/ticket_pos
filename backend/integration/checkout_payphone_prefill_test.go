package integration

import (
	"net/http"
	"testing"
)

// The PayPhone prefills at the provider seam (#104, parent #103): the buyer
// reaches PayPhone's hosted card form with their email and their identification
// number already filled, instead of being asked for both a second time.
//
// Everything here is asserted against the Prepare payload the fake PayPhone
// server recorded, driven end to end through the real public checkout endpoint —
// the highest seam this behaviour is visible at. The parent spec deliberately
// leaves the same ground uncovered at the unit level: pinning the field mapping
// twice would fix it in place twice.
//
// These sit beside checkout_payphone_test.go the way checkout_tax_id_test.go
// sits beside checkout_test.go: the general provider legs are pinned there, this
// one feature is pinned here.

// prefillEnv resets the fake PayPhone server around a test, so a recorded
// Prepare belongs to this test and no responder outlives it. The general
// PayPhone tests rely on the default answers; these ones count requests, which
// leaked state would quietly corrupt.
func prefillEnv(t *testing.T) *testEnv {
	t.Helper()
	env := setupTest(t)
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	return env
}

// TestPayPhonePrepareCarriesEmailAndDocumentID walks the three Tax ID Types
// through a real Prepare. The email always rides along; the number rides as
// `documentId` for a cédula and a RUC, the two Ecuadorian identifiers the field
// was built for. A passport is withheld — the field would likely refuse it and a
// refused Prepare is a failed checkout, strictly worse than the buyer typing the
// number on PayPhone's own form as they do today (#103, "Out of Scope").
//
// The Tax ID Type itself is never sent: the redirect integration this system
// uses has no parameter for it.
func TestPayPhonePrepareCarriesEmailAndDocumentID(t *testing.T) {
	env := prefillEnv(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Prefill Fest", "prefill-fest", 1000, 20)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	cases := []struct {
		name           string
		email          string
		taxIDType      string
		number         string
		wantDocumentID string // empty means the key must be absent entirely
	}{
		{"cedula", "cedula-buyer@example.com", "cedula", validCedula, validCedula},
		{"natural ruc", "natural-ruc@example.com", "ruc", naturalRUC, naturalRUC},
		{"company ruc", "company-ruc@example.com", "ruc", companyRUC, companyRUC},
		{"passport", "passport-buyer@example.com", "passport", lowercasePassprt, ""},
	}
	for _, tc := range cases {
		begin := beginCheckoutOK(t, payphoneEnv, "test-org", "prefill-fest",
			taxIDCheckoutBody(tc.email, "Ana", "Lopez", tc.taxIDType, tc.number, line))

		// Every case is a checkout that succeeded: the passport buyer loses a
		// prefill, never their purchase.
		if begin.RedirectURL != payphoneStub.cardURL() {
			t.Fatalf("%s: redirect url = %q, want PayPhone's payWithCard url", tc.name, begin.RedirectURL)
		}

		req := payphoneStub.lastPrepareRequest(t)
		if req.body["email"] != tc.email {
			t.Fatalf("%s: email = %v, want the buyer's %q on every Prepare", tc.name, req.body["email"], tc.email)
		}
		got, sent := req.body["documentId"]
		switch {
		case tc.wantDocumentID == "" && sent:
			t.Fatalf("%s: documentId = %v, want the key absent entirely for a passport", tc.name, got)
		case tc.wantDocumentID != "" && got != tc.wantDocumentID:
			t.Fatalf("%s: documentId = %v, want the Tax ID number %q", tc.name, got, tc.wantDocumentID)
		}
		// The Type has no home in this integration; nothing may carry it.
		for _, key := range []string{"identificationType", "documentType", "customer_tax_id_type"} {
			if _, ok := req.body[key]; ok {
				t.Fatalf("%s: Prepare carries %q; the redirect integration has no field for the Tax ID Type", tc.name, key)
			}
		}
	}
}

// TestPayPhonePrepareRejectionRetriesWithoutPrefills pins the safety net that
// makes the prefills shippable (#103, "Failure handling"): this is the first
// data the system has ever sent PayPhone, so a Prepare refused with a 4xx is
// retried exactly once with every prefill stripped — reproducing the payload
// that worked before this feature existed. The buyer's checkout succeeds on the
// retry; a convenience never costs them their purchase.
func TestPayPhonePrepareRejectionRetriesWithoutPrefills(t *testing.T) {
	env := prefillEnv(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Fallback Fest", "fallback-fest", 1000, 10)

	payphoneStub.prepareFailsOnce(http.StatusBadRequest)
	begin := beginCheckoutOK(t, payphoneEnv, "test-org", "fallback-fest",
		taxIDCheckoutBody("rejected@example.com", "Ana", "Lopez", "cedula", validCedula,
			map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	if begin.RedirectURL != payphoneStub.cardURL() {
		t.Fatalf("redirect url = %q, want the retry's payWithCard url — a rejected prefill must not fail the checkout", begin.RedirectURL)
	}
	if got := payphoneStub.prepareCount(); got != 2 {
		t.Fatalf("Prepare calls = %d, want exactly 2: the refused attempt and one stripped retry", got)
	}

	first := payphoneStub.prepareRequest(t, 0)
	if first.body["email"] != "rejected@example.com" || first.body["documentId"] != validCedula {
		t.Fatalf("first Prepare = %v/%v, want the prefills that were refused", first.body["email"], first.body["documentId"])
	}

	// The retry is today's payload exactly: the prefills gone, everything the
	// charge depends on unchanged.
	retry := payphoneStub.prepareRequest(t, 1)
	for _, key := range []string{"email", "documentId"} {
		if _, ok := retry.body[key]; ok {
			t.Fatalf("retry Prepare still carries %q = %v, want every prefill stripped", key, retry.body[key])
		}
	}
	if retry.body["clientTransactionId"] != begin.ClientTransactionID {
		t.Fatalf("retry clientTransactionId = %v, want the same attempt %q", retry.body["clientTransactionId"], begin.ClientTransactionID)
	}
	// 2 × the all-in 1115¢ a 1000¢ ticket costs under 'pass_on' Fee Handling
	// (ADR 0014) — the amount the refused attempt asked for, unmoved by the retry.
	if retry.body["amount"] != float64(2230) || retry.body["amountWithoutTax"] != float64(2230) {
		t.Fatalf("retry amount/amountWithoutTax = %v/%v, want the unchanged 2230/2230", retry.body["amount"], retry.body["amountWithoutTax"])
	}
	if retry.body["storeId"] != payphoneTestStoreID || retry.body["currency"] != "USD" {
		t.Fatalf("retry storeId/currency = %v/%v, want the unchanged %q/USD", retry.body["storeId"], retry.body["currency"], payphoneTestStoreID)
	}
	if retry.body["responseUrl"] != "http://storefront.example/checkout/return" {
		t.Fatalf("retry responseUrl = %v, want the unchanged Storefront return route", retry.body["responseUrl"])
	}
	if retry.authorization != "Bearer "+payphoneTestAPIToken {
		t.Fatalf("retry authorization = %q, want the configured Bearer token", retry.authorization)
	}

	// One attempt, one Payment: the retry is a second HTTP call, never a second
	// Capacity Hold.
	var payments int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if payments != 1 {
		t.Fatalf("payments = %d, want the single attempt the retry belongs to", payments)
	}
}

// TestPayPhonePrepareServerErrorIsNotRetried pins the tight scope of that
// fallback. A 5xx says nothing about the fields we sent — PayPhone is simply
// unwell — and re-posting a charge request on one risks a double charge, so the
// stripped retry is reserved for the 4xx that actually blames our payload
// (#103, "Failure handling"). The checkout fails as it did before the prefills
// existed, leaving the pending Payment to lapse (ADR 0013).
func TestPayPhonePrepareServerErrorIsNotRetried(t *testing.T) {
	env := prefillEnv(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Outage Prefill Fest", "outage-prefill-fest", 1000, 10)

	payphoneStub.prepareFailsOnce(http.StatusInternalServerError)
	resp, body := beginCheckout(t, payphoneEnv, "test-org", "outage-prefill-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("begin status=%d error=%+v, want 500", resp.StatusCode, body.Error)
	}
	// Had the 5xx been retried, prepareFailsOnce would have answered the second
	// call normally and the checkout would have succeeded — so the failure above
	// and the count below pin the rule from both sides.
	if got := payphoneStub.prepareCount(); got != 1 {
		t.Fatalf("Prepare calls = %d, want exactly 1: a 5xx is never retried", got)
	}

	var status string
	if err := env.db.QueryRow(`SELECT status FROM payments`).Scan(&status); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if status != "pending" {
		t.Fatalf("payment = %s, want the pending attempt left to lapse", status)
	}
}
