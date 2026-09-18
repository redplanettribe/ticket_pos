package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// THE UPGRADE'S ELIGIBILITY, PUBLISHED ON THE CHECKOUT PAYLOAD (#648, ADR 0074).
//
// An Upgrade is the buyer's election that a paid Ticket takes the place of a
// free one, and the platform is the sole authority on whether there is a free
// Ticket to elect out of. One predicate decides it, and the Storefront reads the
// answer off the Event page as `surrenderable_free_tickets` — so no surface can
// offer an Upgrade the commit would refuse, because the commit re-evaluates the
// same predicate inside its own transaction.
//
// A free Ticket qualifies only when ALL of these hold: it is on an active Online
// Sale of this Event, that Sale carries exactly one Ticket, the buyer themself
// still holds it as their own accepted Self-held Ticket, and the line was sold
// at zero. Each clause has a test below, and each is a case ADR 0074 argued: a
// free Ticket somebody else accepted is not the buyer's to surrender, and a free
// Sale of several would take a stranger's Ticket down with it.
//
// WHAT IS PUBLISHED IS A COUNT AND NOT A VERDICT, because the basket counts too
// and no basket exists when this page is read. Two free Tickets in play — from
// either side, or one from each — is no offer at all. That arithmetic is
// repository.UpgradeEligibility.OffersUpgrade and is pinned by a unit test
// beside it; what these tests pin is the half the server can see, and the
// privacy line around it.

// surrenderableFreeTickets reads `surrenderable_free_tickets` off the public
// Event page as the holder of a Customer Session, or anonymously when the token
// is empty.
//
// It returns a POINTER because null and 0 are different statements — "we do not
// know who is asking" against "nothing of yours here is surrenderable" — and a
// helper that let one stand for the other would prove nothing about the
// anonymous read. It insists on 200 whatever the token is, exactly as
// readPublicEventAs does: the Event page is public, and a session only adds to
// it.
func surrenderableFreeTickets(t *testing.T, env *testEnv, eventSlug, token string) *int {
	t.Helper()
	var headers map[string]string
	if token != "" {
		headers = authHeader(token)
	}
	resp, body := env.get(t, "/api/v1/public/organizations/"+testOrgSlug+"/events/"+eventSlug, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var detail struct {
		SurrenderableFreeTickets *int `json:"surrenderable_free_tickets"`
	}
	if err := json.Unmarshal(body.Data, &detail); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	return detail.SurrenderableFreeTickets
}

// assertSurrenderable pins the figure a Customer we can identify is owed.
func assertSurrenderable(t *testing.T, got *int, want int, where string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: surrenderable_free_tickets = null, want %d — a Customer we can identify is owed a number", where, want)
	}
	if *got != want {
		t.Fatalf("%s: surrenderable_free_tickets = %d, want %d", where, *got, want)
	}
}

// publishUpgradeEvent is the fixture every test here shares: a published Event
// with a Free Ticket Type and a paid one beside it, which is the only shape an
// Upgrade can happen in — a buyer holding a free Ticket and buying a dearer one
// on the same Event.
func publishUpgradeEvent(t *testing.T, env *testEnv, sessionID, name, slug string) (eventID, freeID, paidID string) {
	t.Helper()
	eventID, freeID = publishCheckoutEvent(t, env, sessionID, name, slug, 0, 20)
	paidID = createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 20)
	return eventID, freeID, paidID
}

// claimFreeTicket runs a zero-total checkout, which ADR 0017 settles inside the
// begin request, and hands back the Ticket Sale it made.
func claimFreeTicket(t *testing.T, env *testEnv, eventSlug, email, first, last, freeID string, quantity int) (saleID, confirmationRef string) {
	t.Helper()
	result := beginCheckoutSettled(t, env, testOrgSlug, eventSlug, buyerSession(t, env, email),
		checkoutBody(email, first, last, cartLine(freeID, quantity)))
	ref := approvedRef(t, result)
	return saleIDOfPayment(t, env, result.ClientTransactionID), ref
}

// TestTheEventPageStatesOneSurrenderableFreeTicket is the tracer bullet: a buyer
// who took a free Ticket loads the Event page, and the platform tells them — and
// only them — that there is exactly one Ticket here they could upgrade out of.
//
// The anonymous read is asserted in the same test because it is the security
// property this figure carries: it is derived from the Customer Session the
// request arrived with and from nothing in the URL, so a visitor nobody has
// identified must be told nothing about anybody.
func TestTheEventPageStatesOneSurrenderableFreeTicket(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	publishUpgradeEvent(t, env, sessionID, "Upgrade Fest", "upgrade-fest")
	_, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Upgrade Fest Two", "upgrade-fest-2")

	ana := buyerSession(t, env, "ana@example.com")
	// Before buying anything: a Customer we can identify, holding nothing.
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "upgrade-fest-2", ana), 0,
		"a signed-in buyer who holds nothing")

	claimFreeTicket(t, env, "upgrade-fest-2", "ana@example.com", "Ana", "Lopez", freeID, 1)

	assertSurrenderable(t, surrenderableFreeTickets(t, env, "upgrade-fest-2", ana), 1,
		"a buyer holding one free Ticket")

	// Nobody else's read says anything about Ana. An anonymous visitor gets null
	// — not 0, which would be a statement about a person the platform has not
	// identified and is exactly the answer an oracle gives.
	if got := surrenderableFreeTickets(t, env, "upgrade-fest-2", ""); got != nil {
		t.Fatalf("anonymous read = %d, want null — this read identified nobody", *got)
	}
	bea := buyerSession(t, env, "bea@example.com")
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "upgrade-fest-2", bea), 0,
		"a different Customer reading the same page")

	// And it is scoped to the Event: Ana's free Ticket on one Event says nothing
	// about another.
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "upgrade-fest", ana), 0,
		"an Event the buyer holds nothing on")
}

// TestAFreeTicketSomebodyElseAcceptedIsNotSurrenderable: the buyer gave the
// Ticket away and the person at the other end clicked. It is no longer theirs to
// give up, and the Sale that carries it is no longer theirs to reverse — which
// is the whole of why the predicate asks who HOLDS the Ticket and not merely who
// bought it.
func TestAFreeTicketSomebodyElseAcceptedIsNotSurrenderable(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Given Fest", "given-fest")

	ana := buyerSession(t, env, "ana@example.com")
	saleID, _ := claimFreeTicket(t, env, "given-fest", "ana@example.com", "Ana", "Lopez", freeID, 1)
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "given-fest", ana), 1, "before it is given away")

	// Through the real assignment flow: the mail goes out and the person at the
	// other end clicks, which is the only way this state is reachable in the
	// product.
	ticketID := selfHeldTicketID(t, env, saleID)
	acceptTicketAs(t, env, ana, saleID, ticketID, "carla@example.com")

	assertSurrenderable(t, surrenderableFreeTickets(t, env, "given-fest", ana), 0,
		"a free Ticket accepted by somebody else")
}

// TestAFreeSaleOfSeveralTicketsIsNotSurrenderable: reversing is whole-Sale and
// always has been, so surrendering one Ticket of a Sale of three would take the
// other two — strangers' Tickets — down with it. Partial reversal is a thing
// this platform deliberately does not have, so such a Sale is simply not
// offered.
func TestAFreeSaleOfSeveralTicketsIsNotSurrenderable(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Bundle Fest", "bundle-fest")

	ana := buyerSession(t, env, "ana@example.com")
	claimFreeTicket(t, env, "bundle-fest", "ana@example.com", "Ana", "Lopez", freeID, 3)

	// One of those three IS the buyer's own accepted Self-held Ticket, so every
	// other clause of the predicate holds. The Sale's size is the only thing
	// refusing it, which is exactly what this test is for.
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "bundle-fest", ana), 0,
		"a free Sale carrying three Tickets")
}

// TestAReversedFreeSaleIsNotSurrenderable: a Sale that no longer stands has
// nothing left to give up.
func TestAReversedFreeSaleIsNotSurrenderable(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Undone Fest", "undone-fest")

	ana := buyerSession(t, env, "ana@example.com")
	_, ref := claimFreeTicket(t, env, "undone-fest", "ana@example.com", "Ana", "Lopez", freeID, 1)
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "undone-fest", ana), 1, "while the Sale stands")

	// Reversed through the product — the buyer signs in and presses Undo —
	// rather than by an UPDATE, so this test breaks if a Sale Reversal ever
	// stops leaving the status the predicate reads.
	undoOwnSale(t, env, "ana@example.com", ref)

	assertSurrenderable(t, surrenderableFreeTickets(t, env, "undone-fest", ana), 0, "once the Sale is reversed")
}

// TestAPaidSaleIsNeverSurrenderableHoweverCheap: the zero boundary, and it is
// the whole reason the rule sits there rather than at "anything cheaper".
// Surrendering a Ticket that cost something means a partial refund and a credit
// note, against a concept this platform does not have — so a Ticket sold for one
// cent is as unsurrenderable as one sold for fifty dollars.
func TestAPaidSaleIsNeverSurrenderableHoweverCheap(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, pennyID := publishCheckoutEvent(t, env, sessionID, "Penny Fest", "penny-fest", 1, 20)

	ana := buyerSession(t, env, "ana@example.com")
	begun := beginCheckoutOK(t, env, testOrgSlug, "penny-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(pennyID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	assertSurrenderable(t, surrenderableFreeTickets(t, env, "penny-fest", ana), 0,
		"a single-Ticket Sale sold for one cent")
}

// TestAFreeTicketOffAStaffChannelIsNotSurrenderable: an Upgrade is the BUYER's
// election, so only a Sale the buyer themself made online can be elected out of.
// A Manually Recorded Sale lands on the `import` channel and seats its buyer
// exactly as an Online Sale does (ADR 0055), so every other clause of the
// predicate holds here — the channel is the only thing refusing it, which is
// what makes this test worth having.
//
// Nothing an Organization transcribed or rang up at the door is the buyer's to
// unsell here: the platform has no evidence the buyer wanted it gone, and the
// reversal would be the Organization's own record being destroyed by somebody
// else's checkout.
func TestAFreeTicketOffAStaffChannelIsNotSurrenderable(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Doorway Fest", "doorway-fest")

	recorded := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", freeID, 1, "cash", "2026-07-01T10:00:00Z"))
	// The Sale really did seat the buyer, or this test would pass on the wrong
	// clause — every other one holds, and the channel must be what refuses it.
	selfHeldTicketID(t, env, recorded.SaleID)

	ana := buyerSession(t, env, "ana@example.com")
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "doorway-fest", ana), 0,
		"a free Ticket recorded by staff on the import channel")
}

// TestTwoQualifyingFreeSalesAreAmbiguous: the buyer has one seat, and a prompt
// that must ask which of two people it concerns has stopped clarifying. The
// payload states the count rather than a verdict, so what it must do here is
// report 2 — and 2 is what makes every surface withhold the offer.
func TestTwoQualifyingFreeSalesAreAmbiguous(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Twice Fest", "twice-fest")

	ana := buyerSession(t, env, "ana@example.com")
	claimFreeTicket(t, env, "twice-fest", "ana@example.com", "Ana", "Lopez", freeID, 1)
	claimFreeTicket(t, env, "twice-fest", "ana@example.com", "Ana", "Lopez", freeID, 1)

	// TWO, and not "the first one". A count above 1 is the ambiguity itself, and
	// the read must publish it rather than pick a winner: the arithmetic that
	// turns it into "no offer" — here, and again with the basket added — is
	// repository.UpgradeEligibility.OffersUpgrade, and it is the only place that
	// decision is written.
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "twice-fest", ana), 2,
		"a buyer holding two qualifying free Tickets")
}

// TestNothingIsSurrenderableWhileTicketAssignmentIsDark: no Ticket is self-held
// in that build, so nothing can be given up and no Upgrade can be performed. The
// answer is 0 to a Customer we can identify and still null to a visitor we
// cannot — the flag darkens the feature, it does not un-identify the reader.
func TestNothingIsSurrenderableWhileTicketAssignmentIsDark(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, freeID, _ := publishUpgradeEvent(t, env, sessionID, "Dark Fest", "dark-fest")

	ana := buyerSession(t, env, "ana@example.com")
	claimFreeTicket(t, env, "dark-fest", "ana@example.com", "Ana", "Lopez", freeID, 1)
	assertSurrenderable(t, surrenderableFreeTickets(t, env, "dark-fest", ana), 1, "with the flag open")

	closeTicketAssignment(t)

	assertSurrenderable(t, surrenderableFreeTickets(t, env, "dark-fest", ana), 0, "with the flag dark")
	if got := surrenderableFreeTickets(t, env, "dark-fest", ""); got != nil {
		t.Fatalf("anonymous read with the flag dark = %d, want null", *got)
	}
}
