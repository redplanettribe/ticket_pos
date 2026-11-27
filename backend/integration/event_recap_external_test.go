package integration

import (
	"testing"
	"time"
)

// The Event Recap of an Event with External Registration (spec #884, #888),
// through the sweep endpoint and the recording mail sender: its Page Views,
// its Registration Link clicks and its Affiliate Links' Clicks, in place of
// every sales figure, with a link to Reach and to nothing else.
//
// THE CLICKS ARE MADE THROUGH THE PUBLIC ROUTES the Storefront calls: the
// Registration Link hand-off, and the Page View beacon with and without an
// Affiliate Link's code.

// An Event with External Registration is mailed with its Page Views, its
// Registration Link clicks and its Affiliate Links' Clicks, and no sales
// figure; the mail links to Reach only.
func TestEventRecapOfAnEventWithExternalRegistrationStatesItsClicksAndLinksToReachOnly(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := createExternalAffiliateEvent(t, env, f.admin, "Meetup", "meetup")
	over := recapAt(0, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Meetup", over.Add(-3*time.Hour), &over)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")
	publishRecapEvent(t, env, f.admin, eventID)

	newsletter := newAffiliateLink(t, env, f.admin, eventID, "Newsletter")
	recordPageViews(t, env, recapAt(0, 12, 0), testOrgSlug, "meetup", newsletter, 2)
	recordPageViews(t, env, recapAt(0, 13, 0), testOrgSlug, "meetup", "", 4)
	for range 5 {
		assertClickAccepted(t, env, testOrgSlug, "meetup")
	}

	recap := onlyRecap(t, env)
	if recap.Sales != nil {
		t.Errorf("sales figures = %+v, want none", recap.Sales)
	}
	if recap.RegistrationLinkClicks == nil || *recap.RegistrationLinkClicks != 5 {
		t.Fatalf("Registration Link clicks = %v, want 5", recap.RegistrationLinkClicks)
	}
	// The count is the one the staff app states for the same Event.
	if shown := registrationClicks(t, env, f.admin, eventID); shown != 5 {
		t.Errorf("the staff Event payload states %d clicks, the recap 5", shown)
	}
	if reach := recapReach(t, recap); reach.PageViews != 6 || len(reach.AffiliateLinks) != 1 ||
		reach.AffiliateLinks[0].Clicks != 2 || reach.AffiliateLinks[0].AttributedSales != nil {
		t.Errorf("reach = %+v, want 6 Page Views and one link with 2 Clicks and no Attributed Sales", reach)
	}

	wantInBothParts(t, recap,
		"Registration Link clicks: 5",
		"Page views until the Event was over: 6",
		"Affiliate Link Clicks Newsletter 2",
	)
	wantInNeitherPart(t, recap,
		"Tickets sold", "Ticket sales", "Takings", "Net Proceeds", "Reversed sales",
		"Sales Channel", "Ticket Type", "Attributed sales", "$",
		"door sale", "Sale Import", "Sale Reversal",
	)

	staffEvent := "http://staff.example/events/" + eventID
	wantInBothParts(t, recap, staffEvent+"/reach")
	wantInNeitherPart(t, recap, staffEvent+"/sales", "Sales Trends", "Sales list")
}

// An Event that sells Ticket Types states no Registration Link clicks: it has
// no Registration Link to click.
func TestEventRecapOfAnEventThatSellsTicketTypesStatesNoRegistrationLinkClicks(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))
	// The hand-off route answers success for any address and counts nothing
	// for an Event that hands nobody anywhere.
	assertClickAccepted(t, env, testOrgSlug, "jazz-night")

	recap := onlyRecap(t, env)
	if recap.RegistrationLinkClicks != nil {
		t.Fatalf("Registration Link clicks = %d, want none stated", *recap.RegistrationLinkClicks)
	}
	wantInNeitherPart(t, recap, "Registration Link")
	wantInBothParts(t, recap, "Tickets sold: 0", "Sales Trends")
}

// A Registration Link nobody clicked is stated as a zero, like the Page Views
// of a page nobody loaded: that it drew nothing is what the recap is for.
func TestEventRecapOfAnEventWithExternalRegistrationNobodyClickedStatesZeros(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := createExternalAffiliateEvent(t, env, f.admin, "Meetup", "meetup")
	over := recapAt(0, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Meetup", over.Add(-3*time.Hour), &over)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")
	publishRecapEvent(t, env, f.admin, eventID)

	recap := onlyRecap(t, env)
	wantInBothParts(t, recap, "Registration Link clicks: 0", "Page views until the Event was over: 0")
	wantInNeitherPart(t, recap, "Affiliate Link", "Tickets sold")
}
