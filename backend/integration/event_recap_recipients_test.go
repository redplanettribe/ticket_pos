package integration

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Who the Event Recap goes to (spec #884, #885): the Event's Event Owners, or
// the Organization's Org Admins when it has none, judged at send time, one
// copy per person in their own Staff Locale, and never Event Staff or a
// Platform Operator. Exercised through the sweep endpoint and the recording
// mail sender, like event_recap_test.go.

// recapReaders is who has received an Event Recap so far, sorted, one entry
// per mail, so a person written to twice shows twice.
func recapReaders() []string {
	readers := []string{}
	for _, recap := range sharedEmail.EventRecapsSent() {
		readers = append(readers, recap.To)
	}
	sort.Strings(readers)
	return readers
}

// wantRecapReaders fails unless exactly these addresses have been mailed a
// recap so far, one mail each.
func wantRecapReaders(t *testing.T, when string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if got := recapReaders(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s: recaps went to %q, want %q", when, got, want)
	}
}

// unownedEvent makes a published Event over at the end of day 0 with no Event
// assignment at all.
func unownedEvent(t *testing.T, env *testEnv, admin, name string) string {
	t.Helper()
	eventID := createDraftEvent(t, env, admin, name, recapSlug(name))
	over := recapAt(0, 23, 0)
	setEventDates(t, env, admin, eventID, name, over.Add(-3*time.Hour), &over)
	createTicketType(t, env, admin, eventID)
	publishRecapEvent(t, env, admin, eventID)
	return eventID
}

// setMemberRole changes a Member's Organization role through the staff API.
func setMemberRole(t *testing.T, env *testEnv, admin, memberID, role string) {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/members/"+memberID, map[string]string{"role": role}, authHeader(admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set member role %s status=%d error=%+v", role, resp.StatusCode, body.Error)
	}
}

// An Event with no Event Owner goes to every Org Admin of its Organization,
// each in their own Staff Locale and English for one who stored none. Event
// Staff on the Event, an Event Owner by Organization role who is not assigned
// to it, and a Platform Operator are not written to.
func TestEventRecapGoesToEveryOrgAdminWhenTheEventHasNoEventOwner(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	eventID := unownedEvent(t, env, admin, "Jazz Night")

	_, segunda := addRecapMember(t, env, admin, "segunda@example.com", "org_admin")
	setStaffLocale(t, env, segunda, "es")
	addRecapMember(t, env, admin, "unassigned@example.com", "event_owner")
	staffID, _ := addRecapMember(t, env, admin, "door@example.com", "event_staff")
	assignToEvent(t, env, admin, eventID, staffID, "event_staff")
	operatorSession(t, env, "operator@example.com")
	sharedEmail.Reset()

	result := sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	if result.Recapped != 1 || result.Sent != 2 {
		t.Fatalf("sweep = %+v, want one Event recapped in two mails", result)
	}
	wantRecapReaders(t, "the morning after", "admin@example.com", "segunda@example.com")

	for _, recap := range sharedEmail.EventRecapsSent() {
		want := map[string]platform.Locale{
			"admin@example.com":   platform.LocaleEN,
			"segunda@example.com": platform.LocaleES,
		}[recap.To]
		if recap.Locale != want {
			t.Errorf("%s got locale %q, want %q", recap.To, recap.Locale, want)
		}
	}
}

// An Event with an Event Owner goes to the Event Owner alone: the Org Admins
// are not written to as well.
func TestEventRecapDoesNotGoToTheOrgAdminsWhenTheEventHasAnEventOwner(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	addRecapMember(t, env, f.admin, "segunda@example.com", "org_admin")
	f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))
	sharedEmail.Reset()

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	wantRecapReaders(t, "the morning after", "owner@example.com")
}

// A Member who is an Event Owner of the Event and an Org Admin of the
// Organization - assigned, then promoted, which keeps the assignment - gets
// one copy.
func TestEventRecapGoesOnceToAnEventOwnerWhoIsAlsoAnOrgAdmin(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))
	setMemberRole(t, env, f.admin, f.ownerID, "org_admin")
	sharedEmail.Reset()

	if result := sweepEventRecapsAt(t, env, recapAt(1, 9, 0)); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one mail", result)
	}
	wantRecapReaders(t, "the morning after", "owner@example.com")
}

// Event Staff and Platform Operators are never written to, with an Event
// Owner or without one.
func TestEventRecapNeverGoesToEventStaffOrAPlatformOperator(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	owned := f.publishedEvent(t, env, "Owned", recapAt(0, 23, 0))
	unowned := unownedEvent(t, env, f.admin, "Unowned")

	staffID, _ := addRecapMember(t, env, f.admin, "door@example.com", "event_staff")
	assignToEvent(t, env, f.admin, owned, staffID, "event_staff")
	assignToEvent(t, env, f.admin, unowned, staffID, "event_staff")
	operatorSession(t, env, "operator@example.com")
	sharedEmail.Reset()

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	wantRecapReaders(t, "the morning after", "admin@example.com", "owner@example.com")
}

// The readers are those assigned when the mail goes, not when the Event
// ended: an Event Owner assigned the morning after receives it, and one whose
// assignment was removed that morning does not.
func TestEventRecapGoesToTheEventOwnersAssignedWhenItIsSent(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))
	lateID, _ := addRecapMember(t, env, f.admin, "late@example.com", "event_owner")
	sharedEmail.Reset()

	// The Event is over; it is 08:00 the morning after.
	moveClockTo(t, recapAt(1, 8, 0))
	assignToEvent(t, env, f.admin, eventID, lateID, "event_owner")
	resp, body := env.deleteJSON(t, "/api/v1/staff/events/"+eventID+"/assignments/"+f.ownerID, nil, authHeader(f.admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove assignment status=%d error=%+v", resp.StatusCode, body.Error)
	}

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	wantRecapReaders(t, "the morning after", "late@example.com")
}

// An Event that resolves to nobody - no Event Owner, and no Org Admin - sends
// nothing and is recorded as recapped, so it is not retried every morning: a
// later sweep sends nothing for it, even once somebody could be written to.
func TestEventRecapForAnEventThatResolvesToNobodySendsNothingEver(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	unownedEvent(t, env, admin, "Jazz Night")
	// The staff API keeps an Organization's last Org Admin, so the row is
	// written directly: the rule must hold for it all the same.
	if _, err := env.db.Exec(`UPDATE members SET role = 'event_owner' WHERE email = 'admin@example.com'`); err != nil {
		t.Fatalf("demote the Org Admin: %v", err)
	}
	sharedEmail.Reset()

	result := sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	if result.Due != 1 || result.Sent != 0 || result.Recapped != 0 || result.Unaddressed != 1 {
		t.Fatalf("first sweep = %+v, want one Event due, recorded with nobody to write to", result)
	}
	wantRecapReaders(t, "the morning after")

	if _, err := env.db.Exec(`UPDATE members SET role = 'org_admin' WHERE email = 'admin@example.com'`); err != nil {
		t.Fatalf("restore the Org Admin: %v", err)
	}
	if result := sweepEventRecapsAt(t, env, recapAt(2, 9, 0)); result.Due != 0 || result.Sent != 0 {
		t.Fatalf("later sweep = %+v, want nothing due and nothing sent", result)
	}
	wantRecapReaders(t, "the morning after next")
}

// A House Organization's Events are recapped like any other's.
func TestEventRecapIsSentForAHouseOrganizationsEvents(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "House Night", recapAt(0, 23, 0))
	unownedEvent(t, env, f.admin, "House Unowned")
	operator := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operator, organizationIDBySlug(t, testOrgSlug))
	sharedEmail.Reset()

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	wantRecapReaders(t, "the morning after", "admin@example.com", "owner@example.com")
}
