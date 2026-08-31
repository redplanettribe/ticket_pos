package legal_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// Cancelling a scheduled edition (#564), as a question about the LINEAGE alone.
//
// The whole of the feature's safety is here: a cancelled edition must never
// become current, never lift the gating floor and never enter the satisfying
// set, and it must be excluded by the rule rather than by anything firing at
// midnight. These tests walk the rule directly, so a later change that made a
// cancellation depend on a job having run has to argue with them first.

// cancelled marks an edition withdrawn, the way the lineage read carries the
// mark back from `cancelled_at IS NOT NULL`.
func cancelled(e legal.Edition) legal.Edition {
	e.Cancelled = true
	return e
}

// TestACancelledEditionNeverBecomesTheFloor is the acceptance criterion stated
// as a rule: the newest gating edition whose day has arrived, SKIPPING the one
// that was withdrawn.
//
// The dangerous case is the one written first — a cancelled edition whose day
// arrived anyway, because the row is retained and the calendar does not care.
// Nothing deletes it and nothing rewrites its date, so the floor has to refuse
// it on its own every time it is computed.
func TestACancelledEditionNeverBecomesTheFloor(t *testing.T) {
	live := edition("1", 1, 0, 1, 1, true)
	withdrawn := cancelled(edition("2-cancelled", 2, 0, 5, 5, true))

	floor, ok := legal.GatingFloor([]legal.Edition{live, withdrawn})
	if !ok || floor.ID != live.ID {
		t.Fatalf("GatingFloor = %+v (ok=%v); want the live edition %q", floor, ok, live.ID)
	}
}

// TestACancelledEditionIsNotInTheSatisfyingSet covers both halves of the walk:
// a cancelled edition BELOW the floor is out because everything below the floor
// is, and a cancelled edition ABOVE it is out because the prefix skips it too.
//
// The second half is the one that matters. Everything above the floor is in the
// set on purpose — somebody shown a future-dated edition and asked to accept it
// stands on it honestly — and a withdrawn edition is precisely the one nobody
// was ever shown.
func TestACancelledEditionIsNotInTheSatisfyingSet(t *testing.T) {
	superseded := edition("1", 1, 0, 1, 1, true)
	floor := edition("2", 2, 0, 3, 3, true)
	correctionAbove := edition("2.1", 2, 1, 4, 4, true)
	withdrawnAbove := cancelled(edition("3-cancelled", 3, 0, 9, 9, false))

	set, ok := legal.Satisfying([]legal.Edition{superseded, floor, correctionAbove, withdrawnAbove})
	if !ok {
		t.Fatalf("Satisfying reported no floor")
	}
	if set.Contains(withdrawnAbove.ID) {
		t.Fatalf("the withdrawn edition %q clears the gate: %v", withdrawnAbove.ID, set.IDs())
	}
	if set.Contains(superseded.ID) {
		t.Fatalf("a superseded edition clears the gate: %v", set.IDs())
	}
	if !set.Contains(floor.ID) || !set.Contains(correctionAbove.ID) {
		t.Fatalf("Satisfying = %v; want the floor and the correction above it", set.IDs())
	}
}

// TestEverybodyCancellingLeavesTheOlderFloorStanding is the "re-gates nobody"
// property from the other side: withdrawing the newest gating edition returns
// the platform to exactly the set it had before that edition was published, so
// somebody clear yesterday is clear today.
func TestEverybodyCancellingLeavesTheOlderFloorStanding(t *testing.T) {
	standing := edition("1", 1, 0, 1, 1, true)
	scheduled := edition("2", 2, 0, 5, 5, true)

	before, _ := legal.Satisfying([]legal.Edition{standing})
	after, ok := legal.Satisfying([]legal.Edition{standing, cancelled(scheduled)})
	if !ok {
		t.Fatalf("Satisfying reported no floor after a cancellation")
	}
	if len(after) != len(before) || !after.Contains(standing.ID) {
		t.Fatalf("Satisfying after the cancellation = %v; want the untouched %v", after.IDs(), before.IDs())
	}
}

// TestScheduledEditionsAreTheOnesStillWaiting pins what the banner shows and,
// with it, when the cancel control exists: an edition whose day has not come and
// which has not been withdrawn.
//
// The control's disappearance is not a separate rule — an arrived edition is
// simply not in this list, and it leaves the list at midnight because `Arrived`
// is the database's own answer about its own day.
func TestScheduledEditionsAreTheOnesStillWaiting(t *testing.T) {
	current := edition("1", 1, 0, 1, 1, true)
	soon := edition("2", 2, 0, 5, 5, false)
	later := edition("3", 3, 0, 9, 9, false)
	withdrawn := cancelled(edition("4-cancelled", 4, 0, 11, 11, false))

	scheduled := legal.ScheduledEditions([]legal.Edition{current, soon, later, withdrawn})
	if len(scheduled) != 2 {
		t.Fatalf("ScheduledEditions = %+v; want the two still waiting", scheduled)
	}
	// Newest first, the ordering every selector in this codebase uses.
	if scheduled[0].ID != later.ID || scheduled[1].ID != soon.ID {
		t.Fatalf("ScheduledEditions order = %q, %q; want newest first", scheduled[0].ID, scheduled[1].ID)
	}
}

// TestACancelledGenerationIsStillSpent is the one place a cancelled edition
// counts. The row is retained so the label cannot be reused — migration 110's
// UNIQUE would refuse it anyway, and an ambiguous `3` would be worse than a
// refusal — so the next gating publication takes the number after it.
func TestACancelledGenerationIsStillSpent(t *testing.T) {
	editions := []legal.Edition{
		edition("1", 1, 0, 1, 1, true),
		cancelled(edition("2-cancelled", 2, 0, 5, 5, false)),
	}
	if next := legal.NextGating(editions); next.Label() != "3" {
		t.Fatalf("NextGating = %q; want 3, because a withdrawn edition still spent 2", next.Label())
	}
}
