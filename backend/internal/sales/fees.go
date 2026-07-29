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

// FeeHandlingOrDefault reads a stored Fee Handling, falling back to the default
// mode. The column's CHECK admits nothing else, so the fallback stands in for a
// value that cannot occur rather than being a policy of its own — no read is
// worth failing a page load or a checkout over.
func FeeHandlingOrDefault(raw string) FeeHandling {
	if handling, ok := ParseFeeHandling(raw); ok {
		return handling
	}
	return FeeHandlingPassOn
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

// LineNetProceedsSQL is the Net Proceeds of one Ticket Sale Line (aliased tsl)
// as SQL, read off the snapshot the line froze: quantity × (what the Customer
// paid − the Platform Fee − the Fee IVA withheld). It is the same arithmetic
// NetProceedsUnitCents does in Go, and it lives here — beside it, in the package
// that owns the fee vocabulary — because more than one module now sums Net
// Proceeds: the Event's summary and the Organization's Withdrawable Balance in
// sales, and an Affiliate Link's attributed figures in affiliates. One
// definition is what keeps the figure the same wherever it is shown (ADR 0014).
const LineNetProceedsSQL = `tsl.quantity * (tsl.unit_price_cents - tsl.fee_cents - tsl.fee_iva_cents)`

// FeeSnapshot is the per-unit economics of one checkout line, frozen at
// begin-checkout: the price the Organization set, the buyer price the Event's
// Fee Handling turns it into, the withholding the platform takes from it, and
// the rates that produced both. It is written onto the Payment's lines and
// copied onto the Ticket Sale's, so neither a rate change nor a Fee Handling
// flip afterwards can move economics already recorded (ADR 0014).
type FeeSnapshot struct {
	BasePriceCents      int
	BuyerUnitPriceCents int
	FeeCents            int
	FeeIVACents         int
	FeeBasisPoints      int
	FeeIVABasisPoints   int
}

// SnapshotUnit freezes one unit of a Ticket Type the Organization priced at
// baseCents, sold under the Event's Fee Handling.
func (r FeeRates) SnapshotUnit(handling FeeHandling, baseCents int) FeeSnapshot {
	fee := r.Withhold(baseCents)
	return FeeSnapshot{
		BasePriceCents:      baseCents,
		BuyerUnitPriceCents: r.BuyerUnitPriceCents(handling, baseCents),
		FeeCents:            fee.FeeCents,
		FeeIVACents:         fee.FeeIVACents,
		FeeBasisPoints:      r.FeeBasisPoints,
		FeeIVABasisPoints:   r.FeeIVABasisPoints,
	}
}

// applyRate takes a basis-point rate of an integer-cent amount, rounded half-up.
// Both operands are non-negative here, so integer division truncates towards
// zero and the +5000 is exactly the half-up bias.
func applyRate(cents, basisPoints int) int {
	return (cents*basisPoints + 5000) / 10000
}
