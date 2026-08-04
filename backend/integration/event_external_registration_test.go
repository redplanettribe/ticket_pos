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
