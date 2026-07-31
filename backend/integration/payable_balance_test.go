package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Payable Balance: the part of the Withdrawable Balance an Organization may
// ask for today (#174, ADR 0025).
//
// It is the same arithmetic over a subset of the sales — those recorded before
// today in Ecuador, with no Reversal Request still open on them — so every test
// here reads BOTH figures and asserts the relation between them, never the
// smaller number alone. Cleared sales are a subset of all sales, so Payable ≤
// Withdrawable always, including when both are negative.
//
// The day boundary is the reason this file moves the clock rather than inserting
// rows with contrived timestamps. The rule is "before today in Ecuador", and the
// only honest way to test a rule about today is to change what today is. That
// works only because the boundary is computed in Go from the sales service's
// injected clock and passed into the query: SQL NOW() would ignore the harness's
// fixed clock entirely and every assertion below would pass vacuously against
// the wall clock of whoever ran the suite.

// salesClockAt moves the shared app's sales clock, which is the clock the
// Payable Balance's day boundary is derived from.
//
// It is deliberately not holdClocksAt: the catalog clock has nothing to do with
// this rule, and moving it too would let a failure here be a Capacity Hold
// somewhere else. setupTest resets both, so nothing needs cleaning up.
func salesClockAt(at time.Time) {
	sharedApp.SalesService.WithClock(func() time.Time { return at })
}

// ecuadorMidnight is 00:00 Ecuador time, `days` calendar days from the Ecuadorian
// date the harness's fixed clock falls on. Day 0 is the day every sale in this
// file is recorded on; day 1 is the first instant those sales have cleared.
//
// It is built in Ecuador's own zone rather than as an offset from fixedClock
// because that is how the rule is stated. Writing "12:00 UTC plus seventeen
// hours" would re-derive in the test the arithmetic the test exists to check.
func ecuadorMidnight(t *testing.T, days int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(platform.EcuadorTimeZone)
	if err != nil {
		t.Fatalf("load %s: %v", platform.EcuadorTimeZone, err)
	}
	day := fixedClock.In(loc)
	return time.Date(day.Year(), day.Month(), day.Day()+days, 0, 0, 0, 0, loc)
}

// ecuadorTimeOfDay is a wall-clock time on the Ecuadorian date the fixed clock
// falls on — the date every sale in this file is bought on.
func ecuadorTimeOfDay(t *testing.T, hour, minute int) time.Time {
	t.Helper()
	return ecuadorMidnight(t, 0).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

// assertBalances reads the organizer's payouts summary and asserts both figures,
// plus the invariant that binds them.
func assertBalances(t *testing.T, env *testEnv, sessionID string, wantWithdrawable, wantPayable int, when string) payoutsSummary {
	t.Helper()
	summary := getPayouts(t, env, sessionID)
	if summary.WithdrawableBalanceCents != wantWithdrawable || summary.PayableBalanceCents != wantPayable {
		t.Fatalf("%s: withdrawable=%d payable=%d; want withdrawable=%d payable=%d",
			when, summary.WithdrawableBalanceCents, summary.PayableBalanceCents, wantWithdrawable, wantPayable)
	}
	if summary.PayableBalanceCents > summary.WithdrawableBalanceCents {
		t.Fatalf("%s: payable %d exceeds withdrawable %d — cleared sales are a subset of all sales, so this cannot happen",
			when, summary.PayableBalanceCents, summary.WithdrawableBalanceCents)
	}
	return summary
}

// TestPayableBalanceWaitsForTheEcuadorDayToTurn is the heart of the feature: a
// sale recorded today counts toward what the platform owes and toward nothing
// the Organization may ask for, and the two figures converge the instant the
// Ecuadorian day turns.
//
// The 20:30 step is the load-bearing one. By then the Reversal Window has shut —
// it closes at 20:00 Ecuador time on the date of purchase at the very latest
// (ADR 0018) — and the Payable Balance is still zero, because the rule is one
// condition and not two: the settlement lag SUBSUMES the window rather than
// sitting beside it (ADR 0025). An implementation that checked the window would
// pass every other assertion in this file and fail here.
func TestPayableBalanceWaitsForTheEcuadorDayToTurn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// Nothing sold: both figures are zero, and neither is an error.
	assertBalances(t, env, sessionID, 0, 0, "an Organization that has sold nothing")

	_, gaID := publishCheckoutEvent(t, env, sessionID, "Cleared Fest", "cleared-fest", feeTestBaseCents, 10)
	begin := beginCheckoutOK(t, env, "test-org", "cleared-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	netProceeds := 2 * feeTestBaseCents

	// A free Online Sale, which had no Payment Provider and no approved payment
	// to join to (ADR 0017). It contributes nothing either way — a free ticket
	// leaves no Net Proceeds behind — so what it fences is structural: the
	// Payable Balance is keyed on the Ticket Sale's own created_at and joins
	// `payments` nowhere, and a future join would drop this row rather than
	// counting it as zero.
	_, freeID := publishCheckoutEvent(t, env, sessionID, "Free Fest", "free-fest", 0, 10)
	approvedRef(t, beginCheckoutSettled(t, env, "test-org", "free-fest", "",
		checkoutBody("bea@example.com", "Bea", "Ruiz", cartLine(freeID, 2))))

	// 07:00 in Ecuador, the moment of purchase. The platform owes the money and
	// the Organization cannot ask for it yet.
	assertBalances(t, env, sessionID, netProceeds, 0, "on the morning of the sale")

	// 20:30 in Ecuador. The buyer can no longer undo the sale themselves, and
	// that is deliberately not enough.
	salesClockAt(ecuadorTimeOfDay(t, 20, 30))
	assertBalances(t, env, sessionID, netProceeds, 0, "after the Reversal Window shut, same day")

	// One second before Ecuadorian midnight: still today, still not payable.
	salesClockAt(ecuadorMidnight(t, 1).Add(-time.Second))
	assertBalances(t, env, sessionID, netProceeds, 0, "a second before the day turns")

	// Midnight itself. "Before today" is a half-open boundary: a sale recorded
	// at 07:00 yesterday is strictly before the start of today, so it clears the
	// instant the date changes.
	salesClockAt(ecuadorMidnight(t, 1))
	assertBalances(t, env, sessionID, netProceeds, netProceeds, "the instant the Ecuadorian day turned")
}

// TestPayableBalanceExcludesASaleWithALiveReversalRequest: a Ticket Sale whose
// buyer has asked to undo it, and whose Payment Provider has not yet answered,
// is money nobody can say is the Organization's — so it stays out of the Payable
// Balance until the question is closed.
//
// The Withdrawable Balance is unmoved throughout, and that is the point of
// reading both. A Reversal Request leaves the sale `active` by design, because
// no money is known to have moved (ADR 0024), so the sale keeps counting in
// everything it counted in before. The Payable Balance is the one figure that
// steps back from it.
//
// The request here is a real one, pressed through the real endpoint against the
// fake PayPhone that says nothing, rather than a row this test invented. Both
// sales are day-cleared before anything is asserted, so the only thing separating
// them is the open request.
func TestPayableBalanceExcludesASaleWithALiveReversalRequest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Open Question Fest", "open-question-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	// The untouched sale, and the one whose buyer pressed Undo into silence.
	settled, _ := buyOnlineThroughPayPhone(t, "open-question-fest", gaID, "ana@example.com", 2)
	payphoneStub.reverseHangsUp()
	stuck, _, _ := stuckReversal(t, "open-question-fest", gaID, "bea@example.com", 3)
	if request := theOnlyReversalRequest(t, env, stuck); request.status != "in_flight" {
		t.Fatalf("Reversal Request = %+v, want in_flight — this test needs a genuinely open ask", request)
	}
	settledNet := 2 * feeTestBaseCents
	stuckNet := 3 * feeTestBaseCents

	// Both sales are from yesterday now, so the day rule lets both through and
	// the open request is the only thing left to exclude one of them.
	salesClockAt(ecuadorMidnight(t, 1))
	assertBalances(t, env, sessionID, settledNet+stuckNet, settledNet, "while the reversal is still in flight")
	if settled == stuck {
		t.Fatal("the two fixtures share a Sale Confirmation reference")
	}

	// PayPhone comes back and refuses: it considered the reversal and said no,
	// so nothing happened and the sale was never in doubt. The money is the
	// Organization's to ask for again.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
	if result := drainReversals(t, payphoneEnv); result.Refused != 1 {
		t.Fatalf("drain = %+v, want the one stuck request refused", result)
	}
	if request := theOnlyReversalRequest(t, env, stuck); request.status != "refused" {
		t.Fatalf("Reversal Request = %+v, want refused", request)
	}

	salesClockAt(ecuadorMidnight(t, 1))
	assertBalances(t, env, sessionID, settledNet+stuckNet, settledNet+stuckNet, "once the reversal was refused")
}

// TestPayableBalanceGoesNegativeForASettledOrganizationThatSoldToday is the
// case ADR 0025 says is correct rather than a bug, and the reason both figures
// are signed and unclamped.
//
// The Organization is settled in full against its Withdrawable Balance — the
// figure that counts today's sales, which is exactly the hazard the Payable
// Balance exists to stop — and then sells again the same day. Every Payout is
// subtracted from both figures in full and unchanged, so what is left is a
// positive Withdrawable Balance beside a negative Payable one. The Organization
// is owed money and may ask for none of it, which is the truth.
func TestPayableBalanceGoesNegativeForASettledOrganizationThatSoldToday(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Settled Fest", "settled-fest", feeTestBaseCents, 20)

	buy := func(email, first, last string, quantity int) {
		t.Helper()
		begin := beginCheckoutOK(t, env, "test-org", "settled-fest",
			checkoutBody(email, first, last, map[string]any{"ticket_type_id": gaID, "quantity": quantity}))
		confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	}

	buy("ana@example.com", "Ana", "Lopez", 2)
	firstNet := 2 * feeTestBaseCents
	assertBalances(t, env, sessionID, firstNet, 0, "after the first sale of the day")

	// The operator settles the whole Withdrawable Balance, today, against money
	// that has not cleared. Recording a Payout is never refused: it is a record
	// of money that already moved (ADR 0015).
	recordPayout(t, env, "test-org", firstNet, "2026-07-07", "paid against today's sales")
	assertBalances(t, env, sessionID, 0, -firstNet, "immediately after being settled in full")

	// And then sells again the same day.
	buy("bea@example.com", "Bea", "Ruiz", 3)
	secondNet := 3 * feeTestBaseCents
	assertBalances(t, env, sessionID, secondNet, -firstNet, "after selling again on the day of settlement")

	// The day turns and both sales clear, which is the only thing that ever
	// repairs it: the Payout is never adjusted, and the two figures converge.
	salesClockAt(ecuadorMidnight(t, 1))
	assertBalances(t, env, sessionID, secondNet, secondNet, "once both sales cleared")
}

// TestPayableBalanceOnTheOperatorOrganizationDetail: the operator answering a
// request sees the same two figures the Organization asking sees, side by side
// on the Organization they are about to pay.
func TestPayableBalanceOnTheOperatorOrganizationDetail(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")

	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "Operator Fest", "operator-fest", feeTestBaseCents, 10)
	begin := beginCheckoutOK(t, env, "test-org", "operator-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	netProceeds := 2 * feeTestBaseCents

	operatorSessionID := operatorSession(t, env, "operator@example.com")

	var today operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &today)
	if today.WithdrawableBalanceCents != netProceeds || today.PayableBalanceCents != 0 {
		t.Fatalf("operator detail on the day of the sale = withdrawable %d, payable %d; want %d and 0",
			today.WithdrawableBalanceCents, today.PayableBalanceCents, netProceeds)
	}

	salesClockAt(ecuadorMidnight(t, 1))
	var tomorrow operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &tomorrow)
	if tomorrow.WithdrawableBalanceCents != netProceeds || tomorrow.PayableBalanceCents != netProceeds {
		t.Fatalf("operator detail once the day turned = withdrawable %d, payable %d; want both %d",
			tomorrow.WithdrawableBalanceCents, tomorrow.PayableBalanceCents, netProceeds)
	}
}
