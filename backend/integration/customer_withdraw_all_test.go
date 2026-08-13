package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Withdraw All (#269, parent #265): one act, one record, one disclosure.
//
// THE PROPERTY THE WHOLE TICKET RESTS ON IS "ONE ACT, ONE ROW". A Customer who
// asks to be left alone has performed a single act with a single meaning, and
// the evidence must say so: `granted → denied` on both optional consents in one
// Consent Record, rather than two rows that a compliance officer would have to
// read as a coincidence of timing. That is why this is an endpoint of its own
// and not two calls to the per-purpose control (#268) — that route takes ONE
// purpose in its path precisely so that it cannot express this, and the tests
// below assert the row count rather than only the resulting state, because the
// resulting state is identical either way and would pass against two rows.
//
// THE OTHER HALF IS THE DISCLOSURE, AND IT IS NOT AN ENDPOINT. Counsel attaches
// a duty to the total withdrawal that the individual controls do not carry, so
// the act sits behind a dialog that says what withdrawal does and does not mean.
// The dialog is Storefront copy and is asserted where copy lives; what the API
// can be held to is that NOTHING IS WRITTEN UNTIL THE ACT IS PERFORMED, which
// is what makes dismissing the dialog free — see the test of that name.
//
// The confirmation mail is #267's mechanism reused and asked nothing new: it
// follows what the receipt says MOVED, so a Customer whose consents were both
// already denied is not told about a change that did not happen.

const customerWithdrawAllPath = customerPrivacyPath + "/withdraw-all"

// withdrawAllView is what one Withdraw All did: both consents as they now
// stand, and what the act actually took away — which are different facts, since
// `denied` reads the same whether somebody just gave something up or was
// declining for the second time.
type withdrawAllView struct {
	Consents optionalConsentsView `json:"consents"`
	Withdrew struct {
		MarketingConsent  bool `json:"marketing_consent"`
		NetworkingConsent bool `json:"networking_consent"`
	} `json:"withdrew"`
}

// withdrawAll performs the act, through the API and under a session.
//
// IT SENDS NO BODY, and that is the endpoint's contract rather than this
// helper's shortcut: the act names nothing and chooses nothing, so there is no
// field in which a request could ask for a grant.
func withdrawAll(t *testing.T, env *testEnv, token string) withdrawAllView {
	t.Helper()
	resp, body := env.post(t, customerWithdrawAllPath, nil,
		mergeHeaders(authHeader(token), consentEvidenceHeaders()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdraw all status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("withdraw all error=%+v, want none", body.Error)
	}
	var view withdrawAllView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode withdraw all view: %v", err)
	}
	return view
}

// TestWithdrawAllWritesOneRecordDenyingBothConsents is the ticket.
//
// One act by a Customer who had granted both, and afterwards: both consents
// denied, the Follow Digest off with Marketing in the same transaction (ADR
// 0034), EXACTLY ONE Consent Record carrying both answers and both prior
// states, and one confirmation naming both.
func TestWithdrawAllWritesOneRecordDenyingBothConsents(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	if !digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("the Digest is not on for a Customer who granted Marketing Consent at sign-in")
	}

	view := withdrawAll(t, env, token)

	wantConsents(t, view.Consents, "denied", "denied", "Withdraw All denies every optional consent")
	if !view.Withdrew.MarketingConsent || !view.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v, want both: this act took both consents away", view.Withdrew)
	}
	state := readConsentState(t, env, "ana@example.com")
	if state.MarketingConsent.String != "denied" || state.NetworkingConsent.String != "denied" {
		t.Fatalf("consent state = %+v after Withdraw All, want both denied", state)
	}
	if digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("digest_enabled survived a Withdraw All, so the Follow Digest and Marketing Consent are two switches")
	}
	// Policy Acceptance is untouched: it is not withdrawable, and an act that
	// cleared it would re-gate the person rather than free them (ADR 0038).
	if !state.PolicyAcceptedAt.Valid {
		t.Fatal("Withdraw All cleared the Policy Acceptance, which is not a consent and is not withdrawable")
	}

	// ONE ROW. onlyRecordOn fails on two, which is the assertion that fails
	// against the tempting implementation: two calls to the per-purpose control.
	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	if !record.MarketingConsent.Valid || record.MarketingConsent.Bool {
		t.Fatalf("the record does not carry the No answered for Marketing Consent: %+v", record)
	}
	if !record.NetworkingConsent.Valid || record.NetworkingConsent.Bool {
		t.Fatalf("the record does not carry the No answered for Networking Consent: %+v", record)
	}
	if record.PolicyAcceptance.Valid {
		t.Fatalf("the record answers a Policy Acceptance box that was never shown: %+v", record)
	}
	// Both prior states on the one row, which is what makes "they asked for
	// everything" legible without reading the log in order (#266).
	wantPrior(t, record, "granted", "granted")
	if !record.EmailProven {
		t.Fatal("a Withdraw All made behind a Customer Session is not recorded as proven")
	}
	if !record.IP.Valid || !record.UserAgent.Valid || !record.SessionID.Valid || !record.OriginURL.Valid {
		t.Fatalf("the Withdraw All carries no technical proof: %+v", record)
	}

	// One mail, naming both, because both moved.
	confirmation := onlyWithdrawalConfirmation(t, env, "ana@example.com")
	if !confirmation.MarketingConsent || !confirmation.NetworkingConsent {
		t.Fatalf("the confirmation names %+v, want both consents named", confirmation)
	}
	wantConfirmationStamped(t, env, "ana@example.com", "account_settings")

	// And the page a Customer lands back on says the same thing.
	wantConsents(t, readPrivacy(t, env, token).Consents, "denied", "denied",
		"the Privacy page must report what the act made true")
}

// TestWithdrawAllConfirmsOnlyWhatItActuallyTookAway: the mail is about the
// CHANGE and not about the request.
//
// This Customer had granted Marketing and never been asked about Networking, so
// one consent moved and one was answered No for the first time — a refusal, not
// a withdrawal (CONTEXT.md, and the guidance's revocation register asks for
// exactly that distinction). Naming Networking in the confirmation would tell
// somebody they had taken back something they never gave.
func TestWithdrawAllConfirmsOnlyWhatItActuallyTookAway(t *testing.T) {
	env := setupTest(t)

	token := signInAnsweringMarketingOnly(t, env, "ana@example.com", true)

	view := withdrawAll(t, env, token)

	wantConsents(t, view.Consents, "denied", "denied",
		"a never-answered consent is answered No by this act, behind a proven session")
	if !view.Withdrew.MarketingConsent {
		t.Fatal("withdrawing a granted Marketing Consent is not reported as having taken it away")
	}
	if view.Withdrew.NetworkingConsent {
		t.Fatal("a consent that was never answered was reported as withdrawn; a first refusal is not a withdrawal")
	}
	// Still one row, and its prior states say which of the two this act moved.
	wantPrior(t, onlyRecordOn(t, env, "ana@example.com", "account_settings"), "granted", "")

	confirmation := onlyWithdrawalConfirmation(t, env, "ana@example.com")
	if !confirmation.MarketingConsent || confirmation.NetworkingConsent {
		t.Fatalf("the confirmation names %+v, want Marketing Consent alone", confirmation)
	}
}

// TestWithdrawAllOnAConsentlessCustomerRecordsTheActAndSendsNothing is the
// no-op, and it is the case #269 names explicitly: no mail when both consents
// were already denied.
//
// The Consent Record is still written, because the log says what HAPPENED and
// somebody did perform this act. Nobody is mailed and nothing is stamped,
// because reporting a change that did not happen is the failure the rule exists
// to prevent — and a Customer who presses Withdraw All twice must not be
// written to twice for it.
func TestWithdrawAllOnAConsentlessCustomerRecordsTheActAndSendsNothing(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, false, false)
	before := readConsentState(t, env, "ana@example.com")

	view := withdrawAll(t, env, token)

	wantConsents(t, view.Consents, "denied", "denied", "both consents were already denied and stay denied")
	if view.Withdrew.MarketingConsent || view.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v, want neither: nothing was standing to take away", view.Withdrew)
	}
	if after := readConsentState(t, env, "ana@example.com"); after != before {
		t.Fatalf("consent state moved on a no-op: before=%+v after=%+v", before, after)
	}
	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	wantPrior(t, record, "denied", "denied")
	if record.ConfirmationSentAt.Valid {
		t.Fatalf("confirmation_sent_at is stamped on an act that took nothing away: %+v", record)
	}
	wantNoWithdrawalConfirmation(t, env, "ana@example.com",
		"both consents were already denied, so the act took nothing away")
}

// TestWithdrawAllSettlesAPendingConfirmationAsNo: somebody else's tick is
// standing against this address, and a Customer asking to be left alone settles
// it — which IS a withdrawal, because something was standing and stops standing.
func TestWithdrawAllSettlesAPendingConfirmationAsNo(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), boolPtr(true))
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q, want the pending state this test needs", state.MarketingConsent.String)
	}

	token := customerSignIn(t, env, "ana@example.com")
	view := withdrawAll(t, env, token)

	wantConsents(t, view.Consents, "denied", "denied", "the owner settled both pending ticks as No")
	if !view.Withdrew.MarketingConsent || !view.Withdrew.NetworkingConsent {
		t.Fatalf("withdrew = %+v, want both: a Pending Confirmation settled as No took something away", view.Withdrew)
	}
	wantPrior(t, onlyRecordOn(t, env, "ana@example.com", "account_settings"),
		"pending_confirmation", "pending_confirmation")
	onlyWithdrawalConfirmation(t, env, "ana@example.com")
}

// TestWithdrawAllIsNotAOneWayDoor is the promise the disclosure makes: anything
// withdrawn can be turned back on at any time.
//
// It is asserted here rather than left to #268's tests because it is THIS
// dialog's sentence, and copy that a later change quietly falsified would be
// the worst kind of untrue.
//
// An hour later, so the two acts are distinguishable by their own timestamps:
// the evidence log is read in captured_at order and the tiebreaker is a random
// UUID, so two acts sharing this suite's fixed clock come back in either order.
func TestWithdrawAllIsNotAOneWayDoor(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	withdrawAll(t, env, token)

	setSignInClock(t, env, env.fixedClock.Add(time.Hour))
	wantConsents(t, setOptionalConsent(t, env, token, "networking", true), "denied", "granted",
		"a Customer who withdrew everything can turn a consent back on")

	records := consentRecordsOn(t, env, "ana@example.com", "account_settings")
	if len(records) != 2 {
		t.Fatalf("a Withdraw All and a later grant wrote %d records, want 2", len(records))
	}
	wantPrior(t, records[1], "", "denied")
}

// TestDismissingTheWithdrawAllDisclosureWritesNothing: the disclosure is not an
// act, and reaching it costs nothing.
//
// A Customer opens the Privacy page, reads what Withdraw All would do, and
// closes the dialog. NOTHING ABOUT THAT REACHES THE API — the disclosure is
// copy already on the rendered page and the Storefront component issues its one
// request from the confirm button alone — so what is asserted here is the fact
// that makes that safe: everything reachable without performing the act leaves
// the evidence log and the consent columns exactly as they were. If the
// disclosure ever grew a request of its own, this is the assertion that would
// have to be deleted to keep the suite green.
func TestDismissingTheWithdrawAllDisclosureWritesNothing(t *testing.T) {
	env := setupTest(t)

	// A Customer with something to lose in every column.
	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	before := readConsentState(t, env, "ana@example.com")
	recordsBefore := len(readConsentRecords(t, env, "ana@example.com"))

	// Opening the page the dialog lives on, twice, and going away again.
	readPrivacy(t, env, token)
	readPrivacy(t, env, token)

	if after := len(readConsentRecords(t, env, "ana@example.com")); after != recordsBefore {
		t.Fatalf("the evidence log grew from %d to %d rows without anybody confirming anything", recordsBefore, after)
	}
	if after := readConsentState(t, env, "ana@example.com"); after != before {
		t.Fatalf("consent state moved without the act being performed: before=%+v after=%+v", before, after)
	}
	if records := consentRecordsOn(t, env, "ana@example.com", "account_settings"); len(records) != 0 {
		t.Fatalf("a dismissed disclosure wrote %d Consent Records, want none", len(records))
	}
	wantNoWithdrawalConfirmation(t, env, "ana@example.com", "nobody confirmed anything")
}

// TestWithdrawAllIsClosedToVisitorsWithoutASession: it is an act about one
// identified person, performed behind the same full Customer Session the
// controls beside it require.
func TestWithdrawAllIsClosedToVisitorsWithoutASession(t *testing.T) {
	env := setupTest(t)

	resp, _ := env.post(t, customerWithdrawAllPath, nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous Withdraw All status=%d, want 401", resp.StatusCode)
	}
}
