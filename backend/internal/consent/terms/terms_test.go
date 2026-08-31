package terms_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Every published Locale's document is complete: a body and an acceptance
// label. An empty artifact would be a capture moment with nothing on it.
func TestEveryPublishedTermsDocumentIsComplete(t *testing.T) {
	t.Parallel()

	for _, locale := range terms.Locales {
		doc, ok := terms.For(locale)
		if !ok {
			t.Fatalf("%s is listed as published but has no document", locale)
		}
		if doc.Locale != locale {
			t.Errorf("%s document reports locale %q", locale, doc.Locale)
		}
		for name, text := range map[string]string{
			"body":             doc.BodyMarkdown,
			"acceptance label": doc.AcceptanceLabel,
		} {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s %s is empty", locale, name)
			}
		}
	}
}

// Both languages are published, and the Spanish one is the prevailing text
// (§37). This pins the pair rather than the count: an edition that quietly lost
// its translation would leave an English reader ticking a box beside a document
// they cannot read, and one that quietly lost the Spanish would leave the
// operative contract unpublished.
func TestTheTermsArePublishedInBothLocalesWithSpanishPrevailing(t *testing.T) {
	t.Parallel()

	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		if _, ok := terms.For(locale); !ok {
			t.Errorf("the terms are not published in %s", locale)
		}
	}
	if terms.PrevailingLocale != platform.LocaleES {
		t.Errorf("prevailing locale = %q, want es", terms.PrevailingLocale)
	}
}

// A Locale this platform does not publish is refused rather than answered in
// another language — policy.For's rule, for its reason: a contract the reader
// cannot read must never be presented as the one they accepted.
func TestAnUnpublishedLocaleHasNoDocument(t *testing.T) {
	t.Parallel()

	if _, ok := terms.For(platform.Locale("fr")); ok {
		t.Error("an unpublished locale returned a document")
	}
}

// The English text says out loud that it is a translation and that the Spanish
// prevails. Without that line an English reader would have no way to know from
// the page in front of them which words bind them, and §37 alone — buried at
// the end of a long document — is not where somebody looks before ticking.
func TestTheEnglishTextDeclaresItselfATranslation(t *testing.T) {
	t.Parallel()

	doc, ok := terms.For(platform.LocaleEN)
	if !ok {
		t.Fatal("the terms are not published in en")
	}
	// The first block after the title, so it is read before the contract is.
	blocks := strings.SplitN(doc.BodyMarkdown, "\n\n", 3)
	if len(blocks) < 2 {
		t.Fatalf("the english body has no block after its title")
	}
	notice := blocks[1]
	for _, want := range []string{"Courtesy translation", "Spanish version prevails"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the english body's first block does not carry %q: %q", want, notice)
		}
	}
}

// The published text identifies the Operator and says where to write, in every
// language it is published in — the same guard the policy's body carries, for
// the same reason: a contract that cannot say who is on the other side of it
// fails silently, with the page rendering and the hash verifying.
func TestThePublishedTextIdentifiesTheOperator(t *testing.T) {
	t.Parallel()

	for _, locale := range terms.Locales {
		doc, _ := terms.For(locale)
		for _, want := range []string{"REDPLANETTRIBE", "1793228468001", "info@redplanettribe.org"} {
			if !strings.Contains(doc.BodyMarkdown, want) {
				t.Errorf("the %s body does not carry %q", locale, want)
			}
		}
	}
}

// The draft's own header lines — "First Draft", "Versión 0.1", the draft date —
// are stripped from the served artifact (#535): what a person accepts is the
// edition the Terms Version row names, and a body that called itself a draft
// with its own version number would contradict the label beside it.
func TestTheDraftHeaderLinesAreStripped(t *testing.T) {
	t.Parallel()

	for _, locale := range terms.Locales {
		doc, _ := terms.For(locale)
		for _, leftover := range []string{"First Draft", "Versión 0.1", "Fecha del borrador"} {
			if strings.Contains(doc.BodyMarkdown, leftover) {
				t.Errorf("the %s body still carries the draft header %q", locale, leftover)
			}
		}
	}
}

// No bracketed placeholder survives into the published edition — the policy
// body's guard, applied to this document in every language.
func TestThePublishedTextHasNoPlaceholdersLeft(t *testing.T) {
	t.Parallel()

	bracketed := regexp.MustCompile(`\[[A-ZÁÉÍÓÚÑ /]{4,}\]`)
	for _, locale := range terms.Locales {
		doc, _ := terms.For(locale)
		if found := bracketed.FindString(doc.BodyMarkdown); found != "" {
			t.Errorf("the %s body still carries the placeholder %s", locale, found)
		}
	}
}

// The two languages are ONE EDITION and have to say the same things in the same
// order. Section numbering is the thin, mechanical half of that — the half a
// test can hold — and it is what the cross-references inside both texts (§3,
// §37) and every clause citation in this codebase depend on: a translation that
// dropped or renumbered a section would silently make those citations point at
// different clauses in the two languages.
func TestTheTranslationKeepsTheSameSections(t *testing.T) {
	t.Parallel()

	headings := regexp.MustCompile(`(?m)^## (\d+)\.`)
	sections := func(locale platform.Locale) []string {
		doc, _ := terms.For(locale)
		var numbers []string
		for _, match := range headings.FindAllStringSubmatch(doc.BodyMarkdown, -1) {
			numbers = append(numbers, match[1])
		}
		return numbers
	}

	spanish, english := sections(platform.LocaleES), sections(platform.LocaleEN)
	if len(spanish) == 0 {
		t.Fatal("the spanish body has no numbered sections")
	}
	if strings.Join(spanish, ",") != strings.Join(english, ",") {
		t.Errorf("the two languages number their sections differently:\n  es: %v\n  en: %v", spanish, english)
	}
}

// The fingerprint moves when the served text moves and not otherwise; this pins
// the second half, beside seed_test.go which pins the first.
func TestTheContentHashIsStable(t *testing.T) {
	t.Parallel()

	if first, second := terms.ContentHash(), terms.ContentHash(); first != second {
		t.Fatalf("content hash is not stable: %s then %s", first, second)
	}
	if len(terms.ContentHash()) != 64 {
		t.Fatalf("content hash is not a hex SHA-256: %q", terms.ContentHash())
	}
}
