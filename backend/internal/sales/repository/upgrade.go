package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Upgrade's cross-Sale half (#650, parent #645, ADR 0074): the buyer's free
// Ticket Sale is REVERSED INSIDE THE TRANSACTION THAT COMMITS THE PAID ONE.
//
// THE WHOLE DESIGN IS THAT INSTANT. An Upgrade surrenders something the buyer
// already holds, and the only safe moment to surrender it is the moment the
// replacement becomes theirs: the buyer ends up holding the paid Ticket and not
// the free one, or nothing moved at all. Doing it at begin-checkout — where the
// election is made, and where it would be easiest to write — would let a
// checkout abandoned at the Payment Provider or declined by it take a Ticket and
// give nothing back. ADR 0074 calls that the mirror of the worst outcome this
// system has, and it is the reason this file's one function takes a *sql.Tx and
// is unexported: there is no honest caller outside the commit.
//
// IT IS THE SECOND MECHANISM AND NOT THE FIRST. Where the free Ticket is in the
// SAME basket, nothing is reversed because nothing was ever sold — the free line
// is simply not bought (#649). This half exists only for a Ticket Sale already
// made, and the two are told apart by the eligibility answer itself rather than
// by anything the client says: a basket-side candidate leaves
// UpgradeEligibility.Ticket at its zero value, and this function then has
// nothing to act on.

// upgradeOutOfEarlierFreeSale is one commit's attempt to perform the cross-Sale
// Upgrade: who is buying, what they have just been seated on, and how much of
// the ambiguity rule this basket contributes.
//
// It is an ATTEMPT and not an instruction, which is the point of the name. Every
// field below is a fact about the commit in progress; whether an Upgrade happens
// is decided by re-reading eligibility inside the transaction, never by the
// caller and never by the buyer.
type upgradeOutOfEarlierFreeSale struct {
	EventID        string
	OrganizationID string
	// CustomerID is the buyer as the Customer upsert resolved them in THIS
	// transaction — which is also why eligibility cannot be read on the pool: a
	// buyer minted seconds ago is invisible outside these writes.
	CustomerID string
	// PaidSaleID is the Ticket Sale just inserted, the one that takes the free
	// Sale's place and names it through replaces_sale_id.
	PaidSaleID string
	// SeatUnitPriceCents is what the buyer paid for the Ticket they were just
	// seated on — the dearest line's unit price as sold (#646).
	//
	// IT IS THE "FREE TO PAID" PRECONDITION, and it is checked here rather than
	// in OffersUpgrade because OffersUpgrade is the AMBIGUITY rule and nothing
	// else (#648). A basket with no paid Ticket in it has nothing to upgrade TO,
	// and an Upgrade out of a free Sale into another free Sale would destroy a
	// Ticket and mint an identical one, telling nobody. Zero refuses.
	SeatUnitPriceCents int
	// FreeTicketsInBasket is how many Tickets this Sale's own zero-priced lines
	// mint, counted over quantities. It is the other half of the ambiguity rule:
	// one free Ticket in play is an offer, two is no offer at all, and the two
	// sides are counted together or the rule is not the rule.
	//
	// IT IS COUNTED FROM THE LINES AS THE SPINE WRITES THEM, and that is a
	// CONTRACT with the same-basket mechanism (#649) rather than an incidental
	// choice. A basket holding one free line BESIDE an earlier qualifying free
	// Sale is two Tickets in play and therefore no offer at all — and the only
	// thing making that refusal true here is that the free line is still in the
	// basket when this count is taken. A same-basket drop performed BEFORE the
	// commit would leave this at zero, the ambiguity guard would open, and a
	// forged election could take the earlier Sale as well as the line: the buyer
	// would lose two Tickets having elected to lose one. Either the drop happens
	// after this count, or the same-basket route must not also set
	// CommitTerms.UpgradeElected.
	FreeTicketsInBasket int
	Now                 time.Time
}

// upgradeOutOfEarlierFreeSaleTx performs the Upgrade if it is still available,
// and returns the id of the free Ticket Sale it reversed — or the empty string,
// which is the answer every ordinary commit gets.
//
// IT DEGRADES TO SILENCE AND NEVER TO AN ERROR. Four things can have changed
// between the Upgrade Prompt being shown and this transaction running: the buyer
// reversed the free Sale themselves in another tab, an Operator reversed it,
// they reassigned its Ticket and somebody else accepted, or they went and bought
// a second free Ticket so the offer became ambiguous. In every one of them the
// paid Sale commits unchanged, nothing is reversed and nobody is told — because
// the state ADR 0074 wanted is already the state that exists, and a checkout
// refused over a Ticket the buyer no longer has would be a refusal about
// nothing. THE BUYER IS NEVER SHOWN AN UPGRADE FAILING.
//
// EVERY REFUSAL ABOVE IS A RE-READ AND NOT A TRUST. UpgradeEligibilityFor runs
// again here, against this transaction's own tx, so the predicate that decided
// to OFFER and the predicate that decides to ACT are the same code reading the
// same rows under the same locks (#648). An election the backend did not offer —
// a client that sent one anyway, or one that became stale — is ignored, never
// refused.
//
// THE REVERSAL GOES THROUGH THE SHARED PRIMITIVE, reverseSalesTx, which is what
// makes the capacity return, the row locking and the provenance stamp the same
// act they are on every other route. Three things are stated at this call rather
// than inherited:
//
//   - ORDINARY CUSTOMER PROVENANCE. `reversed_by` is the Customer, because it
//     was: the buyer elected this. An Upgrade is auditable as exactly what it is
//     — a buyer giving a Ticket back — and not as something the platform did to
//     them.
//   - NO REVERSAL REQUEST, and none is possible: a Reversal Request exists to
//     wait on a Payment Provider, and a free Sale has no Payment Provider to
//     wait on. Nothing is left in flight, which is the whole of why the Upgrade
//     can commit inside one transaction at all.
//   - THE INVOICING SEAM IS NIL, like the single imported-sale reversal's. A free
//     Sale carries no money and therefore no Tax Invoice — ADR 0074 counted 4,249
//     free Online Sales and not one invoiced — so there is no document to
//     withdraw and no Credit Note to owe. That is not an omission; it is the zero
//     boundary the whole feature sits inside.
func upgradeOutOfEarlierFreeSaleTx(ctx context.Context, tx *sql.Tx, in upgradeOutOfEarlierFreeSale) (string, error) {
	// Free to paid and no further. A seat worth nothing is not something to
	// upgrade INTO, whatever the buyer elected.
	if in.SeatUnitPriceCents <= 0 {
		return "", nil
	}

	eligibility, err := UpgradeEligibilityFor(ctx, tx, in.EventID, in.CustomerID)
	if err != nil {
		return "", err
	}
	if !eligibility.OffersUpgrade(in.FreeTicketsInBasket) {
		return "", nil
	}
	// The one free Ticket in play is in THIS basket, not on an earlier Sale:
	// there is nothing made to reverse, and #649's mechanism is the one that
	// applies. The zero value is how UpgradeEligibility says so, and asking it
	// directly is what keeps this half from ever acting on the other's case.
	if eligibility.Ticket.TicketSaleID == "" {
		return "", nil
	}

	reversed, err := reverseSalesTx(ctx, tx, ReverseSalesInput{
		EventID:        in.EventID,
		OrganizationID: in.OrganizationID,
		SaleIDs:        []string{eligibility.Ticket.TicketSaleID},
		Actor:          sales.ReversalActorCustomer,
		Route:          sales.ReversalRouteUpgrade,
		Now:            in.Now,
	})
	if err != nil {
		return "", err
	}
	// The primitive returns nothing for a Sale somebody else reversed first. The
	// eligibility read above took no lock, so this is the race it cannot close,
	// and the answer is the same as every other way the offer can go stale: the
	// paid Sale stands, and nothing else happened.
	if len(reversed) != 1 {
		return "", nil
	}

	if err := linkReplacementTx(ctx, tx, reversed[0].ID, in.PaidSaleID, sales.ReplacementReasonUpgrade); err != nil {
		return "", err
	}
	return reversed[0].ID, nil
}

// linkReplacementTx points two Ticket Sales at each other and says WHY: the
// reversed one names its replacement, the replacement names what it stands in
// for, and both carry the reason.
//
// IT IS THE ONLY WRITER OF THAT PAIR, which is the point of it being here rather
// than spelled out at each of the two acts that have one. ADR 0050's Sale
// Correction and ADR 0074's Upgrade differ in exactly one token — the reason —
// and migration 123's CHECK refuses a link without one; a second copy of these
// two statements would be a second chance to write a link that the database then
// rejects, or worse, one that carries the wrong word.
//
// THE PAIR IS ADR 0050'S AND IS REUSED DELIBERATELY. "This Ticket Sale stands in
// the place of that one" is one relation, and a second pair of columns for the
// second reason to have it would be two spellings of one fact. What ADR 0074
// refused was reusing the pair UNMARKED: `corrected` is ADR 0050's word for a
// sale somebody recorded wrongly, and letting it cover an Upgrade would tell an
// Organization its staff erred on a Sale no human touched.
//
// THE REASON IS WRITTEN ON BOTH HALVES, so either row answers on its own. The
// reversed Sale reads "replaced by that one, for this reason"; the replacement
// reads "replaces that one, for the same reason". Neither needs a join to the
// other to be displayed correctly, which is what the Sales list and the Customer
// Dossier both want.
//
// TWO STATEMENTS AND NOT ONE, because each row's link and reason must land
// together for the per-row CHECK to hold at every point, and the two rows are
// different rows.
func linkReplacementTx(ctx context.Context, tx *sql.Tx, reversedSaleID, replacementSaleID, reason string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_sales
		SET replaced_by_sale_id = $2, replacement_reason = $3
		WHERE id = $1
	`, reversedSaleID, replacementSaleID, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_sales
		SET replaces_sale_id = $2, replacement_reason = $3
		WHERE id = $1
	`, replacementSaleID, reversedSaleID, reason); err != nil {
		return err
	}
	return nil
}
