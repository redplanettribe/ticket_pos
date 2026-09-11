package catalog

import "time"

// ClosedAt reports whether a Ticket Type whose Sales Cutoff is salesCutoffAt
// has closed by the instant `at` — still listed, still described, still priced,
// and no longer buyable on the Storefront (ADR 0070).
//
// A nil cutoff never closes: "this Ticket Type never stops selling" and "it
// stops at T" are different statements, and only the absence of a value makes
// the first one without inviting arithmetic.
//
// Half-open like the Promotion's window and closed at the same end of it: the
// cutoff instant itself is closed, so a Ticket Type stops selling the moment
// its cutoff is reached rather than the moment after. Setting one Ticket Type's
// cutoff to the instant another's window opens therefore hands over cleanly,
// with no second in which both or neither are on sale.
//
// The instant is a parameter rather than a clock read, exactly as
// EffectiveBasePriceCents' is: callers pass their own injected clock's now, and
// nothing about closing is ever stored, so clearing the cutoff or moving it
// forward reopens sales on the very next read. That is the undo, and it is why
// there is no state column and no job that closes anything.
func ClosedAt(salesCutoffAt *time.Time, at time.Time) bool {
	if salesCutoffAt == nil {
		return false
	}
	return !at.Before(*salesCutoffAt)
}
