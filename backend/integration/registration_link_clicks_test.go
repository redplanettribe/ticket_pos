package integration

import (
	"net/http"
	"testing"
)

// Counting the hand-off to the Registration Link (issue #210): the Storefront's
// redirect route passes through here on a Customer's way to the registration
// site, and the organizer reads the number off the staff Event payload.
//
// The endpoint is shaped like the Affiliate Link click endpoint, and for the
// same reason: it answers success whatever happened, so a counter can never
// hold up — or refuse — the navigation it is counting.

// clickRegistrationLink is the Customer's side of the hand-off, as the
// Storefront redirect route reports it: no body, no credential, and the Event
// named by the two Storefront slugs and nothing else. There is deliberately no
// destination in this request — the Event owns where it sends people.
func clickRegistrationLink(t *testing.T, env *testEnv, orgSlug, eventSlug string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug+"/registration-link/click", nil, nil)
}

// registrationClicks reads the count off the staff Event payload, which is the
// surface an organizer reads it on.
func registrationClicks(t *testing.T, env *testEnv, sessionID, eventID string) int64 {
	t.Helper()
	return getEventRegistration(t, env, sessionID, eventID).RegistrationClickCount
}

func assertClickAccepted(t *testing.T, env *testEnv, orgSlug, eventSlug string) {
	t.Helper()
	resp, body := clickRegistrationLink(t, env, orgSlug, eventSlug)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("click status=%d, want 2xx; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("click error=%+v, want none", body.Error)
	}
}

func TestRegistrationLinkClickCountsEveryHandOff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	if clicks := registrationClicks(t, env, sessionID, eventID); clicks != 0 {
		t.Fatalf("a new Event starts at %d clicks, want 0", clicks)
	}

	assertClickAccepted(t, env, "test-org", "external-event")
	if clicks := registrationClicks(t, env, sessionID, eventID); clicks != 1 {
		t.Fatalf("clicks=%d after one hand-off, want 1", clicks)
	}
}

// Not deduplicated, deliberately: this counts clicks, not people. The same
// Customer coming back three times clicked three times, and the platform never
// learns whether any of the three registered.
func TestRegistrationLinkClickIsCountedAgainOnEveryRepeat(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	for i := 0; i < 3; i++ {
		assertClickAccepted(t, env, "test-org", "external-event")
	}
	if clicks := registrationClicks(t, env, sessionID, eventID); clicks != 3 {
		t.Fatalf("clicks=%d after three hand-offs, want 3", clicks)
	}
}

// An address that resolves to no hand-off is accepted and counts nothing. The
// caller is a Customer mid-navigation, and a tracking miss may never become an
// error that stops somebody registering.
func TestRegistrationLinkClickOnAnUnresolvableAddressIsAcceptedAndCountsNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	externalID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")
	ticketedID, _ := publishCheckoutEvent(t, env, sessionID, "Ticketed Gig", "ticketed-gig", 1000, 10)

	cases := []struct {
		name      string
		orgSlug   string
		eventSlug string
	}{
		{"an Organization that does not exist", "no-such-org", "external-event"},
		{"an Event that does not exist", "test-org", "no-such-event"},
		{"an Event that sells Ticket Types here", "test-org", "ticketed-gig"},
		{"an external Event under the wrong Organization", "no-such-org", "external-event"},
	}
	for _, tc := range cases {
		resp, body := clickRegistrationLink(t, env, tc.orgSlug, tc.eventSlug)
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			t.Errorf("%s: status=%d, want 2xx; error=%+v", tc.name, resp.StatusCode, body.Error)
		}
		if body.Error != nil {
			t.Errorf("%s: error=%+v, want none", tc.name, body.Error)
		}
	}

	if clicks := registrationClicks(t, env, sessionID, externalID); clicks != 0 {
		t.Fatalf("clicks=%d on the external Event, want 0 — no unresolvable hand-off may count", clicks)
	}
	// A ticketed Event hands nobody anywhere, so its counter never moves however
	// often the route is called against it.
	if clicks := registrationClicks(t, env, sessionID, ticketedID); clicks != 0 {
		t.Fatalf("clicks=%d on the ticketed Event, want 0", clicks)
	}
}

// The count is public to write and staff-only to read: the endpoint above takes
// no credential, and the number it moves comes back on the Event payload every
// Member of the Event reads.
func TestRegistrationClickCountIsReadableOnTheStaffEventPayload(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	assertClickAccepted(t, env, "test-org", "external-event")
	assertClickAccepted(t, env, "test-org", "external-event")

	view := getEventRegistration(t, env, sessionID, eventID)
	if view.RegistrationClickCount != 2 {
		t.Fatalf("registration_click_count=%d on the staff Event payload, want 2", view.RegistrationClickCount)
	}
	// The hand-offs changed nothing else about the Event: the mode and the link a
	// Customer was sent to are exactly as the organizer left them.
	if view.RegistrationMode != "external" {
		t.Fatalf("registration_mode=%q after two hand-offs, want external", view.RegistrationMode)
	}
	if view.RegistrationURL == nil || *view.RegistrationURL != "https://lu.ma/my-meetup" {
		t.Fatalf("registration_url=%v after two hand-offs, want the stored link", view.RegistrationURL)
	}
}
