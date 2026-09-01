package terms_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Terms' counterpart of the policy package's assembly tests. The content
// guards on the published text — the courtesy-translation notice, the section
// numbering, the Operator's identity — moved to internal/consent/legal with the
// text itself (migration 109).

func edition(locales []platform.Locale, drop string) []legal.Artifact {
	var artifacts []legal.Artifact
	for _, locale := range locales {
		for i, slug := range []string{terms.SlugAcceptanceLabel, terms.SlugTerms} {
			if slug == drop {
				continue
			}
			artifacts = append(artifacts, legal.Artifact{
				Locale:  locale,
				Slug:    slug,
				Ordinal: i + 1,
				Body:    string(locale) + " " + slug,
			})
		}
	}
	return artifacts
}

func TestAWholeArtifactSetBecomesAWholeDocument(t *testing.T) {
	t.Parallel()

	document, ok := terms.DocumentFrom(platform.LocaleEN, edition([]platform.Locale{platform.LocaleEN, platform.LocaleES}, ""))
	if !ok {
		t.Fatal("a complete artifact set did not assemble")
	}
	if document.Locale != platform.LocaleEN {
		t.Errorf("locale = %q, want en", document.Locale)
	}
	if document.AcceptanceLabel != "en "+terms.SlugAcceptanceLabel {
		t.Errorf("acceptance label carries %q", document.AcceptanceLabel)
	}
	if document.BodyMarkdown != "en "+terms.SlugTerms {
		t.Errorf("body carries %q", document.BodyMarkdown)
	}
}

// THE EDITION IN EFFECT TODAY, which carries no Adulthood Declaration Artifact,
// still assembles — in both languages, with the field empty (ADR 0069). This is
// the assertion that decides whether this binary can be deployed before an
// operator publishes anything: requiring the Artifact would refuse the current
// edition and take the public terms page, the sign-in gate and the staff
// interstitial down until somebody published.
func TestAnEditionWithoutTheAdulthoodDeclarationIsStillWhole(t *testing.T) {
	t.Parallel()

	artifacts := edition([]platform.Locale{platform.LocaleEN, platform.LocaleES}, "")
	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		document, ok := terms.DocumentFrom(locale, artifacts)
		if !ok {
			t.Fatalf("the %s edition without an adulthood declaration label did not assemble", locale)
		}
		if document.AdulthoodDeclarationLabel != "" {
			t.Errorf("the %s document invented an adulthood declaration label: %q", locale, document.AdulthoodDeclarationLabel)
		}
	}
	if documents := terms.Documents(artifacts); len(documents) != 2 {
		t.Errorf("assembled %d languages, want both", len(documents))
	}
}

// An edition that DOES carry it serves the wording, in both languages: the
// words a person was shown when they declared, which is the whole of what the
// declaration evidences.
func TestAnEditionCarryingTheAdulthoodDeclarationServesItsWording(t *testing.T) {
	t.Parallel()

	artifacts := edition([]platform.Locale{platform.LocaleEN, platform.LocaleES}, "")
	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		artifacts = append(artifacts, legal.Artifact{
			Locale:  locale,
			Slug:    terms.SlugAdulthoodDeclarationLabel,
			Ordinal: 3,
			Body:    string(locale) + " " + terms.SlugAdulthoodDeclarationLabel,
		})
	}

	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		document, ok := terms.DocumentFrom(locale, artifacts)
		if !ok {
			t.Fatalf("the %s edition carrying an adulthood declaration label did not assemble", locale)
		}
		if want := string(locale) + " " + terms.SlugAdulthoodDeclarationLabel; document.AdulthoodDeclarationLabel != want {
			t.Errorf("the %s adulthood declaration label carries %q, want %q", locale, document.AdulthoodDeclarationLabel, want)
		}
		if document.AcceptanceLabel != string(locale)+" "+terms.SlugAcceptanceLabel {
			t.Errorf("the %s acceptance label moved: %q", locale, document.AcceptanceLabel)
		}
	}
}

// The optional Artifact does not stand in for a required one. Completeness is
// every required slug and not a count of slugs found, so an edition carrying an
// adulthood declaration label INSTEAD of its acceptance label is refused as
// firmly as one carrying nothing in its place — §3 again.
func TestTheAdulthoodDeclarationDoesNotSubstituteForTheAcceptanceLabel(t *testing.T) {
	t.Parallel()

	artifacts := edition([]platform.Locale{platform.LocaleES}, terms.SlugAcceptanceLabel)
	artifacts = append(artifacts, legal.Artifact{
		Locale:  platform.LocaleES,
		Slug:    terms.SlugAdulthoodDeclarationLabel,
		Ordinal: 2,
		Body:    "es " + terms.SlugAdulthoodDeclarationLabel,
	})

	if _, ok := terms.DocumentFrom(platform.LocaleES, artifacts); ok {
		t.Error("an edition with no acceptance label assembled because another artifact made up the count")
	}
}

// A language this edition does not publish is refused rather than answered in
// another one: a contract the reader cannot read must never be presented as the
// one they accepted.
func TestAnUnpublishedLocaleHasNoDocument(t *testing.T) {
	t.Parallel()

	if _, ok := terms.DocumentFrom(platform.Locale("fr"), edition([]platform.Locale{platform.LocaleEN, platform.LocaleES}, "")); ok {
		t.Error("an unpublished locale returned a document")
	}
}

// A language whose acceptance label is missing is refused whole: §3 forbids a
// mandatory box with nothing written beside it.
func TestALanguageMissingItsAcceptanceLabelIsRefused(t *testing.T) {
	t.Parallel()

	artifacts := edition([]platform.Locale{platform.LocaleES}, terms.SlugAcceptanceLabel)
	if _, ok := terms.DocumentFrom(platform.LocaleES, artifacts); ok {
		t.Fatal("a language without its acceptance label assembled into a document")
	}
	if documents := terms.Documents(artifacts); len(documents) != 0 {
		t.Fatalf("Documents returned %d languages, want none", len(documents))
	}
}

// The Spanish text is the one that binds (§37), and the package owns that fact
// so no call site has to spell "es".
func TestTheSpanishTextPrevails(t *testing.T) {
	t.Parallel()

	if terms.PrevailingLocale != platform.LocaleES {
		t.Errorf("prevailing locale = %q, want es", terms.PrevailingLocale)
	}
}

// WHICH languages an edition publishes is the edition's own answer, read from
// its rows — an edition published without its translation serves one language.
func TestThePublishedLanguagesComeFromTheEditionsOwnRows(t *testing.T) {
	t.Parallel()

	documents := terms.Documents(edition([]platform.Locale{platform.LocaleES}, ""))
	if len(documents) != 1 {
		t.Fatalf("assembled %d languages, want 1", len(documents))
	}
	if _, ok := documents[platform.LocaleEN]; ok {
		t.Error("english assembled from an edition that does not publish it")
	}
}
