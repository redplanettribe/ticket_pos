package sri

import "testing"

// BackOutIVA (#473): base + IVA is the cents paid, every time, and the base
// is the half-up rounding of paid / 1.15.
func TestBackOutIVASumsToWhatWasPaid(t *testing.T) {
	cases := []struct {
		paid, base, iva int64
	}{
		{0, 0, 0},
		{1, 1, 0},        // 0.87 rounds up to 1; the IVA on a cent is nothing
		{115, 100, 15},   // exact
		{1115, 970, 145}, // 969.565… rounds up
		{2230, 1939, 291},
		{1000, 870, 130},  // 869.565… rounds up
		{2300, 2000, 300}, // exact
		{999, 869, 130},   // 868.695… rounds up
		{1150, 1000, 150},
	}
	for _, c := range cases {
		base, iva, err := BackOutIVA(c.paid, IVACode15)
		if err != nil {
			t.Fatalf("BackOutIVA(%d): %v", c.paid, err)
		}
		if base != c.base || iva != c.iva {
			t.Fatalf("BackOutIVA(%d) = %d + %d; want %d + %d", c.paid, base, iva, c.base, c.iva)
		}
		if base+iva != c.paid {
			t.Fatalf("BackOutIVA(%d) = %d + %d, which is not what was paid", c.paid, base, iva)
		}
	}
	// Exhaustively: the invariant holds for every price under $100.
	for paid := int64(0); paid < 10000; paid++ {
		base, iva, err := BackOutIVA(paid, IVACode15)
		if err != nil || base+iva != paid || iva < 0 || base < 0 {
			t.Fatalf("BackOutIVA(%d) = %d + %d, err %v", paid, base, iva, err)
		}
	}
}

func TestBackOutIVAAtZeroRateIsTheWholePrice(t *testing.T) {
	for _, code := range []IVACode{IVACodeZero, IVACodeExento, IVACodeNoObjeto} {
		base, iva, err := BackOutIVA(1234, code)
		if err != nil || base != 1234 || iva != 0 {
			t.Fatalf("BackOutIVA(1234, %s) = %d + %d, err %v; want 1234 + 0", code, base, iva, err)
		}
	}
}

func TestBackOutIVARefusesWhatItCannotSplit(t *testing.T) {
	if _, _, err := BackOutIVA(-1, IVACode15); err == nil {
		t.Fatal("a negative amount was accepted")
	}
	if _, _, err := BackOutIVA(100, IVACode("9")); err == nil {
		t.Fatal("an unknown IVA code was accepted")
	}
}
