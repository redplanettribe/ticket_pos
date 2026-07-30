package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Reversal Reconciler: the platform pursuing a stuck Reversal Request with
// nobody watching (issue #158, ADR 0024).
//
// Everything the Customer Area drain proves is proved with a buyer on the page.
// That is enough for a buyer who stays and useless for one who closes the tab —
// which is exactly the case a slow PayPhone produces, and exactly the case the
// feature exists for. What is driven below is the internal endpoint alone: no
// Area load, no press, no browser. If a test here passes because a Customer
// happened to look at their purchases, it is proving somebody else's property.
//
// Time moves by moving the clock, never by sleeping, and every run is one HTTP
// call — there is no goroutine and no ticker anywhere in this feature, which is
// the deployment reason ADR 0024 gives for the endpoint existing at all.

// drainReversals runs one Reversal Reconciler tick against the app wired to the
// fake PayPhone server, and asserts only that the endpoint answered.
//
// It takes no credential. The endpoint is authenticated by Cloud Run IAM before
// the request reaches the API (ADR 0008), which is infrastructure this suite
// does not run — so what is exercised here is the behaviour behind that gate,
// and the gate itself is the deployment's to prove.
func drainReversals(t *testing.T, env *testEnv) reversalDrainResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/reversals/drain", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out reversalDrainResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode drain result: %v", err)
	}
	return out
}

// reversalDrainResult is the endpoint's summary: what the run did, and what is
// still stuck once it had done it.
type reversalDrainResult struct {
	Pursued                   int                     `json:"pursued"`
	Reversed                  int                     `json:"reversed"`
	Refused                   int                     `json:"refused"`
	StillInFlight             int                     `json:"still_in_flight"`
	GaveUp                    int                     `json:"gave_up"`
	Skipped                   int                     `json:"skipped"`
	Failed                    int                     `json:"failed"`
	InFlightTotal             int                     `json:"in_flight_total"`
	OldestInFlightRequestedAt string                  `json:"oldest_in_flight_requested_at"`
	Unresolved                []unresolvedReversalRow `json:"unresolved"`
	UnresolvedTotal           int                     `json:"unresolved_total"`
}

type unresolvedReversalRow struct {
	TicketSaleID        string `json:"ticket_sale_id"`
	ConfirmationRef     string `json:"confirmation_ref"`
	ClientTransactionID string `json:"client_transaction_id"`
	RequestedAt         string `json:"requested_at"`
	AttemptCount        int    `json:"attempt_count"`
	LastError           string `json:"last_error"`
	SaleStatus          string `json:"sale_status"`
}

// stuckReversal stages the situation the Reconciler exists for: a paid Online
// Sale whose buyer pressed Undo and whose PayPhone said nothing at all.
//
// The press is a real press through the real endpoint, so what is left behind is
// a Reversal Request the product wrote rather than a row a test invented. The
// caller must have put the stub into an unknown-answer mood first; the returned
// client transaction id is what the Reconciler must present to PayPhone at every
// later probe, and what an operator would type into its dashboard.
func stuckReversal(t *testing.T, eventSlug, ticketTypeID, email string, quantity int) (ref, clientTransactionID, saleID string) {
	t.Helper()
	ref, clientTransactionID = buyOnlineThroughPayPhone(t, eventSlug, ticketTypeID, email, quantity)
	token := customerSignIn(t, payphoneEnv, email)
	saleID = saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref).ID
	reverseSalePending(t, payphoneEnv, token, saleID)
	return ref, clientTransactionID, saleID
}

// TestTheReconcilerResolvesARequestMadeInsideTheWindowAfterItCloses is the rule
// most likely to be tidied away by a future reader, and the one that costs the
// most when it is: the Reconciler only ever finds out, and never re-decides.
//
// The buyer presses at 19:58, two minutes before the Ecuadorian cutoff, and
// PayPhone goes silent. At 20:05 the Window has closed and nobody is looking —
// no Area load, no second press, only the tick. It must still resolve. A
// Reconciler that re-checked eligibility would refuse its own continuation seven
// minutes later and abandon a reversal PayPhone may already have carried out,
// with the sale still active and the money nowhere either party can see.
//
// The Customer Area proves the same rule for the drain a page load drives
// (customer_sale_reversal_pending_test.go); this proves it for the path where
// there is no Customer at all, which is the path where nothing else would notice.
//
// The untouched control sale is what stops it passing vacuously: it proves the
// Window really has closed by 20:05, so the resolution below is the rule being
// exercised rather than the cutoff being somewhere else.
func TestTheReconcilerResolvesARequestMadeInsideTheWindowAfterItCloses(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Tick Late Fest", "tick-late-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	// The control belongs to a DIFFERENT buyer, and that is not cosmetic. Reading
	// a Customer Area drains that Customer's own due Reversal Requests, so a
	// control read as the same buyer would resolve the request through the page
	// load and leave the tick below nothing to do — this test would pass while
	// proving the opposite of its name.
	controlRef, _ := buyOnlineThroughPayPhone(t, "tick-late-fest", gaID, "bea@example.com", 1)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	// 19:58 Ecuador: inside the Window, and PayPhone answers nothing.
	atReversalClock(t, ecuadorOnTheDayOfPurchase(t, 19, 58))
	payphoneStub.reverseHangsUp()
	ref, _, _ := stuckReversal(t, "tick-late-fest", gaID, "ana@example.com", 2)
	if request := theOnlyReversalRequest(t, env, ref); request.status != "in_flight" {
		t.Fatalf("Reversal Request after the press = %+v, want in_flight", request)
	}

	// 20:05 Ecuador: past the cutoff, PayPhone answering again, and the buyer
	// nowhere — nobody loads ana's Area between here and the tick. The control,
	// bought the same day and never asked about, is no longer reversible, which is
	// this test's proof the deadline has passed.
	atReversalClock(t, ecuadorOnTheDayOfPurchase(t, 20, 5))
	payphoneStub.reset()
	beaToken := customerSignIn(t, payphoneEnv, "bea@example.com")
	if control := saleByRef(t, readCustomerArea(t, payphoneEnv, beaToken, ""), controlRef); control.Reversible {
		t.Fatalf("control sale = %+v at 20:05 Ecuador, want the Reversal Window closed — the rest of this test proves nothing otherwise", control)
	}

	result := drainReversals(t, payphoneEnv)
	if result.Pursued != 1 || result.Reversed != 1 {
		t.Fatalf("drain = %+v, want one request pursued and reversed — an ask made inside the Window stays authorised however long the answer takes (ADR 0024)", result)
	}

	// The ordinary aftermath of a reversal, reached with nobody watching:
	// provenance naming the Customer as the actor, capacity back, one void notice.
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" || !reversedAt.Valid || reversedBy.String != "customer" {
		t.Fatalf("provenance = %s/%+v/%+v, want reversed by 'customer' — the Reconciler continues the buyer's ask, it does not make one of its own", status, reversedAt, reversedBy)
	}
	if got := remaining(t, env, "tick-late-fest", "GA"); got != 9 {
		t.Fatalf("remaining = %d, want 9 — the two reversed tickets are back and the control sale's one is not", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one quoting %q", voided, ref)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "succeeded" {
		t.Fatalf("Reversal Request = %+v, want succeeded", request)
	}
	if got := payphoneStub.reverseSuccessCount(); got != 1 {
		t.Fatalf("PayPhone carried out %d reversals, want exactly 1 — the buyer is refunded once however many times we ask", got)
	}
}

// TestTheReconcilerGivesUpAfterADayAndSaysNothingToTheCustomer: PayPhone is
// never well again, and after a day the platform stops asking.
//
// What it records is an Unresolved Reversal, and the three things it does NOT do
// are the decision. The Ticket Sale stays ACTIVE with its capacity held, because
// giving up is not learning the money came back — it is learning nothing. No
// email is sent, because there is no true thing to say: "your refund failed" may
// be false and "your refund is done" may be false, and the platform cannot tell
// which. And the request is never pursued again, which is the whole point of a
// bound: a permanently unwell provider must produce a queue somebody can read
// rather than a row retrying forever.
//
// What it leaves instead is the queue, carrying the client transaction id and
// the last error — the two things that find the transaction on PayPhone's own
// dashboard, which is the only place that can say what actually happened.
func TestTheReconcilerGivesUpAfterADayAndSaysNothingToTheCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Never Answers Fest", "never-answers-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	ref, clientTransactionID, saleID := stuckReversal(t, "never-answers-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// A day after the buyer pressed, with PayPhone still saying nothing. The
	// request gets one last probe — it might have been the one that answered —
	// and it is that answer's silence that ends the pursuit.
	atReversalClock(t, fixedClock.Add(sales.ReversalGiveUpAfter))
	result := drainReversals(t, payphoneEnv)
	if result.Pursued != 1 || result.GaveUp != 1 {
		t.Fatalf("drain = %+v, want the one stuck request pursued and given up on", result)
	}

	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "needs_attention" {
		t.Fatalf("Reversal Request after a day of silence = %+v, want needs_attention — an Unresolved Reversal, awaiting a Platform Operator", request)
	}
	if !request.lastError.Valid || request.lastError.String == "" {
		t.Fatal("the Unresolved Reversal records no last error; whoever reads the queue during the incident has nothing to go on")
	}

	// The sale is untouched, and that is not an oversight. No money is known to
	// have moved, so the tickets are still valid and the capacity is still held.
	assertNothingChanged(t, env, ref, "never-answers-fest", "GA", 8)
	if got := env.email.Voided(); len(got) != 0 {
		t.Fatalf("captured %d void notices for an Unresolved Reversal, want none", len(got))
	}
	if got := env.email.Confirmations(); len(got) != 0 {
		t.Fatalf("captured %d emails to the Customer about an Unresolved Reversal, want none — the platform does not know what happened, and any message would be a guess about their money", len(got))
	}
	// Not even the refused-reversal notice (#161), and this is the closest call
	// in the whole feature: this buyer WAS told their refund was being processed,
	// so the argument for correcting the promise applies to them too. It loses to
	// the fact that the correction would be a claim — "we could not undo your
	// purchase" may be flatly false, since PayPhone may well have reversed the
	// payment and never said so.
	assertNoRefusedNotice(t, env, "the platform never learned whether the refund happened, so it has nothing true to tell them")

	// The queue, as the endpoint hands it to whoever curled it: enough to find
	// the transaction on PayPhone's dashboard without opening a database session.
	if result.UnresolvedTotal != 1 || len(result.Unresolved) != 1 {
		t.Fatalf("drain = %+v, want an Unresolved Reversal queue of exactly one", result)
	}
	queued := result.Unresolved[0]
	if queued.ClientTransactionID != clientTransactionID {
		t.Fatalf("queued Unresolved Reversal = %+v, want the Payment's client transaction id %q — it is what finds the reversal on the provider's dashboard", queued, clientTransactionID)
	}
	if queued.ConfirmationRef != ref || queued.TicketSaleID != saleID {
		t.Fatalf("queued Unresolved Reversal = %+v, want the buyer's own sale %q under reference %q", queued, saleID, ref)
	}
	if queued.LastError == "" || queued.SaleStatus != "active" {
		t.Fatalf("queued Unresolved Reversal = %+v, want the provider's last words and a sale that still stands", queued)
	}

	// And it is over. A later tick finds nothing to pursue however long PayPhone
	// stays unwell, and PayPhone is not asked again about money the platform has
	// admitted it cannot account for.
	asked := payphoneStub.reverseCount()
	atReversalClock(t, fixedClock.Add(sales.ReversalGiveUpAfter+12*time.Hour))
	later := drainReversals(t, payphoneEnv)
	if later.Pursued != 0 {
		t.Fatalf("a later drain = %+v, want nothing pursued — an Unresolved Reversal is never asked about again", later)
	}
	if got := payphoneStub.reverseCount(); got != asked {
		t.Fatalf("PayPhone was asked to reverse %d times after the platform gave up, want it left at %d", got, asked)
	}
	if later.UnresolvedTotal != 1 {
		t.Fatalf("a later drain = %+v, want the Unresolved Reversal still queued for an operator", later)
	}
}

// TestTheReconcilerHonoursTheBackoff: a Reversal Request is not re-probed before
// its next_attempt_at, however often the endpoint is called.
//
// The tick is a schedule and a runbook entry point at once, so a drain that
// ignored the backoff would let anybody — a scheduler misconfigured to the
// minute, an operator curling twice — turn the platform into a load generator
// aimed at a provider that has already failed to answer. The bound below is the
// schedule's own first step (ADR 0024: ten seconds, plus jitter that only ever
// delays), so a drain five seconds after the press must find nothing due and one
// a minute later must find exactly one probe's worth of work.
func TestTheReconcilerHonoursTheBackoff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Paced Fest", "paced-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	ref, _, _ := stuckReversal(t, "paced-fest", gaID, "ana@example.com", 2)
	if got := payphoneStub.reverseCount(); got != 1 {
		t.Fatalf("PayPhone was asked to reverse %d times by the press, want 1", got)
	}

	// The instant of the press, and five seconds later. Neither is due: the first
	// step of the backoff is ten seconds, and nothing may bring a probe forward.
	for _, at := range []time.Time{fixedClock, fixedClock.Add(5 * time.Second)} {
		atReversalClock(t, at)
		if result := drainReversals(t, payphoneEnv); result.Pursued != 0 {
			t.Fatalf("drain %v after the press = %+v, want nothing pursued — the request is not due yet", at.Sub(fixedClock), result)
		}
	}
	if got := payphoneStub.reverseCount(); got != 1 {
		t.Fatalf("PayPhone was asked to reverse %d times across two early ticks, want 1 — only the press was due", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.attempts != 1 {
		t.Fatalf("Reversal Request = %+v, want a single attempt — a tick that finds nothing due is not an attempt", request)
	}

	// A minute later the request has come due. The backoff paces the Reconciler;
	// it does not switch it off.
	atReversalClock(t, fixedClock.Add(time.Minute))
	if result := drainReversals(t, payphoneEnv); result.Pursued != 1 || result.StillInFlight != 1 {
		t.Fatalf("drain a minute after the press = %+v, want the request pursued and still unanswered", result)
	}
	if got := payphoneStub.reverseCount(); got != 2 {
		t.Fatalf("PayPhone was asked to reverse %d times once the request came due, want 2", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.attempts != 2 || request.status != "in_flight" {
		t.Fatalf("Reversal Request = %+v, want 2 attempts and the question still open", request)
	}

	// And the second unknown answer buys a longer wait than the first: an
	// immediate re-tick finds nothing due, which is the schedule growing.
	if result := drainReversals(t, payphoneEnv); result.Pursued != 0 {
		t.Fatalf("an immediate second tick = %+v, want nothing pursued — the wait grows with each unknown answer", result)
	}
}

// TestDrainingAnEmptyQueueDoesNothing: the endpoint is safe to call by hand, at
// any time, whether or not anything is stuck.
//
// That is a requirement rather than a nicety. The endpoint is the incident
// runbook — the first thing somebody curls when a buyer says their refund never
// arrived — and it ships before the schedule that will call it (#159), so its
// answer on a quiet system must be a plain, boring nothing rather than an error
// somebody has to interpret at three in the morning.
func TestDrainingAnEmptyQueueDoesNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Quiet Fest", "quiet-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	// A perfectly ordinary purchase that nobody has asked to undo. The queue is
	// empty because there is nothing in it, not because there is nothing at all.
	ref, _ := buyOnlineThroughPayPhone(t, "quiet-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	for range 3 {
		result := drainReversals(t, payphoneEnv)
		if result.Pursued != 0 || result.Reversed != 0 || result.Refused != 0 ||
			result.StillInFlight != 0 || result.GaveUp != 0 || result.Skipped != 0 || result.Failed != 0 {
			t.Fatalf("drain against an empty queue = %+v, want a summary of nothing at all", result)
		}
		if result.UnresolvedTotal != 0 || len(result.Unresolved) != 0 {
			t.Fatalf("drain against an empty queue = %+v, want no Unresolved Reversals either", result)
		}
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times by a drain with nothing to drain, want 0", got)
	}
	assertNothingChanged(t, env, ref, "quiet-fest", "GA", 8)
}

// TestTheReconcilerStopsAtItsBatchBound: one run works a bounded batch and
// leaves the rest exactly as due as it found them.
//
// A backlog must never be able to wedge the endpoint. Every probe can cost the
// full ten-second provider timeout, so an unbounded run against a queue built up
// during an outage would be killed by Cloud Run mid-probe — losing the outcome
// of a call that may have moved somebody's money — and the tick after it would
// start the same doomed sweep again.
//
// The bound is narrowed for the test because boundedness is a property of the
// loop rather than of the number: proving it at the deployed fifty would mean
// staging fifty stuck purchases, and a test that slow is a test that gets
// deleted.
func TestTheReconcilerStopsAtItsBatchBound(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Backlog Fest", "backlog-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 30)

	payphoneApp.SalesService.WithReversalDrainBatch(2)
	t.Cleanup(func() { payphoneApp.SalesService.WithReversalDrainBatch(0) })

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	for _, buyer := range []string{"ana@example.com", "bea@example.com", "caro@example.com"} {
		stuckReversal(t, "backlog-fest", gaID, buyer, 1)
	}
	env.email.Reset()

	// All three are due, and the first run takes two of them.
	atReversalClock(t, fixedClock.Add(time.Minute))
	first := drainReversals(t, payphoneEnv)
	if first.Pursued != 2 {
		t.Fatalf("first drain = %+v, want exactly the bounded 2 of the 3 stuck requests", first)
	}
	if got := payphoneStub.reverseCount(); got != 5 {
		t.Fatalf("PayPhone was asked to reverse %d times, want 5 — three presses and the two the bounded run pursued", got)
	}

	// The third was not lost, delayed or claimed: the next run finds it still due
	// and works it, which is what makes a bound a pause rather than a ceiling on
	// how much backlog the platform can clear.
	second := drainReversals(t, payphoneEnv)
	if second.Pursued != 1 {
		t.Fatalf("second drain = %+v, want the one request the bound left behind", second)
	}
	if third := drainReversals(t, payphoneEnv); third.Pursued != 0 {
		t.Fatalf("third drain = %+v, want nothing left due", third)
	}
	if got := payphoneStub.reverseCount(); got != 6 {
		t.Fatalf("PayPhone was asked to reverse %d times in all, want 6 — one probe each, never a request pursued twice in a run", got)
	}
}

// TestTheReconcilerNeverAsksAboutASaleReversedOutOfBand is the double refund
// this feature could otherwise cause at scale, on the path where nobody would
// see it happen.
//
// The Customer Area drain already refuses to probe a sale somebody else settled
// (customer_sale_reversal_pending_test.go). The Reconciler runs unattended,
// unscoped and on every stuck request there is, so the same guard failing here
// would refund buyers twice in a loop rather than once on a page load. An
// Operator Reversal (ADR 0019) may have been made through PayPhone's own
// dashboard, where a probe would answer errorCode 24 and cost nothing — or by
// BANK TRANSFER, where PayPhone's transaction is still live and the same probe
// would reverse it for real. Nothing distinguishes the two beforehand, so the
// platform does not ask.
func TestTheReconcilerNeverAsksAboutASaleReversedOutOfBand(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Settled Elsewhere Fest", "settled-elsewhere-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	ref, _, _ := stuckReversal(t, "settled-elsewhere-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// The operator refunds by bank transfer and records it. The sale is reversed
	// and PayPhone was never involved.
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)),
		PlatformFeeKept:     boolPtr(false),
		Note:                strPtr("refunded by bank transfer"),
	})

	// PayPhone is answering perfectly again, so a probe would succeed — which is
	// precisely what makes this the assertion. A reversal carried out here is a
	// buyer paid twice.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reset()
	result := drainReversals(t, payphoneEnv)
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times for a sale somebody else had already reversed, want 0 — the buyer may already have their money and asking would give it to them again", got)
	}
	if result.Pursued != 1 || result.GaveUp != 1 {
		t.Fatalf("drain = %+v, want the request pursued and recorded as unresolved rather than probed", result)
	}

	// And the ask is recorded as what it is: nobody here knows what became of the
	// platform's own in-flight question, and only PayPhone's dashboard can say.
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "needs_attention" {
		t.Fatalf("Reversal Request = %+v, want needs_attention", request)
	}
	if result.UnresolvedTotal != 1 || len(result.Unresolved) != 1 || result.Unresolved[0].SaleStatus != "reversed" {
		t.Fatalf("drain = %+v, want the Unresolved Reversal queued over a sale another hand reversed", result)
	}

	// It stays given up on: a later tick must not quietly resume asking.
	atReversalClock(t, fixedClock.Add(30*time.Minute))
	if later := drainReversals(t, payphoneEnv); later.Pursued != 0 {
		t.Fatalf("a later drain = %+v, want nothing pursued", later)
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times on an Unresolved Reversal, want 0", got)
	}
}

// holdTheSaleReversalLock takes the per-sale advisory lock from outside the
// application, so a drain meets a sale somebody else is already probing.
//
// It is the only honest way to stage contention here. The real holder is a
// buyer's press or a Customer Area load somewhere inside a ten-second provider
// call, and reproducing that means either a second HTTP request timed to land
// inside another one or a fake that blocks — both of which make the test about
// scheduling rather than about what contention costs. The lock is a name two
// sessions agree not to hold at once; taking it in SQL is taking the same name.
//
// The key is repository.ticketSaleReversalLockClass and hashtext of the sale id,
// written out because it is unexported. A drift between the two is a test that
// silently stops staging anything, which is why the caller below also asserts
// the drain actually skipped.
func holdTheSaleReversalLock(t *testing.T, env *testEnv, saleID string) (release func()) {
	t.Helper()
	conn, err := env.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("open a connection to hold the reversal lock: %v", err)
	}
	var acquired bool
	if err := conn.QueryRowContext(context.Background(),
		`SELECT pg_try_advisory_lock(18, hashtext($1))`, saleID,
	).Scan(&acquired); err != nil {
		t.Fatalf("take the reversal lock on %s: %v", saleID, err)
	}
	if !acquired {
		t.Fatalf("the reversal lock on %s was already held; nothing staged", saleID)
	}
	return sync.OnceFunc(func() {
		_, _ = conn.ExecContext(context.Background(),
			`SELECT pg_advisory_unlock(18, hashtext($1))`, saleID)
		_ = conn.Close()
	})
}

// TestContentionCostsATickAndNotTheClaimLease: a run that claimed a Reversal
// Request and then could not probe it hands the claim straight back.
//
// The claim pushes next_attempt_at forward before the advisory lock is even
// attempted, because a claim has to be visible to other claimants in SQL and the
// lock is not. Charging that bound to a request that was merely busy would delay
// somebody's refund by five minutes for the crime of two actors arriving at once
// — which is likeliest exactly when a backlog makes runs overlap.
//
// The tick below arrives a minute after the contended one, which is the deployed
// cadence, and the request must already be due when it does: "the next tick picks
// it up" is a promise the released claim's own delay has to keep, and a delay
// equal to the cadence would miss it by the length of a run.
func TestContentionCostsATickAndNotTheClaimLease(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Busy Fest", "busy-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	ref, _, saleID := stuckReversal(t, "busy-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// A minute after the press the request is due, and somebody else is holding
	// the sale — a buyer's own second press, another instance, a Customer Area
	// load, all indistinguishable from here.
	contendedAt := fixedClock.Add(time.Minute)
	atReversalClock(t, contendedAt)
	release := holdTheSaleReversalLock(t, env, saleID)
	t.Cleanup(release)

	asked := payphoneStub.reverseCount()
	contended := drainReversals(t, payphoneEnv)
	if contended.Pursued != 1 || contended.Skipped != 1 {
		t.Fatalf("drain against a held sale = %+v, want the request claimed and skipped — if it was not skipped, the lock staged nothing and the rest of this test proves nothing", contended)
	}
	if got := payphoneStub.reverseCount(); got != asked {
		t.Fatalf("PayPhone was asked to reverse %d times for a sale another actor was holding, want it left at %d", got, asked)
	}

	// The claim is back, due about a tick from now rather than at the end of the
	// five-minute crash lease.
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "in_flight" {
		t.Fatalf("Reversal Request after a contended run = %+v, want it still in flight", request)
	}
	if got := request.nextAttemptAt.Sub(contendedAt); got >= time.Minute {
		t.Fatalf("a contended Reversal Request is due again in %v, want strictly less than one tick — being busy must not cost the claim lease, and a delay equal to the cadence comes due only after the next tick has already gone past it", got)
	}

	// And it really is picked up on the next tick: whoever held the sale is done,
	// and the request is worked rather than sat out.
	release()
	atReversalClock(t, contendedAt.Add(61*time.Second))
	next := drainReversals(t, payphoneEnv)
	if next.Pursued != 1 || next.StillInFlight != 1 {
		t.Fatalf("the tick after the contended one = %+v, want the request pursued — contention must cost one tick, not five minutes", next)
	}
	if got := payphoneStub.reverseCount(); got != asked+1 {
		t.Fatalf("PayPhone was asked to reverse %d times, want %d — exactly one probe once the sale was free", got, asked+1)
	}
}

// TestAGivenUpReversalIsNotShownAsARefundInProgress: an Unresolved Reversal
// stops reading as an in-flight refund, and pressing Undo on it is refused
// honestly rather than answered "we're processing it" forever.
//
// The buyer is the person this is for. The platform gave up: it asked PayPhone
// for a day and never learned what it did, and a Platform Operator now has to
// settle it against PayPhone's own dashboard. A card that still says "refund in
// progress" tells them to keep waiting for something that will never happen on
// its own.
//
// And the press must not become a fresh ask. The money's state is UNKNOWN — it
// may already be back — so a second reversal posted here is the double refund
// this whole feature is built to avoid. What they get is a typed refusal that
// reaches no provider at all.
func TestAGivenUpReversalIsNotShownAsARefundInProgress(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Given Up Fest", "given-up-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	ref, _, saleID := stuckReversal(t, "given-up-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// A day of silence, and the platform stops asking.
	givenUpAt := fixedClock.Add(sales.ReversalGiveUpAfter)
	atReversalClock(t, givenUpAt)
	if result := drainReversals(t, payphoneEnv); result.GaveUp != 1 {
		t.Fatalf("drain after a day of silence = %+v, want the request given up on", result)
	}

	// The Customer's own Area. The sale is exactly what it was — active, valid,
	// capacity held — and nothing claims a refund is on its way.
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if sale.ReversalPending {
		t.Fatalf("Customer Area sale on an Unresolved Reversal = %+v, want reversal_pending false — the platform has stopped pursuing it, and a buyer told 'refund in progress' is waiting for something that will never happen", sale)
	}
	if sale.Status != "active" {
		t.Fatalf("Customer Area sale = %+v, want it still active — giving up is learning nothing, not learning the money came back", sale)
	}

	// Pressing Undo. It is refused, in its own words, and PayPhone is not asked.
	asked := payphoneStub.reverseCount()
	payphoneStub.reset()
	resp, body := reverseSaleRequest(t, payphoneEnv, token, saleID)
	assertRefused(t, resp, body, http.StatusConflict, "REVERSAL_UNRESOLVED")
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times by a press on an Unresolved Reversal, want 0 — the money may already be back and asking again would return it twice (%d probes preceded this press)", got, asked)
	}

	// No second ask was recorded either: the row stays the one the platform gave
	// up on, waiting for a Platform Operator.
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "needs_attention" {
		t.Fatalf("Reversal Request after the press = %+v, want it left as the Unresolved Reversal it was", request)
	}
	assertNothingChanged(t, env, ref, "given-up-fest", "GA", 8)
}

// TestTheDrainReportsTheInFlightBacklog: the endpoint answers "how much is
// stuck" while it is still stuck.
//
// The Unresolved Reversal queue cannot answer it. That queue is what the
// platform has GIVEN UP on, so it is empty for the first 24 hours of any outage
// — which is exactly the day somebody is curling this endpoint asking whether an
// incident is getting better or worse.
func TestTheDrainReportsTheInFlightBacklog(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Backlog Report Fest", "backlog-report-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 30)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	for _, buyer := range []string{"ana@example.com", "bea@example.com"} {
		stuckReversal(t, "backlog-report-fest", gaID, buyer, 1)
	}
	env.email.Reset()

	atReversalClock(t, fixedClock.Add(time.Minute))
	during := drainReversals(t, payphoneEnv)
	if during.InFlightTotal != 2 {
		t.Fatalf("drain during an outage = %+v, want an in-flight backlog of 2 — the Unresolved queue is empty for a day and cannot answer 'how much is stuck'", during)
	}
	if during.OldestInFlightRequestedAt != fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("drain = %+v, want the oldest ask at %s — how long the backlog has been building is half the answer", during, fixedClock.UTC().Format(time.RFC3339))
	}
	if during.UnresolvedTotal != 0 {
		t.Fatalf("drain = %+v, want nothing given up on yet", during)
	}

	// PayPhone recovers and the backlog drains to nothing, which is the same
	// number saying the incident is over.
	payphoneStub.reset()
	atReversalClock(t, fixedClock.Add(time.Hour))
	after := drainReversals(t, payphoneEnv)
	if after.Reversed != 2 || after.InFlightTotal != 0 || after.OldestInFlightRequestedAt != "" {
		t.Fatalf("drain after PayPhone recovered = %+v, want both reversed and no backlog left", after)
	}
}
