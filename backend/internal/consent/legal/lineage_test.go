package legal_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// day builds an effective date the way a version row carries one: a day, not an
// instant.
func day(n int) time.Time {
	return time.Date(2026, time.September, n, 0, 0, 0, 0, time.UTC)
}

// edition is the shorthand these tests are written in: an id that names what
// the row is for a human reading a failure, its lineage, and its dates.
func edition(id string, generation, revision, effective, created int, arrived bool) legal.Edition {
	return legal.Edition{
		ID:            id,
		Lineage:       legal.Lineage{Generation: generation, Revision: revision},
		EffectiveDate: day(effective),
		CreatedAt:     day(created),
		Arrived:       arrived,
	}
}

// TestGatingIsRevisionZeroAndNothingElse pins the rule the whole feature stands
// on: an edition gates IF AND ONLY IF its revision is 0.
//
// There is no is_gating column, no gating_from column and no published_as
// column to consult — #553 removed promotion, which is the only thing those
// could have expressed — so this one test is the whole of the definition. If a
// later change makes "gating" depend on a second fact, this is the test that
// must be argued with first.
func TestGatingIsRevisionZeroAndNothingElse(t *testing.T) {
	for _, tc := range []struct {
		lineage legal.Lineage
		gating  bool
	}{
		{legal.Lineage{Generation: 0, Revision: 0}, true},
		{legal.Lineage{Generation: 1, Revision: 0}, true},
		{legal.Lineage{Generation: 99, Revision: 0}, true},
		{legal.Lineage{Generation: 1, Revision: 1}, false},
		{legal.Lineage{Generation: 1, Revision: 2}, false},
		{legal.Lineage{Generation: 7, Revision: 1}, false},
	} {
		if got := tc.lineage.Gating(); got != tc.gating {
			t.Errorf("Lineage%+v.Gating() = %v, want %v", tc.lineage, got, tc.gating)
		}
	}
}

// TestLabelIsTheFrozenRenderingOfLineage covers the only place a label is ever
// produced. Flat within the generation: a correction to a correction is 1.2 and
// never 1.1.1.
func TestLabelIsTheFrozenRenderingOfLineage(t *testing.T) {
	for _, tc := range []struct {
		lineage legal.Lineage
		label   string
	}{
		{legal.Lineage{Generation: 0}, "0"},
		{legal.Lineage{Generation: 1}, "1"},
		{legal.Lineage{Generation: 2}, "2"},
		{legal.Lineage{Generation: 1, Revision: 1}, "1.1"},
		{legal.Lineage{Generation: 1, Revision: 2}, "1.2"},
		{legal.Lineage{Generation: 12, Revision: 34}, "12.34"},
	} {
		if got := tc.lineage.Label(); got != tc.label {
			t.Errorf("Lineage%+v.Label() = %q, want %q", tc.lineage, got, tc.label)
		}
		parsed, ok := legal.ParseLabel(tc.label)
		if !ok || parsed != tc.lineage {
			t.Errorf("ParseLabel(%q) = %+v, %v, want %+v, true", tc.label, parsed, ok, tc.lineage)
		}
	}

	for _, label := range []string{"", "0-placeholder", "1.0", "1.", ".1", "01", "1.1.1", "v1", "-1", "1.01"} {
		if _, ok := legal.ParseLabel(label); ok {
			t.Errorf("ParseLabel(%q) accepted a label Label() would never have written", label)
		}
	}
}

// TestSatisfyingSetIsTheFloorAndEverythingAboveIt is the mechanism in one
// story: three gating editions and a correction, and only the ones at or above
// the newest arrived gating edition clear.
func TestSatisfyingSetIsTheFloorAndEverythingAboveIt(t *testing.T) {
	editions := []legal.Edition{
		edition("one", 1, 0, 1, 1, true),
		edition("two", 2, 0, 10, 10, true),
		edition("two-corrected", 2, 1, 12, 12, true),
		edition("three-scheduled", 3, 0, 30, 13, false),
	}

	floor, ok := legal.GatingFloor(editions)
	if !ok || floor.ID != "two" {
		t.Fatalf("GatingFloor = %+v, %v, want edition two — the newest ARRIVED revision-0 row", floor, ok)
	}

	set, ok := legal.Satisfying(editions)
	if !ok {
		t.Fatal("Satisfying reported no set with a gating edition in effect")
	}
	for _, id := range []string{"two", "two-corrected", "three-scheduled"} {
		if !set.Contains(id) {
			t.Errorf("edition %q is at or above the floor and must satisfy the gate; set = %v", id, set)
		}
	}
	if set.Contains("one") {
		t.Errorf("edition one is BELOW the floor and must not satisfy the gate; set = %v", set)
	}
	if set.Contains("never-published") {
		t.Error("an id that is not an edition must never satisfy the gate")
	}
}

// TestPublishingACorrectionReGatesNobody is the point of the whole ticket: a
// correction does not move the floor, so everybody who was clear stays clear —
// with no backfill and no column to maintain.
func TestPublishingACorrectionReGatesNobody(t *testing.T) {
	before := []legal.Edition{
		edition("one", 1, 0, 1, 1, true),
		edition("two", 2, 0, 10, 10, true),
	}
	after := append(append([]legal.Edition{}, before...), edition("two-corrected", 2, 1, 12, 12, true))

	floorBefore, _ := legal.GatingFloor(before)
	floorAfter, _ := legal.GatingFloor(after)
	if floorBefore.ID != floorAfter.ID {
		t.Fatalf("a correction moved the gating floor from %q to %q", floorBefore.ID, floorAfter.ID)
	}

	setBefore, _ := legal.Satisfying(before)
	setAfter, _ := legal.Satisfying(after)
	if !setBefore.Contains("two") || !setAfter.Contains("two") {
		t.Fatal("somebody holding edition two was cleared before the correction and must still be cleared after it")
	}
	if !setAfter.Contains("two-corrected") {
		t.Fatal("somebody shown the correction and accepting it must be cleared by it")
	}
}

// TestPublishingAGatingEditionReGatesEverybody is the other half: the floor
// rises and every earlier acceptance stops satisfying, with nothing notified.
func TestPublishingAGatingEditionReGatesEverybody(t *testing.T) {
	before := []legal.Edition{edition("one", 1, 0, 1, 1, true)}
	after := []legal.Edition{
		edition("one", 1, 0, 1, 1, true),
		edition("two", 2, 0, 10, 10, true),
	}

	setBefore, _ := legal.Satisfying(before)
	if !setBefore.Contains("one") {
		t.Fatal("edition one is the only edition and must satisfy the gate")
	}
	setAfter, _ := legal.Satisfying(after)
	if setAfter.Contains("one") {
		t.Fatal("a gating publication must stop an acceptance of the superseded edition from satisfying")
	}
}

// TestAScheduledEditionDoesNotGateUntilItsDayArrives pins the property nothing
// is allowed to break: Arrived is the database's answer to
// `effective_date <= CURRENT_DATE`, so the gate bites when the day moves and
// not when anything fires.
func TestAScheduledEditionDoesNotGateUntilItsDayArrives(t *testing.T) {
	scheduled := edition("two-scheduled", 2, 0, 30, 13, false)
	editions := []legal.Edition{edition("one", 1, 0, 1, 1, true), scheduled}

	floor, ok := legal.GatingFloor(editions)
	if !ok || floor.ID != "one" {
		t.Fatalf("GatingFloor = %+v, %v, want edition one while edition two is still scheduled", floor, ok)
	}
	set, _ := legal.Satisfying(editions)
	if !set.Contains("one") {
		t.Fatal("holders of edition one are clear until edition two's day arrives")
	}

	// The same rows, one midnight later. Nothing else changed.
	scheduled.Arrived = true
	editions[1] = scheduled
	floor, _ = legal.GatingFloor(editions)
	if floor.ID != "two-scheduled" {
		t.Fatalf("GatingFloor = %q after the scheduled day arrived, want two-scheduled", floor.ID)
	}
	set, _ = legal.Satisfying(editions)
	if set.Contains("one") {
		t.Fatal("holders of edition one are re-gated the moment the scheduled edition's day arrives")
	}
}

// TestOrderingIsEffectiveDateThenCreatedAt pins the tiebreak to the one every
// current-edition selector in this codebase uses, so the floor and the current
// edition cannot disagree about which of two same-day editions came second.
func TestOrderingIsEffectiveDateThenCreatedAt(t *testing.T) {
	editions := []legal.Edition{
		edition("first-inserted", 2, 0, 10, 1, true),
		edition("second-inserted", 3, 0, 10, 2, true),
	}
	floor, _ := legal.GatingFloor(editions)
	if floor.ID != "second-inserted" {
		t.Fatalf("GatingFloor = %q, want second-inserted — created_at breaks the tie", floor.ID)
	}
	set, _ := legal.Satisfying(editions)
	if set.Contains("first-inserted") {
		t.Fatal("the earlier-inserted same-day edition is below the floor")
	}
}

// TestNoGatingEditionInEffectHasNoSet: a lineage of corrections alone, or of
// nothing but future editions, yields no floor — and the caller must be told so
// rather than handed a set that clears or gates everybody by accident.
func TestNoGatingEditionInEffectHasNoSet(t *testing.T) {
	for name, editions := range map[string][]legal.Edition{
		"empty":              nil,
		"only scheduled":     {edition("two", 2, 0, 30, 13, false)},
		"only a correction":  {edition("one-corrected", 1, 1, 1, 1, true)},
		"scheduled + a corr": {edition("one-corrected", 1, 1, 1, 1, true), edition("two", 2, 0, 30, 13, false)},
	} {
		if _, ok := legal.GatingFloor(editions); ok {
			t.Errorf("%s: GatingFloor found a floor where no gating edition is in effect", name)
		}
		if set, ok := legal.Satisfying(editions); ok {
			t.Errorf("%s: Satisfying returned %v, want no set", name, set)
		}
	}
}

// TestNextLabelsAreAllocatedAndNeverTyped covers the generators an operator
// screen calls instead of offering a text box.
func TestNextLabelsAreAllocatedAndNeverTyped(t *testing.T) {
	editions := []legal.Edition{
		edition("zero", 0, 0, 1, 1, true),
		edition("one", 1, 0, 5, 5, true),
		edition("one-corrected", 1, 1, 6, 6, true),
		edition("two-scheduled", 2, 0, 30, 7, false),
	}

	if got := legal.NextGating(editions).Label(); got != "3" {
		t.Errorf("NextGating label = %q, want 3 — the generation after the highest PUBLISHED one, effective or not", got)
	}
	correction, err := legal.NextCorrection(editions, 1)
	if err != nil || correction.Label() != "1.2" {
		t.Errorf("NextCorrection(1) = %q, %v, want 1.2 — flat within the generation", correction.Label(), err)
	}
	correction, err = legal.NextCorrection(editions, 2)
	if err != nil || correction.Label() != "2.1" {
		t.Errorf("NextCorrection(2) = %q, %v, want 2.1", correction.Label(), err)
	}
	if _, err := legal.NextCorrection(editions, 9); err == nil {
		t.Error("NextCorrection against a generation that was never published must fail")
	}
	if got := legal.NextGating(nil).Label(); got != "0" {
		t.Errorf("NextGating on an empty lineage = %q, want 0", got)
	}
}

// TestSatisfyingDoesNotReorderTheCallersSlice: callers hold the rows they read,
// and an ordering is not a mutation they asked for.
func TestSatisfyingDoesNotReorderTheCallersSlice(t *testing.T) {
	editions := []legal.Edition{
		edition("one", 1, 0, 1, 1, true),
		edition("two", 2, 0, 10, 10, true),
	}
	legal.Satisfying(editions)
	legal.GatingFloor(editions)
	if editions[0].ID != "one" || editions[1].ID != "two" {
		t.Fatalf("the caller's slice was reordered: %q, %q", editions[0].ID, editions[1].ID)
	}
}
