package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The House Organization designation (#472, parent #471, ADR 0060): a Platform
// Operator marks an Organization as one the platform's own entity runs, or
// takes the mark back, from the Operator Dashboard. The designation carries
// who made it and when, is refused for an Organization whose currency the
// Issuer does not invoice in, and consults no Issuer state at all — none of
// these tests creates an Issuer, and the designation goes through regardless.
//
// Nothing is invoiced here. The designation is read by nobody but the
// operator organization projection until the next ticket connects it to the
// sale commit path.

// houseOrganizationView is the operator organization projection, as much of
// it as the designation tests read.
type houseOrganizationView struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Currency            string  `json:"currency"`
	IsHouseOrganization bool    `json:"is_house_organization"`
	HouseDesignatedBy   *string `json:"house_designated_by"`
	HouseDesignatedAt   *string `json:"house_designated_at"`
}

type houseOrganizationDetail struct {
	Organization houseOrganizationView `json:"organization"`
}

type houseOrganizationListRow struct {
	Name                string `json:"name"`
	IsHouseOrganization bool   `json:"is_house_organization"`
}

type houseOrganizationList struct {
	Data []houseOrganizationListRow `json:"data"`
}

func houseOrganizationPath(orgID string) string {
	return "/api/v1/operator/organizations/" + orgID + "/house"
}

// designateHouseOrganization presses the toggle on and decodes the
// Organization as it now stands.
func designateHouseOrganization(t *testing.T, env *testEnv, sessionID, orgID string) houseOrganizationView {
	t.Helper()
	resp, body := env.put(t, houseOrganizationPath(orgID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("designate house organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view houseOrganizationView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode designated organization: %v", err)
	}
	return view
}

func houseOrganizationDetailView(t *testing.T, env *testEnv, sessionID, orgID string) houseOrganizationView {
	t.Helper()
	var detail houseOrganizationDetail
	operatorGetOK(t, env, sessionID, "/api/v1/operator/organizations/"+orgID, &detail)
	return detail.Organization
}

func houseOrganizationListRowByName(t *testing.T, env *testEnv, sessionID, name string) houseOrganizationListRow {
	t.Helper()
	var list houseOrganizationList
	operatorGetOK(t, env, sessionID, "/api/v1/operator/organizations", &list)
	for _, row := range list.Data {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("organization %q missing from the operator list %+v", name, list.Data)
	return houseOrganizationListRow{}
}

// assertDesignatedAt checks the trail's instant against the harness clock,
// whatever offset the database rendered it in.
func assertDesignatedAt(t *testing.T, env *testEnv, at *string, where string) {
	t.Helper()
	if at == nil {
		t.Fatalf("%s: house_designated_at is null; want the server's clock", where)
	}
	parsed, err := time.Parse(time.RFC3339, *at)
	if err != nil {
		t.Fatalf("%s: house_designated_at %q: %v", where, *at, err)
	}
	if !parsed.Equal(env.fixedClock) {
		t.Fatalf("%s: house_designated_at = %s; want the server's clock %s", where, *at, env.fixedClock)
	}
}

func assertNotHouse(t *testing.T, view houseOrganizationView, where string) {
	t.Helper()
	if view.IsHouseOrganization || view.HouseDesignatedBy != nil || view.HouseDesignatedAt != nil {
		t.Fatalf("%s = %+v; want no House designation and an empty trail", where, view)
	}
}

// TestOperatorDesignatesAndClearsAHouseOrganization: the toggle on stamps the
// operator's email and the server's clock, the projection says so on the list
// and the detail, and the toggle off empties both. Designating again changes
// nothing: the trail names the act that made it a House Organization, not the
// last operator to press a button that was already on.
func TestOperatorDesignatesAndClearsAHouseOrganization(t *testing.T) {
	env := setupTest(t)
	orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// Before: an ordinary Organization, everywhere the operator reads it.
	assertNotHouse(t, houseOrganizationDetailView(t, env, operatorSessionID, orgID), "detail before designation")
	if row := houseOrganizationListRowByName(t, env, operatorSessionID, "Test Org"); row.IsHouseOrganization {
		t.Fatalf("list row before designation = %+v; want not a House Organization", row)
	}

	designated := designateHouseOrganization(t, env, operatorSessionID, orgID)
	if !designated.IsHouseOrganization {
		t.Fatalf("designated organization = %+v; want is_house_organization true", designated)
	}
	if designated.HouseDesignatedBy == nil || *designated.HouseDesignatedBy != "operator@example.com" {
		t.Fatalf("house_designated_by = %v; want the designating operator's email", designated.HouseDesignatedBy)
	}
	assertDesignatedAt(t, env, designated.HouseDesignatedAt, "designated organization")

	// The projection carries the same fact on the detail and on the list.
	detail := houseOrganizationDetailView(t, env, operatorSessionID, orgID)
	if !detail.IsHouseOrganization || detail.HouseDesignatedBy == nil || *detail.HouseDesignatedBy != "operator@example.com" {
		t.Fatalf("detail after designation = %+v", detail)
	}
	assertDesignatedAt(t, env, detail.HouseDesignatedAt, "detail after designation")
	if row := houseOrganizationListRowByName(t, env, operatorSessionID, "Test Org"); !row.IsHouseOrganization {
		t.Fatalf("list row after designation = %+v; want a House Organization", row)
	}
	// The pre-seeded Organization is untouched: the designation is per
	// Organization, never platform-wide.
	if row := houseOrganizationListRowByName(t, env, operatorSessionID, "Demo Venue"); row.IsHouseOrganization {
		t.Fatalf("Demo Venue = %+v; want not a House Organization", row)
	}

	// A colleague pressing the same toggle again leaves the trail alone.
	colleagueSessionID := operatorSession(t, env, "colleague@example.com")
	again := designateHouseOrganization(t, env, colleagueSessionID, orgID)
	if again.HouseDesignatedBy == nil || *again.HouseDesignatedBy != "operator@example.com" {
		t.Fatalf("house_designated_by after a second designation = %v; want the first operator", again.HouseDesignatedBy)
	}

	// Off: the flag and both halves of the trail go together.
	resp, body := env.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear house designation status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var cleared houseOrganizationView
	if err := json.Unmarshal(body.Data, &cleared); err != nil {
		t.Fatalf("decode cleared organization: %v", err)
	}
	assertNotHouse(t, cleared, "cleared organization")
	assertNotHouse(t, houseOrganizationDetailView(t, env, operatorSessionID, orgID), "detail after clearing")
	if row := houseOrganizationListRowByName(t, env, operatorSessionID, "Test Org"); row.IsHouseOrganization {
		t.Fatalf("list row after clearing = %+v; want not a House Organization", row)
	}

	// Clearing what is already clear is an ordinary answer, not a refusal.
	resp, body = env.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear an undesignated organization status=%d, want 200; error=%+v", resp.StatusCode, body.Error)
	}

	// An Organization that does not exist is 404 on both verbs.
	for _, call := range []func() (*http.Response, envelope){
		func() (*http.Response, envelope) {
			return env.put(t, houseOrganizationPath("00000000-0000-0000-0000-000000000000"), nil, authHeader(operatorSessionID))
		},
		func() (*http.Response, envelope) {
			return env.deleteJSON(t, houseOrganizationPath("not-a-uuid"), nil, authHeader(operatorSessionID))
		},
	} {
		resp, body := call()
		if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "ORGANIZATION_NOT_FOUND" {
			t.Fatalf("unknown organization status=%d error=%+v; want 404 ORGANIZATION_NOT_FOUND", resp.StatusCode, body.Error)
		}
	}
}

// TestHouseDesignationRefusedForANonUSDOrganization: the Issuer invoices in
// USD and nothing else, so an Organization trading in another currency cannot
// owe a Sale Invoice that could ever be built. The refusal names the currency,
// and the Organization is left exactly as it was. No Issuer exists in this
// test, and none is consulted: the refusal is about the Organization's money,
// not the platform's paperwork (ADR 0060).
func TestHouseDesignationRefusedForANonUSDOrganization(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")

	resp, body := env.patch(t, "/api/v1/staff/organization", map[string]string{
		"name":     "Test Org",
		"currency": "EUR",
	}, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch currency status=%d error=%+v", resp.StatusCode, body.Error)
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	resp, body = env.put(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("designate a EUR organization status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "HOUSE_ORGANIZATION_CURRENCY_UNSUPPORTED" {
		t.Fatalf("designate a EUR organization error=%+v; want HOUSE_ORGANIZATION_CURRENCY_UNSUPPORTED", body.Error)
	}
	if !strings.Contains(body.Error.Message, "EUR") {
		t.Fatalf("refusal message %q does not name the currency", body.Error.Message)
	}
	details, _ := body.Error.Details.(map[string]any)
	if details["currency"] != "EUR" {
		t.Fatalf("refusal details = %v; want currency EUR", body.Error.Details)
	}

	assertNotHouse(t, houseOrganizationDetailView(t, env, operatorSessionID, orgID), "detail after the refusal")

	// Back on USD, the same Organization can be designated: the refusal was
	// about the currency and nothing sticks to the Organization.
	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]string{
		"name":     "Test Org",
		"currency": "USD",
	}, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch currency back status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if view := designateHouseOrganization(t, env, operatorSessionID, orgID); !view.IsHouseOrganization {
		t.Fatalf("designation after returning to USD = %+v", view)
	}
}

// TestHouseDesignationIsTheOperatorsAlone: an Org Admin of the very
// Organization cannot designate or clear it from any staff surface — the
// operator namespace refuses them, and no Organization-scoped route offers the
// toggle. Org roles grant nothing platform-wide, and a House Organization is a
// statement about whose sale a ticket is, which is not the Organization's to
// make (CONTEXT.md, House Organization).
func TestHouseDesignationIsTheOperatorsAlone(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")

	for _, call := range []struct {
		verb string
		do   func() (*http.Response, envelope)
	}{
		{"PUT", func() (*http.Response, envelope) {
			return env.put(t, houseOrganizationPath(orgID), nil, authHeader(adminSessionID))
		}},
		{"DELETE", func() (*http.Response, envelope) {
			return env.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(adminSessionID))
		}},
	} {
		resp, body := call.do()
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s house as org_admin status=%d error=%+v; want 403 FORBIDDEN", call.verb, resp.StatusCode, body.Error)
		}
	}

	// The Organization's own settings surface knows nothing of the
	// designation: no field to read it from and none to write it through.
	resp, body := env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":                  "Test Org",
		"is_house_organization": true,
	}, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if strings.Contains(string(body.Data), "house") {
		t.Fatalf("organization settings payload mentions the House designation: %s", body.Data)
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	assertNotHouse(t, houseOrganizationDetailView(t, env, operatorSessionID, orgID), "detail after the org_admin's attempts")
}
