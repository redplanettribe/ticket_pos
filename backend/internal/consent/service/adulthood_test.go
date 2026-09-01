package service

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// WHETHER THE ADULTHOOD DECLARATION BOX IS DRAWN IS A FACT ABOUT THE EDITION
// (#586, ADR 0069), and this is where that fact is read. Everything downstream
// of it — Outstanding, the boxes payload, the refusal — only transcribes the
// answer, so this is the one place a wrong answer could be manufactured.
//
// It is pinned on a Service with no repository, legaledition_test.go's rule: the
// question is answered from the edition that was SERVED and never from a second
// read, and a path that went to the database would dereference a nil repository
// and take the test with it rather than returning something plausible.

// withAdulthoodLabel returns the read with the Artifact added in the given
// languages — which is exactly what an operator publishing a Gating Edition
// does, and the whole of introducing the box.
func withAdulthoodLabel(read repository.TermsEdition, locales ...platform.Locale) repository.TermsEdition {
	for i, locale := range locales {
		read.Artifacts = append(read.Artifacts, legal.Artifact{
			Locale:  locale,
			Slug:    terms.SlugAdulthoodDeclarationLabel,
			Ordinal: 3 + i,
			Body:    "I declare that I am eighteen years of age or older (" + string(locale) + ")",
		})
	}
	read.Version.ContentHash = legal.ContentHash(read.Artifacts)
	return read
}

// TestAnEditionWithoutTheArtifactAsksNothing is the state every environment is
// in today, and the one the binary had to tolerate: the Artifact is OPTIONAL to
// this code, so the current edition serves, gates and renders exactly as it did
// before this feature existed.
func TestAnEditionWithoutTheArtifactAsksNothing(t *testing.T) {
	t.Parallel()

	s := serviceWithWarmCaches(t,
		policyEditionRead("policy-edition-1", "1", "edition one"),
		termsEditionRead("terms-edition-1", "1", "edition one"))

	edition, err := s.currentTermsEdition(t.Context())
	if err != nil {
		t.Fatalf("current terms edition: %v", err)
	}
	if edition.asksAdulthoodDeclaration() {
		t.Fatal("an edition carrying no label-adulthood-declaration artifact must ask for no declaration")
	}
}

// TestAnEditionCarryingTheArtifactAsks is the other half, and the whole of
// introducing the box: a publish, with no deploy between the two states.
func TestAnEditionCarryingTheArtifactAsks(t *testing.T) {
	t.Parallel()

	s := serviceWithWarmCaches(t,
		policyEditionRead("policy-edition-1", "1", "edition one"),
		withAdulthoodLabel(termsEditionRead("terms-edition-2", "2", "edition two"),
			platform.LocaleES, platform.LocaleEN))

	edition, err := s.currentTermsEdition(t.Context())
	if err != nil {
		t.Fatalf("current terms edition: %v", err)
	}
	if !edition.asksAdulthoodDeclaration() {
		t.Fatal("an edition publishing label-adulthood-declaration must ask for the declaration")
	}
	// And the words reach the reader in both languages, unreworded: the box's
	// label is evidence, so it is served from the artifact and never from a
	// catalog.
	for locale, want := range map[string]string{
		"es": "I declare that I am eighteen years of age or older (es)",
		"en": "I declare that I am eighteen years of age or older (en)",
	} {
		view, err := s.CurrentTerms(t.Context(), locale)
		if err != nil {
			t.Fatalf("current terms in %s: %v", locale, err)
		}
		if view.AdulthoodDeclarationLabel != want {
			t.Fatalf("%s label = %q, want %q", locale, view.AdulthoodDeclarationLabel, want)
		}
	}
}

// TestTheDeclarationIsOwedByTheOperativeTextAndNotByTheTranslation pins the
// choice asksAdulthoodDeclaration makes: the question is answered from the
// prevailing Locale, so the gate cannot vary by which language a reader is
// looking at. An edition that carried the Artifact in the translation alone
// would ask nobody, and one that carried it in Spanish alone asks everybody —
// which is the loud failure, not the quiet one.
func TestTheDeclarationIsOwedByTheOperativeTextAndNotByTheTranslation(t *testing.T) {
	t.Parallel()

	translationOnly := serviceWithWarmCaches(t,
		policyEditionRead("policy-edition-1", "1", "edition one"),
		withAdulthoodLabel(termsEditionRead("terms-edition-2", "2", "edition two"), platform.LocaleEN))
	edition, err := translationOnly.currentTermsEdition(t.Context())
	if err != nil {
		t.Fatalf("current terms edition: %v", err)
	}
	if edition.asksAdulthoodDeclaration() {
		t.Fatal("the courtesy translation must not decide what the contract asks for")
	}

	prevailingOnly := serviceWithWarmCaches(t,
		policyEditionRead("policy-edition-1", "1", "edition one"),
		withAdulthoodLabel(termsEditionRead("terms-edition-2", "2", "edition two"), platform.LocaleES))
	edition, err = prevailingOnly.currentTermsEdition(t.Context())
	if err != nil {
		t.Fatalf("current terms edition: %v", err)
	}
	if !edition.asksAdulthoodDeclaration() {
		t.Fatal("the prevailing text carrying the artifact must ask for the declaration")
	}
}
