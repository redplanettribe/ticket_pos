package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	identitymiddleware "github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// failsBehind serves path behind the real session middleware gate, over the
// request pipeline reporting to fake, with a handler that ends the request in
// an unexpected 500, and returns the one report that request produced.
func failsBehind(t *testing.T, fake *reportertest.Reporter, gate func(http.Handler) http.Handler, token string) platform.ErrorReport {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /fail", gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), errors.New("the database went away"))
	})))
	srv := httptest.NewServer(platform.RequestPipeline(slog.New(slog.DiscardHandler), fake, mux))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/fail", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /fail: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want the handler's 500 past the gate", resp.StatusCode)
	}
	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
	}
	if reports[0].RequestID != resp.Header.Get(platform.RequestIDHeader) {
		t.Errorf("request id = %q, want the response's", reports[0].RequestID)
	}
	return reports[0]
}

// THE STAFF SESSION PLACES A STAFF REQUEST'S REPORTS (#766, ADR 0079): the
// Active Member's id as the user, the staff audience, and the Organization,
// and never the session's email or token.
func TestAStaffRequestsReportCarriesItsMemberAndOrganization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	var memberID, organizationID string
	if err := env.db.QueryRowContext(context.Background(),
		`SELECT m.id, m.organization_id FROM sessions s JOIN members m ON m.id = s.active_member_id WHERE s.id = $1`,
		sessionID).Scan(&memberID, &organizationID); err != nil {
		t.Fatalf("read the Active Member: %v", err)
	}

	fake := reportertest.New()
	var now atomic.Int64
	now.Store(env.fixedClock.UnixNano())
	_, app := startAppReportingTo(t, fake, &now)

	report := failsBehind(t, fake, identitymiddleware.SessionAuth(app.IdentityService), sessionID)

	if report.UserID != memberID {
		t.Errorf("user = %q, want the Active Member's id %q", report.UserID, memberID)
	}
	if report.Tags["audience"] != "staff" || report.Tags["organization_id"] != organizationID {
		t.Errorf("tags = %v, want audience staff and organization_id %s", report.Tags, organizationID)
	}
	for _, leaked := range []string{"admin@example.com", sessionID} {
		if strings.Contains(reportText(report), leaked) {
			t.Errorf("the report carries %q: %s", leaked, reportText(report))
		}
	}
}

// THE CUSTOMER SESSION PLACES A CUSTOMER REQUEST'S REPORTS (#766, ADR 0079):
// the Customer's id as the user and the customer audience, never their email.
func TestACustomerRequestsReportCarriesTheCustomerID(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana.reports@example.com")
	var customerID string
	if err := env.db.QueryRowContext(context.Background(),
		`SELECT id FROM customers WHERE email = $1`, "ana.reports@example.com").Scan(&customerID); err != nil {
		t.Fatalf("read the Customer: %v", err)
	}

	fake := reportertest.New()
	var now atomic.Int64
	now.Store(env.fixedClock.UnixNano())
	_, app := startAppReportingTo(t, fake, &now)

	report := failsBehind(t, fake, customersmiddleware.RequireCustomerSession(app.CustomersService), token)

	if report.UserID != customerID || report.Tags["audience"] != "customer" {
		t.Errorf("user, audience = %q, %q; want %q, customer", report.UserID, report.Tags["audience"], customerID)
	}
	for _, leaked := range []string{"ana.reports@example.com", token} {
		if strings.Contains(reportText(report), leaked) {
			t.Errorf("the report carries %q: %s", leaked, reportText(report))
		}
	}
}

// reportText is everything a report holds, as text.
func reportText(report platform.ErrorReport) string {
	return fmt.Sprintf("%+v %s", report, report.Err)
}
