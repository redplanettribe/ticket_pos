package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A Sale Reversal that PayPhone does not answer in time (issue #157, ADR 0024).
//
// This is the case the whole feature exists for, and until now it produced a
// lie. The buyer pressed Undo, the POST timed out, and they were told their
// purchase could not be undone — while PayPhone had very possibly reversed the
// payment, leaving the Ticket Sale active with its capacity held and the
// Organization's dashboard showing revenue that no longer existed. The only
// thing that ever recovered it was the buyer pressing again.
//
// What the tests below assert is that the ask is now RECORDED before PayPhone is
// called, so silence becomes a question the platform holds open rather than a
// failure it reports. The Customer is told "pending", their tickets stay valid,
// and loading their Customer Area is what asks PayPhone again.
//
// Everything runs against payphoneEnv — the app wired to the real PayPhone
// provider with its base URL pointed at the fake server — because the
// distinction being tested is one only a provider integration can draw: a
// definite refusal versus an answer that never came.
//
// The buyer-facing word is "undo", and while a request is in flight it is "we're
// processing your refund". Never "your purchase is undone", which is the one
// sentence a pending reversal must never produce.

// atReversalClock moves the two clocks the asynchronous reversal path reads on
// the PayPhone app: the sales service's, which decides when a Reversal Request
// is due to be asked about again, and the customers service's, which the
// Customer Area renders against.
//
// It has to exist because a drain is now THROTTLED. A request asked about a
// moment ago is not due, so a test that wants a second probe has to let time
// pass, exactly as a buyer coming back to their purchases does. That is not a
// concession to the implementation: a drain nothing paces re-posts Reverse on
// every render, and the only way to prove it does not is to be able to move the
// clock it paces itself by.
//
// setupTest resets the SHARED app's clocks and knows nothing of this second app,
// so the cleanup here is what stops one test's time travel leaking into the next.
func atReversalClock(t *testing.T, at time.Time) {
	t.Helper()
	payphoneApp.SalesService.WithClock(func() time.Time { return at })
	payphoneApp.CustomersService.WithClock(func() time.Time { return at })
	t.Cleanup(func() {
		payphoneApp.SalesService.WithClock(func() time.Time { return fixedClock })
		payphoneApp.CustomersService.WithClock(func() time.Time { return fixedClock })
	})
}

// ecuadorOnTheDayOfPurchase is a wall-clock time on the Ecuadorian calendar date
// that every sale in this suite is bought on — fixedClock is 12:00 UTC, which is
// 07:00 in Ecuador, so the cutoff that governs these sales is 20:00 that same
// day.
//
// It is built in Ecuador's own zone rather than as an offset from fixedClock
// because that is how the rule is stated: the Reversal Window closes at 20:00
// Ecuador time on the date of purchase (ADR 0018). A test that wrote "12:58 UTC
// plus twelve hours" would be re-deriving the rule it is checking.
func ecuadorOnTheDayOfPurchase(t *testing.T, hour, minute int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(platform.EcuadorTimeZone)
	if err != nil {
		t.Fatalf("load %s: %v", platform.EcuadorTimeZone, err)
	}
	day := fixedClock.In(loc)
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
}

// buyThenReadOwnSale completes a paid Online Sale and returns the buyer's
// session, the Sale Confirmation reference and the sale's id as their own
// Customer Area gave it to them.
//
// The Area is read HERE, before anything is asked of PayPhone, deliberately.
// Loading the Area resolves in-flight Reversal Requests, so a test that fetched
// the sale id after pressing Undo would drain its own pending state by accident
// and prove something other than what it says.
func buyThenReadOwnSale(t *testing.T, eventSlug, ticketTypeID, email string, quantity int) (token, ref, saleID string) {
	t.Helper()
	ref, _ = buyOnlineThroughPayPhone(t, eventSlug, ticketTypeID, email, quantity)
	token = customerSignIn(t, payphoneEnv, email)
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if sale.ReversalPending {
		t.Fatal("a purchase nobody has asked to undo reports a Reversal Request in flight")
	}
	return token, ref, sale.ID
}

// assertPendingInTheCustomerArea reads the Customer's own Area and asserts the
// sale is shown as a purchase whose refund is being processed: pending, and
// still every bit as valid as it was.
//
// The second half is the point. A pending reversal changes nothing about the
// money or the tickets — no money is known to have moved — so the sale is still
// active and still counts everywhere it counted before. Only the surface changes.
//
// It also drains: reading the Area is what makes the platform ask PayPhone
// again, so what this returns is the state AFTER that probe.
func assertPendingInTheCustomerArea(t *testing.T, token, ref string) {
	t.Helper()
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if !sale.ReversalPending {
		t.Fatalf("Customer Area sale = %+v, want reversal_pending true while the platform is still finding out", sale)
	}
	if sale.Status != "active" {
		t.Fatalf("Customer Area sale status = %q while a reversal is pending, want active — no money is known to have moved, so the tickets are still valid", sale.Status)
	}
}

// TestTimedOutReversalIsPendingAndResolvesOnAlreadyCancelled is the production
// incident of 2026-07-28 staged end to end, and the tracer bullet for #157.
//
// PayPhone takes longer to answer than the platform will wait, but it DOES
// reverse the payment: the money is on its way back and nobody here knows it.
// The buyer must not be told their undo failed, because it did not, and must not
// be told it succeeded, because the platform cannot yet say so. They are told it
// is being processed — and then, without pressing anything, they reload their
// purchases and it has resolved.
//
// The answer that resolves it is PayPhone's errorCode 24, "La transacción ya se
// encuentra cancelada": not a refusal but a receipt, proof the money already
// left. That single reading is what makes asking again safe, and it is the
// assumption the whole design rests on (ADR 0024).
func TestTimedOutReversalIsPendingAndResolvesOnAlreadyCancelled(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishEventStarting(t, env, sessionID, "Slow Undo Fest", "slow-undo-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "slow-undo-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	// The press. PayPhone holds the reversal past the ten seconds the provider
	// will wait, and then carries it out — so when the buyer's request gives up,
	// the money is already moving and the platform has no way to know.
	payphoneStub.reverseTimesOut()
	pending := reverseSalePending(t, payphoneEnv, token, saleID)
	if pending.ConfirmationRef != ref || pending.TicketSaleID != saleID {
		t.Fatalf("pending undo = %+v, want the buyer's own sale %q under reference %q", pending, saleID, ref)
	}

	// Nothing moved: the Ticket Sale still stands, its capacity is still consumed,
	// and nobody has been told their tickets are gone. The Reversal Request is the
	// only thing that exists that did not before.
	assertNothingChanged(t, env, ref, "slow-undo-fest", "GA", 8)
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "in_flight" || request.attempts != 1 {
		t.Fatalf("Reversal Request after the timeout = %+v, want in_flight after 1 attempt", request)
	}
	if request.requestedAt.UTC().Format(time.RFC3339) != pending.RequestedAt {
		t.Fatalf("requested_at reported as %q, recorded as %v — the buyer is told when THEY asked",
			pending.RequestedAt, request.requestedAt)
	}
	if !request.lastError.Valid || request.lastError.String == "" {
		t.Fatal("the Reversal Request records no last error; an operator reading the queue during an incident has nothing to go on")
	}

	// And the Organization still counts it. This is the reason a Reversal Request
	// is a row of its own rather than a third ticket_sales.status: seven
	// aggregates filter `status = 'active'`, and a sale whose refund is merely
	// being enquired about must keep counting in every one of them, because the
	// money is still the Organization's until somebody says otherwise (ADR 0024).
	// The Net Proceeds strip stands in for all seven — it is the one an organizer
	// looks at — and it must read exactly as it did a moment before the press.
	if got := salesSummaryOK(t, env, sessionID, eventID); got.SalesCount != 1 {
		t.Fatalf("sales summary while a reversal is pending = %+v, want the sale still counted (sales_count 1)", got)
	}

	// PayPhone is still unwell and still says nothing. Loading the Area asks it
	// again — that is the whole of the recovery mechanism until the Reversal
	// Reconciler ships — and an unknown answer keeps the request open rather than
	// resolving it either way.
	//
	// A minute later, because the request is not due the instant it was last
	// asked about. The throttle has its own test below; here it is only the reason
	// the clock moves.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reverseHangsUp()
	assertPendingInTheCustomerArea(t, token, ref)
	assertNothingChanged(t, env, ref, "slow-undo-fest", "GA", 8)
	if request := theOnlyReversalRequest(t, env, ref); request.status != "in_flight" || request.attempts != 2 {
		t.Fatalf("Reversal Request after a second unknown answer = %+v, want in_flight after 2 attempts — still one ask, asked twice", request)
	}

	// PayPhone comes back and says the transaction is already cancelled. The
	// money is with the buyer; the sale follows it, through the same primitive
	// every other reversal goes through.
	atReversalClock(t, fixedClock.Add(2*time.Minute))
	payphoneStub.reverseAlreadyCancelled()
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if sale.Status != "reversed" || sale.ReversalPending || sale.Reversible {
		t.Fatalf("Customer Area sale after the request resolved = %+v, want reversed, no longer pending and no longer on offer", sale)
	}

	// The ordinary aftermath of a reversal, in full: provenance, capacity, and one
	// void notice — the buyer is told once, when it is true.
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" || !reversedAt.Valid || reversedBy.String != "customer" {
		t.Fatalf("provenance = %s/%+v/%+v, want reversed by 'customer'", status, reversedAt, reversedBy)
	}
	if got := remaining(t, env, "slow-undo-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after the resolved undo, want all 10 back on sale", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one quoting %q", voided, ref)
	}
	if got := salesSummaryOK(t, env, sessionID, eventID); got.SalesCount != 0 {
		t.Fatalf("sales summary after the reversal committed = %+v, want the sale gone (sales_count 0)", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "succeeded" || request.attempts != 3 {
		t.Fatalf("Reversal Request after resolving = %+v, want succeeded after 3 attempts on ONE request", request)
	}

	// And the money moved exactly once. PayPhone was asked three times — asking
	// again is the whole point — but only one of those asks reversed anything.
	if got := payphoneStub.reverseSuccessCount(); got != 1 {
		t.Fatalf("PayPhone carried out %d reversals, want exactly 1 — the buyer is refunded once however many times we ask", got)
	}
}

// TestTimedOutReversalResolvesWhenPayPhoneFinallyAnswersTrue is the other half
// of the same silence: PayPhone did NOT act on the request it never answered,
// so the reversal it is asked about a moment later is the one that moves the
// money.
//
// It is the same buyer-facing story — pending, then undone without pressing
// anything — reached by the opposite provider truth, which is exactly why the
// platform may not guess between them. The assertion that matters is the last
// one: one successful reversal at the provider, never two.
func TestTimedOutReversalResolvesWhenPayPhoneFinallyAnswersTrue(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Silent Undo Fest", "silent-undo-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "silent-undo-fest", gaID, "ana@example.com", 3)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	// The connection dies with nothing said. Unknown outcome: PayPhone may or may
	// not have acted, and this fake in fact did not.
	payphoneStub.reverseHangsUp()
	reverseSalePending(t, payphoneEnv, token, saleID)
	assertNothingChanged(t, env, ref, "silent-undo-fest", "GA", 7)

	// PayPhone recovers. The next probe — driven by the buyer simply looking at
	// their purchases a minute later — gets the documented `true`, and that is
	// the reversal.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reset()
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if sale.Status != "reversed" || sale.ReversalPending {
		t.Fatalf("Customer Area sale after PayPhone recovered = %+v, want reversed and no longer pending", sale)
	}

	if got := remaining(t, env, "silent-undo-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after the resolved undo, want all 10 back on sale", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one quoting %q", voided, ref)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "succeeded" {
		t.Fatalf("Reversal Request = %+v, want succeeded", request)
	}

	// Exactly one reversal was CARRIED OUT. The counter is reset with the stub
	// above, so this counts only the probes since PayPhone recovered — which is
	// the number that says whether this buyer was refunded once or twice.
	if got := payphoneStub.reverseSuccessCount(); got != 1 {
		t.Fatalf("PayPhone carried out %d reversals after recovering, want exactly 1", got)
	}
}

// TestTimedOutReversalResolvesAsRefused is the promise ADR 0024 admits is a
// broken one: the buyer is told their refund is being processed, and later told
// it could not be done.
//
// It is accepted because the alternative is worse. A timeout says nothing about
// whether the money moved, and telling somebody their money is back when it is
// not is the claim ADR 0018 refused to make. So the ask stays open until PayPhone
// answers definitely — and when the answer is a definite refusal, NOTHING
// happened: the Ticket Sale is untouched, its capacity is still consumed, and no
// void notice goes out.
func TestTimedOutReversalResolvesAsRefused(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Refused Later Fest", "refused-later-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "refused-later-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	payphoneStub.reverseHangsUp()
	reverseSalePending(t, payphoneEnv, token, saleID)

	// The issuing bank refuses it. That is an answer, and a definite one: the ask
	// is over and there is nothing left to come back to.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if sale.ReversalPending {
		t.Fatalf("Customer Area sale after a definite refusal = %+v, want reversal_pending false — the platform is no longer finding out", sale)
	}
	assertNothingChanged(t, env, ref, "refused-later-fest", "GA", 8)

	if request := theOnlyReversalRequest(t, env, ref); request.status != "refused" {
		t.Fatalf("Reversal Request after the refusal = %+v, want refused", request)
	}
	if got := payphoneStub.reverseSuccessCount(); got != 0 {
		t.Fatalf("PayPhone carried out %d reversals on a refused request, want 0 — a refusal means nothing happened", got)
	}

	// And the buyer may genuinely ask again. Nothing happened, they are still
	// inside their Reversal Window, and the refused request must not stand in the
	// way — which is what excluding `refused` from the live-request index buys.
	if !sale.Reversible {
		t.Fatalf("Customer Area sale = %+v, want the undo back on offer after a refusal that changed nothing", sale)
	}
	payphoneStub.reset()
	reverseSaleOK(t, payphoneEnv, token, sale.ID)
	if got := remaining(t, env, "refused-later-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after the buyer asked again and it worked, want all 10 back", got)
	}
	if got := len(reversalRequests(t, env, ref)); got != 2 {
		t.Fatalf("sale has %d Reversal Requests, want 2 — a refusal ends one ask and a new press is a new ask", got)
	}
}

// TestSecondPressWhileTheReversalRequestIsInFlight: a buyer who presses Undo
// again while the platform is still finding out is answered from the ask they
// already made.
//
// This is a READ. No second Reversal Request — the database forbids one — and,
// far more importantly, no second call to PayPhone: a reversal posted twice may
// return the money twice, which is the one thing this system must never do
// (ADR 0018, unamended). The buyer is shown the instant of their ORIGINAL press,
// not of this one, because that is the ask being pursued.
func TestSecondPressWhileTheReversalRequestIsInFlight(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Twice Pressed Fest", "twice-pressed-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "twice-pressed-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	payphoneStub.reverseHangsUp()
	first := reverseSalePending(t, payphoneEnv, token, saleID)
	if got := payphoneStub.reverseCount(); got != 1 {
		t.Fatalf("PayPhone was asked to reverse %d times for one press, want 1", got)
	}

	// The second press. PayPhone would answer instantly and successfully if it
	// were reached at all, so a sale that ends up reversed here is itself the
	// failure: it would mean the buyer's money went back a second time.
	payphoneStub.reset()
	second := reverseSalePending(t, payphoneEnv, token, saleID)
	if second.RequestedAt != first.RequestedAt {
		t.Fatalf("second press reported requested_at %q, want the original ask's %q", second.RequestedAt, first.RequestedAt)
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times by a second press, want 0 — the ask in flight is the one that is happening", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "in_flight" || request.attempts != 1 {
		t.Fatalf("Reversal Request after two presses = %+v, want one in_flight request with a single attempt", request)
	}
	assertNothingChanged(t, env, ref, "twice-pressed-fest", "GA", 8)
}

// TestFreeOnlineSaleMakesNoReversalRequest: a free claim is fully synchronous
// and records no Reversal Request at all.
//
// There is nothing for it to wait on. A zero-total checkout is settled by the
// platform itself with no Payment Provider in the loop (ADR 0017), so no money
// was collected by anybody, nobody can go silent about it, and a row recording
// an open question would be a question about nothing. The buyer keeps their
// instant answer.
func TestFreeOnlineSaleMakesNoReversalRequest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, freeID := publishFreeEvent(t, env, sessionID, "Free Sync Fest", "free-sync-fest", 10)

	ref := claimFree(t, env, "free-sync-fest", freeID, "ana@example.com", 2)
	token := customerSignIn(t, env, "ana@example.com")
	sale := saleByRef(t, readCustomerArea(t, env, token, ""), ref)
	if sale.ReversalPending {
		t.Fatalf("free sale = %+v, want reversal_pending false before anything is asked", sale)
	}
	env.email.Reset()

	result := reverseSaleOK(t, env, token, sale.ID)
	if result.Status != "reversed" || result.ConfirmationRef != ref {
		t.Fatalf("free undo = %+v, want an immediate reversal under %q", result, ref)
	}
	if got := reversalRequests(t, env, ref); len(got) != 0 {
		t.Fatalf("free sale recorded %d Reversal Requests, want none — there is no provider to wait on", len(got))
	}
	if got := remaining(t, env, "free-sync-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after the free undo, want all 10 back on sale", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one quoting %q", voided, ref)
	}
	if after := saleByRef(t, readCustomerArea(t, env, token, ""), ref); after.ReversalPending {
		t.Fatalf("free sale after the undo = %+v, want reversal_pending false", after)
	}
}

// The Reversal Window governs when a Customer may ASK, and nothing else.
//
// The two tests below are the only mechanical guard on that rule, which is the
// central invariant of ADR 0024 and was until now stated purely in comments. A
// reviewer proved by mutation that inserting an eligibility check into the drain
// broke no test, and that moving the press path's live-request lookup below the
// eligibility check broke no test either — so both of the ways this feature can
// be silently undone were free.
//
// What makes them undoings rather than tightenings is where they leave the
// money. A Reversal Request exists precisely when PayPhone may already have
// taken the payment back; refusing to pursue it because a deadline has since
// passed abandons the buyer's money at PayPhone with the sale still active and
// nobody looking — the worst outcome the whole design has to offer.

// TestAReversalRequestMadeInsideTheWindowResolvesAfterItCloses: the buyer
// presses at 19:58, two minutes before the Ecuadorian cutoff, and PayPhone says
// nothing. At 20:05 the Window has closed — and the request still resolves.
//
// The authorisation was recorded when the request was written and is not
// re-decided afterwards. A drain that re-checked eligibility would refuse its
// own continuation seven minutes later and strand a reversal PayPhone may
// already have performed.
//
// The untouched control sale is what stops this passing vacuously: it proves the
// Window really has closed by 20:05, so the resolution below is the rule being
// exercised rather than the cutoff being somewhere else.
func TestAReversalRequestMadeInsideTheWindowResolvesAfterItCloses(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Last Minute Fest", "last-minute-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "last-minute-fest", gaID, "ana@example.com", 2)
	controlRef, _ := buyOnlineThroughPayPhone(t, "last-minute-fest", gaID, "ana@example.com", 1)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	// 19:58 Ecuador: inside the Window, and PayPhone goes silent.
	atReversalClock(t, ecuadorOnTheDayOfPurchase(t, 19, 58))
	payphoneStub.reverseHangsUp()
	pending := reverseSalePending(t, payphoneEnv, token, saleID)
	if request := theOnlyReversalRequest(t, env, ref); request.status != "in_flight" {
		t.Fatalf("Reversal Request after the press = %+v, want in_flight", request)
	}

	// 20:05 Ecuador: past the cutoff, and PayPhone is answering again. The control
	// sale — bought by the same buyer on the same day, never asked about — is no
	// longer reversible, which is this test's proof that the deadline has
	// genuinely passed.
	atReversalClock(t, ecuadorOnTheDayOfPurchase(t, 20, 5))
	payphoneStub.reset()
	area := readCustomerArea(t, payphoneEnv, token, "")
	if control := saleByRef(t, area, controlRef); control.Reversible {
		t.Fatalf("control sale = %+v at 20:05 Ecuador, want the Reversal Window closed — the rest of this test proves nothing otherwise", control)
	}

	// PayPhone recovered before that read and answered the probe the read drove.
	// The request made at 19:58 is still the platform's to finish, so it does.
	sale := saleByRef(t, area, ref)
	if sale.Status != "reversed" || sale.ReversalPending {
		t.Fatalf("sale = %+v after the Window closed, want reversed — a Reversal Request made inside the Window stays authorised however long the answer takes (ADR 0024)", sale)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "succeeded" {
		t.Fatalf("Reversal Request = %+v, want succeeded", request)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.requestedAt.UTC().Format(time.RFC3339) != pending.RequestedAt {
		t.Fatalf("requested_at moved: reported %q, recorded %v — the ask is the 19:58 one throughout",
			pending.RequestedAt, request.requestedAt)
	}
	if got := remaining(t, env, "last-minute-fest", "GA"); got != 9 {
		t.Fatalf("remaining = %d, want 9 — the two reversed tickets are back and the control sale's one is not", got)
	}
	if got := payphoneStub.reverseSuccessCount(); got != 1 {
		t.Fatalf("PayPhone carried out %d reversals, want exactly 1", got)
	}
}

// TestASecondPressAfterTheWindowClosesIsToldTheRefundIsBeingProcessed: the buyer
// pressed at 19:58, saw "we're processing your refund", and presses again at
// 20:05 because nothing has visibly happened.
//
// They must be told the same thing again. They are not too late: the ask they
// are pressing about was made in time and is the one being pursued, and a
// "REVERSAL_WINDOW_CLOSED" here would tell somebody whose money may already be
// on its way back that they missed their chance at it.
//
// That is entirely a question of ORDER. The live-request lookup runs before the
// eligibility check; swap them and this buyer is refused. The control press at
// the end is the same clock, the same buyer and a sale with no request behind it
// — and it IS refused, which is how this test tells the order apart from the
// Window simply not being enforced.
func TestASecondPressAfterTheWindowClosesIsToldTheRefundIsBeingProcessed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Pressed Twice Late Fest", "pressed-twice-late-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "pressed-twice-late-fest", gaID, "ana@example.com", 2)
	controlRef, _ := buyOnlineThroughPayPhone(t, "pressed-twice-late-fest", gaID, "ana@example.com", 1)
	controlID := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), controlRef).ID
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	atReversalClock(t, ecuadorOnTheDayOfPurchase(t, 19, 58))
	payphoneStub.reverseHangsUp()
	first := reverseSalePending(t, payphoneEnv, token, saleID)

	// 20:05, past the cutoff. PayPhone would answer instantly and successfully if
	// it were reached, so this press must not reach it either.
	atReversalClock(t, ecuadorOnTheDayOfPurchase(t, 20, 5))
	payphoneStub.reset()
	second := reverseSalePending(t, payphoneEnv, token, saleID)
	if second.RequestedAt != first.RequestedAt {
		t.Fatalf("second press reported requested_at %q, want the original 19:58 ask's %q", second.RequestedAt, first.RequestedAt)
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times by the second press, want 0", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "in_flight" || request.attempts != 1 {
		t.Fatalf("Reversal Request after the second press = %+v, want one in_flight request with a single attempt", request)
	}
	assertNothingChanged(t, env, ref, "pressed-twice-late-fest", "GA", 7)

	// The control: same buyer, same instant, a sale nobody has asked about. This
	// one IS too late, which is what makes the answer above an ordering property
	// rather than a Window that stopped being enforced.
	resp, body := reverseSaleRequest(t, payphoneEnv, token, controlID)
	assertRefused(t, resp, body, http.StatusConflict, "REVERSAL_WINDOW_CLOSED")
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times on a sale past its Window, want 0", got)
	}
}

// TestTheDrainOnlyPursuesRequestsThatAreDue: a Customer refreshing their
// purchases during a PayPhone outage must not re-post Reverse on every render.
//
// Each probe costs the full ten-second provider timeout and they run inside the
// page load that triggered them, so an unthrottled drain is a buyer hanging their
// own Area — and hammering a provider that is already unwell — by doing the one
// thing an anxious buyer does. `next_attempt_at` is what makes a request due, and
// this test is what makes it mean something.
//
// The delay itself is not pinned here. What is pinned is that repeated reads
// inside it ask PayPhone nothing, and that a read after it asks exactly once.
func TestTheDrainOnlyPursuesRequestsThatAreDue(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Refresh Fest", "refresh-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "refresh-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	payphoneStub.reverseHangsUp()
	reverseSalePending(t, payphoneEnv, token, saleID)

	// The anxious refresh: five Area loads on the same clock as the press.
	// PayPhone was asked once, by the press, and must not be asked again.
	for range 5 {
		assertPendingInTheCustomerArea(t, token, ref)
	}
	if got := payphoneStub.reverseCount(); got != 1 {
		t.Fatalf("PayPhone was asked to reverse %d times across five refreshes, want 1 — only the press was due", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.attempts != 1 {
		t.Fatalf("Reversal Request = %+v, want a single attempt — a refresh is not an attempt", request)
	}

	// Time passes and the request comes due. One more read, one more probe: the
	// throttle paces the drain, it does not switch it off.
	atReversalClock(t, fixedClock.Add(time.Minute))
	assertPendingInTheCustomerArea(t, token, ref)
	if got := payphoneStub.reverseCount(); got != 2 {
		t.Fatalf("PayPhone was asked to reverse %d times once the request came due, want 2", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.attempts != 2 {
		t.Fatalf("Reversal Request = %+v, want 2 attempts", request)
	}
}

// TestReversalRequestNeedsAttentionWhenSomebodyElseReversedTheSale is the
// double-refund this feature could otherwise cause, and the reason the drain
// re-reads the Ticket Sale before it asks PayPhone anything.
//
// An Operator Reversal (ADR 0019) settles the sale out of band while a Reversal
// Request is in flight. The operator may have refunded through PayPhone's own
// dashboard — in which case a probe would answer errorCode 24 and cost nothing —
// or by BANK TRANSFER, in which case PayPhone's transaction is still live and
// the same probe would reverse it for real and pay the buyer twice. Nothing here
// can tell those apart before asking, so the platform does not ask.
//
// What it does instead is say so. The request becomes needs_attention: an
// Unresolved Reversal, the honest record that the platform does not know what
// became of its own in-flight ask, awaiting a Platform Operator who can read
// PayPhone's dashboard.
func TestReversalRequestNeedsAttentionWhenSomebodyElseReversedTheSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Out Of Band Fest", "out-of-band-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "out-of-band-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	payphoneStub.reverseHangsUp()
	reverseSalePending(t, payphoneEnv, token, saleID)

	// The operator refunds by bank transfer and records it. The sale is reversed
	// and PayPhone was never involved.
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)),
		PlatformFeeKept:     boolPtr(false),
		Note:                strPtr("refunded by bank transfer"),
	})

	// The buyer comes back and the drain finds a request over a sale that no
	// longer stands. PayPhone must not be asked — this is THE assertion.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reset()
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times after somebody else reversed the sale, want 0 — the buyer may already have been refunded off-platform and asking would refund them twice", got)
	}
	if sale.Status != "reversed" || sale.ReversalPending {
		t.Fatalf("Customer Area sale = %+v, want reversed and not pending — the reversal happened, by another hand", sale)
	}

	// And the ask is recorded as what it is: unresolved, awaiting an operator.
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "needs_attention" {
		t.Fatalf("Reversal Request = %+v, want needs_attention — the platform never learned what became of its own ask", request)
	}
	if !request.lastError.Valid || request.lastError.String == "" {
		t.Fatalf("Reversal Request = %+v, want the reason it was given up on recorded for whoever reads the queue", request)
	}

	// It stays given up on. A later visit must not quietly resume asking.
	atReversalClock(t, fixedClock.Add(10*time.Minute))
	readCustomerArea(t, payphoneEnv, token, "")
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times on an Unresolved Reversal, want 0", got)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "needs_attention" {
		t.Fatalf("Reversal Request = %+v, want it still needs_attention", request)
	}
}

// TestPressingUndoOnASucceededButUncommittedRequestIsPending: the buyer presses
// again on a sale whose reversal PayPhone confirmed but whose local write
// failed.
//
// That state is SALE_REVERSAL_NOT_COMMITTED (#162): a `succeeded` Reversal
// Request standing over a still-active Ticket Sale, which is a LIVE row by the
// definition the database enforces — `status <> 'refused'`. A press that looked
// only for `in_flight` would find nothing, try to write a second request, and
// hand the buyer a unique violation. They get 202 instead: their refund really
// is being processed, which is the literal truth here.
//
// The state is staged in SQL because the only way to reach it for real is a
// database failure between two writes. What it stages is a row, and the row is
// the whole point.
func TestPressingUndoOnASucceededButUncommittedRequestIsPending(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Not Committed Fest", "not-committed-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "not-committed-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	payphoneStub.reverseHangsUp()
	pressed := reverseSalePending(t, payphoneEnv, token, saleID)

	// PayPhone said yes and the local write did not land: succeeded over active.
	if _, err := env.db.Exec(`
		UPDATE sale_reversals SET status = 'succeeded', last_error = NULL
		WHERE ticket_sale_id = $1
	`, saleID); err != nil {
		t.Fatalf("stage SALE_REVERSAL_NOT_COMMITTED: %v", err)
	}

	// The press. Not a 500, and not a second call to PayPhone about money that
	// has already gone back.
	payphoneStub.reset()
	again := reverseSalePending(t, payphoneEnv, token, saleID)
	if again.RequestedAt != pressed.RequestedAt {
		t.Fatalf("press on an uncommitted reversal reported requested_at %q, want the original ask's %q", again.RequestedAt, pressed.RequestedAt)
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times, want 0 — the money already went back", got)
	}
	if got := len(reversalRequests(t, env, ref)); got != 1 {
		t.Fatalf("sale has %d Reversal Requests, want 1 — the live one, not a second ask", got)
	}

	// And the Area says the same thing: the refund is still being processed,
	// because the sale it belongs to has not been marked yet.
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if !sale.ReversalPending || sale.Status != "active" {
		t.Fatalf("Customer Area sale = %+v, want an active sale whose refund is still being processed", sale)
	}
}

// TestTheBackoffIsMeasuredFromTheEndOfTheProbe: the wait before the platform
// asks PayPhone again starts when PayPhone stopped answering, not when the buyer
// pressed.
//
// The two are the same instant only when the provider is quick, and the case
// this feature exists for is the one where it is not. A probe can cost the full
// ten-second provider timeout and the backoff's first step is also ten seconds,
// so a delay measured from the ask is entirely consumed by the ask itself: the
// first retry comes due the moment the probe returns, and the throttle standing
// between an unwell PayPhone and this platform hammering it is zero on its very
// first step — precisely when a provider is least able to take it.
//
// The clock has to run for this, which is why it is the one test here that does
// not freeze it. A frozen clock cannot tell "when the buyer pressed" from "when
// the probe finished", so it cannot see the bug at all.
func TestTheBackoffIsMeasuredFromTheEndOfTheProbe(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Slow Answer Fest", "slow-answer-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	token, ref, saleID := buyThenReadOwnSale(t, "slow-answer-fest", gaID, "ana@example.com", 2)

	// PayPhone holds the reversal open past the platform's own call timeout: the
	// 2026-07-28 incident, and the only shape in which the two instants differ.
	payphoneStub.reverseTimesOut()
	atRunningReversalClock(t)
	reverseSalePending(t, payphoneEnv, token, saleID)

	// requested_at is when the buyer pressed; next_attempt_at is when the platform
	// may ask again. The gap must cover the probe AND a first backoff step, and it
	// is asserted as a bound rather than an equality because jitter only ever adds
	// and a real ten-second timeout never lands on the second.
	request := theOnlyReversalRequest(t, env, ref)
	if request.status != "in_flight" {
		t.Fatalf("Reversal Request after an unanswered probe = %+v, want in flight", request)
	}
	wait := request.nextAttemptAt.Sub(request.requestedAt)
	if wait < payPhoneCallTimeout+reversalFirstBackoffStep-time.Second {
		t.Fatalf("the next probe is due %v after the buyer pressed, want at least %v — the ten seconds spent waiting for PayPhone is not a backoff, and a wait means a wait since we last asked",
			wait, payPhoneCallTimeout+reversalFirstBackoffStep-time.Second)
	}
}

// payPhoneCallTimeout and reversalFirstBackoffStep are the two ten-second
// intervals the test above proves do not overlap: what one probe can cost, and
// what the schedule owes after it. Both are unexported where they are defined —
// the PayPhone client and ADR 0024's schedule — and the point of the assertion
// is precisely that nobody has to import one to reason about the other.
const (
	payPhoneCallTimeout      = 10 * time.Second
	reversalFirstBackoffStep = 10 * time.Second
)

// atRunningReversalClock lets the reversal path's clock run in real time from
// the suite's fixed instant, instead of standing still at it.
//
// Everything else here freezes the clock, because a frozen clock is what makes
// the Reversal Window and the retry schedule assertable at all. This is the one
// property that a frozen clock makes invisible: whether an instant is read
// before or after a provider call is a distinction with no meaning when both
// reads answer the same number.
func atRunningReversalClock(t *testing.T) {
	t.Helper()
	base, start := fixedClock, time.Now()
	running := func() time.Time { return base.Add(time.Since(start)) }
	payphoneApp.SalesService.WithClock(running)
	payphoneApp.CustomersService.WithClock(running)
	t.Cleanup(func() {
		payphoneApp.SalesService.WithClock(func() time.Time { return fixedClock })
		payphoneApp.CustomersService.WithClock(func() time.Time { return fixedClock })
	})
}
