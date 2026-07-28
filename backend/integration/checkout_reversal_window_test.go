package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Reversal Window as a GUEST is told it (issue #121, ADR 0018).
//
// Online checkout requires no account, so the person most likely to want an undo
// — the one who has just realised they bought the wrong night — is the one least
// equipped to reach it: reversal itself demands a Customer Session. The guest
// surfaces therefore have to state the deadline and point at the door, which
// means an endpoint a buyer with no session at all can read.
//
// The key it accepts is our own client transaction id, which the Storefront kept
// in an httpOnly cookie for the length of the checkout. It names one attempt, not
// a person: nothing here can be widened into an email or a purchase history, and
// the response is two fields wide by design.
//
// Two things are proved throughout. That the endpoint says nothing on a sale
// nobody may undo — the same silence customer_reversal_window_test.go demands of
// the Customer Area — and that when it does speak, it names the SAME instant the
// Customer Area names for the same sale. Those two surfaces disagreeing about
// somebody's deadline is the failure this file exists to prevent.

// checkoutReversalOffer is the guest-facing response: a boolean and an instant,
// and deliberately nothing else. Read as a map rather than a struct in
// TestCheckoutReversalTellsAGuestNothingAboutTheBuyer, which is where the
// "nothing else" half is pinned.
type checkoutReversalOffer struct {
	Reversible             bool    `json:"reversible"`
	ReversalWindowClosesAt *string `json:"reversal_window_closes_at"`
}

func checkoutReversalPath(clientTransactionID string) string {
	return "/api/v1/public/checkout/" + clientTransactionID + "/reversal"
}

// readCheckoutReversal reads the offer as a guest does: no Authorization header
// anywhere, because a guest has nothing to put in one.
func readCheckoutReversal(t *testing.T, env *testEnv, clientTransactionID string) checkoutReversalOffer {
	t.Helper()
	resp, body := env.get(t, checkoutReversalPath(clientTransactionID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checkout reversal status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("checkout reversal error=%+v, want none", body.Error)
	}
	var offer checkoutReversalOffer
	if err := json.Unmarshal(body.Data, &offer); err != nil {
		t.Fatalf("decode checkout reversal: %v", err)
	}
	return offer
}

// assertNoOfferToGuest is the negative contract: no offer, and no deadline
// either. A closing time on a purchase nobody may undo is worse than silence —
// it is a promise printed on a page nobody can act from.
func assertNoOfferToGuest(t *testing.T, offer checkoutReversalOffer, why string) {
	t.Helper()
	if offer.Reversible {
		t.Fatalf("%s is offered an undo to a guest, and must never be", why)
	}
	if offer.ReversalWindowClosesAt != nil {
		t.Fatalf("%s carries reversal_window_closes_at = %q to a guest, want null",
			why, *offer.ReversalWindowClosesAt)
	}
}

// claimFreeCheckout is claimFree with the client transaction id kept: the guest
// surfaces are keyed on the checkout, not on the Sale Confirmation reference.
func claimFreeCheckout(t *testing.T, env *testEnv, eventSlug, ticketTypeID, email string) freeCheckoutResult {
	t.Helper()
	result := beginCheckoutSettled(t, env, testOrgSlug, eventSlug, "",
		checkoutBody(email, "Ana", "Lopez", cartLine(ticketTypeID, 1)))
	approvedRef(t, result)
	return result
}

// TestCheckoutSuccessLearnsItsReversalWindow is the tracer bullet: seconds after
// a guest claims a ticket, holding nothing but the id of the checkout they just
// completed, they can be told they may undo it and until when.
func TestCheckoutSuccessLearnsItsReversalWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := publishFreeEvent(t, env, sessionID, "Guest Fest", "guest-fest", 10)

	checkout := claimFreeCheckout(t, env, "guest-fest", ticketTypeID, "ana@example.com")

	offer := readCheckoutReversal(t, env, checkout.ClientTransactionID)
	if !offer.Reversible {
		t.Fatal("a claim made this morning for a show in three days is not offered an undo; the guest is left emailing the organizer")
	}
	if offer.ReversalWindowClosesAt == nil {
		t.Fatal("reversal_window_closes_at is null on an offered undo; the page cannot say by when")
	}
	if *offer.ReversalWindowClosesAt != ecuadorCutoffAfterFixedClock {
		t.Fatalf("reversal_window_closes_at = %q, want %q — 20:00 Ecuador time on the day of purchase",
			*offer.ReversalWindowClosesAt, ecuadorCutoffAfterFixedClock)
	}
}

// TestGuestAndCustomerAreaNameTheSameDeadline is what the ticket turns on.
//
// One sale, two surfaces: the page a buyer sees before they have any account,
// and the card they see once they sign in. If those two ever printed different
// hours, one of them would be lying about somebody's money — and the one the
// buyer read first is the one they would act on.
//
// Both a paid purchase and a free claim are checked, because the two settle
// through entirely different code (a provider round trip against an in-request
// settlement) and only the shared rule underneath makes them agree.
func TestGuestAndCustomerAreaNameTheSameDeadline(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	startsAt := env.fixedClock.Add(72 * time.Hour)
	_, paidID := publishEventStarting(t, env, sessionID, "Paid Fest", "paid-fest",
		startsAt, "America/Guayaquil", 2500, 10)
	_, freeID := publishEventStarting(t, env, sessionID, "Free Fest", "free-fest",
		startsAt, "America/Guayaquil", 0, 10)

	begun := beginCheckoutOK(t, env, testOrgSlug, "paid-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(paidID, 1)))
	paidRef := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved").ConfirmationRef
	freeCheckout := claimFreeCheckout(t, env, "free-fest", freeID, "ana@example.com")

	token := customerSignIn(t, env, "ana@example.com")
	area := readCustomerArea(t, env, token, "")

	for _, tc := range []struct {
		name                string
		clientTransactionID string
		confirmationRef     string
	}{
		{"paid purchase", begun.ClientTransactionID, paidRef},
		{"free claim", freeCheckout.ClientTransactionID, *freeCheckout.ConfirmationRef},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guest := readCheckoutReversal(t, env, tc.clientTransactionID)
			signedIn := saleByRef(t, area, tc.confirmationRef)

			if guest.Reversible != signedIn.Reversible {
				t.Fatalf("guest is offered an undo=%t while the Customer Area says %t for the same sale",
					guest.Reversible, signedIn.Reversible)
			}
			if guest.ReversalWindowClosesAt == nil || signedIn.ReversalWindowClosesAt == nil {
				t.Fatalf("a deadline is missing: guest=%v area=%v",
					guest.ReversalWindowClosesAt, signedIn.ReversalWindowClosesAt)
			}
			if *guest.ReversalWindowClosesAt != *signedIn.ReversalWindowClosesAt {
				t.Fatalf("the guest is told %q and the Customer Area %q for one sale; one of the two is lying about the buyer's deadline",
					*guest.ReversalWindowClosesAt, *signedIn.ReversalWindowClosesAt)
			}
			if *guest.ReversalWindowClosesAt != ecuadorCutoffAfterFixedClock {
				t.Fatalf("both surfaces agree on %q, but the rule says %q",
					*guest.ReversalWindowClosesAt, ecuadorCutoffAfterFixedClock)
			}
		})
	}
}

// TestAConfirmationLinkRevealsTheDeadlineAndNoMore covers the second guest
// surface (#121): the page a buyer returns to from their confirmation email when
// they reconsider.
//
// It is backed by the Customer Area read under a sale-scoped session, so the
// window fields it needs are the Customer Area's own — the same instant, from
// the same rule, for the same sale. That is asserted here against a full session
// on the same purchase, because "the link page shows what the Area shows" is the
// property the Storefront then draws.
//
// What the link must NOT gain is an action. The session it mints reads one sale
// and cannot undo it, and that refusal is asserted here rather than only in
// #119's file, because this ticket is exactly the one that puts a deadline in
// front of somebody holding a forwarded email.
func TestAConfirmationLinkRevealsTheDeadlineAndNoMore(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := publishFreeEvent(t, env, sessionID, "Link Fest", "link-fest", 10)

	ref := claimFree(t, env, "link-fest", ticketTypeID, "ana@example.com", 1)

	_, linkToken := redeemConfirmationLinkOK(t, env, lastConfirmationLinkToken(t, env), "")
	viaLink := onlySale(t, readCustomerArea(t, env, linkToken, ""))

	if !viaLink.Reversible || viaLink.ReversalWindowClosesAt == nil {
		t.Fatal("a Confirmation Link opens a sale inside its Reversal Window and is told nothing about undoing it")
	}
	if *viaLink.ReversalWindowClosesAt != ecuadorCutoffAfterFixedClock {
		t.Fatalf("the link page is told %q, want %q", *viaLink.ReversalWindowClosesAt, ecuadorCutoffAfterFixedClock)
	}

	// The same sale, read by the Customer who owns it: one deadline, two doors.
	signedIn := saleByRef(t, readCustomerArea(t, env, customerSignIn(t, env, "ana@example.com"), ""), ref)
	if signedIn.ReversalWindowClosesAt == nil ||
		*signedIn.ReversalWindowClosesAt != *viaLink.ReversalWindowClosesAt {
		t.Fatalf("the Confirmation Link says %q and the Customer Area %v for one sale",
			*viaLink.ReversalWindowClosesAt, signedIn.ReversalWindowClosesAt)
	}

	// And the link still cannot act. Revealing the deadline is the whole of what
	// it gained; a forwarded email must not be able to release somebody's
	// tickets (ADR 0018).
	resp, body := reverseSaleRequest(t, env, linkToken, viaLink.ID)
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a Confirmation Link session undid a purchase; the link is a read credential and nothing more")
	}
	if body.Error == nil {
		t.Fatalf("undo from a link session returned %d with no error", resp.StatusCode)
	}

	if status, _, _ := saleProvenance(t, env, ref); status != "active" {
		t.Fatalf("the Ticket Sale is %q after a refused undo from a link session", status)
	}
}

// TestGuestDeadlineFollowsTheEventStartOnASameDayShow: the rule is not "20:00"
// but the earlier of 20:00 Ecuador time and the doors opening, and the guest
// surface must be governed by the whole of it rather than by the half that is
// easy to reproduce in a client.
func TestGuestDeadlineFollowsTheEventStartOnASameDayShow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// 17:00 in Ecuador on the day of purchase — three hours before the cutoff.
	startsAt := time.Date(2026, 7, 7, 22, 0, 0, 0, time.UTC)
	_, ticketTypeID := publishEventStarting(t, env, sessionID, "Matinee", "guest-matinee",
		startsAt, "America/Guayaquil", 0, 10)

	checkout := claimFreeCheckout(t, env, "guest-matinee", ticketTypeID, "ana@example.com")

	offer := readCheckoutReversal(t, env, checkout.ClientTransactionID)
	want := startsAt.Format(time.RFC3339)
	if offer.ReversalWindowClosesAt == nil || *offer.ReversalWindowClosesAt != want {
		t.Fatalf("guest is told %v, want the Event start %q — it comes before the cutoff",
			offer.ReversalWindowClosesAt, want)
	}
}

// TestCheckoutReversalIsSilentWhenThereIsNoUndoOnOffer walks every way the
// answer is "say nothing".
//
// The acceptance criterion is stated as a prohibition — neither guest page
// mentions undo once the window has closed, or on a sale that was never eligible
// — so each case has to come back with both fields empty rather than merely with
// reversible false.
func TestCheckoutReversalIsSilentWhenThereIsNoUndoOnOffer(t *testing.T) {
	t.Run("a checkout nobody ever began", func(t *testing.T) {
		env := setupTest(t)
		assertNoOfferToGuest(t, readCheckoutReversal(t, env, "no-such-client-transaction"),
			"an id that names no checkout")
	})

	t.Run("a Payment still pending", func(t *testing.T) {
		env := setupTest(t)
		sessionID := orgAdminSession(t, env)
		_, ticketTypeID := publishEventStarting(t, env, sessionID, "Pending Fest", "pending-fest",
			env.fixedClock.Add(72*time.Hour), "America/Guayaquil", 2500, 10)

		// Begun and never confirmed: there is no Ticket Sale yet, so there is
		// nothing to undo and nothing to promise.
		begun := beginCheckoutOK(t, env, testOrgSlug, "pending-fest",
			checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 1)))

		assertNoOfferToGuest(t, readCheckoutReversal(t, env, begun.ClientTransactionID),
			"a checkout that never settled")
	})

	t.Run("a purchase made after the Ecuadorian cutoff", func(t *testing.T) {
		env := setupTest(t)
		sessionID := orgAdminSession(t, env)
		_, ticketTypeID := publishFreeEvent(t, env, sessionID, "Late Fest", "guest-late-fest", 10)

		checkout := claimFreeCheckout(t, env, "guest-late-fest", ticketTypeID, "ana@example.com")

		// 20:30 on 6 July in Ecuador: half an hour past that day's cutoff, so the
		// window shut before it could open.
		lateNight := time.Date(2026, 7, 7, 1, 30, 0, 0, time.UTC)
		if _, err := env.db.Exec(
			`UPDATE ticket_sales SET sold_at = $1 WHERE confirmation_ref = $2`,
			lateNight, *checkout.ConfirmationRef,
		); err != nil {
			t.Fatalf("backdate the sale: %v", err)
		}

		assertNoOfferToGuest(t, readCheckoutReversal(t, env, checkout.ClientTransactionID),
			"a claim made at 20:30 Ecuador time")
	})

	t.Run("a purchase the buyer has already undone", func(t *testing.T) {
		env := setupTest(t)
		sessionID := orgAdminSession(t, env)
		_, ticketTypeID := publishFreeEvent(t, env, sessionID, "Undone Fest", "undone-fest", 10)

		checkout := claimFreeCheckout(t, env, "undone-fest", ticketTypeID, "ana@example.com")
		undoOwnSale(t, env, "ana@example.com", *checkout.ConfirmationRef)

		// The confirmation email's link still works and still opens this sale;
		// what it must no longer do is offer a second undo.
		assertNoOfferToGuest(t, readCheckoutReversal(t, env, checkout.ClientTransactionID),
			"a purchase already reversed")
	})
}

// TestCheckoutReversalTellsAGuestNothingAboutTheBuyer pins the response's whole
// shape, because this is the one Reversal Window surface with no credential in
// front of it.
//
// A deadline and a boolean say nothing about who bought what: an id guessed or
// forwarded reveals no more than a clock does. An email, a name or a
// confirmation reference appearing here later would quietly turn a read of the
// window into a lookup of a person, so the field set is asserted exactly rather
// than loosely.
func TestCheckoutReversalTellsAGuestNothingAboutTheBuyer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := publishFreeEvent(t, env, sessionID, "Quiet Fest", "quiet-fest", 10)

	checkout := claimFreeCheckout(t, env, "quiet-fest", ticketTypeID, "ana@example.com")

	resp, body := env.get(t, checkoutReversalPath(checkout.ClientTransactionID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checkout reversal status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var fields map[string]any
	if err := json.Unmarshal(body.Data, &fields); err != nil {
		t.Fatalf("decode checkout reversal payload: %v", err)
	}
	want := map[string]bool{"reversible": true, "reversal_window_closes_at": true}
	for name := range fields {
		if !want[name] {
			t.Fatalf("the unauthenticated Reversal Window read returned %q; it may carry the window and nothing about the buyer", name)
		}
	}
	if len(fields) != len(want) {
		t.Fatalf("payload = %v, want exactly %v", fields, want)
	}
}

// TestAGuestCannotUndoAnythingFromTheCheckoutSurface is the read-only guarantee
// stated as a test.
//
// The whole reason reversal sits behind a Customer Session is that a
// Confirmation Link travels by email and gets forwarded (ADR 0018). A guest
// surface keyed on a client transaction id has exactly the same weakness, so it
// reveals the deadline and never an action: this path answers GET and refuses
// every verb that could change anything.
func TestAGuestCannotUndoAnythingFromTheCheckoutSurface(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := publishFreeEvent(t, env, sessionID, "Readonly Fest", "readonly-fest", 10)

	checkout := claimFreeCheckout(t, env, "readonly-fest", ticketTypeID, "ana@example.com")
	path := checkoutReversalPath(checkout.ClientTransactionID)

	// Raw requests rather than the envelope helpers: a refusal at the router
	// never reaches a handler and so never produces an envelope to decode, which
	// is itself the point — there is no code behind these verbs at all.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, err := http.NewRequest(method, env.server.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s returned %d, want 405; the guest Reversal Window surface must answer reads only",
				method, path, resp.StatusCode)
		}
	}

	// And the sale is untouched by all of that: a refused verb must also be a
	// verb that changed nothing.
	if status, _, _ := saleProvenance(t, env, *checkout.ConfirmationRef); status != "active" {
		t.Fatalf("the Ticket Sale is %q after those requests; something on this path mutated a sale", status)
	}
	if !readCheckoutReversal(t, env, checkout.ClientTransactionID).Reversible {
		t.Fatal("the undo is no longer on offer after read-only requests")
	}
}
