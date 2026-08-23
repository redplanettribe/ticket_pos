package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Tax ID at the Storefront checkout seam (#98, ADR 0016): a buyer must
// supply a Tax ID Type and number to begin an Online Sale, the recorded Ticket
// Sale snapshots what was transacted under, and the Customer's stored Tax ID
// follows the asymmetric write-back rules — fill a blank, refresh while
// unverified, never overwrite a Verified Customer unless the checkout ran under
// that Customer's own Customer Session.
//
// These assert at HTTP: begin, confirm, and the session read. The snapshot
// columns themselves are read with SQL, like every other Customer/sale identity
// assertion in this package, because no API surfaces them yet (#103 adds the
// Sales list columns).

// Tax IDs used across these tests. The cédulas carry real modulo-10 check
// digits — an invalid one is rejected long before it reaches a declaration, so
// the fixtures cannot be arbitrary digits.
const (
	validCedula      = "1712345675"
	otherCedula      = "0923456784"
	naturalRUC       = "1712345675001" // cédula + establishment suffix
	companyRUC       = "1790012346001" // third digit 9: the company form
	invalidCedula    = "1712345678"    // right length, wrong check digit
	lowercasePassprt = "ab123456"
)

// taxIDCheckoutBody is a begin-checkout body with an explicit Tax ID, for the
// tests that care which one was typed. checkoutBody's default cédula serves
// every other checkout test in this package.
func taxIDCheckoutBody(email, firstName, lastName, taxIDType, taxIDNumber string, lines ...map[string]any) map[string]any {
	body := checkoutBody(email, firstName, lastName, lines...)
	body["customer_tax_id_type"] = taxIDType
	body["customer_tax_id_number"] = taxIDNumber
	return body
}

// beginCheckoutAs begins a checkout carrying A PARTICULAR Customer Session, the
// way the Storefront BFF does — for the tests that care WHOSE session it is
// rather than merely that there is one.
//
// An empty token no longer means a guest checkout, because there is no such
// thing (ADR 0054, #386): it means the ordinary helper's behaviour, which is to
// sign the body's buyer in. The tests that read the refusal of an anonymous
// request post one deliberately, with no buyer to sign in.
func beginCheckoutAs(t *testing.T, env *testEnv, orgSlug, eventSlug, token string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	return beginCheckoutWithHeaders(t, env, orgSlug, eventSlug, body, headers)
}

func beginCheckoutAsOK(t *testing.T, env *testEnv, orgSlug, eventSlug, token string, body map[string]any) beginCheckoutResult {
	t.Helper()
	resp, envBody := beginCheckoutAs(t, env, orgSlug, eventSlug, token, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("begin checkout status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	var result beginCheckoutResult
	if err := json.Unmarshal(envBody.Data, &result); err != nil {
		t.Fatalf("decode begin checkout result: %v", err)
	}
	return result
}

// taxIDPair is a Tax ID as stored: both halves null together or set together.
type taxIDPair struct {
	Type   *string
	Number *string
}

func (p taxIDPair) String() string {
	if p.Type == nil && p.Number == nil {
		return "<none>"
	}
	typ, num := "<nil>", "<nil>"
	if p.Type != nil {
		typ = *p.Type
	}
	if p.Number != nil {
		num = *p.Number
	}
	return typ + ":" + num
}

func (p taxIDPair) is(taxIDType, number string) bool {
	return p.Type != nil && p.Number != nil && *p.Type == taxIDType && *p.Number == number
}

func (p taxIDPair) isNone() bool { return p.Type == nil && p.Number == nil }

// readCustomerTaxID reads the Customer's current Tax ID assertion. SQL because
// the Customer record has no read endpoint of its own; the session payload
// (asserted separately below) is the only place it surfaces.
func readCustomerTaxID(t *testing.T, env *testEnv, email string) taxIDPair {
	t.Helper()
	var p taxIDPair
	if err := env.db.QueryRow(`
		SELECT tax_id_type, tax_id_number FROM customers WHERE email = $1
	`, email).Scan(&p.Type, &p.Number); err != nil {
		t.Fatalf("read customer tax id %q: %v", email, err)
	}
	return p
}

// readSaleTaxID reads one Ticket Sale's immutable Tax ID snapshot, by Sale
// Confirmation reference.
func readSaleTaxID(t *testing.T, env *testEnv, ref string) taxIDPair {
	t.Helper()
	var p taxIDPair
	if err := env.db.QueryRow(`
		SELECT customer_tax_id_type, customer_tax_id_number
		FROM ticket_sales WHERE confirmation_ref = $1
	`, ref).Scan(&p.Type, &p.Number); err != nil {
		t.Fatalf("read sale tax id %q: %v", ref, err)
	}
	return p
}

// fieldErrors flattens the validation envelope's field details into
// field → message, which is the shape the checkout form consumes.
func fieldErrors(t *testing.T, body envelope) map[string]string {
	t.Helper()
	if body.Error == nil {
		t.Fatalf("expected an error envelope, got none")
	}
	raw, err := json.Marshal(body.Error.Details)
	if err != nil {
		t.Fatalf("marshal error details: %v", err)
	}
	var details struct {
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode error details %s: %v", raw, err)
	}
	out := map[string]string{}
	for _, f := range details.Fields {
		out[f.Field] = f.Message
	}
	return out
}

// TestBeginCheckoutRequiresValidTaxID pins the gate: a missing, malformed, or
// unknown-typed Tax ID is a field-level validation failure, blamed on the half
// of the pair that is actually at fault, and no Payment is created.
func TestBeginCheckoutRequiresValidTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Tax Fest", "tax-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	cases := []struct {
		name       string
		taxIDType  string
		number     string
		wantField  string
		omitFields bool
	}{
		{name: "both missing", omitFields: true, wantField: "customer_tax_id_type"},
		{name: "missing type", taxIDType: "", number: validCedula, wantField: "customer_tax_id_type"},
		{name: "missing number", taxIDType: "cedula", number: "", wantField: "customer_tax_id_number"},
		{name: "unknown type", taxIDType: "dni", number: validCedula, wantField: "customer_tax_id_type"},
		{name: "bad check digit", taxIDType: "cedula", number: invalidCedula, wantField: "customer_tax_id_number"},
		{name: "wrong length", taxIDType: "cedula", number: "17123456", wantField: "customer_tax_id_number"},
		{name: "impossible province", taxIDType: "cedula", number: "9912345675", wantField: "customer_tax_id_number"},
		{name: "ruc of the wrong length", taxIDType: "ruc", number: validCedula, wantField: "customer_tax_id_number"},
		{name: "passport too short", taxIDType: "passport", number: "AB12", wantField: "customer_tax_id_number"},
	}
	for _, tc := range cases {
		body := checkoutBody("ana@example.com", "Ana", "Lopez", line)
		if tc.omitFields {
			delete(body, "customer_tax_id_type")
			delete(body, "customer_tax_id_number")
		} else {
			body["customer_tax_id_type"] = tc.taxIDType
			body["customer_tax_id_number"] = tc.number
		}

		resp, envBody := beginCheckout(t, env, "test-org", "tax-fest", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", tc.name, resp.StatusCode)
		}
		if envBody.Error == nil || envBody.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%s: error=%+v, want VALIDATION_FAILED", tc.name, envBody.Error)
		}
		if fields := fieldErrors(t, envBody); fields[tc.wantField] == "" {
			t.Fatalf("%s: fields=%+v, want an error on %s", tc.name, fields, tc.wantField)
		}
	}

	// The rejections are total: nothing was recorded, so no Capacity Hold was
	// taken out on a checkout that never started.
	var payments int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if payments != 0 {
		t.Fatalf("payments after rejected begins = %d, want 0", payments)
	}
}

// TestOnlineSaleSnapshotsEveryTaxIDType walks the three Tax ID Types end to end
// and proves each lands on the recorded sale — including the passport's
// uppercase normalisation, which is what makes "ab123456" and "AB123456" one
// passport rather than two.
func TestOnlineSaleSnapshotsEveryTaxIDType(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Types Fest", "types-fest", 1000, 20)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	cases := []struct {
		email      string
		taxIDType  string
		number     string
		wantNumber string
	}{
		{"cedula-buyer@example.com", "cedula", validCedula, validCedula},
		{"natural-ruc@example.com", "ruc", naturalRUC, naturalRUC},
		{"company-ruc@example.com", "ruc", companyRUC, companyRUC},
		{"passport-buyer@example.com", "passport", lowercasePassprt, "AB123456"},
		{"padded-buyer@example.com", "cedula", "  " + otherCedula + "  ", otherCedula},
	}
	for _, tc := range cases {
		begin := beginCheckoutOK(t, env, "test-org", "types-fest",
			taxIDCheckoutBody(tc.email, "Ana", "Lopez", tc.taxIDType, tc.number, line))
		confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
		if confirmed.Status != "approved" {
			t.Fatalf("%s: confirm status = %q, want approved", tc.email, confirmed.Status)
		}

		if got := readSaleTaxID(t, env, confirmed.ConfirmationRef); !got.is(tc.taxIDType, tc.wantNumber) {
			t.Fatalf("%s: sale tax id = %s, want %s:%s", tc.email, got, tc.taxIDType, tc.wantNumber)
		}
		// A guest's first sale fills the blank Tax ID on the Customer it created.
		if got := readCustomerTaxID(t, env, tc.email); !got.is(tc.taxIDType, tc.wantNumber) {
			t.Fatalf("%s: customer tax id = %s, want %s:%s", tc.email, got, tc.taxIDType, tc.wantNumber)
		}
	}
}

// TestSaleFillsNeverSetTaxIDOnVerifiedCustomer proves the "fill" half of the
// rule on the record that most needs it: someone who signed in before ever
// buying holds a verified record with nothing in it, and their first sale is
// allowed to supply the Tax ID rather than leaving the profile permanently
// blank (ADR 0016, mirroring the name rule).
func TestSaleFillsNeverSetTaxIDOnVerifiedCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Fill Fest", "fill-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	// She verifies first: a Verified Customer with no Tax ID at all.
	customerSignIn(t, env, "ana@example.com")
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.isNone() {
		t.Fatalf("tax id after sign-in = %s, want none", got)
	}

	// A checkout with her email — anonymous, no session token — supplies one.
	begin := beginCheckoutOK(t, env, "test-org", "fill-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", validCedula) {
		t.Fatalf("customer tax id = %s, want the filled cedula:%s", got, validCedula)
	}
	if got := readSaleTaxID(t, env, confirmed.ConfirmationRef); !got.is("cedula", validCedula) {
		t.Fatalf("sale tax id = %s, want cedula:%s", got, validCedula)
	}
}

// TestUnverifiedCustomerTaxIDRefreshedByLaterSale proves the "refresh" half: a
// record assembled on someone's behalf self-corrects until they claim it.
func TestUnverifiedCustomerTaxIDRefreshedByLaterSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Refresh Fest", "refresh-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	first := beginCheckoutOK(t, env, "test-org", "refresh-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	firstSale := confirmCheckoutOK(t, env, first.ClientTransactionID, "approved")

	// The second purchase is made under her company RUC instead.
	second := beginCheckoutOK(t, env, "test-org", "refresh-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "ruc", companyRUC, line))
	secondSale := confirmCheckoutOK(t, env, second.ClientTransactionID, "approved")

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id = %s, want the refreshed ruc:%s", got, companyRUC)
	}
	// Each sale keeps what it was transacted under: the refresh rewrote the
	// profile, never the history.
	if got := readSaleTaxID(t, env, firstSale.ConfirmationRef); !got.is("cedula", validCedula) {
		t.Fatalf("first sale tax id = %s, want the immutable cedula:%s", got, validCedula)
	}
	if got := readSaleTaxID(t, env, secondSale.ConfirmationRef); !got.is("ruc", companyRUC) {
		t.Fatalf("second sale tax id = %s, want ruc:%s", got, companyRUC)
	}
}

// TestABodyNamingAnotherAddressReachesNobody is what the adversarial case became
// (ADR 0054, #386).
//
// It used to read: an unproven visitor types a Verified Customer's email and a
// Tax ID of their choosing, the sale records what was typed, and the person's
// own stored assertion is untouched — the profile guard turning the write away.
// That visitor cannot begin a checkout at all now, so the guard is no longer
// what stands between a stranger and Ana's profile. THE ABSENCE OF THE FIELD IS.
//
// What is left to attack with is a body: a signed-in buyer, or a stale client on
// their behalf, still naming an address in one. This is that request, sent
// verbatim from a real session, and it reaches Ana in no way at all — the sale
// is the sender's, the write-back lands on the sender, and her row does not move.
func TestABodyNamingAnotherAddressReachesNobody(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Guarded Fest", "guarded-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	// Ana buys, which fills her Tax ID: the buyer speaking about themselves.
	own := beginCheckoutOK(t, env, "test-org", "guarded-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	ownSale := confirmCheckoutOK(t, env, own.ClientTransactionID, "approved")

	// Bruno checks out under HIS session, with a body naming HER address and a
	// number of his choosing.
	body := taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", otherCedula, line)
	stranger := beginCheckoutSmugglingOK(t, env, "test-org", "guarded-fest",
		buyerSession(t, env, "bruno@example.com"), body)
	strangerSale := confirmCheckoutOK(t, env, stranger.ClientTransactionID, "approved")

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", validCedula) {
		t.Fatalf("customer tax id = %s, want the person-owned cedula:%s", got, validCedula)
	}
	// The sale is his, addressed to the address his session proved.
	var soldTo string
	if err := env.db.QueryRow(`
		SELECT c.email FROM ticket_sales ts JOIN customers c ON c.id = ts.customer_id
		WHERE ts.confirmation_ref = $1
	`, strangerSale.ConfirmationRef).Scan(&soldTo); err != nil {
		t.Fatalf("read sale customer: %v", err)
	}
	if soldTo != "bruno@example.com" {
		t.Fatalf("the sale went to %q, want the address the session proved", soldTo)
	}
	// And the Tax ID he typed is his own assertion about himself, so it lands on
	// him — which is exactly the write the guard used to have to withhold, now
	// unconditionally safe because there is no way to be typing about anybody else.
	if got := readCustomerTaxID(t, env, "bruno@example.com"); !got.is("cedula", otherCedula) {
		t.Fatalf("his tax id = %s, want the cedula he typed", got)
	}
	if got := readSaleTaxID(t, env, strangerSale.ConfirmationRef); !got.is("cedula", otherCedula) {
		t.Fatalf("his sale tax id = %s, want the typed cedula:%s", got, otherCedula)
	}
	if got := readSaleTaxID(t, env, ownSale.ConfirmationRef); !got.is("cedula", validCedula) {
		t.Fatalf("earlier sale tax id = %s, want the immutable cedula:%s", got, validCedula)
	}
}

// TestSessionCheckoutOverrideBecomesStoredTaxID is the exception the whole flag
// exists for: the same overwrite the stranger above was refused is allowed when
// the checkout runs under the Customer's own Customer Session, because then it
// is the person correcting themselves.
func TestSessionCheckoutOverrideBecomesStoredTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Override Fest", "override-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	first := beginCheckoutOK(t, env, "test-org", "override-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	confirmCheckoutOK(t, env, first.ClientTransactionID, "approved")
	token := customerSignIn(t, env, "ana@example.com")

	// Signed in, she overrides the prefilled cédula with her company RUC.
	override := beginCheckoutAsOK(t, env, "test-org", "override-fest", token,
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "ruc", companyRUC, line))
	overrideSale := confirmCheckoutOK(t, env, override.ClientTransactionID, "approved")

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id = %s, want the override ruc:%s", got, companyRUC)
	}
	if got := readSaleTaxID(t, env, overrideSale.ConfirmationRef); !got.is("ruc", companyRUC) {
		t.Fatalf("sale tax id = %s, want ruc:%s", got, companyRUC)
	}

	// THE EXCEPTION IS NOW THE RULE, which is worth pinning because it is the
	// consequence of ADR 0054 people will be surprised by. There is no "buying for
	// a friend" on this route: a body naming somebody else's address, name and Tax
	// ID is still Ana buying, so what it types is still Ana speaking about herself
	// and still lands on her. She cannot address a Sale elsewhere; she can only
	// mislabel her own.
	forFriend := beginCheckoutSmugglingOK(t, env, "test-org", "override-fest", token,
		taxIDCheckoutBody("bob@example.com", "Bob", "Ng", "cedula", otherCedula, line))
	friendSale := confirmCheckoutOK(t, env, forFriend.ClientTransactionID, "approved")
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", otherCedula) {
		t.Fatalf("customer tax id = %s, want the cedula the same buyer typed on her own checkout", got)
	}
	if got := readSaleTaxID(t, env, friendSale.ConfirmationRef); !got.is("cedula", otherCedula) {
		t.Fatalf("sale tax id = %s, want cedula:%s", got, otherCedula)
	}
	var bobs int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customers WHERE email = $1`, "bob@example.com").Scan(&bobs); err != nil {
		t.Fatalf("count bobs: %v", err)
	}
	if bobs != 0 {
		t.Fatal("a customer_email in the body minted a Customer; the route has no such field")
	}
}

// TestCustomerSessionPayloadCarriesTaxID pins the prefill contract: the session
// read reports the stored Tax ID, null before one exists and set once a sale has
// supplied it. It is the only place a Customer's own Tax ID is readable today.
func TestCustomerSessionPayloadCarriesTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Prefill Fest", "prefill-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	token := customerSignIn(t, env, "ana@example.com")

	before := readCustomerSession(t, env, token)
	if before.TaxIDType != nil || before.TaxIDNumber != nil {
		t.Fatalf("session tax id = %v/%v, want null before any sale", before.TaxIDType, before.TaxIDNumber)
	}

	begin := beginCheckoutAsOK(t, env, "test-org", "prefill-fest", token,
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "passport", lowercasePassprt, line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	after := readCustomerSession(t, env, token)
	if after.TaxIDType == nil || *after.TaxIDType != "passport" {
		t.Fatalf("session tax_id_type = %v, want passport", after.TaxIDType)
	}
	if after.TaxIDNumber == nil || *after.TaxIDNumber != "AB123456" {
		t.Fatalf("session tax_id_number = %v, want the normalised AB123456", after.TaxIDNumber)
	}
}

// TestImportedSaleCarriesNoTaxID pins the other side of ADR 0016: the `import`
// channel describes sales transacted elsewhere, where the ID may never have been
// collected, so a Tax ID is not required there and its absence must not clear a
// Customer's stored one.
func TestImportedSaleCarriesNoTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Legacy Fest", "legacy-fest", 1000, 10)

	begin := beginCheckoutOK(t, env, "test-org", "legacy-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	importedRef := importOneSale(t, env, sessionID, eventID, "tax-import", gaID, "ana@example.com", "Ana", "Lopez")

	if got := readSaleTaxID(t, env, importedRef); !got.isNone() {
		t.Fatalf("imported sale tax id = %s, want none", got)
	}
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", validCedula) {
		t.Fatalf("customer tax id after an import = %s, want the untouched cedula:%s", got, validCedula)
	}
}

// TestDeclinedCheckoutRecordsNoTaxID: a Payment that never becomes a sale never
// writes a Tax ID anywhere. The write-back is a property of a *recorded sale*,
// not of a checkout attempt.
func TestDeclinedCheckoutRecordsNoTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Declined Tax Fest", "declined-tax-fest", 1000, 10)

	begin := beginCheckoutOK(t, env, "test-org", "declined-tax-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	declined := confirmCheckoutOK(t, env, begin.ClientTransactionID, "declined")
	if declined.Status != "failed" {
		t.Fatalf("confirm status = %q, want failed", declined.Status)
	}

	// The Customer exists — she signed in to reach the dialog at all (ADR 0054) —
	// and carries NOTHING the declined checkout said about her. The write-back is
	// a property of a recorded sale, and the assertion moved from "no Customer" to
	// "no Tax ID" for that reason alone.
	if got := readCustomerTaxID(t, env, "ana@example.com"); got.Type != nil || got.Number != nil {
		t.Fatalf("customer tax id after a declined Payment = %s, want none written at all", got)
	}
	var sales int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_sales`).Scan(&sales); err != nil {
		t.Fatalf("count ticket sales: %v", err)
	}
	if sales != 0 {
		t.Fatalf("ticket sales after a declined Payment = %d, want 0", sales)
	}
}

// readCustomerSession reads the Customer Session payload — the prefill source
// for the checkout dialog.
func readCustomerSession(t *testing.T, env *testEnv, token string) customerSessionView {
	t.Helper()
	resp, body := env.get(t, customerSessionPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer session status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view customerSessionView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode customer session: %v", err)
	}
	return view
}
