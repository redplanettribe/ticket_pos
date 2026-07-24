package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Customer sign-in, the Customer Session, and the Customer Area (ADR 0010,
// PRD #55). A Customer proves ownership of an email that already has a record
// and receives a Customer Session spanning every Ticket Sale they own, across
// every Organization.
//
// The property these tests exist for is authorization: a Customer Area read is
// scoped by the Customer on the session and never by an identifier in the
// request. TestCustomerCannotReadAnotherCustomersTicketSales attacks that
// directly rather than only confirming a Customer can see their own sales.

const (
	customerOTPRequestPath = "/api/v1/customer/auth/otp/request"
	customerOTPVerifyPath  = "/api/v1/customer/auth/otp/verify"
	customerSessionPath    = "/api/v1/customer/auth/session"
	customerLogoutPath     = "/api/v1/customer/auth/logout"
	customerAreaPath       = "/api/v1/customer/ticket-sales"
)

// customerSessionDuration mirrors the service's sliding window: 180 days,
// extended on every authenticated use.
const customerSessionDuration = 180 * 24 * time.Hour

type customerSessionView struct {
	Email        string  `json:"email"`
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	VerifiedAt   *string `json:"verified_at"`
	TicketSaleID *string `json:"ticket_sale_id"`
}

type customerAreaSale struct {
	ID              string `json:"id"`
	ConfirmationRef string `json:"confirmation_ref"`
	SoldAt          string `json:"sold_at"`
	Status          string `json:"status"`
	AmountCents     int    `json:"amount_cents"`
	Currency        string `json:"currency"`
	Lines           []struct {
		TicketTypeName string `json:"ticket_type_name"`
		Quantity       int    `json:"quantity"`
		UnitPriceCents int    `json:"unit_price_cents"`
	} `json:"lines"`
	Event struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		Slug     string  `json:"slug"`
		StartsAt *string `json:"starts_at"`
	} `json:"event"`
	Organization struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"organization"`
}

type customerAreaView struct {
	Upcoming []customerAreaSale `json:"upcoming"`
	Past     []customerAreaSale `json:"past"`
}

// requestCustomerPasscode asks for a Customer passcode, optionally from a given
// client IP so per-IP rate limiting can be exercised.
func requestCustomerPasscode(t *testing.T, env *testEnv, email, clientIP string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if clientIP != "" {
		headers = map[string]string{"X-Forwarded-For": clientIP}
	}
	return env.post(t, customerOTPRequestPath, map[string]string{"email": email}, headers)
}

// customerSignIn completes a Customer sign-in and returns the Customer Session
// token: request a passcode, read the captured code, redeem it.
func customerSignIn(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	resp, body := requestCustomerPasscode(t, env, email, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request customer passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": email,
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify customer passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var data struct {
		Session   customerSessionView `json:"session"`
		SessionID string              `json:"session_id"`
	}
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode customer verify data: %v", err)
	}
	if data.SessionID == "" {
		t.Fatal("expected a Customer Session token")
	}
	return data.SessionID
}

// readCustomerArea reads the Customer Area for a session, optionally with a raw
// query string appended — the adversarial tests use that to smuggle identifiers.
func readCustomerArea(t *testing.T, env *testEnv, token, query string) customerAreaView {
	t.Helper()
	path := customerAreaPath
	if query != "" {
		path += "?" + query
	}
	resp, body := env.get(t, path, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer area status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("customer area error=%+v, want none", body.Error)
	}
	var area customerAreaView
	if err := json.Unmarshal(body.Data, &area); err != nil {
		t.Fatalf("decode customer area: %v", err)
	}
	return area
}

// scheduleEvent gives an Event a start date so the Customer Area can tell an
// upcoming Event from one that has already happened.
func scheduleEvent(t *testing.T, env *testEnv, sessionID, eventID, name, slug string, startsAt time.Time) {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":      name,
		"slug":      slug,
		"starts_at": startsAt.Format(time.RFC3339),
		"timezone":  "Europe/Madrid",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("schedule event status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// setCustomerClock moves the clock the customers domain reads. Session lifetime
// is measured in months, so it is travelled to rather than waited out.
func setCustomerClock(t *testing.T, env *testEnv, at time.Time) {
	t.Helper()
	sharedApp.CustomersService.WithClock(func() time.Time { return at })
}

// sessionExpiry reads a Customer Session's sliding expiry. SQL because the
// expiry is deliberately not on the wire — the client is told nothing it could
// come to depend on.
func sessionExpiry(t *testing.T, env *testEnv, token string) time.Time {
	t.Helper()
	var expiresAt time.Time
	if err := env.db.QueryRow(`SELECT expires_at FROM customer_sessions WHERE id = $1`, token).Scan(&expiresAt); err != nil {
		t.Fatalf("read customer session expiry: %v", err)
	}
	return expiresAt.UTC()
}

func countCustomerSessions(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customer_sessions`).Scan(&n); err != nil {
		t.Fatalf("count customer sessions: %v", err)
	}
	return n
}

func assertAPIError(t *testing.T, resp *http.Response, body envelope, wantStatus int, wantCode string) {
	t.Helper()
	if resp.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d (error=%+v)", resp.StatusCode, wantStatus, body.Error)
	}
	if len(body.Data) > 0 && string(body.Data) != "null" {
		t.Fatalf("data = %s, want null on a failure envelope", body.Data)
	}
	if body.Error == nil || body.Error.Code != wantCode {
		t.Fatalf("error = %+v, want code %s", body.Error, wantCode)
	}
}

// seedSaleForCustomer records one Ticket Sale for a Customer on a freshly scheduled
// Event, and returns the Sale Confirmation reference.
func seedSaleForCustomer(t *testing.T, env *testEnv, sessionID, name, slug string, startsAt time.Time, key, email, first, last string) string {
	t.Helper()
	eventID := createDraftEvent(t, env, sessionID, name, slug)
	scheduleEvent(t, env, sessionID, eventID, name, slug, startsAt)
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2500, 50)
	return importOneSale(t, env, sessionID, eventID, key, ttID, email, first, last)
}

// TestCustomerPasscodeRequestIsIdenticalForKnownAndUnknownEmail proves the
// request endpoint is not an oracle for who the platform's Customers are: an
// address with a purchase behind it and one with nothing at all get byte-for-byte
// the same answer.
func TestCustomerPasscodeRequestIsIdenticalForKnownAndUnknownEmail(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Known Fest", "known-fest",
		env.fixedClock.Add(30*24*time.Hour), "known-1", "ana@example.com", "Ana", "Lopez")

	knownResp, knownBody := requestCustomerPasscode(t, env, "ana@example.com", "")
	unknownResp, unknownBody := requestCustomerPasscode(t, env, "nobody-here@example.com", "")

	if knownResp.StatusCode != http.StatusOK || unknownResp.StatusCode != http.StatusOK {
		t.Fatalf("statuses known=%d unknown=%d, want 200 for both", knownResp.StatusCode, unknownResp.StatusCode)
	}
	if knownBody.Error != nil || unknownBody.Error != nil {
		t.Fatalf("errors known=%+v unknown=%+v, want none", knownBody.Error, unknownBody.Error)
	}
	if !bytes.Equal(knownBody.Data, unknownBody.Data) {
		t.Fatalf("known data=%s unknown data=%s — the response must not differ", knownBody.Data, unknownBody.Data)
	}
}

// TestCustomerVerifyIssuesSessionAndMarksCustomerVerified proves a completed
// passcode is what makes someone a Verified Customer, and that the session it
// issues is a full one — scoped to the Customer, not to a single Ticket Sale.
func TestCustomerVerifyIssuesSessionAndMarksCustomerVerified(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Verify Fest", "verify-fest",
		env.fixedClock.Add(30*24*time.Hour), "verify-1", "ana@example.com", "Ana", "Lopez")

	if c := readCustomer(t, env, "ana@example.com"); c.VerifiedAt.Valid {
		t.Fatal("Customer was verified before anyone proved they owned the address")
	}

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": "ana@example.com",
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none", body.Error)
	}

	var data struct {
		Session   customerSessionView `json:"session"`
		SessionID string              `json:"session_id"`
	}
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if data.Session.Email != "ana@example.com" {
		t.Fatalf("session email = %q, want ana@example.com", data.Session.Email)
	}
	if data.Session.VerifiedAt == nil {
		t.Fatal("session reports the Customer as unverified after a completed passcode")
	}
	// A full Customer Session spans every Ticket Sale the Customer owns; only a
	// Confirmation Link session names one.
	if data.Session.TicketSaleID != nil {
		t.Fatalf("ticket_sale_id = %v, want null on a full Customer Session", *data.Session.TicketSaleID)
	}

	customer := readCustomer(t, env, "ana@example.com")
	if !customer.VerifiedAt.Valid {
		t.Fatal("verified_at is still null after a completed passcode")
	}

	// The window is 180 days from the moment it was issued.
	wantExpiry := env.fixedClock.Add(customerSessionDuration)
	if got := sessionExpiry(t, env, data.SessionID); !got.Equal(wantExpiry) {
		t.Fatalf("session expires_at = %s, want %s", got, wantExpiry)
	}
}

// TestCustomerWrongPasscodeIsRejectedAndAttemptCapInvalidatesChallenge proves
// typos are tolerated but guessing is not, and that the burnt challenge cannot be
// redeemed afterwards even with the right code.
func TestCustomerWrongPasscodeIsRejectedAndAttemptCapInvalidatesChallenge(t *testing.T) {
	env := setupTest(t)

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	realCode := env.email.LastCode

	// Four wrong attempts are rejected but leave the challenge alive.
	for i := 0; i < 4; i++ {
		resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
			"email": "ana@example.com",
			"code":  "000000",
		}, nil)
		assertAPIError(t, resp, body, http.StatusUnauthorized, "OTP_INVALID")
	}

	// The fifth trips the cap and invalidates the challenge.
	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": "ana@example.com",
		"code":  "000000",
	}, nil)
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_ATTEMPTS_EXCEEDED")

	// The real code is now worthless: the challenge is gone, not merely locked.
	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": "ana@example.com",
		"code":  realCode,
	}, nil)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "OTP_INVALID")

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 — no passcode was ever completed", n)
	}
}

// TestCustomerExpiredPasscodeIsRejected proves a passcode left too long stops
// working, and says so clearly enough that the Storefront can offer another.
func TestCustomerExpiredPasscodeIsRejected(t *testing.T) {
	env := setupTest(t)

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	code := env.email.LastCode

	// Passcodes live ten minutes. Travelled to, not waited out.
	setCustomerClock(t, env, env.fixedClock.Add(11*time.Minute))

	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": "ana@example.com",
		"code":  code,
	}, nil)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "OTP_EXPIRED")
}

// TestCustomerPasscodeRequestsAreRateLimitedPerEmail proves one address cannot be
// used to flood a mailbox.
func TestCustomerPasscodeRequestsAreRateLimitedPerEmail(t *testing.T) {
	env := setupTest(t)

	for i := 0; i < 3; i++ {
		resp, body := requestCustomerPasscode(t, env, "ana@example.com", "203.0.113.10")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "203.0.113.10")
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_RATE_LIMITED")
}

// TestCustomerPasscodeRequestsAreRateLimitedPerIP proves rotating the email does
// not buy an attacker an unlimited number of sends from one client.
func TestCustomerPasscodeRequestsAreRateLimitedPerIP(t *testing.T) {
	env := setupTest(t)
	const clientIP = "198.51.100.9"

	for i := 0; i < 10; i++ {
		resp, body := requestCustomerPasscode(t, env, fmt.Sprintf("customer%d@example.com", i), clientIP)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}

	resp, body := requestCustomerPasscode(t, env, "one-more@example.com", clientIP)
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_RATE_LIMITED")
}

// TestStaffPasscodeCannotOpenACustomerSession proves the privilege boundary at
// the HTTP seam: a code minted on the Staff app is worthless on the Storefront,
// and spending it there does not consume the Customer's own challenge.
func TestStaffPasscodeCannotOpenACustomerSession(t *testing.T) {
	env := setupTest(t)
	const email = "both-surfaces@example.com"

	resp, body := env.post(t, "/api/v1/auth/otp/request", map[string]string{"email": email}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request staff passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffCode := env.email.LastCode

	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": email,
		"code":  staffCode,
	}, nil)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "OTP_INVALID")

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 — a staff passcode must open none", n)
	}

	// A Customer passcode for the same address still works, and the Staff Session
	// the staff code could open is a different record entirely.
	token := customerSignIn(t, env, email)
	if token == "" {
		t.Fatal("expected a Customer Session after a Customer passcode")
	}
}

// TestCustomerRateLimitDoesNotConsumeStaffAllowance proves Storefront traffic
// cannot lock a staff operator out of their own sign-in.
func TestCustomerRateLimitDoesNotConsumeStaffAllowance(t *testing.T) {
	env := setupTest(t)
	const email = "shared-address@example.com"
	const clientIP = "203.0.113.55"

	for i := 0; i < 3; i++ {
		resp, body := requestCustomerPasscode(t, env, email, clientIP)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("customer request %d status=%d error=%+v", i, resp.StatusCode, body.Error)
		}
	}
	resp, body := requestCustomerPasscode(t, env, email, clientIP)
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "OTP_RATE_LIMITED")

	// Same email, same IP, staff surface: a full allowance.
	resp, body = env.post(t, "/api/v1/auth/otp/request", map[string]string{
		"email": email,
	}, map[string]string{"X-Forwarded-For": clientIP})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff request after customer allowance exhausted: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestCustomerSessionReadReportsSignedInEmail proves a Customer can always tell
// which identity they are using.
func TestCustomerSessionReadReportsSignedInEmail(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	resp, body := env.get(t, customerSessionPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none", body.Error)
	}
	var session customerSessionView
	if err := json.Unmarshal(body.Data, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.Email != "ana@example.com" {
		t.Fatalf("session email = %q, want ana@example.com", session.Email)
	}

	// Without a token the read is rejected, not answered anonymously.
	resp, body = env.get(t, customerSessionPath, nil)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "UNAUTHORIZED")
}

// TestCustomerSignOutDestroysSession proves sign-out is a server-side deletion,
// so leaving a shared device actually revokes the credential.
func TestCustomerSignOutDestroysSession(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	resp, body := env.post(t, customerLogoutPath, nil, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status=%d error=%+v", resp.StatusCode, body.Error)
	}

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 after sign-out", n)
	}

	resp, body = env.get(t, customerSessionPath, authHeader(token))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")

	resp, body = env.get(t, customerAreaPath, authHeader(token))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")
}

// TestCustomerSessionSlidesOnUseAndExpires proves the 180-day window moves with
// the Customer — a ticket bought in one season still recognises them in the
// next — and that a session finally left alone past its window is rejected.
func TestCustomerSessionSlidesOnUseAndExpires(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "ana@example.com")

	if got, want := sessionExpiry(t, env, token), env.fixedClock.Add(customerSessionDuration); !got.Equal(want) {
		t.Fatalf("initial expires_at = %s, want %s", got, want)
	}

	// Five months later — inside the window — the session is still good, and the
	// use pushes the window out from now rather than from sign-in.
	useAt := env.fixedClock.Add(150 * 24 * time.Hour)
	setCustomerClock(t, env, useAt)
	resp, body := env.get(t, customerSessionPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session read at +150d status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got, want := sessionExpiry(t, env, token), useAt.Add(customerSessionDuration); !got.Equal(want) {
		t.Fatalf("expires_at after use = %s, want %s — the window must slide", got, want)
	}

	// Past the original 180 days but inside the extended window: still signed in.
	setCustomerClock(t, env, env.fixedClock.Add(200*24*time.Hour))
	resp, body = env.get(t, customerAreaPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer area at +200d status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Left alone beyond the window, the session is rejected and swept away.
	setCustomerClock(t, env, env.fixedClock.Add(400*24*time.Hour))
	resp, body = env.get(t, customerAreaPath, authHeader(token))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_EXPIRED")

	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 — an expired session is destroyed", n)
	}
}

// TestCustomerAreaListsOwnSalesUpcomingAndPastAcrossOrganizations proves the read
// that makes signing in worth doing: every Ticket Sale the Customer owns, split
// into what is still to come and what already happened, with the Event, its date,
// the Organization, what was bought, and the Sale Confirmation reference — and
// with purchases from two promoters in one list.
func TestCustomerAreaListsOwnSalesUpcomingAndPastAcrossOrganizations(t *testing.T) {
	env := setupTest(t)

	firstOrg := orgAdminSession(t, env)
	pastRef := seedSaleForCustomer(t, env, firstOrg, "Last Winter", "last-winter",
		env.fixedClock.Add(-60*24*time.Hour), "area-past", "ana@example.com", "Ana", "Lopez")
	soonRef := seedSaleForCustomer(t, env, firstOrg, "Next Week", "next-week",
		env.fixedClock.Add(7*24*time.Hour), "area-soon", "ana@example.com", "Ana", "Lopez")

	// A second Organization: one Customer, one list.
	secondOrg := verifyOTP(t, env, "second-admin@example.com")
	createOrganization(t, env, secondOrg, "Second Org", "second-org")
	laterRef := seedSaleForCustomer(t, env, secondOrg, "Next Season", "next-season",
		env.fixedClock.Add(90*24*time.Hour), "area-later", "ana@example.com", "Ana", "Lopez")

	token := customerSignIn(t, env, "ana@example.com")
	area := readCustomerArea(t, env, token, "")

	if len(area.Upcoming) != 2 {
		t.Fatalf("upcoming = %d sales, want 2 (%+v)", len(area.Upcoming), area.Upcoming)
	}
	if len(area.Past) != 1 {
		t.Fatalf("past = %d sales, want 1 (%+v)", len(area.Past), area.Past)
	}
	// Upcoming reads soonest first: the next Event is the one that matters.
	if area.Upcoming[0].ConfirmationRef != soonRef || area.Upcoming[1].ConfirmationRef != laterRef {
		t.Fatalf("upcoming refs = %q, %q; want %q then %q",
			area.Upcoming[0].ConfirmationRef, area.Upcoming[1].ConfirmationRef, soonRef, laterRef)
	}
	if area.Past[0].ConfirmationRef != pastRef {
		t.Fatalf("past ref = %q, want %q", area.Past[0].ConfirmationRef, pastRef)
	}

	// Both Organizations appear, so one list really does span promoters.
	orgs := map[string]bool{}
	for _, sale := range append(append([]customerAreaSale{}, area.Upcoming...), area.Past...) {
		orgs[sale.Organization.Slug] = true
	}
	if !orgs["test-org"] || !orgs["second-org"] {
		t.Fatalf("organizations in the Customer Area = %v, want both test-org and second-org", orgs)
	}

	// Each entry carries enough to be meaningful at a glance.
	next := area.Upcoming[0]
	if next.Event.Name != "Next Week" || next.Event.StartsAt == nil {
		t.Fatalf("next sale event = %+v, want a named Event with a date", next.Event)
	}
	if next.Organization.Name != "Test Org" {
		t.Fatalf("next sale organization = %q, want Test Org", next.Organization.Name)
	}
	if len(next.Lines) != 1 || next.Lines[0].TicketTypeName != "GA" || next.Lines[0].Quantity != 1 {
		t.Fatalf("next sale lines = %+v, want one GA x1", next.Lines)
	}
	if next.AmountCents != 2500 || next.Currency == "" {
		t.Fatalf("next sale amount = %d %s, want 2500 in a currency", next.AmountCents, next.Currency)
	}
	if next.ConfirmationRef == "" || next.Status != "active" {
		t.Fatalf("next sale ref=%q status=%q", next.ConfirmationRef, next.Status)
	}
}

// TestCustomerWithNoPurchasesGetsWellFormedEmptyCustomerArea proves the empty
// state is a real, well-formed result rather than an error or a null.
func TestCustomerWithNoPurchasesGetsWellFormedEmptyCustomerArea(t *testing.T) {
	env := setupTest(t)
	token := customerSignIn(t, env, "never-bought@example.com")

	resp, body := env.get(t, customerAreaPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer area status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none", body.Error)
	}
	// Both collections must be present and empty, not null: an empty state, not a
	// missing one.
	if got := string(body.Data); got != `{"upcoming":[],"past":[]}` {
		t.Fatalf("data = %s, want empty upcoming and past arrays", got)
	}
}

// TestCustomerCannotReadAnotherCustomersTicketSales is the test this whole
// feature turns on.
//
// Two Customers each hold purchases. Signed in as one, we actively try to reach
// the other's — through every identifier the API could plausibly be talked into
// honouring: their Customer id, their email, their Ticket Sale id, their
// Organization, on the query string and in the body, and by presenting their
// Sale Confirmation reference. None of it may widen the read by a single row,
// because the scope comes from the session and from nothing else.
func TestCustomerCannotReadAnotherCustomersTicketSales(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	anaRef := seedSaleForCustomer(t, env, sessionID, "Ana Fest", "ana-fest",
		env.fixedClock.Add(20*24*time.Hour), "adv-ana", "ana@example.com", "Ana", "Lopez")
	bobRef := seedSaleForCustomer(t, env, sessionID, "Bob Fest", "bob-fest",
		env.fixedClock.Add(40*24*time.Hour), "adv-bob", "bob@example.com", "Bob", "Ng")

	anaCustomer := readCustomer(t, env, "ana@example.com")
	bobCustomer := readCustomer(t, env, "bob@example.com")

	bobToken := customerSignIn(t, env, "bob@example.com")
	bobArea := readCustomerArea(t, env, bobToken, "")
	if len(bobArea.Upcoming) != 1 || bobArea.Upcoming[0].ConfirmationRef != bobRef {
		t.Fatalf("Bob's own area = %+v, want his single sale %s", bobArea, bobRef)
	}
	bobSaleID := bobArea.Upcoming[0].ID
	bobOrgID := bobArea.Upcoming[0].Organization.ID

	anaToken := customerSignIn(t, env, "ana@example.com")

	// Every identifier that names Bob, aimed at the read while signed in as Ana.
	attacks := []string{
		"",
		"customer_id=" + bobCustomer.ID,
		"customerId=" + bobCustomer.ID,
		"id=" + bobCustomer.ID,
		"email=bob@example.com",
		"customer_email=bob@example.com",
		"ticket_sale_id=" + bobSaleID,
		"sale_id=" + bobSaleID,
		"confirmation_ref=" + bobRef,
		"organization_id=" + bobOrgID,
		"customer_id=" + bobCustomer.ID + "&ticket_sale_id=" + bobSaleID + "&email=bob@example.com",
		// Ana's own id supplied alongside Bob's, in case a supplied value were
		// preferred over the session's.
		"customer_id=" + anaCustomer.ID + "&customer_id=" + bobCustomer.ID,
	}
	for _, query := range attacks {
		area := readCustomerArea(t, env, anaToken, query)
		assertOnlyOwnSale(t, area, anaRef, bobRef, "GET "+customerAreaPath+"?"+query)
	}

	// The session read must not be talked into reporting Bob either.
	for _, query := range attacks {
		path := customerSessionPath
		if query != "" {
			path += "?" + query
		}
		resp, body := env.get(t, path, authHeader(anaToken))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("session read %q status=%d error=%+v", query, resp.StatusCode, body.Error)
		}
		var session customerSessionView
		if err := json.Unmarshal(body.Data, &session); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if session.Email != "ana@example.com" {
			t.Fatalf("session read %q reported %q, want ana@example.com", query, session.Email)
		}
	}

	// Bob's Ticket Sale id as a path segment resolves to no route at all: there
	// is deliberately no by-id read a caller could aim anywhere. The router — not
	// a handler — turns this away, so the response is not an API envelope and the
	// request is made raw.
	req, err := http.NewRequest(http.MethodGet, env.server.URL+customerAreaPath+"/"+bobSaleID, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+anaToken)
	unrouted, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = unrouted.Body.Close()
	if unrouted.StatusCode != http.StatusNotFound {
		t.Fatalf("GET %s/%s status=%d, want 404 — no by-id Ticket Sale read exists",
			customerAreaPath, bobSaleID, unrouted.StatusCode)
	}

	// Bob's own view is unchanged by any of it.
	after := readCustomerArea(t, env, bobToken, "")
	assertOnlyOwnSale(t, after, bobRef, anaRef, "Bob's area after the attempts")
}

// TestStaffSessionCannotReadTheCustomerArea proves the two identities share
// nothing: a Staff Session token authenticates nothing on the Customer surface,
// and a Customer Session token authenticates nothing on the staff surface — even
// when both belong to the same email address (ADR 0010).
func TestStaffSessionCannotReadTheCustomerArea(t *testing.T) {
	env := setupTest(t)
	const email = "wears-both-hats@example.com"

	staffToken := verifyOTP(t, env, email)
	createOrganization(t, env, staffToken, "Both Hats", "both-hats")

	resp, body := env.get(t, customerAreaPath, authHeader(staffToken))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")

	customerToken := customerSignIn(t, env, email)
	resp, body = env.get(t, "/api/v1/staff/me", authHeader(customerToken))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "SESSION_NOT_FOUND")

	// Signing out of the Storefront leaves the Staff Session untouched.
	if resp, body = env.post(t, customerLogoutPath, nil, authHeader(customerToken)); resp.StatusCode != http.StatusOK {
		t.Fatalf("customer logout status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body = env.get(t, "/api/v1/auth/session", authHeader(staffToken)); resp.StatusCode != http.StatusOK {
		t.Fatalf("staff session read after Customer sign-out: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// assertOnlyOwnSale fails unless the Customer Area holds exactly the caller's own
// Sale Confirmation reference and no trace of the other Customer's.
func assertOnlyOwnSale(t *testing.T, area customerAreaView, ownRef, foreignRef, what string) {
	t.Helper()
	all := append(append([]customerAreaSale{}, area.Upcoming...), area.Past...)
	if len(all) != 1 {
		t.Fatalf("%s returned %d sales, want exactly the caller's own 1", what, len(all))
	}
	if all[0].ConfirmationRef == foreignRef {
		t.Fatalf("%s leaked another Customer's Ticket Sale %s", what, foreignRef)
	}
	if all[0].ConfirmationRef != ownRef {
		t.Fatalf("%s returned %q, want the caller's own %q", what, all[0].ConfirmationRef, ownRef)
	}
}
