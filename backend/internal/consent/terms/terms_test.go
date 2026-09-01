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
