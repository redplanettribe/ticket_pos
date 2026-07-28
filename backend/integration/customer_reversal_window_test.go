package integration

import (
	"net/http"
	"testing"
	"time"
)

// The Reversal Window as the Customer Area reports it (issue #118, ADR 0018).
//
// This slice is deliberately read-only: there is no reversal action anywhere in
// it. What is proved here is that the listing tells a Customer, per sale, whether
// they could undo it right now and by when — and, just as importantly, that it
// says nothing at all on the sales they could never undo.
//
// Every expectation is anchored to the harness clock, 2026-07-07T12:00:00Z, which
// is 07:00 in Ecuador — a morning purchase, comfortably before the 20:00 cutoff.
// That cutoff is therefore 2026-07-08T01:00:00Z in every test below, because
// America/Guayaquil is UTC-5 with no daylight saving.
const (
	// ecuadorCutoffAfterFixedClock is 20:00 America/Guayaquil on the harness
	// clock's Ecuadorian calendar date, expressed as the instant the API reports.
	ecuadorCutoffAfterFixedClock = "2026-07-08T01:00:00Z"
	testOrgSlug                  = "test-org"
)

// publishEventStarting publishes a sellable Event with a chosen start instant and
// a chosen IANA timezone, then returns it with one Ticket Type.
//
// publishCheckoutEvent already does this for a fixed 72-hours-out Guayaquil
// Event; these tests need the start and the zone to vary, because those are
// precisely the two things the Reversal Window is accused of confusing.
func publishEventStarting(
	t *testing.T, env *testEnv, sessionID, name, slug string,
	startsAt time.Time, timezone string, priceCents, capacity int,
) (eventID, ticketTypeID string) {
	t.Helper()
	eventID = createDraftEvent(t, env, sessionID, name, slug)
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, map[string]any{
		"name":       name,
		"slug":       slug,
		"starts_at":  startsAt.Format(time.RFC3339),
		"timezone":   timezone,
		"venue_name": "The Hall",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	ticketTypeID = createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", priceCents, capacity)
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return eventID, ticketTypeID
}

// buyOnline completes a paid Online Sale end to end and returns its Sale
// Confirmation reference.
func buyOnline(t *testing.T, env *testEnv, eventSlug, ticketTypeID, email string) string {
	t.Helper()
	begun := beginCheckoutOK(t, env, testOrgSlug, eventSlug,
		checkoutBody(email, "Ana", "Lopez", cartLine(ticketTypeID, 1)))
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if settled.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", settled.Status)
	}
	return settled.ConfirmationRef
}

// onlySale returns the Customer's single Ticket Sale from either half of the
// Area, failing when there is not exactly one.
func onlySale(t *testing.T, area customerAreaView) customerAreaSale {
	t.Helper()
	all := append(append([]customerAreaSale{}, area.Upcoming...), area.Past...)
	if len(all) != 1 {
		t.Fatalf("Customer Area holds %d sales, want exactly 1 (%+v)", len(all), all)
	}
	return all[0]
}

// assertNotReversible is the negative contract in one place: no offer, and no
// deadline either. A closing time on a sale nobody may reverse would be a
// promise the platform cannot keep.
func assertNotReversible(t *testing.T, sale customerAreaSale, why string) {
	t.Helper()
	if sale.Reversible {
		t.Fatalf("%s is reported reversible, and must never be", why)
	}
	if sale.ReversalWindowClosesAt != nil {
		t.Fatalf("%s carries reversal_window_closes_at = %q, want null",
			why, *sale.ReversalWindowClosesAt)
	}
}

// TestOnlineSaleReportsItsReversalWindow is the tracer bullet: a paid Online
// Sale bought this morning, for a show three days out, is reversible now and
// says so — with the closing instant being that day's 20:00 in Ecuador.
func TestOnlineSaleReportsItsReversalWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := publishEventStarting(t, env, sessionID, "Window Fest", "window-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", 2500, 10)

	buyOnline(t, env, "window-fest", ticketTypeID, "ana@example.com")

	token := customerSignIn(t, env, "ana@example.com")
	sale := onlySale(t, readCustomerArea(t, env, token, ""))

	if !sale.Reversible {
		t.Fatal("a sale bought this morning for a show in three days is not reported reversible")
	}
	if sale.ReversalWindowClosesAt == nil {
		t.Fatal("reversal_window_closes_at is null on a reversible sale; the Customer cannot be told how long they have")
	}
	if *sale.ReversalWindowClosesAt != ecuadorCutoffAfterFixedClock {
		t.Fatalf("reversal_window_closes_at = %q, want %q — 20:00 Ecuador time on the day of purchase",
			*sale.ReversalWindowClosesAt, ecuadorCutoffAfterFixedClock)
	}
}

// TestFreeOnlineSaleReportsTheSameReversalWindowAsAPaidOne: the window is a
// platform rule, not a provider one. A free claim settled by the platform itself,
// with no Payment Provider anywhere in it and no money to give back, gets
// byte-for-byte the answer the paid purchase above gets.
func TestFreeOnlineSaleReportsTheSameReversalWindowAsAPaidOne(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	startsAt := env.fixedClock.Add(72 * time.Hour)

	_, paidID := publishEventStarting(t, env, sessionID, "Paid Fest", "paid-fest",
		startsAt, "America/Guayaquil", 2500, 10)
	_, freeID := publishEventStarting(t, env, sessionID, "Free Fest", "free-fest",
		startsAt, "America/Guayaquil", 0, 10)

	buyOnline(t, env, "paid-fest", paidID, "ana@example.com")
	approvedRef(t, beginCheckoutSettled(t, env, testOrgSlug, "free-fest", "",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(freeID, 1))))

	token := customerSignIn(t, env, "ana@example.com")
	area := readCustomerArea(t, env, token, "")
	if len(area.Upcoming) != 2 {
		t.Fatalf("upcoming = %d sales, want the paid purchase and the free claim", len(area.Upcoming))
	}

	for _, sale := range area.Upcoming {
		if !sale.Reversible {
			t.Fatalf("sale %s is not reversible; a free Online Sale is reversible on the same terms as a paid one",
				sale.ConfirmationRef)
		}
		if sale.ReversalWindowClosesAt == nil || *sale.ReversalWindowClosesAt != ecuadorCutoffAfterFixedClock {
			t.Fatalf("sale %s closes at %v, want %q for both",
				sale.ConfirmationRef, sale.ReversalWindowClosesAt, ecuadorCutoffAfterFixedClock)
		}
	}
}

// TestReversalWindowEndsAtEventStartOnASameDayShow: a matinee starting at 17:00
// Ecuador time takes three hours off the window, because the platform holds no
// record of attendance and cannot tell a change of mind from a completed visit.
func TestReversalWindowEndsAtEventStartOnASameDayShow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	// 2026-07-07T22:00:00Z is 17:00 in Ecuador on the day of purchase — three
	// hours before the cutoff, so the doors close the window first.
	startsAt := time.Date(2026, 7, 7, 22, 0, 0, 0, time.UTC)
	_, ticketTypeID := publishEventStarting(t, env, sessionID, "Matinee", "matinee",
		startsAt, "America/Guayaquil", 2500, 10)

	buyOnline(t, env, "matinee", ticketTypeID, "ana@example.com")

	token := customerSignIn(t, env, "ana@example.com")
	sale := onlySale(t, readCustomerArea(t, env, token, ""))

	if !sale.Reversible {
		t.Fatal("a sale for a show later today is not reversible; the window has not closed yet")
	}
	want := startsAt.Format(time.RFC3339)
	if sale.ReversalWindowClosesAt == nil || *sale.ReversalWindowClosesAt != want {
		t.Fatalf("reversal_window_closes_at = %v, want the Event start %q — it is earlier than the cutoff",
			sale.ReversalWindowClosesAt, want)
	}
}

// TestReversalWindowIsEcuadorianForAnEventFarFromEcuador is the test the whole
// ticket turns on.
//
// The Event is in Tokyo and starts at 20:00 *Tokyo* time. The cutoff is 20:00
// *Ecuador* time on the buyer's purchase date. Those are two different clocks
// that read the same on a wall, and the window must be governed by the second:
// the deadline reported is the Ecuadorian one, and the Event's own 20:00 — twelve
// days away and fourteen hours out of step — has no bearing on it whatsoever.
func TestReversalWindowIsEcuadorianForAnEventFarFromEcuador(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}
	// 20:00 on 20 July in Tokyo — the same wall-clock hour as the platform
	// cutoff, in a zone that has nothing to do with it.
	startsAt := time.Date(2026, 7, 20, 20, 0, 0, 0, tokyo)
	_, ticketTypeID := publishEventStarting(t, env, sessionID, "Tokyo Fest", "tokyo-fest",
		startsAt, "Asia/Tokyo", 2500, 10)

	buyOnline(t, env, "tokyo-fest", ticketTypeID, "ana@example.com")

	token := customerSignIn(t, env, "ana@example.com")
	sale := onlySale(t, readCustomerArea(t, env, token, ""))

	if !sale.Reversible {
		t.Fatal("a morning purchase for a Tokyo show is not reversible; where the Event is changes nothing")
	}
	if sale.ReversalWindowClosesAt == nil || *sale.ReversalWindowClosesAt != ecuadorCutoffAfterFixedClock {
		t.Fatalf("reversal_window_closes_at = %v, want the Ecuadorian cutoff %q; the Event's timezone must not touch it",
			sale.ReversalWindowClosesAt, ecuadorCutoffAfterFixedClock)
	}
	// The wrong reading — 20:00 on the purchase date in the Event's own zone —
	// would land twenty-five hours earlier, and it must not.
	if wrong := time.Date(2026, 7, 7, 20, 0, 0, 0, tokyo).UTC().Format(time.RFC3339); *sale.ReversalWindowClosesAt == wrong {
		t.Fatalf("the cutoff was computed in the Event's timezone (%s); it is Ecuadorian and only Ecuadorian", wrong)
	}
}

// TestReversalWindowNeverOpensForAPurchaseAfterTheEcuadorCutoff: the Ticket Sale
// is backdated to 20:30 Ecuador time on the previous day, so its window shut
// before it could open. The buyer is told nothing about reversing, and the
// following day's 20:00 is not theirs to inherit.
//
// The purchase instant is moved with SQL because nothing in the public API sells
// a ticket into the past — the checkout always records the sale at the moment it
// settles.
func TestReversalWindowNeverOpensForAPurchaseAfterTheEcuadorCutoff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := publishEventStarting(t, env, sessionID, "Late Fest", "late-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", 2500, 10)

	ref := buyOnline(t, env, "late-fest", ticketTypeID, "ana@example.com")

	// 2026-07-07T01:30:00Z is 20:30 on 6 July in Ecuador: half an hour past that
	// day's cutoff, and still in the past relative to the harness clock.
	lateNight := time.Date(2026, 7, 7, 1, 30, 0, 0, time.UTC)
	if _, err := env.db.Exec(
		`UPDATE ticket_sales SET sold_at = $1 WHERE confirmation_ref = $2`, lateNight, ref,
	); err != nil {
		t.Fatalf("backdate the sale: %v", err)
	}

	token := customerSignIn(t, env, "ana@example.com")
	sale := onlySale(t, readCustomerArea(t, env, token, ""))
	assertNotReversible(t, sale, "a sale bought at 20:30 Ecuador time")
}

// TestAnImportedSaleIsNeverReversible: the Reversal Window belongs to an Online
// Sale. A sale the Organization recorded off-platform was never collected by the
// platform, so there is nothing here to undo, and the Area must not suggest
// otherwise.
func TestAnImportedSaleIsNeverReversible(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	seedSaleForCustomer(t, env, sessionID, "Imported Fest", "imported-fest",
		env.fixedClock.Add(30*24*time.Hour), "rev-import-1", "ana@example.com", "Ana", "Lopez")

	token := customerSignIn(t, env, "ana@example.com")
	sale := onlySale(t, readCustomerArea(t, env, token, ""))
	assertNotReversible(t, sale, "an imported Ticket Sale")
}

// TestAnAlreadyReversedOnlineSaleIsNeverReversible: a Ticket Sale that has been
// undone cannot be undone again, however far from 20:00 it is. It stays visible —
// a reversed sale is never deleted — but it is not on offer.
//
// The reversal is applied with SQL because the reversal action itself does not
// exist yet: this ticket is the read-only half, and #119 builds the write.
func TestAnAlreadyReversedOnlineSaleIsNeverReversible(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, ticketTypeID := publishEventStarting(t, env, sessionID, "Undone Fest", "undone-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", 2500, 10)

	ref := buyOnline(t, env, "undone-fest", ticketTypeID, "ana@example.com")
	if _, err := env.db.Exec(
		`UPDATE ticket_sales SET status = 'reversed' WHERE confirmation_ref = $1`, ref,
	); err != nil {
		t.Fatalf("mark the sale reversed: %v", err)
	}

	token := customerSignIn(t, env, "ana@example.com")
	sale := onlySale(t, readCustomerArea(t, env, token, ""))
	if sale.Status != "reversed" {
		t.Fatalf("sale status = %q, want reversed — the fixture did not take", sale.Status)
	}
	assertNotReversible(t, sale, "an already reversed Ticket Sale")
}
