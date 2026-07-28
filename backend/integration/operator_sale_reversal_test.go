package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Operator Reversal of a paid Online Sale (issue #125, parent #123): a
// Platform Operator refunds a buyer off-platform — by hand in the PayPhone
// dashboard, or by bank transfer — and then records here that it happened.
//
// It is a pure record, and that is the whole of its design. The Payment
// Provider is NEVER called: the money already moved, the operator asserts it,
// and the system believes them exactly as it believes a recorded Payout. Every
// test below that reverses anything asserts the PayPhone stub was asked to
// reverse nothing, because a feature that quietly called the provider would
// pass every other assertion in this file while refunding somebody twice.
//
// The second defining property is that the Reversal Window is irrelevant. Being
// past it is the reason the feature exists, and being inside it never blocks the
// marking either — no window check exists on this path at all.

// operatorReversalBody is the marking as an operator sends it: what the buyer
// actually got back, whether the platform kept its fee, and an optional note for
// whoever reconciles later. Every field is a pointer so a test can omit one and
// see the refusal, which is the point of "required, no default".
type operatorReversalBody struct {
	RefundedAmountCents *int    `json:"refunded_amount_cents,omitempty"`
	PlatformFeeKept     *bool   `json:"platform_fee_kept,omitempty"`
	Note                *string `json:"note,omitempty"`
}

// operatorReversalMemo is the money memo an Operator Reversal leaves on the
// sale: who asserted it, what the buyer got back, whether the platform kept its
// fee, and the note. Operator-facing only — the Organization sees the sale as
// reversed and nothing of this.
type operatorReversalMemo struct {
	Operator            string  `json:"operator"`
	RefundedAmountCents *int    `json:"refunded_amount_cents"`
	PlatformFeeKept     *bool   `json:"platform_fee_kept"`
	Note                *string `json:"note"`
}

// operatorReversalResult is the marking's success body.
type operatorReversalResult struct {
	TicketSaleID     string               `json:"ticket_sale_id"`
	ConfirmationRef  string               `json:"confirmation_ref"`
	Status           string               `json:"status"`
	ReversedAt       string               `json:"reversed_at"`
	ReversedBy       string               `json:"reversed_by"`
	OperatorReversal operatorReversalMemo `json:"operator_reversal"`
}

func operatorReversalPath(confirmationRef string) string {
	return "/api/v1/operator/sales/" + confirmationRef + "/reverse"
}

func operatorReverseRequest(t *testing.T, env *testEnv, sessionID, confirmationRef string, body operatorReversalBody) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if sessionID != "" {
		headers = authHeader(sessionID)
	}
	return env.post(t, operatorReversalPath(confirmationRef), body, headers)
}

func operatorReverseOK(t *testing.T, env *testEnv, sessionID, confirmationRef string, body operatorReversalBody) operatorReversalResult {
	t.Helper()
	resp, envBody := operatorReverseRequest(t, env, sessionID, confirmationRef, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operator reversal status=%d error=%+v, want 200", resp.StatusCode, envBody.Error)
	}
	var out operatorReversalResult
	if err := json.Unmarshal(envBody.Data, &out); err != nil {
		t.Fatalf("decode operator reversal result: %v", err)
	}
	return out
}

// paidRefund is the whole of what a buyer of `quantity` tickets paid at the fee
// test's price — the most the operator may say they refunded.
func paidRefund(quantity int) int {
	return quantity * feeTestAllInCents
}

func intPtr(v int) *int       { return &v }
func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }

// paymentStatuses returns the statuses of every Payment behind a Ticket Sale.
// The Payment stays `approved` through a reversal — the checkout genuinely did
// settle, and the reversal is a later event on the Sale (ADR 0018) — and SQL is
// the only place that fact is visible.
func paymentStatuses(t *testing.T, env *testEnv, ref string) []string {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT p.status FROM payments p
		JOIN ticket_sales ts ON ts.id = p.ticket_sale_id
		WHERE ts.confirmation_ref = $1
	`, ref)
	if err != nil {
		t.Fatalf("read payment statuses for %q: %v", ref, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan payment status: %v", err)
		}
		out = append(out, status)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read payment statuses: %v", err)
	}
	return out
}

// operatorMemo reads the money memo an Operator Reversal recorded, straight from
// the sale. The Organization's own surfaces never show these columns, so the
// database is where the record is checked; what the operator sees of it is
// asserted through the lookup endpoint instead.
func operatorMemo(t *testing.T, env *testEnv, ref string) (operator, note sql.NullString, refunded sql.NullInt64, feeKept sql.NullBool) {
	t.Helper()
	if err := env.db.QueryRow(`
		SELECT reversed_by_operator, reversal_note, refunded_amount_cents, platform_fee_kept
		FROM ticket_sales WHERE confirmation_ref = $1
	`, ref).Scan(&operator, &note, &refunded, &feeKept); err != nil {
		t.Fatalf("read operator reversal memo for %q: %v", ref, err)
	}
	return operator, note, refunded, feeKept
}

// TestOperatorReversesAPaidOnlineSaleWithoutCallingPayPhone is the tracer
// bullet, and it runs against the app wired to the fake PayPhone server so the
// central promise can be asserted rather than assumed: the provider was not
// asked to reverse anything.
//
// Everything else a reversal has always done still happens — the sale is voided
// with its provenance, capacity returns to the Ticket Type, the buyer gets the
// existing Sale Voided email, the Organization's Withdrawable Balance drops by
// the Net Proceeds — and the money memo the operator asserted is recorded
// alongside it.
func TestOperatorReversesAPaidOnlineSaleWithoutCallingPayPhone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishEventStarting(t, env, sessionID, "Operator Fest", "operator-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	ref, _ := buyOnlineThroughPayPhone(t, "operator-fest", gaID, "ana@example.com", 2)
	if got := remaining(t, env, "operator-fest", "GA"); got != 8 {
		t.Fatalf("remaining after buying 2 of 10 = %d, want 8", got)
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	before := moneySurfaces(t, env, sessionID, operatorSessionID, eventID)
	if before.balanceCents != 2*feeTestBaseCents {
		t.Fatalf("withdrawable balance before = %d, want the sale's Net Proceeds %d",
			before.balanceCents, 2*feeTestBaseCents)
	}
	env.email.Reset()

	result := operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)),
		PlatformFeeKept:     boolPtr(true),
		Note:                strPtr("refunded via the PayPhone dashboard"),
	})

	// THE assertion of the feature: the money already moved off-platform, so the
	// Payment Provider must never be asked to move it again.
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times; an Operator Reversal is a pure record and must never call the provider", got)
	}

	if result.Status != "reversed" || result.ConfirmationRef != ref || result.TicketSaleID == "" {
		t.Fatalf("result = %+v, want the sale reversed under its own reference %q", result, ref)
	}
	if result.ReversedBy != "operator" {
		t.Fatalf("reversed_by = %q, want 'operator' — the third reversal actor", result.ReversedBy)
	}
	if result.ReversedAt != env.fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("reversed_at = %q, want the server's own clock %q",
			result.ReversedAt, env.fixedClock.UTC().Format(time.RFC3339))
	}
	if result.OperatorReversal.Operator != "operator@example.com" {
		t.Fatalf("operator = %q, want the acting operator's own session email — a money assertion is never anonymous",
			result.OperatorReversal.Operator)
	}
	if result.OperatorReversal.RefundedAmountCents == nil || *result.OperatorReversal.RefundedAmountCents != paidRefund(2) ||
		result.OperatorReversal.PlatformFeeKept == nil || !*result.OperatorReversal.PlatformFeeKept ||
		result.OperatorReversal.Note == nil || *result.OperatorReversal.Note != "refunded via the PayPhone dashboard" {
		t.Fatalf("money memo = %+v, want the amount, the kept fee and the note as stated", result.OperatorReversal)
	}

	// The provenance on the sale itself: the third actor, the acting operator's
	// identity, the moment, and the memo.
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" || !reversedAt.Valid || !reversedAt.Time.Equal(env.fixedClock) {
		t.Fatalf("stored status/reversed_at = %q/%+v, want reversed at %v", status, reversedAt, env.fixedClock)
	}
	if !reversedBy.Valid || reversedBy.String != "operator" {
		t.Fatalf("stored reversed_by = %+v, want 'operator'", reversedBy)
	}
	operatorEmail, note, refunded, feeKept := operatorMemo(t, env, ref)
	if !operatorEmail.Valid || operatorEmail.String != "operator@example.com" {
		t.Fatalf("stored operator = %+v, want operator@example.com", operatorEmail)
	}
	if !note.Valid || note.String != "refunded via the PayPhone dashboard" {
		t.Fatalf("stored note = %+v", note)
	}
	if !refunded.Valid || int(refunded.Int64) != paidRefund(2) || !feeKept.Valid || !feeKept.Bool {
		t.Fatalf("stored money memo = %+v / %+v, want %d refunded with the fee kept", refunded, feeKept, paidRefund(2))
	}

	// The Payment stays approved: the checkout genuinely settled, and the
	// reversal is a later event on the Sale rather than a retroactive edit.
	if got := paymentStatuses(t, env, ref); len(got) != 1 || got[0] != "approved" {
		t.Fatalf("payment statuses = %v, want exactly one approved Payment", got)
	}

	// Capacity comes back for everybody who did not get a ticket.
	if ga := publicTicketTypes(t, env, testOrgSlug, "operator-fest")["GA"]; ga.Remaining != 10 || ga.SoldOut {
		t.Fatalf("public GA = %+v after the marking, want all 10 back on sale", ga)
	}

	// The buyer gets the existing Sale Voided email, quoting their reference —
	// the system tells them, not just the bank transfer landing.
	voided := env.email.Voided()
	if len(voided) != 1 || voided[0].To != "ana@example.com" || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one addressed to the buyer quoting %q", voided, ref)
	}

	// The Organization's money moves by the status flip alone: its Withdrawable
	// Balance drops by this sale's Net Proceeds.
	after := moneySurfaces(t, env, sessionID, operatorSessionID, eventID)
	if after.balanceCents != before.balanceCents-2*feeTestBaseCents {
		t.Fatalf("withdrawable balance after = %d (before %d), want it lower by the reversed sale's Net Proceeds %d",
			after.balanceCents, before.balanceCents, 2*feeTestBaseCents)
	}
	if after.summary.SalesCount != 0 || after.summary.NetProceedsCents != 0 {
		t.Fatalf("event summary after = %+v, want the reversed sale to stop counting", after.summary)
	}

	// The Event's staff see a sale left their totals, behind the same status
	// filter as any other Sale Reversal.
	list := listSalesOK(t, env, sessionID, eventID, "")
	if list.ReversedCount != 1 {
		t.Fatalf("reversed_count = %d after the marking, want 1", list.ReversedCount)
	}
	if list.Pagination.Total != 0 {
		t.Fatalf("active sales = %d after the marking, want none", list.Pagination.Total)
	}

	// They see WHO reversed it — the platform, not the buyer and not their own
	// import undo — and none of the money memo, which is operator-facing only.
	resp, listBody := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status=reversed", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reversed sales list status=%d error=%+v", resp.StatusCode, listBody.Error)
	}
	reversedRows := salesList(t, listBody)
	if len(reversedRows.Data) != 1 || reversedRows.Data[0].ReversedBy == nil || *reversedRows.Data[0].ReversedBy != "operator" {
		t.Fatalf("reversed rows = %+v, want one reversed by the operator", reversedRows.Data)
	}
	for _, hidden := range []string{"refunded_amount_cents", "platform_fee_kept", "reversal_note", "reversed_by_operator"} {
		if bytes.Contains(listBody.Data, []byte(hidden)) {
			t.Fatalf("the Organization's Sales list carries %q; the money memo is operator-facing only", hidden)
		}
	}

	// The Customer's own view agrees with the email, and offers no undo: there
	// is nothing left for them to undo, and no un-reversal exists.
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	area := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if area.Status != "reversed" || area.Reversible || area.ReversibleUntil != nil {
		t.Fatalf("Customer Area sale = %+v, want a reversed sale with no undo offer", area)
	}

	// And the operator's own lookup carries the memo, so the full record is one
	// lookup away from the reference a support thread quoted.
	looked := lookUpSale(t, env, operatorSessionID, ref).Sale
	if looked.Status != "reversed" || looked.ReversedBy == nil || *looked.ReversedBy != "operator" {
		t.Fatalf("looked-up sale = %+v, want reversed by the operator", looked)
	}
	if looked.OperatorReversal == nil || looked.OperatorReversal.Operator != "operator@example.com" ||
		looked.OperatorReversal.RefundedAmountCents == nil || *looked.OperatorReversal.RefundedAmountCents != paidRefund(2) ||
		looked.OperatorReversal.PlatformFeeKept == nil || !*looked.OperatorReversal.PlatformFeeKept {
		t.Fatalf("looked-up money memo = %+v", looked.OperatorReversal)
	}
}

// TestOperatorReversalIgnoresTheReversalWindow: the window is not consulted on
// this path in either direction. The sale here was bought yesterday morning, so
// its window shut at 20:00 that evening and the buyer's own undo is long gone —
// which is precisely the situation the feature exists for.
//
// The in-window half is proved by the tracer bullet above, where the sale is
// marked hours before its window closes.
func TestOperatorReversalIgnoresTheReversalWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Late Fest", "operator-late-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "operator-late-fest", gaID, "ana@example.com")

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	// The next day: past 20:00 Ecuador time on the day of purchase, so the
	// lookup reports the window passed and the buyer can do nothing.
	tomorrow := env.fixedClock.Add(24 * time.Hour)
	holdClocksAt(tomorrow)
	if !lookUpSale(t, env, operatorSessionID, ref).Sale.ReversalWindowPassed {
		t.Fatal("the Reversal Window has not passed a day after the purchase; the fixture proves nothing")
	}

	result := operatorReverseOK(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(false),
	})
	if result.Status != "reversed" {
		t.Fatalf("result = %+v; a passed Reversal Window must not block an Operator Reversal", result)
	}
	if status, reversedAt, _ := saleProvenance(t, env, ref); status != "reversed" || !reversedAt.Time.Equal(tomorrow) {
		t.Fatalf("provenance = %q at %+v, want reversed at the server's clock %v", status, reversedAt, tomorrow)
	}
	if got := remaining(t, env, "operator-late-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after the marking, want 10", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 {
		t.Fatalf("captured %d void notices, want exactly 1", len(voided))
	}

	// No fee kept, and nothing else asserted about it here: what the platform's
	// revenue total does with a kept fee is #127's question.
	_, _, refunded, feeKept := operatorMemo(t, env, ref)
	if !refunded.Valid || int(refunded.Int64) != paidRefund(1) || !feeKept.Valid || feeKept.Bool {
		t.Fatalf("money memo = %+v / %+v, want %d refunded with the fee returned", refunded, feeKept, paidRefund(1))
	}
}

// TestOperatorReversalDrivesTheBalanceNegativeAfterAPayout: the sale was
// settled with the Organization before anybody asked for the refund, so
// reversing it leaves the Organization owing the platform — and the signed
// Withdrawable Balance says so rather than clamping at zero. No code change is
// expected for this; it is asserted because it is the case that hurts.
func TestOperatorReversalDrivesTheBalanceNegativeAfterAPayout(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Settled Fest", "settled-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "settled-fest", gaID, "ana@example.com")

	// The platform has already paid the Organization every cent of it.
	recordPayout(t, env, testOrgSlug, feeTestBaseCents, "2026-07-07", "settled in full")
	if got := getPayouts(t, env, sessionID).WithdrawableBalanceCents; got != 0 {
		t.Fatalf("balance after settling in full = %d, want 0", got)
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(true),
	})

	if got := getPayouts(t, env, sessionID).WithdrawableBalanceCents; got != -feeTestBaseCents {
		t.Fatalf("balance after the marking = %d, want %d — what the Organization owes back is stated, not hidden",
			got, -feeTestBaseCents)
	}
}

// TestOperatorReversalRequiresTheMoneyFacts: the operator states what the buyer
// actually got back and whether the platform kept its fee, and neither has a
// default — a pre-filled amount invites rubber-stamping, and a defaulted fee
// decision would put words in the operator's mouth.
//
// Every refusal leaves the sale exactly as it was, which is asserted once at the
// end against the state every one of them shared.
func TestOperatorReversalRequiresTheMoneyFacts(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Strict Fest", "strict-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "strict-fest", gaID, "ana@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	collected := paidRefund(1)
	cases := []struct {
		name string
		body operatorReversalBody
		code string
	}{
		{
			name: "no refunded amount at all",
			body: operatorReversalBody{PlatformFeeKept: boolPtr(true)},
			code: "VALIDATION_FAILED",
		},
		{
			name: "zero refunded, which is not the same as nothing to refund",
			body: operatorReversalBody{RefundedAmountCents: intPtr(0), PlatformFeeKept: boolPtr(true)},
			code: "VALIDATION_FAILED",
		},
		{
			name: "a negative refund",
			body: operatorReversalBody{RefundedAmountCents: intPtr(-100), PlatformFeeKept: boolPtr(true)},
			code: "VALIDATION_FAILED",
		},
		{
			name: "no fee decision",
			body: operatorReversalBody{RefundedAmountCents: intPtr(collected)},
			code: "VALIDATION_FAILED",
		},
		{
			name: "more than the buyer ever paid",
			body: operatorReversalBody{RefundedAmountCents: intPtr(collected + 1), PlatformFeeKept: boolPtr(true)},
			code: "REFUNDED_AMOUNT_EXCEEDS_COLLECTED",
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

	assertNothingChanged(t, env, ref, "strict-fest", "GA", 9)

	// And the same sale is marked once the facts are stated, so the refusals
	// above are about the request rather than about the sale.
	operatorReverseOK(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(collected),
		PlatformFeeKept:     boolPtr(true),
	})
}

// TestOperatorReversalIsRefusedOnAnAlreadyReversedSale: pressing twice, or
// racing the buyer's own undo, records nothing twice — no second void notice,
// no capacity restored again, and no second money memo overwriting the first.
func TestOperatorReversalIsRefusedOnAnAlreadyReversedSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Twice Fest", "operator-twice-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "operator-twice-fest", gaID, "ana@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	operatorReverseOK(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(true),
		Note:                strPtr("the first and only marking"),
	})
	env.email.Reset()

	resp, body := operatorReverseRequest(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(false),
		Note:                strPtr("a second press that must record nothing"),
	})
	assertRefused(t, resp, body, http.StatusConflict, "SALE_ALREADY_REVERSED")

	if got := remaining(t, env, "operator-twice-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after marking twice, want 10 — capacity was restored more than once", got)
	}
	if voided := env.email.Voided(); len(voided) != 0 {
		t.Fatalf("captured %d void notices from the refused second marking, want none", len(voided))
	}
	_, note, _, feeKept := operatorMemo(t, env, ref)
	if !note.Valid || note.String != "the first and only marking" || !feeKept.Valid || !feeKept.Bool {
		t.Fatalf("memo = %+v / %+v after a refused second marking, want the first one untouched", note, feeKept)
	}
}

// TestOperatorReversalLosesToTheCustomersOwnUndo: the buyer got there first,
// through their own Reversal Window. The operator's marking finds nothing to do
// and says so, and the sale keeps the buyer's provenance rather than being
// restamped as a platform action.
func TestOperatorReversalLosesToTheCustomersOwnUndo(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishFreeEvent(t, env, sessionID, "Race Fest", "operator-race-fest", 10)
	ref := claimFree(t, env, "operator-race-fest", gaID, "ana@example.com", 1)
	undoOwnSale(t, env, "ana@example.com", ref)
	env.email.Reset()

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	resp, body := operatorReverseRequest(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(500),
		PlatformFeeKept:     boolPtr(true),
	})
	assertRefused(t, resp, body, http.StatusConflict, "SALE_ALREADY_REVERSED")

	if _, _, reversedBy := saleProvenance(t, env, ref); !reversedBy.Valid || reversedBy.String != "customer" {
		t.Fatalf("reversed_by = %+v, want the buyer's own undo left standing", reversedBy)
	}
	operatorEmail, _, refunded, _ := operatorMemo(t, env, ref)
	if operatorEmail.Valid || refunded.Valid {
		t.Fatalf("a refused marking wrote a money memo: operator=%+v refunded=%+v", operatorEmail, refunded)
	}
	if voided := env.email.Voided(); len(voided) != 0 {
		t.Fatalf("captured %d void notices from a refused marking, want none", len(voided))
	}
}

// TestOperatorReversalIsRefusedOnAnImportedSale: no money for an imported sale
// ever passed through the platform, so there is nothing here for an operator to
// assert about it. Its undo is the batch-level Sale Import undo, and stays so.
func TestOperatorReversalIsRefusedOnAnImportedSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	ref := seedSaleForCustomer(t, env, sessionID, "Imported Fest", "operator-imported-fest",
		env.fixedClock.Add(30*24*time.Hour), "operator-import-1", "ana@example.com", "Ana", "Lopez")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	resp, body := operatorReverseRequest(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(2500),
		PlatformFeeKept:     boolPtr(false),
	})
	assertRefused(t, resp, body, http.StatusConflict, "SALE_NOT_REVERSIBLE")

	if status, _, _ := saleProvenance(t, env, ref); status != "active" {
		t.Fatalf("status = %q after a refused marking, want active", status)
	}
	if voided := env.email.Voided(); len(voided) != 0 {
		t.Fatalf("captured %d void notices, want none", len(voided))
	}
}

// TestOperatorReversalIsOperatorsOnly: reversal-by-assertion stays with the
// party that moved the money. An Org Admin of the very Organization whose sale
// it is cannot mark it — the platform cannot verify an organizer's claim that
// they refunded somebody, and letting them would let an Organization erase
// fee-bearing revenue on its own say-so.
func TestOperatorReversalIsOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Gated Fest", "operator-gated-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "operator-gated-fest", gaID, "ana@example.com")
	env.email.Reset()

	body := operatorReversalBody{RefundedAmountCents: intPtr(paidRefund(1)), PlatformFeeKept: boolPtr(true)}

	resp, envBody := operatorReverseRequest(t, env, "", ref, body)
	if resp.StatusCode != http.StatusUnauthorized || envBody.Error == nil || envBody.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("unauthenticated marking status=%d error=%+v, want 401 UNAUTHORIZED", resp.StatusCode, envBody.Error)
	}

	resp, envBody = operatorReverseRequest(t, env, sessionID, ref, body)
	if resp.StatusCode != http.StatusForbidden || envBody.Error == nil || envBody.Error.Code != "FORBIDDEN" {
		t.Fatalf("org_admin marking status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, envBody.Error)
	}

	assertNothingChanged(t, env, ref, "operator-gated-fest", "GA", 9)

	// And an operator, who is a Member of nothing, marks the same sale.
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	if got := operatorReverseOK(t, env, operatorSessionID, ref, body); got.Status != "reversed" {
		t.Fatalf("operator marking = %+v", got)
	}
}

// TestOperatorReversalOfAnUnknownReference is the same 404 the lookup gives: a
// mistyped reference names no sale, and the marking says so rather than
// inventing one.
func TestOperatorReversalOfAnUnknownReference(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	resp, body := operatorReverseRequest(t, env, operatorSessionID, "TP-NOSUCHREF", operatorReversalBody{
		RefundedAmountCents: intPtr(100),
		PlatformFeeKept:     boolPtr(true),
	})
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Fatalf("unknown reference status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}
}
