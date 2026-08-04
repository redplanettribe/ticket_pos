package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// External Registration on the Event (issue #207): the explicit choice that an
// Event registers its audience somewhere else instead of selling Ticket Types,
// and the Registration Link it hands them (ADR 0028). This ticket lands the
// mode, the link and the exclusivity guard on both sides; publishing, the
// Storefront and the click counter arrive in the tickets that follow.

type eventRegistrationView struct {
	Status                 string  `json:"status"`
	RegistrationMode       string  `json:"registration_mode"`
	RegistrationURL        *string `json:"registration_url"`
	RegistrationClickCount int64   `json:"registration_click_count"`
}

func getEventRegistration(t *testing.T, env *testEnv, sessionID, eventID string) eventRegistrationView {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view eventRegistrationView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return view
}

// patchEventRegistration sends the Event form's payload carrying whichever of
// the registration fields the caller names; the rest of the form is the same
// every time so the assertions are about the mode and the link alone.
func patchEventRegistration(
	t *testing.T,
	env *testEnv,
	sessionID, eventID string,
	fields map[string]any,
) (*http.Response, envelope) {
	t.Helper()
	body := map[string]any{
		"name":      "External Event",
		"slug":      "external-event",
		"starts_at": env.fixedClock.Add(72 * time.Hour).Format(time.RFC3339),
		"timezone":  "America/Guayaquil",
	}
	for k, v := range fields {
		body[k] = v
	}
	return env.patch(t, "/api/v1/staff/events/"+eventID, body, authHeader(sessionID))
}

func TestEventRegistrationModeDefaultsToTicketsWithNoRegistrationLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "External Event", "external-event")

	view := getEventRegistration(t, env, sessionID, eventID)
	if view.RegistrationMode != "tickets" {
		t.Fatalf("new event registration_mode = %q; want tickets", view.RegistrationMode)
	}
	if view.RegistrationURL != nil {
		t.Fatalf("new event registration_url = %v; want null", *view.RegistrationURL)
	}
	// The click count is on the staff payload from the start, so the Event page
	// never has to distinguish "no clicks yet" from "the platform is not counting".
	if view.RegistrationClickCount != 0 {
		t.Fatalf("new event registration_click_count = %d; want 0", view.RegistrationClickCount)
	}
}

func TestEventCreatedWithExternalRegistrationAndARegistrationLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/events", map[string]any{
		"name":              "External Event",
		"slug":              "external-event",
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/my-meetup",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var created struct {
		ID string `json:"id"`
		eventRegistrationView
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if created.RegistrationMode != "external" {
		t.Fatalf("created registration_mode = %q; want external", created.RegistrationMode)
	}
	if created.RegistrationURL == nil || *created.RegistrationURL != "https://lu.ma/my-meetup" {
		t.Fatalf("created registration_url = %v; want the submitted link", created.RegistrationURL)
	}

	persisted := getEventRegistration(t, env, sessionID, created.ID)
	if persisted.RegistrationMode != "external" || persisted.RegistrationURL == nil {
		t.Fatalf("persisted registration = %+v; want external with a link", persisted)
	}
}

func TestEventCreatedWithExternalRegistrationBeforeTheRegistrationLinkIsKnown(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// The mode is what the organizer chose, the link is what they typed: a draft
	// that has decided on External Registration but has no URL yet is legitimate.
	resp, body := env.post(t, "/api/v1/staff/events", map[string]any{
		"name":              "External Event",
		"slug":              "external-event",
		"registration_mode": "external",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	view := getEventRegistration(t, env, sessionID, created.ID)
	if view.RegistrationMode != "external" {
		t.Fatalf("registration_mode = %q; want external", view.RegistrationMode)
	}
	if view.RegistrationURL != nil {
		t.Fatalf("registration_url = %v; want null on a draft with no link yet", *view.RegistrationURL)
	}
}

func TestDraftEventSwitchedToExternalRegistrationWithARegistrationLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "External Event", "external-event")

	resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_mode": "external",
		"registration_url":  "https://www.eventbrite.com/e/123",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch registration status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var updated eventRegistrationView
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if updated.RegistrationMode != "external" {
		t.Fatalf("patched registration_mode = %q; want external", updated.RegistrationMode)
	}
	if updated.RegistrationURL == nil || *updated.RegistrationURL != "https://www.eventbrite.com/e/123" {
		t.Fatalf("patched registration_url = %v; want the submitted link", updated.RegistrationURL)
	}

	// The Registration Link is repointable while the Event is a draft.
	if resp, body = patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_url": "https://lu.ma/rescheduled",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("repoint registration_url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	persisted := getEventRegistration(t, env, sessionID, eventID)
	if persisted.RegistrationURL == nil || *persisted.RegistrationURL != "https://lu.ma/rescheduled" {
		t.Fatalf("persisted registration_url = %v; want the repointed link", persisted.RegistrationURL)
	}
	if persisted.RegistrationMode != "external" {
		t.Fatalf("registration_mode after a link-only update = %q; want external", persisted.RegistrationMode)
	}

	// A form that says nothing about either field leaves both alone rather than
	// silently reverting the Event to selling tickets.
	if resp, body = patchEventRegistration(t, env, sessionID, eventID, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch without registration fields status=%d error=%+v", resp.StatusCode, body.Error)
	}
	persisted = getEventRegistration(t, env, sessionID, eventID)
	if persisted.RegistrationMode != "external" || persisted.RegistrationURL == nil {
		t.Fatalf("registration after an omitting update = %+v; want it untouched", persisted)
	}

	// And back to selling Ticket Types. The mode is what the organizer chose and
	// the link is what they typed (ADR 0028), so the link they typed survives the
	// flip — every consumer branches on the mode, never on the link's presence.
	if resp, body = patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_mode": "tickets",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch back to tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	persisted = getEventRegistration(t, env, sessionID, eventID)
	if persisted.RegistrationMode != "tickets" {
		t.Fatalf("registration_mode = %q; want tickets", persisted.RegistrationMode)
	}

	// An empty Registration Link is how the form clears it.
	if resp, body = patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_url": "",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("clear registration_url status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := getEventRegistration(t, env, sessionID, eventID).RegistrationURL; got != nil {
		t.Fatalf("registration_url after an empty submission = %v; want null", *got)
	}
}

func TestEventRegistrationLinkRefusesEverySchemeButHTTPS(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "External Event", "external-event")

	// The scheme allowlist is what keeps an executable or inline payload out of a
	// value destined for both an href and a Location header, and it is what keeps
	// a Customer from being downgraded mid-journey.
	refused := []string{
		"http://lu.ma/my-meetup",
		"javascript:alert(document.cookie)",
		"JavaScript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"ftp://example.com/registration",
		"//lu.ma/my-meetup",
		"lu.ma/my-meetup",
		"https://",
	}
	for _, url := range refused {
		resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
			"registration_mode": "external",
			"registration_url":  url,
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("registration_url %q status=%d; want 400", url, resp.StatusCode)
		}
		if field, ok := fieldErrorsByName(t, body)["registration_url"]; !ok {
			t.Fatalf("registration_url %q: no field error blaming registration_url", url)
		} else if field.Code != "INVALID_REGISTRATION_URL" {
			t.Fatalf("registration_url %q code=%q; want INVALID_REGISTRATION_URL", url, field.Code)
		}
	}

	// The Event is untouched by every one of those refusals.
	view := getEventRegistration(t, env, sessionID, eventID)
	if view.RegistrationMode != "tickets" || view.RegistrationURL != nil {
		t.Fatalf("registration after refused updates = %+v; want the untouched default", view)
	}

	// No host allowlist: any https host is accepted.
	for _, url := range []string{
		"https://lu.ma/my-meetup",
		"https://docs.google.com/forms/d/e/abc/viewform",
		"https://registration.some-obscure-host.example/sign-up?ref=1#top",
	} {
		resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
			"registration_mode": "external",
			"registration_url":  url,
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("registration_url %q status=%d error=%+v; want 200", url, resp.StatusCode, body.Error)
		}
	}
}

func TestEventRegistrationModeRefusesAnUnknownMode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "External Event", "external-event")

	resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_mode": "luma",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown registration_mode status=%d error=%+v; want 400", resp.StatusCode, body.Error)
	}
	if field, ok := fieldErrorsByName(t, body)["registration_mode"]; !ok {
		t.Fatalf("no field error blaming registration_mode: %+v", body.Error)
	} else if field.Code != "INVALID_REGISTRATION_MODE" {
		t.Fatalf("registration_mode code=%q; want INVALID_REGISTRATION_MODE", field.Code)
	}
	if got := getEventRegistration(t, env, sessionID, eventID).RegistrationMode; got != "tickets" {
		t.Fatalf("registration_mode after a rejected update = %q; want the untouched tickets", got)
	}
}

func TestTicketTypeCreationRefusedOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "External Event", "external-event")

	resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/my-meetup",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("switch to external status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// The refusal names the mode rather than reading as a generic validation
	// failure, so the organizer understands the two modes are exclusive.
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        "GA",
		"price_cents": 1000,
		"capacity":    50,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("create ticket type on external event status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_IS_EXTERNAL_REGISTRATION" {
		t.Fatalf("create ticket type error=%+v; want EVENT_IS_EXTERNAL_REGISTRATION", body.Error)
	}
	if body.Data != nil && string(body.Data) != "null" {
		t.Fatalf("expected null data on a refusal, got %s", body.Data)
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var types []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &types); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	if len(types) != 0 {
		t.Fatalf("external event has %d ticket types; want none", len(types))
	}
}

func TestSwitchingToExternalRegistrationRefusedWhileTicketTypesExist(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "External Event", "external-event")
	ticketTypeID := createTicketType(t, env, sessionID, eventID)

	resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/my-meetup",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("switch to external with ticket types status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_HAS_TICKET_TYPES" {
		t.Fatalf("switch to external error=%+v; want EVENT_HAS_TICKET_TYPES", body.Error)
	}
	if got := getEventRegistration(t, env, sessionID, eventID).RegistrationMode; got != "tickets" {
		t.Fatalf("registration_mode after the refusal = %q; want the untouched tickets", got)
	}

	// Changing your mind before publishing is possible: delete the Ticket Types
	// and the same switch goes through.
	resp, body = env.deleteJSON(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/my-meetup",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("switch to external after deleting ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	view := getEventRegistration(t, env, sessionID, eventID)
	if view.RegistrationMode != "external" || view.RegistrationURL == nil {
		t.Fatalf("registration = %+v; want external with a link", view)
	}
}

func TestEventCreationRefusedWithExternalRegistrationAndABadRegistrationLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/events", map[string]any{
		"name":              "External Event",
		"slug":              "external-event",
		"registration_mode": "external",
		"registration_url":  "javascript:alert(1)",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create with a javascript link status=%d error=%+v; want 400", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("create error=%+v; want VALIDATION_FAILED", body.Error)
	}

	// Nothing was created by the refused request.
	resp, body = env.get(t, "/api/v1/staff/events", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var listed []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &listed); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed %d events after a refused create; want none", len(listed))
	}
}

// Publishing an externally registered Event (issue #208). The publish gate takes
// a Registration Link in place of Ticket Types, and publishing is the moment the
// mode sets: afterwards the mode is frozen in both directions and the link may be
// corrected but never emptied.

// createExternalEvent creates a draft Event that registers externally, with a
// Registration Link when one is given, and fills in everything else the publish
// gate asks for so the only open question is the registration pair.
func createPublishableExternalEvent(t *testing.T, env *testEnv, sessionID, registrationURL string) string {
	t.Helper()
	create := map[string]any{
		"name":              "External Event",
		"slug":              "external-event",
		"registration_mode": "external",
	}
	if registrationURL != "" {
		create["registration_url"] = registrationURL
	}
	resp, body := env.post(t, "/api/v1/staff/events", create, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create external event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	// patchEventRegistration carries the schedule and timezone the publish gate
	// requires of every Event, external or not.
	if resp, body = patchEventRegistration(t, env, sessionID, created.ID, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch external event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return created.ID
}

func publishExternalRegistrationEvent(t *testing.T, env *testEnv, sessionID, registrationURL string) string {
	t.Helper()
	eventID := createPublishableExternalEvent(t, env, sessionID, registrationURL)
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish external event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return eventID
}

func TestExternalEventPublishesWithARegistrationLinkAndNoTicketTypes(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createPublishableExternalEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish status=%d error=%+v; want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("publish error=%+v; want none", body.Error)
	}
	var published eventRegistrationView
	if err := json.Unmarshal(body.Data, &published); err != nil {
		t.Fatalf("decode published event: %v", err)
	}
	if published.Status != "published" {
		t.Fatalf("status = %q; want published", published.Status)
	}
	if published.RegistrationMode != "external" || published.RegistrationURL == nil {
		t.Fatalf("published registration = %+v; want external with its link", published)
	}

	// It really has no Ticket Types: the Registration Link stood in for them.
	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var types []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &types); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	if len(types) != 0 {
		t.Fatalf("published external event has %d ticket types; want none", len(types))
	}
}

func TestExternalEventPublishRefusedWithoutARegistrationLink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createPublishableExternalEvent(t, env, sessionID, "")

	// The missing field names the Registration Link, not ticket types: an
	// organizer told to add a ticket type to an Event that sells none has been
	// sent to fix the wrong thing.
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("publish without a link status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_PUBLISH_REQUIREMENTS_NOT_MET" {
		t.Fatalf("publish error=%+v; want EVENT_PUBLISH_REQUIREMENTS_NOT_MET", body.Error)
	}
	assertMissingFields(t, body.Error.Details, "registration_url")

	// Adding the link is the whole fix.
	if resp, body = patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_url": "https://lu.ma/my-meetup",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("add registration link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("publish after adding the link status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestExternalEventPublishStillRequiresTheEventsOwnFields(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// An Event created but never filled in: no schedule, no timezone, and no
	// Registration Link either.
	resp, body := env.post(t, "/api/v1/staff/events", map[string]any{
		"name":              "External Event",
		"slug":              "external-event",
		"registration_mode": "external",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create external event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+created.ID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("publish status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_PUBLISH_REQUIREMENTS_NOT_MET" {
		t.Fatalf("publish error=%+v; want EVENT_PUBLISH_REQUIREMENTS_NOT_MET", body.Error)
	}
	assertMissingFields(t, body.Error.Details, "starts_at", "timezone", "registration_url")
}

func TestTicketedEventPublishStillRefusedWithoutTicketTypes(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "External Event", "external-event")

	// The regression guard: External Registration changed nothing for an Event
	// that sells tickets here, and the refusal still names ticket types.
	if resp, body := patchEventRegistration(t, env, sessionID, eventID, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("publish status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_PUBLISH_REQUIREMENTS_NOT_MET" {
		t.Fatalf("publish error=%+v; want EVENT_PUBLISH_REQUIREMENTS_NOT_MET", body.Error)
	}
	assertMissingFields(t, body.Error.Details, "ticket_types")
}

func TestRegistrationLinkEditableOnAPublishedEventAndTheClickCountSurvives(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/typo")

	// The public click endpoint could reach this count, but only one hand-off at
	// a time and only via the Event's slugs, which this helper does not return.
	// Seeding it directly keeps the test about what an edit preserves rather
	// than about how the counter got there — that is registration_link_clicks_test.go's job.
	if _, err := env.db.ExecContext(t.Context(),
		`UPDATE events SET registration_click_count = 17 WHERE id = $1`, eventID,
	); err != nil {
		t.Fatalf("seed click count: %v", err)
	}

	resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_url": "https://lu.ma/rescheduled",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit link on a published event status=%d error=%+v; want 200", resp.StatusCode, body.Error)
	}

	view := getEventRegistration(t, env, sessionID, eventID)
	if view.RegistrationURL == nil || *view.RegistrationURL != "https://lu.ma/rescheduled" {
		t.Fatalf("registration_url = %v; want the corrected link", view.RegistrationURL)
	}
	// Correcting a typo is not a reason to forget how many people the Event has
	// already handed over.
	if view.RegistrationClickCount != 17 {
		t.Fatalf("registration_click_count = %d after an edit; want the untouched 17", view.RegistrationClickCount)
	}
	if view.Status != "published" {
		t.Fatalf("status = %q; want published", view.Status)
	}
}

func TestRegistrationLinkCannotBeClearedOnAPublishedEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	// Blank in any spelling the form can produce is refused: the guard is on the
	// state the update would leave behind, so a published external Event can
	// never end a request with nowhere to send its audience.
	for _, submitted := range []any{"", "   "} {
		resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
			"registration_url": submitted,
		})
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("clear link with %#v status=%d error=%+v; want 409", submitted, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "EVENT_REGISTRATION_URL_REQUIRED" {
			t.Fatalf("clear link with %#v error=%+v; want EVENT_REGISTRATION_URL_REQUIRED", submitted, body.Error)
		}
		if body.Data != nil && string(body.Data) != "null" {
			t.Fatalf("expected null data on a refusal, got %s", body.Data)
		}
	}

	// A JSON null is how this API says nothing about a field (#207), so it cannot
	// empty the link either — it leaves the Event exactly as it was.
	resp, body := patchEventRegistration(t, env, sessionID, eventID, map[string]any{
		"registration_url": nil,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("null registration_url status=%d error=%+v; want 200", resp.StatusCode, body.Error)
	}

	// The link the Customers are following is still there.
	view := getEventRegistration(t, env, sessionID, eventID)
	if view.RegistrationURL == nil || *view.RegistrationURL != "https://lu.ma/my-meetup" {
		t.Fatalf("registration_url after the refusals = %v; want the untouched link", view.RegistrationURL)
	}
}

// The Storefront Event page of an externally registered Event (issue #209). The
// public payload is what tells the page which of two Events it is looking at,
// and it carries the Registration Link itself because the Register panel names
// the destination's hostname before a Customer clicks it.

type publicEventRegistrationView struct {
	Slug             string  `json:"slug"`
	RegistrationMode string  `json:"registration_mode"`
	RegistrationURL  *string `json:"registration_url"`
	TicketTypes      []struct {
		Name string `json:"name"`
	} `json:"ticket_types"`
}

func getPublicEventRegistration(t *testing.T, env *testEnv, slug string) publicEventRegistrationView {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/"+slug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event %q status=%d error=%+v", slug, resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("public event %q error=%+v; want none", slug, body.Error)
	}
	var view publicEventRegistrationView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	return view
}

func TestPublicEventDetailCarriesTheRegistrationModeAndLinkForAnExternalEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	view := getPublicEventRegistration(t, env, "external-event")
	if view.RegistrationMode != "external" {
		t.Fatalf("public registration_mode = %q; want external", view.RegistrationMode)
	}
	// The URL itself and not merely the fact of it: the Storefront renders the
	// destination's hostname beneath the Register call to action.
	if view.RegistrationURL == nil || *view.RegistrationURL != "https://lu.ma/my-meetup" {
		t.Fatalf("public registration_url = %v; want the Registration Link", view.RegistrationURL)
	}
	if len(view.TicketTypes) != 0 {
		t.Fatalf("public external event has %d ticket types; want none", len(view.TicketTypes))
	}
}

func TestPublicEventDetailOfATicketedEventRegistersHereAndHandsNobodyAnywhere(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishEvent(t, env, sessionID, "Ticketed Event", "ticketed-event", env.fixedClock.Add(72*time.Hour), true, 4200, 10)

	view := getPublicEventRegistration(t, env, "ticketed-event")
	if view.RegistrationMode != "tickets" {
		t.Fatalf("public registration_mode = %q; want tickets", view.RegistrationMode)
	}
	if view.RegistrationURL != nil {
		t.Fatalf("public registration_url = %v; want null on an Event that sells here", *view.RegistrationURL)
	}
	if len(view.TicketTypes) != 1 {
		t.Fatalf("public ticketed event has %d ticket types; want 1", len(view.TicketTypes))
	}
}

func TestRegistrationModeFrozenOnAPublishedEventInBothDirections(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// External to ticketed: refused because it passes through the state the
	// publish gate exists to forbid — published with zero Ticket Types.
	externalID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")
	resp, body := patchEventRegistration(t, env, sessionID, externalID, map[string]any{
		"registration_mode": "tickets",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("external to tickets status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_REGISTRATION_MODE_LOCKED" {
		t.Fatalf("external to tickets error=%+v; want EVENT_REGISTRATION_MODE_LOCKED", body.Error)
	}
	if got := getEventRegistration(t, env, sessionID, externalID).RegistrationMode; got != "external" {
		t.Fatalf("registration_mode after the refusal = %q; want external", got)
	}

	// Resubmitting the mode it already has is not a change and goes through: the
	// Event form sends the whole registration pair on every save.
	if resp, body = patchEventRegistration(t, env, sessionID, externalID, map[string]any{
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/my-meetup",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("resubmit the same mode status=%d error=%+v; want 200", resp.StatusCode, body.Error)
	}

	// Ticketed to external: refused because it would orphan real Ticket Sales.
	ticketedID := createDraftEvent(t, env, sessionID, "Ticketed Event", "ticketed-event")
	createTicketType(t, env, sessionID, ticketedID)
	if resp, body = env.patch(t, "/api/v1/staff/events/"+ticketedID, map[string]any{
		"name":      "Ticketed Event",
		"slug":      "ticketed-event",
		"starts_at": env.fixedClock.Add(72 * time.Hour).Format(time.RFC3339),
		"timezone":  "America/Guayaquil",
	}, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch ticketed event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body = env.post(t, "/api/v1/staff/events/"+ticketedID+"/publish", nil, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("publish ticketed event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+ticketedID, map[string]any{
		"name":              "Ticketed Event",
		"slug":              "ticketed-event",
		"starts_at":         env.fixedClock.Add(72 * time.Hour).Format(time.RFC3339),
		"timezone":          "America/Guayaquil",
		"registration_mode": "external",
		"registration_url":  "https://lu.ma/my-meetup",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("tickets to external status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_REGISTRATION_MODE_LOCKED" {
		t.Fatalf("tickets to external error=%+v; want EVENT_REGISTRATION_MODE_LOCKED", body.Error)
	}
	if got := getEventRegistration(t, env, sessionID, ticketedID).RegistrationMode; got != "tickets" {
		t.Fatalf("registration_mode after the refusal = %q; want tickets", got)
	}
}
