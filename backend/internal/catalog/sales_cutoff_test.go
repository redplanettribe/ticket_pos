package catalog_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

func TestClosedAt(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 7, 20, 18, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		cutoff *time.Time
		at     time.Time
		want   bool
	}{
		{"no cutoff never closes", nil, cutoff.Add(8760 * time.Hour), false},
		{"long before the cutoff is open", &cutoff, cutoff.Add(-720 * time.Hour), false},
		{"a second before the cutoff is open", &cutoff, cutoff.Add(-time.Second), false},
		{"the cutoff instant itself is closed", &cutoff, cutoff, true},
		{"a second after the cutoff is closed", &cutoff, cutoff.Add(time.Second), true},
		{"long after the cutoff is closed", &cutoff, cutoff.Add(720 * time.Hour), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := catalog.ClosedAt(tc.cutoff, tc.at); got != tc.want {
				t.Fatalf("ClosedAt = %v, want %v", got, tc.want)
			}
		})
	}
}
