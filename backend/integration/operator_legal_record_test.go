package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ONE PERSON'S CONSENT RECORD (#566, spec #556, ADR 0067): the screen an
// operator answering a data subject's request works from, and the only place
// the Consent Withdrawal now lives.
//
// WHAT THIS SLICE MUST PROVE, beyond "the endpoints return rows":
//
//   - THE RECORD IS THE EVIDENCE, not a summary of it. Every act is listed with
//     what it answered, what it replaced, on which surface, and against which
//     EXACT EDITION — and a NULL comes back as NULL, so the screen can spell it
//     in words instead of rendering a blank that reads as a loss.
//   - `presented_locale` IS THREE-STATE (#567): absent on the channels that
//     present no document, null where one was shown and no language was
//     recorded, and a locale otherwise. The absence is the load-bearing case —
//     it is the difference between "nothing was shown" and "text was shown and
//     the platform forgot which language".
//   - PAGING IS KEYSET AT 25, and the VISIBLE COUNT is reported, so a truncated
//     page is distinguishable from a complete history. The clock is advanced
//     between captures, because the harness's is fixed and multi-row assertions
//     over one tick are a coin toss.
//   - TWO SCREENS, CROSS-LINKED SERVER-SIDE. A Customer by UUID and a staff
//     person by digest, each naming the other where one human is both — and
//     never merged.
//   - EXACTLY TWO ACTS ARE AVAILABLE, and the absences are as ruled as the
//     presences: there is no route that manufactures an acceptance, re-gates
//     one person, erases anybody, or withdraws the Terms or a Policy
//     Acceptance.
//   - NO EMAIL ADDRESS APPEARS IN ANY REQUEST LINE, and the two routes that
//     used to carry one are gone.

// legalCustomerRecordPath and its two siblings. Everything is keyed on the
// OPAQUE UUID; nothing here is an address.
func legalCustomerRecordPath(customerID string) string {
	return "/api/v1/operator/legal/customers/" + url.PathEscape(customerID)
}

func legalCustomerRecordsPath(customerID string) string {
	return legalCustomerRecordPath(customerID) + "/records"
}

func legalStaffRecordPath(digest string) string {
	return "/api/v1/operator/legal/staff/" + url.PathEscape(digest)
}

// legalEditionRefView names an edition: the id an Evidence Pack (#568) resolves
// to bytes, and the label a human reads. Both, never one.
type legalEditionRefView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type legalGateStandingView struct {
	AcceptedAt *string              `json:"accepted_at"`
	Edition    *legalEditionRefView `json:"edition"`
	Standing   string               `json:"standing"`
}

type legalCustomerSubjectView struct {
	ID                string                `json:"id"`
	Email             string                `json:"email"`
	FirstName         string                `json:"first_name"`
	LastName          string                `json:"last_name"`
	Policy            legalGateStandingView `json:"policy"`
	Terms             legalGateStandingView `json:"terms"`
	MarketingConsent  *string               `json:"marketing_consent"`
	NetworkingConsent *string               `json:"networking_consent"`
}

type legalCustomerRecordView struct {
	Customer    legalCustomerSubjectView `json:"customer"`
	StaffDigest *string                  `json:"staff_digest"`
}

// consentActView is one act. `PresentedLocale` is decoded as json.RawMessage on
// purpose: the three states are ABSENT, `null` and a string, and any decode
// that flattened the first two would destroy exactly the distinction the field
// exists to make.
type consentActView struct {
	ID                     string               `json:"id"`
	CapturedAt             string               `json:"captured_at"`
	Channel                string               `json:"channel"`
	Email                  string               `json:"email"`
	PolicyEdition          legalEditionRefView  `json:"policy_edition"`
	PolicyAcceptance       *bool                `json:"policy_acceptance"`
	MarketingConsent       *bool                `json:"marketing_consent"`
	NetworkingConsent      *bool                `json:"networking_consent"`
	PriorMarketingConsent  *string              `json:"prior_marketing_consent"`
	PriorNetworkingConsent *string              `json:"prior_networking_consent"`
	TermsAcceptance        *bool                `json:"terms_acceptance"`
	TermsEdition           *legalEditionRefView `json:"terms_edition"`
	// AdulthoodDeclaration is decoded as json.RawMessage for PresentedLocale's
	// reason, over a different distinction (#590): what must be assertable here
	// is `true` against `null` against ABSENT, and a *bool would make the last
	// two indistinguishable. The whole ruling is that a null is "never asked"
	// and not a No, so a test that could not see the difference between a null
	// and a missing key would be asserting nothing.
	AdulthoodDeclaration json.RawMessage `json:"adulthood_declaration"`
	EmailProven          bool            `json:"email_proven"`
	IP                   *string         `json:"ip"`
	RecordedBy           *string         `json:"recorded_by"`
	RequestReference     *string         `json:"request_reference"`
	PresentedLocale      json.RawMessage `json:"presented_locale"`
}

type consentActPageView struct {
	Acts         []consentActView `json:"acts"`
	VisibleCount int              `json:"visible_count"`
	NextCursor   *string          `json:"next_cursor"`
}

type staffAcceptanceRecordView struct {
	ID                   string              `json:"id"`
	TermsEdition         legalEditionRefView `json:"terms_edition"`
	Capacity             string              `json:"capacity"`
	AcceptedAt           string              `json:"accepted_at"`
	IP                   *string             `json:"ip"`
	PresentedLocale      *string             `json:"presented_locale"`
	AdulthoodDeclaration json.RawMessage     `json:"adulthood_declaration"`
}

type staffLegalRecordView struct {
	Digest       string                      `json:"digest"`
	Email        string                      `json:"email"`
	Standing     string                      `json:"standing"`
	Acceptances  []staffAcceptanceRecordView `json:"acceptances"`
	VisibleCount int                         `json:"visible_count"`
	CustomerID   *string                     `json:"customer_id"`
}

func readCustomerLegalRecord(t *testing.T, env *testEnv, sessionID, customerID string) legalCustomerRecordView {
	t.Helper()
	var view legalCustomerRecordView
	operatorGetOK(t, env, sessionID, legalCustomerRecordPath(customerID), &view)
	return view
}

// readConsentActs fetches one page. `query` is the raw query string, so a test
// can send a cursor, a limit, or a deliberately unparseable one.
func readConsentActs(t *testing.T, env *testEnv, sessionID, customerID, query string) consentActPageView {
	t.Helper()
	path := legalCustomerRecordsPath(customerID)
	if query != "" {
		path += "?" + query
	}
	var page consentActPageView
	operatorGetOK(t, env, sessionID, path, &page)
	return page
}

func readStaffLegalRecord(t *testing.T, env *testEnv, sessionID, digest string) staffLegalRecordView {
	t.Helper()
	var view staffLegalRecordView
	operatorGetOK(t, env, sessionID, legalStaffRecordPath(digest), &view)
	return view
}

// setConsentClock moves the clock that stamps a Consent Record.
//
// THE HARNESS CLOCK IS FIXED, so two captures made without this land on the
// same tick and a keyset walk over them is a coin toss — the exact hazard the
// (captured_at, id) cursor exists to survive, and one this file must not itself
// trip over while proving it.
func setConsentClock(at time.Time) {
	sharedApp.ConsentService.WithClock(func() time.Time { return at })
}

// seedConsentAct appends one Consent Record straight into the log.
//
// Direct SQL, for seedCustomer's reason: what is under test is the READ, and
// driving thirty acts through the capture path would take a minute and prove
// nothing about the record. The channel and the locale are the two things these
// tests vary, so they are parameters and everything else is a plausible
// constant.
func seedConsentAct(t *testing.T, env *testEnv, customerID, email, channel, locale string, at time.Time) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO consent_records (customer_id, email, channel, captured_at, policy_version_id,
		                             marketing_consent, email_proven, presented_locale)
		SELECT $1, $2, $3, $4,
		       (SELECT id FROM policy_versions
		         WHERE effective_date <= CURRENT_DATE
		         ORDER BY effective_date DESC, created_at DESC LIMIT 1),
		       TRUE, TRUE, NULLIF($5, '')
	`, customerID, email, channel, at, locale); err != nil {
		t.Fatalf("seed consent act on %q: %v", channel, err)
	}
}

// TestCustomerRecordIsTheEvidenceAndNamesTheExactEdition is the landing read
// and the history together: who the person is, where they stand against both
// gates, and every act that got them there.
func TestCustomerRecordIsTheEvidenceAndNamesTheExactEdition(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	customerID := customerIDFor(t, env, "ana@example.com")
	sessionID := operatorSession(t, env, "operator@example.com")

	record := readCustomerLegalRecord(t, env, sessionID, customerID)
	if record.Customer.ID != customerID || record.Customer.Email != "ana@example.com" {
		t.Fatalf("record identified %+v; want the Customer at that id", record.Customer)
	}
	// EACH GATE NAMES THE EXACT EDITION, id AND label, so it resolves to bytes
	// and reads as something a human can say out loud.
	if record.Customer.Policy.Edition == nil || record.Customer.Policy.Edition.ID == "" ||
		record.Customer.Policy.Edition.Label == "" {
		t.Fatalf("policy gate names no edition: %+v", record.Customer.Policy)
	}
	if record.Customer.Policy.Standing != "current" {
		t.Fatalf("policy standing = %q; want current for somebody who just accepted", record.Customer.Policy.Standing)
	}
	if record.Customer.Policy.AcceptedAt == nil {
		t.Fatal("policy accepted_at is null for somebody who just accepted")
	}
	// NULL IS UNANSWERED AND NOT DENIED — but this person answered, so both are
	// words. The unanswered case has its own test below.
	if record.Customer.MarketingConsent == nil || *record.Customer.MarketingConsent != "granted" {
		t.Fatalf("marketing consent = %v; want granted", record.Customer.MarketingConsent)
	}
	if record.Customer.NetworkingConsent == nil || *record.Customer.NetworkingConsent != "denied" {
		t.Fatalf("networking consent = %v; want denied", record.Customer.NetworkingConsent)
	}
	// The cross-link is absent: this person is not on the Staff platform, and a
	// link that existed anyway would assert that they are.
	if record.StaffDigest != nil {
		t.Fatalf("staff_digest = %q for somebody who is not staff", *record.StaffDigest)
	}

	// THE HISTORY IS ITS OWN ENDPOINT, and it carries the act the sign-in made.
	page := readConsentActs(t, env, sessionID, customerID, "")
	if page.VisibleCount != 1 || len(page.Acts) != 1 {
		t.Fatalf("history = %d acts (visible_count %d); want the sign-in's one", len(page.Acts), page.VisibleCount)
	}
	// A COMPLETE HISTORY, not a truncated page: no cursor.
	if page.NextCursor != nil {
		t.Fatalf("a one-act history named a next cursor: %v", *page.NextCursor)
	}
	act := page.Acts[0]
	if act.Channel != "signin" {
		t.Fatalf("act channel = %q; want signin", act.Channel)
	}
	if act.PolicyEdition.ID != record.Customer.Policy.Edition.ID || act.PolicyEdition.Label == "" {
		t.Fatalf("the act names edition %+v; want the one the person accepted", act.PolicyEdition)
	}
	if act.MarketingConsent == nil || !*act.MarketingConsent {
		t.Fatalf("the act's marketing answer = %v; want the tick that was made", act.MarketingConsent)
	}
	// NULL COMES BACK AS NULL. Networking was answered No here, so the null
	// under test is the RECORDED_BY pair: nobody recorded this act on anybody's
	// behalf, and a blank string would read as somebody having done so.
	if act.RecordedBy != nil || act.RequestReference != nil {
		t.Fatalf("a self-service act carries an attribution: recorded_by=%v reference=%v",
			act.RecordedBy, act.RequestReference)
	}

	// READING WROTE NOTHING. A record read that captured something would put an
	// act in the evidence log that nobody performed.
	if page := readConsentActs(t, env, sessionID, customerID, ""); page.VisibleCount != 1 {
		t.Fatalf("reading the record wrote to the evidence log: %d acts now", page.VisibleCount)
	}
}

// TestCustomerRecordReportsAnUnansweredConsentAsUnanswered: null is not denied,
// and an operator deciding what a withdrawal would change must never be shown a
// refusal the person did not make.
func TestCustomerRecordReportsAnUnansweredConsentAsUnanswered(t *testing.T) {
	env := setupTest(t)
	// A Customer created by a box office sale has never been asked anything.
	seedCustomerWithoutConsent(t, env, "unasked@example.com")
	customerID := customerIDFor(t, env, "unasked@example.com")
	sessionID := operatorSession(t, env, "operator@example.com")

	record := readCustomerLegalRecord(t, env, sessionID, customerID)
	if record.Customer.MarketingConsent != nil || record.Customer.NetworkingConsent != nil {
		t.Fatalf("consents = %+v; want both unanswered (null)", record.Customer)
	}
	if record.Customer.Policy.AcceptedAt != nil || record.Customer.Policy.Edition != nil {
		t.Fatalf("policy gate = %+v; want nothing accepted", record.Customer.Policy)
	}
	if record.Customer.Policy.Standing != "never_seen" || record.Customer.Terms.Standing != "never_seen" {
		t.Fatalf("standings = %q/%q; want never_seen on both",
			record.Customer.Policy.Standing, record.Customer.Terms.Standing)
	}

	// A person with no acts is an EMPTY PAGE, not a 404: somebody created
	// before the evidence log existed has a record and no history, and telling
	// an operator they do not exist would be false.
	page := readConsentActs(t, env, sessionID, customerID, "")
	if page.VisibleCount != 0 || len(page.Acts) != 0 || page.NextCursor != nil {
		t.Fatalf("history of a never-asked Customer = %+v; want an empty page", page)
	}
}

// TestCustomerRecordOfSomebodyWhoDoesNotExistIs404: an operator following a
// stale link is told plainly rather than shown a blank record they might then
// act on — and BOTH endpoints answer the same way, so it does not matter which
// the screen fires first.
func TestCustomerRecordOfSomebodyWhoDoesNotExistIs404(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	const nobody = "8f1c0c66-0000-4000-8000-000000000000"

	for _, path := range []string{legalCustomerRecordPath(nobody), legalCustomerRecordsPath(nobody)} {
		resp, body := env.get(t, path, authHeader(sessionID))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status=%d, want 404; error=%+v", path, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "LEGAL_SUBJECT_NOT_FOUND" || !isNullData(body.Data) {
			t.Fatalf("GET %s envelope: data=%s error=%+v", path, body.Data, body.Error)
		}
	}
}

// TestConsentRecordPresentedLocaleIsThreeStated is #567's rule as the record
// renders it, and the case that fails against the obvious implementation.
//
// A channel that presents NO DOCUMENT omits the key entirely: rendering it
// there — even as null — would claim that text was displayed and its language
// forgotten. A channel that DID present one and has no locale recorded reports
// null, which the screen spells "not recorded", because that is every row
// written before migration 115 and the answer is not recoverable from anything
// stored.
func TestConsentRecordPresentedLocaleIsThreeStated(t *testing.T) {
	env := setupTest(t)
	seedCustomerWithoutConsent(t, env, "ana@example.com")
	customerID := customerIDFor(t, env, "ana@example.com")
	sessionID := operatorSession(t, env, "operator@example.com")

	base := fixedClock
	// A sign-in, which renders the Short Notice, in Spanish.
	seedConsentAct(t, env, customerID, "ana@example.com", "signin", "es", base)
	// A sign-in whose locale nobody recorded: a row from before migration 115.
	seedConsentAct(t, env, customerID, "ana@example.com", "signin", "", base.Add(time.Minute))
	// A Customer Area toggle, which shows no notice at all.
	seedConsentAct(t, env, customerID, "ana@example.com", "account_settings", "", base.Add(2*time.Minute))

	page := readConsentActs(t, env, sessionID, customerID, "")
	if page.VisibleCount != 3 {
		t.Fatalf("history = %d acts; want 3", page.VisibleCount)
	}
	// Newest first: the toggle, then the unrecorded sign-in, then the Spanish one.
	toggle, unrecorded, spanish := page.Acts[0], page.Acts[1], page.Acts[2]

	if toggle.Channel != "account_settings" || unrecorded.Channel != "signin" || spanish.Channel != "signin" {
		t.Fatalf("history is not newest-first: %q %q %q", toggle.Channel, unrecorded.Channel, spanish.Channel)
	}
	// ABSENT. json.RawMessage is nil when the key was not in the object at all
	// — which is the whole point of decoding it this way.
	if toggle.PresentedLocale != nil {
		t.Fatalf("a channel that presents no document rendered presented_locale=%s", toggle.PresentedLocale)
	}
	// NULL: text was shown, and which language is not recorded.
	if string(unrecorded.PresentedLocale) != "null" {
		t.Fatalf("presented_locale on a document-showing act with no locale = %s; want null", unrecorded.PresentedLocale)
	}
	// A LOCALE.
	if string(spanish.PresentedLocale) != `"es"` {
		t.Fatalf("presented_locale = %s; want \"es\"", spanish.PresentedLocale)
	}
}

// TestConsentHistoryIsKeysetPagedAndReportsTheVisibleCount walks a history
// longer than one page and proves the two properties a record presented as
// evidence lives or dies on: nothing is skipped or repeated, and a truncated
// page is DISTINGUISHABLE from a complete one.
func TestConsentHistoryIsKeysetPagedAndReportsTheVisibleCount(t *testing.T) {
	env := setupTest(t)
	seedCustomerWithoutConsent(t, env, "ana@example.com")
	customerID := customerIDFor(t, env, "ana@example.com")
	sessionID := operatorSession(t, env, "operator@example.com")

	// Thirty acts, each on its own tick. THE CLOCK IS ADVANCED BETWEEN THEM:
	// the harness's is fixed, and thirty rows on one tick would make this walk
	// a coin toss for reasons that have nothing to do with the code under test.
	const total = 30
	for i := range total {
		seedConsentAct(t, env, customerID, "ana@example.com", "signin", "es",
			fixedClock.Add(time.Duration(i)*time.Minute))
	}

	first := readConsentActs(t, env, sessionID, customerID, "")
	// PAGE SIZE 25, and the count says so — read with the cursor, "25 acts and
	// there is more" is a different sentence from "25 acts and that is all".
	if first.VisibleCount != 25 || len(first.Acts) != 25 {
		t.Fatalf("first page = %d acts (visible_count %d); want 25", len(first.Acts), first.VisibleCount)
	}
	if first.NextCursor == nil {
		t.Fatal("a truncated page named no cursor, so it is indistinguishable from a complete history")
	}

	second := readConsentActs(t, env, sessionID, customerID, "cursor="+url.QueryEscape(*first.NextCursor))
	if second.VisibleCount != total-25 {
		t.Fatalf("second page = %d acts; want %d", second.VisibleCount, total-25)
	}
	if second.NextCursor != nil {
		t.Fatalf("the last page named a cursor: %v", *second.NextCursor)
	}

	// NOTHING SKIPPED AND NOTHING REPEATED.
	seen := make(map[string]bool, total)
	for _, act := range append(append([]consentActView{}, first.Acts...), second.Acts...) {
		if seen[act.ID] {
			t.Fatalf("act %s appeared twice across the walk", act.ID)
		}
		seen[act.ID] = true
	}
	if len(seen) != total {
		t.Fatalf("the walk saw %d of %d acts", len(seen), total)
	}

	// A LIMIT MAY LOWER THE PAGE SIZE AND CAN NEVER RAISE IT. An evidence log
	// downloaded in one request would be an export by another name, and the
	// export is a deliberate, named act (#568) rather than a query parameter.
	if page := readConsentActs(t, env, sessionID, customerID, "limit=5"); page.VisibleCount != 5 {
		t.Fatalf("limit=5 returned %d acts", page.VisibleCount)
	}
	if page := readConsentActs(t, env, sessionID, customerID, "limit=1000"); page.VisibleCount != 25 {
		t.Fatalf("limit=1000 returned %d acts; want the page size", page.VisibleCount)
	}
	// AN UNPARSEABLE CURSOR SERVES THE FIRST PAGE, the browsers' rule: a bad
	// cursor is a stale bookmark, not an error worth showing over evidence.
	stale := readConsentActs(t, env, sessionID, customerID, "cursor=not-a-cursor")
	if stale.VisibleCount != 25 || stale.Acts[0].ID != first.Acts[0].ID {
		t.Fatalf("an unparseable cursor did not serve the first page: %+v", stale.Acts[0])
	}
}

// TestWithdrawalFromTheRecordIsTheOnlyActAndIsAttributedToTheSession is the
// available act, exercised where it now lives — and the proof that
// `recorded_by` still comes from the Staff Session and never from the body.
func TestWithdrawalFromTheRecordIsTheOnlyActAndIsAttributedToTheSession(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, true)
	customerID := customerIDFor(t, env, "ana@example.com")
	sessionID := operatorSession(t, env, "operator@example.com")

	// The clock moves so the withdrawal's act cannot land on the sign-in's
	// tick: two rows on one tick make the newest-first assertion below a coin
	// toss.
	setConsentClock(fixedClock.Add(time.Hour))

	resp, body := env.post(t, operatorConsentWithdrawalPath(customerID), map[string]any{
		"marketing_consent": false,
		"request_reference": paperFormRef,
		// A crafted attribution, which must be IGNORED rather than honoured.
		"recorded_by": "someone.else@example.com",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdrawal status=%d error=%+v", resp.StatusCode, body.Error)
	}

	page := readConsentActs(t, env, sessionID, customerID, "")
	act := page.Acts[0]
	if act.Channel != "operator_request" {
		t.Fatalf("newest act channel = %q; want the withdrawal", act.Channel)
	}
	if act.RecordedBy == nil || *act.RecordedBy != "operator@example.com" {
		t.Fatalf("recorded_by = %v; want the acting operator's SESSION email, never the body's", act.RecordedBy)
	}
	if act.RequestReference == nil || *act.RequestReference != paperFormRef {
		t.Fatalf("request_reference = %v; want the artefact the operator named", act.RequestReference)
	}
	// PRIOR STATE, which is the only way to read that this act took something
	// away: `denied` looks the same whether somebody gave something up or
	// refused twice.
	if act.PriorMarketingConsent == nil || *act.PriorMarketingConsent != "granted" {
		t.Fatalf("prior_marketing_consent = %v; want granted", act.PriorMarketingConsent)
	}
	// THE CHANNEL PRESENTS NO DOCUMENT, so the locale key is absent rather than
	// null: a form on paper showed nobody anything.
	if act.PresentedLocale != nil {
		t.Fatalf("an operator_request act claimed a presented locale: %s", act.PresentedLocale)
	}
	// The Terms and the Policy Acceptance are untouched: neither is
	// withdrawable, and this act showed neither box.
	record := readCustomerLegalRecord(t, env, sessionID, customerID)
	if record.Customer.Policy.AcceptedAt == nil || record.Customer.Terms.AcceptedAt == nil {
		t.Fatalf("the withdrawal cleared a gate: %+v", record.Customer)
	}
}

// TestTheRecordOffersNoOtherAct is the absences, and they are as ruled as the
// presences.
//
// It is written as "these routes do not exist" rather than as a comment,
// because a screen without a button is not a guarantee — the API is reachable
// with curl — and the next person to add a route here should have to delete a
// test to do it.
func TestTheRecordOffersNoOtherAct(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	customerID := customerIDFor(t, env, "ana@example.com")
	sessionID := operatorSession(t, env, "operator@example.com")
	base := legalCustomerRecordPath(customerID)

	// No acceptance may be manufactured, nobody re-gated one at a time, nobody
	// erased, and neither the Terms nor a Policy Acceptance withdrawn — a
	// contract's basis is performance rather than consent, and clearing a
	// Policy Acceptance would re-gate the person rather than free them.
	for _, absent := range []struct{ method, path string }{
		{http.MethodPost, base + "/acceptance"},
		{http.MethodPost, base + "/acceptances"},
		{http.MethodPost, base + "/re-gate"},
		{http.MethodPost, base + "/erasure"},
		{http.MethodDelete, base},
		{http.MethodPost, base + "/terms-withdrawal"},
		{http.MethodPost, base + "/policy-withdrawal"},
	} {
		if status := routeStatus(t, env, absent.method, absent.path, sessionID); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status=%d; that act must not exist", absent.method, absent.path, status)
		}
	}

	// And the withdrawal that DOES exist still cannot grant, wherever it is
	// addressed from. The refusal is the write path's, so it holds for this
	// route exactly as it held for the one it replaced.
	resp, body := env.post(t, operatorConsentWithdrawalPath(customerID), map[string]any{
		"marketing_consent": true,
		"request_reference": paperFormRef,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "CONSENT_GRANT_NOT_PERMITTED" {
		t.Fatalf("a crafted grant status=%d error=%+v; want 400 CONSENT_GRANT_NOT_PERMITTED", resp.StatusCode, body.Error)
	}
}

// TestTheOldEmailKeyedConsentRoutesAreGone. No surface keeps putting an address
// in a request line: reading about somebody must not leak them into an access
// log, a proxy log, a browser history or the Referer header of the next click.
func TestTheOldEmailKeyedConsentRoutesAreGone(t *testing.T) {
	env := setupTest(t)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	sessionID := operatorSession(t, env, "operator@example.com")

	const legacy = "/api/v1/operator/customers/ana%40example.com/consent"
	if status := routeStatus(t, env, http.MethodGet, legacy, sessionID); status != http.StatusNotFound {
		t.Fatalf("the deleted lookup answered %d", status)
	}
	if status := routeStatus(t, env, http.MethodPost, legacy+"/withdrawal", sessionID); status != http.StatusNotFound {
		t.Fatalf("the deleted withdrawal answered %d", status)
	}
	// Nothing was recorded by either attempt.
	if records := consentRecordsOn(t, env, "ana@example.com", "operator_request"); len(records) != 0 {
		t.Fatalf("a deleted route wrote %d records", len(records))
	}
}

// TestStaffRecordIsReachedByDigestAndListsEveryAcceptance is the staff half:
// a person with no id, named by 32 hex characters, with the complete unpaged
// history the browser links to.
func TestStaffRecordIsReachedByDigestAndListsEveryAcceptance(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "zz-operator@example.com")
	orgID := seedOrganization(t, env, "record-org")

	first := currentEditionID(t, env, "terms_versions")
	seedMember(t, env, orgID, "ana@example.com")
	seedStaffAcceptance(t, env, "ana@example.com", first)
	second := publishTermsVersion(t, env, 2, 0)
	seedStaffAcceptance(t, env, "ana@example.com", second)

	// The digest is minted by the BROWSER, which is how an operator reaches
	// this screen — never typed, and never derived here from an address.
	digest := staffDigestFromBrowser(t, env, sessionID, "ana@example.com")

	record := readStaffLegalRecord(t, env, sessionID, digest)
	if record.Email != "ana@example.com" || record.Digest != digest {
		t.Fatalf("staff record = %+v; want the person the digest names", record)
	}
	// UNPAGED, so the count is the WHOLE count and there is no truncation to
	// distinguish: a person holds at most one acceptance per edition.
	if record.VisibleCount != 2 || len(record.Acceptances) != 2 {
		t.Fatalf("acceptances = %d (visible_count %d); want both", len(record.Acceptances), record.VisibleCount)
	}
	// EACH ACT NAMES THE EXACT EDITION, and the newest is first.
	if record.Acceptances[0].TermsEdition.ID != second || record.Acceptances[0].TermsEdition.Label != "2" {
		t.Fatalf("newest acceptance names %+v; want edition 2", record.Acceptances[0].TermsEdition)
	}
	if record.Acceptances[1].TermsEdition.ID != first {
		t.Fatalf("older acceptance names %+v; want the superseded edition", record.Acceptances[1].TermsEdition)
	}
	if record.Acceptances[0].Capacity != "organizer" {
		t.Fatalf("capacity = %q; it is carried rather than assumed", record.Acceptances[0].Capacity)
	}
	// Membership of the satisfying set, so the person who accepted the newest
	// edition is current.
	if record.Standing != "current" {
		t.Fatalf("standing = %q; want current", record.Standing)
	}
	// NULL IS NULL: nothing was collected for these seeded rows, and a blank
	// string would read as something collected and empty.
	if record.Acceptances[0].IP != nil || record.Acceptances[0].PresentedLocale != nil {
		t.Fatalf("a seeded acceptance invented technical proof: %+v", record.Acceptances[0])
	}
	// Not a Customer, so no cross-link.
	if record.CustomerID != nil {
		t.Fatalf("customer_id = %q for somebody who has never bought a ticket", *record.CustomerID)
	}
}

// TestTheTwoRecordsAreCrossLinkedAndNeverMerged is the ruling that most needs a
// test: one human being who is both a Customer and staff has TWO records, each
// naming the other, resolved SERVER-SIDE — and no third record that claims they
// are one identity the platform can evidence.
func TestTheTwoRecordsAreCrossLinkedAndNeverMerged(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "zz-operator@example.com")
	orgID := seedOrganization(t, env, "crosslink-org")

	// The same address on both sides of the platform.
	signInAnswering(t, env, "ana@example.com", true, true, false)
	seedMember(t, env, orgID, "ana@example.com")
	seedStaffAcceptance(t, env, "ana@example.com", currentEditionID(t, env, "terms_versions"))

	customerID := customerIDFor(t, env, "ana@example.com")
	customer := readCustomerLegalRecord(t, env, sessionID, customerID)
	if customer.StaffDigest == nil {
		t.Fatal("the Customer record offers no link to the staff record of the same human being")
	}

	// The link RESOLVES, and lands on the same person — which is the whole
	// claim the cross-link makes.
	staff := readStaffLegalRecord(t, env, sessionID, *customer.StaffDigest)
	if staff.Email != "ana@example.com" {
		t.Fatalf("the cross-link landed on %q", staff.Email)
	}
	// And back the other way.
	if staff.CustomerID == nil || *staff.CustomerID != customerID {
		t.Fatalf("staff record's customer_id = %v; want %q", staff.CustomerID, customerID)
	}

	// NEVER MERGED: the two records are two payloads with two keys, and the
	// staff one carries no consent state at all. A Customer's optional consents
	// are not facts about a staff person, and a merged record would assert an
	// identity no row anywhere establishes.
	if staff.VisibleCount != 1 {
		t.Fatalf("staff record = %+v; want exactly the one acceptance", staff)
	}
}

// TestStaffRecordForADigestThatNamesNobodyIs404. The digest is one-way by
// design — there is no reverse — so it is matched across the population until
// somebody answers, and a digest under a rotated key or a mistyped one matches
// none of them.
func TestStaffRecordForADigestThatNamesNobodyIs404(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	resp, body := env.get(t, legalStaffRecordPath("00000000000000000000000000000000"), authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown digest status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "STAFF_SUBJECT_NOT_FOUND" || !isNullData(body.Data) {
		t.Fatalf("unknown digest envelope: data=%s error=%+v", body.Data, body.Error)
	}
}

// TestTheRecordScreensAreOperatorsOnly is ADR 0010 at the door, on the new
// routes. Customer identity is global and separate from staff, so no
// Organization-scoped role may read anybody's consent record — and the refusal
// is identical for a person who exists and an id that names nobody.
func TestTheRecordScreensAreOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	customerID := customerIDFor(t, env, "ana@example.com")

	for _, path := range []string{
		legalCustomerRecordPath(customerID),
		legalCustomerRecordsPath(customerID),
		legalCustomerRecordPath("8f1c0c66-0000-4000-8000-000000000000"),
		legalStaffRecordPath("00000000000000000000000000000000"),
	} {
		resp, body := env.get(t, path, nil)
		if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("unauthenticated GET %s status=%d error=%+v; want 401", path, resp.StatusCode, body.Error)
		}
		resp, body = env.get(t, path, authHeader(adminSessionID))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("org_admin GET %s status=%d error=%+v; want 403", path, resp.StatusCode, body.Error)
		}
	}
}

// THE ADULTHOOD DECLARATION ON THE PER-SUBJECT RECORD (#590, parent #584, ADR
// 0069).
//
// The payoff of the whole feature: "show me that this individual affirmed they
// were an adult" stops being an inference from a document's contents and
// becomes a record. What this slice must prove is the answer AND its absence —
// an act captured under an edition that carried no 18+ Artifact reads "never
// asked", in both populations, and never reads as a No, because a No would be a
// stored claim that a named individual is a child and no such row is ever
// written.

// seedTermsActWithoutDeclaration appends a Consent Record that ACCEPTED THE
// TERMS and declared nothing: the shape of every act captured before an
// operator published the Artifact.
//
// Direct SQL, for seedConsentAct's reason — what is under test is the read —
// and it is a separate helper because this is the interesting null. An act that
// showed no Terms box at all is trivially null; an act that showed one, under
// an edition with no adulthood label, is the case a reader is most likely to
// mistake for a refusal.
func seedTermsActWithoutDeclaration(t *testing.T, env *testEnv, customerID, email, termsEditionID string, at time.Time) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO consent_records (customer_id, email, channel, captured_at, policy_version_id,
		                             policy_acceptance, terms_acceptance, terms_version_id,
		                             email_proven, presented_locale)
		SELECT $1, $2, 'signin', $3,
		       (SELECT id FROM policy_versions
		         WHERE effective_date <= CURRENT_DATE
		         ORDER BY effective_date DESC, created_at DESC LIMIT 1),
		       TRUE, TRUE, $4::uuid, TRUE, 'es'
	`, customerID, email, at, termsEditionID); err != nil {
		t.Fatalf("seed a terms act with no declaration: %v", err)
	}
}

// seedStaffAcceptanceDeclaring is seedStaffAcceptance with the 18+ box ticked:
// an acceptance made under an edition that carried the Artifact.
func seedStaffAcceptanceDeclaring(t *testing.T, env *testEnv, email, editionID string) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO staff_terms_acceptances (email, terms_version_id, capacity, accepted_at,
		                                     adulthood_declaration)
		VALUES ($1, $2, 'organizer', NOW(), TRUE)
		ON CONFLICT (email, terms_version_id, capacity) DO NOTHING
	`, email, editionID); err != nil {
		t.Fatalf("seed a declaring staff acceptance for %q: %v", email, err)
	}
}

// TestTheCustomerRecordShowsTheDeclarationPerActAndSpellsANullNeverAsked is the
// attendee half: one person, two acts, two different answers to the same
// question — and neither of them a No.
func TestTheCustomerRecordShowsTheDeclarationPerActAndSpellsANullNeverAsked(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	// The world before the publish: an edition with no adulthood Artifact, and
	// an acceptance captured under it.
	oldEdition := currentEditionID(t, env, "terms_versions")
	seedCustomerWithoutConsent(t, env, "ana@example.com")
	customerID := customerIDFor(t, env, "ana@example.com")
	seedTermsActWithoutDeclaration(t, env, customerID, "ana@example.com", oldEdition, fixedClock)

	// And then the publish, and a real capture through the sign-in gate: the
	// box is drawn because the edition in effect carries the Artifact, and the
	// tick is recorded beside the acceptance it travelled with.
	publishTermsVersionAskingAdulthood(t, env, 2, 0)
	// The clock moves so the two acts cannot share a tick — the harness's is
	// fixed, and a newest-first assertion over one tick is a coin toss.
	setConsentClock(fixedClock.Add(time.Hour))
	signInAnswering(t, env, "ana@example.com", true, true, false)

	page := readConsentActs(t, env, sessionID, customerID, "")
	if page.VisibleCount != 2 {
		t.Fatalf("history = %d acts; want the seeded one and the sign-in", page.VisibleCount)
	}
	declared, neverAsked := page.Acts[0], page.Acts[1]

	// THE DECLARATION IS PER ACT. The newest act asked and was answered.
	if string(declared.AdulthoodDeclaration) != "true" {
		t.Fatalf("the declaring act carries adulthood_declaration=%s; want true", declared.AdulthoodDeclaration)
	}
	// AND IT NAMES NO EDITION OF ITS OWN: the words declared under are an
	// Artifact of the Terms edition the act already names.
	if declared.TermsEdition == nil || declared.TermsEdition.ID == "" {
		t.Fatalf("the declaring act names no Terms edition: %+v", declared)
	}

	// THE NULL, WHICH IS THE LOAD-BEARING CASE. The older act accepted the
	// Terms under an edition that carried no 18+ box, so the platform holds no
	// answer — and it says so as `null`, present in the payload rather than
	// omitted from it. An absent key would leave the reader to decide what its
	// absence meant, and the one wrong guess is "No".
	if string(neverAsked.AdulthoodDeclaration) != "null" {
		t.Fatalf("an act under a non-Artifact edition carries adulthood_declaration=%s; want null",
			neverAsked.AdulthoodDeclaration)
	}
	if neverAsked.TermsAcceptance == nil || !*neverAsked.TermsAcceptance {
		t.Fatalf("the seeded act did not accept the Terms: %+v", neverAsked)
	}
	// NOWHERE IN THE WHOLE HISTORY IS THERE A FALSE. A refusal is refused
	// before any capture and writes nothing, so no act can ever say one.
	for _, act := range page.Acts {
		if string(act.AdulthoodDeclaration) == "false" {
			t.Fatalf("act %s records a refused declaration; a refusal writes nothing", act.ID)
		}
	}
}

// TestTheStaffRecordShowsTheDeclarationPerAcceptance is the organizer half,
// over the same two states: an acceptance that declared, and one made under an
// edition that never asked.
func TestTheStaffRecordShowsTheDeclarationPerAcceptance(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "zz-operator@example.com")
	orgID := seedOrganization(t, env, "declaration-org")

	first := currentEditionID(t, env, "terms_versions")
	seedMember(t, env, orgID, "ana@example.com")
	seedStaffAcceptance(t, env, "ana@example.com", first)
	second := publishTermsVersionAskingAdulthood(t, env, 2, 0)
	seedStaffAcceptanceDeclaring(t, env, "ana@example.com", second)

	digest := staffDigestFromBrowser(t, env, sessionID, "ana@example.com")
	record := readStaffLegalRecord(t, env, sessionID, digest)
	if record.VisibleCount != 2 {
		t.Fatalf("acceptances = %d; want both", record.VisibleCount)
	}
	declared, neverAsked := record.Acceptances[0], record.Acceptances[1]
	if declared.TermsEdition.ID != second || neverAsked.TermsEdition.ID != first {
		t.Fatalf("the acceptances are not newest-first: %+v", record.Acceptances)
	}
	if string(declared.AdulthoodDeclaration) != "true" {
		t.Fatalf("the declaring acceptance carries %s; want true", declared.AdulthoodDeclaration)
	}
	// The same null, spelled the same way, on the population that has no
	// Consent Record at all: one rule, two evidence logs.
	if string(neverAsked.AdulthoodDeclaration) != "null" {
		t.Fatalf("an acceptance under a non-Artifact edition carries %s; want null",
			neverAsked.AdulthoodDeclaration)
	}
	for _, acceptance := range record.Acceptances {
		if string(acceptance.AdulthoodDeclaration) == "false" {
			t.Fatalf("acceptance %s records a refusal; an untick is refused before the insert", acceptance.ID)
		}
	}
}

// TestTheAcceptanceBrowsersGainNoAdulthoodColumnFilterOrStanding, asserted
// rather than merely omitted.
//
// An adulthood standing would be a near-copy of Terms standing, and those
// browsers are deliberately not a segmentation tool: a filterable roster of who
// has and has not declared is the closest thing to "a list of self-declared
// minors" this platform could accidentally build, and it is the one shape the
// feature must never take.
func TestTheAcceptanceBrowsersGainNoAdulthoodColumnFilterOrStanding(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "zz-operator@example.com")
	orgID := seedOrganization(t, env, "browser-org")

	edition := publishTermsVersionAskingAdulthood(t, env, 2, 0)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	seedMember(t, env, orgID, "ana@example.com")
	seedStaffAcceptanceDeclaring(t, env, "ana@example.com", edition)

	for _, browser := range []struct{ name, path string }{
		{"customers/policy", customerBrowserPolicyPath},
		{"customers/terms", customerBrowserTermsPath},
		{"staff/terms", staffBrowserTermsPath},
	} {
		// NO COLUMN. The rows are read as raw JSON rather than through a struct,
		// because a struct would silently ignore exactly the field being
		// asserted absent.
		resp, body := env.post(t, browser.path, map[string]any{}, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST %s status=%d error=%+v", browser.path, resp.StatusCode, body.Error)
		}
		plain := string(body.Data)
		if strings.Contains(plain, "adulthood") {
			t.Fatalf("%s carries the declaration: %s", browser.name, plain)
		}

		// NO FILTER, AND NO STANDING TO FILTER ON. A filter the API silently
		// ignored would be as bad as one it honoured: the operator would read a
		// segmented list that is not segmented. So the demand is that naming it
		// changes nothing — the page is the same page — and there is no
		// `adulthood` standing for it to name either.
		resp, filtered := env.post(t, browser.path, map[string]any{
			"adulthood_declaration": true,
			"standing":              "adulthood_declared",
		}, authHeader(sessionID))
		switch resp.StatusCode {
		case http.StatusOK:
			if string(filtered.Data) != plain {
				t.Fatalf("%s answered an adulthood filter with a different page", browser.name)
			}
		case http.StatusBadRequest:
			// An unknown standing refused outright is the other honest answer.
		default:
			t.Fatalf("%s answered an adulthood filter with %d: %+v",
				browser.name, resp.StatusCode, filtered.Error)
		}
	}
}

// TestReadingADeclarationLogsExactlyOneSubjectRead is the Consent Access Log's
// half of the ruling: an operator reading somebody's declaration is a
// `subject_read` OF THE RECORD THAT HOLDS IT, and one touch is one row.
//
// The log gains no act kind. There is no `adulthood_read`, and there must not
// be: an act kind per field is how an audit log stops being readable, and the
// declaration is not a resource — it is a column on an act that is already
// logged.
func TestReadingADeclarationLogsExactlyOneSubjectRead(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	publishTermsVersionAskingAdulthood(t, env, 2, 0)
	signInAnswering(t, env, "ana@example.com", true, true, false)
	customerID := customerIDFor(t, env, "ana@example.com")

	before := len(accessLogRowsOf(t, env, "subject_read"))
	record := readCustomerLegalRecord(t, env, sessionID, customerID)
	if record.Customer.ID != customerID {
		t.Fatalf("read the wrong record: %+v", record.Customer)
	}
	// EXACTLY ONE, and not one per fact disclosed. Opening a record that
	// happens to carry a declaration must not double-count the same touch.
	if got := len(accessLogRowsOf(t, env, "subject_read")) - before; got != 1 {
		t.Fatalf("opening a record carrying a declaration wrote %d subject_read rows; want exactly 1", got)
	}
	// AND NO NEW ACT KIND EXISTS. Asserted against the schema rather than
	// against a list somebody maintains: the vocabulary is a CHECK, and a
	// migration that widened it for this feature would fail here.
	if _, err := env.db.Exec(`
		INSERT INTO consent_access_log (act, actor_email, subject_email)
		VALUES ('adulthood_read', 'operator@example.com', 'ana@example.com')
	`); err == nil {
		t.Fatal("the Consent Access Log accepted an adulthood act kind")
	}
}

// TestNoPathCreatesEditsOrWithdrawsADeclaration. Like Terms Acceptance it has
// no withdrawal path — and unlike Terms Acceptance it has no creation path
// either that is not the gate itself.
//
// Written as "these routes do not exist" and as "these acts move nothing",
// because a screen without a button is not a guarantee: the API is reachable
// with curl, and the next person to add a route here should have to delete a
// test to do it.
func TestNoPathCreatesEditsOrWithdrawsADeclaration(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	publishTermsVersionAskingAdulthood(t, env, 2, 0)
	signInAnswering(t, env, "ana@example.com", true, true, true)
	customerID := customerIDFor(t, env, "ana@example.com")
	base := legalCustomerRecordPath(customerID)

	declarationOf := func(t *testing.T) string {
		t.Helper()
		page := readConsentActs(t, env, sessionID, customerID, "")
		for _, act := range page.Acts {
			if act.Channel == "signin" {
				return string(act.AdulthoodDeclaration)
			}
		}
		t.Fatal("the sign-in act is gone from the record")
		return ""
	}
	if got := declarationOf(t); got != "true" {
		t.Fatalf("the sign-in recorded adulthood_declaration=%s; want true", got)
	}

	// NO ENDPOINT MANUFACTURES, EDITS OR WITHDRAWS ONE.
	for _, absent := range []struct{ method, path string }{
		{http.MethodPost, base + "/adulthood"},
		{http.MethodPost, base + "/adulthood-declaration"},
		{http.MethodPut, base + "/adulthood-declaration"},
		{http.MethodPatch, base + "/adulthood-declaration"},
		{http.MethodDelete, base + "/adulthood-declaration"},
		{http.MethodPost, base + "/adulthood-withdrawal"},
	} {
		if status := routeStatus(t, env, absent.method, absent.path, sessionID); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status=%d; that act must not exist", absent.method, absent.path, status)
		}
	}

	// AND THE WITHDRAWAL THAT DOES EXIST LEAVES IT UNTOUCHED — including a
	// Withdraw All, which takes back every optional consent at once and is the
	// broadest act any surface can perform on somebody's record.
	setConsentClock(fixedClock.Add(time.Hour))
	resp, body := env.post(t, operatorConsentWithdrawalPath(customerID), map[string]any{
		"marketing_consent":  false,
		"networking_consent": false,
		"request_reference":  paperFormRef,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdrawal status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := declarationOf(t); got != "true" {
		t.Fatalf("a Withdraw All moved the declaration to %s", got)
	}
	// And the withdrawal's own act declares nothing, rather than recording a
	// No: an operator's form showed nobody an 18+ box.
	page := readConsentActs(t, env, sessionID, customerID, "")
	if string(page.Acts[0].AdulthoodDeclaration) != "null" {
		t.Fatalf("the withdrawal act carries adulthood_declaration=%s; want null",
			page.Acts[0].AdulthoodDeclaration)
	}
	// A crafted answer on the withdrawal body is ignored, never honoured: there
	// is no such field on that request and no path to one.
	setConsentClock(fixedClock.Add(2 * time.Hour))
	resp, body = env.post(t, operatorConsentWithdrawalPath(customerID), map[string]any{
		"marketing_consent":     false,
		"adulthood_declaration": false,
		"request_reference":     paperFormRef,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a crafted declaration status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := declarationOf(t); got != "true" {
		t.Fatalf("a crafted body moved the declaration to %s", got)
	}

	// NO LIST OF SELF-DECLARED MINORS EXISTS IN ANY FORM, because no row ever
	// says one — on the attendee log, the organizer log, or the checkout hold
	// between them.
	for _, table := range []struct{ name, column string }{
		{"consent_records", "adulthood_declaration"},
		{"staff_terms_acceptances", "adulthood_declaration"},
		{"payments", "consent_adulthood_declaration"},
	} {
		var refusals int
		if err := env.db.QueryRow(
			`SELECT COUNT(*) FROM ` + table.name + ` WHERE ` + table.column + ` IS FALSE`,
		).Scan(&refusals); err != nil {
			t.Fatalf("count refusals on %s: %v", table.name, err)
		}
		if refusals != 0 {
			t.Fatalf("%s holds %d rows declaring somebody a minor", table.name, refusals)
		}
	}
}

// TestTheDeclarationHasNoBearingOnTicketsOrTransactionalMail. A compliance
// control must not cost somebody what they bought.
//
// It lives beside the other absences rather than in the checkout's own file
// because it is the same kind of claim as the ones above it: the declaration is
// evidence and nothing else, so it must not appear in — or alter — anything the
// buyer receives. What the checkout file proves is that the box is owed,
// answered and recorded; what this proves is that having answered it changes
// nothing whatever about the sale it travelled with.
func TestTheDeclarationHasNoBearingOnTicketsOrTransactionalMail(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Declared Fest", "declared-fest", 1000, 10)

	token, _ := signedInOwingTheDeclaration(t, env, "dora@example.com", 2)
	setSignInClock(t, env, env.fixedClock.Add(time.Hour))

	body := consentCheckoutBody("Dora", "Vega", nil, nil, nil, cartLine(gaID, 1))
	body["terms_acceptance"] = true
	body["adulthood_declaration"] = true
	begin := beginCheckoutWithEvidenceOK(t, env, "test-org", "declared-fest", token, body)
	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirm.Status != "approved" {
		t.Fatalf("confirm status = %q; a declared checkout must complete like any other", confirm.Status)
	}

	// THE SALE IS AN ORDINARY SALE, and its payload says nothing about the
	// declaration. Read as raw JSON, because a struct would ignore exactly the
	// field being asserted absent.
	resp, envelope := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	if strings.Contains(string(envelope.Data), "adulthood") {
		t.Fatalf("the Sales list carries the declaration: %s", envelope.Data)
	}

	// AND THE SALE CONFIRMATION CARRIES NOTHING OF IT EITHER. The receipt is
	// about tickets and money; a line reporting what somebody declared about
	// their age would be a compliance control leaking into a buyer's inbox.
	confirmations := env.email.Confirmations()
	if len(confirmations) != 1 {
		t.Fatalf("sale confirmations = %d; want the one this sale earned", len(confirmations))
	}
	receipt := strings.ToLower(fmt.Sprintf("%+v", confirmations[0]))
	for _, forbidden := range []string{"adulthood", "mayor de edad", "eighteen"} {
		if strings.Contains(receipt, forbidden) {
			t.Fatalf("the Sale Confirmation mentions %q: %s", forbidden, receipt)
		}
	}
	if confirmations[0].Reference != confirm.ConfirmationRef {
		t.Fatalf("the receipt names %q; want the sale's own reference", confirmations[0].Reference)
	}
}

// routeStatus asks the server what it makes of a request line, without decoding
// an envelope.
//
// It exists because THE ABSENCE OF A ROUTE IS NOT AN ENVELOPE. A path the mux
// does not know answers with net/http's own plain-text 404, and the harness's
// helpers would fail decoding it — which would make "this act does not exist"
// unassertable, and that is the assertion this file most needs to be able to
// make.
func routeStatus(t *testing.T, env *testEnv, method, path, sessionID string) int {
	t.Helper()
	req, err := http.NewRequest(method, env.server.URL+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range authHeader(sessionID) {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// staffDigestFromBrowser mints a digest the only way an operator ever gets one:
// off the staff acceptance browser (#565).
//
// A TEST THAT COMPUTED THE DIGEST ITSELF would be asserting that two copies of
// the preimage rule agree, which is not the property that matters. What matters
// is that the link on the browser LANDS ON THE RIGHT PERSON'S RECORD, which is
// only proved by carrying the browser's own value across.
func staffDigestFromBrowser(t *testing.T, env *testEnv, sessionID, email string) string {
	t.Helper()
	for _, standing := range []string{"current", "outstanding", "never_seen", "former"} {
		page := browseStaff(t, env, sessionID, map[string]any{"standing": standing, "search_email": email})
		for _, row := range page.Rows {
			if row.Email == email {
				return row.Digest
			}
		}
	}
	t.Fatalf("%q appears on no page of the staff browser", email)
	return ""
}
