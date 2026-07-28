package integration

import (
	"testing"
)

// Platform revenue keeps the fee the platform kept (issue #127, parent #123).
//
// Every money aggregate on this platform filters on active sales, and an
// Operator Reversal is the one deliberate exception — but only on the
// platform's side of the ledger. When the operator states that the platform
// kept its commission on the refunded sale, the Platform Fee and its Fee IVA
// stay in platform revenue: the buyer got back the ticket money, the platform's
// service was still rendered, and a figure that dropped anyway would be a lie
// about money the platform still holds.
//
// The Organization's side never moves with that flag. Its Net Proceeds, its
// Withdrawable Balance and its Event earnings fall by the reversed sale's own
// contribution and by exactly that, whichever way the fee decision went — the
// status flip already subtracts them, and `platform_fee_kept` is not a fact
// about the Organization. Both tests below therefore assert the identical
// org-side movement, so the flag can never grow a second meaning.

// keptFeeFixture stages the shape both tests read: one Online Sale that
// survives and one the operator reverses, at the fee test's price. It returns
// the Event and the surviving/reversed references, plus the reading taken
// before the marking.
func keptFeeFixture(t *testing.T, env *testEnv) (staffSession, operatorSessionID, eventID, reversedRef string, before moneyReading) {
	t.Helper()

	staffSession = orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, staffSession, "Kept Fee Fest", "kept-fee-fest", feeTestBaseCents, 10)
	buyOnline(t, env, "kept-fee-fest", gaID, "ana@example.com")
	reversedRef = buyOnline(t, env, "kept-fee-fest", gaID, "bea@example.com")

	operatorSessionID = operatorSession(t, env, "operator@example.com")
	before = moneySurfaces(t, env, staffSession, operatorSessionID, eventID)

	// Two sales in, both counting everywhere, and nothing kept yet: the
	// disclosure term is zero until an Operator Reversal creates one.
	if before.platformFeeCents != 2*feeTestFeeCents || before.feeIVACents != 2*feeTestIVACents {
		t.Fatalf("platform revenue before = fee %d, iva %d; want %d and %d",
			before.platformFeeCents, before.feeIVACents, 2*feeTestFeeCents, 2*feeTestIVACents)
	}
	if before.keptFeeCents != 0 || before.keptFeeIVACents != 0 {
		t.Fatalf("kept-fee term before = fee %d, iva %d; want zero before any Operator Reversal",
			before.keptFeeCents, before.keptFeeIVACents)
	}
	if before.balanceCents != 2*feeTestBaseCents || before.summary.NetProceedsCents != 2*feeTestBaseCents {
		t.Fatalf("org money before = balance %d, net proceeds %d; want %d each",
			before.balanceCents, before.summary.NetProceedsCents, 2*feeTestBaseCents)
	}
	return staffSession, operatorSessionID, eventID, reversedRef, before
}

// assertOrgSideFell pins the Organization's half of the ledger: it loses the
// reversed sale's Net Proceeds and nothing more, and it does so identically
// whichever way the operator decided the fee.
func assertOrgSideFell(t *testing.T, before, after moneyReading) {
	t.Helper()
	if want := before.balanceCents - feeTestBaseCents; after.balanceCents != want {
		t.Fatalf("withdrawable balance after = %d (before %d), want %d — the reversed sale's Net Proceeds and nothing else",
			after.balanceCents, before.balanceCents, want)
	}
	if after.summary.NetProceedsCents != feeTestBaseCents || after.summary.SalesCount != 1 {
		t.Fatalf("event summary after = %+v, want the one surviving sale's %d",
			after.summary, feeTestBaseCents)
	}
}

// TestKeptFeeStaysInPlatformRevenue: the operator states the platform kept its
// commission, so platform revenue does not move at all — and the summary says
// how much of it is now fees kept on reversed sales, so the standing figure
// stays explainable.
func TestKeptFeeStaysInPlatformRevenue(t *testing.T) {
	env := setupTest(t)
	staffSession, operatorSessionID, eventID, reversedRef, before := keptFeeFixture(t, env)

	operatorReverseOK(t, env, operatorSessionID, reversedRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(true),
		Note:                strPtr("refunded by bank transfer, commission kept"),
	})

	after := moneySurfaces(t, env, staffSession, operatorSessionID, eventID)
	if after.platformFeeCents != before.platformFeeCents || after.feeIVACents != before.feeIVACents {
		t.Fatalf("platform revenue after = fee %d, iva %d; want it standing at %d and %d — the platform kept this sale's commission",
			after.platformFeeCents, after.feeIVACents, before.platformFeeCents, before.feeIVACents)
	}
	if after.keptFeeCents != feeTestFeeCents || after.keptFeeIVACents != feeTestIVACents {
		t.Fatalf("kept-fee term after = fee %d, iva %d; want the reversed sale's %d and %d, so the dashboard can disclose the inclusion",
			after.keptFeeCents, after.keptFeeIVACents, feeTestFeeCents, feeTestIVACents)
	}

	assertOrgSideFell(t, before, after)
}

// TestReturnedFeeLeavesPlatformRevenue: the operator states the platform gave
// its commission back, so platform revenue falls by that sale's fee plus Fee
// IVA — indistinguishable, on this surface, from the Customer's own undo — and
// the kept-fee term stays zero, so the dashboard says nothing.
func TestReturnedFeeLeavesPlatformRevenue(t *testing.T) {
	env := setupTest(t)
	staffSession, operatorSessionID, eventID, reversedRef, before := keptFeeFixture(t, env)

	operatorReverseOK(t, env, operatorSessionID, reversedRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(false),
		Note:                strPtr("goodwill: commission returned too"),
	})

	after := moneySurfaces(t, env, staffSession, operatorSessionID, eventID)
	if after.platformFeeCents != feeTestFeeCents || after.feeIVACents != feeTestIVACents {
		t.Fatalf("platform revenue after = fee %d, iva %d; want only the surviving sale's %d and %d",
			after.platformFeeCents, after.feeIVACents, feeTestFeeCents, feeTestIVACents)
	}
	if after.keptFeeCents != 0 || after.keptFeeIVACents != 0 {
		t.Fatalf("kept-fee term after = fee %d, iva %d; want zero — nothing was kept, so there is nothing to disclose",
			after.keptFeeCents, after.keptFeeIVACents)
	}

	assertOrgSideFell(t, before, after)
}
