package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Suggested Follows (#231, #232 and #233, parent #229, ADR 0031): Tags and
// Organizations a Customer does not Follow, offered to them beneath the ones
// they do.
//
// TWO RANKINGS, AND THE TESTS ARE IN THREE PARTS BECAUSE OF IT. A Customer who
// Follows NOTHING is ranked on ACTIVITY — the count of discoverable upcoming
// Events carrying a Tag or run by an Organization — and every suggestion they
// get carries a null reason, because Activity has no producing Tag to name.
// A Customer who Follows anything is ranked first on CO-OCCURRENCE with it, and
// each of those suggestions names the Tag that produced it. The first part of
// this file is #231's and asserts the reason is absent; the second is #232's,
// seeded by the Tags the Customer chose; the third is #233's, seeded by the
// DERIVED Tags on the upcoming Events of the Organizations they Follow, which
// are weighted below the chosen ones and are never offered back.
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
// RETURNED THEM, and insists on the way each entry claims its place: no reason,
// and no entry without a subject.
//
// Still no reason, after #232 added them and #233 widened where they come from.
// Every catalogue below that uses this helper is one where the reading Customer
// has no seed — chosen or derived — that co-occurs with what they are offered,
// so the suggestion came from Activity, which has no producing Tag to name. A
// reason appearing here would mean the ranking had invented one;
// suggestedTagOffers is what the Co-occurrence tests read with.
func suggestedTagKeys(t *testing.T, view followSuggestionsView) []string {
	t.Helper()
	keys := make([]string, 0, len(view.Tags))
	for _, suggestion := range view.Tags {
		if suggestion.Tag.CanonicalKey == "" {
			t.Fatalf("suggested tag carries no canonical key: %+v", suggestion)
		}
		if suggestion.Reason != nil {
			t.Fatalf("suggested tag %q carries reason %+v — Activity ranking has no producing Tag to name",
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

// ---------------------------------------------------------------------------
// Co-occurrence (#232). Two Tags co-occur when ONE EVENT CARRIES BOTH.
//
// It is a fact about the CATALOGUE and never about other Customers. Nothing
// below arranges a second Customer's Follows and then reads the first
// Customer's panel, because no such path exists: there is no collaborative
// filtering here in any disguise, and the tests are written so that adding one
// would not make any of them pass.
// ---------------------------------------------------------------------------

// suggestedTagOffers flattens the Tag group to "key" or "key from reason-key",
// in the order the API returned them.
//
// The reason belongs in the same string as the subject rather than in a second
// map, because the two assertions are one: a panel that offers the right Tags
// while naming the wrong interest is as wrong as one that offers the wrong
// Tags, and a test that checked them separately would let the pairing drift.
func suggestedTagOffers(t *testing.T, view followSuggestionsView) []string {
	t.Helper()
	offers := make([]string, 0, len(view.Tags))
	for _, suggestion := range view.Tags {
		if suggestion.Tag.CanonicalKey == "" {
			t.Fatalf("suggested tag carries no canonical key: %+v", suggestion)
		}
		offers = append(offers, offer(suggestion.Tag.CanonicalKey, suggestion.Reason))
	}
	return offers
}

// suggestedOrganizationOffers is the Organization group's counterpart, keyed on
// slug as suggestedOrganizationSlugs is.
func suggestedOrganizationOffers(t *testing.T, view followSuggestionsView) []string {
	t.Helper()
	offers := make([]string, 0, len(view.Organizations))
	for _, suggestion := range view.Organizations {
		if suggestion.Organization.Slug == "" {
			t.Fatalf("suggested organization carries no slug: %+v", suggestion)
		}
		offers = append(offers, offer(suggestion.Organization.Slug, suggestion.Reason))
	}
	return offers
}

// offer renders one suggestion as the subject and, if it has one, the Tag that
// produced it.
//
// A CANONICAL KEY AND NEVER A SENTENCE, which is what these tests are checking
// as much as which key it is: the Storefront words a Tag from its own message
// catalogues in the page's Locale (ADR 0027), so a reason arriving as English
// prose would be the one place a Tag's name crossed the wire in a language.
func offer(subject string, reason *suggestionReason) string {
	if reason == nil {
		return subject
	}
	if reason.TagCanonicalKey == "" {
		return subject + " from an empty reason"
	}
	return subject + " from " + reason.TagCanonicalKey
}

// TestFollowSuggestionsOfferTagsCoOccurringWithAFollowedTag is the ticket's
// first sentence: a Customer who Follows one Tag stops getting everybody's list.
//
// Two Events carry Music and Comedy together, one carries Film alone. Comedy is
// offered ahead of Film — not because Comedy is busier, since it is not, but
// because it rides alongside the Tag this Customer chose. And Music itself is
// never offered back, which is the panel's most basic obligation.
func TestFollowSuggestionsOfferTagsCoOccurringWithAFollowedTag(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	first := discoverableEvent(t, env, sessionID, "Jazz Night", "jazz-night", upcoming)
	setEventTagsOK(t, env, sessionID, first, []string{"Music", "Comedy"})
	second := discoverableEvent(t, env, sessionID, "Jazz Again", "jazz-again", upcoming)
	setEventTagsOK(t, env, sessionID, second, []string{"Music", "Comedy"})
	// Film shares no Event with Music, so it co-occurs with nothing this
	// Customer Follows and can only arrive behind Comedy, on Activity alone.
	unrelated := discoverableEvent(t, env, sessionID, "Film Club", "film-club", upcoming)
	setEventTagsOK(t, env, sessionID, unrelated, []string{"Film"})

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	got := suggestedTagOffers(t, readFollowSuggestions(t, env, token))
	if !equalStrings(got, []string{"comedy from music", "film"}) {
		t.Fatalf("suggested tags = %v, want [comedy from music, film] — Comedy rides alongside the Tag this Customer chose", got)
	}
	for _, offered := range got {
		if offered == "music" {
			t.Fatalf("suggested tags = %v, include the Tag the Customer already Follows", got)
		}
	}
}

// TestFollowSuggestionsNormaliseAwayATagThatRidesWithEverything is the reason
// normalisation exists, and it is not a refinement.
//
// Nightlife is on six upcoming Events and shares three of them with Music;
// Comedy is on two and shares both. RAW CO-OCCURRENCE WOULD PUT NIGHTLIFE
// FIRST — three shared Events beat two — and would put it first for every
// Customer on the platform, whatever they Follow, because a Tag carried by
// nearly everything co-occurs with nearly everything. Dividing by a damped
// function of each candidate's own Activity is what turns that around.
func TestFollowSuggestionsNormaliseAwayATagThatRidesWithEverything(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	// Three Events carrying the followed Tag. Two of them also carry Comedy, and
	// all three also carry Nightlife.
	withComedy := []string{"Music", "Comedy", "Nightlife"}
	first := discoverableEvent(t, env, sessionID, "One", "one", upcoming)
	setEventTagsOK(t, env, sessionID, first, withComedy)
	second := discoverableEvent(t, env, sessionID, "Two", "two", upcoming)
	setEventTagsOK(t, env, sessionID, second, withComedy)
	third := discoverableEvent(t, env, sessionID, "Three", "three", upcoming)
	setEventTagsOK(t, env, sessionID, third, []string{"Music", "Nightlife"})

	// Three more Events that make Nightlife ubiquitous and have nothing to do
	// with this Customer.
	for _, slug := range []string{"four", "five", "six"} {
		event := discoverableEvent(t, env, sessionID, slug, slug, upcoming)
		setEventTagsOK(t, env, sessionID, event, []string{"Nightlife"})
	}

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	got := suggestedTagOffers(t, readFollowSuggestions(t, env, token))
	if !equalStrings(got, []string{"comedy from music", "nightlife from music"}) {
		t.Fatalf("suggested tags = %v, want [comedy from music, nightlife from music] — Nightlife shares MORE Events with Music and must still rank below it, because it shares that many with everything", got)
	}
}

// TestFollowSuggestionsDoNotLetASingleCoincidenceOutrankRealSupply is the other
// half of the same decision, and the reason the division is DAMPED rather than
// plain.
//
// Comedy is on one Event, which happens to carry Music: one shared Event out of
// one, a perfect ratio built on a coincidence. Festival is on three, two of them
// shared: a worse ratio with real supply behind it. Dividing by Activity
// outright would hand the top of the list to Comedy (1.00 against 0.67);
// dividing by its square root keeps the ratio's judgement while letting the
// well-supported Tag win (1.15 against 1.00).
//
// The alphabet is against the assertion on purpose — "comedy" sorts before
// "festival", so the tie-break would seat the coincidence first if the two ever
// scored equal, and this test would notice.
func TestFollowSuggestionsDoNotLetASingleCoincidenceOutrankRealSupply(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	first := discoverableEvent(t, env, sessionID, "One", "one", upcoming)
	setEventTagsOK(t, env, sessionID, first, []string{"Music", "Festival", "Comedy"})
	second := discoverableEvent(t, env, sessionID, "Two", "two", upcoming)
	setEventTagsOK(t, env, sessionID, second, []string{"Music", "Festival"})
	third := discoverableEvent(t, env, sessionID, "Three", "three", upcoming)
	setEventTagsOK(t, env, sessionID, third, []string{"Festival"})

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	got := suggestedTagOffers(t, readFollowSuggestions(t, env, token))
	if !equalStrings(got, []string{"festival from music", "comedy from music"}) {
		t.Fatalf("suggested tags = %v, want [festival from music, comedy from music] — one Event that co-occurred once is a coincidence, not a related Tag", got)
	}
}

// TestFollowSuggestionsRankOrganizationsByHowMuchOfTheirProgrammeMatches is the
// same mechanism reaching the other followable kind through the SAME join.
//
// Tags are worn by Events and never by Organizations, so an Organization is
// related to a Tag exactly when its upcoming Events carry it. No
// Organization–Tag association is invented, and none exists to invent.
//
// AND ORGANIZATIONS ARE NOT NORMALISED, which this catalogue is built to pin.
// test-org runs four upcoming Events of which two carry Music; other-org runs
// one, which carries Music. Normalising by programme size would put other-org
// first on a perfect one-of-one. It ranks second, because a match count is
// genuine signal for an Organization: an Organization spans only its own
// programme, so it cannot be ubiquitous the way a shared-pool Tag can.
func TestFollowSuggestionsRankOrganizationsByHowMuchOfTheirProgrammeMatches(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	for _, slug := range []string{"mine-one", "mine-two"} {
		event := discoverableEvent(t, env, sessionID, slug, slug, upcoming)
		setEventTagsOK(t, env, sessionID, event, []string{"Music"})
	}
	for _, slug := range []string{"mine-three", "mine-four"} {
		event := discoverableEvent(t, env, sessionID, slug, slug, upcoming)
		setEventTagsOK(t, env, sessionID, event, []string{"Film"})
	}

	theirs := discoverableEvent(t, env, other, "Theirs", "theirs", upcoming)
	setEventTagsOK(t, env, other, theirs, []string{"Music"})

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	got := suggestedOrganizationOffers(t, readFollowSuggestions(t, env, token))
	if !equalStrings(got, []string{testOrgSlug + " from music", "other-org from music"}) {
		t.Fatalf("suggested organizations = %v, want [%s from music, other-org from music] — two matching Events beat one, and neither is divided by the size of the programme it came from", got, testOrgSlug)
	}
}

// TestFollowSuggestionsNameTheFollowedTagThatProducedTheSuggestion is user
// story 19: the panel reads as reasoned rather than random.
//
// Ana Follows Music and Film. Comedy shares two Events with Music and one with
// Film, so Music is named; Sports shares one with Film alone, so Film is. And
// Festival shares exactly one with each, which is the tie: it breaks on the
// canonical key ascending, so "film" is named, deterministically, and the same
// read twice cannot name two different interests.
func TestFollowSuggestionsNameTheFollowedTagThatProducedTheSuggestion(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	first := discoverableEvent(t, env, sessionID, "One", "one", upcoming)
	setEventTagsOK(t, env, sessionID, first, []string{"Music", "Comedy"})
	second := discoverableEvent(t, env, sessionID, "Two", "two", upcoming)
	setEventTagsOK(t, env, sessionID, second, []string{"Music", "Comedy"})
	third := discoverableEvent(t, env, sessionID, "Three", "three", upcoming)
	setEventTagsOK(t, env, sessionID, third, []string{"Film", "Comedy", "Sports"})
	fourth := discoverableEvent(t, env, sessionID, "Four", "four", upcoming)
	setEventTagsOK(t, env, sessionID, fourth, []string{"Music", "Film", "Festival"})

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")
	followTagOK(t, env, token, "film")

	got := suggestedTagOffers(t, readFollowSuggestions(t, env, token))
	want := []string{"festival from film", "comedy from music", "sports from film"}
	if !equalStrings(got, want) {
		t.Fatalf("suggested tags = %v, want %v — each suggestion names the strongest contributor among the Tags this Customer Follows, ties broken on canonical key", got, want)
	}
}

// TestFollowSuggestionsOrderIsTotalUnderCoOccurrence keeps the panel from
// shuffling under the Customer.
//
// Three candidates scoring identically, which at this catalogue size is the
// common case rather than the exotic one: the same Event carrying four Tags
// gives every one of them the same Co-occurrence and the same Activity. Without
// a tie-break Postgres may return them in any order it likes, and a Following
// page re-rendering after a Follow would show a shuffle of things the Customer
// never touched.
func TestFollowSuggestionsOrderIsTotalUnderCoOccurrence(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	tied := []string{"Music", "Sports", "Comedy", "Festival"}
	first := discoverableEvent(t, env, sessionID, "One", "one", upcoming)
	setEventTagsOK(t, env, sessionID, first, tied)
	second := discoverableEvent(t, env, sessionID, "Two", "two", upcoming)
	setEventTagsOK(t, env, sessionID, second, tied)

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")

	want := []string{"comedy from music", "festival from music", "sports from music"}
	firstRead := suggestedTagOffers(t, readFollowSuggestions(t, env, token))
	if !equalStrings(firstRead, want) {
		t.Fatalf("suggested tags = %v, want %v — equal scores break on canonical key ascending", firstRead, want)
	}
	if secondRead := suggestedTagOffers(t, readFollowSuggestions(t, env, token)); !equalStrings(secondRead, firstRead) {
		t.Fatalf("two identical reads returned %v then %v — the order is not total", firstRead, secondRead)
	}
}

// TestFollowSuggestionsFallBackToActivityWithoutAnyFollow keeps #231's ranking
// alive beside the personalised one, for the person it was written for.
//
// THIS TEST WAS NARROWED BY #233 AND THE NARROWING IS THE FEATURE. It used to
// arrange a Customer who Follows one Organization and assert they met the
// generic Activity ranking, pinning the boundary #232 stopped at. Deriving Tags
// from a followed Organization is exactly the erasure of that boundary, so the
// arrangement now Follows nothing at all — which is the only remaining case
// with nothing for Co-occurrence to start from, and still the right answer for
// somebody the platform knows nothing about yet. Every suggestion carries a null
// reason, because there is no Tag, chosen or derived, to name as the producer.
func TestFollowSuggestionsFallBackToActivityWithoutAnyFollow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	for _, slug := range []string{"one", "two"} {
		event := discoverableEvent(t, env, sessionID, slug, slug, upcoming)
		setEventTagsOK(t, env, sessionID, event, []string{"Nightlife", "Music"})
	}
	theirs := discoverableEvent(t, env, other, "Theirs", "theirs", upcoming)
	setEventTagsOK(t, env, other, theirs, []string{"Nightlife"})

	token := customerSignIn(t, env, "ana@example.com")

	view := readFollowSuggestions(t, env, token)
	// suggestedTagKeys is the assertion: it fails on any reason at all. Nightlife
	// leads on three upcoming Events to Music's two — Activity, not
	// Co-occurrence, which has nothing to work from here.
	if got := suggestedTagKeys(t, view); !equalStrings(got, []string{"nightlife", "music"}) {
		t.Fatalf("suggested tags = %v, want [nightlife music] — a Customer who Follows nothing is ranked on Activity, with no reason to name", got)
	}
	if got := suggestedOrganizationSlugs(t, view); !equalStrings(got, []string{testOrgSlug, "other-org"}) {
		t.Fatalf("suggested organizations = %v, want [%s other-org]", got, testOrgSlug)
	}
}

// ---------------------------------------------------------------------------
// Derived Tags (#233). The Tags carried by a followed Organization's upcoming
// Events seed the Co-occurrence ranking too, weighted BELOW the Tags the
// Customer chose, and are never offered back to that Customer.
//
// Still nothing about other Customers. A derived Tag is read from the
// catalogue — this Customer's own Organization Follow joined to Events and
// their Tags — so the widening keeps Co-occurrence a fact about what is on
// sale (ADR 0031).
// ---------------------------------------------------------------------------

// TestFollowSuggestionsDeriveTagsFromAFollowedOrganization is the ticket's first
// sentence: a Customer who Follows only an Organization stops being treated as a
// Customer who Follows nothing.
//
// The catalogue is built so the two rankings disagree, because a test where they
// agree proves nothing. On ACTIVITY alone the panel would read [film, music,
// comedy] — Film is on three upcoming Events and leads comfortably. Following
// test-org derives Music from its Event, and Comedy rides alongside Music on
// other-org's Event, so Comedy comes first and Film arrives behind it on
// Activity alone, with no reason to name.
//
// Music itself is absent, which is the rule the next test is about.
func TestFollowSuggestionsDeriveTagsFromAFollowedOrganization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	// The followed Organization's whole programme, and the whole of what it says
	// about this Customer: one Event carrying Music.
	mine := discoverableEvent(t, env, sessionID, "Mine", "mine", upcoming)
	setEventTagsOK(t, env, sessionID, mine, []string{"Music"})

	// Somebody else's Event carrying Music and Comedy together: the Co-occurrence.
	shared := discoverableEvent(t, env, other, "Shared", "shared", upcoming)
	setEventTagsOK(t, env, other, shared, []string{"Music", "Comedy"})

	// And the busiest Tag in the catalogue, related to nothing this Customer has
	// touched. It would lead the panel on Activity.
	for _, slug := range []string{"film-one", "film-two", "film-three"} {
		event := discoverableEvent(t, env, other, slug, slug, upcoming)
		setEventTagsOK(t, env, other, event, []string{"Film"})
	}

	token := customerSignIn(t, env, "ana@example.com")
	followOrganizationOK(t, env, token, testOrgSlug)

	view := readFollowSuggestions(t, env, token)
	got := suggestedTagOffers(t, view)
	if !equalStrings(got, []string{"comedy from music", "film"}) {
		t.Fatalf("suggested tags = %v, want [comedy from music, film] — the Tags on a followed Organization's Events seed the ranking, so Comedy leads a busier Film", got)
	}

	// Derived Tags reach Organization suggestions THROUGH THE SAME JOIN chosen
	// Tags reach them by: other-org carries Music on an upcoming Event, so it is
	// offered and names the Tag that produced it.
	if got := suggestedOrganizationOffers(t, view); !equalStrings(got, []string{"other-org from music"}) {
		t.Fatalf("suggested organizations = %v, want [other-org from music] — a derived Tag reaches Organizations through the same join as a chosen one", got)
	}
}

// TestFollowSuggestionsNeverOfferBackATagDerivedFromAFollowedOrganization is the
// explicit rule of ADR 0031, and the panel's most obvious way of looking foolish
// if it is missed.
//
// Offering somebody the Tag just inferred from their own Follow presents them
// their own answer as a discovery. Both Tags on the followed Organization's
// Events are the two busiest in this catalogue and would lead the Activity
// ranking; neither may appear, in either ranking — the exclusion has to survive
// the Activity backfill behind Co-occurrence as well, which is the half a query
// gets wrong by only filtering the personalised path.
func TestFollowSuggestionsNeverOfferBackATagDerivedFromAFollowedOrganization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	for _, slug := range []string{"one", "two"} {
		event := discoverableEvent(t, env, sessionID, slug, slug, upcoming)
		setEventTagsOK(t, env, sessionID, event, []string{"Music", "Nightlife"})
	}
	theirs := discoverableEvent(t, env, other, "Theirs", "theirs", upcoming)
	setEventTagsOK(t, env, other, theirs, []string{"Comedy"})

	token := customerSignIn(t, env, "ana@example.com")
	followOrganizationOK(t, env, token, testOrgSlug)

	got := suggestedTagOffers(t, readFollowSuggestions(t, env, token))
	if !equalStrings(got, []string{"comedy"}) {
		t.Fatalf("suggested tags = %v, want [comedy] — Music and Nightlife were inferred from this Customer's own Follow and are never offered back", got)
	}
}

// TestFollowSuggestionsWeighAChosenTagAboveADerivedOne is where the inference is
// kept honest: you Followed the Organization, not necessarily its genre.
//
// THE WEIGHT IS ONLY OBSERVABLE WHEN THE TWO KINDS OF SEED COMPETE, which is why
// this arranges a Customer who Follows both. A weight applied to every seed a
// Customer has is a constant factor across every candidate, and a constant
// factor changes no order at all — so a Customer who Follows only Organizations
// cannot show it, and the assertion has to be a race between a candidate reached
// through a chosen Tag and one reached through a derived Tag on identical
// supply.
//
// Ana chose Music and Follows other-org, whose Event carries Film. Sports shares
// one Event with Music; Comedy shares one Event with Film; both are on exactly
// one upcoming Event, so nothing but the weight separates them. THE ALPHABET IS
// AGAINST THE ASSERTION on purpose — "comedy" sorts before "sports", so if the
// two ever scored equal the tie-break would seat the derived one first and this
// test would notice.
//
// Festival is the third case: it shares one Event with the chosen Tag and one
// with the derived Tag, so it scores above both and NAMES THE CHOSEN ONE. A
// reason is the strongest contributor, and a chosen Tag contributes more than a
// derived Tag on the same evidence.
func TestFollowSuggestionsWeighAChosenTagAboveADerivedOne(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	other := otherOrganizationSession(t, env)
	upcoming := env.fixedClock.Add(30 * 24 * time.Hour)

	// The followed Organization's Event, and the only thing that derives Film.
	theirs := discoverableEvent(t, env, other, "Theirs", "theirs", upcoming)
	setEventTagsOK(t, env, other, theirs, []string{"Film"})

	// One candidate through the chosen Tag, one through the derived Tag.
	chosen := discoverableEvent(t, env, sessionID, "Chosen", "chosen", upcoming)
	setEventTagsOK(t, env, sessionID, chosen, []string{"Music", "Sports"})
	derived := discoverableEvent(t, env, sessionID, "Derived", "derived", upcoming)
	setEventTagsOK(t, env, sessionID, derived, []string{"Film", "Comedy"})

	// And one candidate reached by both at once.
	both := discoverableEvent(t, env, sessionID, "Both", "both", upcoming)
	setEventTagsOK(t, env, sessionID, both, []string{"Music", "Film", "Festival"})

	token := customerSignIn(t, env, "ana@example.com")
	followTagOK(t, env, token, "music")
	followOrganizationOK(t, env, token, "other-org")

	view := readFollowSuggestions(t, env, token)
	want := []string{"festival from music", "sports from music", "comedy from film"}
	if got := suggestedTagOffers(t, view); !equalStrings(got, want) {
		t.Fatalf("suggested tags = %v, want %v — a Tag the Customer chose outranks the same evidence reached by derivation, and names the suggestion when both contribute", got, want)
	}

	// The weighting reaches Organizations through the same seeds. test-org
	// carries Music on two upcoming Events and Film on two, and names the chosen
	// Tag for the same reason a suggested Tag does.
	if got := suggestedOrganizationOffers(t, view); !equalStrings(got, []string{testOrgSlug + " from music"}) {
		t.Fatalf("suggested organizations = %v, want [%s from music]", got, testOrgSlug)
	}
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
