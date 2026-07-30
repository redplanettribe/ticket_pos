package sales_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// TestReversalRetryDelayFollowsTheSchedule pins the backoff ADR 0024 states:
// 10s, 30s, 2m, 5m, 15m, then every 30m.
//
// It is checked without jitter so the steps themselves are the assertion. What
// jitter may do to them is the next test's business, and pinning the two
// together would leave neither pinned.
func TestReversalRetryDelayFollowsTheSchedule(t *testing.T) {
	cases := []struct {
		name     string
		attempts int
		want     time.Duration
	}{
		{name: "after the press itself", attempts: 1, want: 10 * time.Second},
		{name: "second unknown answer", attempts: 2, want: 30 * time.Second},
		{name: "third", attempts: 3, want: 2 * time.Minute},
		{name: "fourth", attempts: 4, want: 5 * time.Minute},
		{name: "fifth", attempts: 5, want: 15 * time.Minute},
		// From here the schedule stops growing: every half hour, for as long as
		// the request is pursued at all.
		{name: "sixth settles at the ceiling", attempts: 6, want: 30 * time.Minute},
		{name: "and stays there", attempts: 40, want: 30 * time.Minute},
		// A request that has never been asked about is due now, and the delay it
		// would be given is the first step rather than the ceiling.
		{name: "no attempts yet", attempts: 0, want: 10 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sales.ReversalRetryDelay(tc.attempts, 0); got != tc.want {
				t.Fatalf("ReversalRetryDelay(%d, 0) = %v, want %v", tc.attempts, got, tc.want)
			}
		})
	}
}

// TestReversalRetryJitterOnlyEverDelays: jitter spreads a herd of stranded
// Reversal Requests apart, and it may never move one EARLIER than the schedule
// allows.
//
// That direction is the whole safety property. The schedule is what protects a
// provider that has already failed to answer, so a jitter that could subtract
// would let the platform probe sooner than it decided it may — quietly, and
// exactly during the outage the backoff exists for.
func TestReversalRetryJitterOnlyEverDelays(t *testing.T) {
	// The ids are arbitrary and the fractions they produce are not: a fraction
	// derived from the id is the same on every step, which is what keeps two
	// requests stranded together from colliding forever.
	for _, id := range []string{
		"a2f0b1c4-0000-4000-8000-000000000001",
		"7c3d9e11-0000-4000-8000-000000000002",
		"ffffffff-0000-4000-8000-000000000003",
		"",
	} {
		jitter := sales.ReversalRetryJitter(id)
		if jitter < 0 || jitter >= 1 {
			t.Fatalf("ReversalRetryJitter(%q) = %v, want a fraction in [0, 1)", id, jitter)
		}
		for attempts := 1; attempts <= 8; attempts++ {
			plain := sales.ReversalRetryDelay(attempts, 0)
			jittered := sales.ReversalRetryDelay(attempts, jitter)
			if jittered < plain {
				t.Fatalf("attempt %d for %q waits %v with jitter and %v without; jitter must never bring a probe forward",
					attempts, id, jittered, plain)
			}
			if jittered > plain+plain/4 {
				t.Fatalf("attempt %d for %q waits %v, want at most a fifth over the %v step", attempts, id, jittered, plain)
			}
		}
	}

	// Derived from the id and therefore repeatable: the same row asked about
	// twice is scheduled identically, which is what makes the delay a thing a
	// test can assert on at all.
	if a, b := sales.ReversalRetryJitter("same-id"), sales.ReversalRetryJitter("same-id"); a != b {
		t.Fatalf("ReversalRetryJitter is not stable for one id: %v then %v", a, b)
	}
}

// TestReversalGivenUpAfterADay: the platform pursues a Reversal Request for a
// day from the moment the Customer pressed, and then records an Unresolved
// Reversal.
//
// The instant it measures from is the point. requested_at is the buyer's ask;
// measuring from the last attempt instead would let a request that kept being
// probed be pursued forever, which is the row-retrying-forever ADR 0024 refused.
func TestReversalGivenUpAfterADay(t *testing.T) {
	pressed := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "the moment they pressed", now: pressed, want: false},
		{name: "an hour in", now: pressed.Add(time.Hour), want: false},
		{name: "a second short of the bound", now: pressed.Add(24*time.Hour - time.Second), want: false},
		{name: "exactly a day", now: pressed.Add(24 * time.Hour), want: true},
		{name: "a week", now: pressed.Add(7 * 24 * time.Hour), want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sales.ReversalGivenUp(pressed, tc.now); got != tc.want {
				t.Fatalf("ReversalGivenUp(pressed, pressed+%v) = %v, want %v", tc.now.Sub(pressed), got, tc.want)
			}
		})
	}
}
