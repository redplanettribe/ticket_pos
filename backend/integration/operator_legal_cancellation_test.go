package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Cancelling a scheduled edition (#564, spec #556, ADR 0067): the night the
// overnight delay buys, made usable.
//
// A gating edition cannot take effect the day it is published, so between the
// click and the midnight rollover there is a night in which the operator can
// change their mind. Until this slice, that night bought a feeling rather than a
// chance: the row was written and nothing could unwrite it.
//
// THE ASSERTION THIS FILE EXISTS FOR is the last one: a cancelled edition
// RE-GATES NOBODY, at both gates, ON ITS OWN DATE. Everything else here — the
// banner, the retained row, the refusal after the date — is arranged around
// proving that the day arriving does not resurrect a withdrawn edition. The
// calendar cannot be advanced inside a test, so the row's date is moved back TO
// the calendar instead, which puts the database in exactly the state tomorrow
// morning would.

// legalScheduledView is one edition still waiting for its day, as the banner
// names it.
type legalScheduledView struct {
	VersionID     string `json:"version_id"`
	Label         string `json:"label"`
	EffectiveDate string `json:"effective_date"`
	Gating        bool   `json:"gating"`
}

// legalCancelWorkspaceView is the workspace as this slice reads it: what is
// published, what is waiting, and what the publish step would do. Its own struct
// rather than an addition to the other Legal Center slices' views, so the four
// files' assertions stay independent.
type legalCancelWorkspaceView struct {
	Published legalEditionView      `json:"published"`
	Draft     legalPublishDraftView `json:"draft"`
	Publish   legalPublishPlanView  `json:"publish"`
	Scheduled []legalScheduledView  `json:"scheduled"`
}

func legalCancelWorkspace(t *testing.T, env *testEnv, sessionID, path string) legalCancelWorkspaceView {
	t.Helper()
	var workspace legalCancelWorkspaceView
	operatorGetOK(t, env, sessionID, path, &workspace)
	return workspace
}

// scheduleGatingEdition publishes the document's current text again as a GATING
// edition, effective on a day the database has not reached.
//
// THE WHOLE PATH, THROUGH HTTP: save the draft, preview every cell, look at the
// diff, publish. Nothing here reaches around a rule — an empty diff is
// deliberately publishable as a gating edition (re-gating over unchanged text is
// a real act), which is what lets this helper schedule an edition without
// inventing legal text nobody wrote.
//
// The date is counted from the DATABASE's day and not from the app's fixed
// clock: what makes an edition "scheduled" is `effective_date > CURRENT_DATE`,
// answered by Postgres, and the harness's clock is months behind the calendar.
func scheduleGatingEdition(t *testing.T, env *testEnv, sessionID, path string, daysAhead int) (string, string) {
	t.Helper()
	fresh := legalPublishWorkspace(t, env, sessionID, path)
	saveLegalDraftAt(t, env, sessionID, path, draftBody([]string{"en", "es"}, fresh.Draft.Artifacts))
	reviewWholeDraft(t, env, sessionID, path)

	var today time.Time
	if err := env.db.QueryRowContext(t.Context(), `SELECT CURRENT_DATE`).Scan(&today); err != nil {
		t.Fatalf("read the database's day: %v", err)
	}
	effective := today.AddDate(0, 0, daysAhead).Format("2006-01-02")

	publishLegalOK(t, env, sessionID, path, map[string]any{
		"kind": "edition", "effective_date": effective,
	})

	// The edition is found through the BANNER rather than through a returned id,
	// which is the same road the operator's own cancel button travels: if it is
	// not in `scheduled`, there is nothing to cancel and the test should say so
	// here rather than three assertions later.
	published := legalCancelWorkspace(t, env, sessionID, path)
	if len(published.Scheduled) == 0 {
		t.Fatalf("nothing is scheduled after publishing effective %s: %+v", effective, published)
	}
	return published.Scheduled[0].VersionID, effective
}

// arriveAt moves an edition's day to today, which is the ONE thing a test cannot
// do by waiting.
//
// It writes `effective_date` directly, and that is the point: nothing in the
// product ever does, so this is not a shortcut around a rule but a simulation of
// the only actor that can move the boundary — the calendar. What the assertions
// after it check is that the edition's day arriving changes nothing, because the
// cancellation mark is consulted by the same query that consults the date.
func arriveAt(t *testing.T, env *testEnv, table, versionID string) {
	t.Helper()
	if _, err := env.db.ExecContext(t.Context(),
		`UPDATE `+table+` SET effective_date = CURRENT_DATE WHERE id::text = $1`, versionID); err != nil {
		t.Fatalf("bring %s forward to today: %v", versionID, err)
	}
}

func cancelLegalEdition(t *testing.T, env *testEnv, sessionID, path, versionID string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, path+"/publications/"+versionID+"/cancel", map[string]any{}, authHeader(sessionID))
}

func cancelLegalEditionOK(t *testing.T, env *testEnv, sessionID, path, versionID string) legalCancelWorkspaceView {
	t.Helper()
	resp, body := cancelLegalEdition(t, env, sessionID, path, versionID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var workspace legalCancelWorkspaceView
	if err := json.Unmarshal(body.Data, &workspace); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	return workspace
}

// TestOperatorLegalSchedulesAnEditionAndCancelsIt walks the night: publish for a
// day that has not come, find the banner, take it back, and find the row still
// there.
func TestOperatorLegalSchedulesAnEditionAndCancelsIt(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	before := legalCancelWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	if len(before.Scheduled) != 0 {
		t.Fatalf("something was already scheduled: %+v", before.Scheduled)
	}

	versionID, effective := scheduleGatingEdition(t, env, sessionID, operatorLegalPolicyPath, 3)

	// THE BANNER. It names the edition and the day it takes effect, so the
	// operator cannot forget that something is about to happen.
	scheduled := legalCancelWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	if len(scheduled.Scheduled) != 1 {
		t.Fatalf("scheduled = %+v; want the one edition waiting for its day", scheduled.Scheduled)
	}
	banner := scheduled.Scheduled[0]
	if banner.Label != "2" || banner.EffectiveDate != effective || !banner.Gating {
		t.Fatalf("banner = %+v; want the gating edition 2 effective %s", banner, effective)
	}

	// AND IT IS NOT CURRENT. The published edition the editor draws against is
	// still the one people are actually held to.
	if scheduled.Published.VersionID != before.Published.VersionID {
		t.Fatalf("a scheduled edition became current: %q -> %q",
			before.Published.VersionID, scheduled.Published.VersionID)
	}

	// THE CANCELLATION: no body, no reason, no confirmation token. The request
	// is the act.
	after := cancelLegalEditionOK(t, env, sessionID, operatorLegalPolicyPath, versionID)
	if len(after.Scheduled) != 0 {
		t.Fatalf("the withdrawn edition is still on the banner: %+v", after.Scheduled)
	}

	// THE ROW IS RETAINED AND MARKED, NEVER DELETED. Every word of what was
	// nearly published survives, its fingerprint with it, and the mark says who
	// took it back and when — whole, in migration 112's house style.
	var (
		label       string
		cancelledBy string
		cancelledAt time.Time
		artifacts   int
	)
	if err := env.db.QueryRowContext(t.Context(), `
		SELECT v.label, v.cancelled_by, v.cancelled_at,
		       (SELECT count(*) FROM policy_version_artifacts a WHERE a.version_id = v.id)
		FROM policy_versions v WHERE v.id::text = $1
	`, versionID).Scan(&label, &cancelledBy, &cancelledAt, &artifacts); err != nil {
		t.Fatalf("the withdrawn edition went missing: %v", err)
	}
	if label != "2" || cancelledBy != "operator@example.com" || cancelledAt.IsZero() {
		t.Fatalf("cancellation mark = %q / %q / %v", label, cancelledBy, cancelledAt)
	}
	if artifacts == 0 {
		t.Fatalf("the withdrawn edition kept no text: the record of what was nearly published must survive")
	}

	// ITS LABEL STAYS SPENT. The next gating publication takes 3, not 2 — a name
	// means one thing forever, which is the other half of retaining the row.
	if after.Publish.GatingLabel != "3" {
		t.Fatalf("next gating label = %q; want 3, because a withdrawn edition still spent 2", after.Publish.GatingLabel)
	}

	// CANCELLING TWICE IS A SUCCESS, and the FIRST cancellation's provenance
	// stands: a second click on a stale tab must not rewrite who withdrew it.
	cancelLegalEditionOK(t, env, sessionID, operatorLegalPolicyPath, versionID)
	var again time.Time
	if err := env.db.QueryRowContext(t.Context(),
		`SELECT cancelled_at FROM policy_versions WHERE id::text = $1`, versionID).Scan(&again); err != nil {
		t.Fatalf("re-read the mark: %v", err)
	}
	if !again.Equal(cancelledAt) {
		t.Fatalf("the second cancellation rewrote the mark: %v -> %v", cancelledAt, again)
	}
}

// TestOperatorLegalCancelDisappearsOnceTheDateHasPassed is the rule the
// interface expresses by hiding a button, asserted where it is actually
// enforced.
//
// The control's absence is not a second implementation: an edition whose day has
// come is simply not in `scheduled`, and the seam refuses the act with the same
// date predicate the rest of the feature turns on.
func TestOperatorLegalCancelDisappearsOnceTheDateHasPassed(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	versionID, _ := scheduleGatingEdition(t, env, sessionID, operatorLegalTermsPath, 2)

	// Morning. The edition's day is now today.
	arriveAt(t, env, "terms_versions", versionID)

	arrived := legalCancelWorkspace(t, env, sessionID, operatorLegalTermsPath)
	if len(arrived.Scheduled) != 0 {
		t.Fatalf("an edition in force is still offered for cancellation: %+v", arrived.Scheduled)
	}
	// It became current on its own, with nothing fired — the property #563 and
	// #558 both rest on, restated here because a cancellation must not have
	// broken it.
	if arrived.Published.VersionID != versionID {
		t.Fatalf("published = %q; want the scheduled edition %q, which took effect by itself",
			arrived.Published.VersionID, versionID)
	}

	resp, body := cancelLegalEdition(t, env, sessionID, operatorLegalTermsPath, versionID)
	refusedWith(t, resp, body, http.StatusConflict, "LEGAL_EDITION_ALREADY_EFFECTIVE")

	// An id that names no edition of this document is a 404 — including an id
	// that names one of the OTHER document's, because the two are separate tables
	// and neither surface confirms the other's rows.
	policyEdition := legalCancelWorkspace(t, env, sessionID, operatorLegalPolicyPath).Published.VersionID
	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid", policyEdition} {
		resp, body := cancelLegalEdition(t, env, sessionID, operatorLegalTermsPath, id)
		refusedWith(t, resp, body, http.StatusNotFound, "LEGAL_EDITION_NOT_FOUND")
	}
}

// TestACancelledPolicyEditionReGatesNobody is the acceptance criterion this
// whole slice is arranged around, at the Customer gate.
//
// Ana accepts what is in force. A gating edition is scheduled under her feet and
// then withdrawn. THE DAY ARRIVES ANYWAY — the row is retained, and the calendar
// does not know it was cancelled — and she is asked nothing, because the
// cancellation is read by the same query that reads the date rather than by
// anything that was supposed to fire.
func TestACancelledPolicyEditionReGatesNobody(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")

	token := signInAnswering(t, env, "ana@example.com", true, true, true)
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"she has just answered everything")

	standing := legalCancelWorkspace(t, env, sessionID, operatorLegalPolicyPath).Published.VersionID
	versionID, _ := scheduleGatingEdition(t, env, sessionID, operatorLegalPolicyPath, 4)
	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"a SCHEDULED edition re-gates nobody yet: its day has not come")

	cancelLegalEditionOK(t, env, sessionID, operatorLegalPolicyPath, versionID)

	// The morning the edition would have taken effect.
	arriveAt(t, env, "policy_versions", versionID)

	assertBoxes(t, signedInConsentBoxes(t, env, token), false, false, false,
		"a CANCELLED edition re-gates nobody, on its own date or ever: it is excluded from the gating floor and from the satisfying set by the same query that answers `effective_date <= CURRENT_DATE`")

	// And it never became current: the words on the Storefront are still the
	// ones Ana was shown.
	current := legalCancelWorkspace(t, env, sessionID, operatorLegalPolicyPath)
	if current.Published.VersionID != standing {
		t.Fatalf("a cancelled edition became current: %q -> %q", standing, current.Published.VersionID)
	}
}

// TestACancelledTermsEditionReGatesNobodyOnTheStaffPlatform is the same
// criterion at the OTHER gate.
//
// The Terms gate is both populations' — every human on the Staff platform
// accepts them before a Staff Session is minted (§3, ADR 0066) — and it reaches
// the satisfying set by a different path (identity/service/termsgate.go), so
// "excluded from the satisfying set" has to be true there too or a cancellation
// would stop every Organizer signing in on a morning nobody chose.
func TestACancelledTermsEditionReGatesNobodyOnTheStaffPlatform(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	email := "cancellation@example.com"

	if sessionThroughTermsGate(t, env, requestAndVerify(t, env, email)) == "" {
		t.Fatal("expected a session once the edition in force is accepted")
	}

	versionID, _ := scheduleGatingEdition(t, env, sessionID, operatorLegalTermsPath, 5)
	cancelLegalEditionOK(t, env, sessionID, operatorLegalTermsPath, versionID)
	arriveAt(t, env, "terms_versions", versionID)

	outcome := decodeStaffSignInOutcome(t, requestAndVerify(t, env, email))
	if outcome.TermsRequired != nil {
		t.Fatalf("a CANCELLED edition re-gated a signed-in Organizer on its own date: %+v", outcome.TermsRequired)
	}
	if outcome.SessionID == "" {
		t.Fatal("expected the sign-in to mint a session straight through")
	}
}
