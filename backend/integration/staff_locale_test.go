package integration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// The Staff Locale (#284, parent #281): the language a person who signs in to
// staff is written to and written for.
//
// Everything here is asserted over HTTP, because everything here IS observable
// over HTTP — unlike the Sale Locale and the Mail Locale, whose only reader is
// the mail this platform sends and whose tests therefore have to read the
// column. A person's language is reported by the "me" endpoint, changed by a
// write endpoint, and remembered by a sign-in, so the caller's view is the whole
// view. The one exception is absence-without-a-session, which by definition no
// session can be opened to read; that test reads the table and says why.

const (
	staffMePath       = "/api/v1/staff/me"
	staffLocalePath   = "/api/v1/staff/me/locale"
	authSessionPath   = "/api/v1/auth/session"
	otpRequestPath    = "/api/v1/auth/otp/request"
	otpVerifyPathAuth = "/api/v1/auth/otp/verify"
)

// staffMeView is the shape /api/v1/staff/me answers with, narrowed to what these
// tests care about. `locale` is a pointer because null is a real answer: nobody
// has stated a language, which is not the same claim as English.
type staffMeView struct {
	OrganizationSlug string  `json:"organization_slug"`
	Locale           *string `json:"locale"`
}

type staffLocaleSessionView struct {
	Email  string  `json:"email"`
	Locale *string `json:"locale"`
}

// staffSignIn completes a staff passcode sign-in from a login page rendered in
// the given language. An empty locale sends no field at all, which is a caller
// that detected nothing — an older app, or one whose detection ladder fell all
// the way through.
func staffSignIn(t *testing.T, env *testEnv, email, locale string) string {
	t.Helper()

	_, _ = env.post(t, otpRequestPath, map[string]string{"email": email}, nil)

	payload := map[string]string{"email": email, "code": env.email.LastCode}
	if locale != "" {
		payload["locale"] = locale
	}
	resp, body := env.post(t, otpVerifyPathAuth, payload, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify otp status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var data struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if data.SessionID == "" {
		t.Fatal("expected session_id")
	}
	return data.SessionID
}

func staffMe(t *testing.T, env *testEnv, sessionID string) staffMeView {
	t.Helper()
	resp, body := env.get(t, staffMePath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET staff me status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view staffMeView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode staff me: %v", err)
	}
	return view
}

func staffSession(t *testing.T, env *testEnv, sessionID string) staffLocaleSessionView {
	t.Helper()
	resp, body := env.get(t, authSessionPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET session status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view staffLocaleSessionView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return view
}

// setStaffLocale drives the switcher's write and insists it succeeded.
func setStaffLocale(t *testing.T, env *testEnv, sessionID, locale string) {
	t.Helper()
	resp, body := env.put(t, staffLocalePath, map[string]string{"locale": locale}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT staff locale status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("PUT staff locale returned error %+v", body.Error)
	}
	var view struct {
		Locale string `json:"locale"`
	}
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode staff locale: %v", err)
	}
	if view.Locale != locale {
		t.Fatalf("write echoed locale=%q, want %q", view.Locale, locale)
	}
}

// storedStaffLocale reads the row itself.
//
// SQL rather than the API, and only in the tests that need it: absence is a
// claim about a person who has NO SESSION — somebody invited and never signed
// in, or an address that only ever appeared on a Payout Profile — and there is
// no session to authenticate a read with. That is exactly the case the storage
// is absent-by-default to serve, so it has to be asserted from underneath.
func storedStaffLocale(t *testing.T, env *testEnv, email string) sql.NullString {
	t.Helper()
	var locale sql.NullString
	err := env.db.QueryRow(`SELECT locale FROM staff_locales WHERE email = $1`, email).Scan(&locale)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}
	}
	if err != nil {
		t.Fatalf("read staff locale for %s: %v", email, err)
	}
	return locale
}

func wantLocale(t *testing.T, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("locale is null, want %q", want)
	}
	if *got != want {
		t.Fatalf("locale=%q, want %q", *got, want)
	}
}

// TestStaffMeReportsTheStaffLocale is the read the staff app makes on every
// render: it already asks who the Active Member is, and the language now comes
// back on that same answer rather than on a request of its own.
func TestStaffMeReportsTheStaffLocale(t *testing.T) {
	env := setupTest(t)

	sessionID := staffSignIn(t, env, "ana@example.com", "es")
	createOrganization(t, env, sessionID, "Sala Sur", "sala-sur")

	wantLocale(t, staffMe(t, env, sessionID).Locale, "es")
}

// TestStaffMeReportsNoLocaleForAPersonWhoStatedNone proves null is reachable and
// means what it says. An app that detected nothing signs somebody in, and the
// API reports that nobody has chosen — not that they chose English. The
// distinction is the whole reason this storage is absent by default: the day
// this person signs in from a Spanish browser, their choice is still free to be
// recorded.
func TestStaffMeReportsNoLocaleForAPersonWhoStatedNone(t *testing.T) {
	env := setupTest(t)

	sessionID := staffSignIn(t, env, "ana@example.com", "")
	createOrganization(t, env, sessionID, "South Hall", "south-hall")

	if got := staffMe(t, env, sessionID).Locale; got != nil {
		t.Fatalf("locale=%q, want null", *got)
	}
}

// TestStaffLocaleWriteIsReportedOnTheNextRead is the switcher: a Member picks a
// language and the next render is in it.
func TestStaffLocaleWriteIsReportedOnTheNextRead(t *testing.T) {
	env := setupTest(t)

	sessionID := staffSignIn(t, env, "ana@example.com", "")
	createOrganization(t, env, sessionID, "South Hall", "south-hall")

	setStaffLocale(t, env, sessionID, "es")

	wantLocale(t, staffMe(t, env, sessionID).Locale, "es")
}

// TestStaffLocaleSurvivesANewSession proves the choice is durable rather than a
// property of the session it was made on. Signing out and back in — from an
// English browser, at that — finds it exactly as it was left.
func TestStaffLocaleSurvivesANewSession(t *testing.T) {
	env := setupTest(t)

	first := staffSignIn(t, env, "ana@example.com", "")
	createOrganization(t, env, first, "South Hall", "south-hall")
	setStaffLocale(t, env, first, "es")

	resp, body := env.post(t, "/api/v1/auth/logout", nil, authHeader(first))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status=%d error=%+v", resp.StatusCode, body.Error)
	}

	second := staffSignIn(t, env, "ana@example.com", "en")
	wantLocale(t, staffMe(t, env, second).Locale, "es")
}

// TestStaffLocaleRefusesALanguageThisPlatformDoesNotServe. A stated choice that
// quietly did nothing would be worse than one that failed: the person would
// press the switcher, see nothing change, and have no idea why.
func TestStaffLocaleRefusesALanguageThisPlatformDoesNotServe(t *testing.T) {
	env := setupTest(t)

	sessionID := staffSignIn(t, env, "ana@example.com", "")

	resp, body := env.put(t, staffLocalePath, map[string]string{"locale": "fr"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT staff locale status=%d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("error=%+v, want VALIDATION_FAILED", body.Error)
	}
	raw, err := json.Marshal(body.Error.Details)
	if err != nil {
		t.Fatalf("marshal error details: %v", err)
	}
	var details struct {
		Fields []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode error details %s: %v", raw, err)
	}
	if len(details.Fields) != 1 || details.Fields[0].Field != "locale" || details.Fields[0].Code != "INVALID_LOCALE" {
		t.Fatalf("details=%s, want one locale/INVALID_LOCALE", raw)
	}
	if got := storedStaffLocale(t, env, "ana@example.com"); got.Valid {
		t.Fatalf("stored locale=%q after a refused write, want none", got.String)
	}
}

// TestSignInRemembersADetectedLocale is the narrow divergence from ADR 0033.
// That ADR leaves a box office sale's language absent because there is genuinely
// no evidence of the buyer's. A sign-in always carries some: the browser stated
// a preference, the login page was rendered in it, and the person read that page
// and carried on. Weak evidence, but not absent — and recording it is what makes
// somebody's app and their mail agree without them ever finding a setting.
func TestSignInRemembersADetectedLocale(t *testing.T) {
	env := setupTest(t)

	sessionID := staffSignIn(t, env, "ana@example.com", "es")

	wantLocale(t, staffSession(t, env, sessionID).Locale, "es")
}

// TestSignInDoesNotOverwriteAStoredStaffLocale is the other half of that rule,
// and the more important one. Somebody who chose Spanish and then signs in on a
// borrowed English laptop stays in Spanish: a browser's guess must never
// overrule a person's choice.
func TestSignInDoesNotOverwriteAStoredStaffLocale(t *testing.T) {
	env := setupTest(t)

	first := staffSignIn(t, env, "ana@example.com", "")
	setStaffLocale(t, env, first, "es")

	second := staffSignIn(t, env, "ana@example.com", "en")

	wantLocale(t, staffSession(t, env, second).Locale, "es")
}

// TestSignInIgnoresALanguageThisPlatformDoesNotServe. A malformed or unserved
// locale must never fail a sign-in — the person proved their address and the
// session is what they came for — so it is dropped and nothing is remembered.
func TestSignInIgnoresALanguageThisPlatformDoesNotServe(t *testing.T) {
	env := setupTest(t)

	sessionID := staffSignIn(t, env, "ana@example.com", "fr")

	if got := staffSession(t, env, sessionID).Locale; got != nil {
		t.Fatalf("locale=%q, want null", *got)
	}
}

// TestMemberOfTwoOrganizationsReadsOneStaffLocale is why the Staff Locale is not
// a column on `members`. That table is unique on (organization, email), so a
// column there would give this one human two languages and the app would change
// language when they switched context. One person, one language, under both
// Organizations.
func TestMemberOfTwoOrganizationsReadsOneStaffLocale(t *testing.T) {
	env := setupTest(t)

	const email = "ana@example.com"
	memberNorth, memberSouth := seedMultiMembership(t, env, email)
	sessionID := staffSignIn(t, env, email, "")

	selectStaffOrganization(t, env, sessionID, memberNorth)
	setStaffLocale(t, env, sessionID, "es")

	north := staffMe(t, env, sessionID)
	if north.OrganizationSlug != "north-hall" {
		t.Fatalf("organization_slug=%q, want north-hall", north.OrganizationSlug)
	}
	wantLocale(t, north.Locale, "es")

	selectStaffOrganization(t, env, sessionID, memberSouth)

	south := staffMe(t, env, sessionID)
	if south.OrganizationSlug != "south-hall" {
		t.Fatalf("organization_slug=%q, want south-hall", south.OrganizationSlug)
	}
	wantLocale(t, south.Locale, "es")
}

func selectStaffOrganization(t *testing.T, env *testEnv, sessionID, memberID string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/session/organization", map[string]string{
		"member_id": memberID,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("select organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestPlatformOperatorWithNoMembershipHoldsAStaffLocale is the other reason the
// language is not a column on `members`: an operator need not be a Member of
// anything (ADR 0015), and a column there would leave the people who read the
// Operator Dashboard with nowhere to put one. The write is reachable without an
// Active Member and the read comes back on the session.
func TestPlatformOperatorWithNoMembershipHoldsAStaffLocale(t *testing.T) {
	env := setupTest(t)

	sessionID := operatorSession(t, env, "operator@example.com")

	if session := staffSession(t, env, sessionID); session.Locale != nil {
		t.Fatalf("locale=%q before any choice, want null", *session.Locale)
	}

	setStaffLocale(t, env, sessionID, "es")
	wantLocale(t, staffSession(t, env, sessionID).Locale, "es")

	setStaffLocale(t, env, sessionID, "en")
	wantLocale(t, staffSession(t, env, sessionID).Locale, "en")
}

// TestPersonWhoNeverSignedInHasNoStaffLocale. Nobody is given a language by
// being invited: existing Members are not backfilled and no row is written for
// an address until somebody at it signs in or chooses. The preseeded Member is a
// Member of an Organization, has never signed in, and has nothing stored — and
// a different person signing in beside them does not change that.
func TestPersonWhoNeverSignedInHasNoStaffLocale(t *testing.T) {
	env := setupTest(t)

	if got := storedStaffLocale(t, env, "preseeded@example.com"); got.Valid {
		t.Fatalf("stored locale=%q for a Member who never signed in, want none", got.String)
	}

	staffSignIn(t, env, "ana@example.com", "es")

	if got := storedStaffLocale(t, env, "preseeded@example.com"); got.Valid {
		t.Fatalf("stored locale=%q after somebody else signed in, want none", got.String)
	}
	if got := storedStaffLocale(t, env, "ana@example.com"); !got.Valid || got.String != "es" {
		t.Fatalf("stored locale=%+v for the person who signed in, want es", got)
	}
}
