package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Online checkout through the public API with the stub Payment Provider
// (issue #83): a guest begins a checkout on a published Event and settles it
// via the same begin → redirect → confirm legs the real provider drives. The
// harness sets no PAYPHONE_* credentials, so the stub is selected exactly as it
// is in local development (ADR 0009/0012).

// publishCheckoutEvent creates and publishes an Event with one Ticket Type,
// returning both ids. The event gets a future start so it is publishable.
func publishCheckoutEvent(t *testing.T, env *testEnv, sessionID, name, slug string, priceCents, capacity int) (eventID, ticketTypeID string) {
	t.Helper()
	eventID = createDraftEvent(t, env, sessionID, name, slug)
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":       name,
		"slug":       slug,
		"starts_at":  env.fixedClock.Add(72 * time.Hour).Format(time.RFC3339),
		"timezone":   "America/Guayaquil",
		"venue_name": "The Hall",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	ticketTypeID = createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", priceCents, capacity)
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return eventID, ticketTypeID
}

// checkoutBody builds a begin-checkout request body for a guest.
func checkoutBody(email, firstName, lastName string, lines ...map[string]any) map[string]any {
	return map[string]any{
		"customer_email":      email,
		"customer_first_name": firstName,
		"customer_last_name":  lastName,
		"lines":               lines,
	}
}

func beginCheckout(t *testing.T, env *testEnv, orgSlug, eventSlug string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/public/organizations/"+orgSlug+"/events/"+eventSlug+"/checkout", body, nil)
}

type beginCheckoutResult struct {
	ClientTransactionID string `json:"client_transaction_id"`
	RedirectURL         string `json:"redirect_url"`
	AmountCents         int    `json:"amount_cents"`
	Currency            string `json:"currency"`
}

func beginCheckoutOK(t *testing.T, env *testEnv, orgSlug, eventSlug string, body map[string]any) beginCheckoutResult {
	t.Helper()
	resp, envBody := beginCheckout(t, env, orgSlug, eventSlug, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("begin checkout status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	var result beginCheckoutResult
	if err := json.Unmarshal(envBody.Data, &result); err != nil {
		t.Fatalf("decode begin checkout result: %v", err)
	}
	if result.ClientTransactionID == "" || result.RedirectURL == "" {
		t.Fatalf("begin checkout result incomplete: %+v", result)
	}
	return result
}

// confirmCheckout settles a Payment, relaying the stub interstitial's outcome
// param the way the Storefront return handler will (ticket #84).
func confirmCheckout(t *testing.T, env *testEnv, clientTransactionID, outcome string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, "/api/v1/public/checkout/"+clientTransactionID+"/confirm", map[string]any{
		"provider_params": map[string]string{"outcome": outcome},
	}, nil)
}

type confirmCheckoutResult struct {
	ClientTransactionID string `json:"client_transaction_id"`
	Status              string `json:"status"`
	ConfirmationRef     string `json:"confirmation_ref"`
}

func confirmCheckoutOK(t *testing.T, env *testEnv, clientTransactionID, outcome string) confirmCheckoutResult {
	t.Helper()
	resp, body := confirmCheckout(t, env, clientTransactionID, outcome)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm checkout status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var result confirmCheckoutResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode confirm checkout result: %v", err)
	}
	return result
}

// paymentRecord reads a Payment row directly: no public API exposes Payment
// state (a Payment is not a sale), so the lifecycle assertions go to SQL.
func paymentRecord(t *testing.T, env *testEnv, clientTransactionID string) (status string, ticketSaleID *string, providerTransactionID *string) {
	t.Helper()
	err := env.db.QueryRow(`
		SELECT status, ticket_sale_id, provider_transaction_id
		FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&status, &ticketSaleID, &providerTransactionID)
	if err != nil {
		t.Fatalf("read payment: %v", err)
	}
	return status, ticketSaleID, providerTransactionID
}

// customerCountByEmail counts Customer records for an email. SQL because no
// public API lists Customer records; the platform-global upsert is exactly the
// side effect under test.
func customerCountByEmail(t *testing.T, env *testEnv, email string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customers WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count customers: %v", err)
	}
	return n
}

func TestBeginCheckoutRejectsDraftAndCancelledEvents(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// Draft: created but never published. Like the public event page, checkout
	// does not acknowledge it exists.
	draftID := createDraftEvent(t, env, sessionID, "Draft Fest", "draft-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, draftID, "GA", 1000, 10)
	resp, body := beginCheckout(t, env, "test-org", "draft-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ttID, "quantity": 1}))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("draft event status=%d, want 404", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("draft event error=%+v, want EVENT_NOT_FOUND", body.Error)
	}

	// Cancelled: published then cancelled — terminal, no longer sellable.
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Doomed Fest", "doomed-fest", 1500, 10)
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/cancel", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = beginCheckout(t, env, "test-org", "doomed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cancelled event status=%d, want 404", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("cancelled event error=%+v, want EVENT_NOT_FOUND", body.Error)
	}
}

func TestBeginCheckoutValidationErrors(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Valid Fest", "valid-fest", 1000, 10)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"zero quantity", checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 0})},
		{"negative quantity", checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": -3})},
		{"no lines", checkoutBody("ana@example.com", "Ana", "Lopez")},
		{"malformed ticket type id", checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": "not-a-uuid", "quantity": 1})},
		{"invalid email", checkoutBody("not-an-email", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1})},
		{"missing name", checkoutBody("ana@example.com", "", "", map[string]any{"ticket_type_id": gaID, "quantity": 1})},
	}
	for _, tc := range cases {
		resp, body := beginCheckout(t, env, "test-org", "valid-fest", tc.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d, want 400", tc.name, resp.StatusCode)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%s: error=%+v, want VALIDATION_FAILED", tc.name, body.Error)
		}
	}

	// No Payment was created by any rejected begin.
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&n); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if n != 0 {
		t.Fatalf("payments after rejected begins = %d, want 0", n)
	}
}

func TestBeginCheckoutUnknownTicketType(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishCheckoutEvent(t, env, sessionID, "Known Fest", "known-fest", 1000, 10)

	// A well-formed id that names no Ticket Type on this Event.
	resp, body := beginCheckout(t, env, "test-org", "known-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": "c0000000-0000-4000-8000-000000000009", "quantity": 1}))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "TICKET_TYPE_NOT_FOUND" {
		t.Fatalf("error=%+v, want TICKET_TYPE_NOT_FOUND", body.Error)
	}
}

func TestBeginCheckoutOverCapacity(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Tiny Fest", "tiny-fest", 1000, 3)

	resp, body := beginCheckout(t, env, "test-org", "tiny-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 4}))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("error=%+v, want CAPACITY_EXCEEDED", body.Error)
	}

	// Split across duplicate lines of the same Ticket Type: the check must see
	// the request whole, not per line.
	resp, body = beginCheckout(t, env, "test-org", "tiny-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": gaID, "quantity": 2},
			map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("split lines status=%d, want 409", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "CAPACITY_EXCEEDED" {
		t.Fatalf("split lines error=%+v, want CAPACITY_EXCEEDED", body.Error)
	}
}

// TestOnlineCheckoutApproveHappyPath is the tracer bullet: begin → stub
// redirect → confirm(approve) produces a real Online Sale with correct lines,
// price snapshots, sold_count, Customer upsert, and a captured Sale
// Confirmation email.
func TestOnlineCheckoutApproveHappyPath(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Online Fest", "online-fest", 1000, 10)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 5)

	begin := beginCheckoutOK(t, env, "test-org", "online-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": gaID, "quantity": 2},
			map[string]any{"ticket_type_id": vipID, "quantity": 1}))

	// The stub redirect URL is the contract the Storefront interstitial (#84)
	// is built against: storefront origin, /checkout/stub, and the params.
	redirect, err := url.Parse(begin.RedirectURL)
	if err != nil {
		t.Fatalf("parse redirect url: %v", err)
	}
	if got := redirect.Scheme + "://" + redirect.Host; got != "http://storefront.example" {
		t.Fatalf("redirect origin = %q, want the storefront origin", got)
	}
	if redirect.Path != "/checkout/stub" {
		t.Fatalf("redirect path = %q, want /checkout/stub", redirect.Path)
	}
	q := redirect.Query()
	if q.Get("client_transaction_id") != begin.ClientTransactionID {
		t.Fatalf("redirect client_transaction_id = %q, want %q", q.Get("client_transaction_id"), begin.ClientTransactionID)
	}
	if q.Get("amount_cents") != "7000" || begin.AmountCents != 7000 {
		t.Fatalf("amount = %s/%d, want 7000 (2×1000 + 1×5000)", q.Get("amount_cents"), begin.AmountCents)
	}
	if begin.Currency != "USD" || q.Get("currency") != "USD" {
		t.Fatalf("currency = %q/%q, want USD", begin.Currency, q.Get("currency"))
	}
	if q.Get("response_url") != "http://storefront.example/checkout/return" {
		t.Fatalf("response_url = %q, want the storefront return route", q.Get("response_url"))
	}

	// Nothing is sold while the Payment is pending: capacity is check-only.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Fatalf("sold_count before confirm = %d, want 0", got)
	}

	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirm.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirm.Status)
	}
	if !strings.HasPrefix(confirm.ConfirmationRef, "TP-") {
		t.Fatalf("confirmation ref = %q, want a TP- reference", confirm.ConfirmationRef)
	}

	// Capacity moved exactly by the approved quantities.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("GA sold_count = %d, want 2", got)
	}
	if got := soldCount(t, env, sessionID, eventID, vipID); got != 1 {
		t.Fatalf("VIP sold_count = %d, want 1", got)
	}

	// The Ticket Sale appears in the staff Sales list as an Online Sale: channel
	// online, no Sales Source, Payment Method payphone, lines rolled up.
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	list := salesList(t, body)
	if len(list.Data) != 1 {
		t.Fatalf("sales rows = %d, want 1", len(list.Data))
	}
	sale := list.Data[0]
	if sale.Channel != "online" {
		t.Fatalf("channel = %q, want online", sale.Channel)
	}
	if sale.Source != nil {
		t.Fatalf("source = %v, want none on an Online Sale", *sale.Source)
	}
	if sale.PaymentMethod == nil || *sale.PaymentMethod != "payphone" {
		t.Fatalf("payment_method = %v, want payphone", sale.PaymentMethod)
	}
	if sale.AmountCents != 7000 {
		t.Fatalf("sale amount = %d, want 7000", sale.AmountCents)
	}
	if sale.ConfirmationRef != confirm.ConfirmationRef {
		t.Fatalf("sale confirmation ref = %q, want %q", sale.ConfirmationRef, confirm.ConfirmationRef)
	}
	if sale.CustomerEmail != "guest@example.com" || sale.CustomerFirstName != "Ana" || sale.CustomerLastName != "Lopez" {
		t.Fatalf("sale customer = %s %s <%s>", sale.CustomerFirstName, sale.CustomerLastName, sale.CustomerEmail)
	}
	if len(sale.TicketTypes) != 2 {
		t.Fatalf("sale lines = %d, want 2", len(sale.TicketTypes))
	}
	lineQty := map[string]int{}
	for _, l := range sale.TicketTypes {
		lineQty[l.TicketTypeName] = l.Quantity
	}
	if lineQty["GA"] != 2 || lineQty["VIP"] != 1 {
		t.Fatalf("line quantities = %+v, want GA×2, VIP×1", lineQty)
	}

	// The guest Customer was upserted inside the sale's transaction.
	if got := customerCountByEmail(t, env, "guest@example.com"); got != 1 {
		t.Fatalf("customers for guest = %d, want 1", got)
	}

	// The Payment ends approved and linked to the sale it produced (SQL: no
	// public API exposes Payment rows).
	status, ticketSaleID, providerTxID := paymentRecord(t, env, begin.ClientTransactionID)
	if status != "approved" {
		t.Fatalf("payment status = %q, want approved", status)
	}
	if ticketSaleID == nil || *ticketSaleID != sale.ID {
		t.Fatalf("payment ticket_sale_id = %v, want %q", ticketSaleID, sale.ID)
	}
	if providerTxID == nil || *providerTxID == "" {
		t.Fatal("payment provider_transaction_id not recorded")
	}

	// The Sale Confirmation went out with the reference and a working link shape.
	confs := env.email.Confirmations()
	if len(confs) != 1 {
		t.Fatalf("sale confirmations sent = %d, want 1", len(confs))
	}
	if confs[0].To != "guest@example.com" || confs[0].Reference != confirm.ConfirmationRef {
		t.Fatalf("confirmation = %+v, want ref %s to guest@example.com", confs[0], confirm.ConfirmationRef)
	}
	if confs[0].EventName != "Online Fest" {
		t.Fatalf("confirmation event = %q, want Online Fest", confs[0].EventName)
	}
	if confs[0].ConfirmationLink == "" {
		t.Fatal("confirmation carried no Confirmation Link")
	}
}

// TestOnlineCheckoutReusesExistingCustomer proves the platform-global upsert:
// a Customer who already exists (from any Sales Channel) is reused, never
// duplicated, by an Online Sale.
func TestOnlineCheckoutReusesExistingCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Repeat Fest", "repeat-fest", 1000, 10)

	// The Customer record first arrives through the import channel.
	commitBatch(t, env, sessionID, eventID, "seed-customer", []map[string]any{
		{"customer_email": "regular@example.com", "customer_first_name": "Rita", "customer_last_name": "Mora",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	if got := customerCountByEmail(t, env, "regular@example.com"); got != 1 {
		t.Fatalf("customers after import = %d, want 1", got)
	}

	begin := beginCheckoutOK(t, env, "test-org", "repeat-fest",
		checkoutBody("Regular@Example.com", "Rita", "Mora", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirm.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirm.Status)
	}

	// Same person (email normalised), still one platform-global Customer, now
	// with two active Ticket Sales referencing it. The join goes through
	// customer_id because each sale keeps the email verbatim as entered
	// ("Regular@Example.com" here) while the Customer row holds the normalised
	// one — the very distinction under test. SQL because no public API lists a
	// Customer's sales across channels.
	if got := customerCountByEmail(t, env, "regular@example.com"); got != 1 {
		t.Fatalf("customers after checkout = %d, want 1 (reused, not duplicated)", got)
	}
	var salesForCustomer int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_sales ts
		JOIN customers c ON c.id = ts.customer_id
		WHERE c.email = $1 AND ts.status = 'active'
	`, "regular@example.com").Scan(&salesForCustomer); err != nil {
		t.Fatalf("count sales for customer: %v", err)
	}
	if salesForCustomer != 2 {
		t.Fatalf("sales for customer = %d, want 2", salesForCustomer)
	}
}

// TestConfirmCheckoutIdempotentReplay pins the refresh/back-button property: a
// repeat confirm returns the recorded outcome without a second sale, a second
// reference, or a second email — even if the replay relays a different outcome.
func TestConfirmCheckoutIdempotentReplay(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Replay Fest", "replay-fest", 1000, 10)

	begin := beginCheckoutOK(t, env, "test-org", "replay-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	first := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// Replay with the same outcome…
	replay := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if replay.Status != "approved" || replay.ConfirmationRef != first.ConfirmationRef {
		t.Fatalf("replay = %+v, want approved with ref %s", replay, first.ConfirmationRef)
	}
	// …and with a contradictory one: the recorded outcome still wins, because a
	// settled Payment never consults the provider again.
	contradictory := confirmCheckoutOK(t, env, begin.ClientTransactionID, "declined")
	if contradictory.Status != "approved" || contradictory.ConfirmationRef != first.ConfirmationRef {
		t.Fatalf("contradictory replay = %+v, want the recorded approved outcome", contradictory)
	}

	if got := salesCountByEmail(t, env, eventID, "guest@example.com"); got != 1 {
		t.Fatalf("sales = %d, want 1 (no double commit)", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Fatalf("sold_count = %d, want 2 (unchanged by replays)", got)
	}
	if got := len(env.email.Confirmations()); got != 1 {
		t.Fatalf("confirmations sent = %d, want 1 (replays re-send nothing)", got)
	}
}

// TestOnlineCheckoutDecline pins the failed leg: a declined Payment ends
// failed, no Ticket Sale exists, capacity is untouched, and no email goes out.
// A later "approve" replay cannot resurrect it.
func TestOnlineCheckoutDecline(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Declined Fest", "declined-fest", 1000, 10)

	begin := beginCheckoutOK(t, env, "test-org", "declined-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	declined := confirmCheckoutOK(t, env, begin.ClientTransactionID, "declined")
	if declined.Status != "failed" {
		t.Fatalf("confirm status = %q, want failed", declined.Status)
	}
	if declined.ConfirmationRef != "" {
		t.Fatalf("confirmation ref = %q, want none on a failed Payment", declined.ConfirmationRef)
	}

	status, ticketSaleID, _ := paymentRecord(t, env, begin.ClientTransactionID)
	if status != "failed" || ticketSaleID != nil {
		t.Fatalf("payment = %s/%v, want failed with no sale", status, ticketSaleID)
	}
	if got := salesCountByEmail(t, env, eventID, "guest@example.com"); got != 0 {
		t.Fatalf("sales = %d, want 0", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Fatalf("sold_count = %d, want 0", got)
	}
	if got := len(env.email.Confirmations()); got != 0 {
		t.Fatalf("confirmations sent = %d, want 0", got)
	}

	// A failed Payment is settled: replaying with "approved" returns the
	// recorded failure and still sells nothing.
	replay := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if replay.Status != "failed" {
		t.Fatalf("replay status = %q, want failed", replay.Status)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 0 {
		t.Fatalf("sold_count after replay = %d, want 0", got)
	}
}

// TestOnlineCheckoutPriceSnapshot pins the "price you saw is the price you
// pay" rule: a catalog price edit between begin and confirm does not change
// what the committed sale records.
func TestOnlineCheckoutPriceSnapshot(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Snapshot Fest", "snapshot-fest", 1000, 10)

	begin := beginCheckoutOK(t, env, "test-org", "snapshot-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if begin.AmountCents != 2000 {
		t.Fatalf("begin amount = %d, want 2000", begin.AmountCents)
	}

	// The organizer doubles the price mid-payment.
	raiseTicketTypeCapacity(t, env, sessionID, eventID, gaID, "GA", 9999, 10)

	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirm.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirm.Status)
	}

	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	list := salesList(t, body)
	if len(list.Data) != 1 {
		t.Fatalf("sales rows = %d, want 1", len(list.Data))
	}
	if list.Data[0].AmountCents != 2000 {
		t.Fatalf("sale amount = %d, want the 2000 snapshot, not the edited price", list.Data[0].AmountCents)
	}
}

func TestConfirmCheckoutUnknownPayment(t *testing.T) {
	env := setupTest(t)

	resp, body := confirmCheckout(t, env, "no-such-client-transaction-id", "approved")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "PAYMENT_NOT_FOUND" {
		t.Fatalf("error=%+v, want PAYMENT_NOT_FOUND", body.Error)
	}
}
