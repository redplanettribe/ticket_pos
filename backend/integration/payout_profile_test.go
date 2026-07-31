package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The Payout Profile (issue #173, ADR 0025): where an Organization is paid.
// The bank, whether the account is `ahorros` or `corriente`, the account
// number, the name on the account, and the Organization's own Tax ID for the
// factura — set by an Org Admin and by nobody else.
//
// These tests are the whole contract of the surface. There is deliberately no
// repository- or service-level test beside them: the rules being pinned here
// are the ones a person can break by typing, so the seam that has to hold is
// the HTTP one, with a real Postgres underneath enforcing the CHECK constraints
// the migration writes.

const payoutProfilePath = "/api/v1/staff/organization/payout-profile"

type payoutProfile struct {
	BankName          string `json:"bank_name"`
	AccountType       string `json:"account_type"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
	TaxIDType         string `json:"tax_id_type"`
	TaxIDNumber       string `json:"tax_id_number"`
	UpdatedAt         string `json:"updated_at"`
}

// completeProfile is a valid Payout Profile body: the shape every rejection
// test below mutates exactly one field of, so a failure names the field the
// case was about rather than whichever one happened to be wrong first.
func completeProfile() map[string]any {
	return map[string]any{
		"bank_name":           "Banco Pichincha",
		"account_type":        "ahorros",
		"account_number":      "2201234821",
		"account_holder_name": "Fundación Ritmo",
		"tax_id_type":         "cedula",
		"tax_id_number":       validCedula,
	}
}

func putPayoutProfile(t *testing.T, env *testEnv, sessionID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.put(t, payoutProfilePath, body, authHeader(sessionID))
}

// getPayoutProfileOK reads the profile back and insists on a 200. The second
// return says whether a profile exists at all: an Organization that has never
// recorded one reads back a null payload, which is an ordinary answer and not
// an error.
func getPayoutProfileOK(t *testing.T, env *testEnv, sessionID string) (payoutProfile, bool) {
	t.Helper()
	resp, body := env.get(t, payoutProfilePath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get payout profile status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if len(body.Data) == 0 || string(body.Data) == "null" {
		return payoutProfile{}, false
	}
	var profile payoutProfile
	if err := json.Unmarshal(body.Data, &profile); err != nil {
		t.Fatalf("decode payout profile: %v", err)
	}
	return profile, true
}

// TestPayoutProfileSavesAndReadsBackUnchanged walks the ordinary life of the
// record: nothing at first, then a saved profile that reads back exactly as it
// was typed, then a correction that replaces it rather than adding a second.
func TestPayoutProfileSavesAndReadsBackUnchanged(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A new Organization has no Payout Profile, and being asked for one is not
	// an error — it is the state every Organization starts in.
	if _, exists := getPayoutProfileOK(t, env, sessionID); exists {
		t.Fatalf("a brand-new Organization already has a Payout Profile")
	}

	resp, body := putPayoutProfile(t, env, sessionID, completeProfile())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d error=%+v", resp.StatusCode, body.Error)
	}

	saved, exists := getPayoutProfileOK(t, env, sessionID)
	if !exists {
		t.Fatalf("saved profile reads back as absent")
	}
	want := completeProfile()
	if saved.BankName != want["bank_name"] || saved.AccountType != want["account_type"] ||
		saved.AccountNumber != want["account_number"] || saved.AccountHolderName != want["account_holder_name"] ||
		saved.TaxIDType != want["tax_id_type"] || saved.TaxIDNumber != want["tax_id_number"] {
		t.Fatalf("profile read back = %+v; want %+v", saved, want)
	}
	if saved.UpdatedAt == "" {
		t.Fatalf("profile carries no updated_at: %+v", saved)
	}

	// Changing banks replaces the profile; it never accumulates a second one.
	changed := completeProfile()
	changed["bank_name"] = "Cooperativa JEP"
	changed["account_type"] = "corriente"
	changed["account_number"] = "0004821"
	changed["tax_id_type"] = "ruc"
	changed["tax_id_number"] = naturalRUC
	if resp, body := putPayoutProfile(t, env, sessionID, changed); resp.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d error=%+v", resp.StatusCode, body.Error)
	}

	updated, _ := getPayoutProfileOK(t, env, sessionID)
	if updated.BankName != "Cooperativa JEP" || updated.AccountType != "corriente" ||
		updated.TaxIDType != "ruc" || updated.TaxIDNumber != naturalRUC {
		t.Fatalf("updated profile = %+v; want the corrected details", updated)
	}

	var rows int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM organization_payout_profiles`).Scan(&rows); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	if rows != 1 {
		t.Fatalf("profile rows = %d; want exactly one per Organization", rows)
	}
}

// TestPayoutProfileKeepsLeadingZeros is the reason the account number is TEXT
// and not a number. An account number that loses a leading zero reaches the
// wrong account, so the round trip has to be byte-for-byte.
func TestPayoutProfileKeepsLeadingZeros(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	body := completeProfile()
	body["account_number"] = "0004821"
	if resp, envBody := putPayoutProfile(t, env, sessionID, body); resp.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d error=%+v", resp.StatusCode, envBody.Error)
	}

	saved, _ := getPayoutProfileOK(t, env, sessionID)
	if saved.AccountNumber != "0004821" {
		t.Fatalf("account number = %q; want the leading zeros intact", saved.AccountNumber)
	}

	// And the column itself holds the string, not an integer that lost them.
	var stored string
	if err := env.db.QueryRow(`SELECT account_number FROM organization_payout_profiles`).Scan(&stored); err != nil {
		t.Fatalf("read stored account number: %v", err)
	}
	if stored != "0004821" {
		t.Fatalf("stored account number = %q; want %q", stored, "0004821")
	}
}

// TestPayoutProfileNormalisesTheAccountNumber: an organizer copying an account
// number off a bank statement brings its grouping with it. The separators are
// stripped on the way in, so what is stored is what gets typed into a banking
// app.
func TestPayoutProfileNormalisesTheAccountNumber(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	body := completeProfile()
	body["account_number"] = " 0022-0123 4821 "
	if resp, envBody := putPayoutProfile(t, env, sessionID, body); resp.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d error=%+v", resp.StatusCode, envBody.Error)
	}

	saved, _ := getPayoutProfileOK(t, env, sessionID)
	if saved.AccountNumber != "002201234821" {
		t.Fatalf("account number = %q; want the separators stripped and the zeros kept", saved.AccountNumber)
	}
}

// TestPayoutProfileRejectsBadFields pins the field-level refusals. Every case
// names the field the organizer must go and fix, and carries the stable code a
// client keys its own copy on.
func TestPayoutProfileRejectsBadFields(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	longName := strings.Repeat("a", 200)

	cases := []struct {
		name      string
		field     string
		value     any
		omit      bool
		wantField string
		wantCode  string
	}{
		{name: "blank bank", field: "bank_name", value: "   ", wantField: "bank_name", wantCode: "REQUIRED"},
		{name: "missing bank", field: "bank_name", omit: true, wantField: "bank_name", wantCode: "REQUIRED"},
		{name: "overlong bank", field: "bank_name", value: longName, wantField: "bank_name", wantCode: "TOO_LONG"},
		{name: "blank holder", field: "account_holder_name", value: "", wantField: "account_holder_name", wantCode: "REQUIRED"},
		{name: "overlong holder", field: "account_holder_name", value: longName, wantField: "account_holder_name", wantCode: "TOO_LONG"},
		{name: "blank account type", field: "account_type", value: "", wantField: "account_type", wantCode: "REQUIRED"},
		{name: "unknown account type", field: "account_type", value: "savings", wantField: "account_type", wantCode: "INVALID_ACCOUNT_TYPE"},
		{name: "blank account number", field: "account_number", value: "", wantField: "account_number", wantCode: "REQUIRED"},
		{name: "separators only", field: "account_number", value: " - ", wantField: "account_number", wantCode: "REQUIRED"},
		{name: "lettered account number", field: "account_number", value: "22012A4821", wantField: "account_number", wantCode: "INVALID_ACCOUNT_NUMBER"},
		{name: "overlong account number", field: "account_number", value: strings.Repeat("7", 40), wantField: "account_number", wantCode: "TOO_LONG"},
		{name: "blank tax id type", field: "tax_id_type", value: "", wantField: "tax_id_type", wantCode: "INVALID_TAX_ID_TYPE"},
		{name: "unknown tax id type", field: "tax_id_type", value: "dni", wantField: "tax_id_type", wantCode: "INVALID_TAX_ID_TYPE"},
		{name: "bad cedula check digit", field: "tax_id_number", value: invalidCedula, wantField: "tax_id_number", wantCode: "INVALID_CEDULA"},
		{name: "cedula of the wrong length", field: "tax_id_number", value: "17123456", wantField: "tax_id_number", wantCode: "INVALID_CEDULA"},
		{name: "blank tax id number", field: "tax_id_number", value: "", wantField: "tax_id_number", wantCode: "INVALID_CEDULA"},
	}

	for _, tc := range cases {
		body := completeProfile()
		if tc.omit {
			delete(body, tc.field)
		} else {
			body[tc.field] = tc.value
		}

		resp, envBody := putPayoutProfile(t, env, sessionID, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", tc.name, resp.StatusCode)
		}
		fields := fieldErrorsByName(t, envBody)
		got, ok := fields[tc.wantField]
		if !ok {
			t.Fatalf("%s: fields=%+v, want an error on %s", tc.name, fields, tc.wantField)
		}
		if got.Code != tc.wantCode {
			t.Fatalf("%s: code on %s = %q; want %q", tc.name, tc.wantField, got.Code, tc.wantCode)
		}
		// Bank details never travel back out in an error (ADR 0025): the
		// message says what the rule is, never what was typed.
		if submitted, isString := tc.value.(string); isString && len(strings.TrimSpace(submitted)) > 2 {
			if strings.Contains(got.Message, strings.TrimSpace(submitted)) {
				t.Fatalf("%s: message %q echoes the submitted value", tc.name, got.Message)
			}
		}
	}

	// Every one of those was total: nothing was written.
	var rows int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM organization_payout_profiles`).Scan(&rows); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	if rows != 0 {
		t.Fatalf("profile rows = %d; want none after only rejections", rows)
	}
}

// TestPayoutProfileTaxIDIsNeverAPassport: the profile's Tax ID identifies the
// party holding the Ecuadorian bank account being wired to, and a passport
// holder has none (ADR 0025). The buyer's Tax ID at checkout answers a
// different question and still takes all three types — this test asserts both
// halves, because narrowing one surface must not narrow the other (ADR 0016).
func TestPayoutProfileTaxIDIsNeverAPassport(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	body := completeProfile()
	body["tax_id_type"] = "passport"
	body["tax_id_number"] = "AB123456"

	resp, envBody := putPayoutProfile(t, env, sessionID, body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("passport profile status=%d, want 400", resp.StatusCode)
	}
	fields := fieldErrorsByName(t, envBody)
	if got := fields["tax_id_type"]; got.Code != "INVALID_TAX_ID_TYPE" {
		t.Fatalf("passport rejection = %+v; want INVALID_TAX_ID_TYPE on tax_id_type", fields)
	}

	// The same passport is still an ordinary buyer identification at checkout.
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Passport Fest", "passport-fest", 1000, 10)
	checkout := taxIDCheckoutBody("tourist@example.com", "Ann", "Traveller", "passport", "AB123456",
		map[string]any{"ticket_type_id": gaID, "quantity": 1})
	if resp, envBody := beginCheckout(t, env, "test-org", "passport-fest", checkout); resp.StatusCode != http.StatusCreated {
		t.Fatalf("passport checkout status=%d error=%+v; ADR 0016 is untouched", resp.StatusCode, envBody.Error)
	}
}

// TestPayoutProfileIsOrgAdminOnly: where the Organization's money goes is not
// hired staff's business, and it is not a stranger's either.
func TestPayoutProfileIsOrgAdminOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	if resp, body := putPayoutProfile(t, env, sessionID, completeProfile()); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin save status=%d error=%+v", resp.StatusCode, body.Error)
	}

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	// An Event Owner runs an Event; the Organization's bank account is not part
	// of that, so the refusal covers the read as well as the write.
	for _, actor := range []struct {
		role      string
		sessionID string
	}{
		{role: "event_owner", sessionID: addMember("owner@example.com", "event_owner")},
		{role: "event_staff", sessionID: addMember("doorstaff@example.com", "event_staff")},
	} {
		resp, body := env.get(t, payoutProfilePath, authHeader(actor.sessionID))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s read status=%d error=%+v; want 403 FORBIDDEN", actor.role, resp.StatusCode, body.Error)
		}
		resp, body = putPayoutProfile(t, env, actor.sessionID, completeProfile())
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s write status=%d error=%+v; want 403 FORBIDDEN", actor.role, resp.StatusCode, body.Error)
		}
	}

	// Nobody at all is nobody at all.
	if resp, _ := env.get(t, payoutProfilePath, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous read status=%d; want 401", resp.StatusCode)
	}
	if resp, _ := env.put(t, payoutProfilePath, completeProfile(), nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous write status=%d; want 401", resp.StatusCode)
	}
}

// TestPayoutProfileIsScopedToTheActingOrganization: one profile per
// Organization means one profile per Organization, and reading yours never
// returns somebody else's bank account.
func TestPayoutProfileIsScopedToTheActingOrganization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// The pre-seeded demo Organization banks somewhere else entirely.
	if _, err := env.db.Exec(`
		INSERT INTO organization_payout_profiles
			(organization_id, bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number)
		SELECT id, 'Banco Guayaquil', 'corriente', '0009999', 'Demo Venue', 'cedula', $1
		FROM organizations WHERE slug = 'demo-venue'
	`, otherCedula); err != nil {
		t.Fatalf("seed other org profile: %v", err)
	}

	if _, exists := getPayoutProfileOK(t, env, sessionID); exists {
		t.Fatalf("another Organization's Payout Profile leaked into this one")
	}

	if resp, body := putPayoutProfile(t, env, sessionID, completeProfile()); resp.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d error=%+v", resp.StatusCode, body.Error)
	}
	mine, _ := getPayoutProfileOK(t, env, sessionID)
	if mine.BankName != "Banco Pichincha" {
		t.Fatalf("profile = %+v; want this Organization's own", mine)
	}

	var others string
	if err := env.db.QueryRow(`
		SELECT p.bank_name FROM organization_payout_profiles p
		JOIN organizations o ON o.id = p.organization_id WHERE o.slug = 'demo-venue'
	`).Scan(&others); err != nil {
		t.Fatalf("read other org profile: %v", err)
	}
	if others != "Banco Guayaquil" {
		t.Fatalf("other Organization's profile = %q; want it untouched", others)
	}
}
