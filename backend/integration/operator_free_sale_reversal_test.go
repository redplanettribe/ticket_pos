package integration

import (
	"net/http"
	"testing"
	"time"
)

// The Operator Reversal of a FREE Online Sale (issue #126, parent #123).
//
// #125 built the marking for a sale that collected money, and required the
// operator to state what the buyer got back. A free Online Sale collected
// nothing, so there is nothing for anybody to have refunded — and under #125's
// rules that made the cheaper sale the harder one to undo: every amount an
// operator could state exceeded the zero the sale collected, and stating none
// was refused outright.
//
// So the money memo becomes a fact about the SALE rather than about the
// request. A free sale takes neither money field and refuses both; a paid sale
// takes both and refuses neither. Absent is not zero: "nothing to refund" is
// NULL in the record, and no zero is ever written.
//
// Everything else is #125 unchanged — `operator` provenance with the acting
// operator's email, the optional note, capacity restored, the Sale Voided
// email, irreversibility — and the provider is not called here either, which
// costs nothing to assert and is the promise the whole feature rests on.

// claimFreeOnlineSale stages the fixture this file is about: a free Online Sale
// bought through the app WIRED TO PAYPHONE, so every assertion below that the
// provider was left alone is made against an app that could in fact have called
// it. The claim itself never contacts a provider (ADR 0017); the reversal must
// not either.
func claimFreeOnlineSale(t *testing.T, env *testEnv, sessionID, name, slug, email string, quantity int) string {
	t.Helper()
	_, freeID := publishFreeEvent(t, env, sessionID, name, slug, 10)
	return claimFree(t, payphoneEnv, slug, freeID, email, quantity)
}

// TestOperatorReversesAFreeOnlineSaleWithNoMoneyFields is the tracer bullet: the
// marking a free sale accepts is the one with no money in it at all.
//
// It asserts the same shape #125's tracer does — provenance, capacity, the void
// notice, the operator's own lookup — plus the one fact that belongs only here:
// the two money columns are NULL in the database, not zero.
func TestOperatorReversesAFreeOnlineSaleWithNoMoneyFields(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	ref := claimFreeOnlineSale(t, env, sessionID, "Free Fest", "operator-free-fest", "ana@example.com", 3)
	if got := remaining(t, env, "operator-free-fest", "GA"); got != 7 {
		t.Fatalf("remaining after claiming 3 of 10 = %d, want 7", got)
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	result := operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		Note: strPtr("the buyer never paid anything; nothing to send back"),
	})

	// The promise the feature rests on, asserted here too: no money moved, so
	// there was never anything to ask a provider about.
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times on a FREE sale; no provider is called on this path at all", got)
	}

	if result.Status != "reversed" || result.ConfirmationRef != ref {
		t.Fatalf("result = %+v, want the sale reversed under its own reference %q", result, ref)
	}
	if result.ReversedBy != "operator" {
		t.Fatalf("reversed_by = %q, want 'operator' — the same third actor a paid marking records", result.ReversedBy)
	}
	if result.OperatorReversal.Operator != "operator@example.com" {
		t.Fatalf("operator = %q, want the acting operator's own session email", result.OperatorReversal.Operator)
	}
	if result.OperatorReversal.RefundedAmountCents != nil || result.OperatorReversal.PlatformFeeKept != nil {
		t.Fatalf("money memo = %+v, want both money facts ABSENT on a free sale", result.OperatorReversal)
	}
	if result.OperatorReversal.Note == nil || *result.OperatorReversal.Note != "the buyer never paid anything; nothing to send back" {
		t.Fatalf("note = %+v, want the operator's own words — the note is not money and is kept", result.OperatorReversal.Note)
	}

	// The record itself: the operator is named, the note is kept, and the two
	// money columns are NULL. "Nothing to refund" and "zero refunded" are
	// different answers, and only the first one is true here.
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" || !reversedAt.Valid || !reversedAt.Time.Equal(env.fixedClock) {
		t.Fatalf("stored status/reversed_at = %q/%+v, want reversed at %v", status, reversedAt, env.fixedClock)
	}
	if !reversedBy.Valid || reversedBy.String != "operator" {
		t.Fatalf("stored reversed_by = %+v, want 'operator'", reversedBy)
	}
	operatorEmail, note, refunded, feeKept := operatorMemo(t, env, ref)
	if !operatorEmail.Valid || operatorEmail.String != "operator@example.com" {
		t.Fatalf("stored operator = %+v, want operator@example.com — a marking is never anonymous, money or no money", operatorEmail)
	}
	if !note.Valid || note.String != "the buyer never paid anything; nothing to send back" {
		t.Fatalf("stored note = %+v", note)
	}
	if refunded.Valid || feeKept.Valid {
		t.Fatalf("stored money memo = %+v / %+v, want both NULL; a zero here would claim a refund of nothing happened", refunded, feeKept)
	}

	// Capacity comes back for everybody who did not get a ticket — the reason a
	// free sale needed an undo path of its own at all.
	if ga := publicTicketTypes(t, env, testOrgSlug, "operator-free-fest")["GA"]; ga.Remaining != 10 || ga.SoldOut {
		t.Fatalf("public GA = %+v after the marking, want all 10 back on sale", ga)
	}

	// The buyer is told by the same Sale Voided email every reversal sends.
	voided := env.email.Voided()
	if len(voided) != 1 || voided[0].To != "ana@example.com" || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one addressed to the buyer quoting %q", voided, ref)
	}

	// And the operator's own lookup carries the memo with its money absent, so
	// the record reads the same way it was written.
	looked := lookUpSale(t, env, operatorSessionID, ref).Sale
	if looked.Status != "reversed" || looked.ReversedBy == nil || *looked.ReversedBy != "operator" {
		t.Fatalf("looked-up sale = %+v, want reversed by the operator", looked)
	}
	if looked.AmountCents != 0 {
		t.Fatalf("looked-up amount = %d, want a sale that collected nothing; the fixture is not free", looked.AmountCents)
	}
	if looked.OperatorReversal == nil || looked.OperatorReversal.Operator != "operator@example.com" ||
		looked.OperatorReversal.RefundedAmountCents != nil || looked.OperatorReversal.PlatformFeeKept != nil {
		t.Fatalf("looked-up money memo = %+v, want the operator named and no money", looked.OperatorReversal)
	}
}

// TestOperatorReversalOfAFreeSaleRefusesMoneyFields: on a sale that collected
// nothing, every money fact is a claim about money that never existed, so all of
// them are refused — including a zero, which is the one an unthinking client
// would send.
//
// The refusals split by which rule they break. Stating BOTH facts is a
// well-formed marking that is wrong about this sale, so it is refused where the
// sale is known, by NOTHING_TO_REFUND. Stating ONE is malformed whatever the
// sale is — the amount and the fee decision travel together on every sale — so
// it is refused on shape, and the operator's fix is to drop the other one too.
func TestOperatorReversalOfAFreeSaleRefusesMoneyFields(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	ref := claimFreeOnlineSale(t, env, sessionID, "Strict Free Fest", "operator-strict-free-fest", "ana@example.com", 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	cases := []struct {
		name string
		body operatorReversalBody
		code string
	}{
		{
			name: "a refund of money that was never collected",
			body: operatorReversalBody{RefundedAmountCents: intPtr(500), PlatformFeeKept: boolPtr(false)},
			code: "NOTHING_TO_REFUND",
		},
		{
			name: "zero refunded, which is a claim and not an absence",
			body: operatorReversalBody{RefundedAmountCents: intPtr(0), PlatformFeeKept: boolPtr(false)},
			code: "VALIDATION_FAILED",
		},
		{
			name: "a fee decision about a fee that was never charged",
			body: operatorReversalBody{PlatformFeeKept: boolPtr(true)},
			code: "VALIDATION_FAILED",
		},
		{
			name: "an amount with no fee decision beside it",
			body: operatorReversalBody{RefundedAmountCents: intPtr(500)},
			code: "VALIDATION_FAILED",
		},
	}
	for _, tc := range cases {
		resp, body := operatorReverseRequest(t, env, operatorSessionID, ref, tc.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d error=%+v, want 400 %s", tc.name, resp.StatusCode, body.Error, tc.code)
		}
		if body.Error == nil || body.Error.Code != tc.code {
			t.Fatalf("%s: error=%+v, want code %s", tc.name, body.Error, tc.code)
		}
	}

	assertNothingChanged(t, env, ref, "operator-strict-free-fest", "GA", 9)

	// And the same sale is marked once the money is left out, so the refusals
	// above are about the money rather than about the sale.
	if got := operatorReverseOK(t, env, operatorSessionID, ref, operatorReversalBody{}); got.Status != "reversed" {
		t.Fatalf("marking with no money at all = %+v, want it accepted", got)
	}
}

// TestOperatorReversalOfAPaidSaleStillRequiresTheMoneyFacts is the other half of
// the same rule, and the regression guard on #125: making the money optional for
// a free sale must not make it optional for a paid one. A sale that collected
// money is never marked without saying what came back.
func TestOperatorReversalOfAPaidSaleStillRequiresTheMoneyFacts(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Paid Fest", "operator-paid-strict-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "operator-paid-strict-fest", gaID, "ana@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	resp, body := operatorReverseRequest(t, env, operatorSessionID, ref, operatorReversalBody{
		Note: strPtr("a marking that says nothing about the money"),
	})
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "REFUNDED_AMOUNT_REQUIRED" {
		t.Fatalf("marking a paid sale with no money facts: status=%d error=%+v, want 400 REFUNDED_AMOUNT_REQUIRED",
			resp.StatusCode, body.Error)
	}

	assertNothingChanged(t, env, ref, "operator-paid-strict-fest", "GA", 9)
}
