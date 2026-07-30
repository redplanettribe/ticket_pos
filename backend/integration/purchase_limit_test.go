package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Purchase Limit: the most of one Ticket Type a single Customer may hold at
// once (CONTEXT.md, ADR 0025). This file covers the catalog half — setting,
// reading, clearing and rejecting the value. Nothing here enforces it; the
// refusal at begin-checkout and on Sale Import arrive with their own tickets,
// and extend this file.
//
// The whole behavioural question this slice turns on is the difference between
// "no Purchase Limit" and "a Purchase Limit of N", so every test below asserts
// on null as carefully as it asserts on a number.

// ticketTypeWithLimit is a staff Ticket Type read the way a client reads it:
// the Purchase Limit is a nullable integer, so it is a pointer, and absent is
// a first-class value rather than zero.
type ticketTypeWithLimit struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	PriceCents     int    `json:"price_cents"`
	Capacity       int    `json:"capacity"`
	MaxPerCustomer *int   `json:"max_per_customer"`
}

func decodeTicketTypeWithLimit(t *testing.T, body envelope) ticketTypeWithLimit {
	t.Helper()
	if body.Error != nil {
		t.Fatalf("ticket type error=%+v", body.Error)
	}
	var tt ticketTypeWithLimit
	if err := json.Unmarshal(body.Data, &tt); err != nil {
		t.Fatalf("decode ticket type: %v", err)
	}
	return tt
}

// assertNoPurchaseLimit fails unless the Ticket Type carries no Purchase Limit.
// Spelled out as a helper because "unset" is the state every existing Ticket
// Type is in, and a test that let a zero pass for it would prove nothing.
func assertNoPurchaseLimit(t *testing.T, tt ticketTypeWithLimit, where string) {
	t.Helper()
	if tt.MaxPerCustomer != nil {
		t.Fatalf("%s: max_per_customer = %d, want null", where, *tt.MaxPerCustomer)
	}
}

func assertPurchaseLimit(t *testing.T, tt ticketTypeWithLimit, want int, where string) {
	t.Helper()
	if tt.MaxPerCustomer == nil {
		t.Fatalf("%s: max_per_customer = null, want %d", where, want)
	}
	if *tt.MaxPerCustomer != want {
		t.Fatalf("%s: max_per_customer = %d, want %d", where, *tt.MaxPerCustomer, want)
	}
}

// TestTicketTypeWithoutPurchaseLimitIsUnrestricted pins the migration's promise:
// a Ticket Type created without mentioning a Purchase Limit has none, on both
// the create response and a subsequent read. Every Ticket Type that existed
// before ADR 0025 is in exactly this state, so this is the case that must not
// change.
func TestTicketTypeWithoutPurchaseLimitIsUnrestricted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Limitless", "limitless")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        "General Admission",
		"price_cents": 1000,
		"capacity":    50,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoPurchaseLimit(t, decodeTicketTypeWithLimit(t, body), "create response")

	_, listBody := env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if listBody.Error != nil {
		t.Fatalf("list ticket types error=%+v", listBody.Error)
	}
	var list []ticketTypeWithLimit
	if err := json.Unmarshal(listBody.Data, &list); err != nil {
		t.Fatalf("decode ticket type list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ticket type list length = %d, want 1", len(list))
	}
	assertNoPurchaseLimit(t, list[0], "list response")
}

// TestTicketTypePurchaseLimitRoundTrips walks the value through create, read,
// update to a new value, and clearing — the four things an Org Admin does with
// it. Clearing is the interesting one: the update endpoint is a full
// restatement, so an omitted key and an explicit null both mean "no Purchase
// Limit", and both are asserted here.
func TestTicketTypePurchaseLimitRoundTrips(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rationed", "rationed")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":             "Free Entry",
		"price_cents":      0,
		"capacity":         100,
		"max_per_customer": 1,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	created := decodeTicketTypeWithLimit(t, body)
	assertPurchaseLimit(t, created, 1, "create response")

	// Raised to a plus-one allowance. A Purchase Limit above 1 is ordinary, so
	// the field is a number rather than the boolean the free case alone suggests.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+created.ID, map[string]any{
		"name":             "Free Entry",
		"price_cents":      0,
		"capacity":         100,
		"sort_order":       0,
		"max_per_customer": 2,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("raise purchase limit status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertPurchaseLimit(t, decodeTicketTypeWithLimit(t, body), 2, "raised")

	// Cleared with an explicit null — the Ticket Type becomes unrestricted again.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+created.ID, map[string]any{
		"name":             "Free Entry",
		"price_cents":      0,
		"capacity":         100,
		"sort_order":       0,
		"max_per_customer": nil,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear purchase limit status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoPurchaseLimit(t, decodeTicketTypeWithLimit(t, body), "cleared with explicit null")

	// Set again, then cleared by omitting the key. The endpoint is a full
	// restatement, so silence means "no Purchase Limit" — the same rule
	// Description already follows.
	resp, _ = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+created.ID, map[string]any{
		"name":             "Free Entry",
		"price_cents":      0,
		"capacity":         100,
		"sort_order":       0,
		"max_per_customer": 3,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset purchase limit status=%d", resp.StatusCode)
	}
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+created.ID, map[string]any{
		"name":        "Free Entry",
		"price_cents": 0,
		"capacity":    100,
		"sort_order":  0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear by omission status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoPurchaseLimit(t, decodeTicketTypeWithLimit(t, body), "cleared by omitting the key")
}

// TestTicketTypePurchaseLimitRejectsNonPositive pins the field error. Zero is
// refused rather than read as "nobody may buy this": a Ticket Type nobody may
// buy is expressed by not publishing it, and a quantity field that silently
// meant "none" would mislead an Org Admin into thinking they had said so.
//
// The code is the one capacity already uses, so a client keying copy on
// INVALID_POSITIVE_INT needs nothing new to render this.
func TestTicketTypePurchaseLimitRejectsNonPositive(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Bad Limit", "bad-limit")

	for _, value := range []int{0, -1} {
		resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
			"name":             "General Admission",
			"price_cents":      1000,
			"capacity":         50,
			"max_per_customer": value,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("create with max_per_customer=%d status=%d, want 400", value, resp.StatusCode)
		}
		fields := fieldErrorsByName(t, body)
		want := fieldError{
			Field:   "max_per_customer",
			Code:    "INVALID_POSITIVE_INT",
			Message: "must be greater than zero",
		}
		got, ok := fields["max_per_customer"]
		if !ok {
			t.Fatalf("no field error on max_per_customer for %d; got %+v", value, fields)
		}
		if got != want {
			t.Fatalf("field error for %d = %+v, want %+v", value, got, want)
		}
	}

	// The same refusal on update, because the guard lives on both writes rather
	// than only the one a form happens to hit first.
	ticketTypeID := createTicketType(t, env, sessionID, eventID)
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":             "GA",
		"price_cents":      1000,
		"capacity":         50,
		"sort_order":       0,
		"max_per_customer": 0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("update with max_per_customer=0 status=%d, want 400", resp.StatusCode)
	}
	if _, ok := fieldErrorsByName(t, body)["max_per_customer"]; !ok {
		t.Fatalf("no field error on max_per_customer from update")
	}
}

// TestPublicEventStatesPurchaseLimit proves the Storefront can bound its
// quantity picker without guessing: the public Event payload carries each
// Ticket Type's Purchase Limit, and carries null for one that has none.
//
// It reveals the Ticket Type's limit and nothing about any Customer's holdings
// — an anonymous reader learns the rule, never who has already used it up
// (ADR 0025).
func TestPublicEventStatesPurchaseLimit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	startsAt := env.fixedClock.Add(48 * time.Hour)
	eventID := publishEvent(t, env, sessionID, "Public Limits", "public-limits", startsAt, true, 1000, 100)

	// publishEvent already added one unrestricted Ticket Type; add a rationed one
	// beside it so a single read proves both renderings.
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":             "Free Entry",
		"price_cents":      0,
		"capacity":         100,
		"max_per_customer": 1,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create rationed ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	_, publicBody := env.get(t, "/api/v1/public/organizations/test-org/events/public-limits", nil)
	if publicBody.Error != nil {
		t.Fatalf("public event error=%+v", publicBody.Error)
	}
	var publicEvent struct {
		TicketTypes []ticketTypeWithLimit `json:"ticket_types"`
	}
	if err := json.Unmarshal(publicBody.Data, &publicEvent); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	if len(publicEvent.TicketTypes) != 2 {
		t.Fatalf("public ticket types = %d, want 2", len(publicEvent.TicketTypes))
	}

	byName := map[string]ticketTypeWithLimit{}
	for _, tt := range publicEvent.TicketTypes {
		byName[tt.Name] = tt
	}
	rationed, ok := byName["Free Entry"]
	if !ok {
		t.Fatalf("no Free Entry ticket type in public payload; got %+v", byName)
	}
	assertPurchaseLimit(t, rationed, 1, "public rationed ticket type")

	// publishEvent names its own Ticket Type "General Admission"; it carries no
	// Purchase Limit, which is the reading that must not change.
	unrestricted, ok := byName["General Admission"]
	if !ok {
		t.Fatalf("no General Admission ticket type in public payload; got %+v", byName)
	}
	assertNoPurchaseLimit(t, unrestricted, "public unrestricted ticket type")
}
