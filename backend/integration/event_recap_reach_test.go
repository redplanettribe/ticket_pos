package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Event Recap's reach figures (spec #884, #887), through the sweep
// endpoint and the recording mail sender: the Page Views an Event's page drew
// until the Event was over, and its busiest Affiliate Links.
//
// THE LOADS ARE RECORDED THROUGH THE BEACON, the Storefront's own call, with
// the affiliates clock moved to the moment of each, and the Affiliate Links
// are made, followed and bought through with the real APIs.

// loadRecapPage loads an Event's page count times at one moment, through the
// Affiliate Link with that code when there is one.
func loadRecapPage(t *testing.T, env *testEnv, at time.Time, slug, code string, count int) {
	t.Helper()
	sharedApp.AffiliatesService.WithClock(func() time.Time { return at })
	for i := 0; i < count; i++ {
		if resp, body := recordPageView(t, env, testOrgSlug, slug, code); resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Fatalf("page view status=%d error=%+v", resp.StatusCode, body.Error)
		}
	}
}

// recapReach is the recap's reach figures, which every recap states.
func recapReach(t *testing.T, recap platform.EventRecap) *platform.EventRecapReach {
	t.Helper()
	if recap.Reach == nil {
		t.Fatal("the recap states no reach figures")
	}
	return recap.Reach
}

// wantInBothParts fails unless each part of the mail says every one of these.
func wantInBothParts(t *testing.T, recap platform.EventRecap, want ...string) {
	t.Helper()
	for part, read := range recapParts(t, recap) {
		for _, figure := range want {
			if !strings.Contains(read, figure) {
				t.Errorf("the %s part does not say %q:\n%s", part, figure, read)
			}
		}
	}
}

// wantInNeitherPart fails if either part of the mail says any of these.
func wantInNeitherPart(t *testing.T, recap platform.EventRecap, unwanted ...string) {
	t.Helper()
	for part, read := range recapParts(t, recap) {
		for _, figure := range unwanted {
			if strings.Contains(read, figure) {
				t.Errorf("the %s part says %q:\n%s", part, figure, read)
			}
		}
	}
}

// Page Views before the over instant are counted, and ones after it are not:
// somebody looking at a finished Event does not inflate the figure.
func TestEventRecapCountsThePageViewsUntilTheEventWasOver(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	over := recapAt(0, 23, 0)
	f.publishedEvent(t, env, "Jazz Night", over)

	loadRecapPage(t, env, recapAt(-3, 12, 10), "jazz-night", "", 2)
	loadRecapPage(t, env, recapAt(0, 20, 30), "jazz-night", "", 3)
	loadRecapPage(t, env, recapAt(0, 22, 59), "jazz-night", "", 1)
	// From the over instant on: the hour that began at it, the night, and the
	// morning of the recap itself.
	loadRecapPage(t, env, over, "jazz-night", "", 4)
	loadRecapPage(t, env, recapAt(1, 0, 5), "jazz-night", "", 8)
	loadRecapPage(t, env, recapAt(1, 8, 0), "jazz-night", "", 16)

	recap := onlyRecap(t, env)
	if got := recapReach(t, recap).PageViews; got != 6 {
		t.Fatalf("Page Views = %d, want 6: the 28 loads from the over instant on are left out", got)
	}
	wantInBothParts(t, recap, "Page views until the Event was over: 6")
}

// The cut is by the hour, the floor of the data (ADR 0057): the bucket whose
// hour began before the over instant is counted whole, and the next is not.
func TestEventRecapCutsPageViewsAtTheHourThatBeganBeforeTheEventWasOver(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Jazz Night", recapAt(0, 22, 30))

	loadRecapPage(t, env, recapAt(0, 22, 10), "jazz-night", "", 2)
	loadRecapPage(t, env, recapAt(0, 22, 50), "jazz-night", "", 1)
	loadRecapPage(t, env, recapAt(0, 23, 0), "jazz-night", "", 4)

	if got := recapReach(t, onlyRecap(t, env)).PageViews; got != 3 {
		t.Fatalf("Page Views = %d, want 3: the whole of the 22:00 bucket and nothing of the 23:00 one", got)
	}
}

// The three Affiliate Links with the most Clicks before the over instant are
// listed, busiest first, each with its Clicks and its attributed active
// Sales, and a fourth is not, however many Clicks it drew afterwards.
func TestEventRecapListsTheThreeBusiestAffiliateLinks(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, f.admin, "Jazz Night", "jazz-night", 2000, 100)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")
	over := recapAt(0, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Jazz Night", over.Add(-3*time.Hour), &over)

	poster := newAffiliateLink(t, env, f.admin, eventID, "Poster")
	radio := newAffiliateLink(t, env, f.admin, eventID, "Radio")
	flyer := newAffiliateLink(t, env, f.admin, eventID, "Flyer")
	late := newAffiliateLink(t, env, f.admin, eventID, "Latecomer")

	// Two Sales through the poster, one of them undone by its Customer, and
	// one through the flyer. The radio drove Clicks and no Sale.
	buy := func(email, code string) string {
		begun := beginCheckoutOK(t, env, testOrgSlug, "jazz-night", affiliateCheckoutBody(email, lastClick(code), cartLine(gaID, 2)))
		return confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved").ConfirmationRef
	}
	buy("ana.poster@example.com", poster)
	undoOwnSale(t, env, "rita.poster@example.com", buy("rita.poster@example.com", poster))
	buy("bea.flyer@example.com", flyer)

	loadRecapPage(t, env, recapAt(-2, 10, 0), "jazz-night", poster, 3)
	loadRecapPage(t, env, recapAt(0, 21, 0), "jazz-night", poster, 2)
	loadRecapPage(t, env, recapAt(-1, 9, 0), "jazz-night", radio, 3)
	loadRecapPage(t, env, recapAt(-1, 15, 0), "jazz-night", flyer, 2)
	loadRecapPage(t, env, recapAt(0, 12, 0), "jazz-night", late, 1)
	loadRecapPage(t, env, recapAt(0, 12, 0), "jazz-night", "", 4)
	// After the Event was over the fourth link drew more Clicks than any
	// other ever did. They are in its lifetime figure and not in the recap.
	loadRecapPage(t, env, recapAt(1, 1, 0), "jazz-night", late, 9)

	recap := onlyRecap(t, env)
	reach := recapReach(t, recap)
	if reach.PageViews != 15 {
		t.Errorf("Page Views = %d, want 15: 11 Clicks and 4 loads no link drove, each once", reach.PageViews)
	}
	type row struct {
		name          string
		clicks, sales int
	}
	want := []row{{"Poster", 5, 1}, {"Radio", 3, 0}, {"Flyer", 2, 1}}
	if len(reach.AffiliateLinks) != len(want) {
		t.Fatalf("Affiliate Links = %+v, want the three busiest", reach.AffiliateLinks)
	}
	for i, link := range reach.AffiliateLinks {
		if link.AttributedSales == nil {
			t.Fatalf("link %q states no Attributed Sales on an Event that sells here", link.Name)
		}
		if got := (row{link.Name, link.Clicks, *link.AttributedSales}); got != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, got, want[i])
		}
	}

	// The Attributed Sales are the ones the Affiliate Links list states.
	for code, sales := range map[string]int{poster: 1, radio: 0, flyer: 1} {
		if listed := affiliateLinkStatsByCode(t, env, f.admin, eventID, code).SalesCount; listed != sales {
			t.Errorf("the Affiliate Links list states %d sales for %s, the recap %d", listed, code, sales)
		}
	}

	wantInBothParts(t, recap,
		"Page views until the Event was over: 15",
		"Its busiest Affiliate Links, by Clicks until the Event was over:",
		"Affiliate Link Clicks Attributed sales Poster 5 1 Radio 3 0 Flyer 2 1",
	)
	wantInNeitherPart(t, recap, "Latecomer",
		"ana.poster@example.com", "rita.poster@example.com", "bea.flyer@example.com", "Buyer",
		poster, radio, flyer, late)
}

// An Event with no Affiliate Links states its Page Views and has no Affiliate
// Link block.
func TestEventRecapOfAnEventWithNoAffiliateLinksHasNoAffiliateLinkBlock(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))
	loadRecapPage(t, env, recapAt(0, 12, 0), "jazz-night", "", 2)

	recap := onlyRecap(t, env)
	if reach := recapReach(t, recap); reach.PageViews != 2 || len(reach.AffiliateLinks) != 0 {
		t.Fatalf("reach = %+v, want 2 Page Views and no Affiliate Link", reach)
	}
	wantInBothParts(t, recap, "Page views until the Event was over: 2")
	wantInNeitherPart(t, recap, "Affiliate Link", "Clicks", "Attributed sales")
}

// An Affiliate Link nobody followed before the Event was over is not listed,
// as Reach Trends unlists it, and an Event nobody loaded states a zero.
func TestEventRecapLeavesOutAnAffiliateLinkNobodyFollowed(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))
	unfollowed := newAffiliateLink(t, env, f.admin, eventID, "Unfollowed")
	// Its only Clicks came once the Event was over.
	loadRecapPage(t, env, recapAt(1, 2, 0), "jazz-night", unfollowed, 3)

	recap := onlyRecap(t, env)
	if reach := recapReach(t, recap); reach.PageViews != 0 || len(reach.AffiliateLinks) != 0 {
		t.Fatalf("reach = %+v, want no Page View and no Affiliate Link", reach)
	}
	wantInBothParts(t, recap, "Page views until the Event was over: 0")
	wantInNeitherPart(t, recap, "Unfollowed", "Affiliate Link")
}

// An Event with External Registration states its Page Views and its Affiliate
// Links' Clicks, and no Attributed Sales, which its links can never have.
func TestEventRecapOfAnEventWithExternalRegistrationStatesClicksAndNoAttributedSales(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := createExternalAffiliateEvent(t, env, f.admin, "Meetup", "meetup")
	over := recapAt(0, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Meetup", over.Add(-3*time.Hour), &over)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")
	publishRecapEvent(t, env, f.admin, eventID)
	newsletter := newAffiliateLink(t, env, f.admin, eventID, "Newsletter")
	loadRecapPage(t, env, recapAt(0, 12, 0), "meetup", newsletter, 2)
	loadRecapPage(t, env, recapAt(0, 12, 0), "meetup", "", 1)

	recap := onlyRecap(t, env)
	reach := recapReach(t, recap)
	if reach.PageViews != 3 || len(reach.AffiliateLinks) != 1 || reach.AffiliateLinks[0].Clicks != 2 || reach.AffiliateLinks[0].AttributedSales != nil {
		t.Fatalf("reach = %+v, want 3 Page Views and one link with 2 Clicks and no Attributed Sales", reach)
	}
	wantInBothParts(t, recap, "Page views until the Event was over: 3", "Affiliate Link Clicks Newsletter 2")
	wantInNeitherPart(t, recap, "Attributed sales")
}
