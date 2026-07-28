package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// A Customer undoing a PAID Online Sale, end to end through the real PayPhone
// provider (issue #120, ADR 0018). The free path is proved in
// customer_sale_reversal_test.go; what is added here is the one leg a free claim
// has never had — a Payment Provider that must agree before anything local moves.
//
// Everything below runs against payphoneEnv, the second app wired to the real
// PayPhone provider with its base URL pointed at the fake PayPhone server. Staff
// setup and the money surfaces are read through sharedEnv, which talks to the
// same database. That split is what makes these assertions real: the reversal
// travels over HTTP to something that answers the way PayPhone documents, and
// the figures are read back from the surfaces an organizer actually looks at.
//
// The buyer-facing word is "undo". Never "cancel", never "refund".

// buyOnlineThroughPayPhone completes a paid Online Sale against the fake PayPhone
// server — Prepare, the return redirect's params, V2/Confirm — and returns the
// Sale Confirmation reference with the client transaction id the Payment
// recorded. That id is what the reversal must present to PayPhone.
func buyOnlineThroughPayPhone(t *testing.T, eventSlug, ticketTypeID, email string, quantity int) (ref, clientTransactionID string) {
	t.Helper()
	begun := beginCheckoutOK(t, payphoneEnv, testOrgSlug, eventSlug,
		checkoutBody(email, "Ana", "Lopez", cartLine(ticketTypeID, quantity)))

	resp, body := confirmCheckoutParams(t, payphoneEnv, begun.ClientTransactionID,
		payphoneReturnParams(begun.ClientTransactionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var settled confirmCheckoutResult
	if err := json.Unmarshal(body.Data, &settled); err != nil {
		t.Fatalf("decode confirm result: %v", err)
	}
	if settled.Status != "approved" || settled.ConfirmationRef == "" {
		t.Fatalf("confirm = %+v, want approved with a reference", settled)
	}
	return settled.ConfirmationRef, begun.ClientTransactionID
}

// TestCustomerUndoesTheirOwnPaidOnlineSale is the ticket's tracer bullet, and it
// is the first proof in the suite that a reversal moves money-shaped numbers.
//
// A paid purchase is undone through the real endpoint: PayPhone is asked first
// and answers with its documented literal `true`, and only then is the sale
// voided, its capacity returned and the buyer told. The Organization's Net
// Proceeds, its Withdrawable Balance and the Operator Dashboard's platform
// revenue all fall by a NON-ZERO amount — the assertion #119 could not make with
// free claims alone, where every figure was zero before and after.
func TestCustomerUndoesTheirOwnPaidOnlineSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishEventStarting(t, env, sessionID, "Paid Undo Fest", "paid-undo-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	ref, clientTransactionID := buyOnlineThroughPayPhone(t, "paid-undo-fest", gaID, "ana@example.com", 2)
	if got := remaining(t, env, "paid-undo-fest", "GA"); got != 8 {
		t.Fatalf("remaining after buying 2 of 10 = %d, want 8", got)
	}

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	before := moneySurfaces(t, env, sessionID, operatorSessionID, eventID)
	if before.summary.NetProceedsCents != 2*feeTestBaseCents || before.summary.SalesCount != 1 {
		t.Fatalf("before = %+v, want %d net over 1 sale", before.summary, 2*feeTestBaseCents)
	}
	if before.platformFeeCents != 2*feeTestFeeCents || before.feeIVACents != 2*feeTestIVACents {
		t.Fatalf("platform revenue before = fee %d, iva %d; want %d and %d",
			before.platformFeeCents, before.feeIVACents, 2*feeTestFeeCents, 2*feeTestIVACents)
	}
	env.email.Reset()

	// The Customer Area offers the undo on a paid sale, which is the half of this
	// feature a buyer ever sees: the button exists because the provider that
	// collected the money can give it back.
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	sale := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if !sale.Reversible {
		t.Fatal("a paid purchase made this morning is not offered as reversible; the provider can reverse it and the window is open")
	}
	if sale.ReversibleUntil == nil || *sale.ReversibleUntil != ecuadorCutoffAfterFixedClock {
		t.Fatalf("reversible_until = %v, want %q", sale.ReversibleUntil, ecuadorCutoffAfterFixedClock)
	}

	result := reverseSaleOK(t, payphoneEnv, token, sale.ID)
	if result.Status != "reversed" || result.ConfirmationRef != ref {
		t.Fatalf("undo = %+v, want the sale reversed under its own reference %q", result, ref)
	}

	// PayPhone was asked exactly once, on the merchant-keyed endpoint, under the
	// configured bearer token, naming OUR id for the transaction. Once and not
	// twice: a retried reversal risks returning the money a second time.
	req := payphoneStub.lastReverseRequest(t)
	if req.authorization != "Bearer "+payphoneTestAPIToken {
		t.Fatalf("reverse authorization = %q, want the configured Bearer token", req.authorization)
	}
	if req.body["clientId"] != clientTransactionID {
		t.Fatalf("reverse clientId = %v, want the Payment's client transaction id %q", req.body["clientId"], clientTransactionID)
	}
	if got := payphoneStub.reverseCount(); got != 1 {
		t.Fatalf("PayPhone was asked to reverse %d times, want exactly 1", got)
	}

	// The same outcome the free path already proves: voided sale with the
	// Customer named as the actor, tickets back on sale, one void notice.
	status, reversedAt, reversedBy := saleProvenance(t, env, ref)
	if status != "reversed" || !reversedAt.Valid || !reversedBy.Valid || reversedBy.String != "customer" {
		t.Fatalf("provenance = %s/%+v/%+v, want reversed by 'customer'", status, reversedAt, reversedBy)
	}
	if ga := publicTicketTypes(t, env, testOrgSlug, "paid-undo-fest")["GA"]; ga.Remaining != 10 || ga.SoldOut {
		t.Fatalf("public GA = %+v after the undo, want all 10 back on sale", ga)
	}
	voided := env.email.Voided()
	if len(voided) != 1 || voided[0].To != "ana@example.com" || voided[0].Reference != ref {
		t.Fatalf("void notices = %+v, want exactly one addressed to the buyer quoting %q", voided, ref)
	}

	// The money surfaces drop, and by the reversed sale's own contribution rather
	// than by nothing at all. Every figure returns to the empty Event's zero,
	// having been non-zero a moment ago.
	after := moneySurfaces(t, env, sessionID, operatorSessionID, eventID)
	if after.summary.SalesCount != 0 || after.summary.NetProceedsCents != 0 {
		t.Fatalf("summary after the undo = %+v, want no sales and no proceeds", after.summary)
	}
	if after.balanceCents != before.balanceCents-2*feeTestBaseCents {
		t.Fatalf("withdrawable balance after = %d (before %d), want it lower by the reversed sale's %d",
			after.balanceCents, before.balanceCents, 2*feeTestBaseCents)
	}
	if after.platformFeeCents != 0 || after.feeIVACents != 0 || after.totalOwedCents != 0 {
		t.Fatalf("platform revenue after = fee %d, iva %d, owed %d; want the reversed sale to stop counting entirely",
			after.platformFeeCents, after.feeIVACents, after.totalOwedCents)
	}

	// And the sale is still there, reversed rather than deleted, no longer on
	// offer — it cannot be undone twice.
	area := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref)
	if area.Status != "reversed" || area.Reversible || area.ReversibleUntil != nil {
		t.Fatalf("Customer Area sale after the undo = %+v, want a reversed sale with no offer", area)
	}
}

// TestPayPhoneRefusalLeavesEverythingUntouched is the promise a refusal makes,
// against every shape a refusal can arrive in.
//
// PayPhone documents exactly one success — a literal `true` — so a refusal
// object, a `false`, a body that is not JSON, and a connection dropped mid-call
// are all failures, and all of them must reverse nothing. The buyer is told 502
// SALE_REVERSAL_FAILED with their Sale Confirmation reference and no claim about
// the cause: the published catalogue has no "too late" code, so an explanation
// would be a guess about somebody's money.
//
// The Ticket Sale, its capacity and the buyer's inbox are asserted untouched
// after each one, which is what the provider-first ordering buys.
func TestPayPhoneRefusalLeavesEverythingUntouched(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Refused Undo Fest", "refused-undo-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	ref, _ := buyOnlineThroughPayPhone(t, "refused-undo-fest", gaID, "ana@example.com", 3)
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref).ID
	env.email.Reset()

	cases := []struct {
		name  string
		setup func()
	}{
		// The issuing-bank refusal, the one code PayPhone documents that says
		// nothing about timing.
		{"error code 42", func() {
			payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar, contáctese con el banco emisor", 42)
		}},
		{"transaction not found", func() {
			payphoneStub.reverseRefuses(http.StatusNotFound, "Transacción no encontrada", 20)
		}},
		// A 200 whose body is the documented value's opposite: an answer, and a no.
		{"literal false", func() { payphoneStub.reverseAnswers("false") }},
		// A 200 carrying an object rather than the documented bare value. Not a
		// success just because it parses.
		{"200 with a refusal object", func() {
			payphoneStub.reverseAnswers(`{"message":"no","errorCode":40}`)
		}},
		{"malformed json", func() { payphoneStub.reverseAnswers("<html>maintenance</html>") }},
		// PayPhone was never reached, or answered nothing: unknown outcome, and an
		// unknown outcome may never void a Ticket Sale.
		{"transport failure", func() { payphoneStub.reverseHangsUp() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			resp, body := reverseSaleRequest(t, payphoneEnv, token, saleID)
			assertRefused(t, resp, body, http.StatusBadGateway, "SALE_REVERSAL_FAILED")

			// The reference travels with the refusal so the buyer has something to
			// quote to the Organization; nothing else does — no error code, no cause.
			details, _ := body.Error.Details.(map[string]any)
			if details["confirmation_ref"] != ref {
				t.Fatalf("refusal details = %+v, want the Sale Confirmation reference %q", body.Error.Details, ref)
			}
			assertNothingChanged(t, env, ref, "refused-undo-fest", "GA", 7)
		})
	}

	// PayPhone recovers, and the same buyer's same request now goes through: the
	// refusals above cost them the attempt and nothing else.
	payphoneStub.reset()
	reverseSaleOK(t, payphoneEnv, token, saleID)
	if got := remaining(t, env, "refused-undo-fest", "GA"); got != 10 {
		t.Fatalf("remaining after the undo finally succeeded = %d, want 10", got)
	}
}

// TestConcurrentUndoAsksPayPhoneOnce is the money half of the double-press
// promise, and the free path could never make it: with no Payment Provider in
// the loop, a second reversal costs nothing, so the row lock inside the
// repository primitive was enough. On a PAID sale it is not. That lock is taken
// AFTER the provider has been asked, so two attempts that both read an active
// sale both POST /api/Reverse/Client and only then serialise on the local write —
// one buyer, one 200, one restored seat, and their money returned twice.
//
// The race is staged rather than hoped for: PayPhone holds the first reversal
// open, the second attempt is made while it hangs there, and only then is the
// first let go. That is the exact interval a real double-press falls into, held
// wide enough to aim at.
//
// The assertion that matters is the count. Exactly one reversal reaches PayPhone
// — a reversal is never retried and never doubled, because it may return the
// money twice (ADR 0018) — with the ordinary outcome around it: one 200, one
// refusal, one void notice, and the capacity back exactly once.
func TestConcurrentUndoAsksPayPhoneOnce(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Double Press Fest", "double-press-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	ref, _ := buyOnlineThroughPayPhone(t, "double-press-fest", gaID, "ana@example.com", 4)
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref).ID

	env.email.Reset()
	payphoneStub.reset()
	t.Cleanup(payphoneStub.reset)
	entered, release := payphoneStub.reverseBlocksFirst()
	defer release()

	// The first press, parked inside PayPhone.
	type attempt struct {
		status int
		code   string
	}
	first := make(chan attempt, 1)
	go func() {
		resp, body := reverseSaleRequest(t, payphoneEnv, token, saleID)
		var code string
		if body.Error != nil {
			code = body.Error.Code
		}
		first <- attempt{resp.StatusCode, code}
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("PayPhone was never asked to reverse the first press")
	}

	// The second press, made while the first is mid-reversal at the provider. It
	// must not reach PayPhone at all: the reversal already in flight is the one
	// that is happening, and this buyer has no second one to make.
	resp, body := reverseSaleRequest(t, payphoneEnv, token, saleID)
	assertRefused(t, resp, body, http.StatusConflict, "SALE_ALREADY_REVERSED")

	release()
	won := <-first
	if won.status != http.StatusOK {
		t.Fatalf("the first press returned %d %s, want 200", won.status, won.code)
	}

	// One call to PayPhone. This is the assertion the whole guard exists for:
	// every figure below would look identical if the buyer had been refunded
	// twice.
	if got := payphoneStub.reverseCount(); got != 1 {
		t.Fatalf("PayPhone was asked to reverse %d times for one sale pressed twice, want exactly 1 — a second reversal returns the buyer's money again", got)
	}

	if status, _, reversedBy := saleProvenance(t, env, ref); status != "reversed" || reversedBy.String != "customer" {
		t.Fatalf("provenance = %s/%+v after a double press, want reversed by 'customer'", status, reversedBy)
	}
	if got := remaining(t, env, "double-press-fest", "GA"); got != 10 {
		t.Fatalf("remaining = %d after a double press, want all 10 back — capacity is restored once, not twice", got)
	}
	if voided := env.email.Voided(); len(voided) != 1 {
		t.Fatalf("captured %d void notices from a double press, want exactly 1", len(voided))
	}
}

// TestPayPhoneIsNeverAskedToReverseWhatTheWindowAlreadyRefuses: the Reversal
// Window is the platform's own rule and is enforced before the provider is
// involved at all. A sale past its cutoff is refused without a single request to
// PayPhone — the boundary is asked only about reversals the platform is willing
// to make.
func TestPayPhoneIsNeverAskedToReverseWhatTheWindowAlreadyRefuses(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Late Paid Fest", "late-paid-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)

	ref, _ := buyOnlineThroughPayPhone(t, "late-paid-fest", gaID, "ana@example.com", 1)
	token := customerSignIn(t, payphoneEnv, "ana@example.com")
	saleID := saleByRef(t, readCustomerArea(t, payphoneEnv, token, ""), ref).ID
	env.email.Reset()

	// 2026-07-07T01:30:00Z is 20:30 on 6 July in Ecuador: past that day's cutoff.
	if _, err := env.db.Exec(
		`UPDATE ticket_sales SET sold_at = $1 WHERE confirmation_ref = $2`,
		time.Date(2026, 7, 7, 1, 30, 0, 0, time.UTC), ref,
	); err != nil {
		t.Fatalf("backdate the sale: %v", err)
	}

	resp, body := reverseSaleRequest(t, payphoneEnv, token, saleID)
	assertRefused(t, resp, body, http.StatusConflict, "REVERSAL_WINDOW_CLOSED")
	assertNothingChanged(t, env, ref, "late-paid-fest", "GA", 9)
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times on a sale past its window, want 0", got)
	}
}
