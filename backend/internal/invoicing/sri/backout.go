package sri

import "fmt"

// BackOutIVA splits an amount that already contains IVA into the base and
// the tax, in cents, for a document the platform prices the way the buyer
// paid it (#473, ADR 0060): a ticket's price is what the buyer was charged
// with the IVA inside, and the factura has to state a base such that base +
// IVA equals that price to the cent.
//
// THE BASE IS ROUNDED AND THE IVA IS THE REMAINDER, never the other way
// round, so the sum is the paid amount by construction rather than by luck:
// base = paid / (1 + rate), rounded half up; iva = paid − base. Dividing the
// other way (iva = paid × rate / (1 + rate)) and rounding both would sum to
// paid ± 1 on roughly one price in ten.
//
// Only the 15% rate is ever backed out today; the rate comes from the code
// so a 0% document has base = paid and iva = 0 without a special case.
func BackOutIVA(paidCents int64, code IVACode) (baseCents, ivaCents int64, err error) {
	if paidCents < 0 {
		return 0, 0, fmt.Errorf("%w: a paid amount must not be negative", ErrInvalidFactura)
	}
	rate, err := code.RatePercent()
	if err != nil {
		return 0, 0, err
	}
	baseCents = divRoundHalfUp(paidCents*100, int64(100+rate))
	return baseCents, paidCents - baseCents, nil
}
