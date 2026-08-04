package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Follow (#217, parent #215): a Customer's standing subscription to an
// Organization. Everything below goes in and out over HTTP, because the Follow's
// whole visible existence is three routes and one list — and because the rules
// worth pinning here (idempotency, session scope, whose list is whose) are all
// rules about what a request is allowed to do, not about what a function
// returns.
//
// Nothing here sends email. The Follow Digest is ADR 0030's and is not built.

const (
	customerFollowsPath = "/api/v1/customer/follows"
)

// followedOrganization is the Organization as a Follow reports it: exactly the
// three facts its public profile publishes, and no internal id.
type followedOrganization struct {
	Name    string  `json:"name"`
	Slug    string  `json:"slug"`
	LogoURL *string `json:"logo_url"`
}

// followView is one entry in the Customer's Follows. `type` is the
// discriminator; the subject hangs off the field it names, so a Tag Follow
// (#218) arrives in this same list as a `tag` entry without moving anything a
// client already reads.
type followView struct {
	Type         string                `json:"type"`
	FollowedAt   time.Time             `json:"followed_at"`
	Organization *followedOrganization `json:"organization"`
}

type followsView struct {
	Follows []followView `json:"follows"`
}

func followOrganizationPath(slug string) string {
	return customerFollowsPath + "/organizations/" + slug
}

// followOrganization presses Follow on an Organization and returns the raw
// exchange, so error-path tests can assert the envelope.
func followOrganization(t *testing.T, env *testEnv, token, slug string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, followOrganizationPath(slug), nil, authHeader(token))
}

// followOrganizationOK presses Follow and insists it worked, returning the
// Follow as the API reports it.
func followOrganizationOK(t *testing.T, env *testEnv, token, slug string) followView {
	t.Helper()
	resp, body := followOrganization(t, env, token, slug)
	// 200, not 201. The call is idempotent, so there is no "created" that a
	// repeat could contradict; see the double-follow test below.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("follow %q status=%d error=%+v", slug, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("follow %q error=%+v, want none", slug, body.Error)
	}
	var view followView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode follow: %v", err)
	}
	return view
}

func unfollowOrganization(t *testing.T, env *testEnv, token, slug string) (*http.Response, envelope) {
	t.Helper()
	return env.deleteJSON(t, followOrganizationPath(slug), nil, authHeader(token))
}

func unfollowOrganizationOK(t *testing.T, env *testEnv, token, slug string) {
	t.Helper()
	resp, body := unfollowOrganization(t, env, token, slug)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unfollow %q status=%d error=%+v", slug, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("unfollow %q error=%+v, want none", slug, body.Error)
	}
}

// listFollows reads everything a Customer Follows. The request carries no
// identifier of whose list it is: the session is the only scope.
func listFollows(t *testing.T, env *testEnv, token string) followsView {
	t.Helper()
	resp, body := env.get(t, customerFollowsPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list follows status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("list follows error=%+v, want none", body.Error)
	}
	var view followsView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode follows: %v", err)
	}
	return view
}

// followedSlugs flattens a listing to the Organization slugs in it, in the order
// the API returned them.
func followedSlugs(t *testing.T, view followsView) []string {
	t.Helper()
	slugs := make([]string, 0, len(view.Follows))
	for _, follow := range view.Follows {
		if follow.Type != "organization" {
			t.Fatalf("follow type = %q, want organization — this feature builds no other kind", follow.Type)
		}
		if follow.Organization == nil {
			t.Fatalf("follow of type organization carries no organization: %+v", follow)
		}
		slugs = append(slugs, follow.Organization.Slug)
	}
	return slugs
}

// countOrganizationFollows reads the table directly. SQL because idempotency is
// a statement about ROWS, and the API deliberately cannot show two Follows of
// one Organization however many exist — which is exactly the bug this guards
// against.
func countOrganizationFollows(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customer_organization_follows`).Scan(&n); err != nil {
		t.Fatalf("count follows: %v", err)
	}
	return n
}

// TestCustomerFollowsAnOrganizationAndItAppearsInTheirFollows is the whole
// feature in one pass: press Follow on an Organization's public page, and the
// Customer's Follows say so.
func TestCustomerFollowsAnOrganizationAndItAppearsInTheirFollows(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	// Nothing followed yet, and that is an empty list rather than an error or a
	// null — a Customer who follows nothing owes the same well-formed answer as
	// one who follows everything.
	before := listFollows(t, env, token)
	if len(before.Follows) != 0 {
		t.Fatalf("follows before = %+v, want empty", before.Follows)
	}

	follow := followOrganizationOK(t, env, token, "test-org")
	if follow.Type != "organization" {
		t.Fatalf("type = %q, want organization", follow.Type)
	}
	if follow.Organization == nil || follow.Organization.Slug != "test-org" || follow.Organization.Name != "Test Org" {
		t.Fatalf("organization = %+v, want Test Org / test-org", follow.Organization)
	}
	if follow.FollowedAt.IsZero() {
		t.Fatal("followed_at is zero — a Follow must record when it was made")
	}

	after := listFollows(t, env, token)
	if got := followedSlugs(t, after); len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("follows = %v, want [test-org]", got)
	}
	if after.Follows[0].FollowedAt != follow.FollowedAt {
		t.Fatalf("listing followed_at = %v, write said %v", after.Follows[0].FollowedAt, follow.FollowedAt)
	}
}

// TestCustomerUnfollowsAnOrganization is the other half of the same control:
// pressing it again takes the Follow away.
func TestCustomerUnfollowsAnOrganization(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	followOrganizationOK(t, env, token, "test-org")
	unfollowOrganizationOK(t, env, token, "test-org")

	if got := followedSlugs(t, listFollows(t, env, token)); len(got) != 0 {
		t.Fatalf("follows after unfollow = %v, want none", got)
	}
	if n := countOrganizationFollows(t, env); n != 0 {
		t.Fatalf("%d Follow rows survive the unfollow, want 0", n)
	}
}

// TestFollowingTwiceLeavesOneFollowAndDoesNotMoveWhenItWasMade is the
// idempotency the acceptance criteria ask for, stated as two facts rather than
// one. A retried or double-tapped request must not leave a second Follow — and
// it must not quietly rewrite when the person subscribed either, because that
// instant is what the listing is ordered by and what a Digest will one day reason
// from.
func TestFollowingTwiceLeavesOneFollowAndDoesNotMoveWhenItWasMade(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	first := followOrganizationOK(t, env, token, "test-org")

	// Time moves between the two presses, so a followed_at that was being
	// overwritten would be visibly different rather than coincidentally equal.
	setCustomerClock(t, env, env.fixedClock.Add(48*time.Hour))
	second := followOrganizationOK(t, env, token, "test-org")

	if !second.FollowedAt.Equal(first.FollowedAt) {
		t.Fatalf("repeat follow moved followed_at from %v to %v", first.FollowedAt, second.FollowedAt)
	}
	if n := countOrganizationFollows(t, env); n != 1 {
		t.Fatalf("%d Follow rows after following twice, want exactly 1", n)
	}
	if got := followedSlugs(t, listFollows(t, env, token)); len(got) != 1 {
		t.Fatalf("follows = %v, want exactly one entry", got)
	}
}

// TestUnfollowingSomethingNotFollowedIsNotAnError pins the mirror of the above.
// The caller asked for a state — "I do not Follow this" — and that state already
// holds; reporting it as a failure would make an ordinary second tap look like
// something went wrong.
func TestUnfollowingSomethingNotFollowedIsNotAnError(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	unfollowOrganizationOK(t, env, token, "test-org")
	unfollowOrganizationOK(t, env, token, "test-org")

	if n := countOrganizationFollows(t, env); n != 0 {
		t.Fatalf("%d Follow rows, want 0", n)
	}
}

// TestFollowsPersistAcrossSessionsAndDevices is the point of ADR 0010's durable
// Customer identity applied to this feature: a Follow belongs to the person, not
// to the browser that made it. Signing out and back in — which is what a second
// device looks like from the API's side — finds it exactly where it was left.
func TestFollowsPersistAcrossSessionsAndDevices(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	first := customerSignIn(t, env, "ana@example.com")
	followed := followOrganizationOK(t, env, first, "test-org")

	resp, body := env.post(t, "/api/v1/customer/auth/logout", nil, authHeader(first))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign out status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A wholly separate session on the same email: the second device.
	second := customerSignIn(t, env, "ana@example.com")
	if second == first {
		t.Fatal("expected a distinct Customer Session token")
	}

	view := listFollows(t, env, second)
	if got := followedSlugs(t, view); len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("follows on the second session = %v, want [test-org]", got)
	}
	if !view.Follows[0].FollowedAt.Equal(followed.FollowedAt) {
		t.Fatalf("followed_at = %v on the second session, want %v", view.Follows[0].FollowedAt, followed.FollowedAt)
	}
}

// TestOneCustomersFollowsAreNeverAnothers is the adversarial read. The listing
// takes no identifier at all, so the only thing that could widen it is a bug in
// the scoping — and this is the test that would catch one.
func TestOneCustomersFollowsAreNeverAnothers(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// A second Organization so the two Customers follow different things and a
	// leak shows up as the wrong slug rather than as a count.
	createOrganization(t, env, sessionID, "Other Org", "other-org")

	ana := customerSignIn(t, env, "ana@example.com")
	bruno := customerSignIn(t, env, "bruno@example.com")

	followOrganizationOK(t, env, ana, "test-org")
	followOrganizationOK(t, env, bruno, "other-org")

	if got := followedSlugs(t, listFollows(t, env, ana)); len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("ana's follows = %v, want [test-org] alone", got)
	}
	if got := followedSlugs(t, listFollows(t, env, bruno)); len(got) != 1 || got[0] != "other-org" {
		t.Fatalf("bruno's follows = %v, want [other-org] alone", got)
	}

	// And one cannot reach into the other's list by unfollowing on their behalf:
	// the delete is scoped by session too, so this removes nothing.
	unfollowOrganizationOK(t, env, bruno, "test-org")
	if got := followedSlugs(t, listFollows(t, env, ana)); len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("ana's follows after bruno unfollowed test-org = %v, want [test-org] untouched", got)
	}
}

// TestFollowsListIsOrderedMostRecentlyFollowedFirst pins the order the listing
// promises, because a client rendering "what I follow" will show the top of it.
func TestFollowsListIsOrderedMostRecentlyFollowedFirst(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	createOrganization(t, env, sessionID, "Other Org", "other-org")
	token := customerSignIn(t, env, "ana@example.com")

	followOrganizationOK(t, env, token, "test-org")
	setCustomerClock(t, env, env.fixedClock.Add(time.Hour))
	followOrganizationOK(t, env, token, "other-org")

	got := followedSlugs(t, listFollows(t, env, token))
	if len(got) != 2 || got[0] != "other-org" || got[1] != "test-org" {
		t.Fatalf("follows = %v, want [other-org test-org] — most recent first", got)
	}
}

// TestFollowingAnUnknownOrganizationIs404 keeps the Follow endpoints from
// becoming an oracle the public Organization profile is not: a slug nobody owns
// answers here exactly as it answers there.
func TestFollowingAnUnknownOrganizationIs404(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	resp, body := followOrganization(t, env, token, "does-not-exist")
	assertAPIError(t, resp, body, http.StatusNotFound, "ORGANIZATION_NOT_FOUND")

	resp, body = unfollowOrganization(t, env, token, "does-not-exist")
	assertAPIError(t, resp, body, http.StatusNotFound, "ORGANIZATION_NOT_FOUND")
}

// TestFollowRoutesRefuseAnUnauthenticatedRequest covers all three routes, and
// covers them with a missing token and with a worthless one — the two ways a
// browser actually arrives without a session.
func TestFollowRoutesRefuseAnUnauthenticatedRequest(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	for _, credential := range []struct {
		what  string
		token string
		code  string
	}{
		{"no token at all", "", "UNAUTHORIZED"},
		{"a token that authenticates nothing", "not-a-session", "CUSTOMER_SESSION_NOT_FOUND"},
	} {
		var headers map[string]string
		if credential.token != "" {
			headers = authHeader(credential.token)
		}

		resp, body := env.get(t, customerFollowsPath, headers)
		assertAPIError(t, resp, body, http.StatusUnauthorized, credential.code)

		resp, body = env.post(t, followOrganizationPath("test-org"), nil, headers)
		assertAPIError(t, resp, body, http.StatusUnauthorized, credential.code)

		resp, body = env.deleteJSON(t, followOrganizationPath("test-org"), nil, headers)
		assertAPIError(t, resp, body, http.StatusUnauthorized, credential.code)

		if n := countOrganizationFollows(t, env); n != 0 {
			t.Fatalf("%d Follow rows after unauthenticated calls with %s, want 0", n, credential.what)
		}
	}
}

// TestAConfirmationLinkSessionCannotFollowAnything is the rule the ticket's own
// wording does not reach.
//
// "An active Customer Session implies a verified email" is true of a FULL
// Customer Session and false of the one redeemed from a Confirmation Link: that
// session is minted from a token in an email somebody was SENT, it does not mark
// the Customer verified, and it proves nothing about who controls the address. A
// Follow is a standing request to be written to (ADR 0030), so honouring one
// from a forwarded receipt would let a stranger subscribe another person's inbox
// — precisely what ADR 0010 forbids.
//
// The read is refused alongside the writes: what a person Follows is a standing
// statement of their interests, and a forwarded confirmation is not authority to
// see it.
func TestAConfirmationLinkSessionCannotFollowAnything(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Link Fest", "link-fest",
		env.fixedClock.Add(60*24*time.Hour), "follow-link-1", "ana@example.com", "Ana", "Lopez")

	_, saleScoped := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")

	// The same session can still do what a Confirmation Link is for — read its
	// one sale — so this is a narrowing of these three routes and not a dead
	// credential.
	if area := readCustomerArea(t, env, saleScoped, ""); len(area.Upcoming) != 1 {
		t.Fatalf("sale-scoped session sees %d upcoming sales, want its one", len(area.Upcoming))
	}

	resp, body := followOrganization(t, env, saleScoped, "test-org")
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	resp, body = unfollowOrganization(t, env, saleScoped, "test-org")
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	resp, body = env.get(t, customerFollowsPath, authHeader(saleScoped))
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	if n := countOrganizationFollows(t, env); n != 0 {
		t.Fatalf("%d Follow rows after a sale-scoped session tried, want 0", n)
	}

	// The same person, having proved they own the address, may follow freely.
	full := customerSignIn(t, env, "ana@example.com")
	followOrganizationOK(t, env, full, "test-org")
}

// TestDeletingAnOrganizationRemovesItsFollows is the cascade the acceptance
// criteria ask for, and it matters beyond tidiness: these rows are an input to a
// mailing, and the one thing a mailing must never do is send about something
// that no longer exists.
func TestDeletingAnOrganizationRemovesItsFollows(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	createOrganization(t, env, sessionID, "Other Org", "other-org")
	token := customerSignIn(t, env, "ana@example.com")

	followOrganizationOK(t, env, token, "test-org")
	followOrganizationOK(t, env, token, "other-org")

	// The Org Admin's active Organization is the last one they created, so the
	// delete below removes "Other Org" and must leave the other Follow standing.
	resp, body := env.deleteJSON(t, "/api/v1/staff/organization", map[string]string{
		"confirmation_name": "Other Org",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete organization status=%d error=%+v", resp.StatusCode, body.Error)
	}

	got := followedSlugs(t, listFollows(t, env, token))
	if len(got) != 1 || got[0] != "test-org" {
		t.Fatalf("follows after deleting Other Org = %v, want [test-org]", got)
	}
	if n := countOrganizationFollows(t, env); n != 1 {
		t.Fatalf("%d Follow rows survive, want 1 — the deleted Organization's Follow must go with it, not be orphaned", n)
	}
}

// TestAStaffSessionCannotFollowAnything keeps the two identities apart
// (ADR 0010). A Staff Session token is not a weaker Customer Session; it is a
// record on an unrelated table that these routes cannot even see.
func TestAStaffSessionCannotFollowAnything(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := followOrganization(t, env, sessionID, "test-org")
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")

	resp, body = env.get(t, customerFollowsPath, authHeader(sessionID))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")
}

// TestFollowReportsTheOrganizationsLogo proves the listing carries what a
// control needs to render an Organization without a second call per entry.
func TestFollowReportsTheOrganizationsLogo(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/organization/logo-upload-url", map[string]string{
		"content_type": "image/png",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logo upload url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var presign struct {
		ObjectKey string `json:"object_key"`
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(body.Data, &presign); err != nil {
		t.Fatalf("decode presign: %v", err)
	}
	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]any{
		"name":           "Test Org",
		"logo_image_key": presign.ObjectKey,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch logo status=%d error=%+v", resp.StatusCode, body.Error)
	}

	token := customerSignIn(t, env, "ana@example.com")
	follow := followOrganizationOK(t, env, token, "test-org")
	if follow.Organization.LogoURL == nil || *follow.Organization.LogoURL != presign.PublicURL {
		t.Fatalf("logo_url = %v, want %q", follow.Organization.LogoURL, presign.PublicURL)
	}

	listed := listFollows(t, env, token)
	if listed.Follows[0].Organization.LogoURL == nil ||
		*listed.Follows[0].Organization.LogoURL != presign.PublicURL {
		t.Fatalf("listing logo_url = %v, want %q", listed.Follows[0].Organization.LogoURL, presign.PublicURL)
	}
}
