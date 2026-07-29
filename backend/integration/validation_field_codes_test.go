package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// fieldError is one entry of a VALIDATION_FAILED envelope's details, read the
// way a client reads it: a stable code to key copy on, and the English message
// to fall back to.
type fieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// fieldErrorsByName flattens the validation envelope keyed by field name. It is
// the code-carrying sibling of fieldErrors in checkout_tax_id_test.go, which
// keeps only the messages the checkout form renders today.
func fieldErrorsByName(t *testing.T, body envelope) map[string]fieldError {
	t.Helper()
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected a VALIDATION_FAILED envelope, got %+v", body.Error)
	}
	raw, err := json.Marshal(body.Error.Details)
	if err != nil {
		t.Fatalf("marshal error details: %v", err)
	}
	var details struct {
		Fields []fieldError `json:"fields"`
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode error details %s: %v", raw, err)
	}
	out := map[string]fieldError{}
	for _, f := range details.Fields {
		out[f.Field] = f
	}
	return out
}

// TestCheckoutValidationCarriesCodeAndMessage walks the Storefront-reachable
// field validations on begin-checkout and asserts both halves of every failure:
// the stable code the Storefront will key its Spanish copy on, and the English
// message — unchanged — that it falls back to when a code has no translation.
//
// The messages are spelled out here rather than referenced, deliberately. They
// are the contract a client falls back to, and a test that asserted them by
// constant would pass through a rewording that broke every unlocalised client.
func TestCheckoutValidationCarriesCodeAndMessage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Code Fest", "code-fest", soon, true, 2500, 100)

	body := taxIDCheckoutBody("not-an-email", "", "Buyer", "cedula", "1712345670",
		map[string]any{"ticket_type_id": "not-a-uuid", "quantity": 0})
	body["customer_phone"] = "+593223456789"

	resp, envBody := beginCheckout(t, env, "test-org", "code-fest", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("begin checkout status=%d, want 400 (error=%+v)", resp.StatusCode, envBody.Error)
	}
	fields := fieldErrorsByName(t, envBody)

	want := []fieldError{
		{Field: "customer_email", Code: "INVALID_EMAIL", Message: "must be a valid email"},
		{Field: "customer_first_name", Code: "REQUIRED", Message: "is required"},
		{Field: "customer_tax_id_number", Code: "INVALID_CEDULA", Message: "must be a valid 10-digit cédula"},
		{Field: "customer_phone", Code: "INVALID_PHONE_EC", Message: "must be an Ecuadorian mobile: 9 digits starting with 9"},
		{Field: "lines[0].ticket_type_id", Code: "INVALID_ID", Message: "must be a valid id"},
		{Field: "lines[0].quantity", Code: "INVALID_POSITIVE_INT", Message: "must be greater than zero"},
	}
	for _, w := range want {
		got, ok := fields[w.Field]
		if !ok {
			t.Fatalf("no field error on %s; got %+v", w.Field, fields)
		}
		if got != w {
			t.Fatalf("field error on %s = %+v, want %+v", w.Field, got, w)
		}
	}
}

// TestCheckoutTaxIDTypeValidationCarriesCode covers the one Tax ID failure the
// case above cannot reach at the same time: a Tax ID Type outside the three
// blames the type field rather than the number.
func TestCheckoutTaxIDTypeValidationCarriesCode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	soon := env.fixedClock.Add(10 * 24 * time.Hour)
	publishEvent(t, env, sessionID, "Code Fest", "code-fest", soon, true, 2500, 100)

	body := taxIDCheckoutBody("buyer@example.com", "Ada", "Buyer", "drivers_license", "1712345675",
		map[string]any{"ticket_type_id": "00000000-0000-0000-0000-000000000000", "quantity": 1})

	resp, envBody := beginCheckout(t, env, "test-org", "code-fest", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("begin checkout status=%d, want 400 (error=%+v)", resp.StatusCode, envBody.Error)
	}
	got := fieldErrorsByName(t, envBody)["customer_tax_id_type"]
	want := fieldError{
		Field:   "customer_tax_id_type",
		Code:    "INVALID_TAX_ID_TYPE",
		Message: "must be cedula, ruc, or passport",
	}
	if got != want {
		t.Fatalf("field error = %+v, want %+v", got, want)
	}
}

// TestCustomerOTPVerifyValidationCarriesCode pins the codes on the Customer
// Session sign-in, the other Storefront surface that rejects fields: a malformed
// One-time Passcode is a shape complaint, distinct from the OTP_INVALID envelope
// a well-formed but wrong passcode earns.
func TestCustomerOTPVerifyValidationCarriesCode(t *testing.T) {
	env := setupTest(t)

	resp, envBody := env.post(t, customerOTPVerifyPath, map[string]string{
		"email": "buyer@example.com",
		"code":  "123",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("verify status=%d, want 400 (error=%+v)", resp.StatusCode, envBody.Error)
	}
	got := fieldErrorsByName(t, envBody)["code"]
	want := fieldError{Field: "code", Code: "INVALID_PASSCODE_FORMAT", Message: "must be 6 digits"}
	if got != want {
		t.Fatalf("field error = %+v, want %+v", got, want)
	}

	resp, envBody = env.post(t, customerOTPRequestPath, map[string]string{"email": ""}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("request status=%d, want 400 (error=%+v)", resp.StatusCode, envBody.Error)
	}
	gotEmail := fieldErrorsByName(t, envBody)["email"]
	wantEmail := fieldError{Field: "email", Code: "REQUIRED", Message: "is required"}
	if gotEmail != wantEmail {
		t.Fatalf("field error = %+v, want %+v", gotEmail, wantEmail)
	}
}
