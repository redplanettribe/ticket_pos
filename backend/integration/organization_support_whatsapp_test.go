package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Organization's Support WhatsApp number (#200, parent #199): the number an
// Org Admin publishes so a Customer on an Event page can message them for
// support.
//
// These sit beside organization_settings_test.go, which covers the rest of the
// profile PATCH, and lean on the phone fixtures in
// checkout_payphone_prefill_test.go — the same numbers, because it is the same
// rule. ADR 0029 records why the strict Ecuadorian tier applies to a support
// line even though platform.ValidatePhone was written for a card form.
//
// What renders from this value lives in #201; nothing here reaches the
// Storefront.

// ecuadorLandline is an Ecuadorian number that is NOT a mobile — an 02… landline
// in canonical form. The strict tier rejects it, which is the point: WhatsApp
// Business can be verified on a landline, but an organizer typing their landline
// by mistake would publish a number that never answers a message, and a Customer
// discovering that mid-purchase is a worse failure than a rejection at the form.
const ecuadorLandline = "+593223456789"

// organizationSupportWhatsApp reads the Support WhatsApp number off the staff
// Organization profile, which is where an Org Admin sees what they published.
// Nil means the Organization has none.
func organizationSupportWhatsApp(t *testing.T, env *testEnv, sessionID string) *string {
	t.Helper()

	resp, body := env.get(t, "/api/v1/staff/organization", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var org struct {
		SupportWhatsApp *string `json:"support_whatsapp"`
	}
	if err := json.Unmarshal(body.Data, &org); err != nil {
		t.Fatalf("decode organization: %v", err)
	}
	return org.SupportWhatsApp
}

// patchOrganization sends a profile PATCH carrying the always-required name plus
// whatever else the caller wants to say. The body is map[string]any rather than
// a struct so a test can spell all three things the wire allows about the
// Support WhatsApp number: a value, an explicit null, and no key at all.
func patchOrganization(t *testing.T, env *testEnv, sessionID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()

	if _, ok := body["name"]; !ok {
		body["name"] = "Test Org"
	}
	return env.patch(t, "/api/v1/staff/organization", body, authHeader(sessionID))
}

// TestSupportWhatsAppStoredCanonical proves the number an Org Admin types the
// way they would write it down is stored in the single canonical E.164 form the
// Storefront will strip a plus off to build a wa.me link.
func TestSupportWhatsAppStoredCanonical(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	if got := organizationSupportWhatsApp(t, env, sessionID); got != nil {
		t.Fatalf("support whatsapp before any edit = %q, want none", *got)
	}

	resp, body := patchOrganization(t, env, sessionID, map[string]any{
		"support_whatsapp": ecuadorMobileTyped,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// The response and the subsequent read must agree: an Org Admin who saves and
	// one who reloads are looking at the same number.
	var updated struct {
		SupportWhatsApp *string `json:"support_whatsapp"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode organization: %v", err)
	}
	if updated.SupportWhatsApp == nil || *updated.SupportWhatsApp != ecuadorMobile {
		t.Fatalf("support whatsapp in patch response = %v, want %s", updated.SupportWhatsApp, ecuadorMobile)
	}
	if got := organizationSupportWhatsApp(t, env, sessionID); got == nil || *got != ecuadorMobile {
		t.Fatalf("support whatsapp on reload = %v, want %s", got, ecuadorMobile)
	}
}

// TestSupportWhatsAppAcceptsForeignNumber proves the permissive tier: outside
// Ecuador the rule is generic E.164 and nothing more, because guessing at 200
// national numbering plans would reject real organizers.
func TestSupportWhatsAppAcceptsForeignNumber(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := patchOrganization(t, env, sessionID, map[string]any{
		"support_whatsapp": foreignMobile,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := organizationSupportWhatsApp(t, env, sessionID); got == nil || *got != foreignMobile {
		t.Fatalf("support whatsapp = %v, want %s", got, foreignMobile)
	}
}

// TestSupportWhatsAppCleared covers withdrawing the number, spelled both ways the
// wire allows. Clearing is a capability in its own right — it takes the number
// off every Event page at once — and not an empty form nobody filled in.
func TestSupportWhatsAppCleared(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"explicit null", nil},
		{"blank string", ""},
		{"whitespace only", "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			sessionID := orgAdminSession(t, env)

			resp, body := patchOrganization(t, env, sessionID, map[string]any{
				"support_whatsapp": ecuadorMobile,
			})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("seed patch status=%d error=%+v", resp.StatusCode, body.Error)
			}

			resp, body = patchOrganization(t, env, sessionID, map[string]any{
				"support_whatsapp": tc.value,
			})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("clear patch status=%d error=%+v", resp.StatusCode, body.Error)
			}
			if got := organizationSupportWhatsApp(t, env, sessionID); got != nil {
				t.Fatalf("support whatsapp after clear = %q, want none", *got)
			}
		})
	}
}

// TestSupportWhatsAppUntouchedWhenKeyAbsent is the other half of the
// presence-keyed contract, and the half a naive implementation gets wrong: a
// PATCH that never mentions the number must leave it exactly where it is.
//
// This is not hypothetical. The field was added to an endpoint that already
// existed, so a Staff app running the previous build sends no key at all — and a
// deploy window is no reason for an Organization to lose its published number.
func TestSupportWhatsAppUntouchedWhenKeyAbsent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := patchOrganization(t, env, sessionID, map[string]any{
		"support_whatsapp": ecuadorMobile,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed patch status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// An ordinary rename, saying nothing about the phone number.
	resp, body = patchOrganization(t, env, sessionID, map[string]any{
		"name": "Renamed Org",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename patch status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := organizationSupportWhatsApp(t, env, sessionID); got == nil || *got != ecuadorMobile {
		t.Fatalf("support whatsapp after unrelated edit = %v, want the untouched %s", got, ecuadorMobile)
	}
}

// TestSupportWhatsAppRejectsIllFormedNumbers proves the verdict and the wording
// an Org Admin reads, tier by tier. The message must name the shape they are
// looking at, because "is not valid" tells someone staring at a landline nothing
// about what to change.
func TestSupportWhatsAppRejectsIllFormedNumbers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		number string
		want   fieldError
	}{
		{
			name:   "ecuadorian landline",
			number: ecuadorLandline,
			want: fieldError{
				Field:   "support_whatsapp",
				Code:    "INVALID_PHONE_EC",
				Message: "must be an Ecuadorian mobile: 9 digits starting with 9",
			},
		},
		{
			name:   "no dialling code",
			number: "0987654321",
			want: fieldError{
				Field:   "support_whatsapp",
				Code:    "INVALID_PHONE",
				Message: "must be 4–15 digits in international format, like +12025550123",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			sessionID := orgAdminSession(t, env)

			resp, body := patchOrganization(t, env, sessionID, map[string]any{
				"support_whatsapp": tc.number,
			})
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("patch status=%d, want 400 (error=%+v)", resp.StatusCode, body.Error)
			}
			fields := fieldErrorsByName(t, body)
			got, ok := fields["support_whatsapp"]
			if !ok {
				t.Fatalf("no field error on support_whatsapp; got %+v", fields)
			}
			if got != tc.want {
				t.Fatalf("field error = %+v, want %+v", got, tc.want)
			}

			// A rejected edit must not have partially landed.
			if got := organizationSupportWhatsApp(t, env, sessionID); got != nil {
				t.Fatalf("support whatsapp after rejected patch = %q, want none", *got)
			}
		})
	}
}

// TestSupportWhatsAppForbiddenForNonOrgAdmin proves the restriction is enforced
// on the server and not only by hiding a form field. The Organization's public
// contact details are not a delegated Member's to change.
func TestSupportWhatsAppForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := verifyOTP(t, env, "staff@example.com")
	resp, body = patchOrganization(t, env, staffSessionID, map[string]any{
		"support_whatsapp": ecuadorMobile,
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("patch as event staff status=%d, want 403 (error=%+v)", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %+v", body.Error)
	}

	if got := organizationSupportWhatsApp(t, env, sessionID); got != nil {
		t.Fatalf("support whatsapp after forbidden patch = %q, want none", *got)
	}
}
