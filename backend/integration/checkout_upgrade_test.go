package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// THE UPGRADE, SAME BASKET: THE FREE LINE IS NEVER BOUGHT (#649, ADR 0074).
//
// An Upgrade is the buyer's election that a paid Ticket takes the place of a
// free one. Where both are in the SAME cart there is nothing to undo: the free
// line is dropped before the Ticket Sale is written, so no Ticket is minted for
// it, no capacity is consumed, and the Sale that exists afterwards is the one
// the buyer meant. The dropped line was worth zero, so the total, the Payment
// and the Tax Invoice are what the un-upgraded basket would have produced.
//
// THE ELECTION IS A CHECKBOX AND THE PLATFORM IS THE AUTHORITY. `upgrade_elected`
// rides the checkout body — the begin leg, which settles a free cart on the
// spot, and the confirm leg, which settles a paid one on the way back from the
// Payment Provider. Nothing on the way in validates it. Eligibility is
// re-evaluated INSIDE the transaction that commits the sale, and an election the
// platform did not offer is IGNORED RATHER THAN REFUSED — which is the single
// most important behaviour here. A stale tab, a replayed body or a forged one
// must never surrender somebody's Ticket, and must never fail a payment either.
//
// THE SEAM IS THE HTTP API AS A CUSTOMER MEETS IT: the session-gated
// begin-checkout, the public confirm, the buyer's own Sale page (`self_held`),
// and the staff Sales list (which lines were bought, for how much). SQL is used
// only for the Payment row, which no surface exposes.

// electing adds the Upgrade Prompt's answer to a checkout body.
//
// It is `true` and never `false` because false is what every other body in this
// package already says: the field is absent from them, and absence means keep
// both. Writing the default out would test a spelling rather than a decision.
func electing(body map[string]any) map[string]any {
	body["upgrade_elected"] = true
	return body
}

// confirmElectingUpgrade settles a Payment on the provider's return leg while
// relaying the buyer's Upgrade election, the way the Storefront return handler
// carries it back across the redirect.
func confirmElectingUpgrade(t *testing.T, env *testEnv, clientTransactionID string) confirmCheckoutResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/public/checkout/"+clientTransactionID+"/confirm", map[string]any{
		"provider_params": payphoneReturnParams(clientTransactionID),
		"upgrade_elected": true,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status=%d error=%+v, want 200 — an election may never fail a payment", resp.StatusCode, body.Error)
	}
	var result confirmCheckoutResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode confirm result: %v", err)
	}
	if result.Status != "approved" || result.ConfirmationRef == "" {
		t.Fatalf("confirm = %+v, want an approved Sale with a reference", result)
	}
	return result
}

// saleOf reads the one Ticket Sale an Event has for a buyer off the staff Sales
// list — the surface that states which Ticket Types a Sale carries and for how
// much, which is exactly what an Upgrade changes.
func saleOf(t *testing.T, env *testEnv, sessionID, eventID, email string) saleListRow {
	t.Helper()
	var found []saleListRow
	for _, row := range eventSales(t, env, sessionID, eventID) {
		if row.CustomerEmail == email && row.Status == "active" {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s has %d active Ticket Sales on this Event, want exactly 1", email, len(found))
	}
	return found[0]
}

// assertLines pins the Ticket Types a Sale actually carries, by name and
// quantity, against what the cart asked for.
func assertLines(t *testing.T, sale saleListRow, want map[string]int, where string) {
	t.Helper()
	got := map[string]int{}
	for _, line := range sale.TicketTypes {
		got[line.TicketTypeName] += line.Quantity
	}
	if len(got) != len(want) {
		t.Fatalf("%s: Sale carries %v, want %v", where, got, want)
	}
	for name, quantity := range want {
		if got[name] != quantity {
			t.Fatalf("%s: Sale carries %v, want %v", where, got, want)
		}
	}
}

// mixedBasket is the only cart shape an Upgrade can happen in: one free Ticket
// and one paid one, bought together.
func mixedBasket(email, freeID, paidID string) map[string]any {
	return checkoutBody(email, "Ana", "Lopez", cartLine(freeID, 1), cartLine(paidID, 1))
}

// TestSameBasketUpgradeBuysThePaidLineAlone is the tracer bullet. A buyer puts a
// giveaway and a VIP Ticket in one cart, ticks the Upgrade Prompt, and pays: the
// Sale that comes out holds the VIP line alone, charged what it always would
// have been, and the buyer is seated on the Ticket they paid for.
//
// The giveaway's sold_count is the capacity assertion. The Payment's Capacity
// Hold covered both lines while the buyer was at the provider; the line that was
// never bought converts into nothing, so the Ticket Type is exactly as available
// as before somebody nearly took one.
func TestSameBasketUpgradeBuysThePaidLineAlone(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	// The buyer's own Ticket list is the surface that says `self_held`, and it is
	// served behind the Ticket Questions flag.
	enableTicketQuestions(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, paidID := publishUpgradeEvent(t, env, sessionID, "Upgrade Fest", "upgrade-fest")

	ana := buyerSession(t, env, "ana@example.com")
	begun := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "upgrade-fest", ana,
		electing(mixedBasket("ana@example.com", freeID, paidID)))
	confirmElectingUpgrade(t, payphoneEnv, begun.ClientTransactionID)

	// The same basket again, from somebody who elected nothing. The two Sales
	// carry different Tickets and must be charged the same to the cent: the
	// dropped line was worth zero, so the total, the Platform Fee arithmetic and
	// the Tax Invoice they all feed are what the un-upgraded basket produced.
	// That identity is the boundary ADR 0074 drew, and it is the reason an
	// Upgrade may happen inside a payment's own transaction at all.
	bruno := buyerSession(t, env, "bruno@example.com")
	kept := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "upgrade-fest", bruno,
		checkoutBody("bruno@example.com", "Bruno", "Diaz", cartLine(freeID, 1), cartLine(paidID, 1)))
	confirmCheckoutParams(t, payphoneEnv, kept.ClientTransactionID, payphoneReturnParams(kept.ClientTransactionID))
	if begun.AmountCents != kept.AmountCents {
		t.Fatalf("the upgraded basket was quoted %d and the un-upgraded one %d; a line worth zero may not change a total",
			begun.AmountCents, kept.AmountCents)
	}

	sale := saleOf(t, env, sessionID, eventID, "ana@example.com")
	assertLines(t, sale, map[string]int{"VIP": 1}, "after an elected Upgrade")
	if sale.AmountCents != begun.AmountCents {
		t.Fatalf("Sale amount_cents = %d, Payment %d; the dropped line was worth zero", sale.AmountCents, begun.AmountCents)
	}
	if other := saleOf(t, env, sessionID, eventID, "bruno@example.com"); sale.AmountCents != other.AmountCents {
		t.Fatalf("the upgraded Sale totals %d and the un-upgraded one %d", sale.AmountCents, other.AmountCents)
	}
	if got := selfHeldTypeName(t, env, ana, sale.ID); got != "VIP" {
		t.Fatalf("the buyer is seated on %q, want VIP — the Ticket they paid for", got)
	}
	// The capacity assertion, and the only Ticket counted against the giveaway is
	// the one Bruno actually bought. Ana's Payment held a giveaway for the whole
	// time she was at the provider; a line that is never bought converts into
	// nothing, so the hold lapses with the settlement and leaves no trace.
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 1 {
		t.Fatalf("the giveaway's sold_count = %d, want 1 — only the basket that kept its free line consumed capacity", got)
	}
	if got := soldCount(t, env, sessionID, eventID, paidID); got != 2 {
		t.Fatalf("VIP sold_count = %d, want 2", got)
	}
}

// TestDecliningTheUpgradeBuysBothLines is the other half of the same cart, and
// the default: a buyer who scrolled past the prompt gets both Tickets and is
// seated on the dearer one (#646). Keep-both is the recoverable answer, which is
// why silence falls to it.
func TestDecliningTheUpgradeBuysBothLines(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	// As above: `self_held` is read off the buyer's own Ticket list.
	enableTicketQuestions(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, paidID := publishUpgradeEvent(t, env, sessionID, "Keep Both Fest", "keep-both-fest")

	ana := buyerSession(t, env, "ana@example.com")
	begun := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "keep-both-fest", ana,
		mixedBasket("ana@example.com", freeID, paidID))
	confirmCheckoutParams(t, payphoneEnv, begun.ClientTransactionID, payphoneReturnParams(begun.ClientTransactionID))

	sale := saleOf(t, env, sessionID, eventID, "ana@example.com")
	assertLines(t, sale, map[string]int{"GA": 1, "VIP": 1}, "with no Upgrade elected")
	if got := selfHeldTypeName(t, env, ana, sale.ID); got != "VIP" {
		t.Fatalf("the buyer is seated on %q, want VIP — the dearest line of the Sale", got)
	}
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 1 {
		t.Fatalf("the giveaway's sold_count = %d, want 1 — nobody elected anything", got)
	}
}

// TestAnElectionTheBackendDidNotOfferIsIgnored is the behaviour this ticket
// exists to guarantee, and it is asserted on the shape that makes the mistake
// most tempting: a buyer whose cart holds a free Ticket AND who already holds a
// free Ticket from an earlier Sale. Two free Tickets are in play, so no Upgrade
// was ever offered — the Event page says so — and a body claiming one anyway
// must change nothing at all.
//
// The three things it must not do are each asserted: it must not fail the
// payment, it must not drop the basket's free line, and it must not touch the
// earlier Sale (which is the other mechanism's territory and is not reached from
// here on any path).
func TestAnElectionTheBackendDidNotOfferIsIgnored(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, paidID := publishUpgradeEvent(t, env, sessionID, "Ambiguous Fest", "ambiguous-fest")

	ana := buyerSession(t, env, "ana@example.com")
	earlierSaleID, _ := claimFreeTicket(t, env, "ambiguous-fest", "ana@example.com", "Ana", "Lopez", freeID, 1)
	// One free Ticket on file and another in the cart is exactly as ambiguous as
	// holding two, and the platform withholds the offer.
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "ambiguous-fest", ana), 1, "before the second basket")

	begun := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "ambiguous-fest", ana,
		electing(mixedBasket("ana@example.com", freeID, paidID)))
	confirmElectingUpgrade(t, payphoneEnv, begun.ClientTransactionID)

	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	for _, row := range eventSales(t, env, sessionID, eventID) {
		if row.ID == saleID {
			assertLines(t, row, map[string]int{"GA": 1, "VIP": 1}, "an election nobody offered")
		}
		if row.ID == earlierSaleID && row.Status != "active" {
			t.Fatalf("the earlier free Sale reads %q; an unoffered election may not reverse anything", row.Status)
		}
	}
	if got := salesCountByEmail(t, env, eventID, "ana@example.com"); got != 2 {
		t.Fatalf("the buyer has %d active Sales, want 2 — both stand", got)
	}
}

// TestAnElectionOverTwoFreeTicketsInOneBasketIsIgnored pins the ambiguity rule
// on the side this ticket owns. Two giveaways in one cart is not "upgrade one of
// them": a prompt that must ask which of several Tickets it is about has stopped
// clarifying, so nothing is offered and nothing is dropped.
func TestAnElectionOverTwoFreeTicketsInOneBasketIsIgnored(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, paidID := publishUpgradeEvent(t, env, sessionID, "Two Free Fest", "two-free-fest")

	ana := buyerSession(t, env, "ana@example.com")
	begun := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "two-free-fest", ana,
		electing(checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(freeID, 2), cartLine(paidID, 1))))
	confirmElectingUpgrade(t, payphoneEnv, begun.ClientTransactionID)

	assertLines(t, saleOf(t, env, sessionID, eventID, "ana@example.com"),
		map[string]int{"GA": 2, "VIP": 1}, "two free Tickets in one basket")
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 2 {
		t.Fatalf("the giveaway's sold_count = %d, want 2", got)
	}
}

// TestAnElectionWithNothingPaidInTheBasketIsIgnored is the precondition that is
// this surface's alone: an Upgrade is free to paid, so a cart with nothing paid
// in it has nothing to move onto. Such a cart totals zero, which means it
// settles inside the begin request (ADR 0017) — so this is also the proof that
// the begin leg reads the field and refuses nothing over it.
func TestAnElectionWithNothingPaidInTheBasketIsIgnored(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Free Only Fest", "free-only-fest")

	ana := buyerSession(t, env, "ana@example.com")
	claim := beginCheckoutSettled(t, env, testOrgSlug, "free-only-fest", ana,
		electing(checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(freeID, 1))))
	approvedRef(t, claim)

	assertLines(t, saleOf(t, env, sessionID, eventID, "ana@example.com"),
		map[string]int{"GA": 1}, "an election with nothing paid to upgrade to")
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 1 {
		t.Fatalf("the giveaway's sold_count = %d, want 1 — the buyer still claimed it", got)
	}
}

// TestNoUpgradeIsPerformedWhileTicketAssignmentIsDark pins the gate. Nothing is
// seated in a dark build, so nothing is surrenderable in one: a deployment that
// ships this code before the flag opens must sell both lines however the body
// reads, and the checkbox must not become a way to destroy a Ticket in a build
// that cannot even say who holds it.
func TestNoUpgradeIsPerformedWhileTicketAssignmentIsDark(t *testing.T) {
	env := setupTest(t)
	// Deliberately no enableTicketAssignment: setupTest leaves the flag closed,
	// which is the shipped state this test is about.
	sessionID := orgAdminSession(t, env)
	eventID, freeID, paidID := publishUpgradeEvent(t, env, sessionID, "Dark Fest", "dark-fest")

	ana := buyerSession(t, env, "ana@example.com")
	begun := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "dark-fest", ana,
		electing(mixedBasket("ana@example.com", freeID, paidID)))
	confirmElectingUpgrade(t, payphoneEnv, begun.ClientTransactionID)

	assertLines(t, saleOf(t, env, sessionID, eventID, "ana@example.com"),
		map[string]int{"GA": 1, "VIP": 1}, "an election in a dark build")
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 1 {
		t.Fatalf("the giveaway's sold_count = %d, want 1", got)
	}
}

// TestUpgradeEligibilityIsReEvaluatedAtCommit is the difference between reading
// the payload and asking the database, and the only way to see it is to change
// the answer while the buyer is away at the Payment Provider.
//
// The buyer begins a mixed basket with the Upgrade elected, and at that instant
// they qualify: one free Ticket in play, theirs, in this very cart. Then — in
// another tab, before the provider answers — they claim a second free Ticket.
// Two are now in play, the offer is withdrawn, and the commit must see that
// rather than the world the checkout dialog was drawn against. Both lines are
// bought and the payment succeeds.
func TestUpgradeEligibilityIsReEvaluatedAtCommit(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, paidID := publishUpgradeEvent(t, env, sessionID, "Race Fest", "race-fest")

	ana := buyerSession(t, env, "ana@example.com")
	// At begin the buyer holds nothing: the one free Ticket in play is the one
	// in this cart, and the Upgrade is genuinely on offer.
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "race-fest", ana), 0, "at begin")
	begun := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "race-fest", ana,
		electing(mixedBasket("ana@example.com", freeID, paidID)))

	// And then, while they are at the provider, they take another giveaway.
	claimFreeTicket(t, env, "race-fest", "ana@example.com", "Ana", "Lopez", freeID, 1)
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "race-fest", ana), 1, "while the buyer is at the provider")

	confirmElectingUpgrade(t, payphoneEnv, begun.ClientTransactionID)

	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	for _, row := range eventSales(t, env, sessionID, eventID) {
		if row.ID == saleID {
			assertLines(t, row, map[string]int{"GA": 1, "VIP": 1}, "an election overtaken by a second free Ticket")
		}
	}
	if got := soldCount(t, env, sessionID, eventID, freeID); got != 2 {
		t.Fatalf("the giveaway's sold_count = %d, want 2 — the second claim and the line nobody dropped", got)
	}
}

// TestAnUpgradedHouseBasketStillOwesItsSaleInvoice is the invoicing seam, and
// the reason it is worth a test of its own is the shape of the failure it
// guards: the document is owed INSIDE the commit transaction, from the lines the
// commit wrote, and a seam still describing the submitted cart would look for a
// line that was never written — failing the commit of a Payment the provider had
// already approved, which is the one incident this platform resolves by hand.
//
// The money is untouched, which is the whole of why an Upgrade may happen here
// at all: the dropped line was worth zero, so the factura's total is what the
// un-upgraded basket would have produced.
func TestAnUpgradedHouseBasketStillOwesItsSaleInvoice(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, testOrgSlug)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	eventID, freeID, paidID := publishUpgradeEvent(t, env, adminSessionID, "House Upgrade Fest", "house-upgrade-fest")

	ana := buyerSession(t, env, "ana@example.com")
	begun := beginCheckoutAsOK(t, payphoneEnv, testOrgSlug, "house-upgrade-fest", ana,
		electing(mixedBasket("ana@example.com", freeID, paidID)))
	confirmElectingUpgrade(t, payphoneEnv, begun.ClientTransactionID)

	sale := saleOf(t, env, adminSessionID, eventID, "ana@example.com")
	assertLines(t, sale, map[string]int{"VIP": 1}, "an upgraded House basket")

	list := getSaleInvoiceList(t, operatorSessionID)
	if list.Pagination.Total != 1 || len(list.Data) != 1 {
		t.Fatalf("invoices after an upgraded paid House checkout = %d rows (total %d); want exactly one",
			len(list.Data), list.Pagination.Total)
	}
	if row := list.Data[0]; row.Kind != "sale" || row.Status != "owed" {
		t.Fatalf("row kind/status = %s/%s; want sale/owed", row.Kind, row.Status)
	}
}
