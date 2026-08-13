package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Customer's Privacy page (#268, parent #265): the read that reports what
// somebody has authorized, and the controls that move each optional consent in
// EITHER direction.
//
// The seam is HTTP, as always. What a person can observe — the four states, the
// mail, the resulting page — is asserted through the API; the evidence log and
// the consent columns are asserted through the harness's database handle,
// because the platform deliberately publishes neither and the existing consent
// tests established that pattern rather than inventing a seam.
//
// FOUR PROPERTIES CARRY THIS TICKET, and every test below is one of them:
//
//   - Rendering the page WRITES NOTHING. A settings page that recorded a
//     refusal because somebody read it would convert "never asked" into
//     "denied" for people who did nothing, so the read is asserted to leave the
//     evidence log and the consent columns untouched.
//   - All four states are DISTINGUISHABLE, including the two that are not
//     answers: never answered is not a refusal, and a Pending Confirmation is
//     somebody else's tick still standing.
//   - Both directions work, for both consents. A one-way page would trap a
//     Customer who withdrew Networking Consent by mistake, because the surfaces
//     that grant it show the box only while the state is unanswered.
//   - Withdrawing is confirmed by email and granting is not, which is #267's
//     rule reused rather than restated: the mail follows what MOVED, not what
//     was answered.

const customerPrivacyPath = "/api/v1/customer/privacy"

// optionalConsentsView is the pair of optional consents as the API reports
// them: four values each, and the four mean four different things.
type optionalConsentsView struct {
	MarketingConsent  string `json:"marketing_consent"`
	NetworkingConsent string `json:"networking_consent"`
}

// privacyView is the Privacy page's read.
type privacyView struct {
	PolicyVersion    *string              `json:"policy_version"`
	PolicyAcceptedAt *time.Time           `json:"policy_accepted_at"`
	Consents         optionalConsentsView `json:"consents"`
}

func consentPurposePath(purpose string) string {
	return customerPrivacyPath + "/consents/" + purpose
}

// readPrivacy is the page's own read, through the API and under a session.
func readPrivacy(t *testing.T, env *testEnv, token string) privacyView {
	t.Helper()
	resp, body := env.get(t, customerPrivacyPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read privacy status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("read privacy error=%+v, want none", body.Error)
	}
	var view privacyView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode privacy view: %v", err)
	}
	return view
}

// setOptionalConsent moves one control, and is the ONLY act on this surface
// that may write anything.
func setOptionalConsent(t *testing.T, env *testEnv, token, purpose string, granted bool) optionalConsentsView {
	t.Helper()
	resp, body := env.put(t, consentPurposePath(purpose),
		map[string]bool{"granted": granted}, mergeHeaders(authHeader(token), consentEvidenceHeaders()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set %s granted=%v status=%d error=%+v", purpose, granted, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("set %s granted=%v error=%+v, want none", purpose, granted, body.Error)
	}
	var view optionalConsentsView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode optional consents: %v", err)
	}
	return view
}

// mergeHeaders puts the session beside the evidence a real request arrives
// with, because a consent act on this surface records both.
func mergeHeaders(headers ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, set := range headers {
		for k, v := range set {
			merged[k] = v
		}
	}
	return merged
}

// wantConsents asserts the pair the API reported, which is what the page draws.
func wantConsents(t *testing.T, got optionalConsentsView, marketing, networking, why string) {
	t.Helper()
	want := optionalConsentsView{MarketingConsent: marketing, NetworkingConsent: networking}
	if got != want {
		t.Fatalf("consents = %+v, want %+v (%s)", got, want, why)
	}
}

// TestPrivacyPageReportsTheAcceptedPolicyVersionAndBothConsents is the read, and
// it asserts the three facts the page is made of at once because they are one
// screen: a Customer must be able to see what they agreed to, when, and where
// each optional consent stands.
//
// The Customer here answered the two optional boxes DIFFERENTLY. A read that
// reported both alike would pass against an implementation that had collapsed
// the states into a single flag.
func TestPrivacyPageReportsTheAcceptedPolicyVersionAndBothConsents(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, false)

	view := readPrivacy(t, env, token)
	if view.PolicyVersion == nil {
		t.Fatal("the Privacy page reports no accepted Policy Version for a Customer who just accepted one")
	}
	if view.PolicyAcceptedAt == nil {
		t.Fatal("the Privacy page reports no acceptance timestamp beside the version it names")
	}
	// The label the platform holds, not one the page invented. Read from the
	// evidence rather than hard-coded, so publishing a new edition does not make
	// this test a liar.
	var label string
	if err := env.db.QueryRow(`SELECT label FROM policy_versions WHERE id = $1`,
		currentPolicyVersionID(t, env)).Scan(&label); err != nil {
		t.Fatalf("read current policy version label: %v", err)
	}
	if *view.PolicyVersion != label {
		t.Fatalf("policy_version = %q, want the edition this Customer accepted (%q)", *view.PolicyVersion, label)
	}
	wantConsents(t, view.Consents, "granted", "denied",
		"the Customer ticked marketing and left networking unticked at sign-in")
}

// TestPrivacyPageDistinguishesNeverAnsweredFromDenied is the state the parent
// spec is most emphatic about: the platform must not put words in somebody's
// mouth.
//
// This Customer was asked about Marketing and never about Networking — the
// checkout showed one box and not the other — so one is an answer and the other
// is the absence of one. Reporting both as "denied" would be the platform
// claiming a refusal nobody made, and it is exactly the collapse a bool pair on
// the wire would force.
func TestPrivacyPageDistinguishesNeverAnsweredFromDenied(t *testing.T) {
	env := setupTest(t)

	token := signInAnsweringMarketingOnly(t, env, "ana@example.com", false)

	wantConsents(t, readPrivacy(t, env, token).Consents, "denied", "unanswered",
		"marketing was answered No and networking was never asked; the two are not the same fact")
}

// signInAnsweringMarketingOnly signs a Customer in, accepts the policy, answers
// marketing as given and leaves networking UNANSWERED.
//
// It is a sign-in through the ordinary door followed by a direct write of the
// networking column back to NULL, because every capture surface that shows the
// marketing box shows the networking one beside it — there is no route through
// the API that answers one and not the other at sign-in, and the state this test
// is about is genuinely reachable in production by a Customer whose account
// predates the networking box.
func signInAnsweringMarketingOnly(t *testing.T, env *testEnv, email string, marketing bool) string {
	t.Helper()
	token := signInAnswering(t, env, email, true, marketing, false)
	if _, err := env.db.Exec(`UPDATE customers SET networking_consent = NULL WHERE email = $1`, email); err != nil {
		t.Fatalf("clear networking consent for %q: %v", email, err)
	}
	return token
}

// TestPrivacyPageShowsAPendingConfirmationAsUnresolved is the fourth state, and
// it must not be reported as an answer.
//
// A guest ticked Marketing for an address they had not proven, so the platform
// is holding that tick unresolved (ADR 0035). It is denied for sending and
// unanswered for prompting, and it is NEITHER on this page: a Customer who saw
// "denied" would think they had refused, and one who saw "granted" would think
// they had agreed. They did neither, and somebody else did something.
func TestPrivacyPageShowsAPendingConfirmationAsUnresolved(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), nil)
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "pending_confirmation" {
		t.Fatalf("marketing_consent = %q, want the pending state this test needs", state.MarketingConsent.String)
	}

	// Both of the two states that are NOT answers, on one screen: the guest's
	// unresolved tick, and the box that guest was never shown. Neither may be
	// drawn as something the owner said.
	wantConsents(t, readPrivacy(t, env, customerSignIn(t, env, "ana@example.com")).Consents,
		"pending_confirmation", "unanswered",
		"somebody else's unresolved tick is not the owner's answer and must be shown as neither")
}

// TestLoadingThePrivacyPageWritesNothing is the boundary the parent spec draws
// in the strongest terms, and the one that would be silently wrong forever.
//
// A page that recorded a refusal because somebody read it would convert "never
// asked" into "denied" for everybody who opened it out of curiosity and closed
// it again — an act they never performed, written into an evidence log that
// exists precisely to say what people did. Reading it several times must be
// worth exactly as much as reading it none.
func TestLoadingThePrivacyPageWritesNothing(t *testing.T) {
	env := setupTest(t)

	// A Customer with something to lose in every column: an answered consent, an
	// unanswered one, and an acceptance on the row.
	token := signInAnsweringMarketingOnly(t, env, "ana@example.com", true)
	before := readConsentState(t, env, "ana@example.com")
	recordsBefore := len(readConsentRecords(t, env, "ana@example.com"))

	readPrivacy(t, env, token)
	readPrivacy(t, env, token)
	readPrivacy(t, env, token)

	if after := len(readConsentRecords(t, env, "ana@example.com")); after != recordsBefore {
		t.Fatalf("the evidence log grew from %d to %d rows because a page was rendered", recordsBefore, after)
	}
	after := readConsentState(t, env, "ana@example.com")
	if after != before {
		t.Fatalf("consent state moved by being read: before=%+v after=%+v", before, after)
	}
	// Said again as the property a person would notice: an unanswered consent is
	// still unanswered, and was not converted into a refusal.
	wantConsents(t, readPrivacy(t, env, token).Consents, "granted", "unanswered",
		"reading the page must not answer anything on the reader's behalf")
}

// TestWithdrawingMarketingFromThePrivacyPage is the central act: the state
// moves, the Digest goes with it, exactly one Consent Record is written under
// the account settings channel with the prior state and the technical proof,
// and the Customer is told.
func TestWithdrawingMarketingFromThePrivacyPage(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	view := setOptionalConsent(t, env, token, "marketing", false)

	wantConsents(t, view, "denied", "granted",
		"moving one control answers one box; the other consent is not shown and must not move")
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "denied" {
		t.Fatalf("marketing_consent = %q after a withdrawal, want denied", state.MarketingConsent.String)
	}
	// Exactly one record for the act, with what it took away legible from the
	// row alone (#266) and the circumstances of the act beside it.
	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	if !record.MarketingConsent.Valid || record.MarketingConsent.Bool {
		t.Fatalf("the record does not carry the No that was answered: %+v", record)
	}
	if record.NetworkingConsent.Valid {
		t.Fatalf("the record answers a networking box that was never shown: %+v", record)
	}
	wantPrior(t, record, "granted", "")
	if !record.EmailProven {
		t.Fatal("a withdrawal made behind a Customer Session is not recorded as proven")
	}
	if !record.IP.Valid || !record.UserAgent.Valid || !record.SessionID.Valid || !record.OriginURL.Valid {
		t.Fatalf("the withdrawal carries no technical proof: %+v", record)
	}
	// And the Customer was told, because this act took something away.
	onlyWithdrawalConfirmation(t, env, "ana@example.com")
	wantConfirmationStamped(t, env, "ana@example.com", "account_settings")
}

// TestWithdrawingMarketingSwitchesTheFollowDigestOffInLockstep is ADR 0034 held
// across the new surface: they are ONE SWITCH RENDERED TWICE, never two
// switches, so the `/following` toggle must reflect what was done on `/privacy`
// without anybody synchronising anything.
//
// Asserted through the Follows listing as well as through the column, because
// the listing is what the other page actually draws — a test of the column
// alone would pass against a listing that had come to compute the switch from
// somewhere else.
func TestWithdrawingMarketingSwitchesTheFollowDigestOffInLockstep(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	discoverableEvent(t, env, sessionID, "Lockstep Fest", "lockstep-fest", env.fixedClock.Add(72*time.Hour))

	token := signInAnswering(t, env, "ana@example.com", true, true, false)
	followOrganizationOK(t, env, token, testOrgSlug)
	if !listFollows(t, env, token).DigestEnabled {
		t.Fatal("the Digest is not on for a Customer who granted Marketing Consent at sign-in")
	}

	setOptionalConsent(t, env, token, "marketing", false)

	if digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("digest_enabled survived a Marketing Consent withdrawal made on the Privacy page")
	}
	listing := listFollows(t, env, token)
	if listing.DigestEnabled {
		t.Fatal("the Following page still shows the Digest as on after it was withdrawn on the Privacy page")
	}
	// Unsubscribing is a switch and not a purge: the Follow stands.
	if len(listing.Follows) != 1 {
		t.Fatalf("the Customer Follows %d things after withdrawing Marketing Consent, want 1", len(listing.Follows))
	}
}

// TestTheDigestToggleAndThePrivacyControlAreOneSwitch is the same property from
// the other side, and it is the one that fails if the two surfaces ever become
// two columns: a move made on `/following` must be what `/privacy` reports, and
// the other way round.
func TestTheDigestToggleAndThePrivacyControlAreOneSwitch(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, false)

	// Thrown on the Following page, read on the Privacy page.
	setDigestEnabled(t, env, token, false)
	wantConsents(t, readPrivacy(t, env, token).Consents, "denied", "denied",
		"the digest toggle answers the marketing box, so the Privacy page must report the same state")
	// Thrown on the Privacy page, read on the Following page.
	setOptionalConsent(t, env, token, "marketing", true)
	if !listFollows(t, env, token).DigestEnabled {
		t.Fatal("granting Marketing Consent on the Privacy page did not switch the Follow Digest back on")
	}
}

// TestGrantingFromThePrivacyPageSendsNothing is the negative half of the
// confirmation rule, on this surface.
//
// The mail follows what an act TOOK AWAY, and a grant took nothing away.
// Confirming one as a withdrawal would tell somebody the opposite of what they
// just did.
func TestGrantingFromThePrivacyPageSendsNothing(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, false, false)
	view := setOptionalConsent(t, env, token, "marketing", true)

	wantConsents(t, view, "granted", "denied", "an affirmative grant behind a proven session lands granted")
	wantNoWithdrawalConfirmation(t, env, "ana@example.com",
		"the act granted Marketing Consent rather than withdrawing it")
}

// TestNetworkingConsentMovesInBothDirectionsFromThePrivacyPage is the whole
// reason the page is not one-way, and it is the criterion the parent spec
// argues hardest for.
//
// Networking Consent is a one-way door everywhere else: the only surfaces that
// write it — sign-in and checkout — show the box solely while the state is
// unanswered, so a Customer who granted it once can never be shown that box
// again. Without this route back, withdrawing it by mistake would be permanent.
func TestNetworkingConsentMovesInBothDirectionsFromThePrivacyPage(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)

	withdrawn := setOptionalConsent(t, env, token, "networking", false)
	wantConsents(t, withdrawn, "granted", "denied",
		"withdrawing networking must leave a standing Marketing Consent alone")
	onlyWithdrawalConfirmation(t, env, "ana@example.com")
	wantConfirmationStamped(t, env, "ana@example.com", "account_settings")
	// The Digest is Marketing's switch and nothing to do with networking.
	if !digestEnabledInDatabase(t, env, "ana@example.com") {
		t.Fatal("withdrawing Networking Consent switched off the Follow Digest, which is Marketing's switch")
	}

	// And back again, which is the door that exists nowhere else.
	granted := setOptionalConsent(t, env, token, "networking", true)
	wantConsents(t, granted, "granted", "granted",
		"a Customer who withdrew Networking Consent by mistake can turn it back on")
	if state := readConsentState(t, env, "ana@example.com"); state.NetworkingConsent.String != "granted" {
		t.Fatalf("networking_consent = %q after being turned back on, want granted", state.NetworkingConsent.String)
	}
	// Two acts, two rows: the log says what happened, and the second one is a
	// grant rather than a second withdrawal.
	records := consentRecordsOn(t, env, "ana@example.com", "account_settings")
	if len(records) != 2 {
		t.Fatalf("two moves wrote %d records, want 2", len(records))
	}
	wantPrior(t, records[1], "", "denied")
}

// TestAPendingConfirmationIsSettledEitherWayFromThePrivacyPage: somebody else's
// tick is standing against this address, and the owner settles it by answering
// for themselves.
//
// Both directions in one test, because the criterion is that the Customer
// chooses — a page offering only "confirm" or only "refuse" would be settling it
// for them.
func TestAPendingConfirmationIsSettledEitherWayFromThePrivacyPage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// Refused. Settling somebody else's tick as No is a withdrawal too: something
	// had been standing against this address and stops standing.
	guestCheckoutPending(t, env, sessionID, "ana@example.com", "pending-fest", boolPtr(true), nil)
	anaToken := customerSignIn(t, env, "ana@example.com")
	wantConsents(t, setOptionalConsent(t, env, anaToken, "marketing", false), "denied", "unanswered",
		"the owner settled a Pending Confirmation as No")
	onlyWithdrawalConfirmation(t, env, "ana@example.com")
	wantPrior(t, onlyRecordOn(t, env, "ana@example.com", "account_settings"), "pending_confirmation", "")

	// Confirmed. The same starting state, settled the other way, and it sends
	// nothing: this is a grant.
	guestCheckoutPending(t, env, sessionID, "bruno@example.com", "pending-fest-two", boolPtr(true), nil)
	brunoToken := customerSignIn(t, env, "bruno@example.com")
	wantConsents(t, setOptionalConsent(t, env, brunoToken, "marketing", true), "granted", "unanswered",
		"the owner settled a Pending Confirmation as Yes")
	wantNoWithdrawalConfirmation(t, env, "bruno@example.com",
		"settling a pending tick as Yes is a grant and confirms no withdrawal")
	if !digestEnabledInDatabase(t, env, "bruno@example.com") {
		t.Fatal("confirming a pending Marketing Consent did not switch the Follow Digest on with it")
	}
}

// TestMovingAControlThatChangesNothingRecordsTheActAndSendsNothing is the no-op
// case, and it is where "was a consent answered No" and "did this take
// something away" come apart.
//
// A Consent Record is still written — the log says what HAPPENED, and a
// repeated act is still an act — and nobody is mailed, because reporting a
// change that did not happen is the failure this rule exists to prevent.
func TestMovingAControlThatChangesNothingRecordsTheActAndSendsNothing(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, false, false)
	before := readConsentState(t, env, "ana@example.com")

	wantConsents(t, setOptionalConsent(t, env, token, "networking", false), "denied", "denied",
		"switching off a switch that was already off changes nothing")

	if after := readConsentState(t, env, "ana@example.com"); after.NetworkingConsent != before.NetworkingConsent {
		t.Fatalf("networking_consent moved on a no-op: before=%+v after=%+v", before, after)
	}
	record := onlyRecordOn(t, env, "ana@example.com", "account_settings")
	wantPrior(t, record, "", "denied")
	if record.ConfirmationSentAt.Valid {
		t.Fatalf("confirmation_sent_at is stamped on an act that took nothing away: %+v", record)
	}
	wantNoWithdrawalConfirmation(t, env, "ana@example.com",
		"the consent was already denied, so the act took nothing away")
}

// TestPrivacySurfaceRefusesAnUnknownPurpose: the vocabulary is closed, and
// Policy Acceptance is deliberately not in it.
//
// Acceptance is not withdrawable — it is absent from counsel's form and gates
// the platform on a basis other than consent — so a request naming it is
// refused by the same rule that refuses a typo, and never by a special case
// that could later be relaxed.
func TestPrivacySurfaceRefusesAnUnknownPurpose(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	for _, purpose := range []string{"policy", "policy_acceptance", "everything"} {
		resp, body := env.put(t, consentPurposePath(purpose), map[string]bool{"granted": false}, authHeader(token))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("purpose %q status=%d, want 400", purpose, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("purpose %q error=%+v, want VALIDATION_FAILED", purpose, body.Error)
		}
	}
	// And nothing moved: a refused request is not a capture act.
	if records := consentRecordsOn(t, env, "ana@example.com", "account_settings"); len(records) != 0 {
		t.Fatalf("a refused request wrote %d Consent Records, want none", len(records))
	}
	if state := readConsentState(t, env, "ana@example.com"); !state.PolicyAcceptedAt.Valid {
		t.Fatal("a refused request cleared the Policy Acceptance it named")
	}
}

// TestPrivacySurfaceRefusesAnAbsentAnswer: the field is a pointer and its
// absence is refused, because a bool's zero value is the destructive half of
// this control and a client that forgot the field must not withdraw somebody's
// consent by omission.
func TestPrivacySurfaceRefusesAnAbsentAnswer(t *testing.T) {
	env := setupTest(t)

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	resp, body := env.put(t, consentPurposePath("marketing"), map[string]any{}, authHeader(token))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("absent answer status=%d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("absent answer error=%+v, want VALIDATION_FAILED", body.Error)
	}
	if state := readConsentState(t, env, "ana@example.com"); state.MarketingConsent.String != "granted" {
		t.Fatalf("marketing_consent = %q after a request with no answer in it, want granted", state.MarketingConsent.String)
	}
}

// TestPrivacySurfaceIsClosedToVisitorsWithoutASession: everything on this page
// is a fact about one identified person, and neither half is reachable without
// one.
func TestPrivacySurfaceIsClosedToVisitorsWithoutASession(t *testing.T) {
	env := setupTest(t)

	resp, _ := env.get(t, customerPrivacyPath, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous read status=%d, want 401", resp.StatusCode)
	}
	resp, _ = env.put(t, consentPurposePath("marketing"), map[string]bool{"granted": false}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous write status=%d, want 401", resp.StatusCode)
	}
}
