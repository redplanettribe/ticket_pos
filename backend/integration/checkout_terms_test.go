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
func signedInOwingTermsOnly(t *testing.T, env *testEnv, email string, generation int) (string, string) {
	t.Helper()
	token := customerSignIn(t, env, email)
	editionID := publishTermsVersion(t, env, generation, 0)
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

	token, _ := signedInOwingTermsOnly(t, env, "tomas@example.com", 2)

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

	token, shownEdition := signedInOwingTermsOnly(t, env, "teresa@example.com", 2)

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
	publishTermsVersion(t, env, 3, 0)

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

// The Adulthood Declaration at the checkout owed-boxes fallback (#588, parent
// #584, ADR 0069): the fourth and last capture point, and the backstop behind
// the other three.
//
// It is a section of THIS file rather than a file of its own because it is not
// a second gate. The declaration rides the Terms box — owed exactly where that
// box is owed and the edition in effect carries the
// `label-adulthood-declaration` Artifact — so every test below is one of the
// tests above with a second answer travelling beside the first, and reading
// them together is what keeps that relationship legible.

// readPaymentAdulthoodHold reads the declaration a Payment is holding between
// begin and commit (migration 119), beside readPaymentTermsHold's pair.
func readPaymentAdulthoodHold(t *testing.T, env *testEnv, clientTransactionID string) sql.NullBool {
	t.Helper()
	var declaration sql.NullBool
	if err := env.db.QueryRow(`
		SELECT consent_adulthood_declaration
		FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&declaration); err != nil {
		t.Fatalf("read payment adulthood hold %q: %v", clientTransactionID, err)
	}
	return declaration
}

// signedInOwingTheDeclaration builds the person this backstop exists for: a
// Customer who settled every box at sign-in — under an edition that did not ask
// — and is then re-gated mid-session by an edition that does.
//
// It is signedInOwingTermsOnly with the Artifact published on the new edition,
// which is exactly how the box arrives in production: a publish, with no deploy
// on either side of it.
func signedInOwingTheDeclaration(t *testing.T, env *testEnv, email string, generation int) (string, string) {
	t.Helper()
	token := customerSignIn(t, env, email)
	editionID := publishTermsVersionAskingAdulthood(t, env, generation, 0)
	boxes := signedInConsentBoxes(t, env, token)
	if !boxes.TermsAcceptance || !boxes.AdulthoodDeclaration {
		t.Fatalf("boxes = %+v, want the terms box AND the declaration owed at the dialog", boxes)
	}
	if boxes.PolicyAcceptance || boxes.MarketingConsent || boxes.NetworkingConsent {
		t.Fatalf("privacy boxes = %+v, want none owed — a Terms bump re-shows its own boxes ALONE", boxes)
	}
	return token, editionID
}

// TestCheckoutRefusedWithoutOwedAdulthoodDeclaration is the refusal, which is
// the whole of ADR 0069 at this surface: no Payment, no Capacity Hold, no
// evidence, and — above all — NO ROW SAYING ANYBODY IS A MINOR.
//
// It also pins the ORDER of the two refusals. A buyer who unticks both meets
// TERMS_ACCEPTANCE_REQUIRED, because the declaration rides that box and the
// document is what the act is about; a buyer who accepts the Terms and declines
// the declaration meets ADULTHOOD_DECLARATION_REQUIRED.
func TestCheckoutRefusedWithoutOwedAdulthoodDeclaration(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Adult Fest", "adult-fest", 1000, 10)

	token, _ := signedInOwingTheDeclaration(t, env, "adela@example.com", 2)
	recordsBefore := len(readConsentRecords(t, env, "adela@example.com"))

	cases := []struct {
		name        string
		terms       bool
		declaration any
		want        string
	}{
		// A client that never heard of the box — an old Storefront, or a caller
		// that is not the Storefront at all. The deploy cannot be bypassed by
		// omitting the field.
		{name: "declaration absent", terms: true, declaration: nil, want: "ADULTHOOD_DECLARATION_REQUIRED"},
		// Somebody who read the words and said they are not eighteen. Their
		// purchase does not happen, and nothing about the answer is kept.
		{name: "declaration declined", terms: true, declaration: false, want: "ADULTHOOD_DECLARATION_REQUIRED"},
		// Both unticked: the Terms refusal comes first, naming the document.
		{name: "both declined", terms: false, declaration: false, want: "TERMS_ACCEPTANCE_REQUIRED"},
	}
	for _, tc := range cases {
		body := consentCheckoutBody("Adela", "Cruz", nil, nil, nil, cartLine(gaID, 1))
		body["terms_acceptance"] = tc.terms
		if tc.declaration != nil {
			body["adulthood_declaration"] = tc.declaration
		}
		resp, envelope := beginCheckoutWithEvidence(t, env, "test-org", "adult-fest", token, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", tc.name, resp.StatusCode)
		}
		if envelope.Error == nil || envelope.Error.Code != tc.want {
			t.Fatalf("%s: error=%+v, want %s", tc.name, envelope.Error, tc.want)
		}
	}

	// The refusal is total, and it is total BEFORE the Payment Provider: no
	// Payment means no hold on tickets nobody is buying and no reachable
	// payment page. This is why the check sits at begin and never at confirm.
	var payments int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if payments != 0 {
		t.Fatalf("payments after refused begins = %d, want 0", payments)
	}
	if got := len(readConsentRecords(t, env, "adela@example.com")); got != recordsBefore {
		t.Fatalf("consent records after refused begins = %d, want the %d her sign-in left", got, recordsBefore)
	}
	// And the standing state is untouched: a refused checkout does not accept
	// the Terms on somebody's behalf, so the boxes are still owed at the next
	// surface — which is what "never blocked with no way forward" means here.
	if boxes := signedInConsentBoxes(t, env, token); !boxes.TermsAcceptance || !boxes.AdulthoodDeclaration {
		t.Fatalf("boxes after refusals = %+v, want both still owed", boxes)
	}
}

// TestCheckoutAdulthoodDeclarationSurvivesTheProviderRedirect is the journey:
// held on the Payment at begin, beside the Terms answer and the edition it was
// declared under, and written to the Consent Record when the provider answers —
// even though a further edition lands in between.
func TestCheckoutAdulthoodDeclarationSurvivesTheProviderRedirect(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Adult Redirect Fest", "adult-redirect-fest", 1000, 10)

	token, shownEdition := signedInOwingTheDeclaration(t, env, "adriana@example.com", 2)

	// The sign-in above wrote a Consent Record on the fixed clock; the checkout
	// capture below must not tie with it (known fixture behaviour).
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	body := consentCheckoutBody("Adriana", "Sosa", nil, nil, nil, cartLine(gaID, 1))
	body["terms_acceptance"] = true
	body["adulthood_declaration"] = true
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "adult-redirect-fest", token, body)

	// Held, not recorded. Between the legs there is a Payment carrying the pair
	// and an evidence log that says nothing yet.
	declaration := readPaymentAdulthoodHold(t, env, begin.ClientTransactionID)
	if !declaration.Valid || !declaration.Bool {
		t.Fatalf("held consent_adulthood_declaration = %+v, want true", declaration)
	}
	answer, heldVersion := readPaymentTermsHold(t, env, begin.ClientTransactionID)
	if !answer.Valid || !answer.Bool || !heldVersion.Valid || heldVersion.String != shownEdition {
		t.Fatalf("held terms pair = (%+v, %+v), want true beside the edition the box was shown about", answer, heldVersion)
	}
	if got := len(consentRecordsOn(t, env, "adriana@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records before the sale commits = %d, want 0", got)
	}

	// An edition published between the legs must not rewrite what was declared,
	// nor which words it was declared under.
	publishTermsVersion(t, env, 3, 0)

	if confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}

	records := consentRecordsOn(t, env, "adriana@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want exactly one for one capture act", len(records))
	}
	if !records[0].AdulthoodDeclaration.Valid || !records[0].AdulthoodDeclaration.Bool {
		t.Fatalf("recorded adulthood_declaration = %+v, want true", records[0].AdulthoodDeclaration)
	}
	termsAnswers := readTermsAnswers(t, env, "adriana@example.com")
	last := termsAnswers[len(termsAnswers)-1]
	if !last.VersionID.Valid || last.VersionID.String != shownEdition {
		t.Fatalf("recorded terms_version_id = %+v, want the SHOWN edition — the one whose Artifact worded the box", last.VersionID)
	}
	// And the declaration is owed only where the acceptance it rode in on is:
	// it has no standing of its own to go stale separately.
	if boxes := signedInConsentBoxes(t, env, token); boxes.AdulthoodDeclaration && !boxes.TermsAcceptance {
		t.Fatalf("boxes = %+v, want the declaration owed only where the terms box is", boxes)
	}
}

// TestAnEditionWithoutTheArtifactOwesNoDeclarationAtCheckout is the world
// between the deploy and the publish, which is every environment today: the
// Terms box is owed, the declaration is not, and the whole checkout behaves
// exactly as it did before this shipped.
func TestAnEditionWithoutTheArtifactOwesNoDeclarationAtCheckout(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Silent Fest", "silent-fest", 1000, 10)

	token, _ := signedInOwingTermsOnly(t, env, "silvia@example.com", 2)
	if boxes := signedInConsentBoxes(t, env, token); boxes.AdulthoodDeclaration {
		t.Fatal("the declaration is owed under an edition that publishes no label-adulthood-declaration artifact")
	}
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	// The Terms box alone, answered alone — and accepted, with no declaration
	// demanded of anybody.
	body := consentCheckoutBody("Silvia", "Mora", nil, nil, nil, cartLine(gaID, 1))
	body["terms_acceptance"] = true
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "silent-fest", token, body)
	if declaration := readPaymentAdulthoodHold(t, env, begin.ClientTransactionID); declaration.Valid {
		t.Fatalf("held declaration = %+v, want NULL when no such box was drawn", declaration)
	}

	if confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}
	records := consentRecordsOn(t, env, "silvia@example.com", "checkout")
	if len(records) != 1 {
		t.Fatalf("checkout consent records = %d, want one", len(records))
	}
	if records[0].AdulthoodDeclaration.Valid {
		t.Fatalf("recorded adulthood_declaration = %+v, want NULL — the act did not ask", records[0].AdulthoodDeclaration)
	}
}

// TestUnowedAdulthoodDeclarationIsDropped: the owed-boxes rule over the new
// field. A buyer current on the Terms is not asked, and a crafted body cannot
// manufacture a declaration for them — nor, for that matter, refuse their own
// checkout by sending a `false` for a box nobody drew.
func TestUnowedAdulthoodDeclarationIsDropped(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Crafted Adult Fest", "crafted-adult-fest", 1000, 10)

	token := customerSignIn(t, env, "cristina@example.com")
	if boxes := signedInConsentBoxes(t, env, token); boxes.TermsAcceptance || boxes.AdulthoodDeclaration {
		t.Fatalf("boxes = %+v, want none owed for a Customer current on the Terms", boxes)
	}
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	// A tick for a box that was never drawn: DROPPED, so nothing is held and
	// nothing is evidenced.
	body := consentCheckoutBody("Cristina", "Paz", nil, nil, nil, cartLine(gaID, 1))
	body["terms_acceptance"] = true
	body["adulthood_declaration"] = true
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "crafted-adult-fest", token, body)
	if declaration := readPaymentAdulthoodHold(t, env, begin.ClientTransactionID); declaration.Valid {
		t.Fatalf("held declaration = %+v, want the unowed answer DROPPED at begin", declaration)
	}

	// And an untick for a box that was never drawn cannot refuse a checkout
	// this buyer is entitled to: what was owed is the server's finding, so a
	// `false` here is not an answer to anything.
	refused := consentCheckoutBody("Cristina", "Paz", nil, nil, nil, cartLine(gaID, 1))
	refused["adulthood_declaration"] = false
	second := beginCheckoutWithEvidenceOK(t, env, "test-org", "crafted-adult-fest", token, refused)
	if declaration := readPaymentAdulthoodHold(t, env, second.ClientTransactionID); declaration.Valid {
		t.Fatalf("held declaration = %+v, want NULL from a dropped untick", declaration)
	}

	if confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}
	if got := len(consentRecordsOn(t, env, "cristina@example.com", "checkout")); got != 0 {
		t.Fatalf("checkout consent records = %d, want 0 from dropped answers alone", got)
	}
}
