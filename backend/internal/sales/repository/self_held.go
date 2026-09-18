package repository

// selfHeldSeat is one Ticket Sale Line's claim to carry the Self-held Ticket,
// as the commit spine writes the lines: the Ticket the line would seat the buyer
// on, and the three facts that decide whether it beats the line before it.
//
// THE SEAT IS THE DEAREST LINE'S (ADR 0074, #646). A buyer is presumed to attend
// on what they paid most for. The rule it replaced — first in the catalog's
// order — meant "cheapest in the basket" every time a buyer mixed Ticket Types,
// because Organizations list their catalogs cheap-to-dear, and it reliably
// seated buyers on a giveaway while the Ticket they had paid for went out
// unassigned and owing its Answers.
//
// THE PRICE IS THE UNIT PRICE AS SOLD, not the Ticket Type's catalog price: the
// amount the line snapshots into `unit_price_cents`. A Promotional Price is
// therefore respected, and so is a Sale Import row's overriding amount — a
// discounted dear line can lose to a cheaper line sold at its List Price, which
// is the honest answer to "what did this buyer pay most for".
//
// THE CATALOG BREAKS TIES AND NOTHING ELSE, unchanged: sort_order, then name by
// byte order — byte-for-byte the tie-break migrations 084/088 spell as
// `tt.name COLLATE "C"`, which is why that collation is in them.
//
// THE STOREFRONT'S LIST IS NOT QUITE THE SAME ORDER, and this comment will not
// pretend otherwise: the Event page reads its Ticket Types by `sort_order,
// created_at` (catalog ListTicketTypesByEventID), so two equally priced Ticket
// Types SHARING a sort_order are separated by creation time there and by name
// here. `sort_order` carries no unique constraint, so that is reachable. It
// predates this rule and is not made worse by it — under the dearest rule a tie
// needs equal prices too — but it is the one seam where the page and this spine
// could still name different Tickets, and it wants a ticket rather than a claim.
type selfHeldSeat struct {
	// ticketID is the Ticket that would be seated: the first of the line.
	// Empty on the zero value, which is what "no line has claimed the seat"
	// means and the only thing that distinguishes it from a real claim.
	ticketID string
	// unitPriceCents is the line's unit price AS SOLD.
	unitPriceCents int
	sortOrder      int
	name           string
}

// outranks reports whether this line's claim takes the seat from the one
// holding it — dearest first, then the catalog's order, then the name.
//
// EVERY COMPARISON IS STRICT, so a line that ranks equal with the incumbent
// leaves it alone: two lines of the same Ticket Type at the same price seat the
// buyer on the earlier one, the way ordering by these three keys would.
func (s selfHeldSeat) outranks(held selfHeldSeat) bool {
	// A line that minted no Ticket claims nothing, however dear it was. The
	// quantities that reach the spine are all positive, so this is a guard and
	// not a case: what it buys is that no arithmetic here can ever UNSEAT a
	// buyer the line before had seated.
	if s.ticketID == "" {
		return false
	}
	if held.ticketID == "" {
		return true
	}
	if s.unitPriceCents != held.unitPriceCents {
		return s.unitPriceCents > held.unitPriceCents
	}
	if s.sortOrder != held.sortOrder {
		return s.sortOrder < held.sortOrder
	}
	return s.name < held.name
}
