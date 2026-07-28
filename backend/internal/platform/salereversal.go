package platform

import (
	"database/sql"
	"time"
)

// OnlineSalesChannel is the one Sales Channel a Customer can reverse on their
// own. An In-Person Sale or an imported one records money the platform never
// touched, so there is nothing here to give back and nobody here to ask.
const OnlineSalesChannel = "online"

// ActiveSaleStatus is the Ticket Sale that still stands. A reversed one has
// already been undone and cannot be undone twice.
const ActiveSaleStatus = "active"

// SaleReversalFacts is everything about a Ticket Sale that decides whether the
// Customer may undo it, and nothing else.
//
// It exists so that every surface that mentions undo — the Customer Area's
// cards, the checkout success page a guest lands on seconds after paying, the
// Confirmation Link page they come back to (#121) — and the endpoint that
// actually performs the reversal are all answered by one function over one type.
// The Customer Area's rows carry these five fields among twenty others; a guest
// checkout read carries only these five, because a guest is told a deadline and
// never a purchase history.
//
// Notice what is not here: the amount, the Organization, the buyer. None of them
// enters the rule (ADR 0018), and a struct that cannot carry them cannot leak
// them onto an unauthenticated response.
type SaleReversalFacts struct {
	// Channel is the Sales Channel: only OnlineSalesChannel can be reversed.
	Channel string
	// Status is the sale's own state; a reversed sale cannot be reversed again.
	Status string
	// SoldAt is the Payment approval instant, which opens the window and fixes
	// the Ecuadorian calendar date whose 20:00 closes it.
	SoldAt time.Time
	// PaymentMethod is who settled the money, and therefore who would have to
	// give it back. Null on the channels that carry none.
	PaymentMethod sql.NullString
	// EventStartsAt closes the window early when the doors open first.
	EventStartsAt sql.NullTime
}

// ReversalRefusal is why a Ticket Sale may not be reversed right now, or
// ReversalAllowed when it may.
//
// It is a reason and not a message. The two callers of the rule want very
// different things from it — the Customer Area turns any refusal alike into "no
// offer", while the reversal endpoint turns each into its own typed error and
// HTTP status — so what is shared is the decision, and each module keeps its own
// output.
type ReversalRefusal int

const (
	// ReversalAllowed: this sale can be reversed at the instant asked about.
	ReversalAllowed ReversalRefusal = iota
	// ReversalNotAnOnlineSale: an In-Person or imported sale records money the
	// platform never held.
	ReversalNotAnOnlineSale
	// ReversalAlreadyReversed: the sale has already been undone, by this buyer
	// moments ago or by a staff Sale Import undo.
	ReversalAlreadyReversed
	// ReversalPaymentNotReversible: the Payment Provider that collected the money
	// cannot give it back.
	ReversalPaymentNotReversible
	// ReversalWindowClosed: past 20:00 Ecuador time on the day of purchase, or
	// the Event has started — including a sale whose Event has no recorded start
	// at all, which is a sale with no window rather than an unbounded one.
	ReversalWindowClosed
)

// EligibilityAt is THE decision of ADR 0018: may this Ticket Sale be reversed at
// this instant, and if so, until when?
//
// It lives here, on the boundary object that already owns both the Reversal
// Window and the question of whether a Payment can be given back, because two
// modules need the same answer for opposite purposes. The customers module asks
// it to decide whether to OFFER the undo; the sales module asks it to decide
// whether to PERFORM one. Two copies of these four checks would eventually
// disagree, and the shape of that disagreement is an Undo button that appears on
// a sale the endpoint then refuses.
//
// The order of the checks is part of the answer, not an implementation detail:
// the most permanent reason a sale cannot be undone is reported first, so a
// buyer is told "this was never yours to undo here" rather than "your time ran
// out" about a sale for which time was never running.
//
// Nothing here writes, calls out, or blocks. It is a pure function of the facts,
// the clock and the configured Payment Provider's capability — which is what
// lets an offer surface call it on every row of a list.
//
// The window it returns is meaningful only alongside ReversalAllowed. A closing
// time on a sale nobody may reverse is a deadline that means nothing, so a
// refusal returns the zero window and a caller that publishes one cannot mislead
// somebody with it.
func (r PaymentReversal) EligibilityAt(sale SaleReversalFacts, now time.Time) (ReversalWindow, ReversalRefusal) {
	if sale.Channel != OnlineSalesChannel {
		return ReversalWindow{}, ReversalNotAnOnlineSale
	}
	if sale.Status != ActiveSaleStatus {
		return ReversalWindow{}, ReversalAlreadyReversed
	}
	// A free claim always can be undone — nothing was collected, so voiding the
	// sale is the whole reversal (ADR 0017) — while a paid one can only when the
	// Payment Provider that collected it supports reversal.
	if !r.Supports(sale.PaymentMethod.String) {
		return ReversalWindow{}, ReversalPaymentNotReversible
	}
	// An Online Sale always has an Event start — publishing requires one and only
	// a published Event is sellable — so a missing start is a sale with no window
	// rather than a sale with an unbounded one. Inventing a deadline in that case
	// would be inventing permission.
	if !sale.EventStartsAt.Valid {
		return ReversalWindow{}, ReversalWindowClosed
	}
	// SoldAt is the Payment approval instant on an Online Sale: the sale commits
	// in the transaction that approves the Payment.
	window := NewReversalWindow(sale.SoldAt, sale.EventStartsAt.Time)
	if !window.IsOpenAt(now) {
		return ReversalWindow{}, ReversalWindowClosed
	}
	return window, ReversalAllowed
}
