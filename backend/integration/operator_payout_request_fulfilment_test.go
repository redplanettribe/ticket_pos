package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fulfilling and declining a Payout Request: the Platform Operator answers
// (#177, ADR 0026).
//
// #176 gave the operator the backlog to read. This is the half that writes, and
// almost everything difficult about it lives at this seam rather than in any one
// layer:
//
//   - FULFILMENT IS A COMPARE-AND-SWAP IN ONE TRANSACTION. The Payout is
//     inserted exactly as the direct path inserts it, then the request is
//     updated `WHERE status = 'pending'`. Zero rows affected rolls the WHOLE
//     transaction back, so the loser of a race leaves NO Payout row behind. That
//     property is asserted by COUNTING the rows in the table — inferring it from
//     the request's status would prove only that the second update failed, which
//     is the easy half.
//
//   - THE REFUSAL IS THE MITIGATION. The compare-and-swap stops a double
//     RECORD, not a double TRANSFER (ADR 0026). An operator who genuinely wired
//     the money and lost the race must be told to record the Payout directly, or
//     they will silently drop a real payment on the floor and the books will
//     understate what left the account. The message is the whole of that
//     defence, so it is asserted as a message and not merely as a code.
//
//   - A FULFILLED PAYOUT IS INDISTINGUISHABLE FROM A DIRECTLY RECORDED ONE.
//     Asserted by recording one of each and comparing them field for field, and
//     by reading the Organization's own payouts page, which knows nothing about
//     requests and must not start to.
//
//   - AUTHORITY IS THE SESSION'S. resolved_by and the Payout's recorded_by are
//     both taken from the Staff Session and never from a body, exactly as
//     payouts.recorded_by and the Operator Reversal's operator email are
//     (ADR 0015, ADR 0019).

// operatorFulfilment is what the operator is handed after answering with money:
// the Payout that was recorded, and the request as it now stands.
//
// Both, rather than one: the Payout is the money fact and the request is the
// queue's answer, and an operator who fulfilled needs to see the record they
// just created as well as the ask leaving their backlog.
type operatorFulfilment struct {
	Payout  operatorPayout `json:"payout"`
	Request payoutRequest  `json:"request"`
}

func fulfilPath(requestID string) string {
	return operatorPayoutRequestsPath + "/" + requestID + "/fulfil"
}

func declinePath(requestID string) string {
	return operatorPayoutRequestsPath + "/" + requestID + "/decline"
}

// fulfilBody is the fulfilment form as it submits: the same three fields the
// direct record-payout endpoint takes, and deliberately no operator identity.
func fulfilBody(amountCents int, paidAt, note string) map[string]any {
	body := map[string]any{"amount_cents": amountCents, "paid_at": paidAt}
	if note != "" {
		body["note"] = note
	}
	return body
}

func fulfilPayoutRequest(t *testing.T, env *testEnv, sessionID, requestID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, fulfilPath(requestID), body, authHeader(sessionID))
}

func fulfilPayoutRequestOK(t *testing.T, env *testEnv, sessionID, requestID string, body map[string]any) operatorFulfilment {
	t.Helper()
	resp, envBody := fulfilPayoutRequest(t, env, sessionID, requestID, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("fulfil status=%d; want 201; error=%+v", resp.StatusCode, envBody.Error)
	}
	var result operatorFulfilment
	if err := json.Unmarshal(envBody.Data, &result); err != nil {
		t.Fatalf("decode fulfilment: %v", err)
	}
	return result
}

func declinePayoutRequest(t *testing.T, env *testEnv, sessionID, requestID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, declinePath(requestID), body, authHeader(sessionID))
}

// payoutRowCount counts the Organization's `payouts` rows in the database
// itself, going around every API on purpose.
//
// This is the assertion the whole compare-and-swap exists to satisfy, and no
// endpoint can make it honestly: a refused fulfilment that left an orphan row
// behind would still report the request as already resolved, and every
// request-shaped read would agree. Only the ledger knows.
func payoutRowCount(t *testing.T, env *testEnv, orgSlug string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRow(`
		SELECT COUNT(*)
		FROM payouts p
		JOIN organizations o ON o.id = p.organization_id
		WHERE o.slug = $1
	`, orgSlug).Scan(&count); err != nil {
		t.Fatalf("count payouts: %v", err)
	}
	return count
}

// TestOperatorFulfilsPayoutRequestAndTheOrganizationSeesThePayout is the whole
// feature in one pass: the operator transfers the money by hand, records it
// once, and the request is answered in the same action — there is no second step
// to forget (ADR 0026).
//
// It then asserts the property the Organization actually cares about: the Payout
// that came out of a request is indistinguishable from one recorded directly. It
// lands on the same payouts page in the same shape and moves the same balance,
// and nothing on that surface knows a request was involved.
func TestOperatorFulfilsPayoutRequestAndTheOrganizationSeesThePayout(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Fulfil Fest", "fulfil-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	result := fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID,
		fulfilBody(payable, "2026-07-20", "July settlement"))

	// The Payout, exactly as the direct path would have written it.
	if result.Payout.ID == "" || result.Payout.AmountCents != payable ||
		result.Payout.PaidAt != "2026-07-20" || result.Payout.CreatedAt == "" ||
		result.Payout.Note == nil || *result.Payout.Note != "July settlement" {
		t.Fatalf("recorded payout = %+v", result.Payout)
	}
	// Taken from the Staff Session and never from the body, exactly as it is on
	// the direct path and on an Operator Reversal (ADR 0015, ADR 0019).
	if result.Payout.RecordedBy == nil || *result.Payout.RecordedBy != "operator@example.com" {
		t.Fatalf("recorded_by = %v; want the acting operator's email", result.Payout.RecordedBy)
	}

	// The request, answered in the same transaction.
	if result.Request.ID != request.ID || result.Request.Status != "paid" {
		t.Fatalf("answered request = %+v; want the same ask, paid", result.Request)
	}
	if result.Request.PayoutID == nil || *result.Request.PayoutID != result.Payout.ID {
		t.Fatalf("payout_id = %v; want the Payout just recorded (%s)", result.Request.PayoutID, result.Payout.ID)
	}
	if result.Request.ResolvedBy == nil || *result.Request.ResolvedBy != "operator@example.com" {
		t.Fatalf("resolved_by = %v; want the acting operator's email", result.Request.ResolvedBy)
	}
	if result.Request.ResolvedAt == nil || *result.Request.ResolvedAt == "" {
		t.Fatalf("resolved_at = %v; want the instant it was answered", result.Request.ResolvedAt)
	}
	if result.Request.ResolutionReason != nil {
		t.Fatalf("resolution_reason on a paid request = %v; want null", result.Request.ResolutionReason)
	}
	// The ask keeps what was asked. It is a record of a claim, not of a payment.
	if result.Request.AmountCents != payable {
		t.Fatalf("request amount after fulfilment = %d; want the figure asked for, %d",
			result.Request.AmountCents, payable)
	}

	// The Organization's own page, which knows nothing about requests: the same
	// Payout, and a balance that has already moved.
	orgView := getPayouts(t, env, adminSessionID)
	if len(orgView.Payouts) != 1 || orgView.Payouts[0].ID != result.Payout.ID ||
		orgView.Payouts[0].AmountCents != payable || orgView.Payouts[0].PaidAt != "2026-07-20" ||
		orgView.Payouts[0].Note == nil || *orgView.Payouts[0].Note != "July settlement" {
		t.Fatalf("organization payout history = %+v", orgView.Payouts)
	}
	if orgView.WithdrawableBalanceCents != 0 {
		t.Fatalf("withdrawable balance after settling in full = %d; want 0", orgView.WithdrawableBalanceCents)
	}

	// It has left the backlog, in both readings of it.
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 0 {
		t.Fatalf("pending count after fulfilment = %d; want 0", count)
	}
	if queue := operatorQueue(t, env, operatorSessionID, ""); len(queue.Data) != 0 {
		t.Fatalf("queue after fulfilment = %+v; want empty", queue.Data)
	}
}

// TestOperatorFulfilledPayoutIsIndistinguishableFromADirectOne records one of
// each against the same Organization and compares them.
//
// A Payout is a Payout however it was initiated (CONTEXT.md), and the ledger
// carries no trace of which door it came through: no request id, no marker, no
// second shape. The only difference is on the REQUEST, which names the Payout it
// became — the one and only link between the queue and the ledger.
func TestOperatorFulfilledPayoutIsIndistinguishableFromADirectOne(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Twin Fest", "twin-fest", 8, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	fulfilled := fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID,
		fulfilBody(1000, "2026-07-20", "from the request"))

	resp, body := env.post(t, "/api/v1/operator/organizations/"+orgID+"/payouts", map[string]any{
		"amount_cents": 1000,
		"paid_at":      "2026-07-20",
		"note":         "from the request",
	}, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("direct payout status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var direct operatorPayout
	if err := json.Unmarshal(body.Data, &direct); err != nil {
		t.Fatalf("decode direct payout: %v", err)
	}

	// Everything but the identity and the creation instant, which are the only
	// two things that CAN differ between two rows.
	if fulfilled.Payout.AmountCents != direct.AmountCents ||
		fulfilled.Payout.PaidAt != direct.PaidAt ||
		fulfilled.Payout.Note == nil || direct.Note == nil || *fulfilled.Payout.Note != *direct.Note ||
		fulfilled.Payout.RecordedBy == nil || direct.RecordedBy == nil ||
		*fulfilled.Payout.RecordedBy != *direct.RecordedBy {
		t.Fatalf("fulfilled %+v differs from directly recorded %+v", fulfilled.Payout, direct)
	}

	// And on the Organization's page they are two entries of one kind.
	orgView := getPayouts(t, env, adminSessionID)
	if len(orgView.Payouts) != 2 {
		t.Fatalf("organization payout history = %+v; want both settlements", orgView.Payouts)
	}
	if want := payable - 2000; orgView.WithdrawableBalanceCents != want {
		t.Fatalf("balance = %d; want %d — both Payouts subtracted alike", orgView.WithdrawableBalanceCents, want)
	}
}

// TestOperatorFulfilmentRefusalLeavesNoOrphanPayout is the subtlest assertion in
// the feature and the reason fulfilment is one transaction.
//
// The second operator's Payout INSERT succeeds — nothing stops it — and then the
// guarded UPDATE touches nothing, because the request is no longer pending. If
// the two statements were not in one transaction, or if the zero-row result were
// treated as anything but a rollback, the Organization would be recorded as
// having been paid twice while the operator was told the fulfilment failed. So
// the assertion counts `payouts` rows in the database directly: after the
// refusal there must still be exactly one.
//
// The refusal must also name the state and who got there first, and — the whole
// of the mitigation — tell an operator who genuinely transferred to record the
// Payout directly. The compare-and-swap prevents a double record, not a double
// transfer (ADR 0026).
func TestOperatorFulfilmentRefusalLeavesNoOrphanPayout(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Race Fest", "race-fest", 6, 1)

	firstSessionID := operatorSession(t, env, "first@example.com")
	secondSessionID := operatorSession(t, env, "second@example.com")

	fulfilPayoutRequestOK(t, env, firstSessionID, request.ID, fulfilBody(payable, "2026-07-20", ""))
	if count := payoutRowCount(t, env, "test-org"); count != 1 {
		t.Fatalf("payout rows after the first fulfilment = %d; want 1", count)
	}

	resp, body := fulfilPayoutRequest(t, env, secondSessionID, request.ID, fulfilBody(payable, "2026-07-21", ""))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second fulfilment status=%d; want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" || !isNullData(body.Data) {
		t.Fatalf("second fulfilment envelope: data=%s error=%+v", body.Data, body.Error)
	}

	// THE ASSERTION. Counted, not inferred: the rolled-back INSERT must leave
	// nothing behind.
	if count := payoutRowCount(t, env, "test-org"); count != 1 {
		t.Fatalf("payout rows after the refused fulfilment = %d; want the first one alone", count)
	}
	orgView := getPayouts(t, env, adminSessionID)
	if len(orgView.Payouts) != 1 || orgView.WithdrawableBalanceCents != 0 {
		t.Fatalf("organization sees %d payouts and a %d balance; want one Payout and a settled 0",
			len(orgView.Payouts), orgView.WithdrawableBalanceCents)
	}

	// Who got there first, so the loser learns what actually happened instead of
	// getting a generic conflict.
	if !strings.Contains(body.Error.Message, "first@example.com") {
		t.Fatalf("refusal message = %q; want it to name who resolved it first", body.Error.Message)
	}
	// And the sentence that stops an honest operator dropping a real transfer on
	// the floor.
	if !strings.Contains(strings.ToLower(body.Error.Message), "record the payout directly") {
		t.Fatalf("refusal message = %q; want it to tell an operator who also transferred to record the Payout directly",
			body.Error.Message)
	}
	details, _ := json.Marshal(body.Error.Details)
	var parsed struct {
		Status     string `json:"status"`
		ResolvedBy string `json:"resolved_by"`
	}
	if err := json.Unmarshal(details, &parsed); err != nil {
		t.Fatalf("decode refusal details: %v", err)
	}
	if parsed.Status != "paid" || parsed.ResolvedBy != "first@example.com" {
		t.Fatalf("refusal details = %+v; want the current state and its resolver", parsed)
	}
	// No bank detail rides an error, here as anywhere (ADR 0026).
	assertNoBankDetails(t, body.Error)

	// And the second operator's honest move works: recording directly is never
	// refused, whatever the request says.
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	direct, directBody := env.post(t, "/api/v1/operator/organizations/"+orgID+"/payouts",
		map[string]any{"amount_cents": payable, "paid_at": "2026-07-21", "note": "also transferred"},
		authHeader(secondSessionID))
	if direct.StatusCode != http.StatusCreated {
		t.Fatalf("direct payout after a lost race status=%d error=%+v", direct.StatusCode, directBody.Error)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 2 {
		t.Fatalf("payout rows after recording directly = %d; want 2", count)
	}
}

// TestOperatorFulfilsForLessThanWasAsked: partial fulfilment needs no model of
// its own (ADR 0026).
//
// An operator who transfers less records what MOVED. The request keeps what was
// ASKED, goes `paid` all the same, and the divergence between the two is visible
// on the request forever — for the Organization as much as for the operator,
// which is what lets them ask again for the rest.
func TestOperatorFulfilsForLessThanWasAsked(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Partial Fest", "partial-fest", 6, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	half := payable / 2
	result := fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID,
		fulfilBody(half, "2026-07-20", "half now, half after the show"))

	if result.Payout.AmountCents != half {
		t.Fatalf("payout amount = %d; want what actually moved, %d", result.Payout.AmountCents, half)
	}
	if result.Request.AmountCents != payable || result.Request.Status != "paid" {
		t.Fatalf("request after a partial fulfilment = %+v; want the asked figure, paid", result.Request)
	}

	// The Organization reads both halves of the divergence on its own surface:
	// what it asked for, and what it got.
	requests := listPayoutRequests(t, env, adminSessionID)
	if len(requests) != 1 || requests[0].Status != "paid" || requests[0].AmountCents != payable {
		t.Fatalf("organization request history = %+v", requests)
	}
	orgView := getPayouts(t, env, adminSessionID)
	if len(orgView.Payouts) != 1 || orgView.Payouts[0].AmountCents != half {
		t.Fatalf("organization payout history = %+v; want the smaller figure that moved", orgView.Payouts)
	}
	if orgView.WithdrawableBalanceCents != payable-half {
		t.Fatalf("balance = %d; want the unpaid remainder %d", orgView.WithdrawableBalanceCents, payable-half)
	}

	// Nobody is chased for the rest except by the Organization asking again, and
	// a paid request never stands in the way of that.
	again, created := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable-half, "the other half"))
	if !created || again.Status != "pending" {
		t.Fatalf("second ask after a partial fulfilment: created=%v request=%+v", created, again)
	}
}

// TestOperatorDeclinesPayoutRequestWithAReasonTheOrganizationReads: a queue that
// swallows requests silently generates the support thread it was built to
// prevent (ADR 0026), so a decline carries a reason and the asker is shown it.
//
// A decline is a "not this" rather than a lockout: it ends the request and frees
// the Organization to ask again immediately.
func TestOperatorDeclinesPayoutRequestWithAReasonTheOrganizationReads(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Decline Fest", "decline-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	resp, body := declinePayoutRequest(t, env, operatorSessionID, request.ID,
		map[string]any{"reason": "Your Event is three months out; ask again after the doors open."})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decline status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var declined payoutRequest
	if err := json.Unmarshal(body.Data, &declined); err != nil {
		t.Fatalf("decode declined request: %v", err)
	}
	if declined.Status != "declined" || declined.PayoutID != nil {
		t.Fatalf("declined request = %+v; want declined and no Payout", declined)
	}
	if declined.ResolutionReason == nil || *declined.ResolutionReason != "Your Event is three months out; ask again after the doors open." {
		t.Fatalf("resolution_reason = %v", declined.ResolutionReason)
	}
	if declined.ResolvedBy == nil || *declined.ResolvedBy != "operator@example.com" ||
		declined.ResolvedAt == nil {
		t.Fatalf("resolution = by %v at %v; want the acting operator and the instant",
			declined.ResolvedBy, declined.ResolvedAt)
	}

	// Nothing moved. A declined request is an answer, not a ledger entry.
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after a decline = %d; want none", count)
	}

	// The Organization reads the reason on its own surface. This is the whole
	// point of requiring one.
	requests := listPayoutRequests(t, env, adminSessionID)
	if len(requests) != 1 || requests[0].Status != "declined" ||
		requests[0].ResolutionReason == nil ||
		!strings.Contains(*requests[0].ResolutionReason, "three months out") {
		t.Fatalf("organization request history = %+v; want the decline and its reason", requests)
	}

	// And it frees them to ask again: a decline is a "not this", not a lockout.
	again, created := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "asking again"))
	if !created || again.Status != "pending" || again.ID == request.ID {
		t.Fatalf("ask after a decline: created=%v request=%+v", created, again)
	}
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count after a decline and a fresh ask = %d; want 1", count)
	}
}

// TestOperatorDeclineRequiresAReason: the reason is structurally inseparable
// from the refusal. A blank one, a whitespace one and a missing one are all the
// same failure, and none of them resolves anything.
func TestOperatorDeclineRequiresAReason(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Reason Fest", "reason-fest", 4, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing", map[string]any{}},
		{"empty", map[string]any{"reason": ""}},
		{"whitespace", map[string]any{"reason": "   "}},
		{"beyond the bound", map[string]any{"reason": strings.Repeat("x", 501)}},
	}
	for _, tc := range cases {
		resp, body := declinePayoutRequest(t, env, operatorSessionID, request.ID, tc.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s reason status=%d; want 400; error=%+v", tc.name, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" || !isNullData(body.Data) {
			t.Fatalf("%s reason envelope: data=%s error=%+v", tc.name, body.Data, body.Error)
		}
		details, _ := json.Marshal(body.Error.Details)
		var parsed struct {
			Fields []struct {
				Field string `json:"field"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(details, &parsed); err != nil {
			t.Fatalf("%s decode details: %v", tc.name, err)
		}
		if len(parsed.Fields) != 1 || parsed.Fields[0].Field != "reason" {
			t.Fatalf("%s fields = %+v; want reason alone", tc.name, parsed.Fields)
		}
	}

	// Nothing was resolved by any of them.
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count after four refused declines = %d; want the ask still waiting", count)
	}
}

// TestOperatorFulfilValidation: the fulfilment form's shape is the handler's
// business, and it is the SAME shape the direct record-payout endpoint takes —
// an amount above zero and a calendar day. Nothing is recorded and nothing is
// resolved by a malformed one.
func TestOperatorFulfilValidation(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Shape Fest", "shape-fest", 4, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"zero amount", map[string]any{"amount_cents": 0, "paid_at": "2026-07-20"}, "amount_cents"},
		{"negative amount", map[string]any{"amount_cents": -100, "paid_at": "2026-07-20"}, "amount_cents"},
		{"missing date", map[string]any{"amount_cents": 100}, "paid_at"},
		{"instant, not a day", map[string]any{"amount_cents": 100, "paid_at": "2026-07-20T10:00:00Z"}, "paid_at"},
	}
	for _, tc := range cases {
		resp, body := fulfilPayoutRequest(t, env, operatorSessionID, request.ID, tc.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status=%d; want 400; error=%+v", tc.name, resp.StatusCode, body.Error)
		}
		if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" || !isNullData(body.Data) {
			t.Fatalf("%s envelope: data=%s error=%+v", tc.name, body.Data, body.Error)
		}
	}

	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after four refused fulfilments = %d; want none", count)
	}
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count = %d; want the ask still waiting", count)
	}
}

// TestOperatorCannotAnswerAnAlreadyResolvedRequest: all three end states are
// final (ADR 0026), so neither action reaches a request that has ended — whoever
// ended it. A request the ORGANIZATION cancelled is refused in exactly the same
// terms as one another operator paid, because the fact that matters is the same:
// it is no longer pending, and somebody is named.
func TestOperatorCannotAnswerAnAlreadyResolvedRequest(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Final Fest", "final-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	if resp, _ := cancelPayoutRequest(t, env, adminSessionID, request.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", resp.StatusCode)
	}

	resp, body := fulfilPayoutRequest(t, env, operatorSessionID, request.ID, fulfilBody(payable, "2026-07-20", ""))
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" {
		t.Fatalf("fulfilling a cancelled request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if !strings.Contains(body.Error.Message, "admin@example.com") {
		t.Fatalf("refusal message = %q; want it to name the Org Admin who cancelled", body.Error.Message)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after fulfilling a cancelled request = %d; want none", count)
	}

	resp, body = declinePayoutRequest(t, env, operatorSessionID, request.ID, map[string]any{"reason": "too late"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" {
		t.Fatalf("declining a cancelled request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}

	// A request that has never existed, and an id that is not one, are 404 on
	// both actions — the same answer the read gives.
	for _, id := range []string{"a0000000-0000-4000-8000-00000000dead", "not-a-uuid"} {
		resp, body := fulfilPayoutRequest(t, env, operatorSessionID, id, fulfilBody(100, "2026-07-20", ""))
		if resp.StatusCode != http.StatusNotFound || body.Error == nil ||
			body.Error.Code != "PAYOUT_REQUEST_NOT_FOUND" {
			t.Fatalf("fulfil %q status=%d error=%+v; want 404", id, resp.StatusCode, body.Error)
		}
		resp, body = declinePayoutRequest(t, env, operatorSessionID, id, map[string]any{"reason": "no"})
		if resp.StatusCode != http.StatusNotFound || body.Error == nil ||
			body.Error.Code != "PAYOUT_REQUEST_NOT_FOUND" {
			t.Fatalf("decline %q status=%d error=%+v; want 404", id, resp.StatusCode, body.Error)
		}
	}
	// And a declined request cannot then be fulfilled either: the second end
	// state is as final as the first.
	second, _ := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "again"))
	if resp, _ := declinePayoutRequest(t, env, operatorSessionID, second.ID, map[string]any{"reason": "not now"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("decline status=%d", resp.StatusCode)
	}
	resp, body = fulfilPayoutRequest(t, env, operatorSessionID, second.ID, fulfilBody(payable, "2026-07-20", ""))
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" {
		t.Fatalf("fulfilling a declined request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after answering resolved requests = %d; want none", count)
	}
}

// TestOperatorAnsweringRequiresAnAllowlistedEmail: both writes join the operator
// namespace's gate unchanged (ADR 0015).
//
// The Org Admin refused here is the very person who made the request. Reading
// their own ask and cancelling it is their business; answering it is not, and
// being an Org Admin of the Organization being paid grants nothing.
func TestOperatorAnsweringRequiresAnAllowlistedEmail(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Gate Two Fest", "gate-two-fest", 4, 1)

	writes := []struct {
		path string
		body map[string]any
	}{
		{fulfilPath(request.ID), fulfilBody(payable, "2026-07-20", "")},
		{declinePath(request.ID), map[string]any{"reason": "no"}},
	}

	for _, write := range writes {
		resp, body := env.post(t, write.path, write.body, nil)
		if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("POST %s anonymous status=%d error=%+v; want 401", write.path, resp.StatusCode, body.Error)
		}
		resp, body = env.post(t, write.path, write.body, authHeader(adminSessionID))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("POST %s as org_admin status=%d error=%+v; want 403", write.path, resp.StatusCode, body.Error)
		}
	}

	// Nothing a refused caller sent was written.
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after refused writes = %d; want none", count)
	}
	requests := listPayoutRequests(t, env, adminSessionID)
	if len(requests) != 1 || requests[0].Status != "pending" {
		t.Fatalf("request after refused writes = %+v; want it still waiting", requests)
	}

	// And an operator who is a Member of no Organization at all answers it,
	// because operator authority is orthogonal to Membership.
	operatorSessionID := operatorSession(t, env, "nomember@example.com")
	result := fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID, fulfilBody(payable, "2026-07-20", ""))
	if result.Request.ResolvedBy == nil || *result.Request.ResolvedBy != "nomember@example.com" {
		t.Fatalf("resolved_by = %v; want the Member-less operator's email", result.Request.ResolvedBy)
	}
}

// The `processing` state: a transfer submitted, and a bank that has not
// confirmed it (#184, ADR 0026 amendment).
//
// Everything difficult about this state is an ABSENCE, which is why every test
// below counts `payouts` rows rather than reading a status back. A Payout has
// meant money that MOVED since ADR 0014, and PayPhone can take 48 hours and can
// send the money back — so the ledger must stay empty from the moment the
// transfer is submitted until somebody finds out what the bank did. A status
// that reads "processing" over a `payouts` row that should not exist is exactly
// the bug these assertions are here to catch, and no request-shaped read can see
// it.

func processingPath(requestID string) string {
	return operatorPayoutRequestsPath + "/" + requestID + "/processing"
}

func markProcessing(t *testing.T, env *testEnv, sessionID, requestID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, processingPath(requestID), body, authHeader(sessionID))
}

func markProcessingOK(t *testing.T, env *testEnv, sessionID, requestID string, body map[string]any) payoutRequest {
	t.Helper()
	resp, envBody := markProcessing(t, env, sessionID, requestID, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark processing status=%d; want 200; error=%+v", resp.StatusCode, envBody.Error)
	}
	var request payoutRequest
	if err := json.Unmarshal(envBody.Data, &request); err != nil {
		t.Fatalf("decode processing request: %v", err)
	}
	return request
}

// transferStamp reads the who-when-and-reference of a submitted transfer from
// the database, because no wire shape carries them yet — exposing them, with the
// 72-hour stale flag, is #186.
//
// Reading them here is not a shortcut around a missing endpoint: who submitted a
// transfer is a different fact from who resolved the request, and the only way
// to assert the two have not been collapsed into one column is to look at both
// columns.
func transferStamp(t *testing.T, env *testEnv, requestID string) (by, reference *string, at *time.Time) {
	t.Helper()
	var submittedBy, ref sql.NullString
	var submittedAt sql.NullTime
	if err := env.db.QueryRow(`
		SELECT transfer_submitted_by, transfer_submitted_at, transfer_reference
		FROM payout_requests WHERE id = $1
	`, requestID).Scan(&submittedBy, &submittedAt, &ref); err != nil {
		t.Fatalf("read transfer stamp: %v", err)
	}
	if submittedBy.Valid {
		by = &submittedBy.String
	}
	if ref.Valid {
		reference = &ref.String
	}
	if submittedAt.Valid {
		instant := submittedAt.Time
		at = &instant
	}
	return by, reference, at
}

// TestOperatorMarksTransferProcessingAndTheLedgerStaysEmpty is the invariant the
// whole state exists for, asserted by COUNTING `payouts` rows.
//
// The operator submitted a transfer they cannot confirm. That is a fact about
// the request and about nothing else: no Payout, no balance movement, and no
// trace in a ledger a rejected transfer would then have to be deleted out of.
// The count is the assertion — a status reading "processing" proves only that
// the request was updated, which is the easy half.
//
// The request also stays OUTSTANDING, in all three readings of that word: the
// operator's queue, the navigation badge, and the Organization's single slot.
func TestOperatorMarksTransferProcessingAndTheLedgerStaysEmpty(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Wire Fest", "wire-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "sender@example.com")

	processing := markProcessingOK(t, env, operatorSessionID, request.ID,
		map[string]any{"transfer_reference": "PP-2026-0042"})

	if processing.ID != request.ID || processing.Status != "processing" {
		t.Fatalf("marked request = %+v; want the same ask, processing", processing)
	}
	// NOT RESOLVED. A processing request has not been answered — it has been
	// acted on — and resolved_by must keep meaning who ENDED it, which may be a
	// different operator days later.
	if processing.ResolvedBy != nil || processing.ResolvedAt != nil || processing.ResolutionReason != nil {
		t.Fatalf("processing request carries a resolution: %+v", processing)
	}
	if processing.PayoutID != nil {
		t.Fatalf("payout_id on a processing request = %v; want null — no money has landed", processing.PayoutID)
	}

	// THE ASSERTION. Counted, never inferred.
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows while the request is processing = %d; want none — a Payout is money that moved", count)
	}

	// Who submitted the transfer, when, and what the bank called it.
	by, reference, at := transferStamp(t, env, request.ID)
	if by == nil || *by != "sender@example.com" {
		t.Fatalf("transfer_submitted_by = %v; want the acting operator's email from the Staff Session", by)
	}
	if at == nil || at.IsZero() {
		t.Fatalf("transfer_submitted_at = %v; want the instant from the injected clock", at)
	}
	if reference == nil || *reference != "PP-2026-0042" {
		t.Fatalf("transfer_reference = %v; want what the bank handed back", reference)
	}

	// Still outstanding. A badge that dropped when a transfer was submitted
	// would tell the queue it was emptier than it is, and a transfer nobody
	// confirmed is precisely the work that must not fall out of sight.
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count while processing = %d; want the ask still outstanding", count)
	}
	queue := operatorQueue(t, env, operatorSessionID, "")
	if len(queue.Data) != 1 || queue.Data[0].Request.ID != request.ID ||
		queue.Data[0].Request.Status != "processing" {
		t.Fatalf("queue while processing = %+v; want the request, showing its state", queue.Data)
	}

	// And the Organization reads it on its own surface.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 1 || history[0].Status != "processing" {
		t.Fatalf("organization history = %+v; want the ask, processing", history)
	}
}

// TestOperatorMarksTransferProcessingWithoutAReference: the reference is
// optional because PayPhone does not always hand one back synchronously, and a
// required field an operator cannot fill is a field they will type "-" into.
//
// Absent, empty and whitespace-only are one answer — no reference — because all
// three mean the provider gave them nothing, and a stored "" would leave every
// reader coalescing a blank against a null.
func TestOperatorMarksTransferProcessingWithoutAReference(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	operatorSessionID := operatorSession(t, env, "sender@example.com")

	for i, body := range []map[string]any{nil, {}, {"transfer_reference": ""}, {"transfer_reference": "   "}} {
		request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org",
			fmt.Sprintf("Blank Fest %d", i), fmt.Sprintf("blank-fest-%d", i), 4, i+1)

		processing := markProcessingOK(t, env, operatorSessionID, request.ID, body)
		if processing.Status != "processing" {
			t.Fatalf("body %v: status = %q; want processing", body, processing.Status)
		}
		by, reference, at := transferStamp(t, env, request.ID)
		if by == nil || at == nil {
			t.Fatalf("body %v: the who-and-when is incomplete: by=%v at=%v", body, by, at)
		}
		if reference != nil {
			t.Fatalf("body %v: transfer_reference = %q; want null rather than a blank", body, *reference)
		}

		// Cleared so the next iteration's Organization has an empty slot to ask
		// from — a processing request holds it, which is the point of the widened
		// index and is asserted in its own test.
		fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID, fulfilBody(100, "2026-07-20", ""))
	}

	// An over-long reference is a field error naming the field, not a constraint
	// violation an operator cannot act on.
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Long Fest", "long-fest", 4, 6)
	resp, body := markProcessing(t, env, operatorSessionID, request.ID,
		map[string]any{"transfer_reference": strings.Repeat("x", 201)})
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("over-long reference status=%d error=%+v; want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
	}
	if listPayoutRequests(t, env, adminSessionID)[0].Status != "pending" {
		t.Fatalf("a refused reference changed the request's state")
	}
}

// TestOperatorFulfilsAProcessingRequestAndRecordsExactlyOnePayout is the happy
// path of the amendment end to end: pending → processing → paid, with EXACTLY
// ONE Payout at the end of it and none before.
//
// The two operators are deliberately different people. The one who submits a
// transfer and the one who confirms it days later need not be the same, which is
// why transfer_submitted_by exists beside resolved_by rather than being folded
// into it — and the stamp must survive fulfilment, or an operator asking "who
// sent this?" about a settled request has nowhere to look.
func TestOperatorFulfilsAProcessingRequestAndRecordsExactlyOnePayout(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Landed Fest", "landed-fest", 5, 1)
	senderSessionID := operatorSession(t, env, "sender@example.com")
	confirmerSessionID := operatorSession(t, env, "confirmer@example.com")

	markProcessingOK(t, env, senderSessionID, request.ID, map[string]any{"transfer_reference": "PP-77"})
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows while processing = %d; want none", count)
	}

	// The money landed. Fulfilment reaches a processing request exactly as it
	// reaches a pending one — money landing is how a submitted transfer ends.
	result := fulfilPayoutRequestOK(t, env, confirmerSessionID, request.ID,
		fulfilBody(payable, "2026-07-22", "confirmed by the bank"))

	if result.Request.Status != "paid" || result.Request.PayoutID == nil ||
		*result.Request.PayoutID != result.Payout.ID {
		t.Fatalf("fulfilled request = %+v; want paid and pointing at the Payout", result.Request)
	}
	// EXACTLY ONE. Counted: pending → processing → paid records one settlement,
	// not one per transition.
	if count := payoutRowCount(t, env, "test-org"); count != 1 {
		t.Fatalf("payout rows after the transfer landed = %d; want exactly 1", count)
	}

	// Two different operators, two different facts, both kept.
	if result.Request.ResolvedBy == nil || *result.Request.ResolvedBy != "confirmer@example.com" {
		t.Fatalf("resolved_by = %v; want whoever ENDED the request", result.Request.ResolvedBy)
	}
	by, reference, _ := transferStamp(t, env, request.ID)
	if by == nil || *by != "sender@example.com" || reference == nil || *reference != "PP-77" {
		t.Fatalf("transfer stamp after fulfilment = by %v ref %v; want the submitter's own record kept", by, reference)
	}

	// The Organization sees one ordinary Payout and a settled balance. Nothing
	// on that surface knows a transfer was ever awaiting the bank.
	orgView := getPayouts(t, env, adminSessionID)
	if len(orgView.Payouts) != 1 || orgView.Payouts[0].AmountCents != payable ||
		orgView.WithdrawableBalanceCents != 0 {
		t.Fatalf("organization view = %+v (balance %d)", orgView.Payouts, orgView.WithdrawableBalanceCents)
	}
	if count := operatorPendingPayoutRequestCount(t, env, senderSessionID); count != 0 {
		t.Fatalf("pending count after the request was paid = %d; want 0", count)
	}
}

// TestOperatorConcurrentFulfilmentsOfAProcessingRequestRecordOnePayout is the
// widened guard's race, run for real: two operators who both watched the same
// transfer land, both pressing at once.
//
// The compare-and-swap must let exactly one through, and the loser's Payout
// INSERT must be rolled back with it — so THE `payouts` COUNT IS 1, NOT 2. That
// is the assertion, counted in the table, because both operators' requests read
// back as "paid" whichever way it went and no request-shaped read can tell a
// rolled-back INSERT from one that never happened. Widening the guard to accept
// `processing` without keeping the transaction around it would pass every other
// test in this file and fail this one.
func TestOperatorConcurrentFulfilmentsOfAProcessingRequestRecordOnePayout(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Race Two Fest", "race-two-fest", 6, 1)
	senderSessionID := operatorSession(t, env, "sender@example.com")
	firstSessionID := operatorSession(t, env, "first@example.com")
	secondSessionID := operatorSession(t, env, "second@example.com")

	markProcessingOK(t, env, senderSessionID, request.ID, map[string]any{"transfer_reference": "PP-RACE"})

	// Both fulfilments are fired from goroutines released together. They are NOT
	// run through the t-taking helpers: those call t.Fatalf, which is illegal off
	// the test goroutine, so each returns its outcome and the assertions happen
	// back on the main one.
	type outcome struct {
		status int
		code   string
		err    error
	}
	outcomes := make([]outcome, 2)
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(2)
	done.Add(2)
	for i, sessionID := range []string{firstSessionID, secondSessionID} {
		go func(i int, sessionID string) {
			defer done.Done()
			payload, err := json.Marshal(fulfilBody(payable, "2026-07-22", ""))
			if err != nil {
				outcomes[i] = outcome{err: err}
				ready.Done()
				return
			}
			req, err := http.NewRequest(http.MethodPost, env.server.URL+fulfilPath(request.ID), bytes.NewReader(payload))
			if err != nil {
				outcomes[i] = outcome{err: err}
				ready.Done()
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+sessionID)

			ready.Done()
			<-start

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				outcomes[i] = outcome{err: err}
				return
			}
			defer resp.Body.Close()
			var body envelope
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				outcomes[i] = outcome{status: resp.StatusCode, err: err}
				return
			}
			code := ""
			if body.Error != nil {
				code = body.Error.Code
			}
			outcomes[i] = outcome{status: resp.StatusCode, code: code}
		}(i, sessionID)
	}
	ready.Wait()
	close(start)
	done.Wait()

	won, lost := 0, 0
	for _, got := range outcomes {
		if got.err != nil {
			t.Fatalf("concurrent fulfilment failed to run: %v", got.err)
		}
		switch {
		case got.status == http.StatusCreated:
			won++
		case got.status == http.StatusConflict && got.code == "PAYOUT_REQUEST_ALREADY_RESOLVED":
			lost++
		default:
			t.Fatalf("concurrent fulfilment outcome = %+v; want one 201 and one 409 ALREADY_RESOLVED", got)
		}
	}
	if won != 1 || lost != 1 {
		t.Fatalf("concurrent fulfilments: %d won and %d lost; want exactly one of each", won, lost)
	}

	// THE ASSERTION: one settlement, not two. The loser's Payout went back with
	// its transaction.
	if count := payoutRowCount(t, env, "test-org"); count != 1 {
		t.Fatalf("payout rows after two concurrent fulfilments = %d; want 1, not 2", count)
	}
	orgView := getPayouts(t, env, adminSessionID)
	if len(orgView.Payouts) != 1 || orgView.WithdrawableBalanceCents != 0 {
		t.Fatalf("organization sees %d payouts and a %d balance; want one settlement",
			len(orgView.Payouts), orgView.WithdrawableBalanceCents)
	}
}

// TestOperatorCannotDeclineAProcessingRequest: the platform cannot refuse an ask
// its own operator is already acting on (ADR 0026 amendment).
//
// The refusal must not be the resolved-by-somebody sentence, which is false here
// twice over: nothing has been resolved, and its advice — record the Payout
// directly — is precisely the ledger entry this state exists to prevent, since
// the bank may still send the money back. What the reader needs is that the
// transfer is being processed and who submitted it.
//
// A second operator marking the same request processing is refused in the same
// terms, so the first submitter's stamp cannot be overwritten by somebody who
// arrived later.
func TestOperatorCannotDeclineAProcessingRequest(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Inflight Fest", "inflight-fest", 5, 1)
	senderSessionID := operatorSession(t, env, "sender@example.com")
	otherSessionID := operatorSession(t, env, "other@example.com")

	markProcessingOK(t, env, senderSessionID, request.ID, map[string]any{"transfer_reference": "PP-99"})

	resp, body := declinePayoutRequest(t, env, otherSessionID, request.ID,
		map[string]any{"reason": "changed my mind"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED" {
		t.Fatalf("declining a processing request status=%d error=%+v; want 409 TRANSFER_ALREADY_SUBMITTED",
			resp.StatusCode, body.Error)
	}
	if !strings.Contains(strings.ToLower(body.Error.Message), "being processed") {
		t.Fatalf("refusal message = %q; want it to say the transfer is being processed", body.Error.Message)
	}
	if !strings.Contains(body.Error.Message, "sender@example.com") {
		t.Fatalf("refusal message = %q; want it to name who submitted the transfer", body.Error.Message)
	}
	assertNoBankDetails(t, body.Error)

	// A second submission of the same transfer is refused the same way, and the
	// first submitter's record is untouched.
	resp, body = markProcessing(t, env, otherSessionID, request.ID, map[string]any{"transfer_reference": "PP-OTHER"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED" {
		t.Fatalf("re-marking a processing request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	by, reference, _ := transferStamp(t, env, request.ID)
	if by == nil || *by != "sender@example.com" || reference == nil || *reference != "PP-99" {
		t.Fatalf("transfer stamp after a refused second submission = by %v ref %v; want the first submitter's", by, reference)
	}

	// Nothing moved, and the request is exactly where it was.
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after refused answers = %d; want none", count)
	}
	if history := listPayoutRequests(t, env, adminSessionID); history[0].Status != "processing" {
		t.Fatalf("request after refused answers = %+v; want it still processing", history[0])
	}
}

// TestOperatorPaidDirectlyCarriesNoTransferStamp: `pending → paid` in one step
// is untouched, and an instant transfer leaves the new columns null.
//
// This is the case the schema's transfer CHECK has to keep legal. A state
// describing uncertainty must not become a ritual operators click through, so
// nothing requires a request to pass through `processing` on its way to being
// paid — and a `paid` request with no submitted-transfer stamp is a true record
// of a settlement that never needed one.
func TestOperatorPaidDirectlyCarriesNoTransferStamp(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Instant Fest", "instant-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	result := fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID, fulfilBody(payable, "2026-07-20", ""))
	if result.Request.Status != "paid" || result.Request.PayoutID == nil {
		t.Fatalf("direct fulfilment = %+v; want paid, with its Payout", result.Request)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 1 {
		t.Fatalf("payout rows after a direct fulfilment = %d; want 1", count)
	}
	by, reference, at := transferStamp(t, env, request.ID)
	if by != nil || at != nil || reference != nil {
		t.Fatalf("a request paid in one step carries a transfer stamp: by=%v at=%v ref=%v", by, at, reference)
	}
}

// The `failed` state: the bank sent the transfer back (#185, ADR 0026
// amendment).
//
// `failed` is the second half of the honest answer `processing` opened up. Its
// invariants are all ABSENCES and REFUSALS — no `payouts` row, no reopening, no
// collapse into `declined` — so every test below either counts rows in the
// ledger or presses a button that must not work.

func failedPath(requestID string) string {
	return operatorPayoutRequestsPath + "/" + requestID + "/failed"
}

func markFailed(t *testing.T, env *testEnv, sessionID, requestID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, failedPath(requestID), body, authHeader(sessionID))
}

func markFailedOK(t *testing.T, env *testEnv, sessionID, requestID string, body map[string]any) payoutRequest {
	t.Helper()
	resp, envBody := markFailed(t, env, sessionID, requestID, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark failed status=%d; want 200; error=%+v", resp.StatusCode, envBody.Error)
	}
	var request payoutRequest
	if err := json.Unmarshal(envBody.Data, &request); err != nil {
		t.Fatalf("decode failed request: %v", err)
	}
	return request
}

// TestOperatorMarksATransferFailedAndTheLedgerStaysEmpty is the invariant the
// `processing` state was built to make possible, asserted by COUNTING `payouts`
// rows across the whole of `pending → processing → failed`.
//
// A rejected transfer must leave NOTHING behind. Counting is the only assertion
// that can see it: a Payout written on submission and deleted on failure would
// leave every request-shaped read agreeing with this test and the books telling
// a story of money that moved and then un-moved. The count is zero at both
// steps, and the second zero is the one that matters — it says nothing had to be
// unwound because nothing was ever written.
//
// The reason travels with the failure and is what the organizer reads. It is not
// decoration: "failed" alone is unactionable, and the account number is the
// commonest cause, so the sentence is also the instruction.
func TestOperatorMarksATransferFailedAndTheLedgerStaysEmpty(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Bounce Fest", "bounce-fest", 5, 1)
	senderSessionID := operatorSession(t, env, "sender@example.com")
	// The operator who learns the outcome need not be the one who sent it, which
	// is why transfer_submitted_by and resolved_by are two columns.
	confirmerSessionID := operatorSession(t, env, "confirmer@example.com")

	markProcessingOK(t, env, senderSessionID, request.ID, map[string]any{"transfer_reference": "PP-BOUNCE"})
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows while the transfer was unconfirmed = %d; want none", count)
	}

	failed := markFailedOK(t, env, confirmerSessionID, request.ID,
		map[string]any{"reason": "The bank rejected the account number."})

	if failed.ID != request.ID || failed.Status != "failed" {
		t.Fatalf("failed request = %+v; want the same ask, failed", failed)
	}
	// THE ASSERTION. Counted, never inferred: nothing was written, so nothing had
	// to be deleted or negated.
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after a rejected transfer = %d; want none — the money never moved", count)
	}
	if failed.PayoutID != nil {
		t.Fatalf("payout_id on a failed request = %v; want null", failed.PayoutID)
	}

	// A failure IS a resolution: the request has ended, and the stamp says who
	// ended it and when — the same pair a decline writes, from the Staff Session
	// and the injected clock.
	if failed.ResolutionReason == nil || *failed.ResolutionReason != "The bank rejected the account number." {
		t.Fatalf("resolution_reason = %v; want the sentence the organizer reads", failed.ResolutionReason)
	}
	if failed.ResolvedBy == nil || *failed.ResolvedBy != "confirmer@example.com" {
		t.Fatalf("resolved_by = %v; want the acting operator's email, never a body value", failed.ResolvedBy)
	}
	if failed.ResolvedAt == nil || *failed.ResolvedAt == "" {
		t.Fatalf("resolved_at = %v; want the instant from the injected clock", failed.ResolvedAt)
	}

	// The record of the transfer that failed survives, so an operator asked
	// "which transfer bounced?" has the reference to give the provider.
	by, reference, at := transferStamp(t, env, request.ID)
	if by == nil || *by != "sender@example.com" || reference == nil || *reference != "PP-BOUNCE" || at == nil {
		t.Fatalf("transfer stamp after a failure = by %v ref %v at %v; want the submitter's own record kept", by, reference, at)
	}

	// It leaves the queue and the badge: a failure is an ANSWER, and the work is
	// no longer outstanding.
	if count := operatorPendingPayoutRequestCount(t, env, senderSessionID); count != 0 {
		t.Fatalf("pending count after a failure = %d; want 0 — the request has been answered", count)
	}
	queue := operatorQueue(t, env, senderSessionID, "")
	if len(queue.Data) != 0 {
		t.Fatalf("queue after a failure = %+v; want it empty", queue.Data)
	}

	// And the organizer reads it as a FAILURE rather than a refusal. The two are
	// different states and must not be collapsed anywhere, this history least of
	// all: a decline is a judgement a person made, and telling an organizer with
	// a typo that the platform refused them is the exact misreading the separate
	// state exists to prevent.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 1 || history[0].Status != "failed" {
		t.Fatalf("organization history = %+v; want the ask, failed and not declined", history)
	}
	if history[0].ResolutionReason == nil || *history[0].ResolutionReason != "The bank rejected the account number." {
		t.Fatalf("the organizer cannot read why it failed: %+v", history[0])
	}
	// No balance moved either. A rejected transfer changes nothing about what the
	// platform owes.
	if orgView := getPayouts(t, env, adminSessionID); len(orgView.Payouts) != 0 {
		t.Fatalf("organization payouts after a failure = %+v; want none", orgView.Payouts)
	}
}

// TestOperatorCannotFailARequestNobodySubmittedATransferFor is the guard the
// ticket is really about: the compare-and-swap is on `processing`, NOT `pending`.
//
// A request nobody submitted a transfer for cannot have had one bounce. Marking
// one `failed` would be an operator refusing an untouched ask in a word that
// blames the bank — which the organizer reads as "your account number is wrong"
// about an account nobody tried to pay, and which makes every failure figure
// count refusals as bank errors.
//
// The refusal is its own code and not the already-resolved one, because a
// `pending` request has not been resolved by anybody: that message would name
// "another operator" for an event that never happened, and tell the reader to
// record a Payout for money nobody sent. Widening the guard to `pending` — the
// single most plausible "simplification" of this transition — is what this test
// exists to fail.
func TestOperatorCannotFailARequestNobodySubmittedATransferFor(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Untouched Fest", "untouched-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	resp, body := markFailed(t, env, operatorSessionID, request.ID,
		map[string]any{"reason": "the account number is wrong"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_TRANSFER_NOT_SUBMITTED" {
		t.Fatalf("failing a pending request status=%d error=%+v; want 409 PAYOUT_REQUEST_TRANSFER_NOT_SUBMITTED",
			resp.StatusCode, body.Error)
	}
	// The operator is pointed at the button they actually wanted. An untouched
	// ask an operator means to refuse is DECLINED, with their name on it.
	if !strings.Contains(strings.ToLower(body.Error.Message), "decline") {
		t.Fatalf("refusal message = %q; want it to name the decline as the way out", body.Error.Message)
	}
	assertNoBankDetails(t, body.Error)

	// Nothing was written: not the status, not the reason, not a resolution.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 1 || history[0].Status != "pending" {
		t.Fatalf("request after a refused failure = %+v; want it untouched and pending", history)
	}
	if history[0].ResolutionReason != nil || history[0].ResolvedBy != nil || history[0].ResolvedAt != nil {
		t.Fatalf("a refused failure left a resolution behind: %+v", history[0])
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows = %d; want none", count)
	}

	// And it is still answerable in every way it was before — the refusal cost
	// the request nothing.
	markProcessingOK(t, env, operatorSessionID, request.ID, nil)
	markFailedOK(t, env, operatorSessionID, request.ID, map[string]any{"reason": "now it really bounced"})
}

// TestOperatorMarkingFailedRequiresAReason: a failure without a sentence is a
// dead end for the organizer, so the reason is required — in the handler, and
// under it in the schema (payout_requests_resolution_has_reason, migration 046).
//
// The refusal is a FIELD ERROR in the same shape the decline's is, because it is
// the same field, the same column and the same rule; the staff app renders one
// dialog for both. Missing, empty and whitespace-only are one failure and not
// three: all of them reach the organizer as a blank, which is the outcome the
// requirement exists to prevent.
func TestOperatorMarkingFailedRequiresAReason(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Blank Reason Fest", "blank-reason-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	markProcessingOK(t, env, operatorSessionID, request.ID, nil)

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{"missing", map[string]any{}, "REQUIRED"},
		{"empty", map[string]any{"reason": ""}, "REQUIRED"},
		{"whitespace", map[string]any{"reason": "   "}, "REQUIRED"},
		// The bound is the column's, and an over-long reason is a field error
		// naming the field rather than a constraint violation an operator cannot
		// act on.
		{"beyond the bound", map[string]any{"reason": strings.Repeat("x", 501)}, "TOO_LONG"},
	}
	for _, tc := range cases {
		resp, envBody := markFailed(t, env, operatorSessionID, request.ID, tc.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s reason status=%d; want 400; error=%+v", tc.name, resp.StatusCode, envBody.Error)
		}
		if envBody.Error == nil || envBody.Error.Code != "VALIDATION_FAILED" || !isNullData(envBody.Data) {
			t.Fatalf("%s reason envelope: data=%s error=%+v", tc.name, envBody.Data, envBody.Error)
		}
		// Read out of details exactly as the decline's own test reads it: the
		// shape is the assertion, because the staff app renders one dialog for
		// both answers and a divergence here is a dialog that stops showing the
		// message on one of them.
		details, _ := json.Marshal(envBody.Error.Details)
		var parsed struct {
			Fields []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(details, &parsed); err != nil {
			t.Fatalf("%s decode details: %v", tc.name, err)
		}
		if len(parsed.Fields) != 1 || parsed.Fields[0].Field != "reason" || parsed.Fields[0].Code != tc.code {
			t.Fatalf("%s fields = %+v; want one %s on `reason`", tc.name, parsed.Fields, tc.code)
		}
	}

	// And the rule is the DATABASE's, not the handler's. This UPDATE goes around
	// every validator on purpose: the service is one writer among several — a
	// console session, a later migration, a future job — and the difference
	// between an organizer reading a sentence and an organizer reading a blank
	// must not depend on which of them wrote the row
	// (payout_requests_resolution_has_reason, migration 046). The resolution
	// stamp is supplied so the only rule left to break is the reason's.
	if _, err := env.db.Exec(`
		UPDATE payout_requests
		SET status = 'failed', resolution_reason = NULL, resolved_by = 'sneaky@example.com', resolved_at = NOW()
		WHERE id = $1
	`, request.ID); err == nil {
		t.Fatal("the database accepted a failed request with no reason; the reason CHECK was not widened to cover `failed`")
	}

	// Every refusal left the request exactly where it was: still processing, still
	// answerable, and still with no Payout behind it.
	if history := listPayoutRequests(t, env, adminSessionID); history[0].Status != "processing" {
		t.Fatalf("request after refused failures = %+v; want it still processing", history[0])
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows = %d; want none", count)
	}
}

// TestFailedPayoutRequestIsTerminal: a failure is final in every direction.
//
// It cannot be fulfilled, declined, cancelled, marked processing, or failed a
// second time. That is not tidiness — reopening a failed request would have an
// operator retrying against the SAME BAD ACCOUNT NUMBER forever, because the
// bank details on a request are a frozen snapshot and a request cannot be
// edited. The organizer's route out is a fresh ask against a corrected Payout
// Profile, which the test below this one holds.
//
// The fulfilment attempt is the one worth counting the ledger after: it is the
// only one of the five that would write a `payouts` row if the compare-and-swap
// let it through, and the guard's rollback is what keeps a rejected transfer
// from becoming a settlement.
func TestFailedPayoutRequestIsTerminal(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Terminal Fest", "terminal-fest", 5, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	otherSessionID := operatorSession(t, env, "other@example.com")

	markProcessingOK(t, env, operatorSessionID, request.ID, map[string]any{"transfer_reference": "PP-TERM"})
	markFailedOK(t, env, operatorSessionID, request.ID, map[string]any{"reason": "wrong account number"})

	// Fulfilment. The one that could otherwise put money in the ledger.
	resp, body := fulfilPayoutRequest(t, env, otherSessionID, request.ID, fulfilBody(payable, "2026-07-22", ""))
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" {
		t.Fatalf("fulfilling a failed request status=%d error=%+v; want 409 PAYOUT_REQUEST_ALREADY_RESOLVED",
			resp.StatusCode, body.Error)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after fulfilling a failed request = %d; want none — the CAS rolled the INSERT back", count)
	}

	// Decline. A failure is not a refusal and cannot be turned into one after the
	// fact; the state that reached the organizer stays the one that happened.
	resp, body = declinePayoutRequest(t, env, otherSessionID, request.ID, map[string]any{"reason": "on second thoughts"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" {
		t.Fatalf("declining a failed request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}

	// Marked processing again. This is the "retry" door, and it is shut: a
	// re-submitted transfer against a frozen snapshot aims at the same bad
	// account.
	resp, body = markProcessing(t, env, otherSessionID, request.ID, map[string]any{"transfer_reference": "PP-RETRY"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" {
		t.Fatalf("re-processing a failed request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}

	// Failed a second time. The refusal names the state it is already in rather
	// than silently overwriting the first reason with a later one.
	resp, body = markFailed(t, env, otherSessionID, request.ID, map[string]any{"reason": "it bounced again"})
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_ALREADY_RESOLVED" {
		t.Fatalf("failing a failed request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if !strings.Contains(body.Error.Message, "failed") {
		t.Fatalf("refusal message = %q; want it to name the state the request is in", body.Error.Message)
	}

	// Cancelled by the Organization. All four end states are final, and this one
	// is no exception just because nothing was paid.
	resp, body = cancelPayoutRequest(t, env, adminSessionID, request.ID)
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_NOT_PENDING" {
		t.Fatalf("cancelling a failed request status=%d error=%+v; want 409 PAYOUT_REQUEST_NOT_PENDING",
			resp.StatusCode, body.Error)
	}

	// Through all five, the record is exactly what the first failure wrote.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 1 || history[0].Status != "failed" ||
		history[0].ResolutionReason == nil || *history[0].ResolutionReason != "wrong account number" {
		t.Fatalf("failed request after five refused answers = %+v; want the original failure intact", history)
	}
	if history[0].ResolvedBy == nil || *history[0].ResolvedBy != "operator@example.com" {
		t.Fatalf("resolved_by = %v; want whoever recorded the failure, not whoever tried afterwards", history[0].ResolvedBy)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows after five refused answers = %d; want none", count)
	}
}
