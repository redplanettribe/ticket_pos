package catalog

import "time"

// Promotion is a time-boxed override of a Ticket Type's List Price (ADR 0021).
// A Ticket Type holds at most one. StartsAt is nil when the Promotion is live
// from the moment it was saved; EndsAt is always set, because a Promotion that
// never ends is a List Price change.
type Promotion struct {
	PromotionalPriceCents int
	StartsAt              *time.Time
	EndsAt                time.Time
}

// LiveAt reports whether the Promotion is in force at the given instant. The
// window is half-open — start inclusive, end exclusive — so a Promotion is over
// the moment its end is reached and two consecutive windows could never both
// answer yes.
func (p *Promotion) LiveAt(at time.Time) bool {
	if p == nil {
		return false
	}
	if p.StartsAt != nil && at.Before(*p.StartsAt) {
		return false
	}
	return at.Before(p.EndsAt)
}

// ConstrainsListPriceAt reports whether the Promotion still has a claim on the
// List Price at the given instant — true until it ends, so a Promotion that has
// not started yet binds just as a live one does: it is a discount the
// Organization has already committed to, and a List Price falling under it
// would invert the discount before it ever opened. Once the window has closed
// the row constrains nothing, and the List Price is the Organization's business
// alone again.
//
// Wider than LiveAt on purpose: LiveAt answers what a Customer pays, this
// answers what staff may set.
func (p *Promotion) ConstrainsListPriceAt(at time.Time) bool {
	if p == nil {
		return false
	}
	return at.Before(p.EndsAt)
}

// EffectiveBasePriceCents answers what a Ticket Type costs at instant `at`
// before any Fee Handling arithmetic: the Promotional Price while the Promotion
// is live, the List Price otherwise. It is the single place that question is
// answered — every Sales Channel that prices from the catalog reads it, and
// checkout snapshots the answer at begin-checkout so an expiry mid-purchase
// never re-prices a Payment already under way.
//
// The instant is a parameter rather than a clock read so callers pass their own
// injected clock's now, and so a caller pricing a past or future moment gets a
// straight answer.
func EffectiveBasePriceCents(listPriceCents int, promotion *Promotion, at time.Time) int {
	if promotion.LiveAt(at) {
		return promotion.PromotionalPriceCents
	}
	return listPriceCents
}
