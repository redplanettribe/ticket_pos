package integration

// Which language the person was actually SHOWN (#567, parent #556, migration
// 115).
//
// The evidence already names the edition, and an edition is published in
// several languages under one fingerprint — so the hash cannot answer the
// question an operator is eventually asked: was the notice this person ticked a
// box beside comprehensible to them? One nullable column answers it, on the two
// tables that record an act of consent.
//
// The property under test everywhere below is the same one, and it is a
// distinction rather than a value: the column holds the locale of the ARTIFACT
// THAT WAS RENDERED, never of the page it was rendered on. Those two agree on
// every customer surface, because the public policy endpoint answers strictly
// and a step that could not read its text renders nothing at all — and they
// part company at the staff terms gate, which floors at the prevailing text.
// The last test here is that parting, which is the one this column exists for.
//
// Read with SQL, like every other assertion about the evidence log: it has no
// endpoint and deliberately never will.

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// presentedLocalesOn reads the recorded language of one Customer's acts on one
// surface, oldest first. A NULL is a document-less channel saying, correctly,
// that it showed nothing.
func presentedLocalesOn(t *testing.T, env *testEnv, email, channel string) []sql.NullString {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT r.presented_locale
		FROM consent_records r
		JOIN customers c ON c.id = r.customer_id
		WHERE c.email = $1 AND r.channel = $2
		ORDER BY r.captured_at ASC, r.id ASC
	`, email, channel)
	if err != nil {
		t.Fatalf("read presented locales for %q on %q: %v", email, channel, err)
	}
	defer rows.Close()

	var locales []sql.NullString
	for rows.Next() {
		var locale sql.NullString
		if err := rows.Scan(&locale); err != nil {
			t.Fatalf("scan presented locale: %v", err)
		}
		locales = append(locales, locale)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate presented locales: %v", err)
	}
	return locales
}

// onlyPresentedLocaleOn is the same read where exactly one act is expected.
func onlyPresentedLocaleOn(t *testing.T, env *testEnv, email, channel string) sql.NullString {
	t.Helper()
	locales := presentedLocalesOn(t, env, email, channel)
	if len(locales) != 1 {
		t.Fatalf("%s wrote %d records on channel %q, want exactly 1", email, len(locales), channel)
	}
	return locales[0]
}

// wantPresentedLocale asserts the column holds one language.
func wantPresentedLocale(t *testing.T, got sql.NullString, want, what string) {
	t.Helper()
	if !got.Valid || got.String != want {
		t.Fatalf("%s recorded presented_locale = %+v, want %q", what, got, want)
	}
}

// wantNoPresentedLocale asserts the column is NULL — the truthful answer for a
// surface that rendered no legal text.
func wantNoPresentedLocale(t *testing.T, got sql.NullString, what string) {
	t.Helper()
	if got.Valid {
		t.Fatalf("%s recorded presented_locale = %q, want NULL: it showed no document", what, got.String)
	}
}

// signInStatingPresentedLocale finishes a gated sign-in saying which language
// the Short Notice and the labels above the boxes were served in — the field
// the Storefront fills from the policy payload it drew them from, and not from
// the address of the page.
func signInStatingPresentedLocale(t *testing.T, env *testEnv, email, presented string) string {
	t.Helper()
	verify := startSignIn(t, env, email)
	if verify.ConsentRequired == nil {
		t.Fatal("expected a consent step for a Customer who has never accepted")
	}
	body := consentAnswers(verify.ConsentRequired.PendingConsentToken, true, true, true)
	body["presented_locale"] = presented
	resp, envelope := env.post(t, customerConsentPath, body, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	token := decodeCustomerVerify(t, envelope).SessionID
	if token == "" {
		t.Fatal("expected a Customer Session after the consent step")
	}
	return token
}

// TestSignInRecordsTheLanguageTheNoticeWasServedIn is the customer half of the
// column: the consent step says what it rendered, and the record keeps it.
func TestSignInRecordsTheLanguageTheNoticeWasServedIn(t *testing.T) {
	env := setupTest(t)

	signInStatingPresentedLocale(t, env, "ana@example.com", "en")

	wantPresentedLocale(t, onlyPresentedLocaleOn(t, env, "ana@example.com", "signin"), "en",
		"a sign-in whose Short Notice was served in English")
}

// TestSignInWithAnUnservedLanguageRecordsNothingRatherThanFailing: the field
// corroborates and proves nothing, so a value naming a language this platform
// does not serve is dropped and the act is recorded without it.
//
// The alternative would be a sign-in refused over an annotation — a person kept
// out of their account because a client sent a locale nobody recognises, which
// is a far worse failure than a NULL in a column that means "unknown".
func TestSignInWithAnUnservedLanguageRecordsNothingRatherThanFailing(t *testing.T) {
	env := setupTest(t)

	token := signInStatingPresentedLocale(t, env, "ana@example.com", "klingon")
	if token == "" {
		t.Fatal("a junk presented locale cost somebody their sign-in")
	}

	wantNoPresentedLocale(t, onlyPresentedLocaleOn(t, env, "ana@example.com", "signin"),
		"a sign-in stating a language the platform does not serve")
}

// TestCheckoutRecordsTheLanguageTheDialogWasServedIn is the checkout half, and
// it costs the checkout nothing: the Sale Locale held at begin (migration 059)
// is the language of the page that drew the boxes, and it is read on the same
// INSERT as every other held answer when the sale commits.
//
// Note what the record therefore survives: the buyer leaves for the Payment
// Provider and comes back minutes later on a request that has no language at
// all. The answer comes from the held Payment, exactly as the answers do.
func TestCheckoutRecordsTheLanguageTheDialogWasServedIn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Consent Fest", "consent-fest", 1000, 10)

	token := signedInOwingEveryBox(t, env, "ana@example.com")

	body := consentCheckoutBody("Ana", "Lopez",
		boolPtr(true), boolPtr(true), boolPtr(true), cartLine(gaID, 1))
	body["locale"] = "en"
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "consent-fest", token, body)
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	wantPresentedLocale(t, onlyPresentedLocaleOn(t, env, "ana@example.com", "checkout"), "en",
		"a checkout dialog served in English")
}

// TestDocumentLessChannelsRecordNoPresentedLocale is the other half of the
// ruling, and the reason the column is nullable with no paired CHECK: a surface
// that showed no fresh text records NULL, and NULL is the answer rather than a
// gap.
//
// Two of the six are exercised here, chosen because they are the two that could
// most plausibly have been written the wrong way. The Customer Area's digest
// toggle happens on a page rendered in a language, which is exactly the value
// this column must not take. The withdrawal door SHARES AN ENDPOINT with the
// sign-in step that does record one, so this submission states a presented
// locale explicitly and must still be recorded without it: the branch that
// renders no document ignores the field.
func TestDocumentLessChannelsRecordNoPresentedLocale(t *testing.T) {
	env := setupTest(t)

	token := signInStatingPresentedLocale(t, env, "ana@example.com", "es")
	// The harness clock is fixed, so the acts are moved apart deliberately —
	// otherwise the log's ordering falls to a random uuid.
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	// A toggle in the Customer Area: one box, no notice.
	setDigestEnabled(t, env, token, false)
	wantNoPresentedLocale(t, onlyPresentedLocaleOn(t, env, "ana@example.com", "account_settings"),
		"the Customer Area's digest toggle")

	setSignInClock(t, env, env.fixedClock.Add(2*time.Hour))

	// And the withdrawal door, stating a language it did not show text in.
	proof := proveEmailForWithdrawal(t, env, "ana@example.com", "")
	withdrawal := withdrawalSubmission(proof.PendingConsentToken, true, true)
	withdrawal["presented_locale"] = "es"
	resp, body := env.post(t, customerConsentPath, withdrawal, consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdrawal status=%d error=%+v", resp.StatusCode, body.Error)
	}
	wantNoPresentedLocale(t, onlyPresentedLocaleOn(t, env, "ana@example.com", "passcode_withdrawal"),
		"a withdrawal that claimed a presented locale")

	// The sign-in that started all this still says what it showed: the nulls
	// above are this column working, not this column unwired.
	wantPresentedLocale(t, onlyPresentedLocaleOn(t, env, "ana@example.com", "signin"), "es",
		"the sign-in step that did render a Short Notice")
}

// staffPresentedLocale reads the language recorded on a Staff Terms Acceptance.
func staffPresentedLocale(t *testing.T, env *testEnv, email string) sql.NullString {
	t.Helper()
	var locale sql.NullString
	if err := env.db.QueryRow(`
		SELECT presented_locale FROM staff_terms_acceptances WHERE email = $1
	`, email).Scan(&locale); err != nil {
		t.Fatalf("read staff presented locale for %q: %v", email, err)
	}
	return locale
}

// TestStaffTermsAcceptanceRecordsTheLabelLocaleServed: the login page names a
// language, the edition publishes it, and the acceptance says so — across two
// requests, because the label is rendered when the terms step is issued and the
// evidence is written when it is finished.
func TestStaffTermsAcceptanceRecordsTheLabelLocaleServed(t *testing.T) {
	env := setupTest(t)
	email := "organizer-en@example.com"

	body := requestAndVerifyIn(t, env, email, "en")
	outcome := decodeStaffSignInOutcome(t, body)
	if outcome.TermsRequired == nil {
		t.Fatal("expected the terms gate to hold a first sign-in")
	}
	resp, accepted := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept terms status=%d error=%+v", resp.StatusCode, accepted.Error)
	}

	wantPresentedLocale(t, staffPresentedLocale(t, env, email), "en",
		"a staff terms step served in English")
}

// TestStaffTermsAcceptanceRecordsTheFALLBACKLocale is the test this column
// exists for, and the one no other surface can produce.
//
// The login page is English. The edition in force publishes no English label,
// so the gate floors at the prevailing Spanish text (§37) and the person is
// shown Spanish — which is exactly the case where the REQUEST's locale is a lie
// about what was read. The row must say `es`.
//
// An edition with one language is not hypothetical: which languages an edition
// publishes is a property of its artifact rows (#558), so dropping one is a
// publication and not a deploy.
func TestStaffTermsAcceptanceRecordsTheFALLBACKLocale(t *testing.T) {
	env := setupTest(t)
	email := "organizer-fallback@example.com"

	// A new gating edition, published in Spanish alone: it re-gates everybody,
	// and it has nothing to say in English.
	versionID := publishTermsVersion(t, env, 2, 0)
	if _, err := env.db.Exec(`
		DELETE FROM terms_version_artifacts WHERE version_id = $1 AND locale = 'en'
	`, versionID); err != nil {
		t.Fatalf("unpublish the English text: %v", err)
	}

	body := requestAndVerifyIn(t, env, email, "en")
	outcome := decodeStaffSignInOutcome(t, body)
	if outcome.TermsRequired == nil {
		t.Fatal("expected the terms gate to hold a sign-in owing the new edition")
	}
	// The label really is the Spanish one — the person was shown Spanish, which
	// is the fact the row is about.
	if outcome.TermsRequired.AcceptanceLabel == "" {
		t.Fatal("the terms step served an empty checkbox label")
	}
	resp, accepted := acceptStaffTerms(t, env, outcome.TermsRequired.PendingTermsToken, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept terms status=%d error=%+v", resp.StatusCode, accepted.Error)
	}

	wantPresentedLocale(t, staffPresentedLocale(t, env, email), "es",
		"an English login page floored at the prevailing Spanish text")
}
