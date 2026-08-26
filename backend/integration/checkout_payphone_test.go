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
	reverse     func(w http.ResponseWriter)
	prepares    []*payPhoneRecordedRequest
	lastConfirm *payPhoneRecordedRequest
	reverses    []*payPhoneRecordedRequest
	// reverseSuccesses counts the reversals this fake actually CARRIED OUT, as
	// distinct from the ones it was asked for. The two differ exactly when an
	// answer never reaches us, which is the case ADR 0024 exists for: a reversal
	// that timed out was still performed, and the money still left.
	//
	// It is the counter that can see a double refund. A test that asks "was the
	// buyer refunded once?" cannot answer it from reverseCount, since asking
	// again after a timeout is correct and expected; only the number of
	// reversals the provider performed says whether the money moved twice.
	reverseSuccesses int
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
		// The merchant-keyed reversal endpoint (#120): its documented success is
		// a literal JSON true, which is what the default answers.
		case "/api/Reverse/Client":
			stub.reverses = append(stub.reverses, rec)
			respond = stub.reverse
			if respond == nil {
				respond = stub.reverseSucceeds
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

// reverseSucceeds is PayPhone's documented answer to a completed reversal: the
// literal value true, with no object around it. It counts the reversal as
// CARRIED OUT before answering, because that is when the money leaves — whether
// or not the answer ever reaches the caller.
func (s *payPhoneServerStub) reverseSucceeds(w http.ResponseWriter) {
	s.recordReverseSuccess()
	payPhoneReverseAnswersTrue(w)
}

// recordReverseSuccess marks the money as having left, which happens when
// PayPhone acts and not when the caller hears about it — the whole distinction
// ADR 0024 turns on.
func (s *payPhoneServerStub) recordReverseSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverseSuccesses++
}

// payPhoneReverseAnswersTrue writes the documented answer and nothing else.
func payPhoneReverseAnswersTrue(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("true"))
}

// reverseSuccessCount is how many reversals this fake has actually performed
// since the last reset — the buyer's money, moved. Exactly one is what a
// correctly pursued Reversal Request must ever produce, however many times the
// provider was asked.
func (s *payPhoneServerStub) reverseSuccessCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reverseSuccesses
}

func (s *payPhoneServerStub) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prepare = nil
	s.confirm = nil
	s.reverse = nil
	s.prepares = nil
	s.lastConfirm = nil
	s.reverses = nil
	s.reverseSuccesses = 0
}

// reverseAnswers makes every reversal answer 200 with a raw body — the seam for
// every not-quite-true body PayPhone could send.
func (s *payPhoneServerStub) reverseAnswers(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverse = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// reverseRefuses makes every reversal answer with PayPhone's failure shape: a
// non-2xx carrying a message and an errorCode.
func (s *payPhoneServerStub) reverseRefuses(status int, message string, errorCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverse = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": message, "errorCode": errorCode})
	}
}

// reverseHangsUp drops the connection without answering at all: a transport
// failure, where PayPhone may or may not have acted and nothing can be inferred.
func (s *payPhoneServerStub) reverseHangsUp() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverse = func(w http.ResponseWriter) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "cannot hijack", http.StatusInternalServerError)
			return
		}
		conn, _, err := hijacker.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}
}

// payPhoneReverseHang is how long reverseTimesOut holds a reversal open. It is
// comfortably past the provider's own ten-second call timeout, so the client
// gives up first and the test observes what production observes: a reversal we
// have no answer to, from a PayPhone that may well have acted (ADR 0024).
//
// It is deliberately a real wait rather than a shortened one. The timeout being
// exercised belongs to the PayPhone client, which takes no configuration, and a
// seam invented to make this faster would be a seam that only tests use.
const payPhoneReverseHang = 12 * time.Second

// reverseTimesOut holds every reversal open past the provider's call timeout and
// then answers the documented success — the 2026-07-28 incident exactly: PayPhone
// DID reverse the payment, and the answer arrived after nobody was listening.
//
// It differs from reverseHangsUp in what it stages rather than in what the caller
// sees. Both are unknown outcomes; this one additionally makes the fake's own
// state say the money went back, so a later probe answering errorCode 24 is the
// truthful continuation of the same story rather than a fixture.
func (s *payPhoneServerStub) reverseTimesOut() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverse = func(w http.ResponseWriter) {
		// The reversal is carried out AT ONCE and only the answer is late, which is
		// the shape of the incident: the money left, and the caller never heard so.
		// Counting it before the sleep is also what lets a test assert on it
		// without waiting out a hang nobody is listening to.
		s.recordReverseSuccess()
		time.Sleep(payPhoneReverseHang)
		payPhoneReverseAnswersTrue(w)
	}
}

// reverseAlreadyCancelled makes every reversal answer PayPhone's errorCode 24,
// "La transacción ya se encuentra cancelada": the transaction is already
// reversed at PayPhone, which is a receipt that the money has left rather than a
// refusal. It is the answer a probe gets after a reversal that timed out on our
// side had in fact been carried out.
func (s *payPhoneServerStub) reverseAlreadyCancelled() {
	s.reverseRefuses(http.StatusBadRequest, "La transacción ya se encuentra cancelada", 24)
}

// reverseBlocksFirst holds the FIRST reversal inside PayPhone until it is
// released, then answers the documented success. Every later reversal is
// answered immediately.
//
// It exists to make the double-press race observable rather than a matter of
// timing. The interval in which two attempts can both be past their checks and
// both about to return somebody's money is exactly the interval a provider call
// occupies, and against a fake that answers instantly that interval is too short
// to hit on purpose. Holding the first attempt inside PayPhone opens it as wide
// as a test needs: entered fires once PayPhone has been reached, release lets it
// answer.
//
// Only the first is held, deliberately. A second attempt that should never have
// reached PayPhone must be RECORDED and answered rather than blocked, so the
// failure that matters shows up as "PayPhone was asked twice" and never as a
// test that hangs.
//
// The responder runs after the stub's own mutex is dropped, so blocking here
// blocks one reversal and not the fake server.
func (s *payPhoneServerStub) reverseBlocksFirst() (entered <-chan struct{}, release func()) {
	arrived := make(chan struct{}, 1)
	releaseCh := make(chan struct{})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reverse = func(w http.ResponseWriter) {
		// The request is recorded before any responder runs, so a recorded count of
		// 1 names the first reversal.
		if s.reverseCount() == 1 {
			arrived <- struct{}{}
			<-releaseCh
		}
		s.reverseSucceeds(w)
	}
	return arrived, sync.OnceFunc(func() { close(releaseCh) })
}

// reverseCount is how many reversals PayPhone has been asked for since the last
// reset. One per attempt and never more: a reversal is never retried.
func (s *payPhoneServerStub) reverseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reverses)
}

// lastReverseRequest returns the most recent reversal as the stub saw it.
func (s *payPhoneServerStub) lastReverseRequest(t *testing.T) *payPhoneRecordedRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reverses) == 0 {
		t.Fatal("PayPhone Reverse/Client was never called")
	}
	return s.reverses[len(s.reverses)-1]
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
		// A paid House checkout made here kicks the Sale Invoice Drainer
		// (#474), which signs with the certificate the suite keeps under the
		// shared key and talks to the fake SRI — never the real one.
		InvoicingCertificateKey: sharedInvoicingKey(),
		SRIBaseURL:              sriStub.server.URL,
		// Sale Invoicing OPEN, which is not how it ships (#471, ADR 0060): the
		// suite proves the feature, and the one test of the closed flag boots
		// its own app without this line.
		SaleInvoicingEnabled: true,
	}

	app, err := server.NewApp(ctx, cfg,
		server.WithEmailSender(email),
		server.WithClock(func() time.Time { return fixedClock }),
		server.WithObjectStorage(&mockObjectStorage{}),
	)
	if err != nil {
		return fmt.Errorf("new payphone app: %w", err)
	}
	app.InvoicingService.WithPollSchedule(testPollDelays, testPollBudget).WithSaleInvoiceKick(false)
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
