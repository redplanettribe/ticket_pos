package legal

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Lineage is where a published edition came from: which generation it belongs
// to, and which revision of that generation it is (#560).
//
// It is the whole of what a label says. A gating edition — one that re-gates
// the customer base and the staff platform — takes the next generation at
// revision 0. A correction takes the next revision within its generation, FLAT:
// a correction to a correction is 1.2, never 1.1.1, because the question a
// label answers is "what does this descend from", and nesting answers a
// question nobody asks.
type Lineage struct {
	// Generation is what this edition descends from. 0 is a real generation
	// (the Privacy Policy's `0-placeholder`), not "unset".
	Generation int
	// Revision is 0 for a gating edition and 1, 2, 3 … for a correction.
	Revision int
}

// Gating reports whether publishing this edition re-gated everybody.
//
// THIS IS THE WHOLE OF "GATING", and there is deliberately nothing else to
// consult: no is_gating column, no gating_from column, no published_as column
// (#553 removed promotion, which is what those would have expressed). The fact
// is fixed when the edition is published and no later act can change it, which
// is what allows the satisfying set to be computed from the version rows alone.
func (l Lineage) Gating() bool { return l.Revision == 0 }

// Label renders the lineage as the edition's name: "2" for a gating edition,
// "1.1" for a correction.
//
// THIS IS THE ONLY PLACE A LABEL IS EVER PRODUCED. Labels are system-generated
// and never typed, so two editions cannot be given the same name and no label
// can lie about its descent — migration 110 makes the database refuse anything
// this function would not have written.
//
// It carries lineage ONLY. Reading "1.1" tells you what it descends from and
// nothing about whether the edition still counts or about what its publication
// did; those are answered elsewhere, so that no screen infers one from another
// by parsing this string.
func (l Lineage) Label() string {
	if l.Revision == 0 {
		return strconv.Itoa(l.Generation)
	}
	return strconv.Itoa(l.Generation) + "." + strconv.Itoa(l.Revision)
}

// ParseLabel reads a rendered label back. It exists for the backfilled rows and
// for tests; nothing in the publishing path parses a label, because the two
// integers are stored beside it.
//
// Reports false for anything Label would not have written — including the
// Privacy Policy's grandfathered "0-placeholder", whose lineage is read from
// its own columns.
func ParseLabel(label string) (Lineage, bool) {
	generation, revision, found := strings.Cut(label, ".")
	g, err := strconv.Atoi(generation)
	if err != nil || g < 0 || generation != strconv.Itoa(g) {
		return Lineage{}, false
	}
	if !found {
		return Lineage{Generation: g}, true
	}
	r, err := strconv.Atoi(revision)
	if err != nil || r < 1 || revision != strconv.Itoa(r) {
		return Lineage{}, false
	}
	return Lineage{Generation: g, Revision: r}, true
}

// Edition is one published edition of one legal document, reduced to what
// deciding "who owes an acceptance" needs: which row it is, where it came from,
// and whether its day has arrived.
//
// It carries no text and no fingerprint. The satisfying set is a question about
// version rows only, and a type that could not hold an artifact is a type no
// caller can accidentally render from.
type Edition struct {
	// ID is the version row's id — the value an acceptance stores.
	ID string
	// Lineage is the edition's generation and revision.
	Lineage Lineage
	// EffectiveDate is the day the edition takes effect: a legal fact stated on
	// the document itself, and the primary sort key.
	EffectiveDate time.Time
	// CreatedAt breaks ties between two editions effective the same day, in the
	// order they were inserted — the tiebreak every current-edition selector in
	// this codebase already uses.
	CreatedAt time.Time
	// Arrived is `effective_date <= CURRENT_DATE`, ANSWERED BY THE DATABASE and
	// never recomputed here.
	//
	// That is load-bearing. The gate bites because the database's own day moves
	// on its own: an edition scheduled for next Tuesday becomes current at
	// midnight with no job firing, nothing notified and no cache invalidated by
	// anything but its own age. Comparing an effective date against a Go clock
	// here would reintroduce the publication hook that the whole design is
	// arranged to avoid.
	Arrived bool
}

// SatisfyingSet is the set of edition ids that clear the gate: an acceptance
// naming any one of them is an acceptance, and an acceptance naming anything
// else is not (#560).
//
// It replaces equality against "the current edition". Two people holding two
// different editions can both be clear, which is the design: each person's
// evidence still resolves to the exact bytes they accepted, and the platform
// still knows nobody is behind the gating floor.
type SatisfyingSet []string

// Contains reports whether an acceptance of this edition id clears the gate.
func (s SatisfyingSet) Contains(editionID string) bool {
	return slices.Contains(s, editionID)
}

// IDs returns the ids for a query parameter (`= ANY($1)`, `<> ALL($1)`).
func (s SatisfyingSet) IDs() []string { return s }

// GatingFloor is the newest GATING edition whose day has arrived: the oldest
// edition anybody is allowed to be standing on.
//
// "Newest" is the ordering every current-edition selector in this codebase
// already uses — effective_date DESC, created_at DESC — so that the floor and
// the current edition move together and cannot disagree about which of two
// editions effective the same day came second.
//
// Corrections are skipped, and that is the entire mechanism by which a
// correction re-gates nobody FOR FREE: publishing one does not move the floor,
// so everybody who was clear stays clear, with no backfill, no notification and
// no column to maintain.
//
// Reports false when no gating edition has arrived — a state migrations 060 and
// 105 make unreachable, and which callers report as "no current edition".
func GatingFloor(editions []Edition) (Edition, bool) {
	ordered := newestFirst(editions)
	for _, edition := range ordered {
		if edition.Arrived && edition.Lineage.Gating() {
			return edition, true
		}
	}
	return Edition{}, false
}

// Satisfying computes the satisfying set: the gating floor and EVERY edition at
// or above it in the same ordering, corrections included.
//
// Corrections above the floor are in the set because a person shown a
// correction accepted the current text; refusing their acceptance would re-gate
// exactly the people who answered most recently. Future-dated editions above
// the floor are in it for the same reason a checkout's held answer is honoured:
// if somebody was shown an edition and accepted it, that acceptance stands, and
// the alternative is a person asked twice for one publication.
//
// Everything BELOW the floor is out: a superseded edition is what the gating
// publication superseded, and nobody is grandfathered.
//
// Reports false when there is no gating floor, in which case there is no
// meaningful set and the caller reports "no current edition" rather than
// clearing or gating everybody by accident.
func Satisfying(editions []Edition) (SatisfyingSet, bool) {
	ordered := newestFirst(editions)
	for i, edition := range ordered {
		if edition.Arrived && edition.Lineage.Gating() {
			set := make(SatisfyingSet, 0, i+1)
			for _, above := range ordered[:i+1] {
				set = append(set, above.ID)
			}
			return set, true
		}
	}
	return nil, false
}

// NextGating is the lineage a gating publication takes: the next generation, at
// revision 0. Nothing is typed and nothing is chosen — the generation after the
// highest one published, whether or not that one has taken effect yet.
func NextGating(editions []Edition) Lineage {
	highest := -1
	for _, edition := range editions {
		highest = max(highest, edition.Lineage.Generation)
	}
	return Lineage{Generation: highest + 1}
}

// NextCorrection is the lineage a correction to a generation takes: the next
// revision within that generation, flat. Reports an error when the generation
// has never been published, because a correction descends from something.
func NextCorrection(editions []Edition, generation int) (Lineage, error) {
	highest := -1
	for _, edition := range editions {
		if edition.Lineage.Generation == generation {
			highest = max(highest, edition.Lineage.Revision)
		}
	}
	if highest < 0 {
		return Lineage{}, fmt.Errorf("next correction: generation %d has never been published", generation)
	}
	return Lineage{Generation: generation, Revision: highest + 1}, nil
}

// newestFirst copies and orders editions the way every current-edition selector
// orders them: effective_date DESC, created_at DESC. A copy, because callers
// hold the slice they read and an ordering is not a mutation they asked for.
func newestFirst(editions []Edition) []Edition {
	ordered := slices.Clone(editions)
	slices.SortStableFunc(ordered, func(a, b Edition) int {
		if c := b.EffectiveDate.Compare(a.EffectiveDate); c != 0 {
			return c
		}
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return ordered
}
