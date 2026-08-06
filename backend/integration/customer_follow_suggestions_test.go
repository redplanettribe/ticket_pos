package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Suggested Follows (#231, parent #229, ADR 0031): Tags and Organizations a
// Customer does not Follow, offered to them beneath the ones they do.
//
// This ticket ranks on ACTIVITY alone — the count of discoverable upcoming
// Events carrying a Tag or run by an Organization. Co-occurrence is #232 and
// derived Tags are #233, so every suggestion here carries a null reason; the
// tests say so explicitly rather than ignoring the field, because a reason
// appearing before #232 lands would mean the panel had started guessing.
//
// Activity measures SUPPLY and never audience. Nothing below counts Followers,
// and nothing below could: no such count is computed, stored or exposed
// anywhere in the platform (CONTEXT.md, ADR 0030).
//
// This is the one seam the spec agreed on. Everything the ranking does is
// observable from outside — what appears, what does not, and in what order — so
// there is no repository test beside these; docs/testing.md admits one only as a
// narrow exception paired with an HTTP test proving the user-visible outcome,
// and here the HTTP test proves it alone.
//
// Prior art: customer_follows_test.go for the session helpers and the Follow
// writes, follow_digest_test.go for arranging tagged Events across two
// Organizations.

const customerFollowSuggestionsPath = "/api/v1/customer/follow-suggestions"

// suggestionReason is why a suggestion is being made: the Tag that produced it,
// named by canonical key and never as a sentence, so the Storefront words it
// from its own catalogues as it words every other Tag (ADR 0027).
//
// It is null throughout this ticket. Activity ranking has no producing Tag to
// name — the subject is offered because things are happening under it, not
// because of anything the Customer Follows.
type suggestionReason struct {
	TagCanonicalKey string `json:"tag_canonical_key"`
}

// suggestedTag is one Tag being offered. The subject is the SAME shape the
// Follows listing publishes (followedTag), which is the promise: a suggested Tag
// and a followed Tag are one type on the far side.
type suggestedTag struct {
	Tag    followedTag       `json:"tag"`
	Reason *suggestionReason `json:"reason"`
}

type suggestedOrganization struct {
	Organization followedOrganization `json:"organization"`
	Reason       *suggestionReason    `json:"reason"`
}

// followSuggestionsView is the whole panel: TWO GROUPS, never one interleaved
// list. A Tag's ranking and an Organization's are different units, and putting
// them in one order would publish a comparability that does not exist (ADR 0031).
type followSuggestionsView struct {
	Tags          []suggestedTag          `json:"tags"`
	Organizations []suggestedOrganization `json:"organizations"`
}

func readFollowSuggestions(t *testing.T, env *testEnv, token string) followSuggestionsView {
	t.Helper()
	resp, body := env.get(t, customerFollowSuggestionsPath, authHeader(token))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read suggestions status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("read suggestions error=%+v, want none", body.Error)
	}
	var view followSuggestionsView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode suggestions: %v", err)
	}
	return view
}

// suggestedTagKeys flattens the Tag group to canonical keys IN THE ORDER THE API
// RETURNED THEM, and insists on the way each entry claims its place: no reason
// before #232, and no entry without a subject.
func suggestedTagKeys(t *testing.T, view followSuggestionsView) []string {
	t.Helper()
	keys := make([]string, 0, len(view.Tags))
	for _, suggestion := range view.Tags {
		if suggestion.Tag.CanonicalKey == "" {
			t.Fatalf("suggested tag carries no canonical key: %+v", suggestion)
		}
		if suggestion.Reason != nil {
			t.Fatalf("suggested tag %q carries reason %+v — Activity ranking has no producing Tag to name (#232 adds one)",
				suggestion.Tag.CanonicalKey, suggestion.Reason)
		}
		keys = append(keys, suggestion.Tag.CanonicalKey)
	}
	return keys
}

// suggestedOrganizationSlugs is the Organization group's counterpart, keyed on
// slug for the reason the Tag group keys on canonical key: it is the identifier
// each kind is addressed by, on the API and in the Storefront's own addresses.
func suggestedOrganizationSlugs(t *testing.T, view followSuggestionsView) []string {
	t.Helper()
	slugs := make([]string, 0, len(view.Organizations))
	for _, suggestion := range view.Organizations {
		if suggestion.Organization.Slug == "" {
			t.Fatalf("suggested organization carries no slug: %+v", suggestion)
		}
		if suggestion.Reason != nil {
			t.Fatalf("suggested organization %q carries reason %+v — Activity ranking has no producing Tag to name",
				suggestion.Organization.Slug, suggestion.Reason)
		}
		slugs = append(slugs, suggestion.Organization.Slug)
	}
	return slugs
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestFollowSuggestionsRankTagsAndOrganizationsByActivity is the ticket in one
// pass: two Organizations, four upcoming Events between them, and a panel that
// puts the busiest subject of each kind first.
//
// Activity is a count of EVENTS. "Nightlife" outranks "Comedy" because three
// upcoming Events carry it and one carries the other — not because anybody
// Follows it, which nothing on this platform records a count of.
func TestFollowSuggestionsRankTagsAndOrganizationsByActivity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	// test-org: two upcoming Events, both carrying Nightlife.
	first := discoverableEvent(t, env, sessionID, "Late Set", "late-set", upcoming)
	setEventTagsOK(t, env, sessionID, first, []string{"Nightlife"})
	second := discoverableEvent(t, env, sessionID, "Later Set", "later-set", upcoming)
	setEventTagsOK(t, env, sessionID, second, []string{"Nightlife"})

	// other-org: one upcoming Event carrying both, so Nightlife reaches three
	// and Comedy one. Two Organizations, two Events and one.
	third := discoverableEvent(t, env, other, "Stand Up", "stand-up", upcoming)
	setEventTagsOK(t, env, other, third, []string{"Nightlife", "Comedy"})

	token := customerSignIn(t, env, "ana@example.com")
	view := readFollowSuggestions(t, env, token)

	if got := suggestedTagKeys(t, view); !equalStrings(got, []string{"nightlife", "comedy"}) {
		t.Fatalf("suggested tags = %v, want [nightlife comedy] — three upcoming Events to one", got)
	}
	if got := suggestedOrganizationSlugs(t, view); !equalStrings(got, []string{testOrgSlug, "other-org"}) {
		t.Fatalf("suggested organizations = %v, want [%s other-org] — two upcoming Events to one", got, testOrgSlug)
	}

	// The subject is the Follows listing's own shape, filled in: a client that
	// can draw a followed Organization can draw a suggested one without learning
	// a second type.
	if view.Organizations[0].Organization.Name != "Test Org" {
		t.Fatalf("suggested organization = %+v, want the Organization's public name", view.Organizations[0].Organization)
	}
	if !view.Tags[0].Tag.Curated {
		t.Fatalf("Nightlife reports curated=%v, want true — it is a Preset Tag", view.Tags[0].Tag.Curated)
	}
}

// TestFollowSuggestionsExcludeWhatTheCustomerAlreadyFollows is the panel's most
// basic obligation. Offering somebody what they already have is the one failure
// that makes the whole feature look like it is not reading their account, and it
// is the failure a ranking query gets for free by forgetting one NOT EXISTS.
func TestFollowSuggestionsExcludeWhatTheCustomerAlreadyFollows(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	mine := discoverableEvent(t, env, sessionID, "Jazz Night", "jazz-night", upcoming)
	setEventTagsOK(t, env, sessionID, mine, []string{"Music"})
	theirs := discoverableEvent(t, env, other, "Film Club", "film-club", upcoming)
	setEventTagsOK(t, env, other, theirs, []string{"Film"})

	token := customerSignIn(t, env, "ana@example.com")

	// Before following anything, both of each kind are on offer.
	before := readFollowSuggestions(t, env, token)
	if got := suggestedTagKeys(t, before); len(got) != 2 {
		t.Fatalf("suggested tags before following = %v, want both", got)
	}
	if got := suggestedOrganizationSlugs(t, before); len(got) != 2 {
		t.Fatalf("suggested organizations before following = %v, want both", got)
	}

	followTagOK(t, env, token, "music")
	followOrganizationOK(t, env, token, testOrgSlug)

	after := readFollowSuggestions(t, env, token)
	if got := suggestedTagKeys(t, after); !equalStrings(got, []string{"film"}) {
		t.Fatalf("suggested tags after following music = %v, want [film]", got)
	}
	if got := suggestedOrganizationSlugs(t, after); !equalStrings(got, []string{"other-org"}) {
		t.Fatalf("suggested organizations after following %s = %v, want [other-org]", testOrgSlug, got)
	}

	// Another Customer's Follows are not this one's. The exclusion is scoped by
	// the session and by nothing in the request, so a busy account cannot empty
	// somebody else's panel.
	bob := customerSignIn(t, env, "bob@example.com")
	if got := suggestedTagKeys(t, readFollowSuggestions(t, env, bob)); len(got) != 2 {
		t.Fatalf("second Customer's suggested tags = %v, want both — the first Customer's Follows are not theirs", got)
	}
}

// TestFollowSuggestionsNeedASecondEventForACustomTagButNotForAPreset is the
// floor ADR 0031 puts under Custom Tags, and the asymmetry is the point.
//
// A Custom Tag on exactly one Event is usually that Event's own name — an
// Organization types the name of its festival into the Tag field — and offering
// an Event's name back as if it were a category is how the panel looks
// unserious. A Preset Tag cannot be an Event's name: it comes from a curated
// pool that predates every Event, so one Event is enough for it.
func TestFollowSuggestionsNeedASecondEventForACustomTagButNotForAPreset(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	// "Cumbia" on one Event, and a Preset Tag beside it on the same Event.
	only := discoverableEvent(t, env, sessionID, "Cumbia Fest", "cumbia-fest", upcoming)
	setEventTagsOK(t, env, sessionID, only, []string{"Cumbia", "Music"})

	token := customerSignIn(t, env, "ana@example.com")
	if got := suggestedTagKeys(t, readFollowSuggestions(t, env, token)); !equalStrings(got, []string{"music"}) {
		t.Fatalf("suggested tags = %v, want [music] — a Custom Tag on one Event is usually that Event's own name", got)
	}

	// A second Event carries it, so it is a category somebody used twice rather
	// than one Event's name, and it qualifies.
	second := discoverableEvent(t, env, sessionID, "Cumbia Again", "cumbia-again", upcoming)
	setEventTagsOK(t, env, sessionID, second, []string{"Cumbia"})

	// Two Events to Music's one, so it also leads the order.
	if got := suggestedTagKeys(t, readFollowSuggestions(t, env, token)); !equalStrings(got, []string{"cumbia", "music"}) {
		t.Fatalf("suggested tags = %v, want [cumbia music] — a Custom Tag on two Events qualifies", got)
	}
}

// TestFollowSuggestionsSkipAnOrganizationWithNothingUpcoming keeps the panel
// from offering a dead end. Following an Organization with no upcoming
// discoverable Event sends the Customer nothing, ever — the entire payload of a
// Follow is the weekly Follow Digest, and there is nothing for it to carry.
func TestFollowSuggestionsSkipAnOrganizationWithNothingUpcoming(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// other-org exists, has a Member and a public page, and never publishes.
	_ = otherOrganizationSession(t, env)

	discoverableEvent(t, env, sessionID, "Live Set", "live-set", env.fixedClock.Add(20*24*time.Hour))

	token := customerSignIn(t, env, "ana@example.com")
	if got := suggestedOrganizationSlugs(t, readFollowSuggestions(t, env, token)); !equalStrings(got, []string{testOrgSlug}) {
		t.Fatalf("suggested organizations = %v, want [%s] — an Organization with nothing upcoming is a dead end", got, testOrgSlug)
	}
}

// TestFollowSuggestionsCountOnlyDiscoverableUpcomingEvents pins Activity to
// exactly the subset the explorer lists: published, discoverable, and not yet
// over.
//
// Anything looser would rank on Events the Customer cannot reach — a draft
// nobody has finished, an unlisted Event its Organization deliberately kept off
// the explorer, a gig that already happened — and would then have Followed
// subjects produce nothing in the Digest, which is the only promise a Follow
// makes.
func TestFollowSuggestionsCountOnlyDiscoverableUpcomingEvents(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)
	past := env.fixedClock.Add(-10 * 24 * time.Hour)

	// The one Event that counts, and the only Tag that should survive.
	counted := discoverableEvent(t, env, sessionID, "Counted", "counted", upcoming)
	setEventTagsOK(t, env, sessionID, counted, []string{"Music"})

	// Three Events under the other Organization, each disqualified in its own
	// way. It runs three "Events" and must still not be suggested, which is what
	// makes this the control rather than three separate assertions.
	unlisted := publishEvent(t, env, other, "Unlisted", "unlisted", upcoming, false, 1000, 50)
	setEventTagsOK(t, env, other, unlisted, []string{"Nightlife"})
	over := publishEvent(t, env, other, "Already Over", "already-over", past, true, 1000, 50)
	setEventTagsOK(t, env, other, over, []string{"Festival"})
	unpublished := createDraftEvent(t, env, other, "Still A Draft", "still-a-draft")
	setEventTagsOK(t, env, other, unpublished, []string{"Workshop"})

	token := customerSignIn(t, env, "ana@example.com")
	view := readFollowSuggestions(t, env, token)

	if got := suggestedTagKeys(t, view); !equalStrings(got, []string{"music"}) {
		t.Fatalf("suggested tags = %v, want [music] — an unlisted, a finished and an unpublished Event count for nothing", got)
	}
	if got := suggestedOrganizationSlugs(t, view); !equalStrings(got, []string{testOrgSlug}) {
		t.Fatalf("suggested organizations = %v, want [%s] — three disqualified Events are no Activity at all", got, testOrgSlug)
	}
}

// TestFollowSuggestionsAreEmptyRatherThanAnErrorWhenNothingQualifies is the
// heavy user's case and the empty catalogue's, which are the same response.
//
// A 200 with empty groups rather than a 404 or an error, because the Storefront
// reads this beside the Follows listing and hides the panel on empty: a failure
// here degrades to no panel silently, so an empty answer arriving AS a failure
// would be indistinguishable from a broken read and would hide nothing extra
// while making every log lie.
func TestFollowSuggestionsAreEmptyRatherThanAnErrorWhenNothingQualifies(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	// A catalogue with nothing in it at all.
	token := customerSignIn(t, env, "ana@example.com")
	empty := readFollowSuggestions(t, env, token)
	if len(empty.Tags) != 0 || len(empty.Organizations) != 0 {
		t.Fatalf("suggestions over an empty catalogue = %+v, want both groups empty", empty)
	}

	// And a Customer who Follows everything that qualifies, which is the same
	// answer reached from the other direction.
	event := discoverableEvent(t, env, sessionID, "Only Gig", "only-gig", upcoming)
	setEventTagsOK(t, env, sessionID, event, []string{"Music"})
	followTagOK(t, env, token, "music")
	followOrganizationOK(t, env, token, testOrgSlug)

	exhausted := readFollowSuggestions(t, env, token)
	if len(exhausted.Tags) != 0 || len(exhausted.Organizations) != 0 {
		t.Fatalf("suggestions for a Customer following everything = %+v, want both groups empty", exhausted)
	}
}

// TestFollowSuggestionsRefuseWithoutAFullCustomerSession draws the same line the
// Follows listing draws, and for the same reason stated one step further on.
//
// What a person is SUGGESTED is derived from what they Follow, which is a
// standing statement of their interests. A Confirmation Link session is
// possession of an email somebody was sent and proves nothing about who controls
// the address, so it is not authority to read what the platform infers about its
// owner any more than it is authority to read the Follows themselves.
func TestFollowSuggestionsRefuseWithoutAFullCustomerSession(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

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
		t.Logf("reading suggestions with %s", credential.what)
		resp, body := env.get(t, customerFollowSuggestionsPath, headers)
		assertAPIError(t, resp, body, http.StatusUnauthorized, credential.code)
	}

	seedSaleForCustomer(t, env, sessionID, "Link Fest", "link-fest",
		env.fixedClock.Add(60*24*time.Hour), "suggest-link-1", "ana@example.com", "Ana", "Lopez")
	_, saleScoped := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")

	// The same session can still do what a Confirmation Link is for, so this is a
	// narrowing of one route and not a dead credential.
	if area := readCustomerArea(t, env, saleScoped, ""); len(area.Upcoming) != 1 {
		t.Fatalf("sale-scoped session sees %d upcoming sales, want its one", len(area.Upcoming))
	}

	resp, body := env.get(t, customerFollowSuggestionsPath, authHeader(saleScoped))
	assertAPIError(t, resp, body, http.StatusForbidden, "CUSTOMER_SESSION_SCOPE_INSUFFICIENT")

	// The same person, having proved they own the address, reads the panel.
	full := customerSignIn(t, env, "ana@example.com")
	readFollowSuggestions(t, env, full)
}

// setEventTagsOK sets an Event's Tags and insists it worked. setEventTags itself
// returns the raw exchange for the catalog tests that assert on rejections; here
// the Tags are arrangement rather than subject, and a silent failure to set them
// would turn every assertion below into a test of an untagged catalogue.
func setEventTagsOK(t *testing.T, env *testEnv, sessionID, eventID string, names []string) {
	t.Helper()
	resp, body := setEventTags(t, env, sessionID, eventID, names)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set tags %v status=%d error=%+v", names, resp.StatusCode, body.Error)
	}
}
