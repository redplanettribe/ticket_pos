package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// startAppReportingTo boots an app over the shared database that reports to
// fake, wired as cmd/server wires the real reporter (ADR 0079): the request
// pipeline and the Logger every service is handed both report to it. Its
// clock reads now.
func startAppReportingTo(t *testing.T, fake platform.Reporter, now *atomic.Int64) (*testEnv, *server.App) {
	t.Helper()
	cfg := platform.Config{
		DatabaseURL:       sharedConnStr,
		RunMigrations:     false,
		StorefrontBaseURL: "http://storefront.example",
		Fees: platform.FeeConfig{
			FeeBasisPoints:    platform.DefaultPlatformFeeBasisPoints,
			FeeIVABasisPoints: platform.DefaultPlatformFeeIVABasisPoints,
		},
	}
	clock := func() time.Time { return time.Unix(0, now.Load()).UTC() }
	app, err := server.NewApp(context.Background(), cfg,
		server.WithEmailSender(sharedEmail),
		server.WithClock(clock),
		server.WithObjectStorage(&mockObjectStorage{}),
		server.WithReporter(fake),
	)
	if err != nil {
		t.Fatalf("new reporting app: %v", err)
	}
	app.InvoicingService.WithSaleInvoiceKick(false)
	app.DigestService.WithClock(clock)
	srv := httptest.NewServer(server.NewHandler(app))
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
	}, app
}

// A SWEEP THAT GIVES UP LOGS AT ERROR, AND IS REPORTED (#765, ADR 0079). A
// Follow Digest whose sends keep failing goes back on the queue, a Warn each
// time and no report; the drain that spends its last attempt abandons it,
// logs that at Error, and that one line is the one event, under the id of
// the drain request that gave up.
func TestAFollowDigestDrainThatGivesUpIsReportedOnceUnderItsRequestID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Given Up Fest", "given-up-fest", env.fixedClock.Add(72*time.Hour))
	followingCustomer(t, env, "ana@example.com")
	enqueueFollowDigests(t, env)

	fake := reportertest.New()
	var now atomic.Int64
	now.Store(env.fixedClock.UnixNano())
	reporting, _ := startAppReportingTo(t, fake, &now)
	env.email.FailWith(errors.New("the mail provider is down"))
	t.Cleanup(func() { env.email.FailWith(nil) })

	var gaveUpRequestID string
	for attempt := 1; attempt <= 10 && gaveUpRequestID == ""; attempt++ {
		resp, body := reporting.post(t, followDigestDrainPath, nil, nil)
		if resp.StatusCode != http.StatusOK || body.Error != nil {
			t.Fatalf("drain %d: status=%d error=%+v, want 200", attempt, resp.StatusCode, body.Error)
		}
		if status, _ := digestRowStatus(t, env, "ana@example.com"); status != "pending" {
			gaveUpRequestID = resp.Header.Get(platform.RequestIDHeader)
			break
		}
		if reports := fake.Errors(); len(reports) != 0 {
			t.Fatalf("a send that goes back on the queue was reported: %+v", reports)
		}
		// Past every backoff.
		now.Add(int64(2 * time.Hour))
	}
	if gaveUpRequestID == "" {
		t.Fatal("the drain never gave up on the Digest")
	}

	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
	}
	if got := reports[0].Err.Error(); !strings.HasPrefix(got, "giving up on a Follow Digest after exhausting its attempts") {
		t.Errorf("reported %q, want the give-up line", got)
	}
	if reports[0].RequestID != gaveUpRequestID {
		t.Errorf("report request id = %q, want the giving-up drain's %q", reports[0].RequestID, gaveUpRequestID)
	}
	if strings.Contains(reports[0].Err.Error(), "ana@example.com") {
		t.Errorf("the report names the Customer: %q", reports[0].Err)
	}
}
