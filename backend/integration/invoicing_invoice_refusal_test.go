package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Refusals before a number is consumed (#454): the form refuses to issue when
// the Issuer cannot sign or the invoice cannot be built, and no secuencial is
// wasted. Every one of these is checked by proving that after the refusal the
// next successful issue still gets secuencial 1.

// TestIssueRefusedWithoutCertificate: an Issuer with no certificate is usable
// for everything except issuing.
func TestIssueRefusedWithoutCertificate(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	putEcuadorIssuer(t, sriEnv, sessionID, validEcuadorIssuerBody())

	resp, body := issueInvoice(t, sessionID, validInvoiceBody())
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "CERTIFICATE_NOT_UPLOADED" {
		t.Fatalf("error=%+v, want CERTIFICATE_NOT_UPLOADED", body.Error)
	}
	assertSequenceStillFresh(t, sessionID)
}

// TestIssueRefusedWithoutIssuer: no Issuer at all, no invoice.
func TestIssueRefusedWithoutIssuer(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	resp, body := issueInvoice(t, sessionID, validInvoiceBody())
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "ISSUER_NOT_FOUND" {
		t.Fatalf("error=%+v, want ISSUER_NOT_FOUND", body.Error)
	}
}

// TestIssueRefusedInvalidTaxID: a mistyped cédula is caught before the SRI
// would warn about it, and before a number is consumed.
func TestIssueRefusedInvalidTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	body := validInvoiceBody()
	body["recipient"].(map[string]any)["tax_id_type"] = "cedula"
	body["recipient"].(map[string]any)["tax_id"] = "1234567890" // fails the check digit
	resp, envBody := issueInvoice(t, sessionID, body)
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want a validation failure", resp.StatusCode)
	}
	if envBody.Error == nil || !fieldNamed(envBody.Error.Details, "recipient.tax_id") {
		t.Fatalf("error=%+v, want a recipient.tax_id field error", envBody.Error)
	}
	assertSequenceStillFresh(t, sessionID)
}

// TestIssueRefusedZeroLines: a factura with no line is refused.
func TestIssueRefusedZeroLines(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	body := validInvoiceBody()
	body["lines"] = []map[string]any{}
	resp, envBody := issueInvoice(t, sessionID, body)
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want a validation failure", resp.StatusCode)
	}
	if envBody.Error == nil || !fieldNamed(envBody.Error.Details, "lines") {
		t.Fatalf("error=%+v, want a lines field error", envBody.Error)
	}
	assertSequenceStillFresh(t, sessionID)
}

// TestIssueRefusedTooManyAdditionalFields: fifteen operator fields are one too
// many — the Recipient's email takes the fifteenth (the kit author's note).
func TestIssueRefusedTooManyAdditionalFields(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	fields := make([]map[string]any, 15)
	for i := range fields {
		fields[i] = map[string]any{"name": "field", "value": "value"}
	}
	body := validInvoiceBody()
	body["additional_fields"] = fields
	resp, envBody := issueInvoice(t, sessionID, body)
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want a validation failure", resp.StatusCode)
	}
	if envBody.Error == nil || !fieldNamed(envBody.Error.Details, "additional_fields") {
		t.Fatalf("error=%+v, want an additional_fields field error", envBody.Error)
	}
	assertSequenceStillFresh(t, sessionID)
}

// TestIssueRefusedForNonOperator: a signed-in Member who is not on the
// allowlist is refused every invoicing route.
func TestIssueRefusedForNonOperator(t *testing.T) {
	env := setupTest(t)
	memberSession := verifyOTP(t, env, "member@example.com")

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, invoicesPath},
		{http.MethodPost, invoicesPath},
		{http.MethodGet, invoicesPath + "/00000000-0000-4000-8000-000000000000"},
		{http.MethodPost, invoicesPath + "/totals"},
	} {
		resp, body := sriEnv.doJSON(t, tc.method, tc.path, map[string]any{}, authHeader(memberSession))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s %s status=%d, want 403", tc.method, tc.path, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s %s error=%+v, want FORBIDDEN", tc.method, tc.path, body.Error)
		}
	}
}

// assertSequenceStillFresh proves no number was consumed: the next successful
// issue takes secuencial 1.
func assertSequenceStillFresh(t *testing.T, sessionID string) {
	t.Helper()
	// The refusals that lack a certificate cannot issue; upload one now so the
	// proof can go through. (For refusals that already have one, this replaces
	// it, which is harmless.)
	uploadCertificate(t, sriEnv, sessionID, rsaP12(t, "1790012345001", "s3cret"), "s3cret")
	view := issueOK(t, sessionID, validInvoiceBody())
	if view.Ecuador.Secuencial != 1 {
		t.Fatalf("next issue took secuencial %d; a number was consumed by the refusal", view.Ecuador.Secuencial)
	}
}

// fieldNamed reports whether the VALIDATION_FAILED details name a field.
func fieldNamed(details any, field string) bool {
	raw, err := json.Marshal(details)
	if err != nil {
		return false
	}
	var parsed struct {
		Fields []struct {
			Field string `json:"field"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return false
	}
	for _, f := range parsed.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}
