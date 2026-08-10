package integration

import (
	"net/http"
	"testing"
	"time"
)

// mailLocale reads a Customer's remembered Mail Locale straight from the row.
//
// SQL rather than the API because no endpoint exposes it: this is enabling work
// for localized mail (ADR 0030, ADR 0033), and the only reader of a Mail Locale
// is the mail this platform sends. Nothing a Customer or a Member can see
// changes, so the side effect has no HTTP surface to assert against.
func mailLocale(t *testing.T, env *testEnv, email string) string {
	t.Helper()
	var locale string
	if err := env.db.QueryRow(`SELECT mail_locale FROM customers WHERE email = $1`, email).Scan(&locale); err != nil {
		t.Fatalf("read mail_locale for %s: %v", email, err)
	}
	return locale
}

// customerSignInWithLocale completes a passcode sign-in from a Storefront page
// served in the given Locale. An empty locale sends no field at all, which is a
// caller that does not name one.
func customerSignInWithLocale(t *testing.T, env *testEnv, email, locale string) {
	t.Helper()
	resp, body := requestCustomerPasscode(t, env, email, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request customer passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	payload := map[string]string{"email": email, "code": env.email.LastCode}
	if locale != "" {
		payload["locale"] = locale
	}
	resp, body = env.post(t, customerOTPVerifyPath, payload, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify customer passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// customerGoogleSignInWithLocale completes a Google Sign-In relayed from a
// Storefront served in the given Locale.
func customerGoogleSignInWithLocale(t *testing.T, env *testEnv, email, locale string) {
	t.Helper()
	googleStub.returns(verifiedGoogleClaims(email))

	payload := map[string]string{
		"code":          googleAuthCode,
		"code_verifier": googleCodeVerifier,
		"redirect_uri":  googleRedirectURI,
	}
	if locale != "" {
		payload["locale"] = locale
	}
	resp, body := env.post(t, customerGoogleVerifyPath, payload, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("google verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestCustomerMailLocaleRememberedFromPasscodeSignIn proves the Storefront's
// language reaches the Customer record through the passcode door: mail has no
// address to carry a Locale, so the sign-in is where one is remembered.
func TestCustomerMailLocaleRememberedFromPasscodeSignIn(t *testing.T) {
	env := setupTest(t)

	customerSignInWithLocale(t, env, "ana@example.com", "es")

	if got := mailLocale(t, env, "ana@example.com"); got != "es" {
		t.Fatalf("mail locale=%q, want es", got)
	}
}

// TestCustomerMailLocaleRememberedFromGoogleSignIn proves the other door
// records it identically. Both doors assert the same fact (ADR 0011) and both
// are opened from a page that names its language.
func TestCustomerMailLocaleRememberedFromGoogleSignIn(t *testing.T) {
	env := setupTest(t)

	customerGoogleSignInWithLocale(t, env, "bruno@example.com", "es")

	if got := mailLocale(t, env, "bruno@example.com"); got != "es" {
		t.Fatalf("mail locale=%q, want es", got)
	}
}

// TestCustomerMailLocaleIsEnglishForACustomerWhoNeverSignedIn proves a
// Customer created by a Ticket Sale — a person who has never been on a
// localized surface at all — carries English rather than an empty Locale, so
// the Digest always has a language to render in.
func TestCustomerMailLocaleIsEnglishForACustomerWhoNeverSignedIn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	seedSaleForCustomer(t, env, sessionID, "Imported Fest", "imported-fest",
		env.fixedClock.Add(30*24*time.Hour), "import-1", "carla@example.com", "Carla", "Ruiz")

	if got := mailLocale(t, env, "carla@example.com"); got != "en" {
		t.Fatalf("mail locale=%q, want en", got)
	}
}

// TestCustomerMailLocaleIsEnglishWhenTheSignInNamesNoLocale proves a sign-in
// relayed by a surface with no language to declare leaves the default standing
// rather than blanking it.
func TestCustomerMailLocaleIsEnglishWhenTheSignInNamesNoLocale(t *testing.T) {
	env := setupTest(t)

	customerSignInWithLocale(t, env, "diego@example.com", "")

	if got := mailLocale(t, env, "diego@example.com"); got != "en" {
		t.Fatalf("mail locale=%q, want en", got)
	}
}

// TestCustomerMailLocaleFollowsTheMostRecentSignIn proves it is remembered
// from the Storefront the Customer LAST signed in on: someone who switches
// language and signs in again is read in the language they switched to.
func TestCustomerMailLocaleFollowsTheMostRecentSignIn(t *testing.T) {
	env := setupTest(t)

	customerSignInWithLocale(t, env, "elena@example.com", "es")
	if got := mailLocale(t, env, "elena@example.com"); got != "es" {
		t.Fatalf("mail locale after first sign-in=%q, want es", got)
	}

	customerGoogleSignInWithLocale(t, env, "elena@example.com", "en")
	if got := mailLocale(t, env, "elena@example.com"); got != "en" {
		t.Fatalf("mail locale after second sign-in=%q, want en", got)
	}
}

// TestCustomerMailLocaleIgnoresALanguageTheStorefrontDoesNotServe proves a
// Locale outside the two supported ones neither refuses the sign-in nor
// overwrites what was remembered. Signing in is Proof of Email Ownership and
// must not fail over a preference; an unserved language is simply not a Locale
// this platform can write mail in.
func TestCustomerMailLocaleIgnoresALanguageTheStorefrontDoesNotServe(t *testing.T) {
	env := setupTest(t)

	customerSignInWithLocale(t, env, "fabio@example.com", "es")
	customerSignInWithLocale(t, env, "fabio@example.com", "fr")

	if got := mailLocale(t, env, "fabio@example.com"); got != "es" {
		t.Fatalf("mail locale=%q, want es — an unserved language must not overwrite one that is served", got)
	}
}
