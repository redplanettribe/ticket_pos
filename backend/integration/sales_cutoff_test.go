package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// THE SALES CUTOFF (#603, parent #602, ADR 0070): the instant a Ticket Type
// stops being sold on the Storefront, set by an Org Admin through the staff API.
//
// This file covers the first slice — the column, the write path that puts a
// value there, and the staff read model that hands it back. Nothing in the
// product reacts to the value yet: the Storefront is untouched, begin-checkout
// refuses nothing, and no sale path reads the column. What matters here is that
// the value round-trips honestly through create, update and read, and that the
// values ADR 0070 calls INTENDED are accepted rather than second-guessed.
//
// The shape is promotions_test.go's, deliberately: an Org Admin session, a
// draft Event, a Ticket Type, then requests and assertions on the envelope that
// comes back. Instants are compared with sameInstant rather than as strings,
// because what was stored is a moment and not a rendering of one.
//
// WHAT IS NOT TESTED HERE, AND WHY. There is no test that a past cutoff refuses
// anything, that a cutoff before the Event's start is required, or that a
// Promotion's window may not straddle it. ADR 0070 rules all three out: nothing
// about the value is validated, a past instant is the intended way to stop
// selling something right now, and an instant after the Event starts is a
// workshop selling at its own door. A test asserting a refusal would be a test
// asserting the feature is broken.

// ticketTypeWithCutoff decodes the staff Ticket Type payload down to the fields
// this slice touches, plus the two that prove closing is a door and not a
// deletion.
type ticketTypeWithCutoff struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Capacity      int     `json:"capacity"`
	SoldCount     int     `json:"sold_count"`
	SalesCutoffAt *string `json:"sales_cutoff_at"`
	// Promotion rides along because a Sales Cutoff and a Promotion share one
	// Ticket Type (user story 42) and the update endpoint is a full
	// restatement: setting a cutoff must not take a discount off the row.
	Promotion *struct {
		PromotionalPriceCents int    `json:"promotional_price_cents"`
		EndsAt                string `json:"ends_at"`
	} `json:"promotion"`
}

func decodeTicketTypeWithCutoff(t *testing.T, data json.RawMessage) ticketTypeWithCutoff {
	t.Helper()
	var tt ticketTypeWithCutoff
	if err := json.Unmarshal(data, &tt); err != nil {
		t.Fatalf("decode ticket type: %v", err)
	}
	return tt
}

func ticketTypePath(eventID, ticketTypeID string) string {
	return "/api/v1/staff/events/" + eventID + "/ticket-types/" + ticketTypeID
}

// listTicketTypeWithCutoff reads the Ticket Type back off the LIST endpoint
// rather than trusting the write's own echo. The list is what the Ticket Types
// section renders from, and it scans its rows on a separate code path from the
// single-row read — so a column added to one and missed on the other would
// otherwise pass unnoticed.
func listTicketTypeWithCutoff(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string) ticketTypeWithCutoff {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/ticket-types", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list ticket types status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var types []ticketTypeWithCutoff
	if err := json.Unmarshal(body.Data, &types); err != nil {
		t.Fatalf("decode ticket types: %v", err)
	}
	for _, tt := range types {
		if tt.ID == ticketTypeID {
			return tt
		}
	}
	t.Fatalf("ticket type %s absent from the list of %d", ticketTypeID, len(types))
	return ticketTypeWithCutoff{}
}

// restateTicketType patches the whole Ticket Type. The staff update is a full
// restatement rather than a patch, so every scalar has to be sent back on every
// edit — which is exactly what makes "send null to clear" the clearing gesture.
func restateTicketType(
	t *testing.T,
	env *testEnv,
	sessionID, eventID, ticketTypeID string,
	salesCutoffAt any,
) ticketTypeWithCutoff {
	t.Helper()
	resp, body := env.patch(t, ticketTypePath(eventID, ticketTypeID), map[string]any{
		"name":            "GA",
		"price_cents":     5000,
		"capacity":        50,
		"sort_order":      0,
		"sales_cutoff_at": salesCutoffAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}
	return decodeTicketTypeWithCutoff(t, body.Data)
}

// TestTicketTypeSalesCutoffSetMoveAndClear walks the whole of user stories 1, 6
// and 7 in one pass: a cutoff is set on create, moved by an edit, cleared by
// sending null, and set again afterwards. Clearing has to leave the slot usable
// — reopening sales is one edit and not a rebuild of the Ticket Type.
func TestTicketTypeSalesCutoffSetMoveAndClear(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Cutoff Fest", "cutoff-fest")

	cutoffAt := env.fixedClock.Add(72 * time.Hour).UTC().Format(time.RFC3339)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":            "GA",
		"price_cents":     5000,
		"capacity":        50,
		"sales_cutoff_at": cutoffAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}
	created := decodeTicketTypeWithCutoff(t, body.Data)
	if created.SalesCutoffAt == nil || !sameInstant(t, *created.SalesCutoffAt, cutoffAt) {
		t.Fatalf("created sales_cutoff_at=%v want %s", created.SalesCutoffAt, cutoffAt)
	}

	listed := listTicketTypeWithCutoff(t, env, sessionID, eventID, created.ID)
	if listed.SalesCutoffAt == nil || !sameInstant(t, *listed.SalesCutoffAt, cutoffAt) {
		t.Fatalf("listed sales_cutoff_at=%v want %s", listed.SalesCutoffAt, cutoffAt)
	}

	// Moved: an Org Admin extends the deadline by a day.
	movedTo := env.fixedClock.Add(96 * time.Hour).UTC().Format(time.RFC3339)
	moved := restateTicketType(t, env, sessionID, eventID, created.ID, movedTo)
	if moved.SalesCutoffAt == nil || !sameInstant(t, *moved.SalesCutoffAt, movedTo) {
		t.Fatalf("moved sales_cutoff_at=%v want %s", moved.SalesCutoffAt, movedTo)
	}

	// Cleared: null reopens sales, and the read model says so.
	cleared := restateTicketType(t, env, sessionID, eventID, created.ID, nil)
	if cleared.SalesCutoffAt != nil {
		t.Fatalf("expected sales_cutoff_at cleared, got %s", *cleared.SalesCutoffAt)
	}
	if listed := listTicketTypeWithCutoff(t, env, sessionID, eventID, created.ID); listed.SalesCutoffAt != nil {
		t.Fatalf("expected listed sales_cutoff_at cleared, got %s", *listed.SalesCutoffAt)
	}

	// Clearing frees the slot: a cutoff may be set again afterwards.
	reset := restateTicketType(t, env, sessionID, eventID, created.ID, cutoffAt)
	if reset.SalesCutoffAt == nil || !sameInstant(t, *reset.SalesCutoffAt, cutoffAt) {
		t.Fatalf("re-set sales_cutoff_at=%v want %s", reset.SalesCutoffAt, cutoffAt)
	}
}

// TestTicketTypeSalesCutoffInThePastIsAccepted covers user story 5, and is the
// test that would fail first if somebody added a "helpful" future check. A past
// instant is how an Org Admin stops selling a Ticket Type THIS INSTANT without
// deleting it and losing its sales history.
//
// It is also set twice: once on create and once on an edit, because a cutoff
// that has already passed must stay movable (story 7) — extending a closed sale
// is an ordinary edit and not a workaround.
func TestTicketTypeSalesCutoffInThePastIsAccepted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Past Cutoff Fest", "past-cutoff-fest")

	longPast := env.fixedClock.Add(-720 * time.Hour).UTC().Format(time.RFC3339)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":            "GA",
		"price_cents":     5000,
		"capacity":        50,
		"sales_cutoff_at": longPast,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create with a past cutoff status=%d error=%+v — a past instant is intended (ADR 0070)", resp.StatusCode, body.Error)
	}
	created := decodeTicketTypeWithCutoff(t, body.Data)
	if created.SalesCutoffAt == nil || !sameInstant(t, *created.SalesCutoffAt, longPast) {
		t.Fatalf("created sales_cutoff_at=%v want %s", created.SalesCutoffAt, longPast)
	}

	// A cutoff that has already passed is moved to another instant that has also
	// already passed. Neither end of the edit is in the future, and both are fine.
	recentPast := env.fixedClock.Add(-time.Hour).UTC().Format(time.RFC3339)
	moved := restateTicketType(t, env, sessionID, eventID, created.ID, recentPast)
	if moved.SalesCutoffAt == nil || !sameInstant(t, *moved.SalesCutoffAt, recentPast) {
		t.Fatalf("moved sales_cutoff_at=%v want %s", moved.SalesCutoffAt, recentPast)
	}
}

// TestTicketTypeSalesCutoffAfterTheEventStartIsAccepted covers user story 11: a
// workshop that sells during its own first hour. The cutoff is never
// cross-checked against the Event's start, which is the Purchase Limit's
// precedent — one catalog value is not judged against a neighbouring one.
func TestTicketTypeSalesCutoffAfterTheEventStartIsAccepted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	startsAt := env.fixedClock.Add(24 * time.Hour)
	eventID := publishEvent(t, env, sessionID, "Workshop", "workshop-door-sales", startsAt, false, 5000, 50)

	afterTheDoorsOpen := startsAt.Add(time.Hour).UTC().Format(time.RFC3339)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":            "Door",
		"price_cents":     6000,
		"capacity":        20,
		"sales_cutoff_at": afterTheDoorsOpen,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create with a cutoff after the Event start status=%d error=%+v — a workshop sells at its own door (ADR 0070)", resp.StatusCode, body.Error)
	}
	created := decodeTicketTypeWithCutoff(t, body.Data)
	if created.SalesCutoffAt == nil || !sameInstant(t, *created.SalesCutoffAt, afterTheDoorsOpen) {
		t.Fatalf("created sales_cutoff_at=%v want %s", created.SalesCutoffAt, afterTheDoorsOpen)
	}
}

// TestTicketTypeWithoutASalesCutoffReadsNull covers user story 4 and 43: a
// Ticket Type that has never needed a cutoff is untouched by this feature, and
// a deploy is not a change to anybody's catalog. Omitting the field entirely
// and sending it as an explicit null are the same statement.
//
// A test that only asserts nulls would pass with the whole feature deleted —
// an unread field reads null exactly as an unset one does. So a SIBLING Ticket
// Type on the same Event carries a cutoff throughout, and is asserted to keep
// it on every read the quiet one is asked for. That sibling is the anchor: rip
// the column out and it reads null too, and this test goes red rather than
// staying quietly green.
func TestTicketTypeWithoutASalesCutoffReadsNull(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Quiet Fest", "quiet-cutoff-fest")

	// The field is absent from the body altogether, exactly as it is in every
	// request written before this ticket existed.
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 5000, 50)

	// The anchor: a neighbour on the same Event, created with a cutoff, read
	// off the same list on the same code path.
	anchorAt := env.fixedClock.Add(72 * time.Hour).UTC().Format(time.RFC3339)
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":            "Early Bird",
		"price_cents":     3000,
		"capacity":        40,
		"sales_cutoff_at": anchorAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create the anchoring Ticket Type status=%d error=%+v", resp.StatusCode, body.Error)
	}
	anchorID := decodeTicketTypeWithCutoff(t, body.Data).ID

	if listed := listTicketTypeWithCutoff(t, env, sessionID, eventID, ticketTypeID); listed.SalesCutoffAt != nil {
		t.Fatalf("expected null sales_cutoff_at on a Ticket Type created without one, got %s", *listed.SalesCutoffAt)
	}
	anchored := listTicketTypeWithCutoff(t, env, sessionID, eventID, anchorID)
	if anchored.SalesCutoffAt == nil || !sameInstant(t, *anchored.SalesCutoffAt, anchorAt) {
		t.Fatalf("the anchoring Ticket Type reads sales_cutoff_at=%v, want %s — a feature that stores nothing would read null here too",
			anchored.SalesCutoffAt, anchorAt)
	}

	// An explicit null says the same thing, and leaves it saying it — and says
	// it about this Ticket Type alone. Clearing one cutoff is not a clearing of
	// the Event's.
	explicit := restateTicketType(t, env, sessionID, eventID, ticketTypeID, nil)
	if explicit.SalesCutoffAt != nil {
		t.Fatalf("expected null sales_cutoff_at after an explicit null, got %s", *explicit.SalesCutoffAt)
	}
	stillAnchored := listTicketTypeWithCutoff(t, env, sessionID, eventID, anchorID)
	if stillAnchored.SalesCutoffAt == nil || !sameInstant(t, *stillAnchored.SalesCutoffAt, anchorAt) {
		t.Fatalf("the neighbour's cutoff = %v after clearing GA's, want the %s it was set to",
			stillAnchored.SalesCutoffAt, anchorAt)
	}
}

// TestSettingASalesCutoffLeavesThePromotionAlone is user story 42 at the write
// seam: a discount and a deadline are not mutually exclusive, and the staff
// update is a FULL RESTATEMENT — every scalar re-sent on every edit. That shape
// is exactly how a Promotion could be lost by accident, since the body that
// sets a cutoff says nothing about one.
//
// Both directions are walked: a cutoff set onto a promoted Ticket Type, and a
// Promotion set onto one that already has a cutoff. Each read is taken off the
// list endpoint, which is what the Ticket Types section renders from.
func TestSettingASalesCutoffLeavesThePromotionAlone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Both Fest", "both-cutoff-fest")
	ticketTypeID := createTicketTypePriced(t, env, sessionID, eventID, promoListPriceCents)

	endsAt := env.fixedClock.Add(48 * time.Hour)
	setPromotion(t, env, sessionID, eventID, ticketTypeID, promoPriceCents, nil, endsAt)

	// The cutoff is set through the same full restatement an Org Admin's form
	// sends, which mentions no Promotion at all.
	cutoffAt := env.fixedClock.Add(24 * time.Hour).UTC().Format(time.RFC3339)
	resp, body := env.patch(t, ticketTypePath(eventID, ticketTypeID), map[string]any{
		"name":            "GA",
		"price_cents":     promoListPriceCents,
		"capacity":        50,
		"sort_order":      0,
		"sales_cutoff_at": cutoffAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set a cutoff on a promoted Ticket Type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	both := listTicketTypeWithCutoff(t, env, sessionID, eventID, ticketTypeID)
	if both.SalesCutoffAt == nil || !sameInstant(t, *both.SalesCutoffAt, cutoffAt) {
		t.Fatalf("sales_cutoff_at=%v want %s", both.SalesCutoffAt, cutoffAt)
	}
	if both.Promotion == nil {
		t.Fatal("the Promotion was lost when a Sales Cutoff was set; story 42 wants both on one Ticket Type")
	}
	if both.Promotion.PromotionalPriceCents != promoPriceCents {
		t.Fatalf("promotional_price_cents = %d after setting a cutoff, want the %d it was set to",
			both.Promotion.PromotionalPriceCents, promoPriceCents)
	}
	if !sameInstant(t, both.Promotion.EndsAt, endsAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("promotion ends_at = %q after setting a cutoff, want %q",
			both.Promotion.EndsAt, endsAt.UTC().Format(time.RFC3339))
	}

	// And the other way round: taking the Promotion off does not take the
	// cutoff with it. The two slots are independent in both directions, which
	// is what "coexist" has to mean for an Org Admin editing one of them.
	removePromotion(t, env, sessionID, eventID, ticketTypeID)
	after := listTicketTypeWithCutoff(t, env, sessionID, eventID, ticketTypeID)
	if after.Promotion != nil {
		t.Fatalf("promotion still present after removal: %+v", after.Promotion)
	}
	if after.SalesCutoffAt == nil || !sameInstant(t, *after.SalesCutoffAt, cutoffAt) {
		t.Fatalf("sales_cutoff_at = %v after removing the Promotion, want the %s it kept",
			after.SalesCutoffAt, cutoffAt)
	}
}

// TestClosedTicketTypeKeepsItsSalesCapacityAndHistory covers user story 10 —
// the whole reason a cutoff exists rather than a deletion. An Event sells some
// tickets, then an Org Admin closes the Ticket Type by setting a cutoff in the
// past. Nothing about the row moves: the capacity is the capacity, the sold
// count is the sold count, and the Sales list still has the sale on it.
//
// This is the assertion that would catch a future slice deciding a cutoff
// should cascade into anything. Closing is a door.
func TestClosedTicketTypeKeepsItsSalesCapacityAndHistory(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	startsAt := env.fixedClock.Add(240 * time.Hour)
	eventID := publishEvent(t, env, sessionID, "Door List Fest", "door-list-fest", startsAt, false, 5000, 50)
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Early Bird", 3000, 40)

	recorded := recordManualSaleOK(t, env, sessionID, eventID, manualSaleBody(
		"ana@example.com", "Ana", "Lopez", ticketTypeID, 3, "cash",
		env.fixedClock.Add(-48*time.Hour).UTC().Format(time.RFC3339),
	))

	before := listTicketTypeWithCutoff(t, env, sessionID, eventID, ticketTypeID)
	if before.SoldCount != 3 {
		t.Fatalf("sold_count before closing = %d, want 3", before.SoldCount)
	}
	salesBefore := listSalesOK(t, env, sessionID, eventID, "")

	// Closed right now: the cutoff is an hour in the past, which is the gesture
	// an Org Admin makes to stop selling immediately.
	closedAt := env.fixedClock.Add(-time.Hour).UTC().Format(time.RFC3339)
	resp, body := env.patch(t, ticketTypePath(eventID, ticketTypeID), map[string]any{
		"name":        "Early Bird",
		"price_cents": 3000,
		"capacity":    40,
		// The second Ticket Type on this Event, so sort_order 1. Restated
		// unchanged: this edit is about the cutoff and nothing else.
		"sort_order":      1,
		"sales_cutoff_at": closedAt,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("close ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	after := listTicketTypeWithCutoff(t, env, sessionID, eventID, ticketTypeID)
	if after.SalesCutoffAt == nil || !sameInstant(t, *after.SalesCutoffAt, closedAt) {
		t.Fatalf("sales_cutoff_at=%v want %s", after.SalesCutoffAt, closedAt)
	}
	if after.Capacity != 40 {
		t.Fatalf("capacity after closing = %d, want 40 — closing is not a capacity change", after.Capacity)
	}
	if after.SoldCount != before.SoldCount {
		t.Fatalf("sold_count after closing = %d, want %d — closing sells nothing back", after.SoldCount, before.SoldCount)
	}

	salesAfter := listSalesOK(t, env, sessionID, eventID, "")
	if salesAfter.Pagination.Total != salesBefore.Pagination.Total {
		t.Fatalf("sales total after closing = %d, want %d — closing is a door, not a deletion", salesAfter.Pagination.Total, salesBefore.Pagination.Total)
	}
	found := false
	for _, row := range salesAfter.Data {
		if row.ID == recorded.SaleID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the sale recorded before the cutoff is gone from the Sales list")
	}
}

// TestTicketTypeSalesCutoffMustBeATimestamp is the one refusal this slice has,
// and it is about the FORMAT and never the value. A string that is not an
// RFC3339 instant is a typo the caller can fix; a past instant or an instant
// after the Event's start is a decision the caller meant, and neither is
// refused anywhere.
func TestTicketTypeSalesCutoffMustBeATimestamp(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Typo Fest", "typo-cutoff-fest")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":            "GA",
		"price_cents":     5000,
		"capacity":        50,
		"sales_cutoff_at": "friday at six",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create with a malformed cutoff status=%d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
	assertFieldError(t, body.Error.Details, "sales_cutoff_at")

	// The same refusal on the edit, so a typo cannot be smuggled in by a second
	// request onto a Ticket Type that already exists.
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA2", 5000, 50)
	resp, body = env.patch(t, ticketTypePath(eventID, ticketTypeID), map[string]any{
		"name":            "GA2",
		"price_cents":     5000,
		"capacity":        50,
		"sort_order":      0,
		"sales_cutoff_at": "2026-13-45",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("update with a malformed cutoff status=%d, want 400", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", body.Error)
	}
	assertFieldError(t, body.Error.Details, "sales_cutoff_at")
}
