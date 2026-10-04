package repository

import (
	"context"
	"database/sql"
)

// The Upgrade's same-basket mechanism (ADR 0074, #649): when the free Ticket
// and the paid one are in the SAME cart, electing the Upgrade means the free
// line is simply never bought.
//
// THERE IS NOTHING TO REVERSE HERE, AND THAT IS THE WHOLE REASON THE TWO
// MECHANISMS ARE DIFFERENT CODE. A Ticket that was never sold owes no reversal,
// no Sale Voided notice and no explanation: the line is dropped before the
// Ticket Sale row is written, so no Ticket is minted, no capacity is consumed,
// and nothing exists afterwards that has to be undone. The other mechanism —
// the free Ticket sitting on an EARLIER Sale — is a reversal inside this same
// transaction, and it lives elsewhere.
//
// THE DROPPED LINE IS WORTH ZERO, so nothing downstream of the arithmetic
// moves: the Sale's total, the Payment's amount, the Platform Fee snapshots and
// the Tax Invoice are what they would have been. That is not a coincidence to
// be maintained but the boundary ADR 0074 drew — free to paid and no further —
// and it is why an Upgrade can happen inside a payment's transaction at all.

// SameBasketUpgrade answers "which of this cart's lines are actually bought",
// given that the buyer ticked the Upgrade Prompt. It is the same-basket
// mechanism's whole rule, and two halves ask it: the commit, which drops the
// line, and begin-checkout, which must not ask for a Holder or Answers on a
// Ticket that will never exist (ADR 0076, #666). The seat SelfHeldSeatOf picks
// is then picked from what this keeps, on both sides.
//
// It returns the lines to write and the line it dropped, or the lines unchanged
// and a nil drop when this election is not one the platform offered. NOTHING
// HERE RETURNS A REFUSAL, deliberately and by ADR 0074: the prompt never gates a
// payment, so every way of saying no to an election resolves to "buy both
// lines" — which is exactly what the buyer's cart already said.
//
// THREE THINGS MUST ALL HOLD, and each is a case the ADR argued:
//
//   - EXACTLY ONE FREE TICKET IN THE BASKET. Counted over quantity, so a line of
//     two free Tickets is two and disqualifies the cart: a prompt that must ask
//     which of several Tickets it is about has stopped clarifying. One free
//     Ticket by count is therefore also one free LINE, which is what makes
//     "drop the line" and "drop the Ticket" the same act here.
//   - AT LEAST ONE PAID TICKET IN THE BASKET. An Upgrade is free to paid, so
//     without something paid to move onto there is nothing to elect. This is the
//     precondition OffersUpgrade deliberately does not carry — it is the
//     offering surface's, and here this function is that surface.
//   - THE AMBIGUITY RULE, asked of OffersUpgrade and never restated: this
//     buyer's qualifying EARLIER free Sales counted together with the basket
//     must come to exactly one. A buyer with a free Ticket already on file who
//     puts another in their cart is ambiguous, and the answer to ambiguity is no
//     Upgrade rather than a guess.
//
// THE PRICE IS THE UNIT PRICE AS SOLD, the same price SelfHeldSeatOf ranks on
// (#646): what the cart charges for the line, not what the catalog lists. A
// Ticket Type given away by a Promotional Price is free in this basket, and that
// is the honest reading of "cost nothing".
//
// THE ELIGIBILITY IS A FUNCTION AND NOT A VALUE so it is read only for a basket
// that could carry an Upgrade at all: an ordinary cart costs nobody a query.
// Which world it is read in is the caller's to say, and that is the one thing
// the two halves do differently - see upgradedBasket for the commit's.
func SameBasketUpgrade(
	lines []CommitLine,
	catalog CatalogLookup,
	eligibility func() (UpgradeEligibility, error),
) (kept []CommitLine, dropped *CommitLine, err error) {
	freeIndex := -1
	freeTickets, paidTickets := 0, 0
	for i, line := range lines {
		if committedUnitPrice(line, catalog(line.TicketTypeID)) == 0 {
			freeTickets += line.Quantity
			if freeIndex < 0 {
				freeIndex = i
			}
			continue
		}
		paidTickets += line.Quantity
	}
	if freeTickets != 1 || paidTickets == 0 {
		return lines, nil, nil
	}

	// The buyer's earlier free Tickets, read through the one definition of the
	// question (#648). An election against a basket like this one is still
	// refused when the buyer has a qualifying free Ticket on file: two in play
	// is two, whichever side they came from.
	earlier, err := eligibility()
	if err != nil {
		return nil, nil, err
	}
	if !earlier.OffersUpgrade(freeTickets) {
		return lines, nil, nil
	}

	surrendered := lines[freeIndex]
	kept = make([]CommitLine, 0, len(lines)-1)
	kept = append(kept, lines[:freeIndex]...)
	kept = append(kept, lines[freeIndex+1:]...)
	return kept, &surrendered, nil
}

// upgradedBasket is the commit's spelling of SameBasketUpgrade.
//
// IT RUNS INSIDE THE COMMIT'S TRANSACTION and takes the tx as its querier, never
// the pool. The eligibility this re-evaluates must see this transaction's own
// writes - and a free Sale reversed moments ago must already be gone from the
// answer.
func upgradedBasket(
	ctx context.Context,
	tx *sql.Tx,
	eventID, customerID string,
	lines []CommitLine,
	locked map[string]lockedType,
) (kept []CommitLine, dropped *CommitLine, err error) {
	return SameBasketUpgrade(lines, catalogOfLocked(locked), func() (UpgradeEligibility, error) {
		return UpgradeEligibilityFor(ctx, tx, eventID, customerID)
	})
}

// committedUnitPrice is the price one unit of a line is being SOLD at: the
// caller's override where there is one, and the Ticket Type's locked catalog
// price otherwise — the same two-line rule the commit spine writes onto
// `unit_price_cents`, asked before the line is written rather than as it is.
//
// It takes the line's CatalogEntry rather than the commit's lock set so that
// SelfHeldSeatOf can ask it outside a transaction too (#666): begin-checkout
// holds lines whose override is the very price the commit will write, and the
// catalog read it priced them from.
func committedUnitPrice(line CommitLine, entry CatalogEntry) int {
	if line.UnitPriceCents != nil {
		return *line.UnitPriceCents
	}
	return entry.PriceCents
}
