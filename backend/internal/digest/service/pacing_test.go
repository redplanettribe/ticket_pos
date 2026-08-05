package service

import (
	"context"
	"testing"
	"time"
)

// Pacing the send to the provider's rate limit (US #63, ADR 0009, ADR 0030).
//
// The batch size bounds how many messages one run may send. It does NOT bound
// how fast it sends them, and the difference is the whole of this file: fifty
// sends is "about twenty-five seconds" only if the provider takes about half a
// second to answer, which is a guess about somebody else's latency rather than a
// limit anybody holds. Answered quickly, the same batch leaves at twenty a
// second against a limit ADR 0009 records as roughly two — and the provider
// begins refusing mail that a Customer will never learn was meant for them.
//
// These tests are unit tests rather than integration ones on purpose. What is
// being asserted is real elapsed time, which the integration suite's fixed clock
// cannot see and which every other Digest test opts out of (see the harness).

// TestTheFirstSendOfARunIsNotDelayed pins the exception. Pacing exists to space
// sends from each other; a tick with one Digest to deliver has nothing to space
// it from, and should answer as fast as it can.
func TestTheFirstSendOfARunIsNotDelayed(t *testing.T) {
	s := &Service{}

	start := time.Now()
	if err := s.waitForSendSlot(context.Background(), 1); err != nil {
		t.Fatalf("waiting for the first send slot: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("the first send of a run waited %v, want no wait", elapsed)
	}
}

// TestEverySendAfterTheFirstHoldsTheProviderInterval is the rule itself. It
// asserts the gap is actually held rather than assumed by the batch size, and it
// is the test that fails if somebody deletes the wait and leaves the comment.
func TestEverySendAfterTheFirstHoldsTheProviderInterval(t *testing.T) {
	s := (&Service{}).WithSendInterval(30 * time.Millisecond)

	start := time.Now()
	for claimed := 2; claimed <= 4; claimed++ {
		if err := s.waitForSendSlot(context.Background(), claimed); err != nil {
			t.Fatalf("waiting for send slot %d: %v", claimed, err)
		}
	}
	// Three waits of thirty milliseconds. Asserted as a floor and never as a
	// ceiling: a slow machine may take longer and that is not a failure, but
	// taking LESS time means the gap was not held.
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
		t.Fatalf("three paced sends took %v, want at least 90ms", elapsed)
	}
}

// TestTheDefaultIntervalHoldsTheRateADR0009Records ties the number to the
// document that justifies it. ADR 0009 records roughly two requests a second, so
// the gap between sends may not be shorter than half a second.
func TestTheDefaultIntervalHoldsTheRateADR0009Records(t *testing.T) {
	if digestSendInterval < 500*time.Millisecond {
		t.Fatalf("digestSendInterval = %v, want at least 500ms for the ~2/sec limit ADR 0009 records", digestSendInterval)
	}
	// And the batch must still fit the budget at that spacing, or a full run
	// would be cut off by the deadline holding an accepted send it had not
	// recorded — the one failure the deadline chain exists to prevent.
	if spacing := time.Duration(digestDrainBatch-1) * digestSendInterval; spacing >= digestDrainBudget {
		t.Fatalf("a full batch spends %v pacing, which does not fit the %v budget", spacing, digestDrainBudget)
	}
}

// TestAPacedRunStopsWhenTheCallerGoesAway proves the wait is not a place a
// shutdown can be ignored. A cancelled context must come back as an error, which
// the drain reads as "stop, do not send" — never as "send now".
func TestAPacedRunStopsWhenTheCallerGoesAway(t *testing.T) {
	s := (&Service{}).WithSendInterval(10 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if err := s.waitForSendSlot(ctx, 2); err == nil {
		t.Fatal("a cancelled run reported a send slot, want an error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a cancelled run waited %v before giving up, want to stop at once", elapsed)
	}
}
