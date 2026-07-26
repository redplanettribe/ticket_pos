package sales_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// launchRates are the rates configured at launch: 10% Platform Fee, 15% Fee IVA.
var launchRates = sales.FeeRates{FeeBasisPoints: 1000, FeeIVABasisPoints: 1500}

func TestFeeRatesWithhold(t *testing.T) {
	cases := []struct {
		name        string
		baseCents   int
		feeCents    int
		feeIVACents int
	}{
		{name: "comp ticket carries no fee", baseCents: 0, feeCents: 0, feeIVACents: 0},
		{name: "round ten dollars", baseCents: 1000, feeCents: 100, feeIVACents: 15},
		{name: "seven ninety-nine rounds the fee up", baseCents: 799, feeCents: 80, feeIVACents: 12},
		{name: "one cent rounds the fee away", baseCents: 1, feeCents: 0, feeIVACents: 0},
		{name: "half a cent of fee rounds up", baseCents: 5, feeCents: 1, feeIVACents: 0},
		// The IVA is taken on the ALREADY-ROUNDED fee: 95¢ yields a 9.5¢ fee that
		// rounds to 10¢, whose IVA is exactly 1.5¢ and rounds to 2¢. Taxing the
		// unrounded 9.5¢ would have yielded 1.425¢ → 1¢, so this row is the one
		// that pins the order of the two roundings.
		{name: "iva is taken on the rounded fee", baseCents: 95, feeCents: 10, feeIVACents: 2},
		{name: "half a cent of iva rounds up", baseCents: 250, feeCents: 25, feeIVACents: 4},
		{name: "just under half a cent of iva rounds down", baseCents: 350, feeCents: 35, feeIVACents: 5},
		{name: "twenty five fifty", baseCents: 2550, feeCents: 255, feeIVACents: 38},
		{name: "ten fifty", baseCents: 1050, feeCents: 105, feeIVACents: 16},
		{name: "a hundred twenty three forty five", baseCents: 12345, feeCents: 1235, feeIVACents: 185},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := launchRates.Withhold(tc.baseCents)
			if got.FeeCents != tc.feeCents || got.FeeIVACents != tc.feeIVACents {
				t.Fatalf("Withhold(%d) = fee %d, fee IVA %d; want fee %d, fee IVA %d",
					tc.baseCents, got.FeeCents, got.FeeIVACents, tc.feeCents, tc.feeIVACents)
			}
			if got.TotalCents() != tc.feeCents+tc.feeIVACents {
				t.Fatalf("TotalCents() = %d; want %d", got.TotalCents(), tc.feeCents+tc.feeIVACents)
			}
		})
	}
}

// Zero rates are what a deployment that has switched the fee off looks like:
// every ticket keeps its whole price.
func TestFeeRatesWithholdZeroRates(t *testing.T) {
	got := sales.FeeRates{}.Withhold(9999)
	if got.FeeCents != 0 || got.FeeIVACents != 0 {
		t.Fatalf("Withhold with zero rates = %+v; want no withholding", got)
	}
}

func TestFeeHandlingBuyerAndNetPrices(t *testing.T) {
	// $7.99 under pass-on: the buyer covers the 80¢ fee and its 12¢ IVA, and the
	// Organization nets exactly the price it set.
	if got := launchRates.BuyerUnitPriceCents(sales.FeeHandlingPassOn, 799); got != 891 {
		t.Fatalf("pass-on buyer price = %d; want 891", got)
	}
	if got := launchRates.NetProceedsUnitCents(sales.FeeHandlingPassOn, 799); got != 799 {
		t.Fatalf("pass-on net proceeds = %d; want 799", got)
	}
	// Under absorb the buyer sees the set price and the Organization eats both.
	if got := launchRates.BuyerUnitPriceCents(sales.FeeHandlingAbsorb, 799); got != 799 {
		t.Fatalf("absorb buyer price = %d; want 799", got)
	}
	if got := launchRates.NetProceedsUnitCents(sales.FeeHandlingAbsorb, 799); got != 707 {
		t.Fatalf("absorb net proceeds = %d; want 707", got)
	}
}

func TestParseFeeHandling(t *testing.T) {
	for _, raw := range []string{"pass_on", "absorb"} {
		if _, ok := sales.ParseFeeHandling(raw); !ok {
			t.Fatalf("ParseFeeHandling(%q) rejected a valid value", raw)
		}
	}
	for _, raw := range []string{"", "PASS_ON", "passon", "none"} {
		if _, ok := sales.ParseFeeHandling(raw); ok {
			t.Fatalf("ParseFeeHandling(%q) accepted an invalid value", raw)
		}
	}
}
