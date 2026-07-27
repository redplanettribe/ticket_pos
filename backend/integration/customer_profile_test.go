package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Customer Area's "My info" profile (#102, ADR 0016): the one seam through
// which a Customer edits what the platform holds about them — their name and
// their current Tax ID assertion — without starting a checkout.
//
// Four properties are the ones worth attacking, and each is tested rather than
// demonstrated:
//
//   - Only a full Customer Session may write. A Confirmation Link session is
//     minted from a forwarded email, not from Proof of Email Ownership, so it
//     may read its one sale and edit nothing.
//   - A name can never be blanked. The customer-upsert guard reads "currently
//     blank" as "never set", and this endpoint is the only write path that could
//     falsify that.
//   - Email is not editable here. It is the Customer's identity (ADR 0010), and
//     the endpoint does not accept it under any spelling.
//   - Editing moves the Customer's current assertion and nothing else. Every
//     Ticket Sale keeps the snapshot it was transacted under.

const customerProfilePath = "/api/v1/customer/profile"

// customerProfileView is the "My info" payload: the Customer's own record as
// they may see and edit it. The email is here to be shown, never to be written.
type customerProfileView struct {
	Email       string  `json:"email"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	TaxIDType   *string `json:"tax_id_type"`
	TaxIDNumber *string `json:"tax_id_number"`
}

func (v customerProfileView) taxID() taxIDPair {
	return taxIDPair{Type: v.TaxIDType, Number: v.TaxIDNumber}
}

// taxID reads the session payload's Tax ID as the same pair, so "the profile
// says X" and "the prefill says X" are compared with one helper.
func (v customerSessionView) taxID() taxIDPair {
	return taxIDPair{Type: v.TaxIDType, Number: v.TaxIDNumber}
}

// profileBody builds an update body. The Tax ID halves are pointers so a test
// can send JSON null — which is how "clear it" is spelled on the wire.
func profileBody(firstName, lastName string, taxIDType, taxIDNumber *string) map[string]any {
	return map[string]any{
		"first_name":    firstName,
		"last_name":     lastName,
		"tax_id_type":   taxIDType,
		"tax_id_number": taxIDNumber,
	}
}

func strptr(s string) *string { return &s }

func updateProfile(t *testing.T, env *testEnv, token string, body any) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	return env.patch(t, customerProfilePath, body, headers)
}

func updateProfileOK(t *testing.T, env *testEnv, token string, body any) customerProfileView {
	t.Helper()
	resp, envBody := updateProfile(t, env, token, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update profile status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	if envBody.Error != nil {
		t.Fatalf("update profile error=%+v, want none", envBody.Error)
	}
	var view customerProfileView
	if err := json.Unmarshal(envBody.Data, &view); err != nil {
		t.Fatalf("decode profile view: %v", err)
	}
	return view
}

// TestCustomerProfileRequiresACustomerSession: the profile is reached through
// the session and through nothing else. No token, a stale token, and a Staff
// Session token all authenticate exactly nothing here (ADR 0010).
func TestCustomerProfileRequiresACustomerSession(t *testing.T) {
	env := setupTest(t)
	staffSession := orgAdminSession(t, env)

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"a token that names nothing", "deadbeef"},
		{"a Staff Session token", staffSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := updateProfile(t, env, tc.token, profileBody("Ana", "Lopez", nil, nil))
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (error=%+v)", resp.StatusCode, body.Error)
			}
			if body.Error == nil {
				t.Fatal("expected an error envelope")
			}
		})
	}
}

// TestConfirmationLinkSessionCannotEditTheProfile is the narrowing that matters
// most here. A Confirmation Link is possession of a forwarded email, and the
// session it mints exists to show one Ticket Sale. Whoever holds it must not be
// able to rewrite the buyer's name or Tax ID.
func TestConfirmationLinkSessionCannotEditTheProfile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Linked Fest", "linked-fest",
		env.fixedClock.Add(30*24*time.Hour), "profile-link", "ana@example.com", "Ana", "Lopez")

	_, linkSession := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")

	resp, body := updateProfile(t, env, linkSession,
		profileBody("Mallory", "Ng", strptr("cedula"), strptr(otherCedula)))
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	// Nothing moved: the name the sale recorded is still the Customer's.
	if got := readCustomerName(t, env, "ana@example.com"); got != "Ana Lopez" {
		t.Fatalf("customer name = %q, want the untouched %q", got, "Ana Lopez")
	}

	// And the same person signing in properly can edit, so the refusal above is
	// the link session's narrowness rather than the endpoint being shut.
	full := customerSignIn(t, env, "ana@example.com")
	updateProfileOK(t, env, full, profileBody("Ana", "Lopez Ruiz", nil, nil))
	if got := readCustomerName(t, env, "ana@example.com"); got != "Ana Lopez Ruiz" {
		t.Fatalf("customer name after a full-session edit = %q, want %q", got, "Ana Lopez Ruiz")
	}
}

// TestCustomerProfileRejectsBlankName preserves the customer-upsert guard's
// precondition. That guard treats "currently blank" as "never set" so a first
// sale can name a Customer who signed in before ever buying; if this endpoint
// could blank a name, a later sale would silently overwrite one the person had
// chosen.
func TestCustomerProfileRejectsBlankName(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")
	updateProfileOK(t, env, token, profileBody("Ana", "Lopez", nil, nil))

	for _, tc := range []struct {
		name      string
		firstName string
		lastName  string
		wantField string
	}{
		{"empty first name", "", "Lopez", "first_name"},
		{"empty last name", "Ana", "", "last_name"},
		{"whitespace-only first name", "   ", "Lopez", "first_name"},
		{"whitespace-only last name", "Ana", "\t", "last_name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := updateProfile(t, env, token, profileBody(tc.firstName, tc.lastName, nil, nil))
			assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
			if got := fieldErrors(t, body)[tc.wantField]; got == "" {
				t.Fatalf("field errors = %v, want one on %s", fieldErrors(t, body), tc.wantField)
			}
		})
	}

	// The stored name survived every attempt.
	if got := readCustomerName(t, env, "ana@example.com"); got != "Ana Lopez" {
		t.Fatalf("customer name = %q, want the untouched %q", got, "Ana Lopez")
	}
}

// TestCustomerProfileRejectsInvalidTaxID pins the same gate the checkout has,
// on the same validator, with the blame on the half of the pair at fault
// (ADR 0016).
func TestCustomerProfileRejectsInvalidTaxID(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	for _, tc := range []struct {
		name        string
		taxIDType   *string
		taxIDNumber *string
		wantField   string
	}{
		{"cedula failing its check digit", strptr("cedula"), strptr(invalidCedula), "tax_id_number"},
		{"a RUC under the cedula type", strptr("cedula"), strptr(naturalRUC), "tax_id_number"},
		{"unknown type", strptr("dni"), strptr(validCedula), "tax_id_type"},
		{"a number with no type", nil, strptr(validCedula), "tax_id_type"},
		{"a type with no number", strptr("cedula"), nil, "tax_id_number"},
		{"a type with a blank number", strptr("cedula"), strptr("  "), "tax_id_number"},
		{"passport too short", strptr("passport"), strptr("AB12"), "tax_id_number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := updateProfile(t, env, token, profileBody("Ana", "Lopez", tc.taxIDType, tc.taxIDNumber))
			assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
			fields := fieldErrors(t, body)
			if fields[tc.wantField] == "" {
				t.Fatalf("field errors = %v, want one on %s", fields, tc.wantField)
			}
		})
	}

	// A rejected edit writes nothing at all — not even the valid name it carried.
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.isNone() {
		t.Fatalf("customer tax id = %s, want none after only rejected edits", got)
	}
	if got := readCustomerName(t, env, "ana@example.com"); got != " " {
		t.Fatalf("customer name = %q, want the sign-in record's blank name", got)
	}
}

// TestCustomerProfileSetsChangesAndClearsTaxID walks the whole editable life of
// a Tax ID through the endpoint, checking the session payload after each step:
// the prefill the checkout dialog reads is the same fact, so it must follow.
func TestCustomerProfileSetsChangesAndClearsTaxID(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	// Set: a Customer who signed in before ever buying names themselves.
	set := updateProfileOK(t, env, token, profileBody("Ana", "Lopez", strptr("cedula"), strptr(validCedula)))
	if !set.taxID().is("cedula", validCedula) {
		t.Fatalf("profile tax id = %s, want cedula:%s", set.taxID(), validCedula)
	}
	if session := readCustomerSession(t, env, token); !session.taxID().is("cedula", validCedula) {
		t.Fatalf("session tax id = %s, want cedula:%s", session.taxID(), validCedula)
	}

	// Change: the buyer switches to their company RUC, and the passport case
	// proves the shared normalisation applies here too.
	changed := updateProfileOK(t, env, token, profileBody("Ana", "Lopez", strptr("ruc"), strptr(companyRUC)))
	if !changed.taxID().is("ruc", companyRUC) {
		t.Fatalf("profile tax id = %s, want ruc:%s", changed.taxID(), companyRUC)
	}
	normalised := updateProfileOK(t, env, token, profileBody("Ana", "Lopez", strptr("passport"), strptr(lowercasePassprt)))
	if !normalised.taxID().is("passport", "AB123456") {
		t.Fatalf("profile tax id = %s, want the normalised passport:AB123456", normalised.taxID())
	}

	// Clear: both halves go together, and the session prefills nothing again.
	cleared := updateProfileOK(t, env, token, profileBody("Ana", "Lopez", nil, nil))
	if !cleared.taxID().isNone() {
		t.Fatalf("profile tax id = %s, want none after clearing", cleared.taxID())
	}
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.isNone() {
		t.Fatalf("stored tax id = %s, want none after clearing", got)
	}
	session := readCustomerSession(t, env, token)
	if session.TaxIDType != nil || session.TaxIDNumber != nil {
		t.Fatalf("session tax id = %v/%v, want null after clearing", session.TaxIDType, session.TaxIDNumber)
	}

	// The name rode along untouched by all of that.
	if got := readCustomerName(t, env, "ana@example.com"); got != "Ana Lopez" {
		t.Fatalf("customer name = %q, want %q", got, "Ana Lopez")
	}
}

// TestCustomerProfileCannotChangeEmail: the email is the Customer's identity, so
// the endpoint does not read it. A body naming a different address updates the
// name it was sent with and leaves the address exactly where it was — and mints
// no second Customer.
func TestCustomerProfileCannotChangeEmail(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	view := updateProfileOK(t, env, token, map[string]any{
		"email":         "attacker@example.com",
		"first_name":    "Ana",
		"last_name":     "Lopez",
		"tax_id_type":   nil,
		"tax_id_number": nil,
	})
	if view.Email != "ana@example.com" {
		t.Fatalf("profile email = %q, want the unchanged ana@example.com", view.Email)
	}
	if session := readCustomerSession(t, env, token); session.Email != "ana@example.com" {
		t.Fatalf("session email = %q, want the unchanged ana@example.com", session.Email)
	}
	if n := customerCountByEmail(t, env, "attacker@example.com"); n != 0 {
		t.Fatalf("customers for the named address = %d, want 0", n)
	}
}

// TestProfileEditNeverMovesATicketSaleSnapshot is the immutability half of
// ADR 0016 read from the profile side: a Customer edits their assertion, and
// every sale still shows what it was transacted under. The next checkout
// prefills the new value, so the drift between the two is deliberate.
func TestProfileEditNeverMovesATicketSaleSnapshot(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Snapshot Fest", "snapshot-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	begin := beginCheckoutOK(t, env, "test-org", "snapshot-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "cedula", validCedula, line))
	sale := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	token := customerSignIn(t, env, "ana@example.com")
	updateProfileOK(t, env, token, profileBody("Ana María", "Lopez", strptr("ruc"), strptr(companyRUC)))

	if got := readSaleTaxID(t, env, sale.ConfirmationRef); !got.is("cedula", validCedula) {
		t.Fatalf("sale tax id after a profile edit = %s, want the snapshot cedula:%s", got, validCedula)
	}
	if got := readSaleCustomerName(t, env, sale.ConfirmationRef); got != "Ana Lopez" {
		t.Fatalf("sale customer name after a profile edit = %q, want the snapshot %q", got, "Ana Lopez")
	}
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id = %s, want the edited ruc:%s", got, companyRUC)
	}

	// The prefill follows the assertion, not the snapshot.
	session := readCustomerSession(t, env, token)
	if !session.taxID().is("ruc", companyRUC) {
		t.Fatalf("session tax id = %s, want the edited ruc:%s", session.taxID(), companyRUC)
	}
	if session.FirstName != "Ana María" || session.LastName != "Lopez" {
		t.Fatalf("session name = %q %q, want the edited Ana María Lopez", session.FirstName, session.LastName)
	}

	// A later sale carries the new assertion and leaves the old sale alone.
	next := beginCheckoutAsOK(t, env, "test-org", "snapshot-fest", token,
		taxIDCheckoutBody("ana@example.com", "Ana María", "Lopez", "ruc", companyRUC, line))
	nextSale := confirmCheckoutOK(t, env, next.ClientTransactionID, "approved")
	if got := readSaleTaxID(t, env, nextSale.ConfirmationRef); !got.is("ruc", companyRUC) {
		t.Fatalf("later sale tax id = %s, want ruc:%s", got, companyRUC)
	}
	if got := readSaleTaxID(t, env, sale.ConfirmationRef); !got.is("cedula", validCedula) {
		t.Fatalf("first sale tax id = %s, want the untouched cedula:%s", got, validCedula)
	}
}

// TestClearedTaxIDIsRefilledByALaterSale is the accepted consequence of allowing
// a clear (#102): nothing is prefilled afterwards, and the very next sale may
// fill the blank under the never-set arm of the write-back rules.
func TestClearedTaxIDIsRefilledByALaterSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Refill Fest", "refill-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	token := customerSignIn(t, env, "ana@example.com")
	updateProfileOK(t, env, token, profileBody("Ana", "Lopez", strptr("cedula"), strptr(validCedula)))
	updateProfileOK(t, env, token, profileBody("Ana", "Lopez", nil, nil))

	if session := readCustomerSession(t, env, token); session.TaxIDType != nil {
		t.Fatalf("session tax id = %v, want nothing to prefill after clearing", session.TaxIDType)
	}

	begin := beginCheckoutAsOK(t, env, "test-org", "refill-fest", token,
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "passport", lowercasePassprt, line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("passport", "AB123456") {
		t.Fatalf("customer tax id = %s, want the refilled passport:AB123456", got)
	}
}

// TestProfileNameSurvivesALaterSale closes the loop the blank-name rejection
// opens: a name set through the editor belongs to a Verified Customer, so the
// upsert's guard protects it exactly as one set at checkout.
func TestProfileNameSurvivesALaterSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Name Fest", "name-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	token := customerSignIn(t, env, "ana@example.com")
	updateProfileOK(t, env, token, profileBody("Ana", "Lopez", nil, nil))

	// A sale bought for her by someone typing a different spelling.
	begin := beginCheckoutOK(t, env, "test-org", "name-fest",
		taxIDCheckoutBody("ana@example.com", "ANITA", "LOPEZ", "cedula", validCedula, line))
	sale := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	if got := readCustomerName(t, env, "ana@example.com"); got != "Ana Lopez" {
		t.Fatalf("customer name after a later sale = %q, want the profile's %q", got, "Ana Lopez")
	}
	if got := readSaleCustomerName(t, env, sale.ConfirmationRef); got != "ANITA LOPEZ" {
		t.Fatalf("sale customer name = %q, want what was transacted", got)
	}
}

// readCustomerName reads the Customer's stored name as "First Last". SQL because
// the Customer record has no read endpoint beyond the session payload, and these
// assertions run for Customers who are not the caller.
func readCustomerName(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	var first, last string
	if err := env.db.QueryRow(`
		SELECT first_name, last_name FROM customers WHERE email = $1
	`, email).Scan(&first, &last); err != nil {
		t.Fatalf("read customer name %q: %v", email, err)
	}
	return first + " " + last
}

// readSaleCustomerName reads one Ticket Sale's immutable recorded name.
func readSaleCustomerName(t *testing.T, env *testEnv, ref string) string {
	t.Helper()
	var first, last string
	if err := env.db.QueryRow(`
		SELECT customer_first_name, customer_last_name
		FROM ticket_sales WHERE confirmation_ref = $1
	`, ref).Scan(&first, &last); err != nil {
		t.Fatalf("read sale customer name %q: %v", ref, err)
	}
	return first + " " + last
}
