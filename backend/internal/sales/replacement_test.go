package sales_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// IsUpgradeReplacement is the question every reading surface asks, and the
// asymmetry of its answers is the whole point: only the one word moves a surface
// off the sentence it printed before ADR 0074 existed, so a Sale Correction —
// and anything nobody here recognises — keeps reading `corrected`.
func TestIsUpgradeReplacement(t *testing.T) {
	correction := sales.ReplacementReasonCorrection
	upgrade := sales.ReplacementReasonUpgrade
	unknown := "some_later_reason"
	shouted := "UPGRADE"
	cases := []struct {
		name   string
		reason *string
		want   bool
	}{
		{"an Upgrade", &upgrade, true},
		{"a Sale Correction", &correction, false},
		{"no replacement at all", nil, false},
		{"a reason this build has never heard of", &unknown, false},
		{"the right word in the wrong case", &shouted, false},
		{"the empty string", new(string), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sales.IsUpgradeReplacement(tc.reason); got != tc.want {
				t.Errorf("IsUpgradeReplacement = %v, want %v", got, tc.want)
			}
		})
	}
}
