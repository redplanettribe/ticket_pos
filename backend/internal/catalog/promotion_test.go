package catalog_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

func TestEffectiveBasePriceCents(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	scheduled := &catalog.Promotion{PromotionalPriceCents: 2500, StartsAt: &start, EndsAt: end}
	open := &catalog.Promotion{PromotionalPriceCents: 0, EndsAt: end}

	cases := []struct {
		name      string
		promotion *catalog.Promotion
		at        time.Time
		want      int
	}{
		{"no promotion is the list price", nil, start, 5000},
		{"before the window is the list price", scheduled, start.Add(-time.Second), 5000},
		{"the start instant is inside the window", scheduled, start, 2500},
		{"mid-window is the promotional price", scheduled, start.Add(24 * time.Hour), 2500},
		{"the end instant is outside the window", scheduled, end, 5000},
		{"after the window is the list price", scheduled, end.Add(time.Second), 5000},
		{"an open start is live from any earlier instant", open, start.Add(-720 * time.Hour), 0},
		{"an open start still ends", open, end, 5000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := catalog.EffectiveBasePriceCents(5000, tc.promotion, tc.at); got != tc.want {
				t.Fatalf("EffectiveBasePriceCents = %d, want %d", got, tc.want)
			}
		})
	}
}
