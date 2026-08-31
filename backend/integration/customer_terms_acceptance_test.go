package integration

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// Terms acceptance on the Customer platform (#536, parent #533, ADR 0066): the
// attendee-capacity acceptance, riding the sign-in consent gate the Privacy
// Policy built.
//
// The seam is the same one customer_consent_test.go drives — a sign-in either
// mints a session or names what is owed, a submission either mints one or is
// refused — and the evidence and state are asserted through SQL for that file's
// reason: the platform deliberately publishes neither.

// readTermsState reads one Customer's Terms current-state pair.
func readTermsState(t *testing.T, env *testEnv, email string) (sql.NullTime, sql.NullString) {
	t.Helper()
	var acceptedAt sql.NullTime
	var versionID sql.NullString
	if err := env.db.QueryRow(`
		SELECT terms_accepted_at, terms_version_id FROM customers WHERE email = $1
	`, email).Scan(&acceptedAt, &versionID); err != nil {
		t.Fatalf("read terms state for %q: %v", email, err)
	}
	return acceptedAt, versionID
}

// readTermsAnswers reads the Terms answer pair off every Consent Record for one
// Customer, oldest first — the columns readConsentRecords predates.
func readTermsAnswers(t *testing.T, env *testEnv, email string) []struct {
	Answer    sql.NullBool
	VersionID sql.NullString
} {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT r.terms_acceptance, r.terms_version_id
		FROM consent_records r
		JOIN customers c ON c.id = r.customer_id
		WHERE c.email = $1
		ORDER BY r.captured_at ASC, r.id ASC
	`, email)
	if err != nil {
		t.Fatalf("read terms answers for %q: %v", email, err)
	}
	defer rows.Close()
	var answers []struct {
		Answer    sql.NullBool
		VersionID sql.NullString
	}
	for rows.Next() {
		var a struct {
			Answer    sql.NullBool
			VersionID sql.NullString
		}
		if err := rows.Scan(&a.Answer, &a.VersionID); err != nil {
			t.Fatalf("scan terms answer: %v", err)
		}
		answers = append(answers, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate terms answers: %v", err)
	}
	return answers
}

// currentTermsVersionID reads the edition in effect, by the same rule the
// backend resolves it with.
func currentTermsVersionID(t *testing.T, env *testEnv) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		SELECT id FROM terms_versions
		WHERE effective_date <= CURRENT_DATE
		ORDER BY effective_date DESC, created_at DESC
		LIMIT 1
	`).Scan(&id); err != nil {
		t.Fatalf("read current terms version: %v", err)
	}
	return id
}

// publishTermsVersion inserts a later Terms edition, which is the WHOLE of a
// version bump: no code changes, no data migration (#536).
//
// It publishes TEXT with the row, because since #558 that is what an edition
// is: a row with no artifacts is an edition nobody can be shown, so the sign-in
// gate refuses to display it and the public page 404s. The fingerprint here is
// junk on purpose — these tests are about re-gating, not about evidence, and a
// mismatch is logged and served rather than refused (service.termsViews).
// It takes a LINEAGE and not a label — publishPolicyVersion's rule (#560) over
// the parallel table.
func publishTermsVersion(t *testing.T, env *testEnv, generation, revision int) string {
	t.Helper()
	label := legal.Lineage{Generation: generation, Revision: revision}.Label()
	var id string
	if err := env.db.QueryRow(`
		INSERT INTO terms_versions (label, generation, revision, effective_date, content_hash)
		VALUES ($1, $2, $3, CURRENT_DATE, repeat('b', 64))
		RETURNING id
	`, label, generation, revision).Scan(&id); err != nil {
		t.Fatalf("publish terms version %q: %v", label, err)
	}
	if _, err := env.db.Exec(`
		INSERT INTO terms_version_artifacts (version_id, locale, slug, ordinal, body)
		SELECT $1, locale, slug, ordinal, format('%s (%s, edition %s)', slug, locale, $2::text)
		FROM (VALUES ('en', 'label-terms-acceptance', 1), ('en', 'terms', 2),
		             ('es', 'label-terms-acceptance', 1), ('es', 'terms', 2))
		     AS artifact (locale, slug, ordinal)
	`, id, label); err != nil {
		t.Fatalf("publish terms version %q text: %v", label, err)
	}
	return id
}

// resetOptionalConsentsToUnanswered puts one Customer's optional consents back
// to never-answered, leaving their Policy and Terms acceptances standing.
//
// It exists because the one-time Terms re-gate (#536) means every Customer who
// signs in is now stopped once — and a stopped Customer is shown, and so
// answers, the optional boxes — while a guest's tick can only pend over a box
// the owner never answered (ADR 0035). The tests about Pending Confirmation
// therefore sign the owner in first and then reset, which is the state
// genuinely reachable in production by an account whose owner has not signed in
// since the boxes existed — the same justification
// signInAnsweringMarketingOnly gives for its own direct write.
func resetOptionalConsentsToUnanswered(t *testing.T, env *testEnv, email string) {
	t.Helper()
	if _, err := env.db.Exec(`
		UPDATE customers SET marketing_consent = NULL, networking_consent = NULL WHERE email = $1
	`, email); err != nil {
		t.Fatalf("reset optional consents for %q: %v", email, err)
	}
}

// TestSignInOwesTheTermsBoxAndRefusesASubmissionWithoutIt is the gate and the
// refusal in one story: the box is owed beside the policy's, and ticking every
// other box does not buy a session.
func TestSignInOwesTheTermsBoxAndRefusesASubmissionWithoutIt(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "tania@example.com")
	if data.ConsentRequired == nil {
		t.Fatal("expected a consent-required outcome — every Customer owes Terms edition 1")
	}
	if !data.ConsentRequired.Boxes.TermsAcceptance {
		t.Fatal("boxes.terms_acceptance = false, want the terms box owed on a first sign-in")
	}

	// Everything ticked but the Terms: refused, and the refusal names the box.
	answers := consentAnswers(data.ConsentRequired.PendingConsentToken, true, true, true)
	answers["terms_acceptance"] = false
	resp, body := env.post(t, customerConsentPath, answers, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a submission without the terms box", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "TERMS_ACCEPTANCE_REQUIRED" {
		t.Fatalf("error = %+v, want TERMS_ACCEPTANCE_REQUIRED", body.Error)
	}
	if n := countCustomerSessions(t, env); n != 0 {
		t.Fatalf("customer sessions = %d, want 0 — no session without the contractual acceptance", n)
	}
	if _, versionID := readTermsState(t, env, "tania@example.com"); versionID.Valid {
		t.Fatal("a refused submission must stamp no Terms acceptance")
	}
}

// TestTermsAcceptanceRecordsTheEditionAndNeverReappears is the crossing and the
// one-time property: one Consent Record carrying the current edition, the state
// stamped, and the next sign-in owing nothing.
func TestTermsAcceptanceRecordsTheEditionAndNeverReappears(t *testing.T) {
	env := setupTest(t)

	data := startSignIn(t, env, "tania@example.com")
	resp, body := env.post(t, customerConsentPath,
		consentAnswers(data.ConsentRequired.PendingConsentToken, true, false, false),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if decodeCustomerVerify(t, body).SessionID == "" {
		t.Fatal("expected a Customer Session once both required boxes are ticked")
	}

	current := currentTermsVersionID(t, env)
	answers := readTermsAnswers(t, env, "tania@example.com")
	if len(answers) != 1 {
		t.Fatalf("consent records = %d, want exactly one for one capture act", len(answers))
	}
	if !answers[0].Answer.Valid || !answers[0].Answer.Bool {
		t.Fatalf("terms_acceptance = %+v, want an explicit true", answers[0].Answer)
	}
	if !answers[0].VersionID.Valid || answers[0].VersionID.String != current {
		t.Fatalf("terms_version_id = %+v, want the current edition %s", answers[0].VersionID, current)
	}
	acceptedAt, versionID := readTermsState(t, env, "tania@example.com")
	if !acceptedAt.Valid || !versionID.Valid || versionID.String != current {
		t.Fatalf("terms state = (%+v, %+v), want a stamp of edition %s", acceptedAt, versionID, current)
	}

	// The second sign-in sails through: the re-gate is exactly once.
	second := startSignIn(t, env, "tania@example.com")
	if second.SessionID == "" || second.ConsentRequired != nil {
		t.Fatalf("second sign-in = %+v, want a session and no consent step", second.ConsentRequired)
	}
}

// TestPublishingATermsVersionRegatesWithTheTermsBoxAlone is the version-bump
// story: one INSERT re-gates everybody, and the person is shown the terms box
// and nothing else — their Policy Acceptance and optional answers stand.
func TestPublishingATermsVersionRegatesWithTheTermsBoxAlone(t *testing.T) {
	env := setupTest(t)

	customerSignIn(t, env, "tania@example.com")
	later := publishTermsVersion(t, env, 2, 0)

	// A later capture must not collide with the first on the fixed clock.
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	data := startSignIn(t, env, "tania@example.com")
	if data.SessionID != "" || data.ConsentRequired == nil {
		t.Fatal("expected a consent step after a Terms edition was published")
	}
	want := consentBoxes{TermsAcceptance: true}
	if data.ConsentRequired.Boxes != want {
		t.Fatalf("boxes = %+v, want the terms box alone: %+v", data.ConsentRequired.Boxes, want)
	}

	// The terms box alone finishes it — no policy_acceptance on the submission,
	// because the person was not shown that box.
	resp, body := env.post(t, customerConsentPath, map[string]any{
		"pending_consent_token": data.ConsentRequired.PendingConsentToken,
		"terms_acceptance":      true,
	}, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("terms-only submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if decodeCustomerVerify(t, body).SessionID == "" {
		t.Fatal("expected a Customer Session from a terms-only re-acceptance")
	}

	answers := readTermsAnswers(t, env, "tania@example.com")
	if len(answers) != 2 {
		t.Fatalf("consent records = %d, want two capture acts", len(answers))
	}
	if !answers[1].VersionID.Valid || answers[1].VersionID.String != later {
		t.Fatalf("re-acceptance edition = %+v, want %s", answers[1].VersionID, later)
	}
	// The re-acceptance said nothing about the Privacy Policy: NULL, not false.
	records := readConsentRecords(t, env, "tania@example.com")
	if records[1].PolicyAcceptance.Valid {
		t.Fatalf("policy_acceptance = %+v on a terms-only act, want NULL — the box was not shown", records[1].PolicyAcceptance)
	}
	_, versionID := readTermsState(t, env, "tania@example.com")
	if versionID.String != later {
		t.Fatalf("terms state = %+v, want re-stamped to %s", versionID, later)
	}
}

// TestWithdrawAllLeavesTermsAcceptanceUntouched is the no-withdrawal ruling
// (ADR 0066) at the widest channel there is: the act that takes away every
// optional consent moves nothing contractual.
func TestWithdrawAllLeavesTermsAcceptanceUntouched(t *testing.T) {
	env := setupTest(t)

	token := customerSignIn(t, env, "tania@example.com")
	acceptedAt, versionID := readTermsState(t, env, "tania@example.com")
	if !versionID.Valid {
		t.Fatal("expected a Terms acceptance from the sign-in helper")
	}

	resp, body := env.post(t, "/api/v1/customer/privacy/withdraw-all", nil, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdraw all status=%d error=%+v", resp.StatusCode, body.Error)
	}

	afterAt, afterVersion := readTermsState(t, env, "tania@example.com")
	if afterAt != acceptedAt || afterVersion != versionID {
		t.Fatalf("terms state moved from (%+v, %+v) to (%+v, %+v) — Withdraw All must not touch it",
			acceptedAt, versionID, afterAt, afterVersion)
	}
	// And the withdrawal's own record says nothing about the Terms. Selected by
	// CHANNEL, not by position: the harness's fixed clock ties captured_at, so
	// "the last row" is a coin toss (see consentRecordsOn).
	var withdrawalAnswer sql.NullBool
	if err := env.db.QueryRow(`
		SELECT r.terms_acceptance
		FROM consent_records r
		JOIN customers c ON c.id = r.customer_id
		WHERE c.email = $1 AND r.channel = 'account_settings'
	`, "tania@example.com").Scan(&withdrawalAnswer); err != nil {
		t.Fatalf("read withdrawal record: %v", err)
	}
	if withdrawalAnswer.Valid {
		t.Fatalf("terms_acceptance = %+v on a Withdraw All, want NULL — the box was not shown", withdrawalAnswer)
	}
}
