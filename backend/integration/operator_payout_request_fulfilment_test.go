package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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
