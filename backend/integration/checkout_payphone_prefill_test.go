package integration

import (
	"net/http"
	"testing"
)

// The PayPhone prefills at the provider seam (#104, #106, parent #103): the
// buyer reaches PayPhone's hosted card form with their email, their
// identification number and — when they volunteered one — their phone number
// already filled, instead of being asked for each a second time.
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

// Phone numbers used below. The Ecuadorian one is already canonical; the others
// exist to prove that what reaches PayPhone and the Payment is the canonical
// form regardless of how a person wrote it down (#105's two tiers).
const (
	ecuadorMobile      = "+593987654321"
	ecuadorMobileTyped = "+593 (0)98-765.4321" // the same number as a human writes it
	foreignMobile      = "+12025550123"        // generic E.164: no national rule beyond Ecuador's
)

// phoneCheckoutBody is a begin-checkout body carrying a phone number, for the
// tests that care which one was typed. Every other checkout body in this package
// omits the field entirely — which is itself the behaviour pinned below.
func phoneCheckoutBody(email, phone string, lines ...map[string]any) map[string]any {
	body := checkoutBody(email, "Ana", "Lopez", lines...)
	body["customer_phone"] = phone
	return body
}

// readPaymentPhone reads the phone number recorded on a Payment. SQL because no
// API exposes Payment rows, like every other Payment assertion in this package.
func readPaymentPhone(t *testing.T, env *testEnv, clientTransactionID string) *string {
	t.Helper()
	var phone *string
	if err := env.db.QueryRow(`
		SELECT customer_phone FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&phone); err != nil {
		t.Fatalf("read payment phone: %v", err)
	}
	return phone
}

// TestPayPhonePrepareCarriesPhoneNumber is the point of the whole feature: a
// buyer who typed their number reaches PayPhone's form with nothing left to
// enter but their card. The number travels as `phoneNumber` in canonical E.164 —
// "Símbolo(+) + Código País + número", the only form PayPhone documents — and is
// recorded on the Payment in that same form, which is what will let it survive
// the redirect to the Customer's profile (#107).
//
// The cases cover both validation tiers and the normalisation between them: an
// Ecuadorian mobile written the way a person writes it, punctuation and domestic
// trunk zero included, and a foreign number under the deliberately permissive
// generic rule.
func TestPayPhonePrepareCarriesPhoneNumber(t *testing.T) {
	env := prefillEnv(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Phone Fest", "phone-fest", 1000, 20)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	cases := []struct {
		name  string
		email string
		typed string
		want  string
	}{
		{"ecuadorian mobile", "ec-buyer@example.com", ecuadorMobile, ecuadorMobile},
		{"ecuadorian mobile as written", "ec-typed@example.com", ecuadorMobileTyped, ecuadorMobile},
		{"foreign number", "us-buyer@example.com", foreignMobile, foreignMobile},
	}
	for _, tc := range cases {
		begin := beginCheckoutOK(t, payphoneEnv, "test-org", "phone-fest",
			phoneCheckoutBody(tc.email, tc.typed, line))

		req := payphoneStub.lastPrepareRequest(t)
		if req.body["phoneNumber"] != tc.want {
			t.Fatalf("%s: phoneNumber = %v, want the canonical %q", tc.name, req.body["phoneNumber"], tc.want)
		}
		// The phone never displaces the prefills it joins.
		if req.body["email"] != tc.email || req.body["documentId"] != validCedula {
			t.Fatalf("%s: email/documentId = %v/%v, want the other prefills untouched", tc.name, req.body["email"], req.body["documentId"])
		}
		if got := readPaymentPhone(t, env, begin.ClientTransactionID); got == nil || *got != tc.want {
			t.Fatalf("%s: payment phone = %v, want the canonical %q recorded", tc.name, got, tc.want)
		}
	}
}

// TestPayPhonePrepareOmitsPhoneNumberWhenNotGiven is the constraint the product
// decision rests on, and it is not a formality: PayPhone's documentation
// prohibits "datos quemados o estáticos" — hardcoded or static cardholder data —
// on pain of transaction rejection and account blocking. A buyer who leaves the
// field blank must produce a Prepare with NO phoneNumber key at all: not an
// empty string, not a placeholder, not a default. Their checkout is today's
// checkout, unchanged, and PayPhone asks them on its own form.
//
// The omitted and the blank case are asserted together because they are the same
// answer arriving by two routes — one buyer who never saw the field, one who saw
// it and typed nothing but spaces.
func TestPayPhonePrepareOmitsPhoneNumberWhenNotGiven(t *testing.T) {
	env := prefillEnv(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "No Phone Fest", "no-phone-fest", 1000, 20)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	cases := []struct {
		name string
		body map[string]any
	}{
		{"field absent", checkoutBody("silent@example.com", "Ana", "Lopez", line)},
		{"field blank", phoneCheckoutBody("blank@example.com", "", line)},
		{"field whitespace", phoneCheckoutBody("spaces@example.com", "   ", line)},
	}
	for _, tc := range cases {
		begin := beginCheckoutOK(t, payphoneEnv, "test-org", "no-phone-fest", tc.body)

		// Skipping an optional convenience costs the buyer nothing: same redirect,
		// same purchase.
		if begin.RedirectURL != payphoneStub.cardURL() {
			t.Fatalf("%s: redirect url = %q, want PayPhone's payWithCard url", tc.name, begin.RedirectURL)
		}

		req := payphoneStub.lastPrepareRequest(t)
		if got, sent := req.body["phoneNumber"]; sent {
			t.Fatalf("%s: Prepare carries phoneNumber = %v, want the key absent entirely — never a fabricated value", tc.name, got)
		}
		// The prefills the buyer DID supply are unaffected: an absent phone is not
		// a reason to stop sending what is genuinely known.
		if req.body["email"] == nil || req.body["documentId"] != validCedula {
			t.Fatalf("%s: email/documentId = %v/%v, want the other prefills still sent", tc.name, req.body["email"], req.body["documentId"])
		}
		if got := readPaymentPhone(t, env, begin.ClientTransactionID); got != nil {
			t.Fatalf("%s: payment phone = %q, want NULL — no phone is one state, not two", tc.name, *got)
		}
	}
}

// TestBeginCheckoutRejectsMalformedPhone pins the other half of "optional": the
// field may be skipped, but a number that IS typed has to be a number. Each case
// is a field-level error on `customer_phone` — the name the form marks — and the
// rejection is total: no Payment, no Capacity Hold, and PayPhone never asked to
// prepare a charge for a request that was never valid.
//
// The verdict is platform.ValidatePhone's, mirrored in the Storefront so the
// buyer normally hears about it without a round trip; this is the server's say,
// which is the one that decides whether a Sale can happen. Note the Ecuadorian
// landline: it parses as a real telephone number and is still refused, because
// PayPhone's form wants a cardholder's mobile.
func TestBeginCheckoutRejectsMalformedPhone(t *testing.T) {
	env := prefillEnv(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Bad Phone Fest", "bad-phone-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	cases := []struct {
		name  string
		phone string
	}{
		{"no dialling code", "0987654321"},
		{"ecuadorian landline", "+59322345678"},
		{"ecuadorian mobile too short", "+59398765432"},
		{"ecuadorian mobile too long", "+5939876543210"},
		{"letters", "+593 not-a-number"},
		{"longer than E.164 allows", "+1202555012345678"},
	}
	for _, tc := range cases {
		resp, envBody := beginCheckout(t, payphoneEnv, "test-org", "bad-phone-fest",
			phoneCheckoutBody("ana@example.com", tc.phone, line))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d error=%+v, want 400", tc.name, resp.StatusCode, envBody.Error)
		}
		if envBody.Error == nil || envBody.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%s: error=%+v, want VALIDATION_FAILED", tc.name, envBody.Error)
		}
		if fields := fieldErrors(t, envBody); fields["customer_phone"] == "" {
			t.Fatalf("%s: fields=%+v, want an error on customer_phone", tc.name, fields)
		}
	}

	var payments int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if payments != 0 {
		t.Fatalf("payments after rejected begins = %d, want 0", payments)
	}
	if got := payphoneStub.prepareCount(); got != 0 {
		t.Fatalf("Prepare calls = %d, want 0: a request rejected at validation never reaches the provider", got)
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
	body := taxIDCheckoutBody("rejected@example.com", "Ana", "Lopez", "cedula", validCedula,
		map[string]any{"ticket_type_id": gaID, "quantity": 2})
	// A phone rides on the refused attempt too, because it is the prefill whose
	// acceptance is least certain: PayPhone's documentation neither states nor
	// denies that a foreign dialling code is allowed (#103, "Further Notes"), and
	// this retry is the safety net that lets the feature ship ahead of that
	// answer. It must therefore be stripped with the rest.
	body["customer_phone"] = ecuadorMobile
	begin := beginCheckoutOK(t, payphoneEnv, "test-org", "fallback-fest", body)

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
	if first.body["phoneNumber"] != ecuadorMobile {
		t.Fatalf("first Prepare phoneNumber = %v, want the refused %q", first.body["phoneNumber"], ecuadorMobile)
	}

	// The retry is today's payload exactly: the prefills gone, everything the
	// charge depends on unchanged.
	retry := payphoneStub.prepareRequest(t, 1)
	for _, key := range []string{"email", "documentId", "phoneNumber"} {
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
