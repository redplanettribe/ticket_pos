package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Named Tickets on the Event (#667, parent #665, ADR 0076): the setting that
// makes a Storefront checkout name a Holder and answer the required Ticket
// Questions for every Ticket. This ticket only makes the setting exist - it is
// read, written and published, and nothing at checkout consults it yet.

const namedTicketsMigration = "125_event_requires_named_tickets.sql"

type eventNamedTicketsView struct {
	Status               string `json:"status"`
	RequiresNamedTickets *bool  `json:"requires_named_tickets"`
}

// getEventNamedTickets reads the setting off the staff Event read model,
// failing when the key is missing: a decoder that turned an absent key into
// false would pass the "existing Events read off" test for the wrong reason.
func getEventNamedTickets(t *testing.T, env *testEnv, sessionID, eventID string) bool {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeNamedTickets(t, body.Data)
}

func decodeNamedTickets(t *testing.T, data json.RawMessage) bool {
	t.Helper()
	var view eventNamedTicketsView
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if view.RequiresNamedTickets == nil {
		t.Fatalf("the Event read carries no requires_named_tickets: %s", data)
	}
	return *view.RequiresNamedTickets
}

// patchEventNamedTickets sends the Event form's payload with a
// requires_named_tickets value; nil omits the key, as a caller that does not
// know the setting would.
func patchEventNamedTickets(t *testing.T, env *testEnv, sessionID, eventID, slug string, requires *bool) (*http.Response, envelope) {
	t.Helper()
	body := map[string]any{
		"name":      "Named Event",
		"slug":      slug,
		"starts_at": env.fixedClock.Add(10 * 24 * time.Hour).Format(time.RFC3339),
		"timezone":  "America/Guayaquil",
	}
	if requires != nil {
		body["requires_named_tickets"] = *requires
	}
	return env.patch(t, "/api/v1/staff/events/"+eventID, body, authHeader(sessionID))
}

func publicEventNamedTickets(t *testing.T, env *testEnv, eventSlug string) bool {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event %s status=%d error=%+v", eventSlug, resp.StatusCode, body.Error)
	}
	return decodeNamedTickets(t, body.Data)
}

func TestANewEventRequiresNamedTickets(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Named Event", "named-event")

	if !getEventNamedTickets(t, env, sessionID, eventID) {
		t.Fatal("a newly created Event reads as not requiring Named Tickets; the product default is on")
	}
}

// The migration is replayed over a staged Event, the way the deployment ran it
// over production: an Event that existed before the column did reads off, and
// one created afterwards reads on. The column is dropped first so the file the
// deployment reads runs exactly as written; the replay puts it back.
func TestAnEventThatExistedBeforeTheMigrationDoesNotRequireNamedTickets(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	existing := createDraftEvent(t, env, sessionID, "Old Event", "old-event")

	t.Cleanup(func() {
		// A failure between the drop and the replay must not leave the shared
		// schema without the column for every test after this one.
		if _, err := env.db.Exec(`ALTER TABLE events ADD COLUMN IF NOT EXISTS requires_named_tickets BOOLEAN NOT NULL DEFAULT TRUE`); err != nil {
			t.Errorf("restore requires_named_tickets: %v", err)
		}
	})
	if _, err := env.db.Exec(`ALTER TABLE events DROP COLUMN requires_named_tickets`); err != nil {
		t.Fatalf("drop requires_named_tickets: %v", err)
	}
	executeMigration(t, env, namedTicketsMigration)

	if getEventNamedTickets(t, env, sessionID, existing) {
		t.Fatal("an Event that existed before the migration requires Named Tickets; its live checkout would change on deploy")
	}
	fresh := createDraftEvent(t, env, sessionID, "New Event", "new-event")
	if !getEventNamedTickets(t, env, sessionID, fresh) {
		t.Fatal("an Event created after the migration does not require Named Tickets; the database default must be the product default")
	}
}

func TestAnOrgAdminSwitchesNamedTicketsOnAPublishedEventAndAnOmittingUpdateLeavesItAlone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishEvent(t, env, sessionID, "Named Event", "named-event", env.fixedClock.Add(10*24*time.Hour), false, 2500, 10)
	if !getEventNamedTickets(t, env, sessionID, eventID) {
		t.Fatal("the published Event does not start out requiring Named Tickets")
	}

	off := false
	resp, body := patchEventNamedTickets(t, env, sessionID, eventID, "named-event", &off)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("switch off status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if decodeNamedTickets(t, body.Data) {
		t.Fatal("the update's response still requires Named Tickets after switching it off")
	}
	if getEventNamedTickets(t, env, sessionID, eventID) {
		t.Fatal("switching Named Tickets off did not persist")
	}

	// An update that says nothing about the setting leaves it where it is,
	// rather than reverting it to the default the column would give a new row.
	if resp, body = patchEventNamedTickets(t, env, sessionID, eventID, "named-event", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("omitting update status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if getEventNamedTickets(t, env, sessionID, eventID) {
		t.Fatal("an update that omitted requires_named_tickets switched it back on")
	}

	on := true
	if resp, body = patchEventNamedTickets(t, env, sessionID, eventID, "named-event", &on); resp.StatusCode != http.StatusOK {
		t.Fatalf("switch on status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if !getEventNamedTickets(t, env, sessionID, eventID) {
		t.Fatal("switching Named Tickets back on did not persist")
	}
	if resp, body = patchEventNamedTickets(t, env, sessionID, eventID, "named-event", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("omitting update status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if !getEventNamedTickets(t, env, sessionID, eventID) {
		t.Fatal("an update that omitted requires_named_tickets switched it off")
	}
}

// The setting rides the Event update, and the Event update is the Org Admin's
// alone: an Event Owner and Event Staff are both refused, and the refusal
// leaves the setting untouched.
func TestOnlyAnOrgAdminSwitchesNamedTickets(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := publishEvent(t, env, adminSession, "Named Event", "named-event", env.fixedClock.Add(10*24*time.Hour), false, 2500, 10)

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(adminSession))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	off := false
	for _, member := range []struct{ email, role string }{
		{"owner@example.com", "event_owner"},
		{"doorstaff@example.com", "event_staff"},
	} {
		session := addMember(member.email, member.role)
		resp, body := patchEventNamedTickets(t, env, session, eventID, "named-event", &off)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s switching Named Tickets: status=%d error=%+v, want 403", member.role, resp.StatusCode, body.Error)
		}
	}
	if !getEventNamedTickets(t, env, adminSession, eventID) {
		t.Fatal("a refused update switched Named Tickets off")
	}
}

// The public Event read the Storefront checkout is drawn from carries the
// setting, so the checkout can render the form for it. An Event with External
// Registration sells nothing here, so the setting is ignored there and the
// public read says so whatever is stored.
func TestThePublicEventReadCarriesNamedTickets(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishEvent(t, env, sessionID, "Named Event", "named-event", env.fixedClock.Add(10*24*time.Hour), false, 2500, 10)

	if !publicEventNamedTickets(t, env, "named-event") {
		t.Fatal("the public Event read does not carry requires_named_tickets=true for an Event requiring it")
	}
	off := false
	if resp, body := patchEventNamedTickets(t, env, sessionID, eventID, "named-event", &off); resp.StatusCode != http.StatusOK {
		t.Fatalf("switch off status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if publicEventNamedTickets(t, env, "named-event") {
		t.Fatal("the public Event read still requires Named Tickets after the Org Admin switched it off")
	}

	externalID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")
	if !getEventNamedTickets(t, env, sessionID, externalID) {
		t.Fatal("the external Event's stored setting is not the default; this test cannot tell ignoring it from never having it")
	}
	if publicEventNamedTickets(t, env, "external-event") {
		t.Fatal("the public read of an External Registration Event requires Named Tickets; the setting is ignored there")
	}
}
