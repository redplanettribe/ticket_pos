package policy_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/policy"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// This package used to hold the policy text and the tests that read it. The
// text is rows now (migration 109), and those content guards moved to
// internal/consent/legal, where the stored text is. What is left here is the
// assembly: an edition's rows in, a Document a surface can render out — and
// the refusals that keep a half-published language off a reader's screen.

func edition(locales []platform.Locale, drop string) []legal.Artifact {
	slugs := []string{
		policy.SlugShortNotice,
		policy.SlugPolicyAcceptanceLabel,
		policy.SlugMarketingConsentLabel,
		policy.SlugNetworkingConsentLabel,
		policy.SlugPolicy,
	}
	var artifacts []legal.Artifact
	for _, locale := range locales {
		ordinal := 0
		for _, slug := range slugs {
			ordinal++
			if slug == drop {
				continue
			}
			artifacts = append(artifacts, legal.Artifact{
				Locale:  locale,
				Slug:    slug,
				Ordinal: ordinal,
				Body:    string(locale) + " " + slug,
			})
		}
	}
	return artifacts
}

// The whole artifact set becomes the whole document, each slug in its own
// field: this is the mapping the public endpoint and every capture surface
// render from.
func TestAWholeArtifactSetBecomesAWholeDocument(t *testing.T) {
	t.Parallel()

	document, ok := policy.DocumentFrom(platform.LocaleES, edition([]platform.Locale{platform.LocaleEN, platform.LocaleES}, ""))
	if !ok {
		t.Fatal("a complete artifact set did not assemble")
	}
	if document.Locale != platform.LocaleES {
		t.Errorf("locale = %q, want es", document.Locale)
	}
	for name, got := range map[string]string{
		"short notice":             document.ShortNotice,
		"body":                     document.BodyMarkdown,
		"policy acceptance label":  document.ConsentLabels.PolicyAcceptance,
		"marketing consent label":  document.ConsentLabels.MarketingConsent,
		"networking consent label": document.ConsentLabels.NetworkingConsent,
	} {
		if got == "" {
			t.Errorf("%s is empty", name)
		}
	}
	// Each field carries its OWN slug's text: a mapping that crossed two labels
	// over would put the marketing wording beside the networking box, which no
	// hash would notice because the same bytes are in the edition.
	if document.ConsentLabels.MarketingConsent != "es "+policy.SlugMarketingConsentLabel {
		t.Errorf("the marketing label carries %q", document.ConsentLabels.MarketingConsent)
	}
	if document.BodyMarkdown != "es "+policy.SlugPolicy {
		t.Errorf("the body carries %q", document.BodyMarkdown)
	}
}

// A language this edition does not publish is answered with nothing, never with
// another language wearing its name.
func TestAnUnpublishedLocaleHasNoDocument(t *testing.T) {
	t.Parallel()

	if _, ok := policy.DocumentFrom(platform.Locale("fr"), edition([]platform.Locale{platform.LocaleEN, platform.LocaleES}, "")); ok {
		t.Fatal("an unpublished Locale returned a document")
	}
}

// A language with a MISSING artifact is refused as if it were unpublished. A
// capture moment with a blank checkbox label beside it is worse than an honest
// 404, and the missing bytes are still inside the edition's fingerprint.
func TestAnIncompleteLocaleIsRefused(t *testing.T) {
	t.Parallel()

	artifacts := edition([]platform.Locale{platform.LocaleEN}, policy.SlugMarketingConsentLabel)
	if _, ok := policy.DocumentFrom(platform.LocaleEN, artifacts); ok {
		t.Fatal("a language missing an artifact assembled into a document")
	}
	if documents := policy.Documents(artifacts); len(documents) != 0 {
		t.Fatalf("Documents returned %d languages, want none", len(documents))
	}
}

// Documents is what one cache fill produces: every language of one edition,
// from one read, so two languages can never come from two editions.
func TestDocumentsCoversEveryCompletelyPublishedLanguage(t *testing.T) {
	t.Parallel()

	documents := policy.Documents(edition([]platform.Locale{platform.LocaleEN, platform.LocaleES}, ""))
	if len(documents) != 2 {
		t.Fatalf("assembled %d languages, want 2", len(documents))
	}
	for locale, document := range documents {
		if document.Locale != locale {
			t.Errorf("the %s document reports locale %q", locale, document.Locale)
		}
	}
}
