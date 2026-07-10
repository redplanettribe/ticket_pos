package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func createDraftEvent(t *testing.T, env *testEnv, sessionID, name, slug string) string {
	t.Helper()
	_, body := env.post(t, "/api/v1/staff/events", map[string]string{
		"name": name,
		"slug": slug,
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("create event error=%+v", body.Error)
	}
	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return event.ID
}

func createTicketType(t *testing.T, env *testEnv, sessionID, eventID string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        "GA",
		"price_cents": 1000,
		"capacity":    50,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var ticketType struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &ticketType); err != nil {
		t.Fatalf("decode ticket type: %v", err)
	}
	return ticketType.ID
}

func makeEventPublishable(t *testing.T, env *testEnv, sessionID, eventID string) {
	t.Helper()
	startsAt := env.fixedClock.Add(24 * time.Hour).Format(time.RFC3339)
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":      "Publishable Event",
		"slug":      "publishable-event",
		"starts_at": startsAt,
		"timezone":  "America/New_York",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	createTicketType(t, env, sessionID, eventID)
}

func TestCatalogCreateListGetPatchDeleteDraftEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "Summer Fest",
		"slug": "summer-fest",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var created struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if created.Name != "Summer Fest" || created.Slug != "summer-fest" || created.Status != "draft" {
		t.Fatalf("created event=%+v", created)
	}

	resp, body = env.get(t, "/api/v1/staff/events", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var listed []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body.Data, &listed); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Status != "draft" {
		t.Fatalf("listed events=%+v", listed)
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+created.ID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	startsAt := env.fixedClock.Add(24 * time.Hour).Format(time.RFC3339)
	resp, body = env.patch(t, "/api/v1/staff/events/"+created.ID, map[string]any{
		"name":          "Summer Festival",
		"slug":          "summer-festival",
		"starts_at":     startsAt,
		"timezone":      "America/New_York",
		"venue_name":    "Main Hall",
		"venue_address": "123 Main St",
		"description":   "A **great** show",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var updated struct {
		Name        string  `json:"name"`
		Slug        string  `json:"slug"`
		Timezone    *string `json:"timezone"`
		VenueName   *string `json:"venue_name"`
		Description *string `json:"description"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.Name != "Summer Festival" || updated.Slug != "summer-festival" {
		t.Fatalf("updated event=%+v", updated)
	}
	if updated.Timezone == nil || *updated.Timezone != "America/New_York" {
		t.Fatalf("timezone=%v", updated.Timezone)
	}
	if updated.VenueName == nil || *updated.VenueName != "Main Hall" {
		t.Fatalf("venue_name=%v", updated.VenueName)
	}
	if updated.Description == nil || *updated.Description != "A **great** show" {
		t.Fatalf("description=%v", updated.Description)
	}

	resp, body = env.deleteJSON(t, "/api/v1/staff/events/"+created.ID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+created.ID, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("expected EVENT_NOT_FOUND, got %+v", body.Error)
	}
}

func TestCatalogEventPublishCancelLifecycle(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	eventID := createDraftEvent(t, env, sessionID, "Lifecycle Fest", "lifecycle-fest")
	makeEventPublishable(t, env, sessionID, eventID)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var published struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body.Data, &published); err != nil {
		t.Fatalf("decode published event: %v", err)
	}
	if published.Status != "published" {
		t.Fatalf("expected published status, got %+v", published)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on republish, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_ALREADY_PUBLISHED" {
		t.Fatalf("expected EVENT_ALREADY_PUBLISHED, got %+v", body.Error)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":      "Lifecycle Fest",
		"slug":      "new-slug",
		"starts_at": env.fixedClock.Add(24 * time.Hour).Format(time.RFC3339),
		"timezone":  "America/New_York",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on slug change, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_DRAFT" {
		t.Fatalf("expected EVENT_NOT_DRAFT, got %+v", body.Error)
	}

	resp, body = env.deleteJSON(t, "/api/v1/staff/events/"+eventID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on delete published, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_DELETE_FORBIDDEN" {
		t.Fatalf("expected EVENT_DELETE_FORBIDDEN, got %+v", body.Error)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/cancel", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body.Data, &cancelled); err != nil {
		t.Fatalf("decode cancelled event: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("expected cancelled status, got %+v", cancelled)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/cancel", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on recancel, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_ALREADY_CANCELLED" {
		t.Fatalf("expected EVENT_ALREADY_CANCELLED, got %+v", body.Error)
	}

	resp, body = env.deleteJSON(t, "/api/v1/staff/events/"+eventID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on delete cancelled, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_DELETE_FORBIDDEN" {
		t.Fatalf("expected EVENT_DELETE_FORBIDDEN, got %+v", body.Error)
	}
}

func TestCatalogEventPublishRequirementsNotMet(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	eventID := createDraftEvent(t, env, sessionID, "Incomplete", "incomplete")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_PUBLISH_REQUIREMENTS_NOT_MET" {
		t.Fatalf("expected EVENT_PUBLISH_REQUIREMENTS_NOT_MET, got %+v", body.Error)
	}
	assertMissingFields(t, body.Error.Details, "starts_at", "timezone", "ticket_types")

	startsAt := env.fixedClock.Add(24 * time.Hour).Format(time.RFC3339)
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":      "Incomplete",
		"slug":      "incomplete",
		"starts_at": startsAt,
		"timezone":  "America/New_York",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_PUBLISH_REQUIREMENTS_NOT_MET" {
		t.Fatalf("expected EVENT_PUBLISH_REQUIREMENTS_NOT_MET, got %+v", body.Error)
	}
	assertMissingFields(t, body.Error.Details, "ticket_types")
}

func assertMissingFields(t *testing.T, details any, expected ...string) {
	t.Helper()
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	var parsed struct {
		MissingFields []string `json:"missing_fields"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if len(parsed.MissingFields) != len(expected) {
		t.Fatalf("missing_fields=%v want %v", parsed.MissingFields, expected)
	}
	for i, field := range expected {
		if parsed.MissingFields[i] != field {
			t.Fatalf("missing_fields=%v want %v", parsed.MissingFields, expected)
		}
	}
}

func TestCatalogEventSlugTaken(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	_, body := env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "First",
		"slug": "shared-slug",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("create first event error=%+v", body.Error)
	}

	resp, body := env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "Second",
		"slug": "shared-slug",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_SLUG_TAKEN" {
		t.Fatalf("expected EVENT_SLUG_TAKEN, got %+v", body.Error)
	}
}

func TestCatalogForbiddenForNonOrgAdmin(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "staff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := verifyOTP(t, env, "staff@example.com")

	resp, body = env.get(t, "/api/v1/staff/events", authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %+v", body.Error)
	}
}

func TestTicketTypeCRUDOnDraftEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	_, body := env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "Summer Fest",
		"slug": "summer-fest",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("create event error=%+v", body.Error)
	}

	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	resp, body := env.post(t, "/api/v1/staff/events/"+event.ID+"/ticket-types", map[string]any{
		"name":        "General Admission",
		"description": "Standing room",
		"price_cents": 2500,
		"capacity":    100,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var created struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		PriceCents int     `json:"price_cents"`
		Currency   string  `json:"currency"`
		Capacity   int     `json:"capacity"`
		SoldCount  int     `json:"sold_count"`
		SortOrder  int     `json:"sort_order"`
		Description *string `json:"description"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode ticket type: %v", err)
	}
	if created.Name != "General Admission" || created.PriceCents != 2500 || created.Currency != "USD" {
		t.Fatalf("created ticket type=%+v", created)
	}
	if created.Capacity != 100 || created.SoldCount != 0 || created.SortOrder != 0 {
		t.Fatalf("created ticket type=%+v", created)
	}
	if created.Description == nil || *created.Description != "Standing room" {
		t.Fatalf("description=%v", created.Description)
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+event.ID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var listed []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body.Data, &listed); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed ticket types=%+v", listed)
	}

	resp, body = env.patch(t, "/api/v1/staff/events/"+event.ID+"/ticket-types/"+created.ID, map[string]any{
		"name":        "GA",
		"description": "Updated",
		"price_cents": 3000,
		"capacity":    120,
		"sort_order":  1,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var updated struct {
		Name       string `json:"name"`
		PriceCents int    `json:"price_cents"`
		Capacity   int    `json:"capacity"`
		SortOrder  int    `json:"sort_order"`
	}
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated ticket type: %v", err)
	}
	if updated.Name != "GA" || updated.PriceCents != 3000 || updated.Capacity != 120 || updated.SortOrder != 1 {
		t.Fatalf("updated ticket type=%+v", updated)
	}

	resp, body = env.deleteJSON(t, "/api/v1/staff/events/"+event.ID+"/ticket-types/"+created.ID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+event.ID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list after delete status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &listed); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected empty list, got %+v", listed)
	}
}

func TestTicketTypeDeleteForbiddenOnPublishedEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	eventID := createDraftEvent(t, env, sessionID, "Published Show", "published-show")
	ticketTypeID := createTicketType(t, env, sessionID, eventID)
	makeEventPublishable(t, env, sessionID, eventID)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.deleteJSON(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_TYPE_DELETE_FORBIDDEN" {
		t.Fatalf("expected TICKET_TYPE_DELETE_FORBIDDEN, got %+v", body.Error)
	}
}

func TestOrganizationCurrencyDefaultUpdateAndLock(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	resp, body := env.get(t, "/api/v1/staff/organization", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get organization status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var org struct {
		Currency       string `json:"currency"`
		CurrencyLocked bool   `json:"currency_locked"`
	}
	if err := json.Unmarshal(body.Data, &org); err != nil {
		t.Fatalf("decode organization: %v", err)
	}
	if org.Currency != "USD" || org.CurrencyLocked {
		t.Fatalf("organization=%+v", org)
	}

	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]string{
		"name":     "Test Org",
		"currency": "EUR",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch currency status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &org); err != nil {
		t.Fatalf("decode organization: %v", err)
	}
	if org.Currency != "EUR" || org.CurrencyLocked {
		t.Fatalf("updated organization=%+v", org)
	}

	_, body = env.post(t, "/api/v1/staff/events", map[string]string{
		"name": "Currency Lock Event",
		"slug": "currency-lock-event",
	}, authHeader(sessionID))
	if body.Error != nil {
		t.Fatalf("create event error=%+v", body.Error)
	}

	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+event.ID+"/ticket-types", map[string]any{
		"name":        "GA",
		"price_cents": 1000,
		"capacity":    10,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/staff/organization", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if err := json.Unmarshal(body.Data, &org); err != nil {
		t.Fatalf("decode organization: %v", err)
	}
	if !org.CurrencyLocked {
		t.Fatalf("expected currency_locked=true, got %+v", org)
	}

	resp, body = env.patch(t, "/api/v1/staff/organization", map[string]string{
		"name":     "Test Org",
		"currency": "GBP",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "CURRENCY_LOCKED" {
		t.Fatalf("expected CURRENCY_LOCKED, got %+v", body.Error)
	}
}
