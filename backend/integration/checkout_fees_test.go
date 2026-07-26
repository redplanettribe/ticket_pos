package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The buyer side of the Platform Fee (issue #91): what a Customer is quoted,
// what they are charged, and what the recorded sale remembers about the split
// (ADR 0014). Prices are $7.99-shaped on purpose — a 10% fee of 799¢ is 79.9¢,
// so every figure below depends on the rounding, not on round numbers.
//
// At the launch rates a $7.99 ticket withholds 80¢ of Platform Fee and 12¢ of
// Fee IVA: buyers pay 891¢ under pass_on and 799¢ under absorb, and the
// Organization nets 799¢ / 707¢ respectively.
const (
	feeTestBaseCents  = 799
	feeTestFeeCents   = 80
	feeTestIVACents   = 12
	feeTestAllInCents = 891
)

// setFeeHandling puts an Event into a Fee Handling mode through the staff event
// form, the way an organizer would.
func setFeeHandling(t *testing.T, env *testEnv, sessionID, eventID, name, slug, mode string) {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":         name,
		"slug":         slug,
		"starts_at":    env.fixedClock.Add(72 * time.Hour).Format(time.RFC3339),
		"timezone":     "America/Guayaquil",
		"fee_handling": mode,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set fee_handling=%s status=%d error=%+v", mode, resp.StatusCode, body.Error)
	}
}

// lineFees is the per-unit fee snapshot recorded on a payment or sale line.
// SQL, because no API exposes the split: what the Customer sees is one all-in
// price, and the platform's cut is never displayed as a number.
type lineFees struct {
	Quantity          int
	UnitPriceCents    int
	BasePriceCents    int
	FeeCents          int
	FeeIVACents       int
	FeeBasisPoints    int
	FeeIVABasisPoints int
}

func paymentLineFees(t *testing.T, env *testEnv, clientTransactionID, ticketTypeID string) lineFees {
	t.Helper()
	var l lineFees
	if err := env.db.QueryRow(`
		SELECT pl.quantity, pl.unit_price_cents, pl.base_price_cents, pl.fee_cents,
		       pl.fee_iva_cents, pl.fee_basis_points, pl.fee_iva_basis_points
		FROM payment_lines pl
		JOIN payments p ON p.id = pl.payment_id
		WHERE p.client_transaction_id = $1 AND pl.ticket_type_id = $2
	`, clientTransactionID, ticketTypeID).Scan(&l.Quantity, &l.UnitPriceCents, &l.BasePriceCents,
		&l.FeeCents, &l.FeeIVACents, &l.FeeBasisPoints, &l.FeeIVABasisPoints); err != nil {
		t.Fatalf("read payment line: %v", err)
	}
	return l
}

func saleLineFees(t *testing.T, env *testEnv, confirmationRef, ticketTypeID string) lineFees {
	t.Helper()
	var l lineFees
	if err := env.db.QueryRow(`
		SELECT tsl.quantity, tsl.unit_price_cents, tsl.base_price_cents, tsl.fee_cents,
		       tsl.fee_iva_cents, tsl.fee_basis_points, tsl.fee_iva_basis_points
		FROM ticket_sale_lines tsl
		JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
		WHERE ts.confirmation_ref = $1 AND tsl.ticket_type_id = $2
	`, confirmationRef, ticketTypeID).Scan(&l.Quantity, &l.UnitPriceCents, &l.BasePriceCents,
		&l.FeeCents, &l.FeeIVACents, &l.FeeBasisPoints, &l.FeeIVABasisPoints); err != nil {
		t.Fatalf("read sale line: %v", err)
	}
	return l
}

// paymentAmount is the gross the Customer was charged, as recorded.
func paymentAmount(t *testing.T, env *testEnv, clientTransactionID string) int {
	t.Helper()
	var amount int
	if err := env.db.QueryRow(`
		SELECT amount_cents FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&amount); err != nil {
		t.Fatalf("read payment amount: %v", err)
	}
	return amount
}

// publicTicketPrices returns the buyer prices the Storefront is quoted per
// Ticket Type name, plus the flag that decides the muted "includes service fee"
// note.
func publicTicketPrices(t *testing.T, env *testEnv, eventSlug string) (map[string]int, bool) {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var detail struct {
		PriceIncludesFee bool `json:"price_includes_fee"`
		TicketTypes      []struct {
			Name       string `json:"name"`
			PriceCents int    `json:"price_cents"`
		} `json:"ticket_types"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	prices := map[string]int{}
	for _, tt := range detail.TicketTypes {
		prices[tt.Name] = tt.PriceCents
	}
	return prices, detail.PriceIncludesFee
}

// TestPassOnCheckoutChargesTheAllInPriceAndSnapshotsTheSplit is the pass-on
// tracer bullet: the price quoted publicly, the amount charged, the Payment's
// snapshot, the sale's lines, the staff Sales row, and the Customer's receipt
// are all the same all-in number, and the recorded split is exact.
func TestPassOnCheckoutChargesTheAllInPriceAndSnapshotsTheSplit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Pass On Fest", "pass-on-fest", feeTestBaseCents, 10)
	// A comp Ticket Type: base 0 yields nothing to withhold, in either mode.
	compID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Comp", 0, 5)

	// pass_on is the default; setting it explicitly documents what is under test.
	setFeeHandling(t, env, sessionID, eventID, "Pass On Fest", "pass-on-fest", "pass_on")

	prices, includesFee := publicTicketPrices(t, env, "pass-on-fest")
	if prices["GA"] != feeTestAllInCents {
		t.Fatalf("public GA price = %d; want the all-in %d", prices["GA"], feeTestAllInCents)
	}
	if prices["Comp"] != 0 {
		t.Fatalf("public comp price = %d; want 0", prices["Comp"])
	}
	if !includesFee {
		t.Fatal("public payload says the pass-on price carries no fee; the Storefront would drop the note")
	}

	begin := beginCheckoutOK(t, env, "test-org", "pass-on-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": gaID, "quantity": 2},
			map[string]any{"ticket_type_id": compID, "quantity": 1}))
	if begin.AmountCents != 2*feeTestAllInCents {
		t.Fatalf("charged amount = %d; want 2×%d", begin.AmountCents, feeTestAllInCents)
	}
	if got := paymentAmount(t, env, begin.ClientTransactionID); got != begin.AmountCents {
		t.Fatalf("recorded amount_cents = %d; want the gross charged %d", got, begin.AmountCents)
	}

	ga := paymentLineFees(t, env, begin.ClientTransactionID, gaID)
	want := lineFees{
		Quantity: 2, UnitPriceCents: feeTestAllInCents, BasePriceCents: feeTestBaseCents,
		FeeCents: feeTestFeeCents, FeeIVACents: feeTestIVACents,
		FeeBasisPoints: 1000, FeeIVABasisPoints: 1500,
	}
	if ga != want {
		t.Fatalf("payment line = %+v; want %+v", ga, want)
	}
	comp := paymentLineFees(t, env, begin.ClientTransactionID, compID)
	if comp.UnitPriceCents != 0 || comp.BasePriceCents != 0 || comp.FeeCents != 0 || comp.FeeIVACents != 0 {
		t.Fatalf("comp payment line = %+v; want no money anywhere in it", comp)
	}

	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	if confirm.Status != "approved" {
		t.Fatalf("confirm status = %q; want approved", confirm.Status)
	}

	// Confirm copies the snapshot onto the sale's lines verbatim.
	if got := saleLineFees(t, env, confirm.ConfirmationRef, gaID); got != want {
		t.Fatalf("sale line = %+v; want the payment's snapshot %+v", got, want)
	}

	// The staff Sales row shows what the Customer paid.
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	list := salesList(t, body)
	if len(list.Data) != 1 || list.Data[0].AmountCents != 2*feeTestAllInCents {
		t.Fatalf("sales row amounts = %+v; want one row of %d", list.Data, 2*feeTestAllInCents)
	}

	// So does the Sale Confirmation email.
	confs := env.email.Confirmations()
	if len(confs) != 1 {
		t.Fatalf("confirmations = %d; want 1", len(confs))
	}
	if confs[0].AmountCents != 2*feeTestAllInCents || confs[0].Currency != "USD" {
		t.Fatalf("receipt total = %d %s; want %d USD", confs[0].AmountCents, confs[0].Currency, 2*feeTestAllInCents)
	}
}

// TestAbsorbCheckoutChargesTheSetPriceAndStillWithholds: the Customer sees
// exactly the price the Organization set — no fee mention, no note — while the
// same withholding is recorded against the Organization's proceeds.
func TestAbsorbCheckoutChargesTheSetPriceAndStillWithholds(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Absorb Fest", "absorb-fest", feeTestBaseCents, 10)
	setFeeHandling(t, env, sessionID, eventID, "Absorb Fest", "absorb-fest", "absorb")

	prices, includesFee := publicTicketPrices(t, env, "absorb-fest")
	if prices["GA"] != feeTestBaseCents {
		t.Fatalf("public GA price = %d; want the set price %d", prices["GA"], feeTestBaseCents)
	}
	if includesFee {
		t.Fatal("absorb payload claims the price includes a fee; clean prices must stay clean")
	}

	begin := beginCheckoutOK(t, env, "test-org", "absorb-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	if begin.AmountCents != 2*feeTestBaseCents {
		t.Fatalf("charged amount = %d; want 2×%d", begin.AmountCents, feeTestBaseCents)
	}

	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	want := lineFees{
		Quantity: 2, UnitPriceCents: feeTestBaseCents, BasePriceCents: feeTestBaseCents,
		FeeCents: feeTestFeeCents, FeeIVACents: feeTestIVACents,
		FeeBasisPoints: 1000, FeeIVABasisPoints: 1500,
	}
	if got := saleLineFees(t, env, confirm.ConfirmationRef, gaID); got != want {
		t.Fatalf("absorb sale line = %+v; want %+v", got, want)
	}
}

// TestFeeHandlingFlipDoesNotMoveAPendingPayment: a Fee Handling change takes
// effect on future checkouts only — the Payment already on the provider's page
// settles at the price its snapshot froze.
func TestFeeHandlingFlipDoesNotMoveAPendingPayment(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Flip Fest", "flip-fest", feeTestBaseCents, 10)

	begin := beginCheckoutOK(t, env, "test-org", "flip-fest",
		checkoutBody("guest@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if begin.AmountCents != feeTestAllInCents {
		t.Fatalf("charged amount = %d; want the pass-on %d", begin.AmountCents, feeTestAllInCents)
	}

	// The organizer flips to absorb while the Customer is still paying.
	setFeeHandling(t, env, sessionID, eventID, "Flip Fest", "flip-fest", "absorb")

	confirm := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	want := lineFees{
		Quantity: 1, UnitPriceCents: feeTestAllInCents, BasePriceCents: feeTestBaseCents,
		FeeCents: feeTestFeeCents, FeeIVACents: feeTestIVACents,
		FeeBasisPoints: 1000, FeeIVABasisPoints: 1500,
	}
	if got := saleLineFees(t, env, confirm.ConfirmationRef, gaID); got != want {
		t.Fatalf("sale line after the flip = %+v; want the frozen snapshot %+v", got, want)
	}
	if got := paymentAmount(t, env, begin.ClientTransactionID); got != feeTestAllInCents {
		t.Fatalf("amount_cents after the flip = %d; want the charged %d", got, feeTestAllInCents)
	}

	// The next Customer gets the new mode.
	next := beginCheckoutOK(t, env, "test-org", "flip-fest",
		checkoutBody("second@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if next.AmountCents != feeTestBaseCents {
		t.Fatalf("post-flip amount = %d; want the absorbed %d", next.AmountCents, feeTestBaseCents)
	}
}

// TestNonOnlineSalesCarryNoFee: the fee is withheld from money the platform
// actually holds, so an imported (or in-person) sale records no withholding at
// all — whatever the Event's Fee Handling says.
func TestNonOnlineSalesCarryNoFee(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Cash Fest", "cash-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", feeTestBaseCents, 50)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "cash-batch",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
				"ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var l lineFees
	if err := env.db.QueryRow(`
		SELECT tsl.quantity, tsl.unit_price_cents, tsl.base_price_cents, tsl.fee_cents,
		       tsl.fee_iva_cents, tsl.fee_basis_points, tsl.fee_iva_basis_points
		FROM ticket_sale_lines tsl
		JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
		WHERE ts.event_id = $1
	`, eventID).Scan(&l.Quantity, &l.UnitPriceCents, &l.BasePriceCents,
		&l.FeeCents, &l.FeeIVACents, &l.FeeBasisPoints, &l.FeeIVABasisPoints); err != nil {
		t.Fatalf("read imported sale line: %v", err)
	}
	// The base price is still recorded — it is what the Organization sold at —
	// but nothing is withheld and no rates were read.
	want := lineFees{Quantity: 2, UnitPriceCents: feeTestBaseCents, BasePriceCents: feeTestBaseCents}
	if l != want {
		t.Fatalf("imported sale line = %+v; want the set price and no fee: %+v", l, want)
	}
}
