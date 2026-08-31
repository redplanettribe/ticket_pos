package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// The Operator records a Consent Withdrawal that arrived off-platform (#271,
// parent #265).
//
// WHAT THIS SURFACE IS FOR. Counsel published a withdrawal form and a
// data-protection address, and both of them post paper and email at a platform
// that could previously honour neither. The only way to action a mailed-in form
// was to edit the database by hand, which writes no evidence at all — and an
// evidence log with a hole exactly where the unusual cases went is worse than no
// log, because it is confidently wrong.
//
// THE THREE PROPERTIES UNDER TEST, none of which is about a screen:
//
//   - IT CAN ONLY WITHDRAW. An Operator cannot manufacture consent, and the
//     refusal is the API's rather than the form's: a crafted grant is refused
//     with a code, not merely absent from a page. Because it can only withdraw,
//     the proven-ness question that decides granted-or-pending everywhere else
//     never arises here.
//   - IT IS OPERATOR-ONLY. Customer identity on this platform is global and
//     separate from staff (ADR 0010), so no Organization may inspect or act on
//     the consents of people who bought from other venues — and the lookup does
//     not disclose whether an address exists to anybody the allowlist has not
//     admitted, which is the disclosure posture the operator sale lookup already
//     holds.
//   - IT IS THE SAME ACT AS EVERY OTHER WITHDRAWAL. One Consent Record through
//     the platform's single consent-write path, carrying the prior state (#266),
//     and the same confirmation mail to the Customer (#267) — sent only when
//     something actually moved.
//
// The record it writes carries two things no other channel has: WHO recorded it,
// taken from the Staff Session and never from a body, and WHICH ARTEFACT it
// answers. The second is a pointer to evidence held elsewhere rather than
// evidence itself, and the Operator must supply it.

// operatorConsentCustomer is the Customer the lookup identifies, so an Operator
// holding a form can be sure they have the right person before acting.
type operatorConsentCustomer struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// operatorConsentState is what is TRUE NOW about that Customer's consents.
// Null on an optional consent means unanswered, which is a different fact from
// denied and is reported as the different fact it is.
type operatorConsentState struct {
	MarketingConsent  *string `json:"marketing_consent"`
	NetworkingConsent *string `json:"networking_consent"`
	PolicyAcceptedAt  *string `json:"policy_accepted_at"`
}

// operatorConsentWithdrawn is what one recorded act TOOK AWAY, which is not the
// same question as what it answered.
type operatorConsentWithdrawn struct {
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
}

type operatorConsentView struct {
	Customer operatorConsentCustomer   `json:"customer"`
	Consent  operatorConsentState      `json:"consent"`
	Withdrew *operatorConsentWithdrawn `json:"withdrew"`
}

// operatorConsentWithdrawalPath is the withdrawal's address AFTER #566: the act
// hangs off the person's consent record in the Legal Center, keyed on their
// OPAQUE UUID.
//
// The two routes it replaces — GET and POST on
// /api/v1/operator/customers/{email}/consent* — are deleted, and their absence
// is asserted in operator_legal_record_test.go. No email appears in any request
// line on this path.
func operatorConsentWithdrawalPath(customerID string) string {
	return "/api/v1/operator/legal/customers/" + url.PathEscape(customerID) + "/withdrawal"
}

// recordOperatorWithdrawal posts one withdrawal. The two consents are *bool so
// a test can send absent, a denial, and — for the refusal — a grant.
//
// It still takes an EMAIL, and resolves it to an id here — which is exactly
// what the operator's screen does for them. The address is how a human being is
// recognised; the id is how the request names them.
func recordOperatorWithdrawal(t *testing.T, env *testEnv, sessionID, email string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, operatorConsentWithdrawalPath(customerIDFor(t, env, email)), body, authHeader(sessionID))
}

func recordOperatorWithdrawalOK(t *testing.T, env *testEnv, sessionID, email string, body map[string]any) operatorConsentView {
	t.Helper()
	resp, body_ := recordOperatorWithdrawal(t, env, sessionID, email, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("record withdrawal status=%d error=%+v", resp.StatusCode, body_.Error)
	}
	if body_.Error != nil {
		t.Fatalf("record withdrawal returned error %+v", body_.Error)
	}
	var view operatorConsentView
	if err := json.Unmarshal(body_.Data, &view); err != nil {
		t.Fatalf("decode withdrawal: %v", err)
	}
	return view
}

// paperFormRef is the kind of thing an Operator actually types: a human's note
// saying where the paper is. Nothing is ever decided from its contents.
const paperFormRef = "Formulario de Revocatoria, signed 2026-08-01, received by post 2026-08-05"

// THE READ HALF OF THIS TICKET MOVED IN #566, and its tests moved with it to
// operator_legal_record_test.go. An Operator holding a form no longer starts
// from an address in a URL: they search the acceptance browser from a POSTed
// body, open that person's consent record by its opaque id, and act from there.
// Everything the lookup tests proved — that null is unanswered and not denied,
// that somebody who does not exist is a plain 404 rather than a blank record to
// act on, and that reading writes nothing — is proved there about the record.

// TestOperatorRecordsAWithdrawalOfBothOptionalConsents is the ticket's whole
// point: the mailed-in form is honoured, and the act it performs is a Consent
// Record like any other — one row, prior state on it, the operator's channel,
// who recorded it and which artefact it answers.
func TestOperatorRecordsAWithdrawalOfBothOptionalConsents(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, true)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	view := recordOperatorWithdrawalOK(t, env, operatorSessionID, "ana@example.com", map[string]any{
		"marketing_consent":  false,
		"networking_consent": false,
		"request_reference":  paperFormRef,
	})

	if view.Withdrew == nil || !view.Withdrew.MarketingConsent || !view.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v; want both taken away", view.Withdrew)
	}
	if view.Consent.MarketingConsent == nil || *view.Consent.MarketingConsent != "denied" ||
		view.Consent.NetworkingConsent == nil || *view.Consent.NetworkingConsent != "denied" {
		t.Fatalf("resulting consent = %+v; want both denied", view.Consent)
	}

	// ONE record, on the operator's own channel, saying what it replaced.
	record := onlyRecordOn(t, env, "ana@example.com", "operator_request")
	wantPrior(t, record, "granted", "granted")
	if !record.MarketingConsent.Valid || record.MarketingConsent.Bool ||
		!record.NetworkingConsent.Valid || record.NetworkingConsent.Bool {
		t.Fatalf("the operator record's answers = %+v; want both boxes answered No", record)
	}
	// Policy Acceptance is not withdrawable and was not shown here, so the
	// column is NULL and the standing acceptance is untouched.
	if record.PolicyAcceptance.Valid {
		t.Fatalf("the operator record answered policy_acceptance: %+v", record)
	}
	if state := readConsentState(t, env, "ana@example.com"); !state.PolicyAcceptedAt.Valid {
		t.Fatal("the withdrawal cleared the Customer's Policy Acceptance")
	}

	// WHO, and WHICH ARTEFACT — the pair that makes the one act no Customer
	// performed also the one act somebody is accountable for.
	attribution := readOperatorAttribution(t, env, "ana@example.com")
	if attribution.RecordedBy.String != "operator@example.com" {
		t.Fatalf("recorded_by = %+v; want the acting operator's session email", attribution.RecordedBy)
	}
	if attribution.RequestReference.String != paperFormRef {
		t.Fatalf("request_reference = %+v; want the artefact the Operator named", attribution.RequestReference)
	}

	// Marketing and the Follow Digest are one switch (ADR 0034), so withdrawing
	// one switched the other off in the same transaction.
	if readConsentState(t, env, "ana@example.com").DigestEnabled {
		t.Fatal("digest_enabled survived a Marketing Consent withdrawal")
	}
}

// TestOperatorRecordsAWithdrawalOfOneConsentAlone: a form that asks for one
// thing takes one thing away. The consent the form did not name is NOT SHOWN on
// this act, so it has no answer and no prior state — reading its absence as a
// refusal would turn a marketing withdrawal into a Withdraw All.
func TestOperatorRecordsAWithdrawalOfOneConsentAlone(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, true)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	view := recordOperatorWithdrawalOK(t, env, operatorSessionID, "ana@example.com", map[string]any{
		"networking_consent": false,
		"request_reference":  "email to protecciondedatos@, 2026-08-02",
	})

	if view.Withdrew == nil || view.Withdrew.MarketingConsent || !view.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v; want networking alone", view.Withdrew)
	}
	if view.Consent.MarketingConsent == nil || *view.Consent.MarketingConsent != "granted" {
		t.Fatalf("marketing consent = %v; a networking withdrawal must not touch it", view.Consent.MarketingConsent)
	}

	record := onlyRecordOn(t, env, "ana@example.com", "operator_request")
	wantPrior(t, record, "", "granted")
	if record.MarketingConsent.Valid {
		t.Fatalf("the record answered a box the form never named: %+v", record)
	}
	if !readConsentState(t, env, "ana@example.com").DigestEnabled {
		t.Fatal("a networking withdrawal switched the Follow Digest off")
	}
}

// TestOperatorWithdrawalSettlesSomebodyElsesTick: a Pending Confirmation is a
// tick from somebody who never proved the address, standing against it and
// never expiring. Settling it as No is a withdrawal too — something that had
// been standing stops standing — and the row must say so.
func TestOperatorWithdrawalSettlesSomebodyElsesTick(t *testing.T) {
	env := setupTest(t)
	seedPendingMarketingConsent(t, env, "ana@example.com")

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	view := recordOperatorWithdrawalOK(t, env, operatorSessionID, "ana@example.com", map[string]any{
		"marketing_consent": false,
		"request_reference": paperFormRef,
	})

	if view.Withdrew == nil || !view.Withdrew.MarketingConsent {
		t.Fatalf("withdrew = %+v; settling a Pending Confirmation as No takes something away", view.Withdrew)
	}
	wantPrior(t, onlyRecordOn(t, env, "ana@example.com", "operator_request"), "pending_confirmation", "")
}

// TestOperatorWithdrawalConfirmsTheCustomer: the person who wrote in learns
// their request was actioned, in the same mail every other withdrawal sends
// (#267) — and the evidence records that they were told.
func TestOperatorWithdrawalConfirmsTheCustomer(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, false)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	recordOperatorWithdrawalOK(t, env, operatorSessionID, "ana@example.com", map[string]any{
		"marketing_consent": false,
		"request_reference": paperFormRef,
	})

	// The Customer's own stored address, never one the request named.
	onlyWithdrawalConfirmation(t, env, "ana@example.com")
	wantConfirmationStamped(t, env, "ana@example.com", "operator_request")
}

// TestOperatorWithdrawalOfNothingMailsNobody is the negative half, and the one
// that fails against the obvious wrong implementation — mailing whenever an
// answer is No. A second form for somebody who has already withdrawn is
// recorded as the act it is and reports a change that did not happen to nobody.
func TestOperatorWithdrawalOfNothingMailsNobody(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, false, false)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	view := recordOperatorWithdrawalOK(t, env, operatorSessionID, "ana@example.com", map[string]any{
		"marketing_consent":  false,
		"networking_consent": false,
		"request_reference":  paperFormRef,
	})

	if view.Withdrew == nil || view.Withdrew.MarketingConsent || view.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v; nothing was standing to take away", view.Withdrew)
	}
	// The act is still recorded: the log says what HAPPENED, and a form that
	// arrived and was actioned is an act even where it moved nothing.
	record := onlyRecordOn(t, env, "ana@example.com", "operator_request")
	wantPrior(t, record, "denied", "denied")
	if record.ConfirmationSentAt.Valid {
		t.Fatalf("an act that moved nothing was stamped as confirmed: %+v", record)
	}
	wantNoWithdrawalConfirmation(t, env, "ana@example.com",
		"both consents were already denied, so nothing was taken away")
}

// TestOperatorSurfaceRefusesToGrantConsent is the constraint that makes this
// tool safe to hand anybody: THE API REFUSES A CRAFTED GRANT rather than merely
// omitting the control. A page without a button is not a guarantee — the API is
// reachable with curl — and an Operator who could grant could manufacture the
// consent they exist to honour the withdrawal of.
func TestOperatorSurfaceRefusesToGrantConsent(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, false, false)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	for _, crafted := range []map[string]any{
		{"marketing_consent": true, "request_reference": paperFormRef},
		{"networking_consent": true, "request_reference": paperFormRef},
		{"marketing_consent": false, "networking_consent": true, "request_reference": paperFormRef},
	} {
		resp, body := recordOperatorWithdrawal(t, env, operatorSessionID, "ana@example.com", crafted)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("crafted grant %v status=%d, want 400; error=%+v", crafted, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "CONSENT_GRANT_NOT_PERMITTED" || !isNullData(body.Data) {
			t.Fatalf("crafted grant %v envelope: data=%s error=%+v", crafted, body.Data, body.Error)
		}
	}

	// And nothing was written by any of them. A refusal that had already
	// captured would be a grant with a bad error message.
	if records := consentRecordsOn(t, env, "ana@example.com", "operator_request"); len(records) != 0 {
		t.Fatalf("a refused grant wrote %d records", len(records))
	}
	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" || state.NetworkingConsent.String != "denied" {
		t.Fatalf("a refused grant moved the consent state: %+v", state)
	}
}

// TestOperatorWithdrawalRequiresTheArtefactItAnswers: the reference is what
// makes the record evidence rather than an assertion, so the act is refused
// without it. Blank is the same as absent — "recorded against nothing" has one
// spelling.
func TestOperatorWithdrawalRequiresTheArtefactItAnswers(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	for _, body := range []map[string]any{
		{"marketing_consent": false},
		{"marketing_consent": false, "request_reference": "   "},
	} {
		resp, envelope := recordOperatorWithdrawal(t, env, operatorSessionID, "ana@example.com", body)
		if resp.StatusCode != http.StatusBadRequest || envelope.Error == nil || envelope.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("withdrawal without a reference %v status=%d error=%+v; want 400 VALIDATION_FAILED",
				body, resp.StatusCode, envelope.Error)
		}
	}

	// And one naming no consent at all: there is nothing to record.
	resp, envelope := recordOperatorWithdrawal(t, env, operatorSessionID, "ana@example.com", map[string]any{
		"request_reference": paperFormRef,
	})
	if resp.StatusCode != http.StatusBadRequest || envelope.Error == nil || envelope.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("withdrawal naming no consent status=%d error=%+v; want 400 VALIDATION_FAILED", resp.StatusCode, envelope.Error)
	}

	if records := consentRecordsOn(t, env, "ana@example.com", "operator_request"); len(records) != 0 {
		t.Fatalf("a refused withdrawal wrote %d records", len(records))
	}
}

// TestOperatorConsentSurfaceIsOperatorsOnly is ADR 0010 enforced at the door.
//
// Customer identity is GLOBAL and separate from staff, so an Org Admin — even
// of the Organization the Customer bought from — has no business inspecting or
// altering the consents of a person who also bought from somewhere else. The
// refusal is 403, and it is 403 for a Customer who really exists and for an id
// that names nobody alike: an Organization learns nothing about who is on this
// platform, not even whether somebody is.
func TestOperatorConsentSurfaceIsOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	signInAnswering(t, env, "ana@example.com", true, true, false)

	withdrawal := map[string]any{"marketing_consent": false, "request_reference": paperFormRef}

	// A real person and a UUID nobody holds, refused identically. The second is
	// a well-formed id rather than a nonsense string, so the refusal is proved
	// to come from the DOOR and not from a parse.
	for _, customerID := range []string{customerIDFor(t, env, "ana@example.com"), "8f1c0c66-0000-4000-8000-000000000000"} {
		resp, body := env.post(t, operatorConsentWithdrawalPath(customerID), withdrawal, nil)
		if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("unauthenticated withdrawal for %s status=%d error=%+v; want 401 UNAUTHORIZED", customerID, resp.StatusCode, body.Error)
		}
		resp, body = env.post(t, operatorConsentWithdrawalPath(customerID), withdrawal, authHeader(adminSessionID))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("org_admin withdrawal for %s status=%d error=%+v; want 403 FORBIDDEN", customerID, resp.StatusCode, body.Error)
		}
	}

	// Nothing a refused caller sent was recorded, and the state stands.
	if records := consentRecordsOn(t, env, "ana@example.com", "operator_request"); len(records) != 0 {
		t.Fatalf("a refused caller wrote %d records", len(records))
	}
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "granted" {
		t.Fatalf("a refused caller moved the consent state: %+v", state)
	}

	// The operator, who is a Member of nothing, is served.
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	if got := recordOperatorWithdrawalOK(t, env, operatorSessionID, "ana@example.com", withdrawal); got.Withdrew == nil || !got.Withdrew.MarketingConsent {
		t.Fatalf("operator withdrawal = %+v", got)
	}
}

// customerIDFor resolves an address to the opaque id every route on this path
// is keyed on.
//
// SQL, and deliberately so: there is no longer an API that turns an address
// into a Customer, because there is no longer a route that takes one. The
// screen reaches the id through the acceptance browser's search, which posts
// its fragment in a body.
func customerIDFor(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`SELECT id FROM customers WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("resolve customer id for %q: %v", email, err)
	}
	return id
}

// operatorAttribution is the pair of columns only this channel writes: who
// recorded the act, and which artefact it answers. Read by SQL because the
// evidence log has no endpoint and deliberately never will.
type operatorAttribution struct {
	RecordedBy       sql.NullString
	RequestReference sql.NullString
}

func readOperatorAttribution(t *testing.T, env *testEnv, email string) operatorAttribution {
	t.Helper()
	var a operatorAttribution
	if err := env.db.QueryRow(`
		SELECT r.recorded_by, r.request_reference
		FROM consent_records r
		JOIN customers c ON c.id = r.customer_id
		WHERE c.email = $1 AND r.channel = 'operator_request'
	`, email).Scan(&a.RecordedBy, &a.RequestReference); err != nil {
		t.Fatalf("read operator attribution for %q: %v", email, err)
	}
	return a
}

// seedCustomerWithoutConsent creates a Customer the way a box office sale does:
// a record that exists and has never been asked anything. SQL because no API
// creates an inert Customer without also capturing consent.
func seedCustomerWithoutConsent(t *testing.T, env *testEnv, email string) {
	t.Helper()
	if _, err := env.db.Exec(
		`INSERT INTO customers (email, first_name, last_name) VALUES ($1, 'Box', 'Office')`,
		email,
	); err != nil {
		t.Fatalf("seed customer %q: %v", email, err)
	}
}

// seedPendingMarketingConsent puts a Customer in Pending Confirmation — a tick
// from somebody who never proved the address. SQL because the surfaces that
// produce it (a guest checkout) are a great deal of setup for a state this test
// is not about.
func seedPendingMarketingConsent(t *testing.T, env *testEnv, email string) {
	t.Helper()
	if _, err := env.db.Exec(
		`INSERT INTO customers (email, first_name, last_name, marketing_consent)
		 VALUES ($1, 'Ana', 'Lopez', 'pending_confirmation')`,
		email,
	); err != nil {
		t.Fatalf("seed pending consent for %q: %v", email, err)
	}
}
