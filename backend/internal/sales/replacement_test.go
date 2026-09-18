package sales_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// IsUpgradeReplacement is the question every reading surface asks, and the
// asymmetry of its answers is the whole point: only the one word, on a link that
// is really there, moves a surface off the sentence it printed before ADR 0074
// existed. A Sale Correction — and anything nobody here recognises — keeps
// reading `corrected`.
func TestIsUpgradeReplacement(t *testing.T) {
	linked := "sale-2"
	correction := sales.ReplacementReasonCorrection
	upgrade := sales.ReplacementReasonUpgrade
	unknown := "some_later_reason"
	shouted := "UPGRADE"
	empty := ""
	cases := []struct {
		name   string
		link   *string
		reason *string
		want   bool
	}{
		{"an Upgrade's link", &linked, &upgrade, true},
		{"a Sale Correction's link", &linked, &correction, false},
		{"no link and no reason", nil, nil, false},
		{"a reason this build has never heard of", &linked, &unknown, false},
		{"the right word in the wrong case", &linked, &shouted, false},
		{"the empty reason", &linked, &empty, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sales.IsUpgradeReplacement(tc.link, tc.reason); got != tc.want {
				t.Errorf("IsUpgradeReplacement = %v, want %v", got, tc.want)
			}
		})
	}
}

// THE PAID SALE CARRIES THE SAME REASON AND MUST NOT ANSWER FOR THE OTHER HALF.
//
// The reason is written on both rows so either can be displayed without a join
// (#650), which means the reason alone says "an Upgrade happened near this Sale"
// and not "this Sale was given up". Asked about the link it does NOT have, the
// paid Sale answers no — and that is what keeps it reading `reversed` when its
// buyer later undoes it in their Reversal Window, or an Operator does after
// refunding them, rather than blaming a lever nobody pulled.
func TestIsUpgradeReplacementIsAskedPerLinkAndNotPerSale(t *testing.T) {
	freeSale, paidSale := "free-sale", "paid-sale"
	upgrade := sales.ReplacementReasonUpgrade

	// The surrendered free Sale: replaced_by names the paid Sale.
	if !sales.IsUpgradeReplacement(&paidSale, &upgrade) {
		t.Error("the surrendered free Sale's replaced_by link is not read as an Upgrade")
	}
	// The paid Sale asked about ITS replaced_by, which is empty: it replaced
	// something, nothing replaced it.
	if sales.IsUpgradeReplacement(nil, &upgrade) {
		t.Error("the paid Sale claims it was surrendered in an Upgrade; it is the Sale that took the place")
	}
	// And asked about its replaces link, which it does have.
	if !sales.IsUpgradeReplacement(&freeSale, &upgrade) {
		t.Error("the paid Sale's replaces link is not read as an Upgrade")
	}
}
