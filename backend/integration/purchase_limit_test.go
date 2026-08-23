package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Purchase Limit: the most of one Ticket Type a single Customer may hold at
// once (CONTEXT.md, ADR 0025). This file covers the catalog half — setting,
// reading, clearing and rejecting the value (#164) — the online enforcement
// half: begin-checkout refusing a breach with PURCHASE_LIMIT_EXCEEDED (#166) —
// the Sale Import half: the preview refusing offending rows, counting the
// rows of one file against each other (#167) — and the read half: a signed-in
// Customer being told their own holdings on the public Event page, and nobody
// else learning anything about anybody (#168).
//
// The catalog half turns on the difference between "no Purchase Limit" and "a
// Purchase Limit of N", so every read asserts on null as carefully as on a
// number. The enforcement half turns on the allowance being counted exactly like
// capacity — active Ticket Sales plus live Capacity Holds — so a Sale Reversal
// returns it and a failed or expired Payment releases it, and on the count being
// keyed on the Customer's platform-global email (ADR 0010) rather than on the
// checkout.
//
// Two things stated here are decisions, not accidents, and a test guards each:
// the limit is NOT re-checked when a Payment is approved and its sale commits, so
// one Customer holding limit+1 is an accepted outcome and never a
// PAYMENT_APPROVED_WITHOUT_SALE incident; and a Purchase Limit refusal is
// reported ahead of a capacity refusal when a request breaches both, because it
// is the terminal fact about that buyer.

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

// --- Enforcement at begin-checkout (#166) ---------------------------------
//
// Every test below drives the public checkout endpoint, because the refusal is
// the buyer's own experience of the rule and the error code is the contract the
// Storefront keys its copy on (ADR 0023).

// setPurchaseLimit sets or clears a Ticket Type's Purchase Limit through the
// staff PATCH, which is a full restatement — hence the price and capacity. A nil
// limit clears it.
//
// It is used on ALREADY PUBLISHED Events on purpose: rationing a Ticket Type
// people are already buying is the ordinary case, and lowering a limit below what
// somebody already holds must be an accepted edit (ADR 0025).
func setPurchaseLimit(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string, priceCents, capacity int, limit *int) ticketTypeWithLimit {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":             "GA",
		"price_cents":      priceCents,
		"capacity":         capacity,
		"max_per_customer": limit,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set purchase limit status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeTicketTypeWithLimit(t, body)
}

// publishRationedEvent publishes an Event whose single Ticket Type "GA" carries a
// Purchase Limit, priced as asked — zero for the free path, which settles inside
// begin-checkout with no Payment Provider in the loop (ADR 0017).
func publishRationedEvent(t *testing.T, env *testEnv, sessionID, name, slug string, priceCents, capacity, limit int) (eventID, ticketTypeID string) {
	t.Helper()
	eventID, ticketTypeID = publishEventStarting(t, env, sessionID, name, slug,
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", priceCents, capacity)
	assertPurchaseLimit(t, setPurchaseLimit(t, env, sessionID, eventID, ticketTypeID, priceCents, capacity, &limit), limit, "rationed ticket type")
	return eventID, ticketTypeID
}

// refusedByPurchaseLimit asserts a begin-checkout was refused for the Purchase
// Limit and returns the refusal's details.
//
// The code is asserted as hard as the status: 409 alone is indistinguishable from
// sold out, and telling a Customer who already has their ticket that the Event is
// full is the confusion this error code exists to prevent.
func refusedByPurchaseLimit(t *testing.T, env *testEnv, eventSlug string, body map[string]any) map[string]any {
	t.Helper()
	resp, envBody := beginCheckout(t, env, testOrgSlug, eventSlug, body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("begin checkout status=%d error=%+v, want 409", resp.StatusCode, envBody.Error)
	}
	if envBody.Error == nil || envBody.Error.Code != "PURCHASE_LIMIT_EXCEEDED" {
		t.Fatalf("error=%+v, want PURCHASE_LIMIT_EXCEEDED", envBody.Error)
	}
	if envBody.Error.Message == "" {
		t.Fatal("PURCHASE_LIMIT_EXCEEDED carries no message; the Storefront has nothing to fall back on")
	}
	if got := string(envBody.Data); got != "null" {
		t.Fatalf("data = %s on a refusal, want null", got)
	}
	details, ok := envBody.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %+v, want an object", envBody.Error.Details)
	}
	return details
}

// assertLimitDetails pins the whole details object a Purchase Limit refusal
// carries. The Storefront states the limit and the holdings in its copy, so all
// four keys are contract and each is asserted rather than sampled.
func assertLimitDetails(t *testing.T, details map[string]any, ticketTypeID string, limit, alreadyHeld, requested int) {
	t.Helper()
	want := map[string]any{
		"ticket_type_id": ticketTypeID,
		"limit":          float64(limit),
		"already_held":   float64(alreadyHeld),
		"requested":      float64(requested),
	}
	for key, wantValue := range want {
		if details[key] != wantValue {
			t.Fatalf("details[%s] = %v, want %v (whole object: %+v)", key, details[key], wantValue, details)
		}
	}
}

// paymentCount counts every Payment recorded for an Event, whatever its status.
// SQL because no API exposes Payment rows: it is how a test proves a refusal
// happened BEFORE anything was created, rather than being undone afterwards.
func paymentCount(t *testing.T, env *testEnv, eventID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments WHERE event_id = $1`, eventID).Scan(&n); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	return n
}

// TestPurchaseLimitRefusesTheCheckoutPastTheAllowance is the tracer bullet: a
// Customer who has taken their allowance is refused the next one, with the code
// and the whole details object the Storefront renders from.
//
// The first checkout is settled all the way through confirm, so what refuses the
// second is an ACTIVE TICKET SALE rather than a hold — the two arms are separated
// deliberately, and this is the sale arm.
func TestPurchaseLimitRefusesTheCheckoutPastTheAllowance(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Rationed Fest", "rationed-fest", 1000, 50, 2)

	// A first checkout AT the limit is ordinary and succeeds.
	begun := beginCheckoutOK(t, env, testOrgSlug, "rationed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	if got := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", got.Status)
	}

	// One more of the same Ticket Type is refused: 2 held + 1 asked > 2 allowed.
	details := refusedByPurchaseLimit(t, env, "rationed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 2, 2, 1)

	// The refusal precedes the Payment: only the settled checkout left one behind,
	// so no abandoned pending Payment holds capacity nobody can claim.
	if got := paymentCount(t, env, eventID); got != 1 {
		t.Fatalf("payments on the Event = %d, want 1 — the refused checkout must create none", got)
	}
	// Capacity is untouched by a refusal, and the Ticket Sale that did happen is
	// exactly as recorded.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count = %d, want 2", got)
	}
}

// TestPurchaseLimitAggregatesTheWholeCart: a cart naming one Ticket Type on
// several lines is one buyer asking for the sum. Judged line by line, three
// requests of one would each pass a limit of two.
func TestPurchaseLimitAggregatesTheWholeCart(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Split Fest", "split-fest", 1000, 50, 2)

	// 1 + 2 across two lines is a request for 3, and refused as one.
	details := refusedByPurchaseLimit(t, env, "split-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1), cartLine(gaID, 2)))
	assertLimitDetails(t, details, gaID, 2, 0, 3)

	// The same shape summing to the limit is allowed, so the aggregation is a sum
	// and not a refusal of repeated lines.
	beginCheckoutOK(t, env, testOrgSlug, "split-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1), cartLine(gaID, 1)))
}

// TestPurchaseLimitIsKeyedOnTheCustomer: the allowance follows the person, not
// the checkout. A second address is a second allowance — that is the deterrent
// ADR 0025 accepts — while the SAME address typed with different casing is the
// same Customer, because normalisation is one rule for the whole platform
// (ADR 0010) and no channel may reach a different answer by casing alone.
func TestPurchaseLimitIsKeyedOnTheCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Keyed Fest", "keyed-fest", 1000, 50, 1)

	begun := beginCheckoutOK(t, env, testOrgSlug, "keyed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	// A different person is unaffected: the limit is one person's share of the
	// stock, never the stock itself.
	other := beginCheckoutOK(t, env, testOrgSlug, "keyed-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, other.ClientTransactionID, "approved")

	// The same person, shouted: refused, and the holdings counted are the ones
	// recorded under the lower-cased identity.
	details := refusedByPurchaseLimit(t, env, "keyed-fest",
		checkoutBody("  ANA@Example.COM  ", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 1, 1)
}

// TestLiveCapacityHoldConsumesThePurchaseLimit: a pending Payment inside the hold
// window counts against the allowance, which is what stops a buyer with five
// payment pages open settling five times. Past the window it counts for nothing
// and the allowance is back — the same created_at cutoff that releases capacity
// (ADR 0013), with no bookkeeping of its own.
func TestLiveCapacityHoldConsumesThePurchaseLimit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Tabbed Fest", "tabbed-fest", 1000, 50, 1)

	// A payment page left open on the provider's side. No sale exists — the
	// allowance this consumes is consumed by the HOLD and by nothing else.
	//
	// The Customer does exist, because she signed in to reach the dialog at all
	// (ADR 0054); what a pending Payment still writes nowhere is a SALE, which is
	// what the limit would otherwise be counting.
	first := beginCheckoutOK(t, env, testOrgSlug, "tabbed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	var sales int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_sales`).Scan(&sales); err != nil {
		t.Fatalf("count ticket sales: %v", err)
	}
	if sales != 0 {
		t.Fatalf("Ticket Sales for the buyer = %d, want 0 — a pending Payment records none", sales)
	}

	// Typed with different casing, so this also pins the hold arm's own matching:
	// a pending Payment records the email VERBATIM and has no Customer to point at,
	// and it must still be recognised as the same person's hold.
	details := refusedByPurchaseLimit(t, env, "tabbed-fest",
		checkoutBody("ANA@Example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 1, 1)

	// Past the hold window the first attempt speaks for nothing and the identical
	// second checkout succeeds.
	holdClocksAt(afterHoldWindow())
	beginCheckoutOK(t, env, testOrgSlug, "tabbed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	if got := paymentStatus(t, env, first.ClientTransactionID); got != "expired" {
		t.Fatalf("abandoned payment status = %q, want expired", got)
	}
}

// TestFailedPaymentReleasesThePurchaseLimit: a declined Payment releases the
// allowance immediately, inside the hold window, because the hold arm counts only
// PENDING Payments. A buyer whose card was refused is not locked out of the
// Ticket Type they never got.
func TestFailedPaymentReleasesThePurchaseLimit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Declined Fest", "declined-fest", 1000, 50, 1)

	begun := beginCheckoutOK(t, env, testOrgSlug, "declined-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	if got := confirmCheckoutOK(t, env, begun.ClientTransactionID, "declined"); got.Status != "failed" {
		t.Fatalf("confirm status = %q, want failed", got.Status)
	}

	// The clock has not moved: what freed the allowance is the Payment's verdict,
	// not the hold window lapsing.
	retry := beginCheckoutOK(t, env, testOrgSlug, "declined-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, retry.ClientTransactionID, "approved")

	// And now the allowance is spent for real.
	details := refusedByPurchaseLimit(t, env, "declined-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 1, 1)
}

// TestReversedTicketSaleReturnsThePurchaseLimit: an undone Ticket Sale returns the
// buyer's allowance exactly as it returns the seat to capacity. The sale arm
// counts ACTIVE sales, so a reversal needs nothing of its own to keep in step.
//
// Driven on a Free Ticket Type because the whole journey — claim, refusal, undo,
// re-claim — then runs with no Payment Provider anywhere in it (ADR 0017).
func TestReversedTicketSaleReturnsThePurchaseLimit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Undone Fest", "undone-fest", 0, 50, 1)

	ref := claimFree(t, env, "undone-fest", gaID, "ana@example.com", 1)
	details := refusedByPurchaseLimit(t, env, "undone-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 1, 1)

	undoOwnSale(t, env, "ana@example.com", ref)

	// The allowance is back, and the buyer claims again.
	approvedRef(t, beginCheckoutSettled(t, env, testOrgSlug, "undone-fest", "",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1))))
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 1 {
		t.Fatalf("active Ticket Sales for the buyer = %d, want 1 — the reversed one no longer counts", got)
	}
}

// TestFreeTicketTypePurchaseLimitRefusesTheSecondClaim is the case the Purchase
// Limit was introduced for: a Free Ticket Type nobody may take a hundred of. The
// first claim settles inside begin-checkout and the second is refused, with no
// Payment Provider asked anything on either path.
func TestFreeTicketTypePurchaseLimitRefusesTheSecondClaim(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Free Ration", "free-ration", 0, 100, 1)

	claim := beginCheckoutSettled(t, env, testOrgSlug, "free-ration", "",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	approvedRef(t, claim)
	if claim.AmountCents != 0 {
		t.Fatalf("amount_cents = %d, want 0 on a free claim", claim.AmountCents)
	}

	details := refusedByPurchaseLimit(t, env, "free-ration",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 1, 1)

	// One Payment exists — the settled free one, recorded for the sale that
	// happened. The refusal added nothing.
	if got := paymentCount(t, env, eventID); got != 1 {
		t.Fatalf("payments on the Event = %d, want 1", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 1 {
		t.Fatalf("sold_count = %d, want 1", got)
	}
}

// TestLoweringThePurchaseLimitBelowHoldingsIsAccepted: a Purchase Limit governs
// future checkouts only. Lowering it below what a Customer already holds is an
// ordinary edit — nothing is reversed, flagged or alerted on — and `held > limit`
// is a legitimate state that every read must still render. Raising it again lets
// the refused Customer buy.
func TestLoweringThePurchaseLimitBelowHoldingsIsAccepted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Lowered Fest", "lowered-fest", 1000, 50, 3)

	begun := beginCheckoutOK(t, env, testOrgSlug, "lowered-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 3)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	lowered := setPurchaseLimit(t, env, sessionID, eventID, gaID, 1000, 50, intPtr(1))
	assertPurchaseLimit(t, lowered, 1, "lowered staff read")

	// The Ticket Sale is untouched and both reads still render, with the new rule
	// and the old holdings side by side.
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 1 {
		t.Fatalf("active Ticket Sales = %d, want 1 — lowering a limit reverses nothing", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 3 {
		t.Fatalf("sold_count = %d, want 3", got)
	}
	assertPurchaseLimit(t, publicTicketTypeWithLimit(t, env, "lowered-fest", "GA"), 1, "lowered public read")

	// Further checkouts by that Customer are refused, and already_held is reported
	// as it truly is: above the limit.
	details := refusedByPurchaseLimit(t, env, "lowered-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 3, 1)

	// Raised above their holdings, the same Customer buys again.
	assertPurchaseLimit(t, setPurchaseLimit(t, env, sessionID, eventID, gaID, 1000, 50, intPtr(5)), 5, "raised staff read")
	beginCheckoutOK(t, env, testOrgSlug, "lowered-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
}

// TestPurchaseLimitIsNotRecheckedWhenTheSaleCommits guards the decision, not an
// omission (ADR 0025).
//
// It stages the pathological interleaving the ADR accepts: a Payment whose hold
// has lapsed, a second checkout that legitimately sees a clear allowance, and then
// BOTH confirmed. Capacity is re-checked under lock at commit because overselling
// a venue is a physical failure; the Purchase Limit is not, because refusing there
// would mean the Payment Provider holds the buyer's money for a sale the platform
// declined — the PAYMENT_APPROVED_WITHOUT_SALE incident a Platform Operator
// resolves by hand. The accepted worst case is one Customer holding limit+1, and
// that is what this asserts.
func TestPurchaseLimitIsNotRecheckedWhenTheSaleCommits(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Interleaved Fest", "interleaved-fest", 1000, 50, 1)

	first := beginCheckoutOK(t, env, testOrgSlug, "interleaved-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))

	// The first payment page is abandoned long enough for its hold to lapse, so
	// the second checkout sees an unspent allowance and is right to.
	holdClocksAt(afterHoldWindow())
	second := beginCheckoutOK(t, env, testOrgSlug, "interleaved-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))

	// The buyer then returns to BOTH pages. A confirm past the window is still
	// confirmable (ADR 0013), and neither commit consults the Purchase Limit.
	for _, ctID := range []string{first.ClientTransactionID, second.ClientTransactionID} {
		if got := confirmCheckoutOK(t, env, ctID, "approved"); got.Status != "approved" {
			t.Fatalf("confirm status = %q, want approved — the commit must not re-check the Purchase Limit", got.Status)
		}
		status, saleID, _ := paymentRecord(t, env, ctID)
		if status != "approved" || saleID == nil {
			t.Fatalf("payment status=%q ticket_sale_id=%v, want approved WITH a sale — an approved Payment without one is the PAYMENT_APPROVED_WITHOUT_SALE incident", status, saleID)
		}
	}

	// limit+1 held, exactly as the ADR accepts.
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 2 {
		t.Fatalf("active Ticket Sales = %d, want 2 — the accepted worst case is limit+1", got)
	}

	// And the over-limit state reads back honestly rather than as a corruption:
	// the next checkout is refused, reporting holdings above the limit.
	details := refusedByPurchaseLimit(t, env, "interleaved-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 2, 1)
}

// TestCapacityAndPurchaseLimitRefusalsAreDistinguishable: the two refusals never
// blur into one 409. A request the Event cannot fill is CAPACITY_EXCEEDED; a
// request the buyer's own allowance cannot cover is PURCHASE_LIMIT_EXCEEDED; and
// a request breaching both reports the limit, because that is the fact the buyer
// can act on — CAPACITY_EXCEEDED names an `available` figure and would invite a
// smaller retry the limit refuses just the same.
func TestCapacityAndPurchaseLimitRefusalsAreDistinguishable(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// Capacity 3 with an allowance of 4: the two ceilings are independent, and a
	// request can cross either one first.
	_, gaID := publishRationedEvent(t, env, sessionID, "Both Fest", "both-fest", 1000, 3, 4)

	// Within the allowance, past the stock: the Event is what cannot fill this.
	resp, body := beginCheckout(t, env, testOrgSlug, "both-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 4)))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("status=%d error=%+v, want 409 CAPACITY_EXCEEDED", resp.StatusCode, body.Error)
	}
	if details, _ := body.Error.Details.(map[string]any); details["available"] != float64(3) {
		t.Fatalf("capacity details=%+v, want available=3", body.Error.Details)
	}

	// Past both: reported as the Purchase Limit.
	assertLimitDetails(t, refusedByPurchaseLimit(t, env, "both-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 5))), gaID, 4, 0, 5)

	// Neither breached, so nothing is refused: the ceilings only bite when crossed.
	beginCheckoutOK(t, env, testOrgSlug, "both-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
}

// TestUnrestrictedTicketTypeIgnoresHoldings is the promise the migration made:
// a Ticket Type with no Purchase Limit is not made harder to buy by this feature
// existing. The same Customer buys the same Ticket Type twice, which is exactly
// what a limit of 1 would have refused.
func TestUnrestrictedTicketTypeIgnoresHoldings(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Open Fest", "open-fest", 1000, 50)

	for i := 0; i < 2; i++ {
		begun := beginCheckoutOK(t, env, testOrgSlug, "open-fest",
			checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
		confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	}
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 2 {
		t.Fatalf("active Ticket Sales = %d, want 2 — an unrestricted Ticket Type counts nobody's holdings", got)
	}
}

// publicTicketTypeWithLimit reads one Ticket Type off the public Event page as a
// Storefront client sees it, Purchase Limit included. It exists so a test can
// prove the buyer-facing read still renders after a limit is lowered below what
// somebody holds — `held > limit` must be visible, not a state that breaks a page.
func publicTicketTypeWithLimit(t *testing.T, env *testEnv, eventSlug, name string) ticketTypeWithLimit {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/"+testOrgSlug+"/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var detail struct {
		TicketTypes []ticketTypeWithLimit `json:"ticket_types"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	for _, tt := range detail.TicketTypes {
		if tt.Name == name {
			return tt
		}
	}
	t.Fatalf("ticket type %q is not on the public Event page: %+v", name, detail.TicketTypes)
	return ticketTypeWithLimit{}
}

// --- Sale Import (#167) ---------------------------------------------------
//
// An Organization must not walk past its own Purchase Limit with a spreadsheet.
// The preview is the surface these tests read, because it is where every other
// row problem is already reported cell by cell, and the commit is exercised
// alongside it because a client can post a file straight to it without ever
// previewing.
//
// The load-bearing case is the RUNNING TALLY: two rows of one file for the same
// Customer count against each other. A file of a hundred identical rows that all
// passed would make the refusal decorative, so the second row being refused
// while the first is accepted is the assertion that matters most here.

// rationedImportRow is one data row of a Sale Import file. Only the buyer's
// email and the quantity vary in these tests: the Purchase Limit is keyed on the
// Customer and measured in tickets, and every other cell is scenery.
func rationedImportRow(email, firstName string, quantity int) string {
	return fmt.Sprintf("%s,%s,Lopez,GA,%d,cash,2026-07-01T10:00:00Z,", email, firstName, quantity)
}

// rationedImportFile assembles those rows into a file, numbered from 2 as the
// organizer sees them (row 1 is the header).
func rationedImportFile(rows ...string) []byte {
	return importCSV(plainImportHeader, rows...)
}

// commitImportFileRefused posts a file to the commit endpoint expecting it to be
// refused, and hands back the envelope so the caller can read the complaints.
func commitImportFileRefused(t *testing.T, env *testEnv, sessionID, eventID, idempotencyKey string, file []byte) envelope {
	t.Helper()
	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", file, map[string]string{"idempotency_key": idempotencyKey, "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("commit status=%d error=%+v, want 400 — the commit re-decides the Purchase Limit itself", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("error = %+v, want VALIDATION_FAILED", body.Error)
	}
	return body
}

// commitImportFileSkipping commits a file with some of its rows excluded — how
// an organizer drops the one row the preview refused — expecting acceptance.
func commitImportFileSkipping(t *testing.T, env *testEnv, sessionID, eventID, idempotencyKey string, file []byte, skipRows string) importResultBody {
	t.Helper()
	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", file, map[string]string{"idempotency_key": idempotencyKey, "source": "direct", "skip_rows": skipRows}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit status=%d error=%+v, want 201", resp.StatusCode, body.Error)
	}
	return importResult(t, body)
}

// refusedRowComplaint asserts a preview row was rejected for the Purchase Limit
// and returns the complaint the organizer reads beside the row.
//
// The complaint is blamed on the quantity cell on purpose: that is the cell the
// staff member edits (or the row they delete), and the preview panel puts the
// message beside it. There is no separate warnings channel to look in — a row
// over the allowance is blocking, exactly like a malformed email.
func refusedRowComplaint(t *testing.T, row previewRow) string {
	t.Helper()
	if row.Valid {
		t.Fatalf("row %d is valid; want it refused for the Purchase Limit", row.Row)
	}
	for _, e := range row.Errors {
		if e.Field == "quantity" && strings.Contains(e.Message, "Purchase Limit") {
			return e.Message
		}
	}
	t.Fatalf("row %d errors = %+v, want one on quantity naming the Purchase Limit", row.Row, row.Errors)
	return ""
}

func assertRowValid(t *testing.T, row previewRow, where string) {
	t.Helper()
	if !row.Valid {
		t.Fatalf("%s: row %d = %+v, want valid — only the offending rows are refused", where, row.Row, row.Errors)
	}
}

// TestSaleImportRefusesARowPastWhatTheCustomerAlreadyHolds is the tracer bullet
// for the import half: a Customer who took her allowance on the Storefront
// cannot be handed more of the same Ticket Type through a spreadsheet.
//
// The holdings the row collides with were made on ANOTHER channel, which is the
// point — the allowance belongs to the Customer, not to the channel that spent
// it — and the rest of the batch imports untouched once the offending row goes.
// TestSaleImportRefusesARowThatIsOverTheLimitOnItsOwn covers the case neither
// other complaint can word honestly: a single row asking for more than the
// Purchase Limit allows, by a Customer who holds nothing and has no earlier row
// in the file. The refusal must not say "they already hold 0" — that asserts a
// Ticket Sale which does not exist and sends the organizer looking for it.
func TestSaleImportRefusesARowThatIsOverTheLimitOnItsOwn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishRationedEvent(t, env, sessionID, "Import Solo Fest", "import-solo-fest", 1000, 50, 2)

	// Cora holds nothing at all; her one row asks for three against a limit of two.
	file := rationedImportFile(rationedImportRow("cora@example.com", "Cora", 3))
	res := previewImportFile(t, env, sessionID, eventID, file)
	if res.ValidRows != 0 {
		t.Fatalf("preview valid rows = %d, want 0", res.ValidRows)
	}
	complaint := refusedRowComplaint(t, res.Rows[0])
	if !strings.Contains(complaint, "Purchase Limit of 2") {
		t.Fatalf("complaint = %q, want it to state the Purchase Limit", complaint)
	}
	if strings.Contains(complaint, "already hold") {
		t.Fatalf("complaint = %q, but Cora holds nothing — it must not claim she does", complaint)
	}
	if strings.Contains(complaint, "in this file") {
		t.Fatalf("complaint = %q, but no earlier row spent her allowance", complaint)
	}

	// The refused row takes its quantity back out of the capacity figures with
	// it, so the preview never reports an import wanting tickets it has refused.
	for _, impact := range res.CapacityImpact {
		if impact.Requested != 0 {
			t.Fatalf("capacity impact for %s requests %d after the only row was refused, want 0",
				impact.TicketTypeID, impact.Requested)
		}
		if impact.Oversold {
			t.Fatalf("capacity impact for %s reports oversold on a refused row", impact.TicketTypeID)
		}
	}
}

func TestSaleImportRefusesARowPastWhatTheCustomerAlreadyHolds(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Import Held Fest", "import-held-fest", 1000, 50, 2)

	// Ana takes her whole allowance online, settled through to an active Ticket Sale.
	begun := beginCheckoutOK(t, env, testOrgSlug, "import-held-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	file := rationedImportFile(
		rationedImportRow("ana@example.com", "Ana", 1),
		rationedImportRow("bea@example.com", "Bea", 2),
	)
	res := previewImportFile(t, env, sessionID, eventID, file)
	if res.TotalRows != 2 || res.ValidRows != 1 {
		t.Fatalf("preview counts = %d valid / %d total, want 1 / 2", res.ValidRows, res.TotalRows)
	}
	if res.Committable {
		t.Fatal("committable = true with a row over the Purchase Limit")
	}
	complaint := refusedRowComplaint(t, res.Rows[0])
	if !strings.Contains(complaint, "already hold 2") {
		t.Fatalf("row 2 complaint = %q, want it to state what the Customer already holds", complaint)
	}
	if strings.Contains(complaint, "in this file") {
		t.Fatalf("row 2 complaint = %q, but nothing earlier in the file took her allowance", complaint)
	}
	assertRowValid(t, res.Rows[1], "Bea's row")

	// Committing the file anyway is refused per row, and records nothing at all.
	body := commitImportFileRefused(t, env, sessionID, eventID, "import-held-1", file)
	if got := fieldErrors(t, body)["rows[2].quantity"]; !strings.Contains(got, "Purchase Limit") {
		t.Fatalf("field errors = %v, want one on rows[2].quantity naming the Purchase Limit", fieldErrors(t, body))
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count = %d, want 2 — a blocked batch is all-or-nothing", got)
	}

	// Dropping the one offending row imports the rest, which is the whole promise:
	// one bad cell never costs the organizer the batch.
	if res := commitImportFileOK(t, env, sessionID, eventID, "import-held-2",
		rationedImportFile(rationedImportRow("bea@example.com", "Bea", 2))); res.SaleCount != 1 {
		t.Fatalf("sale_count = %d, want 1", res.SaleCount)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 4 {
		t.Fatalf("sold_count = %d, want 4", got)
	}
}

// TestSaleImportCountsRowsInTheSameFileAgainstEachOther is the core of #167:
// with a limit of one, a file carrying two rows for Ana has the FIRST accepted
// and the SECOND refused, even though she held nothing when it was uploaded.
// Without the running tally a single file of identical rows would defeat the
// limit outright.
//
// The second row spells her address differently, so the tally is proved to be
// keyed on the Customer (ADR 0010) rather than on the literal cell — two
// spellings are one person and share one allowance.
//
// Its complaint NAMES the earlier row and reads differently from the
// already-holds case: one is fixed in the spreadsheet, the other is not, and a
// staff member told the wrong one goes looking in the wrong place.
func TestSaleImportCountsRowsInTheSameFileAgainstEachOther(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Import Tally Fest", "import-tally-fest", 1000, 50, 1)

	file := rationedImportFile(
		rationedImportRow("ana@example.com", "Ana", 1),
		rationedImportRow("ANA@Example.COM", "Ana", 1),
		rationedImportRow("bea@example.com", "Bea", 1),
	)
	res := previewImportFile(t, env, sessionID, eventID, file)
	if res.TotalRows != 3 || res.ValidRows != 2 {
		t.Fatalf("preview counts = %d valid / %d total, want 2 / 3", res.ValidRows, res.TotalRows)
	}
	if res.Committable {
		t.Fatal("committable = true with a row over the Purchase Limit")
	}
	assertRowValid(t, res.Rows[0], "Ana's first row")
	assertRowValid(t, res.Rows[2], "Bea's row")

	complaint := refusedRowComplaint(t, res.Rows[1])
	if !strings.Contains(complaint, "row "+strconv.Itoa(res.Rows[0].Row)) {
		t.Fatalf("row 3 complaint = %q, want it to name row %d — the earlier row that took the allowance", complaint, res.Rows[0].Row)
	}
	if !strings.Contains(complaint, "in this file") {
		t.Fatalf("row 3 complaint = %q, want it to say the conflict is inside the file", complaint)
	}
	if strings.Contains(complaint, "already hold") {
		t.Fatalf("row 3 complaint = %q, but Ana held nothing before this upload", complaint)
	}

	// Skipping the offending row commits the other two: the refusal costs the
	// batch one row, never the upload.
	if got := commitImportFileSkipping(t, env, sessionID, eventID, "import-tally-1", file, "3"); got.SaleCount != 2 {
		t.Fatalf("sale_count = %d, want 2 (Ana once, Bea once)", got.SaleCount)
	}
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 1 {
		t.Fatalf("Ana's active Ticket Sales = %d, want 1", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count = %d, want 2", got)
	}
}

// TestImportedTicketSaleConsumesThePurchaseLimit closes the loop the other way
// round: a Customer given her ticket through a Sale Import cannot then take a
// second one on the Storefront.
//
// Nothing was written for this. An imported Ticket Sale is an ordinary active
// ticket_sales row against the Customer the import upserted, and the allowance
// is counted from those rows whatever channel made them — this test is what
// proves it rather than a second code path that would have to agree.
func TestImportedTicketSaleConsumesThePurchaseLimit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Import Consumes Fest", "import-consumes-fest", 1000, 50, 1)

	commitImportFileOK(t, env, sessionID, eventID, "import-consumes-1",
		rationedImportFile(rationedImportRow("ana@example.com", "Ana", 1)))

	details := refusedByPurchaseLimit(t, env, "import-consumes-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	assertLimitDetails(t, details, gaID, 1, 1, 1)
}

// TestRaisingThePurchaseLimitLetsARejectedFileImport is the documented route out
// for an Organization importing history recorded before they set a limit: raise
// or clear it, import, set it back. The rejection is not a dead end, and this is
// the accepted trade rather than a bug (ADR 0025).
func TestRaisingThePurchaseLimitLetsARejectedFileImport(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Import Raise Fest", "import-raise-fest", 1000, 50, 1)

	file := rationedImportFile(
		rationedImportRow("ana@example.com", "Ana", 1),
		rationedImportRow("ana@example.com", "Ana", 1),
	)
	if previewImportFile(t, env, sessionID, eventID, file).Committable {
		t.Fatal("committable = true at a limit of 1 for two rows of the same Customer")
	}

	raised := 2
	setPurchaseLimit(t, env, sessionID, eventID, gaID, 1000, 50, &raised)
	if !previewImportFile(t, env, sessionID, eventID, file).Committable {
		t.Fatal("committable = false after raising the Purchase Limit to 2")
	}
	commitImportFileOK(t, env, sessionID, eventID, "import-raise-1", file)
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 2 {
		t.Fatalf("Ana's active Ticket Sales = %d, want 2", got)
	}

	// Clearing it is the stronger form of the same escape: with no Purchase Limit
	// the allowance is not counted at all, and history imports whatever its size.
	setPurchaseLimit(t, env, sessionID, eventID, gaID, 1000, 50, nil)
	commitImportFileOK(t, env, sessionID, eventID, "import-raise-2",
		rationedImportFile(rationedImportRow("ana@example.com", "Ana", 5)))
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 7 {
		t.Fatalf("sold_count = %d, want 7", got)
	}
}

// TestUnrestrictedTicketTypeImportsAnyQuantity is the migration's promise on the
// import path: a Ticket Type with no Purchase Limit is not made harder to import
// against by this feature existing. One Customer, three rows, fifteen tickets —
// every one of which a limit would have refused.
func TestUnrestrictedTicketTypeImportsAnyQuantity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Import Open Fest", "import-open-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	file := rationedImportFile(
		rationedImportRow("ana@example.com", "Ana", 5),
		rationedImportRow("ana@example.com", "Ana", 5),
		rationedImportRow("ana@example.com", "Ana", 5),
	)
	res := previewImportFile(t, env, sessionID, eventID, file)
	if res.ValidRows != 3 || !res.Committable {
		t.Fatalf("preview = %d valid / committable %t, want 3 / true", res.ValidRows, res.Committable)
	}
	if got := commitImportFileOK(t, env, sessionID, eventID, "import-open-1", file); got.SaleCount != 3 {
		t.Fatalf("sale_count = %d, want 3", got.SaleCount)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 15 {
		t.Fatalf("sold_count = %d, want 15", got)
	}
}

// --- A signed-in Customer reads their own allowance (#168) ----------------
//
// The same count begin-checkout refuses on, stated on the Storefront Event page
// BEFORE the buyer fills anything in, so the picker can bound itself and a
// Ticket Type whose allowance is spent can say so instead of claiming to be sold
// out. The security property is the reason these tests exist at all: the figure
// is derived from the Customer Session the request carried and from nothing in
// the URL, so an anonymous read must reveal nothing about anybody and there must
// be no way to ask about an address you have not proven you own (ADR 0025).

// publicTicketTypeHoldings is the public Ticket Type as a Storefront client
// reads it once a Customer Session is in play: the Ticket Type's own Purchase
// Limit, plus what THIS reader already holds of it.
//
// AlreadyHeld is a pointer because null and 0 are different statements — "we do
// not know who you are" against "you hold none of these" — and a test that let
// one stand for the other would prove nothing about the anonymous read.
type publicTicketTypeHoldings struct {
	ticketTypeWithLimit
	AlreadyHeld *int `json:"already_held"`
}

// readPublicEventAs reads the Storefront Event page as the holder of a Customer
// Session token, or anonymously when the token is empty, and returns its Ticket
// Types by name.
//
// It insists on 200 whatever the token is. That is the acceptance criterion for
// a route that took on OptionalCustomerSession: the Event page is public, and an
// absent, expired or garbage token is an ordinary visitor rather than an error.
func readPublicEventAs(t *testing.T, env *testEnv, eventSlug, token string) map[string]publicTicketTypeHoldings {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	resp, body := env.get(t, "/api/v1/public/organizations/"+testOrgSlug+"/events/"+eventSlug, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var detail struct {
		TicketTypes []publicTicketTypeHoldings `json:"ticket_types"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	byName := map[string]publicTicketTypeHoldings{}
	for _, tt := range detail.TicketTypes {
		byName[tt.Name] = tt
	}
	return byName
}

func ticketTypeNamed(t *testing.T, types map[string]publicTicketTypeHoldings, name string) publicTicketTypeHoldings {
	t.Helper()
	tt, ok := types[name]
	if !ok {
		t.Fatalf("ticket type %q is not on the public Event page: %+v", name, types)
	}
	return tt
}

func assertAlreadyHeld(t *testing.T, tt publicTicketTypeHoldings, want int, where string) {
	t.Helper()
	if tt.AlreadyHeld == nil {
		t.Fatalf("%s: already_held = null, want %d — a Customer we can identify is owed a number", where, want)
	}
	if *tt.AlreadyHeld != want {
		t.Fatalf("%s: already_held = %d, want %d", where, *tt.AlreadyHeld, want)
	}
}

// assertHoldingsUnknown is the privacy assertion, and it insists on null rather
// than accepting a zero: a 0 would be a statement about a person the platform
// has not identified, and it is exactly the answer an oracle gives.
func assertHoldingsUnknown(t *testing.T, tt publicTicketTypeHoldings, where string) {
	t.Helper()
	if tt.AlreadyHeld != nil {
		t.Fatalf("%s: already_held = %d, want null — this read identified nobody", where, *tt.AlreadyHeld)
	}
}

// TestPublicEventStatesTheSignedInCustomersOwnHoldings is the tracer bullet: a
// Customer who has taken part of their allowance loads the Event page and is
// told so, per Ticket Type, before typing anything.
//
// A second Ticket Type with no Purchase Limit is read alongside it, because
// already_held is reported for every Ticket Type a known Customer reads: null
// must go on meaning "anonymous" and never double as "unrestricted", which is
// what max_per_customer says.
func TestPublicEventStatesTheSignedInCustomersOwnHoldings(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Allowance Fest", "allowance-fest", 1000, 50, 3)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/ticket-types", map[string]any{
		"name":        "Open Tier",
		"price_cents": 1000,
		"capacity":    50,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create unrestricted ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}

	begun := beginCheckoutOK(t, env, testOrgSlug, "allowance-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	token := customerSignIn(t, env, "ana@example.com")
	mine := readPublicEventAs(t, env, "allowance-fest", token)
	ga := ticketTypeNamed(t, mine, "GA")
	assertAlreadyHeld(t, ga, 2, "signed-in buyer's own Ticket Type")
	assertPurchaseLimit(t, ga.ticketTypeWithLimit, 3, "signed-in read")
	open := ticketTypeNamed(t, mine, "Open Tier")
	assertAlreadyHeld(t, open, 0, "unrestricted Ticket Type the buyer holds none of")
	assertNoPurchaseLimit(t, open.ticketTypeWithLimit, "unrestricted Ticket Type")

	// Everybody else's page is untouched by what Ana bought — including another
	// signed-in Customer's, which is what proves the figure is the CALLER's and
	// not the Ticket Type's.
	other := readPublicEventAs(t, env, "allowance-fest", customerSignIn(t, env, "bruno@example.com"))
	assertAlreadyHeld(t, ticketTypeNamed(t, other, "GA"), 0, "a different signed-in Customer")
}

// TestAnonymousPublicEventRevealsNoHoldings is the security property of #168: a
// read that identifies nobody says nothing about anybody. The Purchase Limit
// itself is public — it is the Ticket Type's rule — but who has spent theirs is
// not, and no query parameter, header or body may ask about an address the
// caller has not proven they own (ADR 0025).
func TestAnonymousPublicEventRevealsNoHoldings(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Anon Fest", "anon-fest", 1000, 50, 2)

	begun := beginCheckoutOK(t, env, testOrgSlug, "anon-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	ga := ticketTypeNamed(t, readPublicEventAs(t, env, "anon-fest", ""), "GA")
	assertHoldingsUnknown(t, ga, "anonymous read of an Event whose allowance somebody has spent")
	assertPurchaseLimit(t, ga.ticketTypeWithLimit, 2, "anonymous read")

	// The one thing an anonymous caller might try: naming the address. Neither
	// spelling exists, and neither is allowed to start answering — a page that
	// took an email here would tell anyone whether it had bought a ticket.
	for _, query := range []string{"?email=ana@example.com", "?customer_email=ana@example.com"} {
		probe := ticketTypeNamed(t, readPublicEventAs(t, env, "anon-fest"+query, ""), "GA")
		assertHoldingsUnknown(t, probe, "anonymous read carrying "+query)
	}
}

// TestDeadCustomerTokenReadsThePublicEventAsAGuest: the middleware is optional
// in both directions. Garbage in Authorization, and a token whose session has
// been destroyed, both read the Event page exactly as an anonymous visitor does
// — 200 with no holdings, never a 401. A Storefront holding a stale token must
// not lose the Event page over it.
func TestDeadCustomerTokenReadsThePublicEventAsAGuest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Stale Fest", "stale-fest", 1000, 50, 2)

	begun := beginCheckoutOK(t, env, testOrgSlug, "stale-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	assertHoldingsUnknown(t, ticketTypeNamed(t, readPublicEventAs(t, env, "stale-fest", "not-a-session-token"), "GA"),
		"read carrying a garbage token")

	// A real token, signed out from under itself. It named Ana a moment ago and
	// must reveal nothing about her now.
	token := customerSignIn(t, env, "ana@example.com")
	assertAlreadyHeld(t, ticketTypeNamed(t, readPublicEventAs(t, env, "stale-fest", token), "GA"), 1, "while the session is alive")
	if resp, body := env.post(t, customerLogoutPath, nil, authHeader(token)); resp.StatusCode != http.StatusOK {
		t.Fatalf("sign out status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertHoldingsUnknown(t, ticketTypeNamed(t, readPublicEventAs(t, env, "stale-fest", token), "GA"),
		"read carrying a destroyed session's token")
}

// TestPublicEventHoldingsCountALiveCapacityHold: the page counts the allowance
// exactly as begin-checkout does — both arms — so a Customer with the provider's
// payment page open in another tab is told the tickets on it are already theirs
// to the tune of the Purchase Limit, and is not invited to start a second
// checkout that would only be refused (ADR 0025).
func TestPublicEventHoldingsCountALiveCapacityHold(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Held Fest", "held-fest", 1000, 50, 2)

	// Begun and left on the provider's payment page: a pending Payment, no Ticket
	// Sale, and the Customer record here is the sign-in's rather than the sale's.
	beginCheckoutOK(t, env, testOrgSlug, "held-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 1)))

	token := customerSignIn(t, env, "ana@example.com")
	assertAlreadyHeld(t, ticketTypeNamed(t, readPublicEventAs(t, env, "held-fest", token), "GA"),
		1, "a live Capacity Hold of the signed-in Customer")

	// Past the hold window the abandoned checkout speaks for nothing, and the
	// allowance is visibly back — the page needs no bookkeeping of its own to say
	// so, exactly as capacity does not.
	holdClocksAt(afterHoldWindow())
	assertAlreadyHeld(t, ticketTypeNamed(t, readPublicEventAs(t, env, "held-fest", token), "GA"),
		0, "after the hold window lapsed")
}

// TestReversedSaleDropsOutOfThePublicEventHoldings: an undone Ticket Sale
// returns the allowance on the page as surely as it returns the seat to
// capacity, because the sale arm counts ACTIVE sales and nothing else.
//
// Driven on a Free Ticket Type so the whole journey — claim, read, undo, read —
// runs with no Payment Provider in it (ADR 0017).
func TestReversedSaleDropsOutOfThePublicEventHoldings(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishRationedEvent(t, env, sessionID, "Undone Page", "undone-page", 0, 50, 1)

	ref := claimFree(t, env, "undone-page", gaID, "ana@example.com", 1)
	token := customerSignIn(t, env, "ana@example.com")
	spent := ticketTypeNamed(t, readPublicEventAs(t, env, "undone-page", token), "GA")
	assertAlreadyHeld(t, spent, 1, "after claiming the whole allowance")
	assertPurchaseLimit(t, spent.ticketTypeWithLimit, 1, "spent allowance")

	undoOwnSale(t, env, "ana@example.com", ref)

	assertAlreadyHeld(t, ticketTypeNamed(t, readPublicEventAs(t, env, "undone-page", token), "GA"),
		0, "after undoing the claim")
}

// TestPublicEventHoldingsSurviveALoweredPurchaseLimit: lowering a Purchase Limit
// is never retroactive, so already_held may legitimately EXCEED max_per_customer
// and the page must state both plainly. Remaining allowance is max(0, limit -
// already_held); nothing here is clamped, corrected or alerted on (ADR 0025).
func TestPublicEventHoldingsSurviveALoweredPurchaseLimit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishRationedEvent(t, env, sessionID, "Lowered Page", "lowered-page", 1000, 50, 3)

	begun := beginCheckoutOK(t, env, testOrgSlug, "lowered-page",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 3)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	lowered := 1
	setPurchaseLimit(t, env, sessionID, eventID, gaID, 1000, 50, &lowered)

	token := customerSignIn(t, env, "ana@example.com")
	ga := ticketTypeNamed(t, readPublicEventAs(t, env, "lowered-page", token), "GA")
	assertAlreadyHeld(t, ga, 3, "holdings above a lowered Purchase Limit")
	assertPurchaseLimit(t, ga.ticketTypeWithLimit, 1, "lowered Purchase Limit")
}
