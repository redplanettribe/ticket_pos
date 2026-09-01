package legal_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// WHAT THESE PIN (#565, parent #556, ADR 0067).
//
// The four-state vocabulary and the one rule that reads a person's acceptance
// into it. Every assertion here is one of the collapses the vocabulary exists
// to refuse: Never seen folded into Outstanding, Former folded into
// Outstanding, and — the load-bearing one — a CORRECTION moving somebody into
// Outstanding.
//
// No database and no clock: this is arithmetic over an edition id and a set.

func TestNoAcceptanceAtAllIsNeverSeenAndNotOutstanding(t *testing.T) {
	// 511 of 1,569 Customers on the production copy are in this state — about a
	// third — because a box-office sale, a Sale Import or a pre-consent
	// checkout created their record and they have never acted on a Storefront
	// surface themselves. Folding them into Outstanding would bury the handful
	// who owe a re-acceptance under a third of the customer base, and the
	// default filter would be the one screen nobody could read.
	if got := legal.StandingOf("", legal.SatisfyingSet{"edition-2"}); got != legal.StandingNeverSeen {
		t.Fatalf("StandingOf(no acceptance) = %q, want %q", got, legal.StandingNeverSeen)
	}
}

func TestAnAcceptanceInTheSatisfyingSetIsCurrent(t *testing.T) {
	if got := legal.StandingOf("edition-2", legal.SatisfyingSet{"edition-2", "edition-2.1"}); got != legal.StandingCurrent {
		t.Fatalf("StandingOf = %q, want %q", got, legal.StandingCurrent)
	}
}

func TestAnAcceptanceBelowTheGatingFloorIsOutstanding(t *testing.T) {
	if got := legal.StandingOf("edition-1", legal.SatisfyingSet{"edition-2"}); got != legal.StandingOutstanding {
		t.Fatalf("StandingOf = %q, want %q", got, legal.StandingOutstanding)
	}
}

// THE PROPERTY THE WHOLE BROWSER RESTS ON: publishing a CORRECTION moves nobody
// into Outstanding.
//
// A correction takes the next revision within its generation and is published
// ABOVE the gating floor, so Satisfying grows without the floor moving —
// everybody who was Current stays Current, with no backfill, no notification
// and no column to maintain. The alternative, equality against "the current
// edition", would re-gate the entire population every time a typo was fixed.
//
// Asserted end to end through GatingFloor and Satisfying rather than a
// hand-written set, so that a change to either ordering rule fails here.
func TestPublishingACorrectionMovesNobodyIntoOutstanding(t *testing.T) {
	gatingOne := legal.Edition{ID: "e1", Lineage: legal.Lineage{Generation: 1}, EffectiveDate: day(1), CreatedAt: day(1), Arrived: true}
	gatingTwo := legal.Edition{ID: "e2", Lineage: legal.Lineage{Generation: 2}, EffectiveDate: day(10), CreatedAt: day(10), Arrived: true}

	before, ok := legal.Satisfying([]legal.Edition{gatingOne, gatingTwo})
	if !ok {
		t.Fatal("a published gating edition must produce a satisfying set")
	}

	// Somebody who accepted edition 2 is Current; somebody still on 1 is not.
	if got := legal.StandingOf("e2", before); got != legal.StandingCurrent {
		t.Fatalf("before the correction, e2 = %q, want %q", got, legal.StandingCurrent)
	}
	if got := legal.StandingOf("e1", before); got != legal.StandingOutstanding {
		t.Fatalf("before the correction, e1 = %q, want %q", got, legal.StandingOutstanding)
	}

	// Now publish 2.1, a correction to generation 2.
	correction := legal.Edition{ID: "e2.1", Lineage: legal.Lineage{Generation: 2, Revision: 1}, EffectiveDate: day(20), CreatedAt: day(20), Arrived: true}
	after, ok := legal.Satisfying([]legal.Edition{gatingOne, gatingTwo, correction})
	if !ok {
		t.Fatal("a correction must not destroy the satisfying set")
	}

	floor, ok := legal.GatingFloor([]legal.Edition{gatingOne, gatingTwo, correction})
	if !ok || floor.ID != gatingTwo.ID {
		t.Fatalf("the correction moved the gating floor to %+v; a correction re-gates nobody", floor)
	}
	if got := legal.StandingOf("e2", after); got != legal.StandingCurrent {
		t.Fatalf("after the correction, e2 = %q, want %q — a correction must move nobody into outstanding", got, legal.StandingCurrent)
	}
	if got := legal.StandingOf("e2.1", after); got != legal.StandingCurrent {
		t.Fatalf("somebody shown the correction and accepting it must be current, got %q", got)
	}
	// And the person genuinely behind the floor is unchanged: a correction
	// clears nobody either.
	if got := legal.StandingOf("e1", after); got != legal.StandingOutstanding {
		t.Fatalf("after the correction, e1 = %q, want %q", got, legal.StandingOutstanding)
	}
}

func TestParseStandingAdmitsTheFourAndNothingElse(t *testing.T) {
	for _, raw := range []string{"current", "outstanding", "never_seen", "former"} {
		if _, ok := legal.ParseStanding(raw); !ok {
			t.Fatalf("ParseStanding(%q) was refused; it is one of the four", raw)
		}
	}
	// "withdrawn" is the one that must NOT be here: withdrawal is a state of an
	// optional consent and of NEITHER gate, and admitting it would let a
	// withdrawn marketing consent be read as an unaccepted document.
	for _, raw := range []string{"", "withdrawn", "Current", "never seen", "expired", "accepted"} {
		if standing, ok := legal.ParseStanding(raw); ok {
			t.Fatalf("ParseStanding(%q) = %q, want refused", raw, standing)
		}
	}
}

// The empty string is refused rather than defaulted, so that "no filter given"
// is a decision the surface makes (both browsers default to Outstanding) rather
// than one this function makes silently on its behalf — a filter widened here
// to everybody would answer "who owes an acceptance?" with the whole customer
// base, which on a screen with no total looks exactly like a re-gate.
func TestParseStandingDoesNotDefaultTheEmptyString(t *testing.T) {
	if _, ok := legal.ParseStanding(""); ok {
		t.Fatal("ParseStanding of the empty string must be refused, not defaulted")
	}
}
