package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
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

// followedTag is the Tag as a Follow reports it: the same three facts every
// other Tag surface publishes (catalog's TagView), and no internal id. The Tag
// is named by its canonical key, symmetrically with the Organization being named
// by its slug — the stable machine identity a Storefront already holds, and the
// one a copy edit to the display name cannot break (ADR 0027).
type followedTag struct {
	CanonicalKey string `json:"canonical_key"`
	Name         string `json:"name"`
	Curated      bool   `json:"curated"`
}

// followView is one entry in the Customer's Follows. `type` is the
// discriminator; the subject hangs off the field it names, so an Organization
// Follow carries `organization` and a Tag Follow carries `tag`, each absent on
// the other (#218).
type followView struct {
	Type         string                `json:"type"`
	FollowedAt   time.Time             `json:"followed_at"`
	Organization *followedOrganization `json:"organization"`
	Tag          *followedTag          `json:"tag"`
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
// the API returned them. It is for the Organization-only scenarios and insists
// on that: a Tag arriving in a list where none was followed would be a leak, not
// a widening. Mixed listings go through followedKeys below.
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

// ---------------------------------------------------------------------------
// Tag Follows (#218, parent #215).
//
// The second kind of Follow, and deliberately not a second feature. It joins the
// same list under its own `type`, keys on the canonical key exactly as the
// Organization keys on its slug, and answers with the same statuses — so most of
// what follows is the Organization's own suite read against a different subject,
// which is the promise rather than duplication.
//
// ANY Tag is followable, Preset and Custom alike. ADR 0030 records why: per
// reader volume is bounded by the weekly Digest's cap, not by narrowing what may
// be Followed, so restricting the pool to `curated` Tags would cost the most
// valuable case — a narrow interest — to solve a problem the Digest has already
// solved.
// ---------------------------------------------------------------------------

// followTagPath addresses a Tag by its canonical key, path-escaped. Half the
// Preset pool contains a space and several an ampersand, so the escaping is part
// of the address rather than a nicety.
func followTagPath(canonicalKey string) string {
	return customerFollowsPath + "/tags/" + url.PathEscape(canonicalKey)
}

func followTag(t *testing.T, env *testEnv, token, canonicalKey string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, followTagPath(canonicalKey), nil, authHeader(token))
}

func followTagOK(t *testing.T, env *testEnv, token, canonicalKey string) followView {
	t.Helper()
	resp, body := followTag(t, env, token, canonicalKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("follow tag %q status=%d error=%+v", canonicalKey, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("follow tag %q error=%+v, want none", canonicalKey, body.Error)
	}
	var view followView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode tag follow: %v", err)
	}
	return view
}

func unfollowTag(t *testing.T, env *testEnv, token, canonicalKey string) (*http.Response, envelope) {
	t.Helper()
	return env.deleteJSON(t, followTagPath(canonicalKey), nil, authHeader(token))
}

func unfollowTagOK(t *testing.T, env *testEnv, token, canonicalKey string) {
	t.Helper()
	resp, body := unfollowTag(t, env, token, canonicalKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unfollow tag %q status=%d error=%+v", canonicalKey, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("unfollow tag %q error=%+v, want none", canonicalKey, body.Error)
	}
}

// countTagFollows reads the table directly, for the reason
// countOrganizationFollows does: idempotency is a claim about ROWS, and the API
// cannot show a second Follow of one Tag however many exist.
func countTagFollows(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customer_tag_follows`).Scan(&n); err != nil {
		t.Fatalf("count tag follows: %v", err)
	}
	return n
}

// followedKeys flattens a MIXED listing to one string per entry, prefixed by its
// kind: "organization:test-org", "tag:music".
//
// The prefix is what makes the assertion worth making. The whole promise of the
// combined list is that a client tells the kinds apart from `type` alone and
// reaches the subject through the field that names it, so this also insists the
// other kind's field is absent — an entry carrying both would typecheck on the
// wire and be meaningless.
func followedKeys(t *testing.T, view followsView) []string {
	t.Helper()
	keys := make([]string, 0, len(view.Follows))
	for _, follow := range view.Follows {
		switch follow.Type {
		case "organization":
			if follow.Organization == nil {
				t.Fatalf("follow of type organization carries no organization: %+v", follow)
			}
			if follow.Tag != nil {
				t.Fatalf("organization Follow also carries a tag: %+v", follow)
			}
			keys = append(keys, "organization:"+follow.Organization.Slug)
		case "tag":
			if follow.Tag == nil {
				t.Fatalf("follow of type tag carries no tag: %+v", follow)
			}
			if follow.Organization != nil {
				t.Fatalf("tag Follow also carries an organization: %+v", follow)
			}
			keys = append(keys, "tag:"+follow.Tag.CanonicalKey)
		default:
			t.Fatalf("follow type = %q, want organization or tag", follow.Type)
		}
	}
	return keys
}

// coinCustomTag creates an Event and tags it, which is the only way a Custom Tag
// enters the shared pool: they are coined mid-edit by an Organization rather
// than administered (ADR 0004). Returns the canonical key the pool stored.
func coinCustomTag(t *testing.T, env *testEnv, sessionID, name string) string {
	t.Helper()
	eventID := publishEvent(t, env, sessionID, "Tagged Night", "tagged-night",
		env.fixedClock.Add(30*24*time.Hour), true, 2500, 100)

	resp, body := setEventTags(t, env, sessionID, eventID, []string{name})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set event tags status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, tag := range decodeTags(t, body.Data) {
		if !tag.Curated {
			return tag.CanonicalKey
		}
	}
	t.Fatalf("no Custom Tag coined by %q", name)
	return ""
}

// TestCustomerFollowsAPresetTagAndItAppearsInTheirFollows is the Tag half of the
// feature in one pass: press Follow on a Tag chip, and the Customer's Follows
// say so — in the same list the Organization Follows are in.
func TestCustomerFollowsAPresetTagAndItAppearsInTheirFollows(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	follow := followTagOK(t, env, token, "music")
	if follow.Type != "tag" {
		t.Fatalf("type = %q, want tag", follow.Type)
	}
	if follow.Tag == nil || follow.Tag.CanonicalKey != "music" || follow.Tag.Name != "Music" {
		t.Fatalf("tag = %+v, want Music / music", follow.Tag)
	}
	// `curated` is on the wire because the Storefront needs it to decide whether
	// to word the Tag from its own catalogue or render it as coined (ADR 0027).
	if !follow.Tag.Curated {
		t.Fatal("music is a Preset Tag and must report curated = true")
	}
	if follow.FollowedAt.IsZero() {
		t.Fatal("followed_at is zero — a Follow must record when it was made")
	}

	if got := followedKeys(t, listFollows(t, env, token)); len(got) != 1 || got[0] != "tag:music" {
		t.Fatalf("follows = %v, want [tag:music]", got)
	}
}

// TestCustomerFollowsACustomTag is ADR 0030's decision stated as a test.
//
// The obvious safety valve for a discovery feature that mails people would have
// been to allow only `curated` Tags to be Followed; the ADR rejects it, because
// the weekly Digest already bounds volume and the narrow interest is the case
// worth having. A future reader who "tightens" this will fail here.
func TestCustomerFollowsACustomTag(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	key := coinCustomTag(t, env, sessionID, "Warehouse Techno")
	token := customerSignIn(t, env, "ana@example.com")

	follow := followTagOK(t, env, token, key)
	if follow.Tag == nil || follow.Tag.CanonicalKey != key {
		t.Fatalf("tag = %+v, want canonical key %q", follow.Tag, key)
	}
	if follow.Tag.Curated {
		t.Fatalf("tag %q is a Custom Tag and must report curated = false", key)
	}
	if follow.Tag.Name != "Warehouse Techno" {
		t.Fatalf("tag name = %q, want the display casing the Organization coined", follow.Tag.Name)
	}

	if got := followedKeys(t, listFollows(t, env, token)); len(got) != 1 || got[0] != "tag:"+key {
		t.Fatalf("follows = %v, want the Custom Tag", got)
	}
}

// TestFollowingATagWhoseKeyNeedsEncoding covers the canonical keys that are not
// URL-safe. "arts & theatre" is a seeded Preset Tag and one of the chips a
// reader is most likely to press, so a path that survived only the tidy keys
// would break in the ordinary case rather than an exotic one.
func TestFollowingATagWhoseKeyNeedsEncoding(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	follow := followTagOK(t, env, token, "arts & theatre")
	if follow.Tag == nil || follow.Tag.CanonicalKey != "arts & theatre" {
		t.Fatalf("tag = %+v, want canonical key %q", follow.Tag, "arts & theatre")
	}

	unfollowTagOK(t, env, token, "arts & theatre")
	if n := countTagFollows(t, env); n != 0 {
		t.Fatalf("%d Tag Follow rows after unfollow, want 0", n)
	}
}

// TestFollowingATagTwiceLeavesOneFollowAndDoesNotMoveWhenItWasMade is the
// Organization's idempotency rule again, unchanged, because a Customer cannot be
// expected to learn that one control is safe to double-tap and the other is not.
func TestFollowingATagTwiceLeavesOneFollowAndDoesNotMoveWhenItWasMade(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	first := followTagOK(t, env, token, "music")

	setCustomerClock(t, env, env.fixedClock.Add(48*time.Hour))
	second := followTagOK(t, env, token, "music")

	if !second.FollowedAt.Equal(first.FollowedAt) {
		t.Fatalf("repeat follow moved followed_at from %v to %v", first.FollowedAt, second.FollowedAt)
	}
	if n := countTagFollows(t, env); n != 1 {
		t.Fatalf("%d Tag Follow rows after following twice, want exactly 1", n)
	}
}

// TestUnfollowingATagNotFollowedIsNotAnError mirrors the Organization's rule:
// the caller asked for a state, and that state already holds.
func TestUnfollowingATagNotFollowedIsNotAnError(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	unfollowTagOK(t, env, token, "music")
	unfollowTagOK(t, env, token, "music")

	if n := countTagFollows(t, env); n != 0 {
		t.Fatalf("%d Tag Follow rows, want 0", n)
	}
}

// TestFollowingAnUnknownTagIs404 keeps the endpoint from coining Tags. A Tag is
// coined by an Organization tagging an Event, never by a Customer following a
// word — otherwise the shared pool would fill with typos nothing displays, and
// the Digest would carry Follows of Tags no Event can ever match.
func TestFollowingAnUnknownTagIs404(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	resp, body := followTag(t, env, token, "not-a-real-tag")
	assertAPIError(t, resp, body, http.StatusNotFound, "TAG_NOT_FOUND")

	resp, body = unfollowTag(t, env, token, "not-a-real-tag")
	assertAPIError(t, resp, body, http.StatusNotFound, "TAG_NOT_FOUND")

	if n := countTagFollows(t, env); n != 0 {
		t.Fatalf("%d Tag Follow rows, want 0 — following must not coin a Tag", n)
	}
}

// TestFollowingATagIsCanonicalizedLikeTheRestOfThePool pins that the path
// segment is a CANONICAL key and is canonicalized on the way in, exactly as
// every other Tag surface canonicalizes: lowercased, spaces collapsed. The same
// Tag reached two ways is one Follow rather than two, which only the row count
// can show.
func TestFollowingATagIsCanonicalizedLikeTheRestOfThePool(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	followTagOK(t, env, token, "music")
	follow := followTagOK(t, env, token, "  MUSIC ")

	if follow.Tag == nil || follow.Tag.CanonicalKey != "music" {
		t.Fatalf("tag = %+v, want the canonical key %q", follow.Tag, "music")
	}
	if n := countTagFollows(t, env); n != 1 {
		t.Fatalf("%d Tag Follow rows, want 1 — the same Tag reached two ways is one Follow", n)
	}
}

// TestFollowsListCombinesBothKindsInOneOrderedList is the acceptance criterion
// that both kinds come back from ONE listing rather than two, and it asserts the
// harder half of it: the ORDER is one order over the union, not Organizations
// and then Tags. A client renders this list top to bottom, so the interleaving
// is the contract.
func TestFollowsListCombinesBothKindsInOneOrderedList(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	createOrganization(t, env, sessionID, "Other Org", "other-org")
	token := customerSignIn(t, env, "ana@example.com")

	// Four Follows, alternating kind, each an hour after the last — so a listing
	// that grouped by kind rather than ordering by instant is visibly wrong.
	followOrganizationOK(t, env, token, "test-org")
	setCustomerClock(t, env, env.fixedClock.Add(time.Hour))
	followTagOK(t, env, token, "music")
	setCustomerClock(t, env, env.fixedClock.Add(2*time.Hour))
	followOrganizationOK(t, env, token, "other-org")
	setCustomerClock(t, env, env.fixedClock.Add(3*time.Hour))
	followTagOK(t, env, token, "comedy")

	got := followedKeys(t, listFollows(t, env, token))
	want := []string{"tag:comedy", "organization:other-org", "tag:music", "organization:test-org"}
	if len(got) != len(want) {
		t.Fatalf("follows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("follows = %v, want %v — most recently followed first, across both kinds", got, want)
		}
	}
}

// TestFollowsListOrderIsTotalWhenBothKindsShareAnInstant guards the tie-break.
//
// Two Follows made in one instant are ordinary here — the fixed clock makes them
// the default — and a listing ordered on `followed_at` alone could hand back two
// different orders for two identical reads, which a client re-rendering the
// Following list would show as a shuffle.
func TestFollowsListOrderIsTotalWhenBothKindsShareAnInstant(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	createOrganization(t, env, sessionID, "Other Org", "other-org")
	token := customerSignIn(t, env, "ana@example.com")

	// No clock movement between any of these: one instant, four Follows.
	followTagOK(t, env, token, "music")
	followOrganizationOK(t, env, token, "test-org")
	followTagOK(t, env, token, "comedy")
	followOrganizationOK(t, env, token, "other-org")

	first := followedKeys(t, listFollows(t, env, token))
	if len(first) != 4 {
		t.Fatalf("follows = %v, want four entries", first)
	}
	for i := 0; i < 3; i++ {
		again := followedKeys(t, listFollows(t, env, token))
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("listing reordered between reads: %v then %v", first, again)
			}
		}
	}
}

// TestUnfollowingOneKindLeavesTheOtherStanding is the management surface's rule.
// The Following list holds both kinds, unfollow is available there, and removing
// one entry removes exactly that entry — ending, when the last one goes, at the
// same well-formed empty list a Customer who follows nothing sees.
func TestUnfollowingOneKindLeavesTheOtherStanding(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)
	token := customerSignIn(t, env, "ana@example.com")

	followOrganizationOK(t, env, token, "test-org")
	followTagOK(t, env, token, "music")

	unfollowTagOK(t, env, token, "music")

	got := followedKeys(t, listFollows(t, env, token))
	if len(got) != 1 || got[0] != "organization:test-org" {
		t.Fatalf("follows after unfollowing the Tag = %v, want [organization:test-org]", got)
	}

	unfollowOrganizationOK(t, env, token, "test-org")
	if got := followedKeys(t, listFollows(t, env, token)); len(got) != 0 {
		t.Fatalf("follows = %v, want none", got)
	}
}

// TestOneCustomersTagFollowsAreNeverAnothers is the adversarial read again, for
// the kind just added. The listing takes no identifier, so only a scoping bug
// could widen it.
func TestOneCustomersTagFollowsAreNeverAnothers(t *testing.T) {
	env := setupTest(t)
	_ = orgAdminSession(t, env)

	ana := customerSignIn(t, env, "ana@example.com")
	bruno := customerSignIn(t, env, "bruno@example.com")

	followTagOK(t, env, ana, "music")
	followTagOK(t, env, bruno, "comedy")

	if got := followedKeys(t, listFollows(t, env, ana)); len(got) != 1 || got[0] != "tag:music" {
		t.Fatalf("ana's follows = %v, want [tag:music] alone", got)
	}
	if got := followedKeys(t, listFollows(t, env, bruno)); len(got) != 1 || got[0] != "tag:comedy" {
		t.Fatalf("bruno's follows = %v, want [tag:comedy] alone", got)
	}

	// And one cannot unfollow on the other's behalf: the delete is scoped by
	// session too, so this removes nothing.
	unfollowTagOK(t, env, bruno, "music")
	if got := followedKeys(t, listFollows(t, env, ana)); len(got) != 1 || got[0] != "tag:music" {
		t.Fatalf("ana's follows after bruno unfollowed music = %v, want [tag:music] untouched", got)
	}
}

// TestTagFollowRoutesRefuseAnUnauthenticatedRequest covers the new pair with the
// two ways a browser actually arrives without a session.
func TestTagFollowRoutesRefuseAnUnauthenticatedRequest(t *testing.T) {
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

		resp, body := env.post(t, followTagPath("music"), nil, headers)
		assertAPIError(t, resp, body, http.StatusUnauthorized, credential.code)

		resp, body = env.deleteJSON(t, followTagPath("music"), nil, headers)
		assertAPIError(t, resp, body, http.StatusUnauthorized, credential.code)

		if n := countTagFollows(t, env); n != 0 {
			t.Fatalf("%d Tag Follow rows after unauthenticated calls with %s, want 0", n, credential.what)
		}
	}
}

// TestAConfirmationLinkSessionCannotFollowATag applies #217's settled session
// rule to the new routes without restating its reasoning: a sale-scoped session
// is minted from a forwarded receipt and proves nothing about who owns the
// address, so it may not subscribe that address to mail.
func TestAConfirmationLinkSessionCannotFollowATag(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Link Fest", "link-fest",
		env.fixedClock.Add(60*24*time.Hour), "follow-tag-link-1", "ana@example.com", "Ana", "Lopez")

	_, saleScoped := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")

	resp, body := followTag(t, env, saleScoped, "music")
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	resp, body = unfollowTag(t, env, saleScoped, "music")
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	if n := countTagFollows(t, env); n != 0 {
		t.Fatalf("%d Tag Follow rows after a sale-scoped session tried, want 0", n)
	}

	// The same person, having proved they own the address, may follow freely.
	full := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, full, "music")
}

// TestAStaffSessionCannotFollowATag keeps the two identities apart (ADR 0010).
func TestAStaffSessionCannotFollowATag(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := followTag(t, env, sessionID, "music")
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")
}

// TestDeletingATagRemovesItsFollows is the cascade, and it is why the Follow is
// stored against the Tag's id rather than against its canonical key. A Follow of
// a Tag that no longer exists is an input to a mailing that can match nothing,
// and it would survive forever because nothing else would ever look at it.
//
// SQL for the delete: there is no API that removes a Tag — the pool is shared
// and nothing administers it (ADR 0004) — so the schema is the only place this
// rule can be stated, and the only place it can be read.
func TestDeletingATagRemovesItsFollows(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	key := coinCustomTag(t, env, sessionID, "Warehouse Techno")
	token := customerSignIn(t, env, "ana@example.com")

	followTagOK(t, env, token, key)
	followTagOK(t, env, token, "music")

	if _, err := env.db.Exec(`DELETE FROM tags WHERE canonical_key = $1`, key); err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	got := followedKeys(t, listFollows(t, env, token))
	if len(got) != 1 || got[0] != "tag:music" {
		t.Fatalf("follows after deleting the Custom Tag = %v, want [tag:music]", got)
	}
	if n := countTagFollows(t, env); n != 1 {
		t.Fatalf("%d Tag Follow rows survive, want 1 — the deleted Tag's Follow must go with it", n)
	}
}
