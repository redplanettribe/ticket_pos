package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Follow intent through sign-in (#219, parent #215).
//
// An anonymous visitor presses Follow, proves who they are, and the thing they
// pressed on is Followed when they land back. The press and the identity are
// minutes and one mailbox apart, so the intent has to travel — and the whole
// security question of this feature is WHAT it is allowed to say when it
// arrives.
//
// The answer, pinned below: it names a SUBJECT and never a subscriber. The
// Follow is written against the Customer Session that verification just minted
// and against nothing else, so an email travelling beside the intent — the one
// the passcode was sent to, or any other — can never become the address that
// gets subscribed. That is the difference between this feature and a mechanism
// for signing somebody else's inbox up for mail (ADR 0010).
//
// The browser half of the round trip — the button, the redirect back — is the
// E2E's (e2e/tests/follow-intent.spec.ts) and is deliberately not restated here.

// followIntentFor spells an intent the way it travels: one string, kind first,
// so the query parameter a visitor can see and the JSON field the API validates
// are the same text. Tag Follows (#218) join it as "tag:<key>".
func followIntentFor(slug string) string { return "organization:" + slug }

// verifyResponseWithFollow is the sign-in answer, including what the intent that
// rode along became. `follow` is null whenever no intent was carried or the
// subject it named no longer exists.
type verifyResponseWithFollow struct {
	Session   customerSessionView `json:"session"`
	SessionID string              `json:"session_id"`
	Follow    *followView         `json:"follow"`
}

// customerSignInWithFollowIntent completes a passcode sign-in carrying a Follow
// intent, and returns the raw exchange so refusal tests can read the envelope.
func customerSignInWithFollowIntent(t *testing.T, env *testEnv, email, intent string) (*http.Response, envelope) {
	t.Helper()
	resp, body := requestCustomerPasscode(t, env, email, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request customer passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return env.post(t, customerOTPVerifyPath, map[string]string{
		"email":  email,
		"code":   env.email.LastCode,
		"follow": intent,
	}, nil)
}

// signInWithFollowIntentOK completes a passcode sign-in that is expected to
// work, and returns what it answered.
func signInWithFollowIntentOK(t *testing.T, env *testEnv, email, intent string) verifyResponseWithFollow {
	t.Helper()
	resp, body := customerSignInWithFollowIntent(t, env, email, intent)
	return decodeVerifyWithFollow(t, resp, body)
}

// decodeVerifyWithFollow insists a sign-in worked and hands back its answer.
func decodeVerifyWithFollow(t *testing.T, resp *http.Response, body envelope) verifyResponseWithFollow {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("verify error=%+v, want none", body.Error)
	}
	var data verifyResponseWithFollow
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if data.SessionID == "" {
		t.Fatal("expected a Customer Session token")
	}
	return data
}

// TestFollowIntentThroughPasscodeSignInCreatesTheFollow is the feature in one
// pass on the passcode door: the intent arrives with the verification, and the
// Follow exists the moment the session does.
func TestFollowIntentThroughPasscodeSignInCreatesTheFollow(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	data := signInWithFollowIntentOK(t, env, "ana@example.com", followIntentFor("test-org"))

	if data.Follow == nil {
		t.Fatal("sign-in carried a Follow intent and reported no Follow")
	}
	if data.Follow.Type != "organization" {
		t.Fatalf("follow type = %q, want organization", data.Follow.Type)
	}
	if data.Follow.Organization == nil || data.Follow.Organization.Slug != "test-org" {
		t.Fatalf("followed organization = %+v, want test-org", data.Follow.Organization)
	}

	// And it is a real Follow rather than a report of one: the Customer's own
	// listing, read through the session verification minted, says so.
	if got := followedSlugs(t, listFollows(t, env, data.SessionID)); len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("follows after sign-in = %v, want [test-org]", got)
	}
}

// TestFollowIntentThroughGoogleSignInCreatesTheFollow is the same fact on the
// other door. Both are Proof of Email Ownership and neither is worth more than
// the other (ADR 0011), so an intent must be honoured identically on both — a
// visitor who pressed Follow and then chose Google must not silently lose it.
func TestFollowIntentThroughGoogleSignInCreatesTheFollow(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	googleStub.returns(verifiedGoogleClaims("ana@example.com"))

	resp, body := env.post(t, customerGoogleVerifyPath, map[string]string{
		"code":          googleAuthCode,
		"code_verifier": googleCodeVerifier,
		"redirect_uri":  googleRedirectURI,
		"follow":        followIntentFor("test-org"),
	}, nil)
	data := decodeVerifyWithFollow(t, resp, body)

	if data.Follow == nil || data.Follow.Organization == nil || data.Follow.Organization.Slug != "test-org" {
		t.Fatalf("follow = %+v, want test-org", data.Follow)
	}
	if got := followedSlugs(t, listFollows(t, env, data.SessionID)); len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("follows after Google sign-in = %v, want [test-org]", got)
	}
}

// TestFollowIntentIsWrittenAgainstTheVerifiedSessionAlone is the point of the
// whole ticket.
//
// The verify request names an email — it has to, the passcode was sent to one —
// and an intent travels beside it. If the intent were allowed to name WHOSE
// Follow it is, this endpoint would become a way to subscribe an address the
// caller does not control to a weekly email. So the intent names the subject and
// only the subject, and the subscriber is read from the session verification
// produced: the Follow lands on the Customer who proved the email, and on nobody
// else's list.
func TestFollowIntentIsWrittenAgainstTheVerifiedSessionAlone(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	// The address that never asked for anything. It exists as a Customer before
	// the attempt, so "no Follow" below is a fact about a real record rather than
	// about a row that was never there.
	victim := customerSignIn(t, env, "bruno@example.com")

	data := signInWithFollowIntentOK(t, env, "ana@example.com", followIntentFor("test-org"))

	if got := followedSlugs(t, listFollows(t, env, data.SessionID)); len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("the verifying Customer's follows = %v, want [test-org]", got)
	}
	if got := followedSlugs(t, listFollows(t, env, victim)); len(got) != 0 {
		t.Fatalf("an address that never verified anything now follows %v — the intent chose the subscriber", got)
	}
	if n := countOrganizationFollows(t, env); n != 1 {
		t.Fatalf("%d Follow rows, want exactly the one the verified session made", n)
	}

	// And the intent cannot even SPELL a subscriber: an address in the subject
	// position is not a kind of Follow this platform has, so it is refused before
	// the passcode is looked at rather than interpreted generously.
	resp, body := customerSignInWithFollowIntent(t, env, "ana@example.com", "email:bruno@example.com")
	assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")
	if n := countOrganizationFollows(t, env); n != 1 {
		t.Fatalf("%d Follow rows after the refused intent, want the original 1", n)
	}
}

// TestFollowIntentIsValidatedStrictly walks the shapes an intent must not have.
//
// Every one of these is 400 rather than "ignored quietly", because a malformed
// intent is a caller that believes it asked for something. The two that matter
// most are the last two: a subject position holding an absolute URL is what an
// open redirect looks like when it is smuggled through a Follow, and a subject
// holding an address is what subscribing a stranger looks like. Neither is a
// slug, and nothing that is not a slug gets through.
func TestFollowIntentIsValidatedStrictly(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	// One passcode for the whole table. Every attempt below is refused before the
	// passcode is looked at, so the same code is still live for the next one —
	// which is itself part of what is being asserted (see the test below).
	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	code := env.email.LastCode

	for _, malformed := range []struct {
		what   string
		intent string
	}{
		{"no kind at all", "test-org"},
		{"a kind this platform does not have", "playlist:test-org"},
		{"a kind that is an address", "email:bruno@example.com"},
		{"an empty subject", "organization:"},
		{"an empty kind", ":test-org"},
		{"a subject with a space", "organization:test org"},
		{"a subject shouted", "organization:TEST-ORG"},
		{"a subject that is a path", "organization:../../test-org"},
		{"a subject that is an absolute URL", "organization:https://evil.example/test-org"},
		{"a subject that is a protocol-relative URL", "organization://evil.example"},
		{"a second kind smuggled behind the first", "organization:test-org:tag:techno"},
	} {
		resp, body := env.post(t, customerOTPVerifyPath, map[string]string{
			"email":  "ana@example.com",
			"code":   code,
			"follow": malformed.intent,
		}, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s (%q): status=%d, want 400", malformed.what, malformed.intent, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%s (%q): error=%+v, want VALIDATION_FAILED", malformed.what, malformed.intent, body.Error)
		}
	}

	if n := countOrganizationFollows(t, env); n != 0 {
		t.Fatalf("%d Follow rows after only malformed intents, want 0", n)
	}
}

// TestAMalformedFollowIntentDoesNotSpendThePasscode pins where the validation
// sits: before anything is proved.
//
// A visitor whose intent was mangled in transit must still be able to sign in.
// Were the intent read after verification, a bad one would burn the passcode and
// leave them staring at a form asking for a code that is now worthless.
func TestAMalformedFollowIntentDoesNotSpendThePasscode(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	code := env.email.LastCode

	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email":  "ana@example.com",
		"code":   code,
		"follow": "playlist:test-org",
	}, nil)
	assertAPIError(t, resp, body, http.StatusBadRequest, "VALIDATION_FAILED")

	// The same passcode, second time, with no intent: it must still work.
	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email": "ana@example.com",
		"code":  code,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the passcode was spent by a refused intent: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestAFollowIntentNamingAnUnknownOrganizationStillSignsIn draws the line
// between the two halves of this request.
//
// Verification is the thing that was asked for and it must not fail over the
// other one. An Organization deleted while a visitor was in their mailbox is not
// a reason to refuse them a session — they get signed in, and the answer says
// plainly that nothing was Followed.
func TestAFollowIntentNamingAnUnknownOrganizationStillSignsIn(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	data := signInWithFollowIntentOK(t, env, "ana@example.com", followIntentFor("gone-org"))

	if data.Follow != nil {
		t.Fatalf("follow = %+v, want null — nothing by that slug exists", data.Follow)
	}
	if got := followedSlugs(t, listFollows(t, env, data.SessionID)); len(got) != 0 {
		t.Fatalf("follows = %v, want none", got)
	}
}

// TestAFailedVerificationWithAFollowIntentCreatesNoFollow is the abandoned
// sign-in, stated at the seam where it can be stated: a visitor who never
// completes the proof gets nothing, and an intent is not a Follow.
func TestAFailedVerificationWithAFollowIntentCreatesNoFollow(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A well-formed intent, a wrong passcode. The intent is impeccable and buys
	// nothing, because nothing was proved.
	resp, body = env.post(t, customerOTPVerifyPath, map[string]string{
		"email":  "ana@example.com",
		"code":   wrongPasscode(env.email.LastCode),
		"follow": followIntentFor("test-org"),
	}, nil)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a wrong passcode signed somebody in: %+v", body.Data)
	}
	if n := countOrganizationFollows(t, env); n != 0 {
		t.Fatalf("%d Follow rows after a failed verification, want 0", n)
	}
}

// wrongPasscode returns a six-digit code that is not the one issued.
func wrongPasscode(issued string) string {
	if issued == "000000" {
		return "111111"
	}
	return "000000"
}

// TestAConfirmationLinkCannotCarryAFollowIntent closes the one door that would
// otherwise reopen everything the scope rule shuts.
//
// A Confirmation Link mints a sale-scoped session, and #217 already refuses that
// session the three Follow routes. An intent riding the redemption would be the
// same subscription by another road: whoever a Sale Confirmation was forwarded
// to could sign the ticket-holder's address up for a weekly email without ever
// proving they own it. The redemption is refused outright rather than redeemed
// and then narrowed — this door is for reading one receipt, and subscribing an
// address takes the same proof signing in does (CONTEXT.md, ADR 0010).
func TestAConfirmationLinkCannotCarryAFollowIntent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Link Fest", "link-fest",
		env.fixedClock.Add(60*24*time.Hour), "follow-intent-1", "ana@example.com", "Ana", "Lopez")
	token := lastConfirmationLinkToken(t, env)

	resp, body := env.post(t, confirmationLinkPath, map[string]string{
		"token":  token,
		"follow": followIntentFor("test-org"),
	}, nil)
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	if n := countOrganizationFollows(t, env); n != 0 {
		t.Fatalf("%d Follow rows after a Confirmation Link carried an intent, want 0", n)
	}

	// The link itself is untouched by the refusal: redeemed without an intent it
	// does exactly what a Confirmation Link is for.
	if _, saleScoped := redeemConfirmationLinkOK(t, env, token, ""); saleScoped == "" {
		t.Fatal("expected the Confirmation Link to still redeem without an intent")
	}
}

// tagFollowIntentFor spells a Tag intent. A Tag is named by its canonical key,
// which is NOT a slug: it is the display name lowercased with whitespace
// collapsed (ADR 0004), so it carries interior spaces, and two of the seeded
// Preset Tags carry an ampersand as well.
func tagFollowIntentFor(canonicalKey string) string { return "tag:" + canonicalKey }

// TestAFollowIntentCanNameATag is the other kind of subject reaching the same
// door. Tag Follows (#218) and the intent through sign-in (#219) were built
// side by side and neither could see the other, so this is the seam between
// them: pressing Follow on a Tag chip while signed out has to end in a Tag
// Follow, and it is the integration of the two rather than either alone.
func TestAFollowIntentCanNameATag(t *testing.T) {
	env := setupTest(t)

	data := signInWithFollowIntentOK(t, env, "ana@example.com", tagFollowIntentFor("music"))

	if data.Follow == nil {
		t.Fatal("sign-in carried a Tag Follow intent and reported no Follow")
	}
	if data.Follow.Type != "tag" {
		t.Fatalf("follow type = %q, want tag", data.Follow.Type)
	}
	if data.Follow.Tag == nil || data.Follow.Tag.CanonicalKey != "music" {
		t.Fatalf("followed tag = %+v, want music", data.Follow.Tag)
	}
}

// TestAFollowIntentCanNameATagWhoseKeyIsNotASlug is the bug this test exists
// for, and it is invisible without it.
//
// The intent's key was first validated against the Organization slug pattern —
// lowercase letters, digits and hyphens — which is right for a slug and wrong
// for a Tag. "arts & theatre" and "food & drink" are seeded Preset Tags, so two
// of the twelve most followable Tags on the platform would have been refused:
// the chip renders, the visitor signs in, and the Follow they asked for is
// simply absent, with nothing anywhere saying why.
//
// The rule is loosened only as far as a canonical key needs and no further, so
// this asserts both halves — the ampersand is accepted, and the characters that
// would make a key a URL, a path or an address are still refused.
func TestAFollowIntentCanNameATagWhoseKeyIsNotASlug(t *testing.T) {
	env := setupTest(t)

	data := signInWithFollowIntentOK(t, env, "ana@example.com", tagFollowIntentFor("arts & theatre"))

	if data.Follow == nil {
		t.Fatal("a seeded Preset Tag with an ampersand was refused as a Follow intent")
	}
	if data.Follow.Tag == nil || data.Follow.Tag.CanonicalKey != "arts & theatre" {
		t.Fatalf("followed tag = %+v, want \"arts & theatre\"", data.Follow.Tag)
	}
}

// TestATagFollowIntentIsStillValidatedStrictly pins the half of the loosening
// that must not have happened: a Tag key admits a space and an ampersand and
// nothing else new. Every string below is still refused.
func TestATagFollowIntentIsStillValidatedStrictly(t *testing.T) {
	env := setupTest(t)

	resp, body := requestCustomerPasscode(t, env, "ana@example.com", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request passcode status=%d error=%+v", resp.StatusCode, body.Error)
	}
	code := env.email.LastCode

	for _, malformed := range []struct {
		what   string
		intent string
	}{
		{"a tag key that is a path", "tag:../../music"},
		{"a tag key that is an absolute URL", "tag:https://evil.example/music"},
		{"a tag key that is an address", "tag:bruno@example.com"},
		{"a tag key shouted", "tag:MUSIC"},
		{"a tag key with a leading separator", "tag: music"},
		{"a tag key with a trailing separator", "tag:music-"},
		{"a tag key that is only a separator", "tag:&"},
	} {
		resp, body := env.post(t, customerOTPVerifyPath, map[string]string{
			"email":  "ana@example.com",
			"code":   code,
			"follow": malformed.intent,
		}, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", malformed.what, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%s: error=%+v, want VALIDATION_FAILED", malformed.what, body.Error)
		}
	}
}
