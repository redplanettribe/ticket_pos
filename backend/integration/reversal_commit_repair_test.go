package integration

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// A reversal PayPhone carried out and the platform then failed to record: the
// SALE_REVERSAL_NOT_COMMITTED incident, repairing itself (issue #162, ADR 0018's
// accepted residual risk, ADR 0024's queue).
//
// It is the worst state this feature can reach and the quietest. The money has
// gone back, the Ticket Sale is still active, so the buyer holds both their
// refund and their tickets, the capacity never returns to the Ticket Type, and
// the Organization's dashboard shows revenue that no longer exists — and nobody
// is told any of it. Until now it was a log line and a hand-repair.
//
// What the tests below drive is the repair, and the assertion they all carry is
// about the PROVIDER: it is asked exactly once across the whole sequence. The
// answer is already known, so a repair that re-asked would be refunding somebody
// twice on a loop that runs every minute with nobody watching — the single
// worst thing this codebase could do.

// failTheLocalCommit makes the local half of a reversal fail for one Ticket
// Sale: the Payment Provider is asked and agrees, and the write that would void
// the sale blows up exactly as a database under load would make it.
//
// It is a TEST-ONLY seam and it is a database trigger rather than a hook in the
// product, deliberately. Nothing in the reversal path takes an injectable
// failure and nothing should: a seam in the code would be a way to make a
// production commit fail, and it would also prove less. What is staged here is
// the real primitive (repository.ReverseSales) failing mid-transaction, so the
// state the tests then find — a succeeded Reversal Request over an active sale,
// with capacity NOT restored because the whole transaction rolled back — is the
// state the product produces rather than one a test wrote by hand.
//
// The returned repair drops it, and is what a test calls to let the platform
// succeed. It is idempotent so it can be both deferred and called explicitly.
func failTheLocalCommit(t *testing.T, env *testEnv, saleID string) (repair func()) {
	t.Helper()
	if _, err := env.db.Exec(`
		CREATE OR REPLACE FUNCTION refuse_the_local_commit() RETURNS trigger AS $fn$
		BEGIN
			RAISE EXCEPTION 'staged failure of the local commit of a Sale Reversal';
		END;
		$fn$ LANGUAGE plpgsql;

		CREATE TRIGGER refuse_the_local_commit
		BEFORE UPDATE OF status ON ticket_sales
		FOR EACH ROW WHEN (NEW.status = 'reversed' AND OLD.id = '` + saleID + `'::uuid)
		EXECUTE FUNCTION refuse_the_local_commit();
	`); err != nil {
		t.Fatalf("stage a failing local commit for %s: %v", saleID, err)
	}
	repair = sync.OnceFunc(func() {
		if _, err := env.db.Exec(`
			DROP TRIGGER IF EXISTS refuse_the_local_commit ON ticket_sales;
			DROP FUNCTION IF EXISTS refuse_the_local_commit();
		`); err != nil {
			t.Fatalf("stop failing the local commit: %v", err)
		}
	})
	// The trigger outlives a TRUNCATE, so a test that forgot to repair would fail
	// every later reversal in the package.
	t.Cleanup(repair)
	return repair
}

// halfDoneReversal stages the incident: a paid Online Sale whose buyer pressed
// Undo, whose PayPhone reversed the payment, and whose Ticket Sale the platform
// then failed to void.
//
// The press is a real press through the real endpoint and it is answered 500,
// which is what the buyer actually saw on the day: their refund happened and
// they were told it had not.
func halfDoneReversal(t *testing.T, eventSlug, ticketTypeID, email string, quantity int) (ref, saleID string, repair func()) {
	t.Helper()
	token, ref, saleID := buyThenReadOwnSale(t, eventSlug, ticketTypeID, email, quantity)
	repair = failTheLocalCommit(t, payphoneEnv, saleID)

	resp, body := reverseSaleRequest(t, payphoneEnv, token, saleID)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("undo whose local commit failed: status=%d error=%+v, want 500 — if it succeeded, nothing is staged and the rest of this test proves nothing", resp.StatusCode, body.Error)
	}
	return ref, saleID, repair
}

// assertTheMoneyWentBackOnce is the assertion this whole file exists for: the
// Payment Provider carried out exactly one reversal, however many times the
// platform came back to finish the job it left behind.
func assertTheMoneyWentBackOnce(t *testing.T, when string) {
	t.Helper()
	if got := payphoneStub.reverseSuccessCount(); got != 1 {
		t.Fatalf("PayPhone carried out %d reversals %s, want exactly 1 — the answer is already recorded, so asking again is refunding the buyer twice", got, when)
	}
	if got := payphoneStub.reverseCount(); got != 1 {
		t.Fatalf("PayPhone was asked to reverse %d times %s, want exactly 1 — the repair finishes the local commit and must never be able to reach a provider", got, when)
	}
}

// TestAReversalThatFailedToCommitLocallyRepairsItself is the tracer bullet for
// #162: PayPhone says yes, the local write fails, and a later Reconciler tick
// finishes what was started — with nobody watching and without a second word to
// PayPhone.
func TestAReversalThatFailedToCommitLocallyRepairsItself(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Half Done Fest", "half-done-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	ref, _, repair := halfDoneReversal(t, "half-done-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// The incident, exactly as it stands the moment it happens. The money is
	// gone and nothing local moved: the sale is active, its two tickets are still
	// off sale, and the buyer has been told nothing.
	assertTheMoneyWentBackOnce(t, "on the press")
	assertNothingChanged(t, env, ref, "half-done-fest", "GA", 8)
	if request := theOnlyReversalRequest(t, env, ref); request.status != "succeeded" {
		t.Fatalf("Reversal Request after a failed local commit = %+v, want succeeded — the provider's answer is the fact that makes this repairable, and it must survive the write that failed", request)
	}

	// What the buyer sees meanwhile: a refund in progress. That is the honest
	// word — it IS in progress, on the platform's side now rather than on
	// PayPhone's — and reading the Area here also proves the repair below is the
	// tick's work, since a page load that resolved it would leave nothing to
	// find.
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	assertPendingInTheCustomerArea(t, token, ref)

	// The database recovers, and a tick a minute later finishes the reversal.
	repair()
	atReversalClock(t, fixedClock.Add(time.Minute))
	result := drainReversals(t, payphoneEnv)
	if result.Pursued != 1 || result.Reversed != 1 {
		t.Fatalf("drain = %+v, want the half-done reversal picked up and completed", result)
	}

	// Everything the reversal owed from the start, arriving late: the sale is
	// voided and stamped with the Customer as the actor — the Reconciler finishes
	// their ask rather than making one of its own — the two tickets are back on
	// sale, and the buyer is told once.
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" || !reversedAt.Valid || reversedBy.String != "customer" {
		t.Fatalf("provenance = %s/%+v/%+v, want reversed by 'customer'", status, reversedAt, reversedBy)
	}
	if got := remaining(t, env, "half-done-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after the repair, want 10 — the capacity never came back when the commit failed, and this is what returns it", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one quoting %q", voided, ref)
	}
	assertTheMoneyWentBackOnce(t, "once the repair had completed")

	// And it is over. The request leaves the queue because the SALE followed the
	// money, so a later tick finds nothing — and finds nothing on the next
	// hundred either, which is what stops every completed reversal in the history
	// of the table from being retried forever.
	atReversalClock(t, fixedClock.Add(time.Hour))
	if later := drainReversals(t, payphoneEnv); later.Pursued != 0 {
		t.Fatalf("a later drain = %+v, want nothing pursued — a reversal that has landed is finished work", later)
	}
	assertTheMoneyWentBackOnce(t, "after a later tick")
	if voided := env.email.Voided(); len(voided) != 1 {
		t.Fatalf("captured %d void notices in all, want exactly 1 — a buyer is told their tickets are gone once", len(voided))
	}
}

// TestARepairNobodyCanEverWorkBecomesAnUnresolvedReversal: a half-done reversal
// the platform never even gets to TRY still ends up in front of a human.
//
// Contention is the way a repair can be turned away without ever being attempted
// — a sale held by another actor every single time the tick comes round — and it
// is the one ending with no attempt to count and no error to record. Left
// weightless it would loop: the same row claimed every minute, given up on in the
// tally and never in the database, an incident line logged every tick forever,
// and a buyer holding both their refund and their tickets while the queue a person
// reads stays empty.
//
// What makes that possible on this path in particular is that the row is
// `succeeded` rather than `in_flight`, so the give-up write has to be guarded on
// the status the row actually carries. A guard fixed at in_flight matches nothing
// here and writes nothing, while every layer above it reports a request retired.
func TestARepairNobodyCanEverWorkBecomesAnUnresolvedReversal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Always Busy Fest", "always-busy-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	ref, saleID, _ := halfDoneReversal(t, "always-busy-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// Somebody holds this sale, and has every time the platform has come back to
	// it: another instance mid-probe, a Customer Area load, a second press.
	release := holdTheSaleReversalLock(t, env, saleID)
	t.Cleanup(release)

	// A day after the buyer pressed. The tick claims the request, cannot touch the
	// sale, and stops — as the thing a Platform Operator settles rather than as a
	// row to try again in sixty seconds.
	atReversalClock(t, fixedClock.Add(sales.ReversalGiveUpAfter))
	gaveUp := drainReversals(t, payphoneEnv)
	if gaveUp.Pursued != 1 || gaveUp.GaveUp != 1 {
		t.Fatalf("drain a day after the press against a held sale = %+v, want the request pursued and given up on", gaveUp)
	}
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "needs_attention" {
		t.Fatalf("Reversal Request the platform reported giving up on = %+v, want needs_attention in the DATABASE too — a tally that says retired over a row that did not move is a queue nobody can read", request)
	}
	if gaveUp.UnresolvedTotal != 1 || len(gaveUp.Unresolved) != 1 || gaveUp.Unresolved[0].TicketSaleID != saleID {
		t.Fatalf("drain = %+v, want the buyer's own sale queued for an operator", gaveUp)
	}

	// And the loop is over. The lock is let go, so nothing stands in the way any
	// more, and the request is still not worked again: an Unresolved Reversal is a
	// person's job, and this one's money is known to have gone back.
	release()
	atReversalClock(t, fixedClock.Add(sales.ReversalGiveUpAfter+time.Hour))
	if later := drainReversals(t, payphoneEnv); later.Pursued != 0 {
		t.Fatalf("a later drain = %+v, want nothing pursued", later)
	}
	assertTheMoneyWentBackOnce(t, "across a day of never getting near the sale")
}

// TestTheRepairLeavesASaleSomebodyElseReversedAlone: the repair is not owed on a
// sale that is already voided, and it must recognise that rather than try.
//
// This is the one case the probing path and the repair path see differently, and
// the repair's guard is the only thing that knows it. A Reversal Request standing
// succeeded over a sale a Platform Operator then reversed by hand (ADR 0019) is
// FINISHED: the money went back through PayPhone, the sale went back through the
// operator, and there is nothing left for anybody to do. A repair that ran
// anyway would find nothing to void, restore no capacity, send nothing — and be
// counted as a FAILED repair, logging an incident and spending the request's
// attempts and its give-up bound on a case that is already settled, until the day
// runs out and a human is handed a queue entry that says a reversal did not
// happen when it demonstrably did.
//
// The tick that follows is the other half of it. A settled request must stop
// being due, or this row is re-examined every minute for the rest of the
// deployment's life.
func TestTheRepairLeavesASaleSomebodyElseReversedAlone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Settled By Hand Fest", "settled-by-hand-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	ref, _, repair := halfDoneReversal(t, "settled-by-hand-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// The database recovers — the staged failure has to be dropped before anybody
	// can void this sale, the operator included — and then an Operator refunds by
	// bank transfer and records it. That is the sale settled by a hand that knows
	// nothing about the Reversal Request still standing over it.
	repair()
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)),
		PlatformFeeKept:     boolPtr(false),
		Note:                strPtr("refunded by bank transfer"),
	})
	// The Operator's own reversal has told this buyer their tickets are gone, which
	// is the notice that matters and not the one under test. Cleared here so the
	// assertion below is about what the TICK sent.
	env.email.Reset()

	// A tick a minute later. It picks the request up — nothing had recorded that
	// the local half was done — looks, and leaves it alone: skipped, not repaired
	// and above all not failed.
	atReversalClock(t, fixedClock.Add(time.Minute))
	settled := drainReversals(t, payphoneEnv)
	if settled.Pursued != 1 || settled.Skipped != 1 {
		t.Fatalf("drain over a request whose sale somebody else reversed = %+v, want it pursued and skipped — there is nothing left to repair", settled)
	}
	if settled.Reversed != 0 || settled.Failed != 0 || settled.GaveUp != 0 {
		t.Fatalf("drain = %+v, want no reversal, no failure and no incident — the reversal is complete, by two hands instead of one", settled)
	}

	// The request itself is untouched. Nothing was attempted, so nothing is
	// counted and no local failure is recorded against a reversal that succeeded:
	// this row is the receipt for money that went back, and a repair that scribbled
	// an error on it would be describing an incident that is not happening.
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "succeeded" || request.attempts != 1 {
		t.Fatalf("Reversal Request after the tick = %+v, want it still succeeded with the press's single attempt — a repair with nothing to repair must not spend the give-up bound", request)
	}
	if request.lastError.Valid {
		t.Fatalf("Reversal Request = %+v, want no last error — nothing failed here", request)
	}

	// The Operator's reversal stands as the Operator's, not overwritten by a
	// repair claiming the Customer did it, and the buyer is told nothing twice.
	status, _, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" || reversedBy.String != "operator" {
		t.Fatalf("provenance = %s/%+v, want the Operator Reversal left standing", status, reversedBy)
	}
	if voided := env.email.Voided(); len(voided) != 0 {
		t.Fatalf("void notices = %+v, want none from the tick — the Operator's own reversal already told this buyer", voided)
	}
	assertTheMoneyWentBackOnce(t, "once an Operator had settled the sale by hand")

	// And it is over. The request has left the queue for good, which is what stops
	// a settled reversal being re-examined every minute forever.
	atReversalClock(t, fixedClock.Add(time.Hour))
	if later := drainReversals(t, payphoneEnv); later.Pursued != 0 {
		t.Fatalf("a later drain = %+v, want nothing pursued — a reversal completed by two hands is still completed", later)
	}
	if got := theOnlyReversalRequest(t, env, ref); got.attempts != 1 || got.status != "succeeded" {
		t.Fatalf("Reversal Request after a later tick = %+v, want it still untouched", got)
	}
}

// TestALocalCommitThatNeverLandsBecomesAnUnresolvedReversal: the repair backs
// off like everything else the platform retries, and after a day it stops and
// leaves a human something to do.
//
// A retry with no bound is the failure mode a background loop invites: a row
// nobody can finish would be picked up every minute forever, and the incident
// would never become anything a person reads. The bound runs from the buyer's
// press, exactly as it does for a silent provider.
//
// What it leaves is an Unresolved Reversal that says something DIFFERENT from
// every other one. Elsewhere the platform does not know whether the money left;
// here it knows it did, and only the sale needs voiding — which is why the
// operator's move is an Operator Reversal (ADR 0019) rather than a look at
// PayPhone's dashboard.
func TestALocalCommitThatNeverLandsBecomesAnUnresolvedReversal(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Never Lands Fest", "never-lands-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	// The commit is never repaired: this is a platform that stays unable to
	// finish the write for a whole day.
	ref, saleID, _ := halfDoneReversal(t, "never-lands-fest", gaID, "ana@example.com", 2)
	env.email.Reset()

	// A tick a minute later tries and fails. It is reported as a failure rather
	// than as work done — the buyer's refund is still only half made — and the
	// request keeps the provider's answer.
	atReversalClock(t, fixedClock.Add(time.Minute))
	failed := drainReversals(t, payphoneEnv)
	if failed.Pursued != 1 || failed.Failed != 1 {
		t.Fatalf("drain over a commit that still fails = %+v, want the request pursued and counted as failed", failed)
	}
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "succeeded" || request.attempts != 2 {
		t.Fatalf("Reversal Request after a failed repair = %+v, want it still succeeded with the attempt counted", request)
	}
	if !request.lastError.Valid || request.lastError.String == "" {
		t.Fatalf("Reversal Request = %+v, want the local failure recorded — it is what the operator reads to tell this incident from every other one", request)
	}

	// The wait grows: an immediate second tick finds nothing due. A platform that
	// cannot complete a write is not helped by being asked again at once.
	if immediate := drainReversals(t, payphoneEnv); immediate.Pursued != 0 {
		t.Fatalf("an immediate second tick = %+v, want nothing pursued — the repair backs off like every other retry", immediate)
	}

	// A day after the buyer pressed, the platform stops.
	atReversalClock(t, fixedClock.Add(sales.ReversalGiveUpAfter))
	gaveUp := drainReversals(t, payphoneEnv)
	if gaveUp.Pursued != 1 || gaveUp.GaveUp != 1 {
		t.Fatalf("drain a day after the press = %+v, want the repair given up on", gaveUp)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "needs_attention" {
		t.Fatalf("Reversal Request after a day of failed commits = %+v, want needs_attention — an Unresolved Reversal, awaiting a Platform Operator", request)
	}

	// The queue, as the endpoint hands it over. The sale is still active, which
	// beside a last error naming the local write is how an operator tells this
	// apart from a reversal whose fate nobody knows.
	if gaveUp.UnresolvedTotal != 1 || len(gaveUp.Unresolved) != 1 {
		t.Fatalf("drain = %+v, want an Unresolved Reversal queue of exactly one", gaveUp)
	}
	if queued := gaveUp.Unresolved[0]; queued.TicketSaleID != saleID || queued.SaleStatus != "active" || queued.LastError == "" {
		t.Fatalf("queued Unresolved Reversal = %+v, want the buyer's own sale still standing with the local failure recorded", queued)
	}

	// The sale is untouched and the buyer has been told nothing — no void notice,
	// because their tickets are demonstrably still valid, and no refused notice,
	// because their refund was not refused. It happened; the platform simply
	// could not write it down.
	assertNothingChanged(t, env, ref, "never-lands-fest", "GA", 8)
	assertNoRefusedNotice(t, env, "the money went back and the platform knows it; nothing was refused")

	// And it is over: an Unresolved Reversal is never worked again, whatever
	// recovers afterwards.
	atReversalClock(t, fixedClock.Add(sales.ReversalGiveUpAfter+time.Hour))
	if later := drainReversals(t, payphoneEnv); later.Pursued != 0 {
		t.Fatalf("a later drain = %+v, want nothing pursued — the queue is a person's job now", later)
	}
	assertTheMoneyWentBackOnce(t, "across a day of failed repairs")
}
