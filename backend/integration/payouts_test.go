package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Organization's money surface (issue #93): the Withdrawable Balance — the
// Net Proceeds of its active Online Sales minus every recorded Payout — and the
// payout history beneath it (ADR 0014).
//
// The Organization never records a Payout: it only reads its own. Recording is
// the Platform Operator's write, on the operator surface (issue #95, ADR 0015),
// and operator_test.go covers it end to end — including that a Payout recorded
// there shows up here unchanged. These tests stage their fixtures with the same
// direct database write an operator used before that surface existed.

type payoutEntry struct {
	ID          string  `json:"id"`
	AmountCents int     `json:"amount_cents"`
	PaidAt      string  `json:"paid_at"`
	Note        *string `json:"note"`
}

type payoutsSummary struct {
	WithdrawableBalanceCents int `json:"withdrawable_balance_cents"`
	// PayableBalanceCents is the part of it the Organization may ask for today
	// (#174, ADR 0026). Exercised in payable_balance_test.go.
	PayableBalanceCents int           `json:"payable_balance_cents"`
	Currency            string        `json:"currency"`
	Payouts             []payoutEntry `json:"payouts"`
}

// recordPayout is the platform operator settling off-platform: a row, nothing
// more. note is stored as NULL when empty.
func recordPayout(t *testing.T, env *testEnv, orgSlug string, amountCents int, paidAt, note string) {
	t.Helper()
	var noteArg any
	if note != "" {
		noteArg = note
	}
	if _, err := env.db.Exec(`
		INSERT INTO payouts (organization_id, amount_cents, paid_at, note)
		SELECT id, $2, $3, $4 FROM organizations WHERE slug = $1
	`, orgSlug, amountCents, paidAt, noteArg); err != nil {
		t.Fatalf("record payout: %v", err)
	}
}

// reverseSale is shared with sales_summary_test.go.

func getPayouts(t *testing.T, env *testEnv, sessionID string) payoutsSummary {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/organization/payouts", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("payouts status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var summary payoutsSummary
	if err := json.Unmarshal(body.Data, &summary); err != nil {
		t.Fatalf("decode payouts: %v", err)
	}
	return summary
}

// TestWithdrawableBalanceSumsNetProceedsAndSubtractsPayouts walks the whole
// definition in one Organization: two Online Sales in different Fee Handling
// modes contribute their Net Proceeds, a reversed one contributes nothing, a
// cash import contributes nothing, and the recorded Payouts come off the top.
func TestWithdrawableBalanceSumsNetProceedsAndSubtractsPayouts(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// Nothing sold, nothing paid: an empty balance, not an error.
	empty := getPayouts(t, env, sessionID)
	if empty.WithdrawableBalanceCents != 0 || empty.Currency != "USD" || len(empty.Payouts) != 0 {
		t.Fatalf("opening balance = %+v; want 0 USD and no payouts", empty)
	}

	// A pass-on Event: the Organization nets exactly the price it set.
	passOnID, passOnGA := publishCheckoutEvent(t, env, sessionID, "Pass On Fest", "pass-on-fest", feeTestBaseCents, 10)
	setFeeHandling(t, env, sessionID, passOnID, "Pass On Fest", "pass-on-fest", "pass_on")

	// An absorb Event: the same withholding, taken out of the set price.
	absorbID, absorbGA := publishCheckoutEvent(t, env, sessionID, "Absorb Fest", "absorb-fest", feeTestBaseCents, 10)
	setFeeHandling(t, env, sessionID, absorbID, "Absorb Fest", "absorb-fest", "absorb")

	// 2 pass-on tickets: 2 × 799¢ net.
	begin := beginCheckoutOK(t, env, "test-org", "pass-on-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": passOnGA, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// 3 absorb tickets: 3 × (799 − 80 − 12) = 3 × 707¢ net.
	begin = beginCheckoutOK(t, env, "test-org", "absorb-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": absorbGA, "quantity": 3}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// A reversed Online Sale: refunded money is never withdrawable.
	begin = beginCheckoutOK(t, env, "test-org", "pass-on-fest",
		checkoutBody("cleo@example.com", "Cleo", "Diaz", map[string]any{"ticket_type_id": passOnGA, "quantity": 4}))
	reversed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	reverseSale(t, env, reversed.ConfirmationRef)

	// A cash sale the platform never held: no fee was withheld from it, and it
	// produces no Net Proceeds either.
	importSaleForBalance(t, env, sessionID, absorbID, absorbGA)

	netProceeds := 2*feeTestBaseCents + 3*(feeTestBaseCents-feeTestFeeCents-feeTestIVACents)

	before := getPayouts(t, env, sessionID)
	if before.WithdrawableBalanceCents != netProceeds {
		t.Fatalf("balance before payouts = %d; want the online net proceeds %d", before.WithdrawableBalanceCents, netProceeds)
	}
	if len(before.Payouts) != 0 {
		t.Fatalf("payout history = %+v; want none yet", before.Payouts)
	}

	recordPayout(t, env, "test-org", 1000, "2026-07-01", "June settlement")
	recordPayout(t, env, "test-org", 500, "2026-07-15", "")

	after := getPayouts(t, env, sessionID)
	if want := netProceeds - 1500; after.WithdrawableBalanceCents != want {
		t.Fatalf("balance after payouts = %d; want %d", after.WithdrawableBalanceCents, want)
	}
	if after.Currency != "USD" {
		t.Fatalf("currency = %q; want the Organization's USD", after.Currency)
	}
	// Newest first, and the optional note is optional.
	if len(after.Payouts) != 2 {
		t.Fatalf("payout history = %+v; want 2 entries", after.Payouts)
	}
	if after.Payouts[0].AmountCents != 500 || after.Payouts[0].PaidAt != "2026-07-15" || after.Payouts[0].Note != nil {
		t.Fatalf("newest payout = %+v; want 500¢ on 2026-07-15 with no note", after.Payouts[0])
	}
	if after.Payouts[1].AmountCents != 1000 || after.Payouts[1].PaidAt != "2026-07-01" ||
		after.Payouts[1].Note == nil || *after.Payouts[1].Note != "June settlement" {
		t.Fatalf("older payout = %+v; want 1000¢ on 2026-07-01 noted", after.Payouts[1])
	}
}

// TestWithdrawableBalanceGoesNegativeAfterARefund: a sale reversed after it has
// been paid out leaves the Organization owing the platform, and the balance says
// so rather than clamping at zero.
func TestWithdrawableBalanceGoesNegativeAfterARefund(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Refund Fest", "refund-fest", feeTestBaseCents, 10)

	begin := beginCheckoutOK(t, env, "test-org", "refund-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	sale := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// The platform settles the whole balance, then the sale is reversed.
	recordPayout(t, env, "test-org", feeTestBaseCents, "2026-07-20", "settled in full")
	reverseSale(t, env, sale.ConfirmationRef)

	summary := getPayouts(t, env, sessionID)
	if summary.WithdrawableBalanceCents != -feeTestBaseCents {
		t.Fatalf("balance = %d; want the negative %d", summary.WithdrawableBalanceCents, -feeTestBaseCents)
	}
}

// TestPayoutsAreScopedToTheActingOrganization: another Organization's proceeds
// and payouts are not this Organization's business.
func TestPayoutsAreScopedToTheActingOrganization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Mine Fest", "mine-fest", feeTestBaseCents, 10)
	begin := beginCheckoutOK(t, env, "test-org", "mine-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// The pre-seeded demo Organization gets a payout of its own; it must not
	// move the acting Organization's balance.
	recordPayout(t, env, "demo-venue", 4200, "2026-07-02", "not ours")

	summary := getPayouts(t, env, sessionID)
	if summary.WithdrawableBalanceCents != feeTestBaseCents {
		t.Fatalf("balance = %d; want only this org's %d", summary.WithdrawableBalanceCents, feeTestBaseCents)
	}
	if len(summary.Payouts) != 0 {
		t.Fatalf("payout history = %+v; want none of the other org's", summary.Payouts)
	}
}

// TestPayoutsForbiddenForNonOrgAdmin: the Organization's finances are an Org
// Admin's business, not hired staff's.
func TestPayoutsForbiddenForNonOrgAdmin(t *testing.T) {
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
	resp, body = env.get(t, "/api/v1/staff/organization/payouts", authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %+v", body.Error)
	}
}

// importSaleForBalance records a cash Direct Sale — a Sales Channel the platform
// never collected money on.
func importSaleForBalance(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "cash-batch",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "cash@example.com", "customer_first_name": "Cash", "customer_last_name": "Buyer",
				"ticket_type_id": ticketTypeID, "quantity": 5, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status=%d error=%+v", resp.StatusCode, body.Error)
	}
}
