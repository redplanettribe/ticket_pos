package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Recording a Payout DIRECTLY while the Organization is asking to be paid
// (#178, ADR 0026).
//
// The operator surface has two doors onto the same ledger row. One answers a
// request in the queue; the other answers a WhatsApp thread from the
// Organization detail page. The failure this ticket defends against is paying an
// Organization TWICE — the most expensive mistake the feature can make and the
// hardest to undo — and the defence is a warning on the direct form, which is a
// frontend decision covered by a pure helper (apps/staff/lib/payouts.ts).
//
// What belongs at THIS seam is the pair of facts the warning's wording depends
// on, both of which are properties of the endpoint rather than of the form:
//
//   - THE DIRECT PATH NEVER BLOCKS. Blocking direct Payouts while a request is
//     pending is an option ADR 0026 considered and rejected: the bank transfer
//     happened, and a system that refuses to write it down has not prevented
//     anything, it has only stopped knowing. The unconditional posture ADR 0019
//     depends on is untouched, over-balance amounts included.
//
//   - THE DIRECT PATH NEVER AUTO-CLOSES. Silently marking a pending request paid
//     because some Payout was recorded is the other rejected option: a $200
//     direct Payout and a $900 outstanding request are probably not the same
//     event, and an unrelated settlement would become a fulfilment nobody agreed
//     to. So the request stays `pending`, in the queue, on the badge, and
//     answerable — and the operator dashboard keeps warning about it.
//
// Both are assertions about what did NOT happen, which is exactly the kind that
// rots silently unless something holds them.

// directPayout records a Payout the way the Organization detail form does:
// straight at the Organization, naming no request.
func directPayout(t *testing.T, env *testEnv, sessionID, orgID string, amountCents int, paidAt, note string) (*http.Response, envelope) {
	t.Helper()
	body := map[string]any{"amount_cents": amountCents, "paid_at": paidAt}
	if note != "" {
		body["note"] = note
	}
	return env.post(t, "/api/v1/operator/organizations/"+orgID+"/payouts", body, authHeader(sessionID))
}

func directPayoutOK(t *testing.T, env *testEnv, sessionID, orgID string, amountCents int, paidAt, note string) operatorPayout {
	t.Helper()
	resp, body := directPayout(t, env, sessionID, orgID, amountCents, paidAt, note)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("direct payout status=%d; want 201; error=%+v", resp.StatusCode, body.Error)
	}
	var payout operatorPayout
	if err := json.Unmarshal(body.Data, &payout); err != nil {
		t.Fatalf("decode direct payout: %v", err)
	}
	return payout
}

// TestOperatorDirectPayoutLeavesTheOutstandingRequestPending: an operator
// settles part of what was asked for through the direct form, and the ask is
// exactly where it was.
//
// The Payout is smaller than the request on purpose. It is the case that makes
// auto-closing indefensible: nothing in the system knows whether this transfer
// was the answer to that ask, a partial one, or an unrelated settlement, and
// guessing would turn one into another (ADR 0026). A human decides, on the
// request itself, and until they do the request is still owed an answer.
func TestOperatorDirectPayoutLeavesTheOutstandingRequestPending(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Twice Fest", "twice-fest", 9, 1)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// A third of the ask: plainly not the same event.
	part := payable / 3
	payout := directPayoutOK(t, env, operatorSessionID, orgID, part, "2026-07-20", "advance, not the request")
	if payout.ID == "" || payout.AmountCents != part {
		t.Fatalf("direct payout = %+v; want the amount recorded as stated", payout)
	}

	var detail operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)

	// The money moved and the books know it.
	if len(detail.Payouts) != 1 || detail.Payouts[0].ID != payout.ID {
		t.Fatalf("payout history = %+v; want the direct Payout alone", detail.Payouts)
	}
	if want := payable - part; detail.WithdrawableBalanceCents != want {
		t.Fatalf("balance = %d; want %d — the direct Payout subtracted", detail.WithdrawableBalanceCents, want)
	}

	// THE ASSERTION. The ask is untouched, in every field that would record an
	// answer.
	if len(detail.PayoutRequests) != 1 || detail.PayoutRequests[0].ID != request.ID {
		t.Fatalf("requests = %+v; want the one ask, still there", detail.PayoutRequests)
	}
	still := detail.PayoutRequests[0]
	if still.Status != "pending" {
		t.Fatalf("request status after a direct Payout = %q; want it still pending — nothing is auto-closed", still.Status)
	}
	if still.ResolvedBy != nil || still.ResolvedAt != nil || still.PayoutID != nil || still.ResolutionReason != nil {
		t.Fatalf("request answer fields = %+v; want all null on an unanswered ask", still)
	}
	if still.AmountCents != payable {
		t.Fatalf("request amount = %d; want the figure asked for, %d — a Payout does not edit an ask",
			still.AmountCents, payable)
	}

	// And it is still work: on the badge the operator reads, and in the queue
	// they work down. This is what the warning on the direct form is warning
	// about — a colleague can still fulfil this.
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count after a direct Payout = %d; want 1", count)
	}
	queue := operatorQueue(t, env, operatorSessionID, "")
	if len(queue.Data) != 1 || queue.Data[0].Request.ID != request.ID {
		t.Fatalf("queue after a direct Payout = %+v; want the ask still waiting", queue.Data)
	}

	// The Organization's own view agrees: it is still waiting, and may not ask
	// again, because its one outstanding slot is still taken.
	orgRequests := listPayoutRequests(t, env, adminSessionID)
	if len(orgRequests) != 1 || orgRequests[0].Status != "pending" {
		t.Fatalf("organization's own view = %+v; want its ask still outstanding", orgRequests)
	}

	// Fulfilling still works afterwards, and records a SECOND Payout. The direct
	// one consumed nothing.
	fulfilled := fulfilPayoutRequestOK(t, env, operatorSessionID, request.ID,
		fulfilBody(payable-part, "2026-07-21", "the rest, answering the request"))
	if fulfilled.Request.Status != "paid" {
		t.Fatalf("request after fulfilment = %+v; want paid", fulfilled.Request)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 2 {
		t.Fatalf("payout rows = %d; want both the direct settlement and the fulfilment", count)
	}
}

// TestOperatorDirectPayoutStaysUnconditionalWhileARequestIsPending: the
// endpoint's behaviour is otherwise UNCHANGED, and the pending request changes
// none of it.
//
// A Payout over the Withdrawable Balance was always accepted (ADR 0015) — by
// recording time the money has already left the bank — and an outstanding
// request is not a new reason to start refusing. Nor does recording twice in a
// row hit anything: the second direct Payout is as ordinary as the first, and
// the ask survives both.
//
// The counterpart is asserted at the end: an Organization with NO outstanding
// request is served by exactly the same endpoint in exactly the same way, which
// is what makes "no warning at all" the right answer on that page.
func TestOperatorDirectPayoutStaysUnconditionalWhileARequestIsPending(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Over Fest", "over-fest", 5, 1)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// Over the whole balance, with a request outstanding: the two conditions the
	// form warns about at once, and the API objects to neither.
	over := payable + 50_000
	if payout := directPayoutOK(t, env, operatorSessionID, orgID, over, "2026-07-20", "wired in error, being recovered"); payout.AmountCents != over {
		t.Fatalf("over-balance direct payout = %+v; want it recorded as stated", payout)
	}

	var detail operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)
	if detail.WithdrawableBalanceCents != payable-over {
		t.Fatalf("balance = %d; want the negative %d the over-balance Payout leaves",
			detail.WithdrawableBalanceCents, payable-over)
	}
	if detail.PayoutRequests[0].Status != "pending" {
		t.Fatalf("request status = %q; want pending after an over-balance direct Payout",
			detail.PayoutRequests[0].Status)
	}

	// A second one, on top of a balance that is already negative. Still nothing
	// to argue with, and still no answer to the ask.
	directPayoutOK(t, env, operatorSessionID, orgID, 1_000, "2026-07-21", "")
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)
	if len(detail.Payouts) != 2 || detail.PayoutRequests[0].Status != "pending" {
		t.Fatalf("after two direct Payouts: %d recorded, request %q; want 2 and pending",
			len(detail.Payouts), detail.PayoutRequests[0].Status)
	}
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("pending count = %d; want the ask still waiting after two direct Payouts", count)
	}
	if detail.PayoutRequests[0].ID != request.ID {
		t.Fatalf("request id = %s; want the original ask", detail.PayoutRequests[0].ID)
	}

	// The quiet case: an Organization that has never asked for anything. The
	// same endpoint, the same result, and a detail payload with no request in it
	// — which is what the Organization detail page reads to decide it has
	// nothing to warn about.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	otherID := operatorOrgIDBySlug(t, env, "other-org")

	directPayoutOK(t, env, operatorSessionID, otherID, 2_500, "2026-07-21", "")
	var other operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+otherID, &other)
	if len(other.Payouts) != 1 || other.Payouts[0].AmountCents != 2_500 {
		t.Fatalf("payouts for an Organization that never asked = %+v; want the one just recorded", other.Payouts)
	}
	if len(other.PayoutRequests) != 0 {
		t.Fatalf("requests for an Organization that never asked = %+v; want none", other.PayoutRequests)
	}
}

// TestOperatorDirectPayoutWarnsWhileATransferIsProcessing: the warning's one
// input is still there when the request's transfer is already in flight (#186,
// ADR 0026 amendment).
//
// This is the case where double-paying stops being theoretical. A `pending`
// request is a colleague who MIGHT transfer; a `processing` one is a colleague
// who ALREADY DID, and the money may be hours from landing. If the Organization
// detail payload dropped that request — or if it came back in a status the
// client's outstanding predicate did not recognise — the operator would record a
// second transfer with nothing on the screen to stop them.
//
// The warning itself is copy, decided by outstandingPayoutRequest in
// apps/staff/lib/payouts.ts and tested beside it. What is asserted here is the
// fact the copy hangs off: the request is IN the payload, carrying `processing`,
// with its transfer readable. The endpoint still refuses nothing and still
// closes nothing, which is unchanged and deliberate.
func TestOperatorDirectPayoutWarnsWhileATransferIsProcessing(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Flight Fest", "flight-fest", 6, 1)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "sender@example.com")

	markProcessingOK(t, env, operatorSessionID, request.ID, map[string]any{"transfer_reference": "PP-2026-0178"})

	var before operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &before)
	if len(before.PayoutRequests) != 1 || before.PayoutRequests[0].ID != request.ID {
		t.Fatalf("requests on the detail page = %+v; want the in-flight ask", before.PayoutRequests)
	}
	// THE ASSERTION the warning depends on: the ask is on the page, and it says
	// what state it is in.
	if before.PayoutRequests[0].Status != "processing" {
		t.Fatalf("status = %q; want processing — this is what the direct form warns about",
			before.PayoutRequests[0].Status)
	}
	if before.PayoutRequests[0].TransferSubmittedBy == nil {
		t.Fatalf("the in-flight ask carries no transfer: %+v", before.PayoutRequests[0])
	}

	// The direct path is unchanged: it records what the operator says moved, and
	// argues with nothing. Blocking it would not have prevented a second
	// transfer, only stopped knowing about it (ADR 0026).
	part := payable / 3
	payout := directPayoutOK(t, env, operatorSessionID, orgID, part, "2026-07-22", "unrelated advance")
	if payout.AmountCents != part {
		t.Fatalf("direct payout while a transfer is in flight = %+v; want it recorded as stated", payout)
	}

	var after operatorOrganizationDetail
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &after)
	// And it auto-closes nothing. A direct Payout is not an answer to an ask
	// whose transfer somebody else already submitted, and guessing that it was
	// would turn one event into another.
	if after.PayoutRequests[0].Status != "processing" || after.PayoutRequests[0].PayoutID != nil {
		t.Fatalf("the ask after a direct Payout = %+v; want it still processing and still unanswered", after.PayoutRequests[0])
	}
	if count := operatorPendingPayoutRequestCount(t, env, operatorSessionID); count != 1 {
		t.Fatalf("badge count = %d; want the in-flight ask still counted as work", count)
	}
	// The ledger holds ONLY the direct Payout. Marking a transfer processing
	// wrote nothing, and that is what a rejection would leave nothing to unwind.
	if count := payoutRowCount(t, env, "test-org"); count != 1 {
		t.Fatalf("payout rows = %d; want only the direct settlement", count)
	}
}
