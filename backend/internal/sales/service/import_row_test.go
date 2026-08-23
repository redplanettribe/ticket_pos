package service

import "testing"

// A typed amount is re-stringified before the validator sees it, and the
// rendering must be an honest major-currency amount for every int it can be
// handed — including the negative ones, which exist only to be refused.
//
// This is pinned as a unit test because it CANNOT be caught at the HTTP seam
// today: the amount rule has one complaint, "must be a non-negative amount",
// and a nonsense rendering earns exactly the same refusal a well-formed
// negative does. The bug it guards is therefore invisible from outside until
// the day that rule tells the two apart, which is the day it starts blaming
// the wrong one on both typed routes at once (#367, ADR 0052).
func TestATypedAmountRendersAsAnHonestAmount(t *testing.T) {
	cases := []struct {
		name  string
		cents int
		want  string
	}{
		{"whole units", 4500, "45.00"},
		{"units and cents", 1234, "12.34"},
		{"sub-unit", 50, "0.50"},
		{"a comp", 0, "0.00"},
		{"negative whole units", -100, "-1.00"},
		// The regression: -50/100 truncates toward zero, so the naive rendering
		// puts the sign on the cents and produces "0.-50".
		{"negative sub-unit", -50, "-0.50"},
		{"negative units and cents", -1234, "-12.34"},
	}
	for _, tc := range cases {
		cents := tc.cents
		got := ImportRowInput{AmountCents: &cents}.rawRow().Amount
		if got != tc.want {
			t.Errorf("%s: amount %d rendered as %q, want %q", tc.name, tc.cents, got, tc.want)
		}
	}

	// A nil amount is a blank cell, which means the Ticket Type's own price.
	if got := (ImportRowInput{}).rawRow().Amount; got != "" {
		t.Errorf("nil amount rendered as %q, want the blank cell", got)
	}
}
