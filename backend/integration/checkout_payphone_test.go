package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// Online checkout through the REAL PayPhone Payment Provider (issue #86): the
// same begin → redirect → confirm legs as checkout_test.go, but with PayPhone
// credentials configured and the base-URL override pointed at a fake PayPhone
// server — the one seam the parent spec allows (the same pattern as the Google
// token stub, refused in production by LoadConfig).
//
// Staff setup (events, ticket types, sales list) still goes through sharedEnv;
// only the public checkout legs go through payphoneEnv, the second app wired
// with the PayPhone provider. Both apps share one database, so setupTest's
// truncate isolates these tests like any other.

const (
	// The credentials the payphone app is configured with; the fake server
	// asserts every request presents exactly this Bearer token.
	payphoneTestAPIToken = "payphone-test-api-token"
	payphoneTestStoreID  = "store-0001"

	// PayPhone's ids for the attempt: paymentId assigned at Prepare, and the id
	// its return redirect appends to the responseUrl (echoed by V2/Confirm as
	// transactionId unless a test says otherwise).
	payphoneStubPaymentID     = 118201001
	payphoneStubRedirectID    = "11820"
	payphoneStubTransactionID = 987654
)

var (
	payphoneStub *payPhoneServerStub
	payphoneEnv  *testEnv
	payphoneApp  *server.App
	payphoneSrv  *httptest.Server
)

// payPhoneServerStub stands in for PayPhone's API. Each test says how Prepare
// and Confirm answer; the stub records what was asked, so the requests
// themselves can be inspected. Defaults: Prepare succeeds, Confirm approves.
//
// Every Prepare is kept, not just the last one, because Initiate may call it
// twice: a Prepare refused with a 4xx is retried once with the prefills
// stripped (#104), and the only way to see that retry is to see both requests.
type payPhoneServerStub struct {
	server *httptest.Server

	mu          sync.Mutex
	prepare     func(w http.ResponseWriter)
	confirm     func(w http.ResponseWriter)
	prepares    []*payPhoneRecordedRequest
	lastConfirm *payPhoneRecordedRequest
}

// payPhoneRecordedRequest is one request as the fake PayPhone server saw it.
type payPhoneRecordedRequest struct {
	authorization string
	body          map[string]any
}

func startPayPhoneStub() *payPhoneServerStub {
	stub := &payPhoneServerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		rec := &payPhoneRecordedRequest{
			authorization: r.Header.Get("Authorization"),
			body:          body,
		}

		stub.mu.Lock()
		var respond func(w http.ResponseWriter)
		switch r.URL.Path {
		case "/api/button/Prepare":
			stub.prepares = append(stub.prepares, rec)
			respond = stub.prepare
			if respond == nil {
				respond = stub.defaultPrepare
			}
		case "/api/button/V2/Confirm":
			stub.lastConfirm = rec
			respond = stub.confirm
			if respond == nil {
				respond = payPhoneConfirmVerdict(3, "Approved")
			}
		default:
			respond = func(w http.ResponseWriter) { http.Error(w, "no such endpoint", http.StatusNotFound) }
		}
		stub.mu.Unlock()

		respond(w)
	}))
	return stub
}

// cardURL is the payWithCard URL the default Prepare answer carries — what
// begin-checkout must hand back as the redirect.
func (s *payPhoneServerStub) cardURL() string {
	return s.server.URL + fmt.Sprintf("/pay/card?paymentId=%d", payphoneStubPaymentID)
}

func (s *payPhoneServerStub) defaultPrepare(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"paymentId":       payphoneStubPaymentID,
		"payWithCard":     s.cardURL(),
		"payWithPayPhone": s.server.URL + "/pay/app",
	})
}

// payPhoneConfirmVerdict answers V2/Confirm with a settled verdict in
// PayPhone's shape: statusCode 3 approved, 2 canceled.
func payPhoneConfirmVerdict(statusCode int, transactionStatus string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode":        statusCode,
			"transactionStatus": transactionStatus,
			"transactionId":     payphoneStubTransactionID,
			"authorizationCode": "AUTH99",
			"cardBrand":         "Visa",
			"lastDigits":        "1234",
			"amount":            2000,
		})
	}
}

func (s *payPhoneServerStub) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prepare = nil
	s.confirm = nil
	s.prepares = nil
	s.lastConfirm = nil
}

// prepareFails makes every Prepare answer with an HTTP error, as PayPhone does
// for a bad token or an unregistered store.
func (s *payPhoneServerStub) prepareFails(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prepare = func(w http.ResponseWriter) { http.Error(w, `{"message":"provider says no"}`, status) }
}

// prepareFailsOnce refuses the FIRST Prepare and answers every later one
// normally — the shape the prefill fallback needs (#104): PayPhone objects to
// something we sent, and the stripped retry goes through.
func (s *payPhoneServerStub) prepareFailsOnce(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prepare = func(w http.ResponseWriter) {
		// The request has already been recorded by the time a responder runs, so
		// the recorded count names the attempt: 1 is the one to refuse. Reading it
		// back beats a captured counter, which would be written outside the mutex.
		if s.prepareCount() == 1 {
			http.Error(w, `{"message":"provider says no"}`, status)
			return
		}
		s.defaultPrepare(w)
	}
}

// confirmCancels makes Confirm report the canceled verdict.
func (s *payPhoneServerStub) confirmCancels() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confirm = payPhoneConfirmVerdict(2, "Canceled")
}

// confirmFails makes Confirm answer with an HTTP error: an outage, not a verdict.
func (s *payPhoneServerStub) confirmFails(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confirm = func(w http.ResponseWriter) { http.Error(w, "payphone is down", status) }
}

// confirmGarbage makes Confirm answer 200 with a body that is not JSON: also
// not a verdict.
func (s *payPhoneServerStub) confirmGarbage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confirm = func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>maintenance</html>")) }
}

// prepareCount is how many Prepare requests the stub has seen since the last
// reset — one for an accepted attempt, two for a refused one that was retried.
func (s *payPhoneServerStub) prepareCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.prepares)
}

// prepareRequest returns the nth Prepare (0-based) as the stub saw it, so a
// test can compare the refused attempt against the retry that followed it.
func (s *payPhoneServerStub) prepareRequest(t *testing.T, n int) *payPhoneRecordedRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if n >= len(s.prepares) {
		t.Fatalf("PayPhone Prepare #%d was never called (%d seen)", n+1, len(s.prepares))
	}
	return s.prepares[n]
}

func (s *payPhoneServerStub) lastPrepareRequest(t *testing.T) *payPhoneRecordedRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.prepares) == 0 {
		t.Fatal("PayPhone Prepare was never called")
	}
	return s.prepares[len(s.prepares)-1]
}

func (s *payPhoneServerStub) lastConfirmRequest(t *testing.T) *payPhoneRecordedRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastConfirm == nil {
		t.Fatal("PayPhone V2/Confirm was never called")
	}
	return s.lastConfirm
}

// startPayPhoneEnv starts the fake PayPhone server and a second app instance
// selected onto the real PayPhone provider. Called from TestMain after the
// shared app has run migrations.
func startPayPhoneEnv(ctx context.Context, connStr string, email *platform.CaptureEmailSender) error {
	payphoneStub = startPayPhoneStub()

	cfg := platform.Config{
		DatabaseURL: connStr,
		// The shared app already migrated this database.
		RunMigrations:     false,
		StorefrontBaseURL: "http://storefront.example",
		// The same launch fee schedule the shared app runs with, so buyer prices
		// here are the ones the rest of the suite pins (ADR 0014).
		Fees: platform.FeeConfig{
			FeeBasisPoints:    platform.DefaultPlatformFeeBasisPoints,
			FeeIVABasisPoints: platform.DefaultPlatformFeeIVABasisPoints,
		},
		PayPhone: platform.PayPhoneConfig{
			APIToken: payphoneTestAPIToken,
			StoreID:  payphoneTestStoreID,
			BaseURL:  payphoneStub.server.URL,
		},
	}

	app, err := server.NewApp(ctx, cfg,
		server.WithEmailSender(email),
		server.WithClock(func() time.Time { return fixedClock }),
		server.WithObjectStorage(&mockObjectStorage{}),
	)
	if err != nil {
		return fmt.Errorf("new payphone app: %w", err)
	}
	payphoneApp = app

	mux := http.NewServeMux()
	server.RegisterRoutes(mux, app)
	payphoneSrv = httptest.NewServer(platform.RequestIDMiddleware(mux))
	payphoneEnv = &testEnv{
		server:     payphoneSrv,
		db:         app.DB.Pool,
		email:      email,
		fixedClock: fixedClock,
		service:    app.IdentityService,
	}
	return nil
}

func stopPayPhoneEnv() {
	if payphoneSrv != nil {
		payphoneSrv.Close()
	}
	if payphoneApp != nil {
		_ = payphoneApp.Close()
	}
	if payphoneStub != nil {
		payphoneStub.server.Close()
	}
}

// confirmCheckoutParams settles a Payment relaying arbitrary provider params —
// for PayPhone, the id and clientTransactionId its return redirect carries.
func confirmCheckoutParams(t *testing.T, env *testEnv, clientTransactionID string, params map[string]string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/public/checkout/"+clientTransactionID+"/confirm", map[string]any{
		"provider_params": params,
	}, nil)
}

// payphoneReturnParams is what PayPhone's return redirect appends to the
// responseUrl, as the Storefront return handler relays it.
func payphoneReturnParams(clientTransactionID string) map[string]string {
	return map[string]string{"id": payphoneStubRedirectID, "clientTransactionId": clientTransactionID}
}

// paymentProvider reads the provider recorded on a Payment row (SQL: no public
// API exposes Payment rows).
func paymentProvider(t *testing.T, env *testEnv, clientTransactionID string) string {
	t.Helper()
	var provider string
	if err := env.db.QueryRow(`
		SELECT provider FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&provider); err != nil {
		t.Fatalf("read payment provider: %v", err)
	}
	return provider
}

// TestPayPhoneBeginCheckoutCallsPrepare pins the Prepare leg: begin-checkout
// sends PayPhone exactly the fields its docs require, under our Bearer token,
// and hands the Storefront the payWithCard URL PayPhone answered with.
func TestPayPhoneBeginCheckoutCallsPrepare(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "PayPhone Fest", "payphone-fest", 1500, 10)

	begin := beginCheckoutOK(t, payphoneEnv, "test-org", "payphone-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	req := payphoneStub.lastPrepareRequest(t)
	if req.authorization != "Bearer "+payphoneTestAPIToken {
		t.Fatalf("authorization = %q, want the configured Bearer token", req.authorization)
	}
	// Integer cents, and amount equal to its one component: with no tax
	// breakdown the whole amount rides as amountWithoutTax.
	// 2 × the all-in 1673¢ a 1500¢ ticket costs under 'pass_on' Fee Handling —
	// and the whole of it still rides as amountWithoutTax in both modes, because
	// the fee is charged to the Organization, not to the Customer (ADR 0014).
	if req.body["amount"] != float64(3346) || req.body["amountWithoutTax"] != float64(3346) {
		t.Fatalf("amount/amountWithoutTax = %v/%v, want 3346/3346", req.body["amount"], req.body["amountWithoutTax"])
	}
	if req.body["clientTransactionId"] != begin.ClientTransactionID {
		t.Fatalf("clientTransactionId = %v, want %q", req.body["clientTransactionId"], begin.ClientTransactionID)
	}
	if req.body["storeId"] != payphoneTestStoreID {
		t.Fatalf("storeId = %v, want %q", req.body["storeId"], payphoneTestStoreID)
	}
	if req.body["currency"] != "USD" {
		t.Fatalf("currency = %v, want USD", req.body["currency"])
	}
	if req.body["reference"] != "PayPhone Fest" {
		t.Fatalf("reference = %v, want the Event name", req.body["reference"])
	}
	if req.body["responseUrl"] != "http://storefront.example/checkout/return" {
		t.Fatalf("responseUrl = %v, want the Storefront return route", req.body["responseUrl"])
	}

	// The Storefront redirects to what PayPhone answered: the hosted card form.
	if begin.RedirectURL != payphoneStub.cardURL() {
		t.Fatalf("redirect url = %q, want PayPhone's payWithCard url %q", begin.RedirectURL, payphoneStub.cardURL())
	}

	// The pending Payment records the real provider and PayPhone's paymentId,
	// so the operator can find the attempt on the PayPhone dashboard already.
	if got := paymentProvider(t, env, begin.ClientTransactionID); got != "payphone" {
		t.Fatalf("payment provider = %q, want payphone", got)
	}
	status, ticketSaleID, providerTxID := paymentRecord(t, env, begin.ClientTransactionID)
	if status != "pending" || ticketSaleID != nil {
		t.Fatalf("payment = %s/%v, want pending with no sale yet", status, ticketSaleID)
	}
	if providerTxID == nil || *providerTxID != fmt.Sprint(payphoneStubPaymentID) {
		t.Fatalf("provider_transaction_id = %v, want the Prepare paymentId %d", providerTxID, payphoneStubPaymentID)
	}
}

// TestPayPhoneCheckoutApprove is the real-provider tracer bullet: the return
// redirect's {id, clientTransactionId} params drive V2/Confirm, statusCode 3
// commits the Ticket Sale, and PayPhone's transactionId lands on the Payment.
func TestPayPhoneCheckoutApprove(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Approve Fest", "approve-fest", 1000, 10)

	begin := beginCheckoutOK(t, payphoneEnv, "test-org", "approve-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	resp, body := confirmCheckoutParams(t, payphoneEnv, begin.ClientTransactionID, payphoneReturnParams(begin.ClientTransactionID))
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

	// V2/Confirm was asked with PayPhone's integer id and our id as clientTxId.
	req := payphoneStub.lastConfirmRequest(t)
	if req.authorization != "Bearer "+payphoneTestAPIToken {
		t.Fatalf("confirm authorization = %q, want the configured Bearer token", req.authorization)
	}
	if req.body["id"] != float64(11820) {
		t.Fatalf("confirm id = %v, want the redirect's id param as an integer", req.body["id"])
	}
	if req.body["clientTxId"] != begin.ClientTransactionID {
		t.Fatalf("confirm clientTxId = %v, want %q", req.body["clientTxId"], begin.ClientTransactionID)
	}

	// The sale is committed and the Payment records PayPhone's transactionId.
	status, ticketSaleID, providerTxID := paymentRecord(t, env, begin.ClientTransactionID)
	if status != "approved" || ticketSaleID == nil {
		t.Fatalf("payment = %s/%v, want approved with its sale", status, ticketSaleID)
	}
	if providerTxID == nil || *providerTxID != fmt.Sprint(payphoneStubTransactionID) {
		t.Fatalf("provider_transaction_id = %v, want the Confirm transactionId %d", providerTxID, payphoneStubTransactionID)
	}
	// The card details PayPhone reported are kept on the Payment for support
	// lookups (ticket #86). Read directly: no public API exposes Payment state.
	var instrument *string
	if err := env.db.QueryRow(`SELECT instrument FROM payments WHERE client_transaction_id = $1`, begin.ClientTransactionID).Scan(&instrument); err != nil {
		t.Fatalf("read instrument: %v", err)
	}
	if instrument == nil || *instrument != "visa ····1234" {
		t.Fatalf("instrument = %v, want the Confirm cardBrand+lastDigits as visa ····1234", instrument)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count = %d, want 2", got)
	}
	if got := salesCountByEmail(t, env, eventID, "guest@example.com"); got != 1 {
		t.Fatalf("sales = %d, want 1", got)
	}
	if got := len(env.email.Confirmations()); got != 1 {
		t.Fatalf("confirmations sent = %d, want 1", got)
	}
}

// TestPayPhoneCheckoutDecline pins statusCode 2: a canceled charge fails the
// Payment — a definitive verdict, unlike the outage cases below — and no sale
// exists.
func TestPayPhoneCheckoutDecline(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Decline Fest", "decline-fest", 1000, 10)

	begin := beginCheckoutOK(t, payphoneEnv, "test-org", "decline-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))

	payphoneStub.confirmCancels()
	resp, body := confirmCheckoutParams(t, payphoneEnv, begin.ClientTransactionID, payphoneReturnParams(begin.ClientTransactionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var confirm confirmCheckoutResult
	if err := json.Unmarshal(body.Data, &confirm); err != nil {
		t.Fatalf("decode confirm result: %v", err)
	}
	if confirm.Status != "failed" || confirm.ConfirmationRef != "" {
		t.Fatalf("confirm = %+v, want failed with no reference", confirm)
	}

	status, ticketSaleID, providerTxID := paymentRecord(t, env, begin.ClientTransactionID)
	if status != "failed" || ticketSaleID != nil {
		t.Fatalf("payment = %s/%v, want failed with no sale", status, ticketSaleID)
	}
	// The decline still records PayPhone's transactionId for support.
	if providerTxID == nil || *providerTxID != fmt.Sprint(payphoneStubTransactionID) {
		t.Fatalf("provider_transaction_id = %v, want %d", providerTxID, payphoneStubTransactionID)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Fatalf("sold_count = %d, want 0", got)
	}
	if got := len(env.email.Confirmations()); got != 0 {
		t.Fatalf("confirmations sent = %d, want 0", got)
	}
}

// TestPayPhoneConfirmUnknownOutcomeLeavesPaymentPending pins the distinction
// the money depends on: an outage, garbage, or a mangled return redirect is NOT
// a decline — the charge might have succeeded. The Payment stays pending, the
// error surfaces, and a later confirm (the Customer refreshing the return page)
// can still settle it correctly inside PayPhone's 5-minute window.
func TestPayPhoneConfirmUnknownOutcomeLeavesPaymentPending(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Outage Fest", "outage-fest", 1000, 10)

	begin := beginCheckoutOK(t, payphoneEnv, "test-org", "outage-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))

	cases := []struct {
		name   string
		setup  func()
		params map[string]string
	}{
		{"provider 500", func() { payphoneStub.confirmFails(http.StatusInternalServerError) }, payphoneReturnParams(begin.ClientTransactionID)},
		{"provider garbage", func() { payphoneStub.confirmGarbage() }, payphoneReturnParams(begin.ClientTransactionID)},
		{"missing id param", func() {}, map[string]string{"clientTransactionId": begin.ClientTransactionID}},
	}
	for _, tc := range cases {
		tc.setup()
		resp, body := confirmCheckoutParams(t, payphoneEnv, begin.ClientTransactionID, tc.params)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s: status=%d, want 500", tc.name, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "INTERNAL_ERROR" {
			t.Fatalf("%s: error=%+v, want INTERNAL_ERROR", tc.name, body.Error)
		}
		status, ticketSaleID, _ := paymentRecord(t, env, begin.ClientTransactionID)
		if status != "pending" || ticketSaleID != nil {
			t.Fatalf("%s: payment = %s/%v, want still pending — an unknown outcome must never settle it", tc.name, status, ticketSaleID)
		}
	}

	// PayPhone recovers; the Customer's refresh retries the confirm and the
	// approved charge becomes a sale after all.
	payphoneStub.reset()
	resp, body := confirmCheckoutParams(t, payphoneEnv, begin.ClientTransactionID, payphoneReturnParams(begin.ClientTransactionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retry confirm status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var confirm confirmCheckoutResult
	if err := json.Unmarshal(body.Data, &confirm); err != nil {
		t.Fatalf("decode confirm result: %v", err)
	}
	if confirm.Status != "approved" {
		t.Fatalf("retry confirm = %+v, want approved", confirm)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Fatalf("sold_count after retry = %d, want 1", got)
	}
}

// TestPayPhonePrepareFailureLeavesThePendingPaymentBehind pins begin-checkout's
// behavior when PayPhone refuses Prepare: the request fails cleanly and — the
// service's deliberate choice — the pending Payment stays behind with no
// redirect ever handed out, lapsing to expired like any abandoned checkout
// (ADR 0013). No sale, no email, nothing charged.
func TestPayPhonePrepareFailureLeavesThePendingPaymentBehind(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Refused Fest", "refused-fest", 1000, 10)

	payphoneStub.prepareFails(http.StatusInternalServerError)
	resp, body := beginCheckout(t, payphoneEnv, "test-org", "refused-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("begin status=%d, want 500", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("begin error=%+v, want INTERNAL_ERROR", body.Error)
	}

	// Exactly one Payment: pending, never redirected, with no provider id (the
	// Prepare that would have assigned one failed).
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&n); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if n != 1 {
		t.Fatalf("payments = %d, want the one pending attempt left to lapse", n)
	}
	var status string
	var providerTxID *string
	if err := env.db.QueryRow(`SELECT status, provider_transaction_id FROM payments`).Scan(&status, &providerTxID); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if status != "pending" || providerTxID != nil {
		t.Fatalf("payment = %s/%v, want pending with no provider id", status, providerTxID)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Fatalf("sold_count = %d, want 0", got)
	}
	if got := len(env.email.Confirmations()); got != 0 {
		t.Fatalf("confirmations sent = %d, want 0", got)
	}
}
