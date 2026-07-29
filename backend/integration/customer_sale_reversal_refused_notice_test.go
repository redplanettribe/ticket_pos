package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The email a Customer gets when the refund they were told was being processed
// definitively could not be made (issue #161, ADR 0024).
//
// It is the correction to the one promise this system knowingly breaks. Every
// other ending already answers the buyer: a success sends the void notice, a
// refusal on the press puts the error on their screen, and an Unresolved
// Reversal says nothing because nothing true is known. Only the buyer who was
// answered "pending" and later refused was told something that stopped being
// true — and the person who most needs this email is the one who closed the tab,
// who will never see a page again and whose only remaining channel is their
// inbox.
//
// What the tests below pin is therefore as much about SILENCE as about sending:
// one email on the pending-then-refused path, none on any other, and never more
// than one however many actors race to resolve the request.

// refusedNotice returns the single refused-reversal notice captured, failing
// unless there is exactly one.
//
// The count is asserted here rather than at each call site because it is the
// acceptance criterion, not a precondition: the Reconciler and the Customer Area
// drain can both arrive at one Reversal Request, and "at most once" is what
// stops a buyer being told twice that their refund failed.
func refusedNotice(t *testing.T, env *testEnv) platform.SaleReversalRefused {
	t.Helper()
	notices := env.email.RefusedReversalNotices()
	if len(notices) != 1 {
		t.Fatalf("captured %d refused-reversal notices, want exactly 1", len(notices))
	}
	return notices[0]
}

// assertNoRefusedNotice asserts the buyer's inbox is empty of this notice.
func assertNoRefusedNotice(t *testing.T, env *testEnv, why string) {
	t.Helper()
	if got := env.email.RefusedReversalNotices(); len(got) != 0 {
		t.Fatalf("captured %d refused-reversal notices, want none — %s", len(got), why)
	}
}

// TestARefundThatWasPendingAndIsRefusedTellsTheCustomer is the tracer bullet:
// the buyer presses Undo, PayPhone goes quiet, they are told their refund is
// being processed — and then the answer comes back a definite no.
//
// The email is read the way a provider delivers it, through the same Text() the
// Resend sender calls, because the acceptance criteria are about words: the
// tickets are STILL VALID, the Sale Confirmation reference is there to quote,
// the Organization is who to talk to, and there is no explanation of why.
func TestARefundThatWasPendingAndIsRefusedTellsTheCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Broken Promise Fest", "broken-promise-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	token, ref, saleID := buyThenReadOwnSale(t, "broken-promise-fest", gaID, "ana@example.com", 2)
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	payphoneStub.reverseHangsUp()
	reverseSalePending(t, payphoneEnv, token, saleID)
	assertNoRefusedNotice(t, env, "the platform is still finding out, and the promise it made is still standing")

	// The issuing bank refuses it, and the buyer's own Customer Area load is what
	// asks. Nothing happened to the money.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
	readCustomerArea(t, payphoneEnv, token, "")

	notice := refusedNotice(t, env)
	if notice.To != "ana@example.com" || notice.Reference != ref {
		t.Fatalf("refused-reversal notice = %+v, want it addressed to the buyer with reference %q", notice, ref)
	}
	if got := env.email.Voided(); len(got) != 0 {
		t.Fatalf("captured %d void notices for a refused reversal, want none — nothing was reversed and those tickets still work", len(got))
	}

	text := notice.Text()
	// The line that decides whether this reader turns up at the gate. Without it
	// the email is only bad news, and a buyer who reads "we could not undo your
	// purchase" and nothing else may well conclude they have neither the money
	// nor the tickets.
	if !strings.Contains(text, "still valid") {
		t.Fatalf("refused-reversal notice body does not say the tickets are still valid:\n%s", text)
	}
	if !strings.Contains(text, ref) {
		t.Fatalf("refused-reversal notice body does not carry the Sale Confirmation reference %q:\n%s", ref, text)
	}
	if !strings.Contains(text, "organizer") {
		t.Fatalf("refused-reversal notice body does not point the buyer at the Organization:\n%s", text)
	}
	if !strings.Contains(text, "Broken Promise Fest") {
		t.Fatalf("refused-reversal notice body does not name the event:\n%s", text)
	}

	// And the half that is a promise of its own. PayPhone publishes no code
	// meaning "too late", so anything this email offered as a cause would be a
	// guess about somebody's money — the provider's code and its Spanish message
	// belong to the log and to the Reversal Request row, never to the buyer. The
	// bare number is not searched for, because a Sale Confirmation reference may
	// legitimately contain any digits; the words are what a leak would arrive as.
	for _, leak := range []string{"banco emisor", "errorCode", "error code", "PayPhone", "payment provider"} {
		if strings.Contains(text, leak) || strings.Contains(notice.Subject(), leak) {
			t.Fatalf("refused-reversal notice leaks the provider's answer (%q):\nsubject: %s\n%s", leak, notice.Subject(), text)
		}
	}

	// The sale itself is exactly as it was, which is what the email is telling
	// them: a definite refusal means nothing happened.
	assertNothingChanged(t, env, ref, "broken-promise-fest", "GA", 8)
}

// TestTheRefusedNoticeIsSentOnceHoweverManyActorsResolveTheRequest is the
// at-most-once criterion, staged against the two actors that can both reach one
// Reversal Request: the Reversal Reconciler's tick and the buyer's own Customer
// Area load.
//
// Nothing marks the notice as sent. What makes it once-only is that the send
// hangs off the in_flight -> refused TRANSITION rather than off the refused
// state: every actor takes the same per-sale advisory lock, the guarded write
// can only be made by the one that re-read the request in flight, and once it is
// refused the live-request index no longer returns it, so every later arrival
// skips without a word. That is a property of the ordering, and the way to prove
// it is to let all of them arrive.
func TestTheRefusedNoticeIsSentOnceHoweverManyActorsResolveTheRequest(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Told Once Fest", "told-once-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	payphoneStub.reverseHangsUp()
	ref, _, _ := stuckReversal(t, "told-once-fest", gaID, "ana@example.com", 2)
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	env.email.Reset()

	// The tick gets there first — the buyer closed the tab.
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
	result := drainReversals(t, payphoneEnv)
	if result.Pursued != 1 || result.Refused != 1 {
		t.Fatalf("drain = %+v, want the one stuck request pursued and refused", result)
	}
	refusedNotice(t, env)

	// Then everybody else arrives at the same request: another tick, and the
	// buyer coming back to look. Neither may send a second copy, and neither may
	// ask PayPhone again about an answer it has already given.
	asked := payphoneStub.reverseCount()
	atReversalClock(t, fixedClock.Add(30*time.Minute))
	if later := drainReversals(t, payphoneEnv); later.Pursued != 0 {
		t.Fatalf("a later drain = %+v, want nothing pursued — a refused request is a closed ask", later)
	}
	readCustomerArea(t, payphoneEnv, token, "")
	drainReversals(t, payphoneEnv)

	if got := len(env.email.RefusedReversalNotices()); got != 1 {
		t.Fatalf("the buyer was told %d times that their refund was refused, want exactly 1", got)
	}
	if got := payphoneStub.reverseCount(); got != asked {
		t.Fatalf("PayPhone was asked to reverse %d times after it had already refused, want it left at %d", got, asked)
	}
	if request := theOnlyReversalRequest(t, env, ref); request.status != "refused" {
		t.Fatalf("Reversal Request = %+v, want refused", request)
	}
}

// TestARefusalOnThePressEmailsNobody: a reversal PayPhone refuses inside the
// buyer's own request sends no email at all.
//
// Nothing was promised, so nothing needs correcting — the buyer is looking at
// the refusal on the page, with their reference on it, and an email arriving to
// tell them what they are currently reading is noise. This is the line the
// notice sits behind: it answers a PENDING request, not a refused reversal.
func TestARefusalOnThePressEmailsNobody(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Instant No Fest", "instant-no-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	ref, _ := buyOnlineThroughPayPhone(t, "instant-no-fest", gaID, "ana@example.com", 2)
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref).ID
	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)

	payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
	resp, body := reverseSaleRequest(t, payphoneEnv, token, saleID)
	assertRefused(t, resp, body, http.StatusBadGateway, "SALE_REVERSAL_FAILED")

	assertNoRefusedNotice(t, env, "the buyer is reading the refusal on the page; they were never told anything else")
	assertNothingChanged(t, env, ref, "instant-no-fest", "GA", 8)

	// And a second press that DOES go pending, and is then refused, is told —
	// because that one was promised something. The same buyer, the same sale, the
	// same refusal from PayPhone: the only difference is whether the platform
	// answered them "pending" first, which is exactly the distinction the notice
	// turns on.
	payphoneStub.reset()
	payphoneStub.reverseHangsUp()
	reverseSalePending(t, payphoneEnv, token, saleID)
	atReversalClock(t, fixedClock.Add(time.Minute))
	payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
	readCustomerArea(t, payphoneEnv, token, "")
	if notice := refusedNotice(t, env); notice.Reference != ref {
		t.Fatalf("refused-reversal notice = %+v, want the buyer's own sale under reference %q", notice, ref)
	}
}
