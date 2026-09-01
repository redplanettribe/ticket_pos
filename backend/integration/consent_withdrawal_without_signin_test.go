package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Withdrawing a consent on Proof of Email Ownership alone (#270, parent #265,
// ADR 0039).
//
// THE PROPERTY UNDER TEST IS A CONDITIONAL, AND IT IS TESTED IN BOTH
// DIRECTIONS. `POLICY_ACCEPTANCE_REQUIRED` used to be an unconditional invariant
// of the consent submission endpoint; it is now required unless every answer
// present in the submission is a denial. A suite that only proved the
// denials-only submission succeeds would pass just as well against an endpoint
// that had stopped checking acceptance at all — which is the exact failure ADR
// 0039 says to test for — so every relaxation below is paired with a refusal
// that must still bite.
//
// The seam is HTTP, as always. The evidence log and the consent columns are read
// through the harness database handle because the platform publishes neither,
// and the mail through the capture sender, exactly as the consent tests already
// established.

const consentWithdrawalProofPath = "/api/v1/customer/consent/withdrawal/passcode/verify"

// consentWithdrawalProof is what the withdrawal surface's passcode door answers
// with. It is asserted as a whole in one test below: what is NOT in this struct
// — a session, a session id, the boxes — is the point of the endpoint.
type consentWithdrawalProof struct {
	PendingConsentToken string `json:"pending_consent_token"`
	ExpiresAt           string `json:"expires_at"`
}

// consentWithdrawalOutcome is the `withdrawal` shape the consent submission
// endpoint answers a denials-only submission with.
type consentWithdrawalOutcome struct {
	MarketingConsent  string `json:"marketing_consent"`
	NetworkingConsent string `json:"networking_consent"`
	Withdrew          struct {
		MarketingConsent  bool `json:"marketing_consent"`
		NetworkingConsent bool `json:"networking_consent"`
	} `json:"withdrew"`
}

// consentSubmissionData is the whole of what the submission endpoint returns,
// with both shapes on it: a session, or a withdrawal. Kept as one struct because
// the assertion that matters most is about the fields that are ABSENT on the
// withdrawal branch.
type consentSubmissionData struct {
	Session         *customerSessionView      `json:"session"`
	SessionID       string                    `json:"session_id"`
	ConsentRequired *consentRequiredOutcome   `json:"consent_required"`
	Withdrawal      *consentWithdrawalOutcome `json:"withdrawal"`
}

func decodeConsentSubmission(t *testing.T, body envelope) consentSubmissionData {
	t.Helper()
	var data consentSubmissionData
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode consent submission data: %v", err)
	}
	return data
}

// proveEmailForWithdrawal requests a passcode and redeems it at the withdrawal
// surface's own door, returning the pending-consent token it hands back.
//
// It goes through the ordinary passcode request endpoint, because it is the
// ordinary passcode: this feature adds a way to SPEND a proof of email
// ownership, not a second way to obtain one.
func proveEmailForWithdrawal(t *testing.T, env *testEnv, email, locale string) consentWithdrawalProof {
	t.Helper()
	resp, body := requestCustomerPasscode(t, env, email, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	payload := map[string]string{"email": email, "code": env.email.LastCode}
	if locale != "" {
		payload["locale"] = locale
	}
	resp, body = env.post(t, consentWithdrawalProofPath, payload, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdrawal passcode verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var proof consentWithdrawalProof
	if err := json.Unmarshal(body.Data, &proof); err != nil {
		t.Fatalf("decode withdrawal proof: %v", err)
	}
	if proof.PendingConsentToken == "" {
		t.Fatal("the withdrawal door returned no pending-consent token")
	}
	return proof
}

// withdrawalSubmission is a denials-only submission as the withdrawal surface
// sends it: the token, and a `false` for each consent being taken away.
//
// A consent NOT being withdrawn is ABSENT rather than false, which is the whole
// wire convention of this surface: absent means "not on this submission" and
// leaves the consent exactly as it stands, so nothing here can churn an answer
// the person did not mention.
func withdrawalSubmission(token string, marketing, networking bool) map[string]any {
	body := map[string]any{"pending_consent_token": token}
	if marketing {
		body["marketing_consent"] = false
	}
	if networking {
		body["networking_consent"] = false
	}
	return body
}

// TestWithdrawalWithoutSigningInNeedsNoPolicyAcceptance is the acceptance
// criterion the whole ticket exists for, and it is set up as the bad sentence it
// abolishes: a Customer who granted both consents, then had a new Policy Version
// published under them, so that signing in would demand they accept the new
// edition before they could disagree with anything.
func TestWithdrawalWithoutSigningInNeedsNoPolicyAcceptance(t *testing.T) {
	env := setupTest(t)

	signInAnswering(t, env, "ana@example.com", true, true, true)
	// An hour later, so the two acts are distinguishable by their own timestamps.
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))
	publishPolicyVersion(t, env, 2, 0)
	sessionsBefore := countCustomerSessions(t, env)

	// The re-gated Customer proves their address and is offered no session.
	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	if n := countCustomerSessions(t, env); n != sessionsBefore {
		t.Fatalf("customer sessions = %d, want the %d that already existed — proving an address to withdraw must not sign anybody in",
			n, sessionsBefore)
	}

	// And withdraws both, WITHOUT ACCEPTING THE POLICY VERSION THAT RE-GATED
	// THEM. This is the request that used to be refused.
	resp, body := env.post(t, customerConsentPath,
		withdrawalSubmission(proof.PendingConsentToken, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("denials-only submission status=%d error=%+v — a submission that grants nothing needs no acceptance", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none", body.Error)
	}

	submitted := decodeConsentSubmission(t, body)
	if submitted.Withdrawal == nil {
		t.Fatal("expected a withdrawal outcome")
	}
	if submitted.Withdrawal.MarketingConsent != "denied" || submitted.Withdrawal.NetworkingConsent != "denied" {
		t.Fatalf("states after the withdrawal = %+v, want both denied", submitted.Withdrawal)
	}
	if !submitted.Withdrawal.Withdrew.MarketingConsent || !submitted.Withdrawal.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v, want both — the act moved both out of granted", submitted.Withdrawal.Withdrew)
	}

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" || state.NetworkingConsent.String != "denied" {
		t.Fatalf("consent state = %+v, want both denied", state)
	}
	// Networking Consent moved back to denied, which nothing in the platform
	// could do before this feature: the one-way door is open.
	if state.DigestEnabled {
		t.Fatal("digest_enabled is still true after Marketing Consent was withdrawn — the two are one switch (ADR 0034)")
	}
	// The Policy Acceptance is untouched: withdrawing is not accepting, and it is
	// not un-accepting either. The Customer still stands where the new edition
	// left them.
	if state.PolicyVersionID.String == currentPolicyVersionID(t, env) {
		t.Fatal("the withdrawal recorded an acceptance of the current Policy Version — it must accept nothing")
	}
}

// TestWithdrawalWithoutSigningInMintsNoSession is user story 23 stated as a
// property of the request rather than of a page: proving an address in order to
// withdraw must not leave anybody signed in on a shared machine.
func TestWithdrawalWithoutSigningInMintsNoSession(t *testing.T) {
	env := setupTest(t)

	// A Customer who has ALREADY accepted the current edition, and would
	// therefore be signed straight in by the ordinary verify. This is the case
	// that fails against an implementation which reached for the sign-in door and
	// discarded the session afterwards.
	signInAnswering(t, env, "ana@example.com", true, true, true)
	sessionsAfterSignIn := countCustomerSessions(t, env)

	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	if n := countCustomerSessions(t, env); n != sessionsAfterSignIn {
		t.Fatalf("customer sessions = %d after proving an address to withdraw, want the %d that already existed",
			n, sessionsAfterSignIn)
	}

	resp, body := env.post(t, customerConsentPath,
		withdrawalSubmission(proof.PendingConsentToken, true, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("denials-only submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	submitted := decodeConsentSubmission(t, body)
	if submitted.SessionID != "" {
		t.Fatalf("session_id = %q — a withdrawal must mint no Customer Session", submitted.SessionID)
	}
	if submitted.Session != nil {
		t.Fatalf("session = %+v, want null", submitted.Session)
	}
	if submitted.ConsentRequired != nil {
		t.Fatalf("consent_required = %+v, want null", submitted.ConsentRequired)
	}
	if n := countCustomerSessions(t, env); n != sessionsAfterSignIn {
		t.Fatalf("customer sessions = %d after the withdrawal, want the %d that already existed", n, sessionsAfterSignIn)
	}
}

// TestWithdrawalRecordsTheEvidenceAndTheProof is user story 24: the easier route
// must not be the weaker one. The row this act writes carries everything a
// signed-in withdrawal's row carries — prior state, technical proof, the
// resolved Policy Version — and the two fields it leaves null are the two that
// distinguish it: no acceptance, and no session.
func TestWithdrawalRecordsTheEvidenceAndTheProof(t *testing.T) {
	env := setupTest(t)

	signInAnswering(t, env, "ana@example.com", true, true, true)
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	resp, body := env.post(t, customerConsentPath,
		withdrawalSubmission(proof.PendingConsentToken, false, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("denials-only submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	records := consentRecordsOn(t, env, "ana@example.com", "passcode_withdrawal")
	if len(records) != 1 {
		t.Fatalf("passcode withdrawal records = %d, want the withdrawal alone — the sign-in is filed under its own surface", len(records))
	}
	record := records[0]

	if record.Email != "ana@example.com" {
		t.Fatalf("record email = %q", record.Email)
	}
	if record.PolicyVersionID != currentPolicyVersionID(t, env) {
		t.Fatalf("policy_version_id = %q, want the current edition — resolved server-side on every act", record.PolicyVersionID)
	}
	// NOT AN ACCEPTANCE. The box was not shown and was not answered, and null is
	// what says so — a false here would record a Customer as having REFUSED the
	// Privacy Policy, which is not what withdrawing a marketing consent means.
	if record.PolicyAcceptance.Valid {
		t.Fatalf("policy_acceptance = %+v on a withdrawal, want NULL — the box was not shown", record.PolicyAcceptance)
	}
	// The consent that was withdrawn, answered No; the one that was not on the
	// submission, null.
	if !record.NetworkingConsent.Valid || record.NetworkingConsent.Bool {
		t.Fatalf("networking_consent = %+v, want an explicit false", record.NetworkingConsent)
	}
	if record.MarketingConsent.Valid {
		t.Fatalf("marketing_consent = %+v, want NULL — it was not on this submission", record.MarketingConsent)
	}
	// What it took away, legible from this row alone (#266).
	wantPrior(t, record, "", "granted")
	if !record.EmailProven {
		t.Fatal("email_proven = false — a passcode is Proof of Email Ownership whichever door redeems it")
	}
	// The technical proof, every field the guidance's template asks for and that
	// this surface can have.
	if record.IP.String != "198.51.100.24" {
		t.Fatalf("ip = %q, want the address the BFF derived", record.IP.String)
	}
	if record.UserAgent.String != "Mozilla/5.0 (consent-test)" {
		t.Fatalf("user_agent = %q", record.UserAgent.String)
	}
	if record.OriginURL.String != "http://storefront.example/es/signin" {
		t.Fatalf("origin_url = %q", record.OriginURL.String)
	}
	// And no session, because none exists. This null and the null acceptance
	// above are together what tells this act from a sign-in on the same channel.
	if record.SessionID.Valid {
		t.Fatalf("session_id = %q on an act that minted no session", record.SessionID.String)
	}

	// The Marketing Consent this submission did not name is exactly as it was.
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q — a consent absent from the submission must not be churned", state.MarketingConsent.String)
	}
}

// TestWithdrawalWithoutSigningInIsConfirmedByEmail: the withdrawal is confirmed
// like any other, through the mechanism #267 built — and the message names what
// THIS act took away, which on this surface can be a consent the two older
// surfaces cannot touch.
func TestWithdrawalWithoutSigningInIsConfirmedByEmail(t *testing.T) {
	env := setupTest(t)

	signInAnsweringInLocale(t, env, "ana@example.com", "es", true, true, true)
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "es")
	resp, body := env.post(t, customerConsentPath,
		withdrawalSubmission(proof.PendingConsentToken, false, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("denials-only submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	confirmation := onlyWithdrawalConfirmation(t, env, "ana@example.com")
	// It names NETWORKING, and it is in the Customer's Mail Locale. A message
	// saying "your marketing consent has been withdrawn" would be false about the
	// act that produced it.
	if !strings.Contains(strings.ToLower(confirmation.Text()), "consentimiento de networking") {
		t.Fatalf("the confirmation does not name the Networking Consent it withdrew; body:\n%s", confirmation.Text())
	}
	if strings.Contains(strings.ToLower(confirmation.Text()), "correos de marketing") {
		t.Fatalf("the confirmation claims marketing was withdrawn, which this act did not do; body:\n%s", confirmation.Text())
	}
	// And the evidence records that they were told, on the row the act wrote.
	records := consentRecordsOn(t, env, "ana@example.com", "passcode_withdrawal")
	withdrawal := records[len(records)-1]
	if !withdrawal.ConfirmationSentAt.Valid {
		t.Fatalf("the withdrawal record carries no confirmation_sent_at: %+v", withdrawal)
	}
}

// TestWithdrawalThatMovedNothingIsRecordedAndSendsNothing: a consent that was
// already denied is withdrawn again. The act happened and the log says so; the
// change did not happen and nobody is told one did.
func TestWithdrawalThatMovedNothingIsRecordedAndSendsNothing(t *testing.T) {
	env := setupTest(t)

	signInAnswering(t, env, "ana@example.com", true, false, false)
	// An hour later, so the two acts on this channel are ordered by their own
	// timestamps rather than by the uuid tie-break a shared clock leaves.
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	resp, body := env.post(t, customerConsentPath,
		withdrawalSubmission(proof.PendingConsentToken, true, true), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("denials-only submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	submitted := decodeConsentSubmission(t, body)
	if submitted.Withdrawal.Withdrew.MarketingConsent || submitted.Withdrawal.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v, want neither — both consents were already denied", submitted.Withdrawal.Withdrew)
	}

	wantNoWithdrawalConfirmation(t, env, "ana@example.com",
		"both consents were already denied, so the act took nothing away")

	records := consentRecordsOn(t, env, "ana@example.com", "passcode_withdrawal")
	if len(records) != 1 {
		t.Fatalf("passcode withdrawal records = %d, want the act that moved nothing", len(records))
	}
	if records[0].ConfirmationSentAt.Valid {
		t.Fatalf("confirmation_sent_at is stamped on an act that sent nothing: %+v", records[0])
	}
	wantPrior(t, records[0], "denied", "denied")
}

// TestSubmissionContainingAGrantIsStillRefusedWithoutAcceptance is the OTHER
// direction of the conditional, and the test that must be impossible to pass if
// somebody later widens the condition.
//
// Each submission below carries at least one denial — so an implementation that
// asked "is there a denial here?" instead of "is EVERY answer a denial?" would
// let all of them through — and each also carries something that authorizes:
// which is what Policy Acceptance evidences having been informed about, and is
// exactly what the relaxation must not reach.
func TestSubmissionContainingAGrantIsStillRefusedWithoutAcceptance(t *testing.T) {
	for _, submission := range []struct {
		name string
		body func(token string) map[string]any
	}{
		{
			name: "a denial beside a grant",
			body: func(token string) map[string]any {
				return map[string]any{
					"pending_consent_token": token,
					"marketing_consent":     true,
					"networking_consent":    false,
				}
			},
		},
		{
			name: "a grant alone",
			body: func(token string) map[string]any {
				return map[string]any{
					"pending_consent_token": token,
					"networking_consent":    true,
				}
			},
		},
		{
			name: "no answers at all",
			// A bare token names nothing to take away. Reading "no answers" as
			// "every answer is a denial" would make the empty submission — the one
			// this gate has refused since it existed — start succeeding.
			body: func(token string) map[string]any {
				return map[string]any{"pending_consent_token": token}
			},
		},
		{
			name: "a policy acceptance that is refused",
			// `policy_acceptance: false` is not an acceptance and is not a denial of
			// an optional consent either. The submission is a sign-in, and a sign-in
			// without acceptance is refused.
			body: func(token string) map[string]any {
				return map[string]any{
					"pending_consent_token": token,
					"policy_acceptance":     false,
					"marketing_consent":     false,
				}
			},
		},
	} {
		t.Run(submission.name, func(t *testing.T) {
			env := setupTest(t)

			data := startSignIn(t, env, "ana@example.com")
			token := data.ConsentRequired.PendingConsentToken

			resp, body := env.post(t, customerConsentPath, submission.body(token), consentEvidenceHeaders())
			assertAPIError(t, resp, body, http.StatusBadRequest, "POLICY_ACCEPTANCE_REQUIRED")

			if n := countCustomerSessions(t, env); n != 0 {
				t.Fatalf("customer sessions = %d, want 0 after a refused submission", n)
			}
			if records := readConsentRecords(t, env, "ana@example.com"); len(records) != 0 {
				t.Fatalf("consent records = %d, want 0 — a refused submission records nothing", len(records))
			}
			state := readConsentState(t, env, "ana@example.com")
			if state.PolicyAcceptedAt.Valid || state.MarketingConsent.Valid || state.NetworkingConsent.Valid {
				t.Fatalf("a refused submission wrote state: %+v", state)
			}
		})
	}
}

// TestSubmissionCarryingAPolicyAcceptanceFollowsTheExistingRules: nothing about
// the sign-in path changed. A submission with acceptance still mints a session,
// still records the acceptance, and still reads an omitted optional box as the
// explicit No it has always been on that surface.
func TestSubmissionCarryingAPolicyAcceptanceFollowsTheExistingRules(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "ana@example.com")
	resp, body := env.post(t, customerConsentPath, map[string]any{
		"pending_consent_token": data.ConsentRequired.PendingConsentToken,
		"policy_acceptance":     true,
		// The Terms box rides the same step since #536 and is owed by every
		// first sign-in; this test is about the policy path's rules, which are
		// unchanged around it.
		"terms_acceptance":  true,
		"marketing_consent": true,
		// networking_consent omitted: shown and left unticked on the sign-in
		// surface, which is a refusal there and is recorded as one.
	}, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}

	submitted := decodeConsentSubmission(t, body)
	if submitted.SessionID == "" {
		t.Fatal("expected a Customer Session from a submission carrying a Policy Acceptance")
	}
	if submitted.Withdrawal != nil {
		t.Fatalf("withdrawal = %+v, want null on a sign-in submission", submitted.Withdrawal)
	}

	state := readConsentState(t, env, "ana@example.com")
	if !state.PolicyAcceptedAt.Valid {
		t.Fatal("the acceptance was not recorded")
	}
	if state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q, want granted", state.MarketingConsent.String)
	}
	if state.NetworkingConsent.String != "denied" {
		t.Fatalf("networking_consent = %q — an omitted box on the sign-in surface is still an explicit No", state.NetworkingConsent.String)
	}
}

// TestWithdrawalSurfaceCannotGrantAnything is the criterion that decides what an
// intercepted passcode is worth: everything this door can reach, it can only
// switch OFF.
func TestWithdrawalSurfaceCannotGrantAnything(t *testing.T) {
	env := setupTest(t)

	// A Customer who has answered No to both, so that a grant would be visible as
	// a change.
	signInAnswering(t, env, "ana@example.com", true, false, false)

	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	// The token from the WITHDRAWAL door, spent on a submission that asks for
	// grants. It is not a withdrawal, so it is judged as a sign-in — and refused,
	// because it carries no acceptance.
	resp, body := env.post(t, customerConsentPath, map[string]any{
		"pending_consent_token": proof.PendingConsentToken,
		"marketing_consent":     true,
		"networking_consent":    true,
	}, consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusBadRequest, "POLICY_ACCEPTANCE_REQUIRED")

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" || state.NetworkingConsent.String != "denied" {
		t.Fatalf("consent state = %+v — the withdrawal door granted something", state)
	}
}

// TestWithdrawalTokenIsSpentWhateverTheOutcome: the proof is single-use here
// exactly as it is on the sign-in door, on the refused path as well as the
// successful one, so a stolen token cannot be tried twice for a different
// answer.
func TestWithdrawalTokenIsSpentWhateverTheOutcome(t *testing.T) {
	env := setupTest(t)

	signInAnswering(t, env, "ana@example.com", true, true, true)

	// Spent by a successful withdrawal.
	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	resp, body := env.post(t, customerConsentPath,
		withdrawalSubmission(proof.PendingConsentToken, true, false), consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("denials-only submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.post(t, customerConsentPath,
		withdrawalSubmission(proof.PendingConsentToken, false, true), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusUnauthorized, "PENDING_CONSENT_INVALID")

	// And spent by a refused one: the bare token below is refused for missing
	// acceptance, and the same token is worthless afterwards.
	refused := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	resp, body = env.post(t, customerConsentPath,
		map[string]any{"pending_consent_token": refused.PendingConsentToken}, consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusBadRequest, "POLICY_ACCEPTANCE_REQUIRED")
	resp, body = env.post(t, customerConsentPath,
		withdrawalSubmission(refused.PendingConsentToken, false, true), consentEvidenceHeaders())
	assertAPIError(t, resp, body, http.StatusUnauthorized, "PENDING_CONSENT_INVALID")

	// The one withdrawal that succeeded is the only act in the log for it, and it
	// is filed under its own surface rather than under the door it came through
	// (migration 068).
	if records := consentRecordsOn(t, env, "ana@example.com", "passcode_withdrawal"); len(records) != 1 {
		t.Fatalf("passcode withdrawal records = %d, want the single withdrawal", len(records))
	}
	if records := consentRecordsOn(t, env, "ana@example.com", "signin"); len(records) != 1 {
		t.Fatalf("signin records = %d, want only the sign-in", len(records))
	}
	if n := countPendingConsents(t, env); n != 0 {
		t.Fatalf("pending consents = %d, want 0 — every token here has been spent", n)
	}
}

// TestAbandonedWithdrawalSurfaceRecordsNothing: reading about your rights is not
// itself an act with consequences (parent story 14, applied to this surface).
func TestAbandonedWithdrawalSurfaceRecordsNothing(t *testing.T) {
	env := setupTest(t)

	signInAnswering(t, env, "ana@example.com", true, true, true)
	proveEmailForWithdrawal(t, env, "ana@example.com", "")

	// ... and nothing else happens. The tab is closed.

	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "granted" || state.NetworkingConsent.String != "granted" {
		t.Fatalf("consent state = %+v — abandoning the surface changed something", state)
	}
	if records := consentRecordsOn(t, env, "ana@example.com", "signin"); len(records) != 1 {
		t.Fatalf("signin records = %d, want only the sign-in — being offered a withdrawal is not making one", len(records))
	}
	if records := consentRecordsOn(t, env, "ana@example.com", "passcode_withdrawal"); len(records) != 0 {
		t.Fatalf("passcode withdrawal records = %d, want none — the tab was closed", len(records))
	}
	wantNoWithdrawalConfirmation(t, env, "ana@example.com", "nothing was withdrawn")
}

// TestWithdrawalDoorReturnsAProofAndNothingElse holds the shape of the passcode
// door: a token, an expiry, and no session by construction. The expiry is the
// pending-consent token's own short window, because this IS that credential
// rather than a longer-lived cousin of it.
func TestWithdrawalDoorReturnsAProofAndNothingElse(t *testing.T) {
	env := setupTest(t)

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.post(t, consentWithdrawalProofPath, map[string]string{
		"email": "ana@example.com",
		"code":  env.email.LastCode,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdrawal passcode verify status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Nothing resembling a session comes back, and the assertion is on the raw
	// payload rather than on a decoded struct: a typed decode would silently drop
	// a `session_id` this endpoint must never emit.
	var payload map[string]any
	if err := json.Unmarshal(body.Data, &payload); err != nil {
		t.Fatalf("decode withdrawal proof: %v", err)
	}
	for _, forbidden := range []string{"session", "session_id", "boxes", "consent_required"} {
		if _, present := payload[forbidden]; present {
			t.Fatalf("the withdrawal door published %q: %s", forbidden, body.Data)
		}
	}
	if payload["pending_consent_token"] == "" || payload["expires_at"] == "" {
		t.Fatalf("withdrawal proof = %s, want a token and an expiry", body.Data)
	}
	expires, err := time.Parse(time.RFC3339, payload["expires_at"].(string))
	if err != nil {
		t.Fatalf("parse expires_at %v: %v", payload["expires_at"], err)
	}
	if !expires.After(env.fixedClock) || expires.After(env.fixedClock.Add(time.Hour)) {
		t.Fatalf("expires_at = %s, want a short window after %s", expires, env.fixedClock)
	}

	// The proof of ownership still happened: the Customer exists and is verified.
	// Only the session was never on offer.
	if c := readCustomer(t, env, "ana@example.com"); !c.VerifiedAt.Valid {
		t.Fatal("a proven passcode must stamp verified_at on this door too")
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0", n)
	}
}

// TestWithdrawalDoorRefusesAWrongPasscode: this surface is reachable only past a
// correct code, so it is not a way to learn anything about an address that
// signing in would not already tell you.
func TestWithdrawalDoorRefusesAWrongPasscode(t *testing.T) {
	env := setupTest(t)

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.post(t, consentWithdrawalProofPath, map[string]string{
		"email": "ana@example.com",
		"code":  "000000",
	}, nil)
	if body.Error == nil {
		t.Fatalf("a wrong passcode was accepted: %s", body.Data)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a wrong passcode", resp.StatusCode)
	}
	if n := countPendingConsents(t, env); n != 0 {
		t.Fatalf("pending consents = %d, want 0 — a wrong passcode buys no proof", n)
	}
}
