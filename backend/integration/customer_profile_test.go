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
//
// The phone number (#107, parent #103) is the second value the profile holds
// that a checkout may write, and it is covered at the end of this file rather
// than beside the checkout tests because the question each case asks is "what
// does the Customer's profile now say" — the same question the Tax ID write-back
// cases above ask. Its guard is the Tax ID's guard, clause for clause, so the
// cases mirror those in checkout_tax_id_test.go deliberately: seed a blank,
// refresh while unverified, refuse an anonymous overwrite of a Verified
// Customer, allow the person's own session to override, and never blank a stored
// value with an absent one.

const customerProfilePath = "/api/v1/customer/profile"

// customerProfileView is the "My info" payload: the Customer's own record as
// they may see and edit it. The email is here to be shown, never to be written.
type customerProfileView struct {
	Email       string  `json:"email"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	TaxIDType   *string `json:"tax_id_type"`
	TaxIDNumber *string `json:"tax_id_number"`
	// The stored phone in canonical E.164 form, null when the Customer has none
	// (#108). The Storefront splits it for display; the contract never does.
	Phone *string `json:"phone"`
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

// phoneProfileBody is an update body that talks about the phone. profileBody
// above deliberately omits the key entirely, because "says nothing about the
// phone" is itself a case under test (#108) and is what every pre-existing test
// in this file sends.
//
// The phone is `any` so a test can spell all three things the wire allows: a
// number, an empty string, and JSON null — the last two both meaning "remove it".
func phoneProfileBody(firstName, lastName string, phone any) map[string]any {
	body := profileBody(firstName, lastName, nil, nil)
	body["phone"] = phone
	return body
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

// TestCheckoutPhoneLandsOnTheCustomerProfile is the feature in one test: a
// buyer types their number on the Storefront's checkout dialog, disappears to
// the Payment Provider's hosted card form, comes back, and the number is on
// their profile — so the next purchase prefills it (#107).
//
// It lands at COMMIT and not before, and the assertion on the pending Payment
// says so: there is no Customer at all until the sale is recorded, because a
// begun checkout is an intention and an abandoned one must leave nothing behind
// (ADR 0013). The number therefore has to survive the redirect on the Payment,
// which is the whole reason it is snapshotted there (#106).
//
// The number is stored canonical, not as typed. The buyer here writes theirs the
// way a person writes a phone number down, and what reaches the profile is the
// single E.164 form PayPhone's `phoneNumber` prefill wants (#105).
func TestCheckoutPhoneLandsOnTheCustomerProfile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Dial Fest", "dial-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	begin := beginCheckoutOK(t, env, "test-org", "dial-fest",
		phoneCheckoutBody("ana@example.com", ecuadorMobileTyped, line))

	// Begun, not committed: nothing about this buyer exists yet.
	if n := customerCountByEmail(t, env, "ana@example.com"); n != 0 {
		t.Fatalf("customers before the confirm = %d, want 0 — a begun checkout writes no Customer", n)
	}

	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("customer phone = %s, want the canonical %s", phoneString(got), ecuadorMobile)
	}
}

// TestGuestCheckoutSeedsPhoneOfCustomerWithNone is the permissive half of the
// guard, on the record that most needs it: someone who signed in before ever
// buying holds a Verified record with no phone in it, and a checkout under their
// address — anonymous, no session token — is allowed to supply one rather than
// leaving the field permanently blank.
//
// This is exactly the Tax ID's "fill a never-set value" arm (ADR 0016). The
// guard exists to protect what a person put there, and there is nothing to
// protect yet.
func TestGuestCheckoutSeedsPhoneOfCustomerWithNone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Seed Fest", "seed-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	customerSignIn(t, env, "ana@example.com")
	if got := readCustomerPhone(t, env, "ana@example.com"); got != nil {
		t.Fatalf("phone after sign-in = %s, want none", phoneString(got))
	}

	begin := beginCheckoutOK(t, env, "test-org", "seed-fest",
		phoneCheckoutBody("ana@example.com", ecuadorMobile, line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("customer phone = %s, want the seeded %s", phoneString(got), ecuadorMobile)
	}
}

// TestUnverifiedCustomerPhoneRefreshedByAnyCheckout is the "refresh" arm: a
// record assembled on somebody's behalf by their own purchases self-corrects
// until they claim it by signing in. Nobody has proven ownership of this address
// yet, so there is no assertion to defend — only the most recent thing typed.
func TestUnverifiedCustomerPhoneRefreshedByAnyCheckout(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Renumber Fest", "renumber-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	first := beginCheckoutOK(t, env, "test-org", "renumber-fest",
		phoneCheckoutBody("ana@example.com", ecuadorMobile, line))
	confirmCheckoutOK(t, env, first.ClientTransactionID, "approved")

	// She buys again from abroad, on a different number.
	second := beginCheckoutOK(t, env, "test-org", "renumber-fest",
		phoneCheckoutBody("ana@example.com", foreignMobile, line))
	confirmCheckoutOK(t, env, second.ClientTransactionID, "approved")

	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != foreignMobile {
		t.Fatalf("customer phone = %s, want the refreshed %s", phoneString(got), foreignMobile)
	}
}

// TestGuestCheckoutNeverOverwritesVerifiedPhone is the adversarial case the
// guard exists for, and it is the same attack the Tax ID's guard turns away:
// anyone can type a known email address into a guest checkout. A phone number is
// if anything more dangerous to leave open, because it is the kind of detail a
// support agent later reads back as an identity check — so an unproven visitor
// must not be able to make one appear on somebody else's profile.
//
// The sale itself is untouched by any of this: it records what was transacted,
// and it never recorded a phone in the first place.
func TestGuestCheckoutNeverOverwritesVerifiedPhone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Guarded Dial Fest", "guarded-dial-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	// She buys once, supplying her number, then claims the record by signing in.
	own := beginCheckoutOK(t, env, "test-org", "guarded-dial-fest",
		phoneCheckoutBody("ana@example.com", ecuadorMobile, line))
	confirmCheckoutOK(t, env, own.ClientTransactionID, "approved")
	customerSignIn(t, env, "ana@example.com")

	// A stranger checks out under her address with a number of their choosing.
	stranger := beginCheckoutOK(t, env, "test-org", "guarded-dial-fest",
		phoneCheckoutBody("ana@example.com", foreignMobile, line))
	strangerSale := confirmCheckoutOK(t, env, stranger.ClientTransactionID, "approved")

	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("customer phone = %s, want the person-owned %s", phoneString(got), ecuadorMobile)
	}
	// The stranger's checkout was not rejected — only their write-back was. The
	// number they typed is on their own Payment, where the per-attempt record
	// belongs, and nowhere near her profile.
	if got := readPaymentPhone(t, env, stranger.ClientTransactionID); got == nil || *got != foreignMobile {
		t.Fatalf("stranger's payment phone = %s, want the typed %s", phoneString(got), foreignMobile)
	}
	if strangerSale.ConfirmationRef == "" {
		t.Fatal("stranger's sale has no confirmation ref, want a recorded sale")
	}
}

// TestSessionCheckoutOverwritesStoredPhone is the exception the whole flag exists
// for: the overwrite refused above is allowed when the checkout ran under the
// Customer's own full Customer Session, because then it is the person correcting
// themselves. The session proves ownership of the email address, which is
// precisely what the guest checkout above could not.
//
// The flag it reads is customer_session_authorized, snapshotted on the Payment
// and already carried as platform.SaleTaxID.SelfAsserted. It describes the
// checkout rather than the Tax ID, which is why the phone is entitled to it.
func TestSessionCheckoutOverwritesStoredPhone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Own Dial Fest", "own-dial-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	first := beginCheckoutOK(t, env, "test-org", "own-dial-fest",
		phoneCheckoutBody("ana@example.com", ecuadorMobile, line))
	confirmCheckoutOK(t, env, first.ClientTransactionID, "approved")
	token := customerSignIn(t, env, "ana@example.com")

	// Signed in, she changes her number at checkout — the prefilled value edited
	// by the person it belongs to.
	override := beginCheckoutAsOK(t, env, "test-org", "own-dial-fest", token,
		phoneCheckoutBody("ana@example.com", foreignMobile, line))
	confirmCheckoutOK(t, env, override.ClientTransactionID, "approved")

	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != foreignMobile {
		t.Fatalf("customer phone = %s, want the override %s", phoneString(got), foreignMobile)
	}
}

// TestCheckoutWithoutAPhoneNeverClearsAStoredOne pins the other half of
// "optional" (#103): skipping the field says nothing, and silence must never be
// read as "remove it". The Customer here is deliberately UNVERIFIED, so the
// guard's other arms are wide open — anything this checkout did supply would be
// written. Only the absence of a value stops the phone moving.
//
// This is what keeps the field from decaying: the staff-recorded sale and the
// Sale Import file never collect a phone at all, so a Customer whose next sale
// arrives through one of those channels would otherwise lose the number they
// gave online.
func TestCheckoutWithoutAPhoneNeverClearsAStoredOne(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Silent Fest", "silent-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	withPhone := beginCheckoutOK(t, env, "test-org", "silent-fest",
		phoneCheckoutBody("ana@example.com", ecuadorMobile, line))
	confirmCheckoutOK(t, env, withPhone.ClientTransactionID, "approved")

	// The very next checkout omits the field entirely, and changes the Tax ID so
	// the test proves this upsert did write — it simply wrote nothing to phone.
	silent := beginCheckoutOK(t, env, "test-org", "silent-fest",
		taxIDCheckoutBody("ana@example.com", "Ana", "Lopez", "ruc", companyRUC, line))
	confirmCheckoutOK(t, env, silent.ClientTransactionID, "approved")

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id = %s, want the refreshed ruc:%s — the upsert must have written", got, companyRUC)
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("customer phone = %s, want the untouched %s", phoneString(got), ecuadorMobile)
	}
}

// TestNoPhoneIsWrittenToTheTicketSale is a deliberate non-feature, asserted so
// it cannot be added by accident (#103, "Out of Scope"). The Tax ID is
// snapshotted onto the sale because ADR 0016 makes it a fiscal fact of that
// sale, frozen against later profile edits. A phone number carries no such
// requirement, and a column here would cascade into the staff sales list,
// receipts, the Sale Import format and the public contract for a value none of
// them consume.
//
// The schema is the assertion because that is where the decision lives: the
// per-attempt record on the Payment, checked alongside, is the durable trail if
// the question ever arises.
func TestNoPhoneIsWrittenToTheTicketSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Snapshot Dial Fest", "snapshot-dial-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	begin := beginCheckoutOK(t, env, "test-org", "snapshot-dial-fest",
		phoneCheckoutBody("ana@example.com", ecuadorMobile, line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	var columns int
	if err := env.db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'ticket_sales' AND column_name LIKE '%phone%'
	`).Scan(&columns); err != nil {
		t.Fatalf("read ticket_sales columns: %v", err)
	}
	if columns != 0 {
		t.Fatalf("ticket_sales has %d phone column(s), want none — the phone is no fiscal fact of a sale", columns)
	}

	// It reached the two places it belongs and no third.
	if got := readPaymentPhone(t, env, begin.ClientTransactionID); got == nil || *got != ecuadorMobile {
		t.Fatalf("payment phone = %s, want %s", phoneString(got), ecuadorMobile)
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("customer phone = %s, want %s", phoneString(got), ecuadorMobile)
	}
}

// TestCustomerProfileAndSessionCarryTheStoredPhone is the read half of #108: the
// number a checkout wrote is visible to the person it belongs to, on both the
// payloads a surface reads.
//
// Two payloads because they answer different questions and both need the phone.
// The profile view is "what does the platform hold about me", which is what "My
// info" renders; the session view is "who am I signed in as", which is what the
// checkout dialog prefills from. A number on one and not the other would mean a
// Customer could see their phone but never stop retyping it.
//
// The empty state is asserted first and deliberately: a Customer who has never
// given a number sees null rather than a blank string, so the Storefront can tell
// "none stored" from "stored as nothing" and show Ecuador with an empty field.
func TestCustomerProfileAndSessionCarryTheStoredPhone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Visible Fest", "visible-fest", 1000, 10)
	line := map[string]any{"ticket_type_id": gaID, "quantity": 1}

	token := customerSignIn(t, env, "ana@example.com")

	// Nothing given, nothing shown — on either payload.
	empty := updateProfileOK(t, env, token, profileBody("Ana", "Lopez", nil, nil))
	if empty.Phone != nil {
		t.Fatalf("profile phone = %s, want none for a Customer who has never given one", phoneString(empty.Phone))
	}
	if session := readCustomerSession(t, env, token); session.Phone != nil {
		t.Fatalf("session phone = %s, want none", phoneString(session.Phone))
	}

	// She buys under her own session, giving her number the way a person writes
	// one down, and both payloads report the canonical form of it.
	begin := beginCheckoutAsOK(t, env, "test-org", "visible-fest", token,
		phoneCheckoutBody("ana@example.com", ecuadorMobileTyped, line))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	session := readCustomerSession(t, env, token)
	if session.Phone == nil || *session.Phone != ecuadorMobile {
		t.Fatalf("session phone = %s, want the canonical %s", phoneString(session.Phone), ecuadorMobile)
	}
	// The profile view reports it too — and this edit says nothing about the
	// phone, so reading it back is also proof the number survived an unrelated
	// change of name.
	profile := updateProfileOK(t, env, token, profileBody("Ana", "Lopez Ruiz", nil, nil))
	if profile.Phone == nil || *profile.Phone != ecuadorMobile {
		t.Fatalf("profile phone = %s, want the stored %s", phoneString(profile.Phone), ecuadorMobile)
	}
}

// TestCustomerProfileSetsChangesAndClearsPhone walks the whole editable life of a
// phone number through the endpoint — set, change, clear — checking the session
// payload after each step, because the prefill the checkout dialog reads is the
// same fact and must follow.
//
// Clearing is the step that matters most. It is a capability the Customer is
// owed (#103, user story 10): a person may withdraw a detail they no longer wish
// stored, and "cleared" has to mean genuinely gone from the row rather than
// merely absent from a response. Both spellings of the clear are exercised,
// because the Storefront sends null and a bare-bones client may well send "".
func TestCustomerProfileSetsChangesAndClearsPhone(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	// Set: a Customer who has never bought online gives their number here, so the
	// very first checkout arrives prefilled.
	set := updateProfileOK(t, env, token, phoneProfileBody("Ana", "Lopez", ecuadorMobileTyped))
	if set.Phone == nil || *set.Phone != ecuadorMobile {
		t.Fatalf("profile phone = %s, want the canonical %s", phoneString(set.Phone), ecuadorMobile)
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("stored phone = %s, want %s", phoneString(got), ecuadorMobile)
	}
	if session := readCustomerSession(t, env, token); session.Phone == nil || *session.Phone != ecuadorMobile {
		t.Fatalf("session phone = %s, want %s", phoneString(session.Phone), ecuadorMobile)
	}

	// Change: she moves abroad and edits it, without a purchase in sight — the
	// point of having the field on this screen at all.
	changed := updateProfileOK(t, env, token, phoneProfileBody("Ana", "Lopez", foreignMobile))
	if changed.Phone == nil || *changed.Phone != foreignMobile {
		t.Fatalf("profile phone = %s, want the changed %s", phoneString(changed.Phone), foreignMobile)
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != foreignMobile {
		t.Fatalf("stored phone = %s, want the changed %s", phoneString(got), foreignMobile)
	}

	// Clear, spelled null: withdrawn, and gone from the row rather than blanked.
	cleared := updateProfileOK(t, env, token, phoneProfileBody("Ana", "Lopez", nil))
	if cleared.Phone != nil {
		t.Fatalf("profile phone = %s, want none after clearing", phoneString(cleared.Phone))
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got != nil {
		t.Fatalf("stored phone = %s, want none after clearing", phoneString(got))
	}
	if session := readCustomerSession(t, env, token); session.Phone != nil {
		t.Fatalf("session phone = %s, want nothing to prefill after clearing", phoneString(session.Phone))
	}

	// Clear, spelled as an empty field: the same thing, because an emptied input
	// is how a person expresses it and nothing else could sensibly be meant.
	updateProfileOK(t, env, token, phoneProfileBody("Ana", "Lopez", ecuadorMobile))
	blanked := updateProfileOK(t, env, token, phoneProfileBody("Ana", "Lopez", "  "))
	if blanked.Phone != nil {
		t.Fatalf("profile phone = %s, want none after an emptied field", phoneString(blanked.Phone))
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got != nil {
		t.Fatalf("stored phone = %s, want none after an emptied field", phoneString(got))
	}
}

// TestCustomerProfileRejectsInvalidPhone pins that this surface runs the same
// rule as checkout and blames the same field. The messages are
// platform.PhoneFieldErrors' own, so a number rejected here reads exactly as it
// would in the checkout dialog — one rule, one wording, two surfaces (#105).
//
// The Ecuadorian landline is the case worth naming: it is a perfectly real phone
// number and is refused anyway, because what the Payment Provider's form wants is
// a cardholder's mobile.
func TestCustomerProfileRejectsInvalidPhone(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")
	updateProfileOK(t, env, token, phoneProfileBody("Ana", "Lopez", ecuadorMobile))

	for _, tc := range []struct {
		name  string
		phone string
	}{
		{"an Ecuadorian landline", "+59322345678"},
		{"an Ecuadorian mobile a digit short", "+59398765432"},
		{"an Ecuadorian mobile a digit long", "+5939876543210"},
		{"no dialling code at all", "0987654321"},
		{"letters where digits belong", "+593abcdefghi"},
		{"far past E.164's ceiling", "+1202555012345678"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := updateProfile(t, env, token, phoneProfileBody("Ana", "Lopez", tc.phone))
			assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
			if got := fieldErrors(t, body)["phone"]; got == "" {
				t.Fatalf("field errors = %v, want one on phone", fieldErrors(t, body))
			}
		})
	}

	// A rejected edit writes nothing: the number she already had is still hers.
	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("stored phone = %s, want the untouched %s", phoneString(got), ecuadorMobile)
	}
}

// TestProfileUpdateWithoutThePhoneLeavesItAlone is the difference between the
// phone and the Tax ID on this endpoint, and it is deliberate (#108). Absent Tax
// ID halves clear the Tax ID, because they have been part of this contract since
// it was written and can only mean an emptied form. An absent phone means the
// request is not talking about the phone at all — a client written before the
// field existed, such as a Storefront still running the previous build through a
// deploy window, and no reason for a buyer to lose their number.
//
// Every other test in this file sends exactly such a body, so this pins what all
// of them quietly rely on.
func TestProfileUpdateWithoutThePhoneLeavesItAlone(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")
	updateProfileOK(t, env, token, phoneProfileBody("Ana", "Lopez", ecuadorMobile))

	// An edit that changes the name and asserts a Tax ID, and never mentions the
	// phone. The Tax ID landing is what proves this update wrote at all.
	updated := updateProfileOK(t, env, token,
		profileBody("Ana María", "Lopez", strptr("cedula"), strptr(validCedula)))
	if !updated.taxID().is("cedula", validCedula) {
		t.Fatalf("profile tax id = %s, want cedula:%s — the update must have written", updated.taxID(), validCedula)
	}
	if updated.Phone == nil || *updated.Phone != ecuadorMobile {
		t.Fatalf("profile phone = %s, want the untouched %s", phoneString(updated.Phone), ecuadorMobile)
	}
	if got := readCustomerPhone(t, env, "ana@example.com"); got == nil || *got != ecuadorMobile {
		t.Fatalf("stored phone = %s, want the untouched %s", phoneString(got), ecuadorMobile)
	}
}

// readCustomerPhone reads the Customer's stored phone number directly, nil when
// they have none. SQL rather than the profile contract, deliberately: these
// assertions run for Customers who are not the caller, and a test that checked
// only the payload could not tell "cleared" from "hidden".
func readCustomerPhone(t *testing.T, env *testEnv, email string) *string {
	t.Helper()
	var phone *string
	if err := env.db.QueryRow(`
		SELECT phone FROM customers WHERE email = $1
	`, email).Scan(&phone); err != nil {
		t.Fatalf("read customer phone %q: %v", email, err)
	}
	return phone
}

// phoneString renders a nullable phone for a failure message, so "<none>" and a
// stored number read alike in the diff — the same courtesy taxIDPair.String does
// for the Tax ID.
func phoneString(phone *string) string {
	if phone == nil {
		return "<none>"
	}
	return *phone
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
