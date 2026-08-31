package legal_test

import (
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The rules a publication is refused by (#563), tested where they live.
//
// Every one of them decides whether a legal document may be published, and the
// screen draws the same conclusions from apps/staff/lib/legal-drafts.ts. Two
// implementations of "is this a structural change" that disagreed would show an
// operator a live correction link that the seam then refused — so both are
// tested directly, and neither is verified by rendering a component.

var bothLocales = []platform.Locale{platform.LocaleEN, platform.LocaleES}

// artifact is the shorthand these tests are written in.
func artifact(locale platform.Locale, slug string, ordinal int, body string) legal.Artifact {
	return legal.Artifact{Locale: locale, Slug: slug, Ordinal: ordinal, Body: body}
}

// edition2 is a two-artifact document in both languages: the shape both real
// documents have.
func edition2(policyBody string) []legal.Artifact {
	return []legal.Artifact{
		artifact(platform.LocaleEN, "label-policy-acceptance", 1, "I accept."),
		artifact(platform.LocaleEN, "policy", 2, policyBody),
		artifact(platform.LocaleES, "label-policy-acceptance", 1, "Acepto."),
		artifact(platform.LocaleES, "policy", 2, "El texto."),
	}
}

func TestChangeOfCollapsesAbsentAndEmpty(t *testing.T) {
	// "Typed and then cleared" and "never typed" are the same fact about a
	// document, and the draft table stores both as no row. A rule that told them
	// apart would refuse one publication and allow the identical other.
	cases := []struct {
		name          string
		before, after string
		want          legal.CellChange
	}{
		{"nothing either side", "", "   ", legal.CellMissing},
		{"new artifact", "", "words", legal.CellAdded},
		{"artifact taken out", "words", "", legal.CellRemoved},
		{"artifact emptied is still taken out", "words", "  \n ", legal.CellRemoved},
		{"same words", "words", "words", legal.CellUnchanged},
		{"same words, different padding", " words ", "words", legal.CellUnchanged},
		{"rewritten", "words", "other words", legal.CellModified},
	}
	for _, tc := range cases {
		if got := legal.ChangeOf(tc.before, tc.after); got != tc.want {
			t.Errorf("%s: ChangeOf(%q, %q) = %q; want %q", tc.name, tc.before, tc.after, got, tc.want)
		}
	}
}

func TestCompletenessCountsHolesInPublishedLanguagesOnly(t *testing.T) {
	draft := []legal.Artifact{
		artifact(platform.LocaleES, "label-policy-acceptance", 1, "Acepto."),
		artifact(platform.LocaleES, "policy", 2, "El texto."),
		// The English policy is written; the English label is not.
		artifact(platform.LocaleEN, "policy", 2, "The text."),
	}

	// Publishing both languages: the missing English label is a hole.
	gaps := legal.Completeness(draft, bothLocales)
	if len(gaps) != 1 || gaps[0].Slug != "label-policy-acceptance" || gaps[0].Locale != platform.LocaleEN {
		t.Fatalf("gaps = %+v; want the unwritten English label", gaps)
	}

	// Publishing Spanish alone: the same draft is complete. A language the draft
	// has dropped will not ship, so nothing in it can be a hole.
	if gaps := legal.Completeness(draft, []platform.Locale{platform.LocaleES}); len(gaps) != 0 {
		t.Fatalf("gaps = %+v; want none when only Spanish is published", gaps)
	}
}

func TestIsStructuralSeesADeletionAndNotOnlyAnAddition(t *testing.T) {
	published := edition2("The text.")

	reworded := edition2("The text, restated.")
	if legal.IsStructural(published, reworded, bothLocales) {
		t.Errorf("rewording an artifact reads as structural")
	}

	added := append(edition2("The text."),
		artifact(platform.LocaleEN, "label-analytics-consent", 3, "Analytics."),
		artifact(platform.LocaleES, "label-analytics-consent", 3, "Analítica."),
	)
	if !legal.IsStructural(published, added, bothLocales) {
		t.Errorf("adding an artifact does not read as structural")
	}

	// THE DELETION IS THE ONE THAT GETS MISSED. An artifact the draft removed is
	// by definition not among the draft's slugs, so a rule walking the draft
	// alone would report it as an ordinary rewording — and it would be publishable
	// as a correction, which is exactly what must not happen.
	removed := []legal.Artifact{
		artifact(platform.LocaleEN, "policy", 1, "The text."),
		artifact(platform.LocaleES, "policy", 1, "El texto."),
	}
	if !legal.IsStructural(published, removed, bothLocales) {
		t.Errorf("removing an artifact does not read as structural")
	}
}

func TestLocaleSetChangedComparesThePublishedSetAgainstTheEditionsOwnRows(t *testing.T) {
	published := edition2("The text.")

	if legal.LocaleSetChanged(published, bothLocales) {
		t.Errorf("publishing the same two languages reads as a change")
	}
	if !legal.LocaleSetChanged(published, []platform.Locale{platform.LocaleES}) {
		t.Errorf("dropping English does not read as a locale-set change")
	}
	// Order and duplication are not the set.
	if legal.LocaleSetChanged(published, []platform.Locale{platform.LocaleES, platform.LocaleEN, platform.LocaleES}) {
		t.Errorf("the same set, restated, reads as a change")
	}
}

func TestIsEmptyDiffPermitsARepublicationAndRefusesNothing(t *testing.T) {
	published := edition2("The text.")

	if !legal.IsEmptyDiff(published, edition2("The text."), bothLocales) {
		t.Errorf("a draft identical to the published edition does not read as an empty diff")
	}
	if legal.IsEmptyDiff(published, edition2("The text, restated."), bothLocales) {
		t.Errorf("a reworded draft reads as an empty diff")
	}
	// A hole is not a change: an incomplete draft is refused by Completeness with
	// its own reason, and must not be smuggled past the correction rule as
	// "something moved".
	holed := []legal.Artifact{
		artifact(platform.LocaleEN, "label-policy-acceptance", 1, "I accept."),
		artifact(platform.LocaleEN, "policy", 2, "The text."),
		artifact(platform.LocaleES, "label-policy-acceptance", 1, "Acepto."),
	}
	if legal.IsEmptyDiff(published, holed, bothLocales) {
		t.Errorf("a draft with a removed Spanish policy reads as an empty diff")
	}
}

func TestPublishedInDropsUnpublishedLanguagesAndBlankCells(t *testing.T) {
	draft := append(edition2("The text."),
		artifact(platform.LocaleEN, "note", 3, "   "),
	)

	rows := legal.PublishedIn(draft, []platform.Locale{platform.LocaleES})
	if len(rows) != 2 {
		t.Fatalf("published rows = %+v; want the two Spanish cells only", rows)
	}
	for _, row := range rows {
		if row.Locale != platform.LocaleES {
			t.Fatalf("row in %q survived a Spanish-only publication", row.Locale)
		}
	}

	// A blank cell never becomes a row: migration 109's CHECK refuses an empty
	// body, and bytes nobody can be shown must not be inside a fingerprint.
	all := legal.PublishedIn(draft, bothLocales)
	for _, row := range all {
		if strings.TrimSpace(row.Body) == "" {
			t.Fatalf("a blank cell was published: %+v", row)
		}
	}
	if len(all) != 4 {
		t.Fatalf("published rows = %d; want the four written cells", len(all))
	}

	// The ordinals are the draft's own, carried through: they are the preimage's
	// order, and renumbering here would publish an edition hashed differently
	// from the one that was previewed.
	if all[0].Ordinal != 1 || all[1].Ordinal != 2 {
		t.Fatalf("ordinals were rewritten: %+v", all)
	}
}

func TestDiffSummaryCountsCells(t *testing.T) {
	published := edition2("The text.")
	draft := append(edition2("The text, restated."),
		artifact(platform.LocaleEN, "label-analytics-consent", 3, "Analytics."),
	)

	summary := legal.DiffSummary(published, draft, bothLocales)
	// One English policy body reworded, one English artifact added, its Spanish
	// half missing, and the three untouched cells unchanged.
	want := "cells: 1 modified, 1 added, 0 removed, 3 unchanged; locales: en,es"
	if summary != want {
		t.Fatalf("summary = %q; want %q", summary, want)
	}
}

func TestParsePublishKindHasNoDefault(t *testing.T) {
	if kind, ok := legal.ParsePublishKind("edition"); !ok || kind != legal.PublishGating {
		t.Errorf("edition = %q, %v", kind, ok)
	}
	if kind, ok := legal.ParsePublishKind(" correction "); !ok || kind != legal.PublishCorrection {
		t.Errorf("correction = %q, %v", kind, ok)
	}
	for _, raw := range []string{"", "gating", "gATING", "publish", "gating edition"} {
		if _, ok := legal.ParsePublishKind(raw); ok {
			t.Errorf("ParsePublishKind(%q) resolved; which act this is must never be a default", raw)
		}
	}
}
