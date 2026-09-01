package legal_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/policy"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The guards that used to live in internal/consent/{policy,terms}'s package
// tests, now read where the text lives: migration 109.
//
// They are about the CURRENT editions — policy `1` and terms `1` — because
// those are what a reader is shown. A superseded edition is a record of what
// was shown and is deliberately not held to today's rules: `0-placeholder` is
// placeholder prose with bracketed markers in it, and it stays that way.
//
// These are content assertions, not fingerprint ones. Migration 109's proof and
// preimage_test.go's are what stop the text drifting from the fingerprint; these
// are what stop a legal document being published with a hole in it — a missing
// controller, a leftover draft header, a translation that renumbered a clause —
// which is the kind of failure that renders fine, hashes fine, and is only
// noticed by a reader.

const (
	currentPolicy = "1"
	currentTerms  = "1"
)

func policyText(t *testing.T, locale platform.Locale, slug string) string {
	t.Helper()
	return publishedText(t, "policy", currentPolicy, locale, slug)
}

func termsText(t *testing.T, locale platform.Locale, slug string) string {
	t.Helper()
	return publishedText(t, "terms", currentTerms, locale, slug)
}

// ADR 0034: Marketing Consent and the Follow Digest are one switch, and the
// checkbox copy names the Digest explicitly, so nobody grants or declines it
// without being told what it covers.
func TestTheMarketingLabelNamesTheFollowDigest(t *testing.T) {
	t.Parallel()

	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		label := policyText(t, locale, policy.SlugMarketingConsentLabel)
		if !strings.Contains(label, "Follow Digest") {
			t.Errorf("the %s marketing label does not name the Digest: %q", locale, label)
		}
	}
}

// Networking Consent authorizes TWO audiences (CONTEXT.md), and a Customer told
// about one of them has not been told what they are authorizing.
func TestTheNetworkingLabelNamesBothAudiences(t *testing.T) {
	t.Parallel()

	for locale, wants := range map[platform.Locale][]string{
		platform.LocaleEN: {"attendees", "organizers"},
		platform.LocaleES: {"asistentes", "organizadores"},
	} {
		label := policyText(t, locale, policy.SlugNetworkingConsentLabel)
		for _, want := range wants {
			if !strings.Contains(label, want) {
				t.Errorf("the %s networking label does not name %q: %q", locale, want, label)
			}
		}
	}
}

// The published policy IDENTIFIES ITS CONTROLLER and says where to write, in
// the body and in the Short Notice, in both languages. A notice that cannot say
// who is processing the data, or where a rights request goes, fails the thing a
// privacy notice is for — and fails it silently.
func TestThePublishedPolicyIdentifiesTheController(t *testing.T) {
	t.Parallel()

	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		for _, slug := range []string{policy.SlugPolicy, policy.SlugShortNotice} {
			text := policyText(t, locale, slug)
			for _, want := range []string{"REDPLANETTRIBE", "1793228468001", "info@redplanettribe.org"} {
				if !strings.Contains(text, want) {
					t.Errorf("the %s %s does not carry %q", locale, slug, want)
				}
			}
		}
	}
}

// The published Terms identify the Operator too — the same guard, for the same
// reason: a contract that cannot say who is on the other side of it.
func TestThePublishedTermsIdentifyTheOperator(t *testing.T) {
	t.Parallel()

	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		body := termsText(t, locale, terms.SlugTerms)
		for _, want := range []string{"REDPLANETTRIBE", "1793228468001", "info@redplanettribe.org"} {
			if !strings.Contains(body, want) {
				t.Errorf("the %s terms body does not carry %q", locale, want)
			}
		}
	}
}

// No bracketed placeholder survives into a CURRENT edition: a section of a legal
// document landing with a slot somebody meant to come back to.
func TestThePublishedTextHasNoPlaceholdersLeft(t *testing.T) {
	t.Parallel()

	bracketed := regexp.MustCompile(`\[[A-ZÁÉÍÓÚÑ /]{4,}\]`)
	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		for _, text := range []string{
			policyText(t, locale, policy.SlugPolicy),
			policyText(t, locale, policy.SlugShortNotice),
			termsText(t, locale, terms.SlugTerms),
		} {
			if found := bracketed.FindString(text); found != "" {
				t.Errorf("a published %s document still carries the placeholder %s", locale, found)
			}
		}
	}
}

// The English Terms say out loud that they are a translation and that the
// Spanish prevails, in the first block after the title — so it is read before
// the contract is, and not left to §37 at the far end of a long document.
func TestTheEnglishTermsDeclareThemselvesATranslation(t *testing.T) {
	t.Parallel()

	blocks := strings.SplitN(termsText(t, platform.LocaleEN, terms.SlugTerms), "\n\n", 3)
	if len(blocks) < 2 {
		t.Fatal("the english terms body has no block after its title")
	}
	for _, want := range []string{"Courtesy translation", "Spanish version prevails"} {
		if !strings.Contains(blocks[1], want) {
			t.Errorf("the english terms' first block does not carry %q: %q", want, blocks[1])
		}
	}
}

// The draft's own header lines are stripped from the published contract (#535):
// what a person accepts is the edition the Terms Version row names, and a body
// calling itself a draft with its own version number would contradict it.
func TestTheDraftHeaderLinesAreStripped(t *testing.T) {
	t.Parallel()

	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		body := termsText(t, locale, terms.SlugTerms)
		for _, leftover := range []string{"First Draft", "Versión 0.1", "Fecha del borrador"} {
			if strings.Contains(body, leftover) {
				t.Errorf("the %s terms body still carries the draft header %q", locale, leftover)
			}
		}
	}
}

// The two languages are ONE EDITION and have to say the same things in the same
// order. Section numbering is the mechanical half of that, and it is what every
// clause citation in this codebase (§3, §37) depends on.
func TestTheTermsTranslationKeepsTheSameSections(t *testing.T) {
	t.Parallel()

	headings := regexp.MustCompile(`(?m)^## (\d+)\.`)
	sections := func(locale platform.Locale) []string {
		var numbers []string
		for _, match := range headings.FindAllStringSubmatch(termsText(t, locale, terms.SlugTerms), -1) {
			numbers = append(numbers, match[1])
		}
		return numbers
	}

	spanish, english := sections(platform.LocaleES), sections(platform.LocaleEN)
	if len(spanish) == 0 {
		t.Fatal("the spanish terms body has no numbered sections")
	}
	if strings.Join(spanish, ",") != strings.Join(english, ",") {
		t.Errorf("the two languages number their sections differently:\n  es: %v\n  en: %v", spanish, english)
	}
}
