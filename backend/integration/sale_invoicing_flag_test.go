package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// SALE_INVOICING_ENABLED (#471, ADR 0060) ships closed, and closed means a
// build that sells and reverses exactly as one without the feature: the
// House designation answers 404 both ways, the Organization detail says the
// feature is closed so the staff app offers no toggle, a paid checkout of an
// already-designated House Organization owes nothing and promises nothing,
// and the Drainer's endpoint answers 404. The shared app runs with the flag
// OPEN because the rest of the suite proves the feature; this file boots its
// own app without it, the way the certificate-key test boots one without
// the key.

// startAppWithSaleInvoicingClosed boots a second app over the shared
// database with everything the PayPhone app has — the fake provider, the
// certificate key, the fake SRI — and SALE_INVOICING_ENABLED unset.
func startAppWithSaleInvoicingClosed(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	cfg := platform.Config{
		DatabaseURL:       sharedConnStr,
		RunMigrations:     false,
		StorefrontBaseURL: "http://storefront.example",
		StaffBaseURL:      "http://staff.example",
		Fees: platform.FeeConfig{
			FeeBasisPoints:    platform.DefaultPlatformFeeBasisPoints,
			FeeIVABasisPoints: platform.DefaultPlatformFeeIVABasisPoints,
		},
		PayPhone: platform.PayPhoneConfig{
			APIToken: payphoneTestAPIToken,
			StoreID:  payphoneTestStoreID,
			BaseURL:  payphoneStub.server.URL,
		},
		InvoicingCertificateKey: sharedInvoicingKey(),
		SRIBaseURL:              sriStub.server.URL,
		// The one line the other test apps have and this one does not.
		SaleInvoicingEnabled: false,
	}
	app, err := server.NewApp(ctx, cfg,
		server.WithEmailSender(sharedEmail),
		server.WithClock(func() time.Time { return fixedClock }),
		server.WithObjectStorage(&mockObjectStorage{}),
	)
	if err != nil {
		t.Fatalf("new app with sale invoicing closed: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterRoutes(mux, app)
	srv := httptest.NewServer(platform.RequestIDMiddleware(mux))
	t.Cleanup(func() {
		srv.Close()
		_ = app.Close()
	})
	return &testEnv{
		server:     srv,
		db:         app.DB.Pool,
		email:      sharedEmail,
		fixedClock: fixedClock,
		service:    app.IdentityService,
	}
}

type saleInvoicingFlagDetail struct {
	Organization         houseOrganizationView `json:"organization"`
	SaleInvoicingEnabled bool                  `json:"sale_invoicing_enabled"`
}

func getOrganizationDetailFlag(t *testing.T, env *testEnv, sessionID, orgID string) saleInvoicingFlagDetail {
	t.Helper()
	resp, body := env.get(t, "/api/v1/operator/organizations/"+orgID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("organization detail status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var detail saleInvoicingFlagDetail
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode organization detail: %v", err)
	}
	return detail
}

func TestSaleInvoicingClosedHidesTheHouseDesignation(t *testing.T) {
	env := setupTest(t)
	closed := startAppWithSaleInvoicingClosed(t)
	orgAdminSession(t, env) // seeds test-org
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// The detail states the flag, and states it per deployment: the same
	// Organization reads open on the shared app and closed on this one.
	if d := getOrganizationDetailFlag(t, env, operatorSessionID, orgID); !d.SaleInvoicingEnabled {
		t.Fatalf("shared app: sale_invoicing_enabled = false; want true")
	}
	if d := getOrganizationDetailFlag(t, closed, operatorSessionID, orgID); d.SaleInvoicingEnabled {
		t.Fatalf("closed app: sale_invoicing_enabled = true; want false")
	}

	// Both verbs answer 404 SALE_INVOICING_UNAVAILABLE, and nothing is written.
	resp, body := closed.put(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("designate while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	if d := getOrganizationDetailFlag(t, env, operatorSessionID, orgID); d.Organization.IsHouseOrganization {
		t.Fatalf("a refused designation was recorded: %+v", d.Organization)
	}
	// Clearing is refused on the same terms, even for a real designation:
	// with the flag closed there is nothing here, whichever way it is pressed.
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	resp, body = closed.deleteJSON(t, houseOrganizationPath(orgID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("undesignate while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	if d := getOrganizationDetailFlag(t, env, operatorSessionID, orgID); !d.Organization.IsHouseOrganization {
		t.Fatalf("a refused clearing was recorded: %+v", d.Organization)
	}
}

func TestSaleInvoicingClosedOwesNothingAndDrainsNothing(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	closed := startAppWithSaleInvoicingClosed(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	// Designated while open — the case the flag exists for: a designation
	// already on record when the flag is closed must do nothing.
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	// A paid House checkout through the closed app: approved, confirmed, and
	// no document owed nor promised.
	begin := beginCheckoutOK(t, closed, "test-org", "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	resp, body := confirmCheckoutParams(t, closed, begin.ClientTransactionID, payphoneReturnParams(begin.ClientTransactionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var confirm confirmCheckoutResult
	if err := json.Unmarshal(body.Data, &confirm); err != nil {
		t.Fatalf("decode confirm result: %v", err)
	}
	if confirm.Status != "approved" || confirm.ConfirmationRef == "" {
		t.Fatalf("confirm = %+v, want approved with a reference", confirm)
	}
	if list := getSaleInvoiceList(t, operatorSessionID); list.Pagination.Total != 0 {
		t.Fatalf("invoices after a paid House checkout with the flag closed = %d; want none", list.Pagination.Total)
	}
	assertNoSaleInvoicePromised(t, lastConfirmation(t, env), "closed flag")
	if n := sequenceRows(t, env); n != 0 {
		t.Fatalf("sequence rows = %d; want none consumed", n)
	}

	// The Drainer's endpoint answers 404 on the closed app.
	resp, body = closed.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("drain while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	if n := sriStub.receptionCount(); n != 0 {
		t.Fatalf("the SRI heard %d receptions with the flag closed; want none", n)
	}
}
