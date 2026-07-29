package integration

import (
	"net/http"
	"testing"
	"time"
)

// Checkout prices from the Promotion while one is live (issue #139, ADR 0021).
// The Promotional Price replaces the List Price as the base the Platform Fee
// arithmetic starts from, and begin-checkout snapshots the answer: everything
// downstream — the Payment's lines, the sale copied from them at confirm, the
// Sales list, Net Proceeds — reads that snapshot and knows nothing about
// Promotions.
//
// Prices are chosen so nothing is a round number: a Promotional Price of 499¢
// withholds 50¢ of Platform Fee (49.9 rounded half-up) and 8¢ of Fee IVA (7.5
// rounded half-up), so a pass-on buyer pays 557¢ against the 891¢ the List
// Price of 799¢ would have cost them.
const (
	promoPriceCents     = 499
	promoFeeCents       = 50
	promoIVACents       = 8
	promoAllInCents     = 557
	promoListPriceCents = feeTestBaseCents
)

// setPromotion puts a Promotion on a Ticket Type through the staff endpoint,
// the way an organizer would. A nil startsAt means live from the moment it is
// saved.
func setPromotion(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string, promotionalPriceCents int, startsAt *time.Time, endsAt time.Time) {
	t.Helper()
	body := map[string]any{
		"promotional_price_cents": promotionalPriceCents,
		"ends_at":                 endsAt.UTC().Format(time.RFC3339),
	}
	if startsAt != nil {
		body["starts_at"] = startsAt.UTC().Format(time.RFC3339)
	}
	resp, envBody := env.post(t, promotionPath(eventID, ticketTypeID), body, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("set promotion status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
}

// removePromotion takes the Promotion off a Ticket Type through the staff
// endpoint, which is what an organizer does when a sale ends early. The Ticket
// Type is priced from its List Price again the moment it returns.
func removePromotion(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string) {
	t.Helper()
	resp, body := env.deleteJSON(t, promotionPath(eventID, ticketTypeID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove promotion status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if removed := decodeTicketTypeWithPromotion(t, body.Data); removed.Promotion != nil {
		t.Fatalf("promotion still on the Ticket Type after removal: %+v", removed.Promotion)
	}
}

// promotionalLine is the per-unit snapshot a promoted line must carry under a
// given Fee Handling: the base is the Promotional Price, and the withholding is
// a percentage of it rather than of the List Price.
func promotionalLine(quantity int, buyerUnitCents int) lineFees {
	return lineFees{
		Quantity: quantity, UnitPriceCents: buyerUnitCents, BasePriceCents: promoPriceCents,
		FeeCents: promoFeeCents, FeeIVACents: promoIVACents,
		FeeBasisPoints: 1000, FeeIVABasisPoints: 1500,
	}
}

// TestPassOnCheckoutChargesThePromotionalPriceWhileLive: the buyer pays the
// all-in price built on the Promotional Price, and the Payment's line records
// the fee taken on that base.
func TestPassOnCheckoutChargesThePromotionalPriceWhileLive(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Promo Pass On", "promo-pass-on", promoListPriceCents, 20)
	setFeeHandling(t, env, sessionID, eventID, "Promo Pass On", "promo-pass-on", "pass_on")
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))

	begin := beginCheckoutOK(t, env, "test-org", "promo-pass-on",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if begin.AmountCents != 2*promoAllInCents {
		t.Fatalf("charged amount = %d; want 2×%d built on the Promotional Price", begin.AmountCents, promoAllInCents)
	}
	want := promotionalLine(2, promoAllInCents)
	if got := paymentLineFees(t, env, begin.ClientTransactionID, gaID); got != want {
		t.Fatalf("payment line = %+v; want %+v", got, want)
	}

	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if got := saleLineFees(t, env, confirm.ConfirmationRef, gaID); got != want {
		t.Fatalf("sale line = %+v; want the Payment's snapshot %+v", got, want)
	}
}

// TestAbsorbCheckoutChargesThePromotionalPriceWhileLive: under absorb the buyer
// pays exactly the Promotional Price, and the same withholding comes out of the
// Organization's proceeds.
func TestAbsorbCheckoutChargesThePromotionalPriceWhileLive(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Promo Absorb", "promo-absorb", promoListPriceCents, 20)
	setFeeHandling(t, env, sessionID, eventID, "Promo Absorb", "promo-absorb", "absorb")
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))

	begin := beginCheckoutOK(t, env, "test-org", "promo-absorb",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if begin.AmountCents != 2*promoPriceCents {
		t.Fatalf("charged amount = %d; want 2×%d, the Promotional Price itself", begin.AmountCents, promoPriceCents)
	}
	want := promotionalLine(2, promoPriceCents)
	if got := paymentLineFees(t, env, begin.ClientTransactionID, gaID); got != want {
		t.Fatalf("absorb payment line = %+v; want %+v", got, want)
	}
}

// TestCheckoutChargesTheListPriceOutsideThePromotionWindow: a Promotion that
// has not opened yet prices nothing, and one that has closed prices nothing
// either — the window is half-open, and both ends of it are checked at
// begin-checkout against the clock.
func TestCheckoutChargesTheListPriceOutsideThePromotionWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Window Fest", "window-fest", promoListPriceCents, 20)
	startsAt := env.fixedClock.Add(24 * time.Hour)
	endsAt := env.fixedClock.Add(48 * time.Hour)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, &startsAt, endsAt)

	before := beginCheckoutOK(t, env, "test-org", "window-fest",
		checkoutBody("early@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if before.AmountCents != feeTestAllInCents {
		t.Fatalf("amount before the window = %d; want the List Price all-in %d", before.AmountCents, feeTestAllInCents)
	}
	wantList := lineFees{
		Quantity: 1, UnitPriceCents: feeTestAllInCents, BasePriceCents: promoListPriceCents,
		FeeCents: feeTestFeeCents, FeeIVACents: feeTestIVACents,
		FeeBasisPoints: 1000, FeeIVABasisPoints: 1500,
	}
	if got := paymentLineFees(t, env, before.ClientTransactionID, gaID); got != wantList {
		t.Fatalf("pre-window payment line = %+v; want the List Price %+v", got, wantList)
	}

	// Inside the window the same cart is priced from the Promotion.
	holdClocksAt(env.fixedClock.Add(30 * time.Hour))
	inside := beginCheckoutOK(t, env, "test-org", "window-fest",
		checkoutBody("timely@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if inside.AmountCents != promoAllInCents {
		t.Fatalf("amount inside the window = %d; want the promotional all-in %d", inside.AmountCents, promoAllInCents)
	}

	// Past the end the List Price is back, with no staff action in between.
	holdClocksAt(env.fixedClock.Add(72 * time.Hour))
	after := beginCheckoutOK(t, env, "test-org", "window-fest",
		checkoutBody("late@example.com", "Caro", "Diaz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if after.AmountCents != feeTestAllInCents {
		t.Fatalf("amount after the window = %d; want the List Price all-in %d", after.AmountCents, feeTestAllInCents)
	}
	if got := paymentLineFees(t, env, after.ClientTransactionID, gaID); got != wantList {
		t.Fatalf("post-window payment line = %+v; want the List Price %+v", got, wantList)
	}
}

// TestPromotionExpiringMidPaymentKeepsTheSnapshottedPrice: the price is locked
// at begin-checkout, so a Customer already on the Payment Provider's page when
// the Promotion closes still settles at the Promotional Price (ADR 0021).
func TestPromotionExpiringMidPaymentKeepsTheSnapshottedPrice(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Expiry Fest", "expiry-fest", promoListPriceCents, 20)
	endsAt := env.fixedClock.Add(2 * time.Hour)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, endsAt)

	begin := beginCheckoutOK(t, env, "test-org", "expiry-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if begin.AmountCents != 2*promoAllInCents {
		t.Fatalf("charged amount = %d; want 2×%d", begin.AmountCents, promoAllInCents)
	}

	// The Promotion closes while the Customer is still paying.
	holdClocksAt(env.fixedClock.Add(3 * time.Hour))

	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	want := promotionalLine(2, promoAllInCents)
	if got := saleLineFees(t, env, confirm.ConfirmationRef, gaID); got != want {
		t.Fatalf("sale line after expiry = %+v; want the frozen promotional snapshot %+v", got, want)
	}
	if got := paymentAmount(t, env, begin.ClientTransactionID); got != 2*promoAllInCents {
		t.Fatalf("amount_cents after expiry = %d; want the charged %d", got, 2*promoAllInCents)
	}

	rows := eventSales(t, env, sessionID, eventID)
	if len(rows) != 1 || rows[0].AmountCents != 2*promoAllInCents {
		t.Fatalf("sales rows = %+v; want one row of %d", rows, 2*promoAllInCents)
	}
}

// TestZeroPromotionalPriceSettlesThroughTheFreePath: a Promotion may price a
// Ticket Type at zero, and a cart that totals nothing is settled in the
// begin-checkout request with no Payment Provider involved (ADR 0017). Nothing
// in that path knows a Promotion was responsible.
func TestZeroPromotionalPriceSettlesThroughTheFreePath(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Free Promo Fest", "free-promo-fest", promoListPriceCents, 20)
	setPromotion(t, env, sessionID, eventID, gaID, 0, nil, env.fixedClock.Add(48*time.Hour))

	result := beginCheckoutSettled(t, env, "test-org", "free-promo-fest", "",
		checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	ref := approvedRef(t, result)
	if result.AmountCents != 0 {
		t.Fatalf("amount = %d; want nothing to collect", result.AmountCents)
	}

	// SQL because no API surfaces which Payment Provider settled a checkout —
	// and "none was asked" is the whole claim of this test.
	var provider string
	if err := env.db.QueryRow(`
		SELECT provider FROM payments WHERE client_transaction_id = $1
	`, result.ClientTransactionID).Scan(&provider); err != nil {
		t.Fatalf("read payment provider: %v", err)
	}
	if provider != "free" {
		t.Fatalf("payment provider = %q; want free — no Payment Provider was asked", provider)
	}

	// The rates the online channel read are still snapshotted — they are what the
	// line was sold under — but there was no money for them to take a cut of.
	want := lineFees{Quantity: 2, UnitPriceCents: 0, BasePriceCents: 0, FeeBasisPoints: 1000, FeeIVABasisPoints: 1500}
	if got := saleLineFees(t, env, ref, gaID); got != want {
		t.Fatalf("free promotional sale line = %+v; want no money anywhere in it: %+v", got, want)
	}

	rows := eventSales(t, env, sessionID, eventID)
	if len(rows) != 1 || rows[0].AmountCents != 0 {
		t.Fatalf("sales rows = %+v; want one recorded sale of 0", rows)
	}
}

// TestSalesListAndNetProceedsFollowThePromotionalSnapshot: the staff surfaces
// derive from the recorded snapshot, so a promotional sale shows what the buyer
// paid and nets the Organization the Promotional Price it sold at.
func TestSalesListAndNetProceedsFollowThePromotionalSnapshot(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Promo Net Fest", "promo-net-fest", promoListPriceCents, 20)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))

	begin := beginCheckoutOK(t, env, "test-org", "promo-net-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 3}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	rows := eventSales(t, env, sessionID, eventID)
	if len(rows) != 1 || rows[0].AmountCents != 3*promoAllInCents {
		t.Fatalf("sales rows = %+v; want one row of %d", rows, 3*promoAllInCents)
	}

	// Pass-on: the Organization nets the price the ticket sold at, which is the
	// Promotional Price and not the List Price.
	got := salesSummaryOK(t, env, sessionID, eventID)
	want := salesSummary{NetProceedsCents: 3 * promoPriceCents, Currency: "USD", SalesCount: 1}
	if got != want {
		t.Fatalf("summary = %+v; want %+v", got, want)
	}
}

// TestReversingAPromotionalSaleReturnsTheSnapshottedAmounts: a Sale Reversal
// gives back what the sale recorded, never what the Ticket Type costs now.
//
// The Promotion is REMOVED between the purchase and the undo, so the catalog the
// reversal could re-price from quotes 891¢ while the sale was sold at 557¢. If
// anything downstream of the snapshot consulted the Ticket Type again — the
// provider reversal, the Sales list, the buyer's own Area — those two numbers
// would diverge, and the buyer would be handed back a price they never paid.
// The reversal reads the snapshot, so removing the Promotion changes nothing
// about the sale it left behind.
func TestReversingAPromotionalSaleReturnsTheSnapshottedAmounts(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Promo Undo Fest", "promo-undo-fest", promoListPriceCents, 10)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))

	begin := beginCheckoutOK(t, env, "test-org", "promo-undo-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	if begin.AmountCents != 2*promoAllInCents {
		t.Fatalf("charged amount = %d; want 2×%d", begin.AmountCents, promoAllInCents)
	}
	ref := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved").ConfirmationRef
	if got := remaining(t, env, "promo-undo-fest", "GA"); got != 8 {
		t.Fatalf("remaining after a promotional sale of 2 from 10 = %d, want 8", got)
	}
	env.email.Reset()

	// The Promotion ends before the buyer changes their mind: the Storefront is
	// back on the List Price, and the sale about to be undone is not.
	removePromotion(t, env, sessionID, eventID, gaID)
	if ga := publicTicketTypes(t, env, testOrgSlug, "promo-undo-fest")["GA"]; ga.PriceCents != feeTestAllInCents || ga.Promotion != nil {
		t.Fatalf("public GA = %+v after the Promotion was removed; want the List Price all-in %d and no promotion", ga, feeTestAllInCents)
	}

	token := customerSignIn(t, env, "ana@example.com")
	sale := saleByRef(t, readCustomerArea(t, env, token, ""), ref)
	if sale.AmountCents != 2*promoAllInCents {
		t.Fatalf("Customer Area amount = %d before the undo; want the %d they were charged", sale.AmountCents, 2*promoAllInCents)
	}
	if !sale.Reversible {
		t.Fatal("a promotional sale made this morning is not offered as reversible; the undo cannot be reached")
	}

	result := reverseSaleOK(t, env, token, sale.ID)
	if result.Status != "reversed" || result.ConfirmationRef != ref {
		t.Fatalf("undo result = %+v; want the same reference %q reversed", result, ref)
	}

	// The reversal's own promises: the sale is voided and the tickets are on sale
	// again at the price the catalog now carries.
	if status, _, reversedBy := saleProvenance(t, env, ref); status != "reversed" || !reversedBy.Valid || reversedBy.String != "customer" {
		t.Fatalf("stored status = %q reversed_by = %+v; want reversed by the customer", status, reversedBy)
	}
	if ga := publicTicketTypes(t, env, testOrgSlug, "promo-undo-fest")["GA"]; ga.Remaining != 10 || ga.SoldOut {
		t.Fatalf("public GA = %+v after the undo; want all 10 back on sale", ga)
	}
	if voided := env.email.Voided(); len(voided) != 1 || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v; want exactly one quoting %q", voided, ref)
	}

	// And the amounts: every surface still reports the promotional snapshot, the
	// List Price now on the Ticket Type nowhere among them.
	wantLine := promotionalLine(2, promoAllInCents)
	if got := saleLineFees(t, env, ref, gaID); got != wantLine {
		t.Fatalf("reversed sale line = %+v; want the promotional snapshot %+v, not a re-pricing from the catalog", got, wantLine)
	}
	// The default Sales list shows active sales only, so the reversed row is
	// asked for by name.
	rows := listSalesOK(t, env, sessionID, eventID, "?status=reversed").Data
	if len(rows) != 1 || rows[0].AmountCents != 2*promoAllInCents {
		t.Fatalf("reversed sales rows = %+v; want one row of %d", rows, 2*promoAllInCents)
	}
	after := saleByRef(t, readCustomerArea(t, env, token, ""), ref)
	if after.Status != "reversed" || after.AmountCents != 2*promoAllInCents {
		t.Fatalf("Customer Area sale after the undo = %+v; want it reversed and still showing the %d they paid",
			after, 2*promoAllInCents)
	}
}

// TestWithdrawableBalanceFollowsAPromotionalSaleAndItsReversal: the money the
// Organization may withdraw is the Net Proceeds of its active Online Sales, and
// a promotional sale contributes the Promotional Price it sold at — the discount
// comes out of the Organization's own take, not the platform's.
//
// The reversal then takes exactly that contribution back out, leaving the
// balance where it started. Nothing here is promotion-aware: the balance sums
// snapshots, and this proves the snapshot a Promotion produced is the one it
// sums.
func TestWithdrawableBalanceFollowsAPromotionalSaleAndItsReversal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Promo Balance Fest", "promo-balance-fest", promoListPriceCents, 10)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))

	if got := getPayouts(t, env, sessionID).WithdrawableBalanceCents; got != 0 {
		t.Fatalf("opening balance = %d; want nothing sold, nothing owed", got)
	}

	begin := beginCheckoutOK(t, env, "test-org", "promo-balance-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 3)))
	ref := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved").ConfirmationRef

	// Pass-on Fee Handling: the buyer paid the fee on top, so the Organization's
	// share is the Promotional Price itself — three times 499¢, not 799¢.
	wantBalance := 3 * promoPriceCents
	if got := getPayouts(t, env, sessionID).WithdrawableBalanceCents; got != wantBalance {
		t.Fatalf("balance after the promotional sale = %d; want its Net Proceeds %d", got, wantBalance)
	}

	undoOwnSale(t, env, "ana@example.com", ref)

	if got := getPayouts(t, env, sessionID).WithdrawableBalanceCents; got != 0 {
		t.Fatalf("balance after the undo = %d; want 0 — the reversed sale owes the Organization nothing", got)
	}
	// The Event's own strip agrees, so the two figures cannot drift apart.
	got := salesSummaryOK(t, env, sessionID, eventID)
	want := salesSummary{NetProceedsCents: 0, Currency: "USD", SalesCount: 0}
	if got != want {
		t.Fatalf("summary after the undo = %+v; want %+v", got, want)
	}
}
