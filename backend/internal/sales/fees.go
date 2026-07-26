package sales

// Platform Fee math (ADR 0014). The platform withholds a Platform Fee from the
// Organization on every Online Sale, plus the Fee IVA levied on that fee — the
// fee is the platform's taxable service, the tickets are not. This file is the
// single definition of that arithmetic: every buyer price, charged amount, and
// Net Proceeds figure in the system is a sum of values produced here.
//
// The arithmetic is per-unit and in integer cents, rounded half-up, with the
// Fee IVA taken on the ALREADY-ROUNDED fee. Per-unit means displayed unit
// prices, line totals and charged amounts are the same numbers added up in
// different orders, so they cannot drift by a penny against each other.

// FeeHandling is a per-Event choice of whether the buyer price is raised to
// cover the Platform Fee and its Fee IVA. It never changes who is charged: the
// fee is always withheld from the Organization.
type FeeHandling string

const (
	// FeeHandlingPassOn raises the buyer price by exactly fee + Fee IVA, so the
	// Organization nets the price it set. The default for every Event.
	FeeHandlingPassOn FeeHandling = "pass_on"
	// FeeHandlingAbsorb leaves the buyer price at the price the Organization set,
	// so the Organization nets that price minus fee and Fee IVA.
	FeeHandlingAbsorb FeeHandling = "absorb"
)

// ParseFeeHandling converts a stored or submitted value into a FeeHandling,
// reporting whether it is one of the two modes. Nothing is normalized: a value
// that is not exactly one of the stored spellings is a rejection, because the
// database CHECK constraint accepts nothing else either.
func ParseFeeHandling(raw string) (FeeHandling, bool) {
	switch FeeHandling(raw) {
	case FeeHandlingPassOn:
		return FeeHandlingPassOn, true
	case FeeHandlingAbsorb:
		return FeeHandlingAbsorb, true
	default:
		return "", false
	}
}

// FeeRates are the two configured rates, in basis points (1000 = 10%). They come
// from platform configuration rather than from code so that an IVA change is an
// ops action; every sale line snapshots the rates it used, so recorded economics
// never move when these do.
type FeeRates struct {
	FeeBasisPoints    int
	FeeIVABasisPoints int
}

// Fee is what the platform withholds from the Organization for one ticket.
type Fee struct {
	FeeCents    int
	FeeIVACents int
}

// TotalCents is the whole withholding — the amount a pass-on buyer price is
// raised by, and the amount subtracted from a ticket's price to get its Net
// Proceeds.
func (f Fee) TotalCents() int { return f.FeeCents + f.FeeIVACents }

// Withhold returns the Platform Fee and Fee IVA for one ticket sold at
// baseCents — the price the Organization set. A comp ticket (base 0) yields
// nothing to withhold.
func (r FeeRates) Withhold(baseCents int) Fee {
	if baseCents <= 0 {
		return Fee{}
	}
	fee := applyRate(baseCents, r.FeeBasisPoints)
	return Fee{FeeCents: fee, FeeIVACents: applyRate(fee, r.FeeIVABasisPoints)}
}

// BuyerUnitPriceCents is what the Customer pays for one ticket the Organization
// priced at baseCents, under the Event's Fee Handling.
func (r FeeRates) BuyerUnitPriceCents(handling FeeHandling, baseCents int) int {
	if handling == FeeHandlingPassOn {
		return baseCents + r.Withhold(baseCents).TotalCents()
	}
	return baseCents
}

// NetProceedsUnitCents is what one ticket priced at baseCents leaves the
// Organization: what the Customer paid, minus the withholding.
func (r FeeRates) NetProceedsUnitCents(handling FeeHandling, baseCents int) int {
	return r.BuyerUnitPriceCents(handling, baseCents) - r.Withhold(baseCents).TotalCents()
}

// applyRate takes a basis-point rate of an integer-cent amount, rounded half-up.
// Both operands are non-negative here, so integer division truncates towards
// zero and the +5000 is exactly the half-up bias.
func applyRate(cents, basisPoints int) int {
	return (cents*basisPoints + 5000) / 10000
}
