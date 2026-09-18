package integration

import (
	"database/sql"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Upgrade's cross-Sale half (#650, parent #645, ADR 0074): a buyer who
// already holds a free Ticket buys a paid one and elects to MOVE UP rather than
// to hold both. The free Ticket Sale is reversed inside the very transaction
// that commits the paid one, and the buyer is told nothing about it.
//
// WHAT THESE TESTS ARE REALLY ABOUT IS THE INSTANT. Almost every assertion below
// is about WHEN the free Sale goes, not whether: it must survive an abandoned
// checkout, a declined card and a Payment left to expire, and it must be gone by
// the time the paid Sale exists. A version of this feature that surrendered the
// Ticket at begin-checkout would pass a happy-path test and fail every other one
// in this file, which is the whole reason they are here.
//
// THE SILENCE IS ASSERTED, NOT ASSUMED. Both mails the ordinary reversal routes
// send — the Sale Voided notice from the sales service and the No Longer Holding
// mail from catalog — are read off the capturing sender and required to be
// absent, beside the paid Sale's Sale Confirmation, which must be there.
//
// SQL reads what no surface publishes: the replacement link and its reason are
// #651's to display, and until that lands the columns themselves are the only
// place the marker can be seen.

// crossSaleUpgrade is one buyer, one Event with a free and a paid Ticket Type,
// and a free Ticket Sale already made and held.
//
// It is the state every test in this file starts from, because it is the state
// the feature exists for: the buyer has something to give up.
type crossSaleUpgrade struct {
	env        *testEnv
	staff      string
	eventID    string
	eventSlug  string
	freeTypeID string
	paidTypeID string
	buyer      string
	// freeSaleID is the Ticket Sale an Upgrade would reverse, and freeTicketID
	// the Ticket on it the buyer holds as their own.
	freeSaleID   string
	freeRef      string
	freeTicketID string
}

func newCrossSaleUpgrade(t *testing.T) crossSaleUpgrade {
	t.Helper()
	env := setupTest(t)
	// Nothing is seated in a dark build, so nothing is surrenderable and no
	// Upgrade can be performed. Every test here needs the flag open.
	enableTicketAssignment(t)

	staff := orgAdminSession(t, env)
	eventID, paidTypeID := publishCheckoutEvent(t, env, staff, "Upgrade Night", "upgrade-night", 3000, 20)
	freeTypeID := createTicketTypeWithCapacity(t, env, staff, eventID, "Community", 0, 20)

	buyer := "ada@example.com"
	claim := beginCheckoutSettled(t, env, "test-org", "upgrade-night", buyerSession(t, env, buyer),
		checkoutBody(buyer, "Ada", "Byron", cartLine(freeTypeID, 1)))
	ref := approvedRef(t, claim)
	freeSaleID := saleIDOfPayment(t, env, claim.ClientTransactionID)

	tickets := ticketIDsOfSale(t, env, freeSaleID)
	if len(tickets) != 1 {
		t.Fatalf("the free claim minted %d Tickets, want 1 — eligibility admits single-Ticket Sales only", len(tickets))
	}
	// The eligibility predicate requires the buyer to hold it THEMSELVES and to
	// have accepted. An Online Sale seats its buyer in the commit, so this is a
	// precondition check rather than an act — if it ever stops being true, every
	// test below would pass for the wrong reason.
	assertBuyerHolds(t, env, tickets[0], buyer)

	// The free claim's own Sale Confirmation is not this file's subject, and
	// leaving it captured would make every "nothing was sent" assertion below
	// read one message it was never about.
	env.email.Reset()

	return crossSaleUpgrade{
		env: env, staff: staff,
		eventID: eventID, eventSlug: "upgrade-night",
		freeTypeID: freeTypeID, paidTypeID: paidTypeID,
		buyer:      buyer,
		freeSaleID: freeSaleID, freeRef: ref, freeTicketID: tickets[0],
	}
}

// buyPaid begins a paid checkout for this buyer, electing the Upgrade or not,
// and returns the client transaction id WITHOUT confirming it.
//
// The two halves are separate on purpose: the tests that matter most are the
// ones that never reach a confirm.
func (f crossSaleUpgrade) buyPaid(t *testing.T, elected bool) string {
	t.Helper()
	body := checkoutBody(f.buyer, "Ada", "Byron", cartLine(f.paidTypeID, 1))
	body["upgrade_elected"] = elected
	begun := beginCheckoutAsOK(t, f.env, "test-org", f.eventSlug, buyerSession(t, f.env, f.buyer), body)
	return begun.ClientTransactionID
}

// --- what the API does not publish -----------------------------------------

// saleProvenanceRow is a Ticket Sale's status, reversal provenance and
// replacement linkage as stored. None of it is on a public surface, and the
// replacement reason is not on a staff one either until #651.
type saleProvenanceRow struct {
	Status         string
	ReversedBy     sql.NullString
	ReplacedBy     sql.NullString
	Replaces       sql.NullString
	ReplacementFor sql.NullString
}

func readSaleProvenance(t *testing.T, env *testEnv, saleID string) saleProvenanceRow {
	t.Helper()
	var row saleProvenanceRow
	if err := env.db.QueryRow(`
		SELECT status, reversed_by, replaced_by_sale_id, replaces_sale_id, replacement_reason
		FROM ticket_sales WHERE id = $1
	`, saleID).Scan(&row.Status, &row.ReversedBy, &row.ReplacedBy, &row.Replaces, &row.ReplacementFor); err != nil {
		t.Fatalf("read sale %s: %v", saleID, err)
	}
	return row
}

// assertBuyerHolds insists that this Ticket is the named buyer's own and
// accepted — the shape the eligibility predicate looks for.
func assertBuyerHolds(t *testing.T, env *testEnv, ticketID, email string) {
	t.Helper()
	var holder sql.NullString
	var accepted sql.NullTime
	if err := env.db.QueryRow(`
		SELECT tk.holder_email, tk.accepted_at
		FROM tickets tk WHERE tk.id = $1
	`, ticketID).Scan(&holder, &accepted); err != nil {
		t.Fatalf("read Ticket %s: %v", ticketID, err)
	}
	if !holder.Valid || holder.String != email {
		t.Fatalf("Ticket %s holder = %v, want %s", ticketID, holder, email)
	}
	if !accepted.Valid {
		t.Fatalf("Ticket %s is not accepted; a Self-held Ticket is accepted as it is minted", ticketID)
	}
}

func soldCountOf(t *testing.T, env *testEnv, ticketTypeID string) int {
	t.Helper()
	var sold int
	if err := env.db.QueryRow(`SELECT sold_count FROM ticket_types WHERE id = $1`, ticketTypeID).Scan(&sold); err != nil {
		t.Fatalf("read sold_count of %s: %v", ticketTypeID, err)
	}
	return sold
}

func reversalRequestCount(t *testing.T, env *testEnv, saleID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM sale_reversals WHERE ticket_sale_id = $1`, saleID).Scan(&n); err != nil {
		t.Fatalf("count Reversal Requests of %s: %v", saleID, err)
	}
	return n
}

// assertNobodyWasToldAboutAReversal is how the silence is asserted, and it reads
// BOTH mails because they are composed in two different modules: the Sale Voided
// notice in sales, the No Longer Holding mail in catalog. An Upgrade must
// silence both, and a change that silenced only the one its own module sends
// would pass half of this.
func assertNobodyWasToldAboutAReversal(t *testing.T, env *testEnv) {
	t.Helper()
	if voided := env.email.Voided(); len(voided) != 0 {
		t.Fatalf("%d Sale Voided notices sent; an Upgrade reverses silently", len(voided))
	}
	if held := env.email.NoLongerHoldingsSent(); len(held) != 0 {
		t.Fatalf("%d No Longer Holding mails sent; an Upgrade reverses silently", len(held))
	}
}

// --- the Upgrade itself -----------------------------------------------------

// TestAnElectedUpgradeReversesTheEarlierFreeSaleInsideThePaidCommit is the
// tracer bullet: the buyer moves up, and by the time the paid Sale exists the
// free one is gone.
func TestAnElectedUpgradeReversesTheEarlierFreeSaleInsideThePaidCommit(t *testing.T) {
	f := newCrossSaleUpgrade(t)

	paid := confirmCheckoutOK(t, f.env, f.buyPaid(t, true), "approved")
	if paid.Status != "approved" {
		t.Fatalf("confirm = %+v, want approved", paid)
	}
	paidSaleID := saleIDOfPayment(t, f.env, paid.ClientTransactionID)

	free := readSaleProvenance(t, f.env, f.freeSaleID)
	if free.Status != "reversed" {
		t.Fatalf("the free Sale is %q, want reversed", free.Status)
	}
	// ORDINARY PROVENANCE: the buyer elected this, so the buyer is who reversed
	// it. An Upgrade is auditable as a buyer giving a Ticket back, never as
	// something staff or the platform did to them.
	if free.ReversedBy.String != sales.ReversalActorCustomer {
		t.Fatalf("reversed_by = %q, want %q", free.ReversedBy.String, sales.ReversalActorCustomer)
	}
	// NO REVERSAL REQUEST: a free Sale has no Payment Provider to wait on, so
	// nothing is left in flight and the whole act fits in one transaction.
	if n := reversalRequestCount(t, f.env, f.freeSaleID); n != 0 {
		t.Fatalf("%d Reversal Requests created; a free Sale has no provider to ask", n)
	}

	// The marker #651 reads. The pair is ADR 0050's and the reason is what stops
	// an Upgrade reading as a Sale Correction.
	if free.ReplacedBy.String != paidSaleID {
		t.Fatalf("the free Sale's replaced_by_sale_id = %q, want the paid Sale %q", free.ReplacedBy.String, paidSaleID)
	}
	if free.ReplacementFor.String != "upgrade" {
		t.Fatalf("the free Sale's replacement_reason = %q, want \"upgrade\"", free.ReplacementFor.String)
	}
	paidRow := readSaleProvenance(t, f.env, paidSaleID)
	if paidRow.Replaces.String != f.freeSaleID {
		t.Fatalf("the paid Sale's replaces_sale_id = %q, want the free Sale %q", paidRow.Replaces.String, f.freeSaleID)
	}
	if paidRow.ReplacementFor.String != "upgrade" {
		t.Fatalf("the paid Sale's replacement_reason = %q, want \"upgrade\"", paidRow.ReplacementFor.String)
	}
	if paidRow.Status != "active" {
		t.Fatalf("the paid Sale is %q, want active", paidRow.Status)
	}

	// The buyer holds what they paid for, and no longer holds the giveaway.
	paidTickets := ticketIDsOfSale(t, f.env, paidSaleID)
	if len(paidTickets) != 1 {
		t.Fatalf("the paid Sale minted %d Tickets, want 1", len(paidTickets))
	}
	assertBuyerHolds(t, f.env, paidTickets[0], f.buyer)

	// Capacity returns to the Event, counted off Ticket Sale Line quantities
	// exactly as it always is: the free Ticket Type is back where it started and
	// the paid one has sold one.
	if sold := soldCountOf(t, f.env, f.freeTypeID); sold != 0 {
		t.Fatalf("free Ticket Type sold_count = %d, want 0 — the surrendered Ticket returns to the Event", sold)
	}
	if sold := soldCountOf(t, f.env, f.paidTypeID); sold != 1 {
		t.Fatalf("paid Ticket Type sold_count = %d, want 1", sold)
	}

	// THE SILENCE, and the one mail that is owed.
	assertNobodyWasToldAboutAReversal(t, f.env)
	confirmations := f.env.email.Confirmations()
	if len(confirmations) != 1 {
		t.Fatalf("%d Sale Confirmations sent, want exactly the paid Sale's", len(confirmations))
	}
	if confirmations[0].Reference != paid.ConfirmationRef {
		t.Fatalf("the Sale Confirmation is for %q, want the paid Sale %q", confirmations[0].Reference, paid.ConfirmationRef)
	}
}

// TestWithoutTheElectionTheBuyerKeepsBothSales is the default, and it is the
// reversible one on purpose: an ignored Upgrade Prompt means KEEP BOTH.
func TestWithoutTheElectionTheBuyerKeepsBothSales(t *testing.T) {
	f := newCrossSaleUpgrade(t)

	confirmCheckoutOK(t, f.env, f.buyPaid(t, false), "approved")

	free := readSaleProvenance(t, f.env, f.freeSaleID)
	if free.Status != "active" {
		t.Fatalf("the free Sale is %q, want active — nothing was elected", free.Status)
	}
	if free.ReplacementFor.Valid {
		t.Fatalf("replacement_reason = %q on a Sale nothing replaced", free.ReplacementFor.String)
	}
	assertBuyerHolds(t, f.env, f.freeTicketID, f.buyer)
	if sold := soldCountOf(t, f.env, f.freeTypeID); sold != 1 {
		t.Fatalf("free Ticket Type sold_count = %d, want 1 — nothing was given back", sold)
	}
	assertNobodyWasToldAboutAReversal(t, f.env)
}

// --- the instant: nothing is surrendered before the money lands -------------

// TestAnAbandonedPaymentLeavesTheFreeTicketHeldAfterItExpires is the assertion
// this whole design exists for, and it is made AFTER the Payment expires rather
// than merely after the redirect.
//
// A buyer who elects an Upgrade and then closes the tab at the Payment Provider
// has bought nothing. If the free Sale had been reversed at begin-checkout they
// would now hold no Ticket at all, having paid for nothing and given up
// something — which ADR 0074 calls the mirror of the worst outcome this system
// has. Waiting only for the redirect would not prove it: the lazy 'expired'
// transition is a second chance for a careless implementation to act, so the
// clock is moved past the Capacity Hold window and the transition is forced
// before anything is read.
func TestAnAbandonedPaymentLeavesTheFreeTicketHeldAfterItExpires(t *testing.T) {
	f := newCrossSaleUpgrade(t)

	abandoned := f.buyPaid(t, true)

	// Past the hold window, then a fresh checkout by somebody else — which is
	// what drives the lazy expiry (ADR 0013). Nothing else reaps a pending.
	holdClocksAt(afterHoldWindow())
	t.Cleanup(func() { holdClocksAt(f.env.fixedClock) })
	other := "grace@example.com"
	beginCheckoutAsOK(t, f.env, "test-org", f.eventSlug, buyerSession(t, f.env, other),
		checkoutBody(other, "Grace", "Hopper", cartLine(f.paidTypeID, 1)))

	if status := paymentStatus(t, f.env, abandoned); status != "expired" {
		t.Fatalf("the abandoned Payment is %q, want expired — the assertion below is only worth making after it is", status)
	}

	free := readSaleProvenance(t, f.env, f.freeSaleID)
	if free.Status != "active" {
		t.Fatalf("the free Sale is %q after an abandoned checkout, want active", free.Status)
	}
	assertBuyerHolds(t, f.env, f.freeTicketID, f.buyer)
	if sold := soldCountOf(t, f.env, f.freeTypeID); sold != 1 {
		t.Fatalf("free Ticket Type sold_count = %d, want 1 — nothing was surrendered", sold)
	}
	assertNobodyWasToldAboutAReversal(t, f.env)
}

// TestADeclinedPaymentLeavesTheFreeTicketHeld: the provider said no, so there is
// no paid Ticket for the free one to have made way for.
func TestADeclinedPaymentLeavesTheFreeTicketHeld(t *testing.T) {
	f := newCrossSaleUpgrade(t)

	declined := confirmCheckoutOK(t, f.env, f.buyPaid(t, true), "declined")
	if declined.Status != "failed" {
		t.Fatalf("confirm = %+v, want failed", declined)
	}

	free := readSaleProvenance(t, f.env, f.freeSaleID)
	if free.Status != "active" {
		t.Fatalf("the free Sale is %q after a declined payment, want active", free.Status)
	}
	assertBuyerHolds(t, f.env, f.freeTicketID, f.buyer)
	assertNobodyWasToldAboutAReversal(t, f.env)
}

// --- degrading gracefully ---------------------------------------------------

// TestAFreeSaleReversedMidCheckoutDegradesSilently: the buyer undid the free
// Sale in another tab while they were at the Payment Provider.
//
// The desired end state — they hold the paid Ticket and not the free one — is
// already true by the time the commit runs, so the paid Sale commits unchanged,
// no reversal is attempted, and nobody is told anything. A checkout refused here
// would be a refusal about a Ticket that is already gone.
func TestAFreeSaleReversedMidCheckoutDegradesSilently(t *testing.T) {
	f := newCrossSaleUpgrade(t)

	inFlight := f.buyPaid(t, true)

	// The other tab. This is the buyer's own undo, which is loud by design — it
	// sends the Sale Voided notice and the No Longer Holding mail this Upgrade
	// would have suppressed — so the capture is cleared afterwards and what is
	// asserted below is what the COMMIT said, not what the undo did.
	undoOwnSale(t, f.env, f.buyer, f.freeRef)
	if status := readSaleProvenance(t, f.env, f.freeSaleID).Status; status != "reversed" {
		t.Fatalf("the free Sale is %q after the buyer undid it, want reversed", status)
	}
	// AND THIS IS WHAT AN UPGRADE SUPPRESSES. Reversing exactly this Sale, held by
	// exactly this buyer, through the ordinary route writes to them twice. Asserted
	// here so that every "nothing was sent" in this file is a statement about the
	// Upgrade rather than about a Sale nobody would have been told about anyway.
	if voided := f.env.email.Voided(); len(voided) != 1 {
		t.Fatalf("%d Sale Voided notices on an ordinary undo, want 1", len(voided))
	}
	if held := f.env.email.NoLongerHoldingsSent(); len(held) != 1 {
		t.Fatalf("%d No Longer Holding mails on an ordinary undo, want 1", len(held))
	}
	f.env.email.Reset()

	paid := confirmCheckoutOK(t, f.env, inFlight, "approved")
	if paid.Status != "approved" {
		t.Fatalf("confirm = %+v, want approved — a stale election never refuses a checkout", paid)
	}
	paidSaleID := saleIDOfPayment(t, f.env, paid.ClientTransactionID)

	// No Upgrade was performed, so no marker was written on either half.
	free := readSaleProvenance(t, f.env, f.freeSaleID)
	if free.ReplacedBy.Valid || free.ReplacementFor.Valid {
		t.Fatalf("the free Sale was marked replaced (%v / %v); nothing replaced it", free.ReplacedBy, free.ReplacementFor)
	}
	paidRow := readSaleProvenance(t, f.env, paidSaleID)
	if paidRow.Replaces.Valid || paidRow.ReplacementFor.Valid {
		t.Fatalf("the paid Sale claims to replace something (%v / %v); it replaced nothing", paidRow.Replaces, paidRow.ReplacementFor)
	}
	assertBuyerHolds(t, f.env, ticketIDsOfSale(t, f.env, paidSaleID)[0], f.buyer)
	assertNobodyWasToldAboutAReversal(t, f.env)
}

// TestAmbiguityAtCommitLeavesBothFreeSalesAlone: between the prompt and the
// commit the buyer claimed a SECOND free Ticket, so there is no longer one
// unambiguous Ticket to surrender.
//
// Eligibility is re-evaluated inside the commit transaction, and the ambiguity
// rule is the same one that decided whether to offer — so an offer that has gone
// stale simply does nothing, rather than picking one of the two.
func TestAmbiguityAtCommitLeavesBothFreeSalesAlone(t *testing.T) {
	f := newCrossSaleUpgrade(t)

	inFlight := f.buyPaid(t, true)

	second := beginCheckoutSettled(t, f.env, "test-org", f.eventSlug, buyerSession(t, f.env, f.buyer),
		checkoutBody(f.buyer, "Ada", "Byron", cartLine(f.freeTypeID, 1)))
	approvedRef(t, second)
	secondSaleID := saleIDOfPayment(t, f.env, second.ClientTransactionID)
	f.env.email.Reset()

	confirmCheckoutOK(t, f.env, inFlight, "approved")

	for _, saleID := range []string{f.freeSaleID, secondSaleID} {
		if status := readSaleProvenance(t, f.env, saleID).Status; status != "active" {
			t.Fatalf("free Sale %s is %q, want active — two free Tickets in play is no offer at all", saleID, status)
		}
	}
	if sold := soldCountOf(t, f.env, f.freeTypeID); sold != 2 {
		t.Fatalf("free Ticket Type sold_count = %d, want 2 — neither was surrendered", sold)
	}
	assertNobodyWasToldAboutAReversal(t, f.env)
}

// TestAFreeBasketNeverPerformsACrossSaleUpgrade covers the other checkout leg,
// the free settlement inside begin-checkout.
//
// It reaches the same commit function and honours the same election, and it can
// never perform this Upgrade — not because the leg is special-cased, but because
// an Upgrade is free-to-paid and a basket that totals zero has no paid Ticket to
// move up to. Electing one out of a giveaway and into another giveaway would
// destroy a Ticket and mint an identical one, telling nobody.
func TestAFreeBasketNeverPerformsACrossSaleUpgrade(t *testing.T) {
	f := newCrossSaleUpgrade(t)

	body := checkoutBody(f.buyer, "Ada", "Byron", cartLine(f.freeTypeID, 1))
	body["upgrade_elected"] = true
	settled := beginCheckoutSettled(t, f.env, "test-org", f.eventSlug, buyerSession(t, f.env, f.buyer), body)
	approvedRef(t, settled)

	if status := readSaleProvenance(t, f.env, f.freeSaleID).Status; status != "active" {
		t.Fatalf("the earlier free Sale is %q, want active — there was nothing to upgrade to", status)
	}
	assertBuyerHolds(t, f.env, f.freeTicketID, f.buyer)
	assertNobodyWasToldAboutAReversal(t, f.env)
}

// TestADarkBuildPerformsNoUpgrade: with Ticket Assignment closed nobody is
// seated on anything, so there is no Ticket of the buyer's own to surrender and
// none to surrender it for. A deployment that has never opened the flag must not
// start destroying the historical rows this predicate would find.
func TestADarkBuildPerformsNoUpgrade(t *testing.T) {
	f := newCrossSaleUpgrade(t)
	closeTicketAssignment(t)

	confirmCheckoutOK(t, f.env, f.buyPaid(t, true), "approved")

	if status := readSaleProvenance(t, f.env, f.freeSaleID).Status; status != "active" {
		t.Fatalf("the free Sale is %q in a dark build, want active — nothing is seated, so nothing is surrenderable", status)
	}
	assertNobodyWasToldAboutAReversal(t, f.env)
}
