package repository

import (
	"context"
	"database/sql"
)

// SelfHeldSeat names the Ticket a basket seats its buyer on, before any Ticket
// exists: which line, and which of that line's Tickets.
//
// IT IS ADDRESSED BY POSITION AND NOT BY ID, because the question is asked of
// a basket as well as of a commit (#666). Begin-checkout must know which Ticket
// it is not to ask a Holder for, and at that point nothing has been minted.
type SelfHeldSeat struct {
	// Line is the index of the seating line in the slice SelfHeldSeatOf was
	// given. It means nothing outside that slice: two halves that hold the same
	// basket in different orders must translate it through the line itself
	// (its Ticket Type), never compare indexes.
	Line int
	// TicketIndex is which of the line's Tickets is the seat, 1..quantity -
	// the same numbering a held Answer's TicketIndex uses and the Ticket's
	// `tickets.ordinal` stores. It is always 1 today: the seat is the line's
	// FIRST Ticket. It travels anyway so no caller has to know that.
	TicketIndex int
}

// CatalogEntry is what the Event's catalog contributes to a line's claim on
// the seat: the List Price a line with no override is sold at, and the two keys
// that break a tie on price before the Ticket Type's id does.
type CatalogEntry struct {
	PriceCents int
	SortOrder  int
	Name       string
}

// CatalogLookup answers CatalogEntry for a Ticket Type of the basket's Event.
// The commit answers it from the rows it holds FOR UPDATE, and begin-checkout
// from the catalog read it priced the cart from - neither needs a transaction
// to ask the seating question.
type CatalogLookup func(ticketTypeID string) CatalogEntry

// SelfHeldSeatOf is THE Self-held seating rule (ADR 0048, ADR 0074): the Ticket
// of a basket that becomes the buyer's own. It reports false for a basket that
// mints no Ticket.
//
// IT IS THE ONE DEFINITION, and two halves ask it (#666). The commit spine asks
// it of the lines it is about to write and seats the buyer on the answer;
// begin-checkout asks it of the lines it is about to hold, to exempt exactly
// that Ticket from Named Tickets' "name a Holder" (ADR 0076). Restating the rule
// in either place would let them disagree, and a buyer would be asked to name a
// Ticket that becomes their own, or excused from naming one that does not -
// the constraint the Upgrade already lives under with OffersUpgrade.
//
// THE BASKET IS THE ONE THAT WILL BE WRITTEN: a same-basket Upgrade's
// surrendered free line must already be out of it (see SameBasketUpgrade), or
// a free line nobody will buy could be ranked.
//
// THE PRICE IS committedUnitPrice, the one definition of "what this line is
// sold at" that the commit writes onto `unit_price_cents` and the Upgrade splits
// free from paid on. A line's override wins where there is one - which is how a
// Promotional Price and a Sale Import row's amount both reach the rule - and
// the catalog's price otherwise.
func SelfHeldSeatOf(lines []CommitLine, catalog CatalogLookup) (SelfHeldSeat, bool) {
	var held seatClaim
	for i, line := range lines {
		entry := catalog(line.TicketTypeID)
		claim := seatClaim{
			line:           i,
			tickets:        line.Quantity,
			unitPriceCents: committedUnitPrice(line, entry),
			sortOrder:      entry.SortOrder,
			name:           entry.Name,
			ticketTypeID:   line.TicketTypeID,
		}
		if claim.outranks(held) {
			held = claim
		}
	}
	if held.tickets <= 0 {
		return SelfHeldSeat{}, false
	}
	return SelfHeldSeat{Line: held.line, TicketIndex: 1}, true
}

// seatClaim is one line's claim to carry the Self-held Ticket: the line, how
// many Tickets it mints, and the four facts that decide whether it beats the
// line before it.
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
// byte order — the same comparison, in the same direction, that the storefront's
// lists and migrations 084/088 make.
//
// THE TICKET TYPE'S ID SETTLES WHAT THE CATALOG CANNOT, by byte order, so the
// rule is total over distinct Ticket Types and the order the lines arrive in
// never decides the seat. That is load-bearing: begin-checkout holds its lines
// in the catalog's order and the commit reads `payment_lines` back unordered,
// and on a tie the catalog leaves standing the earlier line would otherwise win
// in each half - possibly a different line in each. The storefront mirrors the
// same four keys (apps/storefront/lib/named-tickets.ts).
type seatClaim struct {
	line int
	// tickets is how many Tickets the line mints. Zero on the zero value,
	// which is what "no line has claimed the seat" means and the only thing
	// that distinguishes it from a real claim.
	tickets int
	// unitPriceCents is the line's unit price AS SOLD.
	unitPriceCents int
	sortOrder      int
	name           string
	ticketTypeID   string
}

// outranks reports whether this line's claim takes the seat from the one
// holding it — dearest first, then the catalog's order, then the name, then
// the Ticket Type's id.
//
// EVERY COMPARISON IS STRICT, so a line that ranks equal with the incumbent
// leaves it alone. Only two lines of the SAME Ticket Type at the same price can
// rank equal, and either is the same Ticket Type's seat; the earlier keeps it.
func (s seatClaim) outranks(held seatClaim) bool {
	// A line that mints no Ticket claims nothing, however dear it is. The
	// quantities that reach the spine are all positive, so this is a guard and
	// not a case: what it buys is that no arithmetic here can ever UNSEAT a
	// buyer the line before had seated.
	if s.tickets <= 0 {
		return false
	}
	if held.tickets <= 0 {
		return true
	}
	if s.unitPriceCents != held.unitPriceCents {
		return s.unitPriceCents > held.unitPriceCents
	}
	if s.sortOrder != held.sortOrder {
		return s.sortOrder < held.sortOrder
	}
	if s.name != held.name {
		return s.name < held.name
	}
	return s.ticketTypeID < held.ticketTypeID
}

// --- The Upgrade's eligibility (ADR 0074, #648) ----------------------------

// SurrenderableFreeTicket names one free Ticket a buyer could give up through
// an Upgrade: the Ticket itself and the Ticket Sale that would be reversed to
// surrender it. Both ids travel because the Upgrade needs both halves — the
// reversal acts on the SALE, and every surface that talks about the thing being
// given up means the TICKET — and a caller holding one and looking the other up
// would be a second place for "which Ticket was this about" to be decided.
type SurrenderableFreeTicket struct {
	TicketID     string
	TicketSaleID string
	// TicketTypeID is the Ticket Type that Sale's one line sold. It travels for
	// one reason and it is not display: it is the `ticket_types` row a cross-Sale
	// Upgrade would lock to give the capacity back, and the commit spine has to
	// know it BEFORE it takes its sorted lock set, or the reversal takes a second
	// lock set in an order nothing coordinates (see CommitSales' "THE ONE LOCK
	// SET"). Unambiguous by the same rule that makes the Ticket unambiguous: the
	// Sale carries exactly one Ticket, so it has exactly one line and therefore
	// exactly one Ticket Type.
	TicketTypeID string
}

// UpgradeEligibility is everything the platform knows about one buyer's free
// Tickets on one Event: how many of them they could surrender, and which one —
// named only when there is exactly one, because that is the only case any
// surface may act on.
//
// THE COUNT IS CARRIED AND NOT MERELY THE VERDICT, because the verdict is not
// this side's alone to give. The offer is withheld whenever more than one free
// Ticket is IN PLAY, and the basket is in play (ADR 0074): a buyer already
// holding one free Ticket who puts a second in their cart is exactly as
// ambiguous as one holding two. The basket is not part of the read that
// produces this — nobody has typed it yet when the Event page loads — so the
// arithmetic that joins the two lives in OffersUpgrade below, and every surface
// calls that rather than restating "exactly one" in its own words.
type UpgradeEligibility struct {
	// SurrenderableFreeTickets is how many free Tickets on this buyer's earlier
	// active Online Sales of this Event qualify. Zero for a buyer with none and
	// for a buyer the caller could not identify — the payload, not this type, is
	// where "we do not know who is asking" is said.
	SurrenderableFreeTickets int
	// Ticket is the one qualifying Ticket, set only when
	// SurrenderableFreeTickets is exactly 1 and zero-valued otherwise. An
	// ambiguous buyer has no candidate by definition, and leaving the field
	// empty means no caller can reach past the ambiguity rule to a Ticket the
	// platform has decided not to name.
	Ticket SurrenderableFreeTicket
}

// OffersUpgrade reports whether an Upgrade may be offered, counting this
// buyer's earlier qualifying Sales and the free Tickets in the basket TOGETHER:
// exactly one free Ticket in play, and no more.
//
// This is the whole ambiguity rule and the only place it is written. Two free
// Tickets in the basket yield nothing; one in the basket beside a qualifying
// earlier Sale yields nothing; one, from either side, is the offer — and which
// side it came from is what decides the mechanism, since a free line in the
// basket is simply not bought while an earlier free Sale is reversed.
//
// It does NOT ask whether the basket holds a paid Ticket. An Upgrade is free to
// paid, so an offer without one is meaningless — but that is a precondition of
// the surface doing the offering, and the ambiguity rule is the part that must
// not be allowed to differ between them.
func (e UpgradeEligibility) OffersUpgrade(freeTicketsInBasket int) bool {
	return e.SurrenderableFreeTickets+freeTicketsInBasket == 1
}

// rowQuerier is the one method the Upgrade's eligibility read needs, so the
// same predicate runs on the pool (the Event page) and inside the commit
// transaction (the Upgrade itself), exactly as queryRower does for the
// imported-sale guard.
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// UpgradeEligibilityFor answers "which free Tickets on this Event could this
// buyer surrender", and it is THE definition of that question — the read that
// publishes the fact to checkout and the commit that acts on it call this same
// function, so no surface can offer an Upgrade the commit would refuse.
//
// It takes a rowQuerier rather than a pool for exactly that reason: eligibility
// is re-evaluated inside the transaction that commits the paid Sale (ADR 0074),
// where a free Sale reversed a moment ago must already be gone from the answer.
// Pass the *sql.Tx there; pass the pool on the read path.
//
// A Ticket qualifies only when ALL of these hold, and each clause is a case
// ADR 0074 argued rather than a filter for tidiness:
//
//   - IT IS ON AN ACTIVE ONLINE SALE OF THIS EVENT. A reversed Sale has nothing
//     left to give up, and the staff channels are excluded because an Upgrade is
//     the BUYER's election: nothing an Organization transcribed or rang up at
//     the door is theirs to unsell here.
//   - THAT SALE CARRIES EXACTLY ONE TICKET, counted over every line's quantity.
//     A free Sale of several would take a stranger's Ticket down with it when
//     the whole Sale is reversed, and partial reversal is a thing this platform
//     deliberately does not have.
//   - THE BUYER THEMSELF STILL HOLDS IT, accepted — their own Self-held Ticket
//     (ADR 0048). A free Ticket somebody else accepted is not the buyer's to
//     surrender, and a reassigned one has stopped being self-held.
//   - THE LINE'S PRICE AS SOLD WAS ZERO. Free to paid and no further: a Ticket
//     that cost nothing can be given up without a refund or a credit note, and
//     that is the whole of why the line sits at zero rather than at "anything
//     cheaper".
//
// An empty customerID — a buyer the platform has never completed a Sale for, or
// a reader it could not identify — matches nothing and yields a zero
// UpgradeEligibility, which is the truth about somebody with no Sales.
//
// IT DOES NOT ASK THE TICKET ASSIGNMENT FLAG, and a caller must. Nothing is
// surrenderable in a dark build, because nothing is seated in one, but the flag
// is a deployment fact and this package has never been told about it. Each side
// gates in its own words on the same env var: the read path in
// service.SurrenderableFreeTickets, and the commit path on `in.Terms.SelfHeld`,
// which is already what decides whether CommitSales seats anybody at all. A
// caller that skips both would offer — or perform — an Upgrade in a build where
// no Ticket is the buyer's own.
func UpgradeEligibilityFor(ctx context.Context, q rowQuerier, eventID, customerID string) (UpgradeEligibility, error) {
	if customerID == "" {
		return UpgradeEligibility{}, nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT t.id, ts.id, tsl.ticket_type_id
		FROM tickets t
		JOIN ticket_sale_lines tsl ON tsl.id = t.ticket_sale_line_id
		JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
		WHERE ts.event_id = $1::uuid
		  AND ts.customer_id = $2::uuid
		  AND ts.status = 'active'
		  AND ts.channel = 'online'
		  AND tsl.unit_price_cents = 0
		  AND t.holder_customer_id = $2::uuid
		  AND t.accepted_at IS NOT NULL
		  AND (
		    SELECT COALESCE(SUM(l.quantity), 0)
		    FROM ticket_sale_lines l
		    WHERE l.ticket_sale_id = ts.id
		  ) = 1
	`, eventID, customerID)
	// NO ORDER BY, deliberately. Order would only matter to a caller picking one
	// candidate out of several, and that caller must not exist: more than one is
	// the ambiguity itself, and the answer to it is no offer rather than a
	// winner. Leaving the rows unordered is what makes "take the first" look as
	// wrong as it is.
	if err != nil {
		return UpgradeEligibility{}, err
	}
	defer rows.Close()

	var found []SurrenderableFreeTicket
	for rows.Next() {
		var candidate SurrenderableFreeTicket
		if err := rows.Scan(&candidate.TicketID, &candidate.TicketSaleID, &candidate.TicketTypeID); err != nil {
			return UpgradeEligibility{}, err
		}
		found = append(found, candidate)
	}
	if err := rows.Err(); err != nil {
		return UpgradeEligibility{}, err
	}

	eligibility := UpgradeEligibility{SurrenderableFreeTickets: len(found)}
	// Named only in the unambiguous case. Two candidates is not "the first one";
	// it is no offer at all, and a struct that answered otherwise would let a
	// careless caller reach the Ticket anyway.
	if len(found) == 1 {
		eligibility.Ticket = found[0]
	}
	return eligibility, nil
}

// ReadUpgradeEligibility is the read-path spelling of UpgradeEligibilityFor:
// the same predicate, against the pool, for a surface that is only publishing
// the fact and writing nothing. A caller inside a transaction must call
// UpgradeEligibilityFor with its own *sql.Tx instead, or it would be deciding
// on a world its own uncommitted writes are not in.
//
// It exists only because the pool is unexported, which is the point: outside
// this package there is no way to ask this question without saying which of the
// two you are.
func (r *Repository) ReadUpgradeEligibility(ctx context.Context, eventID, customerID string) (UpgradeEligibility, error) {
	return UpgradeEligibilityFor(ctx, r.db.Pool, eventID, customerID)
}
