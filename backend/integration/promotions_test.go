package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// ticketTypeWithPromotion decodes the staff Ticket Type payload including the
// Promotion slot, which is what the Ticket Type editor renders state from.
type ticketTypeWithPromotion struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
	Promotion  *struct {
		PromotionalPriceCents int     `json:"promotional_price_cents"`
		StartsAt              *string `json:"starts_at"`
		EndsAt                string  `json:"ends_at"`
	} `json:"promotion"`
}

func decodeTicketTypeWithPromotion(t *testing.T, data json.RawMessage) ticketTypeWithPromotion {
	t.Helper()
	var tt ticketTypeWithPromotion
	if err := json.Unmarshal(data, &tt); err != nil {
		t.Fatalf("decode ticket type: %v", err)
	}
	return tt
}

func promotionPath(eventID, ticketTypeID string) string {
	return "/api/v1/staff/events/" + eventID + "/ticket-types/" + ticketTypeID + "/promotion"
}

// createTicketTypePriced adds a Ticket Type at a chosen List Price so promotion
// tests can pick prices strictly below it.
func createTicketTypePriced(t *testing.T, env *testEnv, sessionID, eventID string, priceCents int) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        "GA",
		"price_cents": priceCents,
		"capacity":    50,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatalf("decode ticket type: %v", err)
	}
	return created.ID
}

func TestTicketTypePromotionSetUpdateRemove(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Promo Fest", "promo-fest")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	startsAt := env.fixedClock.Add(24 * time.Hour).UTC().Format(time.RFC3339)
	endsAt := env.fixedClock.Add(72 * time.Hour).UTC().Format(time.RFC3339)

	resp, body := env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 2500,
		"starts_at":               startsAt,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("set promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}
	created := decodeTicketTypeWithPromotion(t, body.Data)
	if created.Promotion == nil {
		t.Fatalf("expected promotion on ticket type, got %+v", created)
	}
	if created.Promotion.PromotionalPriceCents != 2500 {
		t.Fatalf("promotional price=%d", created.Promotion.PromotionalPriceCents)
	}
	if created.Promotion.StartsAt == nil || !sameInstant(t, *created.Promotion.StartsAt, startsAt) {
		t.Fatalf("starts_at=%v want %s", created.Promotion.StartsAt, startsAt)
	}
	if !sameInstant(t, created.Promotion.EndsAt, endsAt) {
		t.Fatalf("ends_at=%s want %s", created.Promotion.EndsAt, endsAt)
	}

	// Update drops the start (a Promotion live from the moment it is saved) and
	// lowers the Promotional Price further.
	newEndsAt := env.fixedClock.Add(96 * time.Hour).UTC().Format(time.RFC3339)
	resp, body = env.patch(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 1000,
		"starts_at":               nil,
		"ends_at":                 newEndsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}
	updated := decodeTicketTypeWithPromotion(t, body.Data)
	if updated.Promotion == nil || updated.Promotion.PromotionalPriceCents != 1000 {
		t.Fatalf("updated promotion=%+v", updated.Promotion)
	}
	if updated.Promotion.StartsAt != nil {
		t.Fatalf("expected starts_at cleared, got %v", *updated.Promotion.StartsAt)
	}
	if !sameInstant(t, updated.Promotion.EndsAt, newEndsAt) {
		t.Fatalf("ends_at=%s want %s", updated.Promotion.EndsAt, newEndsAt)
	}

	resp, body = env.deleteJSON(t, promotionPath(eventID, ticketTypeID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}
	removed := decodeTicketTypeWithPromotion(t, body.Data)
	if removed.Promotion != nil {
		t.Fatalf("expected promotion removed, got %+v", removed.Promotion)
	}

	// Removing frees the slot: a second Promotion may be set afterwards.
	resp, body = env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 4000,
		"ends_at":                 newEndsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-set promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

func sameInstant(t *testing.T, got, want string) bool {
	t.Helper()
	g, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("parse %q: %v", got, err)
	}
	w, err := time.Parse(time.RFC3339, want)
	if err != nil {
		t.Fatalf("parse %q: %v", want, err)
	}
	return g.Equal(w)
}

func TestTicketTypePromotionRejectedWhenNotBelowListPrice(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Promo Guard", "promo-guard")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	endsAt := env.fixedClock.Add(72 * time.Hour).UTC().Format(time.RFC3339)

	// Equal to the List Price is not a Promotion.
	resp, body := env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 5000,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE" {
		t.Fatalf("expected PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE, got %+v", body.Error)
	}
	if body.Data != nil && string(body.Data) != "null" {
		t.Fatalf("expected null data, got %s", string(body.Data))
	}

	// Above it is not either.
	resp, body = env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 6000,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE" {
		t.Fatalf("expected PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE, got %+v", body.Error)
	}

	// The same guard applies on update of an existing Promotion.
	resp, body = env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 1000,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("set promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.patch(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 5000,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on update, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE" {
		t.Fatalf("expected PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE, got %+v", body.Error)
	}
}

func TestTicketTypePromotionZeroPromotionalPriceAccepted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Free Night", "free-night")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	endsAt := env.fixedClock.Add(48 * time.Hour).UTC().Format(time.RFC3339)

	resp, body := env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 0,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d error=%+v", resp.StatusCode, body.Error)
	}
	created := decodeTicketTypeWithPromotion(t, body.Data)
	if created.Promotion == nil || created.Promotion.PromotionalPriceCents != 0 {
		t.Fatalf("promotion=%+v", created.Promotion)
	}
}

func TestTicketTypePromotionSecondPromotionRejected(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "One Slot", "one-slot")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	endsAt := env.fixedClock.Add(48 * time.Hour).UTC().Format(time.RFC3339)

	resp, body := env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 2500,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("set promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 2000,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PROMOTION_ALREADY_EXISTS" {
		t.Fatalf("expected PROMOTION_ALREADY_EXISTS, got %+v", body.Error)
	}
}

func TestTicketTypePromotionUpdateAndRemoveWithoutPromotionNotFound(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "No Promo", "no-promo")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	endsAt := env.fixedClock.Add(48 * time.Hour).UTC().Format(time.RFC3339)

	resp, body := env.patch(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 1000,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on update, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PROMOTION_NOT_FOUND" {
		t.Fatalf("expected PROMOTION_NOT_FOUND, got %+v", body.Error)
	}

	resp, body = env.deleteJSON(t, promotionPath(eventID, ticketTypeID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on remove, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PROMOTION_NOT_FOUND" {
		t.Fatalf("expected PROMOTION_NOT_FOUND, got %+v", body.Error)
	}
}

func TestTicketTypeListPriceEditRejectedAtOrBelowPromotionalPrice(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Price Guard", "price-guard")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	endsAt := env.fixedClock.Add(48 * time.Hour).UTC().Format(time.RFC3339)
	resp, body := env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 2500,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("set promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Dropping the List Price to the Promotional Price is refused...
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":        "GA",
		"price_cents": 2500,
		"capacity":    50,
		"sort_order":  0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE" {
		t.Fatalf("expected LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE, got %+v", body.Error)
	}

	// ...and so is dropping it below.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":        "GA",
		"price_cents": 1000,
		"capacity":    50,
		"sort_order":  0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE" {
		t.Fatalf("expected LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE, got %+v", body.Error)
	}

	// A List Price still above the Promotional Price is accepted.
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":        "GA",
		"price_cents": 3000,
		"capacity":    50,
		"sort_order":  0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d error=%+v", resp.StatusCode, body.Error)
	}
	updated := decodeTicketTypeWithPromotion(t, body.Data)
	if updated.PriceCents != 3000 {
		t.Fatalf("price_cents=%d", updated.PriceCents)
	}
	if updated.Promotion == nil || updated.Promotion.PromotionalPriceCents != 2500 {
		t.Fatalf("promotion=%+v", updated.Promotion)
	}

	// After removing the Promotion the same edit goes through.
	resp, body = env.deleteJSON(t, promotionPath(eventID, ticketTypeID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":        "GA",
		"price_cents": 1000,
		"capacity":    50,
		"sort_order":  0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after removal, got %d error=%+v", resp.StatusCode, body.Error)
	}
}

func TestTicketTypePromotionWindowValidation(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Window", "window")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	// ends_at is required.
	resp, body := env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 1000,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
	assertFieldError(t, body.Error.Details, "ends_at")

	// A start at or after the end is not a window.
	startsAt := env.fixedClock.Add(72 * time.Hour).UTC().Format(time.RFC3339)
	endsAt := env.fixedClock.Add(24 * time.Hour).UTC().Format(time.RFC3339)
	resp, body = env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": 1000,
		"starts_at":               startsAt,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
	assertFieldError(t, body.Error.Details, "starts_at")

	// A negative Promotional Price is rejected before the service is reached.
	resp, body = env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
		"promotional_price_cents": -1,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
	assertFieldError(t, body.Error.Details, "promotional_price_cents")
}

func assertFieldError(t *testing.T, details any, field string) {
	t.Helper()
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	var parsed struct {
		Fields []struct {
			Field string `json:"field"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	for _, f := range parsed.Fields {
		if f.Field == field {
			return
		}
	}
	t.Fatalf("expected field error on %q, got %s", field, string(raw))
}

func TestTicketTypePromotionForbiddenForMemberWithoutCatalogAuthority(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Guarded", "guarded")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "doorstaff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "doorstaff@example.com")

	endsAt := env.fixedClock.Add(48 * time.Hour).UTC().Format(time.RFC3339)

	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope)
	}{
		{"set", func() (*http.Response, envelope) {
			return env.post(t, promotionPath(eventID, ticketTypeID), map[string]any{
				"promotional_price_cents": 1000,
				"ends_at":                 endsAt,
			}, authHeader(staffSessionID))
		}},
		{"update", func() (*http.Response, envelope) {
			return env.patch(t, promotionPath(eventID, ticketTypeID), map[string]any{
				"promotional_price_cents": 1000,
				"ends_at":                 endsAt,
			}, authHeader(staffSessionID))
		}},
		{"remove", func() (*http.Response, envelope) {
			return env.deleteJSON(t, promotionPath(eventID, ticketTypeID), nil, authHeader(staffSessionID))
		}},
	} {
		resp, body := tc.call()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s: expected 403, got %d error=%+v", tc.name, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s: expected FORBIDDEN, got %+v", tc.name, body.Error)
		}
	}
}

func TestTicketTypeDetailCarriesPromotion(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Detail", "detail")
	promoted := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        "Plain",
		"price_cents": 4000,
		"capacity":    10,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	plain := decodeTicketTypeWithPromotion(t, body.Data)
	if plain.Promotion != nil {
		t.Fatalf("expected no promotion on a fresh ticket type, got %+v", plain.Promotion)
	}

	endsAt := env.fixedClock.Add(48 * time.Hour).UTC().Format(time.RFC3339)
	resp, body = env.post(t, promotionPath(eventID, promoted), map[string]any{
		"promotional_price_cents": 1500,
		"ends_at":                 endsAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("set promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var listed []ticketTypeWithPromotion
	if err := json.Unmarshal(body.Data, &listed); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 ticket types, got %d", len(listed))
	}
	for _, tt := range listed {
		switch tt.ID {
		case promoted:
			if tt.Promotion == nil || tt.Promotion.PromotionalPriceCents != 1500 {
				t.Fatalf("promoted ticket type=%+v", tt.Promotion)
			}
			if tt.Promotion.StartsAt != nil {
				t.Fatalf("expected null starts_at, got %v", *tt.Promotion.StartsAt)
			}
			if !sameInstant(t, tt.Promotion.EndsAt, endsAt) {
				t.Fatalf("ends_at=%s want %s", tt.Promotion.EndsAt, endsAt)
			}
		case plain.ID:
			if tt.Promotion != nil {
				t.Fatalf("plain ticket type carries promotion=%+v", tt.Promotion)
			}
		default:
			t.Fatalf("unexpected ticket type %s", tt.ID)
		}
	}
}

// TestListPriceEditAllowedOnceThePromotionHasEnded: the List Price invariant
// guards the Promotion's own window, not the row that outlives it. An ended
// Promotion constrains nothing, so a later price cut below its old Promotional
// Price goes through without staff having to clear a dead record first.
func TestListPriceEditAllowedOnceThePromotionHasEnded(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Ended Guard", "ended-guard")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	setPromotion(t, env, sessionID, eventID, ticketTypeID, 2500, nil, env.fixedClock.Add(2*time.Hour))

	// While it is live the cut is refused, as ever.
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name": "GA", "price_cents": 2000, "capacity": 50, "sort_order": 0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("live promotion: expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}

	// Past the end the same edit is the Organization's business alone.
	holdClocksAt(env.fixedClock.Add(3 * time.Hour))
	resp, body = env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name": "GA", "price_cents": 2000, "capacity": 50, "sort_order": 0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ended promotion: expected 200, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if updated := decodeTicketTypeWithPromotion(t, body.Data); updated.PriceCents != 2000 {
		t.Fatalf("price_cents=%d; want the cut to land", updated.PriceCents)
	}
}

// TestListPriceEditRefusedByAScheduledPromotion: a Promotion that has not yet
// started still constrains the List Price — it is a discount the Organization
// has already committed to, and letting the List Price fall under it would
// invert the discount before it ever opened.
func TestListPriceEditRefusedByAScheduledPromotion(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Scheduled Guard", "scheduled-guard")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, 5000)

	startsAt := env.fixedClock.Add(24 * time.Hour)
	setPromotion(t, env, sessionID, eventID, ticketTypeID, 2500, &startsAt, env.fixedClock.Add(48*time.Hour))

	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name": "GA", "price_cents": 2000, "capacity": 50, "sort_order": 0,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE" {
		t.Fatalf("expected LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE, got %+v", body.Error)
	}
}
