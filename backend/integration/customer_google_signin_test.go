package integration

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// Google Sign-In on the Storefront (ADR 0011, PRD google-sign-in). Google proves
// the email; it is not an identity. Everything past that proof is the passcode
// path — the same create-or-reuse-and-stamp step, the same Customer Session —
// so most of what these tests assert is sameness: that a Google-minted session
// is indistinguishable from a passcode-minted one, and that a Google Sign-In
// lands on the same Customer record a passcode would have.
//
// Google itself is stood in for by a stub token endpoint, reached through the
// configurable GOOGLE_TOKEN_ENDPOINT. Cross-surface client isolation is
// enforced by Google binding an authorization code to its issuing client, so it
// cannot be asserted here at all: a stub configured with two clients would prove
// only that the stub was configured with two clients. It is covered by the
// first-deploy runbook instead.

const (
	customerGoogleVerifyPath = "/api/v1/customer/auth/google/verify"

	// The Storefront's Google OAuth client, as the harness configures the API
	// with it. The stub asserts the exchange presents exactly these.
	storefrontGoogleClientID     = "storefront-client-id.apps.googleusercontent.com"
	storefrontGoogleClientSecret = "storefront-client-secret"

	// One authorization code, verifier and redirect URI, standing in for what the
	// Storefront's callback route would relay.
	googleAuthCode     = "4/0AY0e-auth-code"
	googleCodeVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	googleRedirectURI  = "http://localhost:64300/api/customer/auth/google/callback"
)

var googleStub *googleTokenEndpointStub

// googleTokenEndpointStub stands in for Google's token endpoint. Each test says
// what Google answers; the stub records what was asked, so the exchange itself
// can be inspected.
type googleTokenEndpointStub struct {
	server *httptest.Server

	mu       sync.Mutex
	respond  func(w http.ResponseWriter)
	lastForm url.Values
}

func startGoogleTokenStub() *googleTokenEndpointStub {
	stub := &googleTokenEndpointStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))

		stub.mu.Lock()
		stub.lastForm = form
		respond := stub.respond
		stub.mu.Unlock()

		if respond == nil {
			// The default is Google's answer to a code it will not honour: a spent
			// code, a forged one, or one issued to the other surface's client.
			googleInvalidGrant(w)
			return
		}
		respond(w)
	}))
	return stub
}

func (s *googleTokenEndpointStub) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = nil
	s.lastForm = nil
}

// returns makes the stub answer the next exchange with an ID token carrying
// these claims.
func (s *googleTokenEndpointStub) returns(claims map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ya29.stub-access-token",
			"expires_in":   3599,
			"token_type":   "Bearer",
			"id_token":     encodeIDToken(claims),
		})
	}
}

// rejects makes the stub refuse the exchange, as Google does for a code that is
// spent, forged, or bound to another client.
func (s *googleTokenEndpointStub) rejects() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = googleInvalidGrant
}

func (s *googleTokenEndpointStub) exchangeForm() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastForm
}

func googleInvalidGrant(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             "invalid_grant",
		"error_description": "Bad Request",
	})
}

// encodeIDToken builds an ID token carrying the given claims. Its signature is
// nonsense, and nothing verifies it: the token reaches the API on a direct TLS
// connection to the token endpoint, which under OIDC is what makes independent
// signature verification unnecessary. A test that had to sign properly would be
// testing JWKS code this feature deliberately does not have.
func encodeIDToken(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"stub"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s.%s.%s",
		header,
		base64.RawURLEncoding.EncodeToString(payload),
		base64.RawURLEncoding.EncodeToString([]byte("not-a-real-signature")),
	)
}

// verifiedGoogleClaims is what Google returns for an account whose address it
// vouches for. `sub` is present because Google always sends it, and nothing
// reads or stores it.
func verifiedGoogleClaims(email string) map[string]any {
	return map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            storefrontGoogleClientID,
		"sub":            "112233445566778899000",
		"email":          email,
		"email_verified": true,
		"exp":            fixedClock.Add(time.Hour).Unix(),
		"iat":            fixedClock.Unix(),
	}
}

// postGoogleVerify relays an authorization code the way the Storefront's
// callback route does: a code, the PKCE verifier, and the redirect URI, and no
// email address anywhere.
func postGoogleVerify(t *testing.T, env *testEnv) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, customerGoogleVerifyPath, map[string]string{
		"code":          googleAuthCode,
		"code_verifier": googleCodeVerifier,
		"redirect_uri":  googleRedirectURI,
	}, nil)
}

// customerGoogleSignIn completes a Google Sign-In for an address Google vouches
// for and returns the Customer Session token — the Google-side counterpart of
// customerSignIn.
func customerGoogleSignIn(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	googleStub.returns(verifiedGoogleClaims(email))

	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	// The consent step, where this Customer has one, exactly as customerSignIn
	// absorbs it on the passcode door: every test in this package that wants a
	// signed-in Customer wants the ordinary end state, and the gate itself is
	// asserted in customer_consent_test.go rather than in each of them.
	return finishSignIn(t, env, decodeCustomerVerify(t, body))
}

// customerVerifyData is what either sign-in door answers with, and it has two
// shapes since #251: a session and its token, or a consent step with both of
// them null. Session is a POINTER so a test can tell "no session was minted"
// from "a session with empty fields" — which is the whole assertion the consent
// gate rests on.
type customerVerifyData struct {
	Session         *customerSessionView    `json:"session"`
	SessionID       string                  `json:"session_id"`
	ConsentRequired *consentRequiredOutcome `json:"consent_required"`
}

func decodeCustomerVerify(t *testing.T, body envelope) customerVerifyData {
	t.Helper()
	var data customerVerifyData
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	return data
}

// TestCustomerGoogleSignInMintsSessionAndVerifiesCustomer proves the whole
// happy path: the exchange presents the Storefront client's own credentials and
// the PKCE verifier, and what comes back is an ordinary full Customer Session on
// the address Google vouched for, with the Customer stamped verified.
func TestCustomerGoogleSignInMintsSessionAndVerifiesCustomer(t *testing.T) {
	env := setupTest(t)
	googleStub.returns(verifiedGoogleClaims("ana@example.com"))

	resp, body := postGoogleVerify(t, env)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none", body.Error)
	}

	// What the API asked Google. The client secret is the API's alone, and the
	// PKCE verifier and redirect URI are what bind the code to this flow.
	form := googleStub.exchangeForm()
	for field, want := range map[string]string{
		"grant_type":    "authorization_code",
		"code":          googleAuthCode,
		"code_verifier": googleCodeVerifier,
		"redirect_uri":  googleRedirectURI,
		"client_id":     storefrontGoogleClientID,
		"client_secret": storefrontGoogleClientSecret,
	} {
		if got := form.Get(field); got != want {
			t.Fatalf("exchange %s = %q, want %q", field, got, want)
		}
	}

	// The session is earned on the far side of the consent step this Customer has
	// never answered (#251), on this door exactly as on the passcode one. What
	// the session IS, once minted, is what the rest of this test asserts.
	data := completeConsentStep(t, env, decodeCustomerVerify(t, body))
	if data.SessionID == "" {
		t.Fatal("expected a Customer Session token")
	}
	if data.Session.Email != "ana@example.com" {
		t.Fatalf("session email = %q, want ana@example.com", data.Session.Email)
	}
	if data.Session.VerifiedAt == nil {
		t.Fatal("session reports the Customer as unverified after a completed Google Sign-In")
	}
	// A full Customer Session, exactly as a passcode mints: it spans every Ticket
	// Sale the Customer owns rather than naming one.
	if data.Session.TicketSaleID != nil {
		t.Fatalf("ticket_sale_id = %v, want null on a full Customer Session", *data.Session.TicketSaleID)
	}

	customer := readCustomer(t, env, "ana@example.com")
	if !customer.VerifiedAt.Valid {
		t.Fatal("verified_at is still null after a completed Google Sign-In")
	}

	// The same 180-day window a passcode earns.
	wantExpiry := env.fixedClock.Add(customerSessionDuration)
	if got := sessionExpiry(t, env, data.SessionID); !got.Equal(wantExpiry) {
		t.Fatalf("session expires_at = %s, want %s", got, wantExpiry)
	}
}

// TestCustomerGoogleSignInReusesTheRecordTicketSalesCreated proves Google does
// not introduce a second person: an address a box office already sold to signs
// in onto that same Customer, and the sales are there on the session.
func TestCustomerGoogleSignInReusesTheRecordTicketSalesCreated(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Google Fest", "google-fest",
		env.fixedClock.Add(30*24*time.Hour), "google-1", "ana@example.com", "Ana", "Lopez")

	before := readCustomer(t, env, "ana@example.com")
	if before.VerifiedAt.Valid {
		t.Fatal("a Ticket Sale must not verify a Customer")
	}

	token := customerGoogleSignIn(t, env, "ana@example.com")

	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1 — Google Sign-In must reuse the record, not mint a second", n)
	}
	after := readCustomer(t, env, "ana@example.com")
	if after.ID != before.ID {
		t.Fatalf("customer id = %s, want the existing %s", after.ID, before.ID)
	}
	if !after.VerifiedAt.Valid {
		t.Fatal("Google Sign-In left the Customer unverified")
	}

	area := readCustomerArea(t, env, token, "")
	if len(area.Upcoming) != 1 {
		t.Fatalf("upcoming sales = %d, want 1 — the Customer's history must be on the session", len(area.Upcoming))
	}
	if area.Upcoming[0].Event.Slug != "google-fest" {
		t.Fatalf("event slug = %q, want google-fest", area.Upcoming[0].Event.Slug)
	}
}

// TestCustomerGoogleSignInCreatesTheRecordForAnAddressNoSaleHasReached proves
// the create half of create-or-reuse: signing in before ever buying mints the
// record a first Ticket Sale would have reused, and the Customer Area is simply
// empty.
func TestCustomerGoogleSignInCreatesTheRecordForAnAddressNoSaleHasReached(t *testing.T) {
	env := setupTest(t)

	if n := countCustomers(t, env); n != 0 {
		t.Fatalf("customers = %d, want 0 before anyone signs in", n)
	}

	token := customerGoogleSignIn(t, env, "newcomer@example.com")

	customer := readCustomer(t, env, "newcomer@example.com")
	if !customer.VerifiedAt.Valid {
		t.Fatal("the created Customer is not verified")
	}
	// Requested scope is `openid email`, not `profile`: no name comes from Google,
	// so the record waits for a Ticket Sale to give it one.
	if customer.FirstName != "" || customer.LastName != "" {
		t.Fatalf("name = %q %q, want empty — no name is taken from Google", customer.FirstName, customer.LastName)
	}

	area := readCustomerArea(t, env, token, "")
	if len(area.Upcoming) != 0 || len(area.Past) != 0 {
		t.Fatalf("area = %+v, want empty for an address no Ticket Sale has reached", area)
	}
}

// TestCustomerGoogleSignInReusesTheRecordAcrossLetterCase proves NormalizeEmail
// is the one rule here too: Google's canonical address is an ordinary address,
// trimmed and lowercased and nothing more. No Gmail dot or alias folding is
// implied by this test and none exists.
func TestCustomerGoogleSignInReusesTheRecordAcrossLetterCase(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Case Fest", "case-fest",
		env.fixedClock.Add(30*24*time.Hour), "case-1", "ana@example.com", "Ana", "Lopez")

	token := customerGoogleSignIn(t, env, "Ana@Example.COM")

	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1 — letter case must not fragment one person into two", n)
	}
	area := readCustomerArea(t, env, token, "")
	if len(area.Upcoming) != 1 {
		t.Fatalf("upcoming sales = %d, want 1 — the differently-cased sign-in must reach the same history", len(area.Upcoming))
	}
}

// TestCustomerGoogleSignInRefusesAnUnverifiedEmail proves the claim is the whole
// of what is consumed: an address Google will not vouch for buys nothing, and
// leaves nothing behind.
func TestCustomerGoogleSignInRefusesAnUnverifiedEmail(t *testing.T) {
	env := setupTest(t)
	claims := verifiedGoogleClaims("unverified@example.com")
	claims["email_verified"] = false
	googleStub.returns(claims)

	resp, body := postGoogleVerify(t, env)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "GOOGLE_SIGN_IN_FAILED")

	if n := countCustomers(t, env); n != 0 {
		t.Fatalf("customers = %d, want 0 — a refused Google Sign-In must write no row", n)
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
}

// TestCustomerGoogleSignInRefusesATokenWithNoEmailClaim proves there is no
// degraded mode when the one claim being consumed is absent.
func TestCustomerGoogleSignInRefusesATokenWithNoEmailClaim(t *testing.T) {
	env := setupTest(t)
	claims := verifiedGoogleClaims("")
	delete(claims, "email")
	googleStub.returns(claims)

	resp, body := postGoogleVerify(t, env)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "GOOGLE_SIGN_IN_FAILED")

	if n := countCustomers(t, env); n != 0 {
		t.Fatalf("customers = %d, want 0", n)
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
}

// TestCustomerGoogleSignInFailuresAreIndistinguishable proves the endpoint is
// not an enumeration oracle. A code Google rejected for an address with a
// purchase behind it, one for an address the platform has never seen, and an
// unverified address all answer byte-for-byte the same — the passcode request
// endpoint spends real effort denying that question and this route must not give
// it back.
func TestCustomerGoogleSignInFailuresAreIndistinguishable(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Oracle Fest", "oracle-fest",
		env.fixedClock.Add(30*24*time.Hour), "oracle-1", "known@example.com", "Ana", "Lopez")

	// Google rejects the exchange outright — the caller learns nothing about the
	// address the code belonged to, known or not.
	googleStub.rejects()
	rejectedResp, rejectedBody := postGoogleVerify(t, env)
	assertAPIError(t, rejectedResp, rejectedBody, http.StatusUnauthorized, "GOOGLE_SIGN_IN_FAILED")

	// A known address whose Google account is unverified.
	knownClaims := verifiedGoogleClaims("known@example.com")
	knownClaims["email_verified"] = false
	googleStub.returns(knownClaims)
	knownResp, knownBody := postGoogleVerify(t, env)

	// An address the platform has never seen, likewise unverified.
	unknownClaims := verifiedGoogleClaims("stranger@example.com")
	unknownClaims["email_verified"] = false
	googleStub.returns(unknownClaims)
	unknownResp, unknownBody := postGoogleVerify(t, env)

	if knownResp.StatusCode != rejectedResp.StatusCode || unknownResp.StatusCode != rejectedResp.StatusCode {
		t.Fatalf("statuses rejected=%d known=%d unknown=%d — every failure must look the same",
			rejectedResp.StatusCode, knownResp.StatusCode, unknownResp.StatusCode)
	}
	for _, other := range []envelope{knownBody, unknownBody} {
		if *other.Error != *rejectedBody.Error {
			t.Fatalf("error = %+v, want the same as %+v — the failure must not say which address it was",
				other.Error, rejectedBody.Error)
		}
	}

	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want only the one the Ticket Sale created", n)
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
}

// TestGoogleMintedSessionBehavesLikeAPasscodeMintedOne is the point of the whole
// feature: two doors, one room. The two sessions are read, slid and destroyed
// the same way, and neither the session view nor anything else on the wire says
// which door was used.
func TestGoogleMintedSessionBehavesLikeAPasscodeMintedOne(t *testing.T) {
	env := setupTest(t)

	googleToken := customerGoogleSignIn(t, env, "google-person@example.com")
	passcodeToken := customerSignIn(t, env, "passcode-person@example.com")

	// The session read returns the same shape for both, differing only in the
	// email each proved.
	readSession := func(token string) customerSessionView {
		t.Helper()
		resp, body := env.get(t, customerSessionPath, authHeader(token))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
		}
		var view customerSessionView
		if err := json.Unmarshal(body.Data, &view); err != nil {
			t.Fatalf("decode session view: %v", err)
		}
		return view
	}

	googleView := readSession(googleToken)
	passcodeView := readSession(passcodeToken)
	if googleView.VerifiedAt == nil || passcodeView.VerifiedAt == nil {
		t.Fatalf("verified_at google=%v passcode=%v, want both stamped", googleView.VerifiedAt, passcodeView.VerifiedAt)
	}
	if googleView.TicketSaleID != nil || passcodeView.TicketSaleID != nil {
		t.Fatal("both sessions must be full Customer Sessions")
	}

	// Sliding expiry: an authenticated read pushes the window out from the moment
	// of use, on both.
	later := env.fixedClock.Add(30 * 24 * time.Hour)
	setCustomerClock(t, env, later)
	_ = readSession(googleToken)
	_ = readSession(passcodeToken)

	wantExpiry := later.Add(customerSessionDuration)
	for name, token := range map[string]string{"google": googleToken, "passcode": passcodeToken} {
		if got := sessionExpiry(t, env, token); !got.Equal(wantExpiry) {
			t.Fatalf("%s session expires_at = %s, want %s", name, got, wantExpiry)
		}
	}

	// And sign-out kills the Google-minted session as dead as any other: the row
	// is gone, so the token is worthless.
	resp, body := env.post(t, customerLogoutPath, nil, authHeader(googleToken))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.get(t, customerSessionPath, authHeader(googleToken))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")

	// The passcode session is untouched by the other's sign-out.
	if got := readSession(passcodeToken); got.Email != "passcode-person@example.com" {
		t.Fatalf("passcode session email = %q, want passcode-person@example.com", got.Email)
	}
}

// TestCustomerGoogleVerifyRequiresTheRelayedFields proves the request is the
// three things the Storefront can honestly supply — and that an email address is
// not among them anywhere.
func TestCustomerGoogleVerifyRequiresTheRelayedFields(t *testing.T) {
	env := setupTest(t)

	resp, body := env.post(t, customerGoogleVerifyPath, map[string]string{
		"code": googleAuthCode,
	}, nil)
	assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
}
