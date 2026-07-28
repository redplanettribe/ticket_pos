package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"
)

type testEnv struct {
	server     *httptest.Server
	db         *sql.DB
	email      *platform.CaptureEmailSender
	fixedClock time.Time
	service    *service.Service
}

type envelope struct {
	Data      json.RawMessage    `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

var (
	sharedEnv   *testEnv
	sharedDB    *sql.DB
	sharedApp   *server.App
	pgContainer testcontainers.Container
	sharedEmail *platform.CaptureEmailSender
	// sharedStorage is the bucket the shared app writes to. Package-level so
	// tests can read its delete recorder; reset per test like sharedEmail.
	sharedStorage = &mockObjectStorage{}
	fixedClock    = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("ticket_pos"),
		postgres.WithUsername("ticket_pos"),
		postgres.WithPassword("ticket_pos"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		os.Exit(1)
	}
	pgContainer = pg

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		os.Exit(1)
	}

	email := &platform.CaptureEmailSender{}

	// The stub Google token endpoint stands in for Google for the whole package
	// run, and is the only reason GOOGLE_TOKEN_ENDPOINT is configurable at all —
	// which is why configuration refuses the override in production. See
	// customer_google_signin_test.go.
	googleStub = startGoogleTokenStub()

	cfg := platform.Config{
		DatabaseURL:   connStr,
		RunMigrations: true,
		// The stub Payment Provider builds its interstitial and return URLs from
		// this origin; no PAYPHONE_* credentials are set, so the stub is selected
		// exactly as it is in local development.
		StorefrontBaseURL: "http://storefront.example",
		// The launch fee schedule, as LoadConfig would default it: 10% Platform
		// Fee, 15% Fee IVA (ADR 0014).
		Fees: platform.FeeConfig{
			FeeBasisPoints:    platform.DefaultPlatformFeeBasisPoints,
			FeeIVABasisPoints: platform.DefaultPlatformFeeIVABasisPoints,
		},
		Google: platform.GoogleConfig{
			Storefront: platform.GoogleOAuthClient{
				ClientID:     storefrontGoogleClientID,
				ClientSecret: storefrontGoogleClientSecret,
			},
			// A second, distinct client, as production has. The stub honours any
			// credentials it is shown, so this does not prove isolation — that is
			// Google's property and the runbook's job. What it does prove is that
			// each endpoint presents its OWN client, which is the half of the
			// property this code is responsible for.
			Staff: platform.GoogleOAuthClient{
				ClientID:     staffGoogleClientID,
				ClientSecret: staffGoogleClientSecret,
			},
			TokenEndpoint: googleStub.server.URL,
		},
	}

	app, err := server.NewApp(ctx, cfg,
		server.WithEmailSender(email),
		server.WithClock(func() time.Time { return fixedClock }),
		server.WithObjectStorage(sharedStorage),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "new app: %v\n", err)
		os.Exit(1)
	}
	sharedApp = app
	sharedDB = app.DB.Pool
	sharedEmail = email

	mux := http.NewServeMux()
	server.RegisterRoutes(mux, app)
	handler := platform.RequestIDMiddleware(mux)
	srv := httptest.NewServer(handler)
	sharedEnv = &testEnv{
		server:     srv,
		db:         app.DB.Pool,
		email:      email,
		fixedClock: fixedClock,
		service:    app.IdentityService,
	}

	// A second app over the SAME database, wired with PayPhone credentials and
	// the base-URL override pointed at a fake PayPhone server, so the real
	// provider is selected exactly as production selects it. Started after the
	// shared app so migrations have already run. See checkout_payphone_test.go.
	if err := startPayPhoneEnv(ctx, connStr, email); err != nil {
		fmt.Fprintf(os.Stderr, "payphone env: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	stopPayPhoneEnv()
	googleStub.server.Close()
	srv.Close()
	_ = app.Close()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

func setupTest(t *testing.T) *testEnv {
	t.Helper()
	if err := resetDatabase(context.Background(), sharedDB); err != nil {
		t.Fatalf("reset database: %v", err)
	}
	sharedEmail.Reset()
	sharedStorage.reset()
	sharedApp.IdentityService.WithClock(func() time.Time { return fixedClock })
	// Customer identity has its own clock: Customer Session lifetime is measured
	// in months, so its tests move time far further than any staff test does.
	sharedApp.CustomersService.WithClock(func() time.Time { return fixedClock })
	// Sales and catalog share the Capacity Hold window (ADR 0013): hold tests
	// move both clocks past it together, so both are reset together.
	sharedApp.SalesService.WithClock(func() time.Time { return fixedClock })
	sharedApp.CatalogService.WithClock(func() time.Time { return fixedClock })
	googleStub.reset()
	payphoneStub.reset()
	return sharedEnv
}

func resetDatabase(ctx context.Context, db *sql.DB) error {
	// Update this list when new application tables are added via migrations.
	if _, err := db.ExecContext(ctx, `
		TRUNCATE TABLE platform_operators, payouts, payment_lines, payments, customer_sessions, ticket_sale_lines, ticket_sales, customers, sale_import_batches, event_tags, event_assignments, ticket_types, events, otp_challenges, sessions, members, organizations RESTART IDENTITY CASCADE
	`); err != nil {
		return fmt.Errorf("truncate tables: %w", err)
	}

	// Preset Tags are seeded once by migration and must survive resets; only
	// Custom Tags coined during a test are cleared for isolation.
	if _, err := db.ExecContext(ctx, `DELETE FROM tags WHERE curated = FALSE`); err != nil {
		return fmt.Errorf("clear custom tags: %w", err)
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO organizations (id, name, slug, created_at)
		VALUES (
			'a0000000-0000-4000-8000-000000000001',
			'Demo Venue',
			'demo-venue',
			NOW()
		);

		INSERT INTO members (id, organization_id, email, role, created_at)
		VALUES (
			'b0000000-0000-4000-8000-000000000001',
			'a0000000-0000-4000-8000-000000000001',
			'preseeded@example.com',
			'org_admin',
			NOW()
		);
	`)
	if err != nil {
		return fmt.Errorf("reseed dev organization: %w", err)
	}

	// The platform operator allowlist is deliberately NOT reseeded: it starts
	// empty so no session is an operator by accident, and the tests that need
	// one insert their own row — which is also how operators are granted in
	// production (ADR 0015). See seedPlatformOperator in operator_test.go.
	return nil
}

func (env *testEnv) post(t *testing.T, path string, body any, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, env.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	var envBody envelope
	decodeEnvelope(t, resp, &envBody)
	return resp, envBody
}

func (env *testEnv) get(t *testing.T, path string, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	var envBody envelope
	decodeEnvelope(t, resp, &envBody)
	return resp, envBody
}

func decodeEnvelope(t *testing.T, resp *http.Response, env *envelope) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.RequestID == "" {
		t.Fatalf("expected request_id in envelope")
	}
	if resp.Header.Get("X-Request-ID") != env.RequestID {
		t.Fatalf("request id header mismatch: header=%q body=%q", resp.Header.Get("X-Request-ID"), env.RequestID)
	}
}

func authHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func (env *testEnv) patch(t *testing.T, path string, body any, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	return env.doJSON(t, http.MethodPatch, path, body, headers)
}

func (env *testEnv) put(t *testing.T, path string, body any, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	return env.doJSON(t, http.MethodPut, path, body, headers)
}

func (env *testEnv) deleteJSON(t *testing.T, path string, body any, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	return env.doJSON(t, http.MethodDelete, path, body, headers)
}

func (env *testEnv) doJSON(t *testing.T, method, path string, body any, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, env.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	var envBody envelope
	decodeEnvelope(t, resp, &envBody)
	return resp, envBody
}

func createOrganization(t *testing.T, env *testEnv, sessionID, name, slug string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/organizations", map[string]string{
		"name": name,
		"slug": slug,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func orgAdminSession(t *testing.T, env *testEnv) string {
	t.Helper()
	sessionID := verifyOTP(t, env, "admin@example.com")
	createOrganization(t, env, sessionID, "Test Org", "test-org")
	return sessionID
}
