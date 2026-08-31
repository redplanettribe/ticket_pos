package legal

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// What a publication is allowed to be (#563, parent #556, ADR 0067).
//
// Publishing is the one act in the Legal Center that a reader can see. Every
// rule in this file is a rule about the DRAFT AGAINST THE PUBLISHED EDITION —
// pure, over two artifact sets and one language set — so that the refusals an
// operator meets at the button are the same refusals the HTTP seam enforces and
// the same ones apps/staff/lib/legal-drafts.ts draws on screen. Three
// implementations of "is this a structural change" would be three answers.
//
// THE RULES ARE HERE AND NOT IN THE SERVICE because they are about documents,
// not about requests: they need no database, no clock and no session, and a
// rule that decides whether a legal document may be published is a rule a unit
// test should be able to walk in full.

// PublishKind is what an operator asked for: a new edition, or a correction.
//
// TWO VALUES AND NO THIRD. #553 removed promotion — there is no "publish as a
// correction and decide later" — so the fact is fixed at publication and is
// spelled, everywhere afterwards, as Lineage.Revision == 0.
type PublishKind string

const (
	// PublishGating is a new edition: the next generation, revision 0. It moves
	// the gating floor the day it takes effect and re-gates everybody standing
	// below it.
	PublishGating PublishKind = "edition"
	// PublishCorrection is a correction: the next revision within the current
	// generation, flat. It re-gates NOBODY, for free, because GatingFloor skips
	// it — there is no column to set and no backfill to run.
	PublishCorrection PublishKind = "correction"
)

// ParsePublishKind reads the kind off the wire. Reports false for anything else,
// including the empty string: which of these two acts is being performed is
// never a default.
func ParsePublishKind(raw string) (PublishKind, bool) {
	switch PublishKind(strings.TrimSpace(raw)) {
	case PublishGating:
		return PublishGating, true
	case PublishCorrection:
		return PublishCorrection, true
	}
	return "", false
}

// CellRef names one cell: one artifact in one language. The unit of the preview
// rule (#562), of the diff, and of every rule below — because a change to the
// Spanish policy and a change to an English checkbox label are two facts about
// two readers, and a rule stated per-artifact would let one hide behind the
// other.
type CellRef struct {
	Slug   string          `json:"slug"`
	Locale platform.Locale `json:"locale"`
}

// CellChange is what happened to one cell between the published edition and the
// draft. It mirrors apps/staff/lib/legal-drafts.ts's CellStatus value for value,
// deliberately: the screen and the seam must agree about what a cell is doing.
type CellChange string

const (
	CellUnchanged CellChange = "unchanged"
	CellModified  CellChange = "modified"
	// CellAdded and CellRemoved are STRUCTURAL: the artifact set itself moved.
	CellAdded   CellChange = "added"
	CellRemoved CellChange = "removed"
	// CellMissing is not a change at all — it is the ABSENCE of text where the
	// draft says there should be some. It is what Completeness counts.
	CellMissing CellChange = "missing"
)

// ChangeOf reports what happened to one cell, from the published bytes and the
// draft bytes. Total: "" on either side is "nothing written there", which is how
// both the draft table (no row) and an emptied textarea spell it.
func ChangeOf(before, after string) CellChange {
	before, after = strings.TrimSpace(before), strings.TrimSpace(after)
	switch {
	case before == "" && after == "":
		return CellMissing
	case before == "":
		return CellAdded
	case after == "":
		// The published edition has this text and the draft does not. Whether
		// the operator deleted it or the whole artifact went, the published
		// document loses an artifact, which is structural.
		return CellRemoved
	case before == after:
		return CellUnchanged
	default:
		return CellModified
	}
}

// Changes is every cell either side names, in each of the languages the draft
// intends to publish, with what happened to it.
//
// THE LANGUAGES ARE AN ARGUMENT and never the union of what is written. A draft
// carries an EXPLICIT published-language set (#561) precisely so a
// half-translated language is a draft that cannot publish rather than one
// quietly dropped by an empty textarea, and a rule that inferred the set from
// the cells would undo that at the last step.
//
// Ordered: published slugs in their published order first, then whatever the
// draft added, and within a slug the languages ascending — the preimage's own
// ordering, so two reads of one draft list the cells in the same places.
func Changes(published, draft []Artifact, locales []platform.Locale) map[CellRef]CellChange {
	before := bodies(published)
	after := bodies(draft)

	changes := make(map[CellRef]CellChange, len(before)+len(after))
	for _, slug := range unionSlugs(published, draft) {
		for _, locale := range sortedLocales(locales) {
			ref := CellRef{Slug: slug, Locale: locale}
			changes[ref] = ChangeOf(before[ref], after[ref])
		}
	}
	return changes
}

// Completeness is every hole in the draft: an artifact the draft carries with
// nothing written in a language the draft intends to publish.
//
// THE DRAFT'S OWN SLUGS, not the published edition's. An artifact the draft
// removed is not a hole — it is a structural change, which is a different
// refusal with a different reason — and an artifact the draft invented is a hole
// in every language it is not written in.
//
// A publication is refused while this is non-empty, so no reader ever meets a
// document with a gap in it: a mandatory checkbox with a blank label beside it,
// or a Short Notice that renders as nothing above the consent boxes.
func Completeness(draft []Artifact, locales []platform.Locale) []CellRef {
	written := bodies(draft)
	gaps := make([]CellRef, 0)
	for _, slug := range slugsInOrder(draft) {
		for _, locale := range sortedLocales(locales) {
			ref := CellRef{Slug: slug, Locale: locale}
			if strings.TrimSpace(written[ref]) == "" {
				gaps = append(gaps, ref)
			}
		}
	}
	return gaps
}

// IsStructural reports whether the draft changes the artifact SET rather than
// only its wording.
//
// This is the one structural change the code can PROVE is not a typo, and it is
// what refuses the correction path. A correction says "the words were wrong"; an
// edition that gained or lost an artifact is not the same document with better
// words — somebody is now being asked for a consent they were not asked for, or
// has stopped being asked for one, and that is a re-gating fact however small
// the text is.
//
// OVER THE UNION OF BOTH SIDES. Walking the draft alone would report a DELETION
// as an ordinary rewording, because an artifact the draft removed is by
// definition not among the draft's slugs — the exact bug apps/staff's
// isStructuralChange notes and corrects.
func IsStructural(published, draft []Artifact, locales []platform.Locale) bool {
	for _, change := range Changes(published, draft, locales) {
		if change == CellAdded || change == CellRemoved {
			return true
		}
	}
	return false
}

// LocaleSetChanged reports whether the draft publishes a different set of
// languages from the edition it is replacing.
//
// REFUSED AS A CORRECTION, and the reason is about the fingerprint rather than
// about the words: the locale set IS part of the preimage (ContentHash frames
// each language's code before its artifacts), so adding or dropping a language
// RESHAPES what is hashed rather than merely changing what is hashed. A
// reshaped preimage cannot be passed off as a fingerprint touch-up — and on the
// reader's side, dropping a language means somebody who read the document in it
// can no longer read what they accepted.
func LocaleSetChanged(published []Artifact, draftLocales []platform.Locale) bool {
	return !slices.Equal(sortedLocales(Locales(published)), sortedLocales(draftLocales))
}

// IsEmptyDiff reports that the draft says exactly what is published, in every
// language it publishes.
//
// A CORRECTION ON AN EMPTY DIFF IS REFUSED — a correction that corrects nothing
// cannot be recorded, and the typed reason would be a sentence about an act that
// did not happen. A GATING EDITION ON AN EMPTY DIFF IS ALLOWED, deliberately:
// re-gating over unchanged text is a real thing an operator may need (a
// correction's words republished so they finally gate, ADR 0067 / migration
// 110's note on content_hash), and the edition is honest about being a
// republication because its fingerprint is the same bytes' fingerprint.
func IsEmptyDiff(published, draft []Artifact, locales []platform.Locale) bool {
	for _, change := range Changes(published, draft, locales) {
		if change != CellUnchanged && change != CellMissing {
			return false
		}
	}
	return true
}

// DiffSummary is what a publication changed, counted in cells: the line stored
// on the version row as `publish_diff_summary`.
//
// A SUMMARY AND NOT THE DIFF. The diff itself is recomputable exactly, forever,
// from the two editions' own artifact rows, so storing it again would be a
// second copy that can disagree with the bytes it describes. What is not
// recomputable is the ASSERTION — that the publication was understood to be this
// size when it was made.
//
// Deliberately mechanical rather than prose: it is read by an auditor and by
// grep, never rendered to a customer, so it is neither translated nor phrased.
func DiffSummary(published, draft []Artifact, locales []platform.Locale) string {
	counts := map[CellChange]int{}
	for _, change := range Changes(published, draft, locales) {
		counts[change]++
	}
	tokens := make([]string, 0, len(locales))
	for _, locale := range sortedLocales(locales) {
		tokens = append(tokens, string(locale))
	}
	return fmt.Sprintf("cells: %d modified, %d added, %d removed, %d unchanged; locales: %s",
		counts[CellModified], counts[CellAdded], counts[CellRemoved], counts[CellUnchanged],
		strings.Join(tokens, ","))
}

// PublishedIn reduces a draft to the rows an edition would actually carry: the
// cells that have text, in the languages the draft intends to publish.
//
// PUBLISHING IS A ROW COPY, and this is the copy. A cell written in a language
// the draft has dropped is work in progress that will not ship; carrying it into
// the version row would put bytes inside the fingerprint that no reader can be
// shown, which is the one thing the preimage rule may not do.
//
// The ordinals are the draft's own, carried through untouched: they are the
// preimage's order (#541), and renumbering them here would publish an edition
// hashed differently from the one that was previewed and diffed.
func PublishedIn(draft []Artifact, locales []platform.Locale) []Artifact {
	rows := make([]Artifact, 0, len(draft))
	for _, artifact := range draft {
		if !slices.Contains(locales, artifact.Locale) {
			continue
		}
		if strings.TrimSpace(artifact.Body) == "" {
			continue
		}
		rows = append(rows, artifact)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Locale != rows[j].Locale {
			return rows[i].Locale < rows[j].Locale
		}
		return rows[i].Ordinal < rows[j].Ordinal
	})
	return rows
}

// bodies indexes artifacts by cell, so the rules above can ask "what is written
// here" without a nested walk per question.
func bodies(artifacts []Artifact) map[CellRef]string {
	index := make(map[CellRef]string, len(artifacts))
	for _, artifact := range artifacts {
		index[CellRef{Slug: artifact.Slug, Locale: artifact.Locale}] = artifact.Body
	}
	return index
}

// slugsInOrder is a set's slugs, deduplicated, in the order the rows arrive —
// which is ordinal order, which is the preimage's order.
func slugsInOrder(artifacts []Artifact) []string {
	ordered := slices.Clone(artifacts)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	slugs := make([]string, 0, len(ordered))
	for _, artifact := range ordered {
		if !slices.Contains(slugs, artifact.Slug) {
			slugs = append(slugs, artifact.Slug)
		}
	}
	return slugs
}

// unionSlugs is every slug either side names: the published order first, then
// whatever the draft added. See IsStructural for why the union and not the
// draft.
func unionSlugs(published, draft []Artifact) []string {
	slugs := slugsInOrder(published)
	for _, slug := range slugsInOrder(draft) {
		if !slices.Contains(slugs, slug) {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}

func sortedLocales(locales []platform.Locale) []platform.Locale {
	ordered := slices.Clone(locales)
	slices.Sort(ordered)
	return slices.Compact(ordered)
}
