package integration

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A Consent Withdrawal is confirmed to the Customer (#267, parent #265).
//
// THE PROPERTY UNDER TEST IS NOT "A CONSENT WAS ANSWERED NO". It is that the
// act MOVED a consent out of granted or Pending Confirmation, which is a fact
// about what changed rather than about what was submitted — and the two come
// apart in exactly the case that matters: switching off a switch that was
// already off answers No and takes nothing away, and mailing somebody about it
// would be telling them a change happened when none did. Every test below is
// either the positive or the negative half of that sentence.
//
// The mail is TRANSACTIONAL and is sent even though the person has just asked to
// stop receiving marketing. It sits on the same footing as a Sale Confirmation
// or a passcode: suppressing it would make the one act that must be confirmed
// the one act met with silence.
//
// Both surfaces that can perform a withdrawal today are covered — the
// unsubscribe link at the foot of a Follow Digest and the Customer Area's digest
// toggle — because they are two entry points to one switch (ADR 0034) and a
// confirmation wired to one of them would be a confirmation the most reachable
// surface does not send. The unsubscribe link is the important case rather than
// the marginal one: it authenticates nobody, so this mail is how the address's
// real owner learns somebody acted on their behalf.
//
// Asserted through the harness's capture email sender and, for the evidence,
// through the database handle, exactly as the consent tests already established.

// withdrawalConfirmationsFor is every Consent Withdrawal confirmation delivered
// to one address, kept as the message rather than as a rendered string so a test
// can call Subject() and Text() itself — which is the only way the language a
// recipient was written to in is visible at all.
func withdrawalConfirmationsFor(t *testing.T, env *testEnv, email string) []platform.ConsentWithdrawalConfirmation {
	t.Helper()
	var out []platform.ConsentWithdrawalConfirmation
	for _, confirmation := range env.email.ConsentWithdrawalConfirmations() {
		if confirmation.To == email {
			out = append(out, confirmation)
		}
	}
	return out
}

// onlyWithdrawalConfirmation is the single confirmation one act produced,
// failing loudly on none and on two: "somebody was told" and "somebody was told
// twice" are different outcomes, and only one of them is the criterion.
func onlyWithdrawalConfirmation(t *testing.T, env *testEnv, email string) platform.ConsentWithdrawalConfirmation {
	t.Helper()
	confirmations := withdrawalConfirmationsFor(t, env, email)
	if len(confirmations) != 1 {
		t.Fatalf("%s received %d Consent Withdrawal confirmations, want exactly 1", email, len(confirmations))
	}
	return confirmations[0]
}

// wantNoWithdrawalConfirmation is the negative half, and it is the assertion
// that fails against the obvious wrong implementation — one that mails whenever
// an answer is No.
func wantNoWithdrawalConfirmation(t *testing.T, env *testEnv, email, because string) {
	t.Helper()
	if confirmations := withdrawalConfirmationsFor(t, env, email); len(confirmations) != 0 {
		t.Fatalf("%s was mailed %d withdrawal confirmations, want none: %s", email, len(confirmations), because)
	}
}

// wantConfirmationStamped asserts the evidence carries the confirmation-sent
// stamp — the fact a compliance officer must be able to establish from the
// platform's own records rather than from a mail provider's retention window.
func wantConfirmationStamped(t *testing.T, env *testEnv, email, channel string) {
	t.Helper()
	record := onlyRecordOn(t, env, email, channel)
	if !record.ConfirmationSentAt.Valid {
		t.Fatalf("the %s record carries no confirmation_sent_at after the Customer was mailed: %+v", channel, record)
	}
}

// signInAnsweringInLocale is signInAnswering for a Customer who reached the
// Storefront in a named language, which is the only way a Mail Locale other
// than the default is ever recorded (ADR 0033).
func signInAnsweringInLocale(t *testing.T, env *testEnv, email, locale string, policy, marketing, networking bool) string {
	t.Helper()
	resp, body := requestCustomerPasscode(t, env, email, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email":  email,
		"code":   env.email.LastCode,
		"locale": locale,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	verify := decodeCustomerVerify(t, body)
	if verify.ConsentRequired == nil {
		t.Fatal("expected a consent step for a Customer who has never accepted")
	}

	resp, body = env.post(t, customerConsentPath,
		consentAnswers(verify.ConsentRequired.PendingConsentToken, policy, marketing, networking),
		consentEvidenceHeaders())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent submission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	token := decodeCustomerVerify(t, body).SessionID
	if token == "" {
		t.Fatal("expected a Customer Session after the consent step")
	}
	return token
}

// TestUnsubscribeLinkConfirmsTheWithdrawal is the acceptance criterion that
// matters most, on the surface that matters most.
//
// The unsubscribe link is the one anybody holding a forwarded Digest can press,
// and it authenticates nobody. Confirming it is therefore how the address's real
// owner finds out — so the mail goes to the Customer's own stored address, which
// is the address the link's holder may not be reading.
func TestUnsubscribeLinkConfirmsTheWithdrawal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// A Customer who granted Marketing at sign-in, Followed, received a Digest
	// and pressed the link in it.
	discoverableEvent(t, env, sessionID, "Withdrawal Fest", "withdrawal-fest", env.fixedClock.Add(72*time.Hour))
	followOrganizationOK(t, env, signInAnswering(t, env, "ana@example.com", true, true, false), testOrgSlug)
	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digests := digestsFor(t, env, "ana@example.com")
	if len(digests) != 1 {
		t.Fatalf("ana received %d Digests, want exactly 1", len(digests))
	}
	unsubscribeWithTokenOK(t, env, unsubscribeTokenFrom(t, digests[0]))

	confirmation := onlyWithdrawalConfirmation(t, env, "ana@example.com")
	if confirmation.Subject() == "" {
		t.Fatal("the withdrawal confirmation has no subject line")
	}
	// The evidence carries the fact that it was sent, on the row the act wrote.
	wantConfirmationStamped(t, env, "ana@example.com", "unsubscribe_link")
	// And the withdrawal itself is exactly what it was before this feature.
	if digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("the Digest is still on after an unsubscribe")
	}
}

// TestDigestToggleConfirmsTheWithdrawal is the same act through the other entry
// point, and it must produce the same message: the two are one switch rendered
// twice, and a Customer who used the one inside their account is owed the
// confirmation as much as one who pressed a link in an email.
func TestDigestToggleConfirmsTheWithdrawal(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, false)
	setDigestEnabled(t, env, token, false)

	onlyWithdrawalConfirmation(t, env, "ana@example.com")
	wantConfirmationStamped(t, env, "ana@example.com", "account_settings")
}

// TestSettlingSomebodyElsesTickAsNoIsConfirmed is the second transition that
// counts as taking something away: Pending Confirmation to denied.
//
// A guest ticked Marketing for an address they had not proven, so the platform
// was holding that tick unresolved against this person. The owner settles it as
// No, and something that had been standing against their address stops standing
// — which is a change, and is the change this mail exists to report. Testing
// only `granted -> denied` would leave this silent.
func TestSettlingSomebodyElsesTickAsNoIsConfirmed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), nil)
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q, want the pending state this test needs", state.MarketingConsent.String)
	}

	setDigestEnabled(t, env, customerSignIn(t, env, "ana@example.com"), false)

	onlyWithdrawalConfirmation(t, env, "ana@example.com")
	wantConfirmationStamped(t, env, "ana@example.com", "account_settings")
}

// TestNoConfirmationWhenTheActMovedNothing is the negative criterion, and the
// one that fails against an implementation reasoning from the answer instead of
// from what changed.
//
// The Customer declined Marketing at sign-in and then switches a Digest that was
// already off. A Consent Record is still written — the log says what happened,
// and a repeated act is still an act — and nothing is sent, because nothing
// moved.
func TestNoConfirmationWhenTheActMovedNothing(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, false, false)
	setDigestEnabled(t, env, token, false)

	wantNoWithdrawalConfirmation(t, env, "ana@example.com",
		"the consent was already denied, so the act took nothing away")

	// The evidence still records the act, and records that nobody was told.
	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	if record.ConfirmationSentAt.Valid {
		t.Fatalf("confirmation_sent_at is stamped on an act that sent nothing: %+v", record)
	}
	wantPrior(t, record, "denied", "")
}

// TestNoConfirmationWhenTheActGranted is the other half of the negative, and it
// is why the rule tests the destination as well as the origin: a grant also
// moves a consent, and confirming one as a withdrawal would tell somebody the
// opposite of what they just did.
func TestNoConfirmationWhenTheActGranted(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, false, false)
	setDigestEnabled(t, env, token, true)

	wantNoWithdrawalConfirmation(t, env, "ana@example.com",
		"the act granted Marketing Consent rather than withdrawing it")
}

// TestWithdrawalConfirmationIsWrittenInTheCustomersMailLocale: a message about
// somebody's legal rights is the last one that may arrive in a language they
// cannot read (ADR 0033).
//
// Asserted on the rendered body rather than on the Locale field, because the
// language is only observable once the message is rendered — a field carrying
// "es" beside English copy would pass a field assertion and fail the reader.
func TestWithdrawalConfirmationIsWrittenInTheCustomersMailLocale(t *testing.T) {
	env := setupTest(t)

	token := signInAnsweringInLocale(t, env, "ana@example.com", "es", true, true, false)
	setDigestEnabled(t, env, token, false)

	confirmation := onlyWithdrawalConfirmation(t, env, "ana@example.com")
	if !strings.Contains(confirmation.Text(), "revocó el consentimiento de marketing") {
		t.Fatalf("the confirmation is not written in Spanish for a Customer who signed in on a Spanish Storefront; body:\n%s", confirmation.Text())
	}
	if !strings.Contains(confirmation.Subject(), "Se revocó") {
		t.Fatalf("the subject is not written in Spanish; subject: %q", confirmation.Subject())
	}
}

// TestWithdrawalConfirmationSaysWhatContinues holds the copy to the two things
// the parent spec is emphatic about, in both languages.
//
// IT MUST NOT CLAIM PROCESSING HAS STOPPED. This platform keeps performing
// contract-based processing for anybody holding a Ticket, and keeps sending
// receipts, passcodes and reversal notices; copy promising otherwise would be
// untrue, and "passive" is scoped to consent-based processing alone
// (CONTEXT.md). And it must say the withdrawal can be undone, or it is a
// notification rather than a right the reader still holds.
func TestWithdrawalConfirmationSaysWhatContinues(t *testing.T) {
	env := setupTest(t)

	englishToken := signInAnswering(t, env, "ana@example.com", true, true, false)
	setDigestEnabled(t, env, englishToken, false)
	english := onlyWithdrawalConfirmation(t, env, "ana@example.com").Text()
	for _, phrase := range []string{
		"you will still receive purchase confirmations",
		"you can turn marketing email back on",
	} {
		if !strings.Contains(strings.ToLower(english), phrase) {
			t.Fatalf("the English confirmation does not say %q; body:\n%s", phrase, english)
		}
	}

	spanishToken := signInAnsweringInLocale(t, env, "bruno@example.com", "es", true, true, false)
	setDigestEnabled(t, env, spanishToken, false)
	spanish := onlyWithdrawalConfirmation(t, env, "bruno@example.com").Text()
	for _, phrase := range []string{
		"seguirá recibiendo las confirmaciones de compra",
		"puede volver a activar los correos de marketing",
	} {
		if !strings.Contains(strings.ToLower(spanish), phrase) {
			t.Fatalf("the Spanish confirmation does not say %q; body:\n%s", phrase, spanish)
		}
	}
}

// TestAFailedConfirmationDoesNotFailTheWithdrawal is the criterion that decides
// what this feature costs when the mail provider is down.
//
// THE WITHDRAWAL IS THE THING THAT HAD TO HAPPEN. A person who asked for quiet
// and was answered with an error would reasonably conclude they are still
// subscribed, and would have to ask again — so the request succeeds, the consent
// moves, the evidence stands, and only the stamp is absent. Null there means
// "not sent", which is a true and useful thing for the log to say.
func TestAFailedConfirmationDoesNotFailTheWithdrawal(t *testing.T) {
	env := setupTest(t)

	// The sign-in happens first: the injected failure applies to every message,
	// including the passcode this Customer needs to get a session at all.
	token := signInAnswering(t, env, "ana@example.com", true, true, false)
	env.email.FailWith(errors.New("mail provider is down"))

	view := setDigestEnabled(t, env, token, false)

	if view.DigestEnabled {
		t.Fatal("the toggle reported the Digest as still on after a withdrawal whose confirmation failed")
	}
	if digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("the Digest is still on: a failed confirmation rolled back the withdrawal")
	}
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q after a withdrawal whose confirmation failed, want denied", state.MarketingConsent.String)
	}
	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	if record.ConfirmationSentAt.Valid {
		t.Fatalf("confirmation_sent_at is stamped although the provider never took the message: %+v", record)
	}
}
