package integration

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// The checkout owed-boxes backstop for the Terms (#537, parent #533, ADR
// 0066): a signed-in Customer whose session spans a Terms edition — the
// one-time re-gate, or any later bump — cannot complete an Online Sale without
// accepting, and the answer survives the Payment Provider redirect exactly as
// the privacy answers do (migration 108 beside 064).
//
// The seam is the checkout service's, driven through the same routes
// checkout_consent_test.go drives; evidence and state are asserted through SQL
// for that file's reason.

// readPaymentTermsHold reads the Terms pair a Payment is holding between begin
// and commit (migration 108).
func readPaymentTermsHold(t *testing.T, env *testEnv, clientTransactionID string) (sql.NullBool, sql.NullString) {
	t.Helper()
	var answer sql.NullBool
	var versionID sql.NullString
	if err := env.db.QueryRow(`
		SELECT consent_terms_acceptance, consent_terms_version_id
		FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&answer, &versionID); err != nil {
		t.Fatalf("read payment terms hold %q: %v", clientTransactionID, err)
	}
	return answer, versionID
}

// signedInOwingTermsOnly builds the one person this backstop exists for: a
// Customer holding a live session — every box settled at sign-in — when a
// later Terms edition is published. Returns the session token and the new
// edition's id.
func signedInOwingTermsOnly(t *testing.T, env *testEnv, email, label string) (string, string) {
	t.Helper()
	token := customerSignIn(t, env, email)
	editionID := publishTermsVersion(t, env, label)
	boxes := signedInConsentBoxes(t, env, token)
	if !boxes.TermsAcceptance {
		t.Fatal("terms box not owed after a later edition was published")
	}
	if boxes.PolicyAcceptance || boxes.MarketingConsent || boxes.NetworkingConsent {
		t.Fatalf("privacy boxes = %+v, want none owed — the Terms bump must re-show the terms box ALONE", boxes)
	}
	return token, editionID
}

// TestCheckoutRefusedWithoutOwedTermsAcceptance is the gate: the API's own
// refusal, not the dialog's disabled button. Nothing is created — no Payment,
// no evidence.
func TestCheckoutRefusedWithoutOwedTermsAcceptance(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Terms Fest", "terms-fest", 1000, 10)

	token, _ := signedInOwingTermsOnly(t, env, "tomas@example.com", "2-checkout-gate")

	// The box owed and not answered at all.
	resp, body := beginCheckoutWithEvidence(t, env, "test-org", "terms-fest", token,
		consentCheckoutBody("Tomas", "Vera", nil, nil, nil, cartLine(gaID, 1)))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("begin with terms owed and unanswered: status=%d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "TERMS_ACCEPTANCE_REQUIRED" {
		t.Fatalf("error = %+v, want TERMS_ACCEPTANCE_REQUIRED", body.Error)
	}

	// The box shown and left unticked: an explicit answer, and still no sale —
	// contractual acceptance gates the checkout as the policy does.
	withFalse := consentCheckoutBody("Tomas", "Vera", nil, nil, nil, cartLine(gaID, 1))
	withFalse["terms_acceptance"] = false
	resp, body = beginCheckoutWithEvidence(t, env, "test-org", "terms-fest", token, withFalse)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("begin with terms unticked: status=%d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "TERMS_ACCEPTANCE_REQUIRED" {
		t.Fatalf("error = %+v, want TERMS_ACCEPTANCE_REQUIRED", body.Error)
	}

	if got := len(consentRecordsOn(t, env, "tomas@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records after two refused begins = %d, want 0", got)
	}
}

// TestCheckoutTermsAnswerSurvivesTheProviderRedirect: the whole journey. The
// answer is held on the Payment WITH the edition it was answered about, and
// the record written at commit names that edition — even when a further
// edition lands between the two legs, which is also why the Customer is,
// correctly, still owed the newest one afterwards.
func TestCheckoutTermsAnswerSurvivesTheProviderRedirect(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Terms Redirect Fest", "terms-redirect-fest", 1000, 10)

	token, shownEdition := signedInOwingTermsOnly(t, env, "teresa@example.com", "2-redirect")

	// The sign-in above wrote a Consent Record on the fixed clock; the checkout
	// capture below must not tie with it (known fixture behaviour).
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	body := consentCheckoutBody("Teresa", "Vega", nil, nil, nil, cartLine(gaID, 1))
	body["terms_acceptance"] = true
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "terms-redirect-fest", token, body)

	// Held, not recorded: between the legs there is a Payment and nothing else.
	answer, heldVersion := readPaymentTermsHold(t, env, begin.ClientTransactionID)
	if !answer.Valid || !answer.Bool {
		t.Fatalf("held terms_acceptance = %+v, want true", answer)
	}
	if !heldVersion.Valid || heldVersion.String != shownEdition {
		t.Fatalf("held terms_version_id = %+v, want the edition the box was shown about", heldVersion)
	}
	if got := len(consentRecordsOn(t, env, "teresa@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records before the sale commits = %d, want 0", got)
	}

	// An edition published between the legs must not rewrite what was accepted.
	publishTermsVersion(t, env, "3-mid-redirect")

	confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}

	records := consentRecordsOn(t, env, "teresa@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want exactly one for one capture act", len(records))
	}
	// A terms-only act: the record says nothing about the privacy boxes.
	if records[0].PolicyAcceptance.Valid {
		t.Fatalf("recorded policy_acceptance = %+v, want NULL on a terms-only act", records[0].PolicyAcceptance)
	}
	termsAnswers := readTermsAnswers(t, env, "teresa@example.com")
	last := termsAnswers[len(termsAnswers)-1]
	if !last.Answer.Valid || !last.Answer.Bool {
		t.Fatalf("recorded terms_acceptance = %+v, want true", last.Answer)
	}
	if !last.VersionID.Valid || last.VersionID.String != shownEdition {
		t.Fatalf("recorded terms_version_id = %+v, want the SHOWN edition, not the one current at commit", last.VersionID)
	}

	// The state stamps the shown edition too — so the mid-redirect edition is,
	// correctly, still owed at the next surface.
	acceptedAt, stateVersion := readTermsState(t, env, "teresa@example.com")
	if !acceptedAt.Valid {
		t.Fatal("terms_accepted_at is null after an accepted checkout")
	}
	if !stateVersion.Valid || stateVersion.String != shownEdition {
		t.Fatalf("stored terms_version_id = %+v, want the shown edition", stateVersion)
	}
	if boxes := signedInConsentBoxes(t, env, token); !boxes.TermsAcceptance {
		t.Fatal("terms box not owed for the edition published mid-redirect")
	}
}

// TestCurrentOnBothDocumentsOwedNothingAtCheckout is the point of the feature:
// the ordinary buyer's flow is byte-for-byte the usual one. No box, no hold,
// no record.
func TestCurrentOnBothDocumentsOwedNothingAtCheckout(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Current Fest", "current-fest", 1000, 10)

	token := customerSignIn(t, env, "carla@example.com")
	boxes := signedInConsentBoxes(t, env, token)
	if boxes.TermsAcceptance || boxes.PolicyAcceptance || boxes.MarketingConsent || boxes.NetworkingConsent {
		t.Fatalf("boxes = %+v, want none owed for a Customer current on both documents", boxes)
	}

	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "current-fest", token,
		consentCheckoutBody("Carla", "Ruiz", nil, nil, nil, cartLine(gaID, 1)))
	answer, heldVersion := readPaymentTermsHold(t, env, begin.ClientTransactionID)
	if answer.Valid || heldVersion.Valid {
		t.Fatalf("terms hold = (%+v, %+v), want the NULL pair when no box was drawn", answer, heldVersion)
	}

	if confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}
	if got := len(consentRecordsOn(t, env, "carla@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records = %d, want 0 — no capture act happened", got)
	}
}

// TestUnowedTermsAnswerIsDropped: the existing owed-boxes rule, applied to the
// new box. A crafted body cannot manufacture Terms evidence for a Customer who
// owes nothing, and cannot churn the standing acceptance.
func TestUnowedTermsAnswerIsDropped(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Crafted Fest", "crafted-fest", 1000, 10)

	token := customerSignIn(t, env, "camilo@example.com")
	_, stampedBefore := readTermsState(t, env, "camilo@example.com")

	body := consentCheckoutBody("Camilo", "Paz", nil, nil, nil, cartLine(gaID, 1))
	body["terms_acceptance"] = true
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "crafted-fest", token, body)

	answer, heldVersion := readPaymentTermsHold(t, env, begin.ClientTransactionID)
	if answer.Valid || heldVersion.Valid {
		t.Fatalf("terms hold = (%+v, %+v), want the unowed answer DROPPED at begin", answer, heldVersion)
	}

	if confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}
	if got := len(consentRecordsOn(t, env, "camilo@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records = %d, want 0 from dropped answers alone", got)
	}
	_, stampedAfter := readTermsState(t, env, "camilo@example.com")
	if stampedBefore.String != stampedAfter.String {
		t.Fatalf("stored terms_version_id churned %q -> %q by an unowed answer", stampedBefore.String, stampedAfter.String)
	}
}
