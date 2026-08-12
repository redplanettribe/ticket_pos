package policy_test

import (
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/policy"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Both Locales carry a complete artifact set. A Locale published with an empty
// Short Notice or a missing checkbox label is a capture moment with nothing on
// it, which is worse than not offering that language at all.
func TestEveryPublishedLocaleCarriesTheWholeArtifactSet(t *testing.T) {
	t.Parallel()

	for _, locale := range policy.Locales {
		doc, ok := policy.For(locale)
		if !ok {
			t.Fatalf("locale %s is published but has no document", locale)
		}
		for name, text := range map[string]string{
			"short notice":             doc.ShortNotice,
			"body":                     doc.BodyMarkdown,
			"policy acceptance label":  doc.ConsentLabels.PolicyAcceptance,
			"marketing consent label":  doc.ConsentLabels.MarketingConsent,
			"networking consent label": doc.ConsentLabels.NetworkingConsent,
		} {
			if strings.TrimSpace(text) == "" {
				t.Errorf("locale %s: %s is empty", locale, name)
			}
		}
	}
}

// A Locale this platform does not publish is answered with nothing, never with
// English wearing another language's name. See policy.For.
func TestAnUnpublishedLocaleHasNoDocument(t *testing.T) {
	t.Parallel()

	if _, ok := policy.For(platform.Locale("fr")); ok {
		t.Fatal("an unpublished Locale returned a document")
	}
}

// ADR 0034: Marketing Consent and the Follow Digest are one switch, and the
// checkbox copy "names the Digest explicitly, so nobody grants or declines it
// without being told what it covers". This is that requirement as a test — the
// Digest is the only marketing mail the platform actually sends today, so a
// label that omitted it would be asking for consent to nothing while turning
// something on.
func TestTheMarketingLabelNamesTheFollowDigest(t *testing.T) {
	t.Parallel()

	for locale, want := range map[platform.Locale]string{
		platform.LocaleEN: "Follow Digest",
		platform.LocaleES: "Follow Digest",
	} {
		doc, _ := policy.For(locale)
		if !strings.Contains(doc.ConsentLabels.MarketingConsent, want) {
			t.Errorf("locale %s marketing label does not name the Digest: %q", locale, doc.ConsentLabels.MarketingConsent)
		}
	}
}

// Networking Consent authorizes TWO audiences (CONTEXT.md), and a Customer who
// is told about one of them has not been told what they are authorizing.
func TestTheNetworkingLabelNamesBothAudiences(t *testing.T) {
	t.Parallel()

	for locale, wants := range map[platform.Locale][]string{
		platform.LocaleEN: {"attendees", "organizers"},
		platform.LocaleES: {"asistentes", "organizadores"},
	} {
		doc, _ := policy.For(locale)
		for _, want := range wants {
			if !strings.Contains(doc.ConsentLabels.NetworkingConsent, want) {
				t.Errorf("locale %s networking label does not name %q: %q", locale, want, doc.ConsentLabels.NetworkingConsent)
			}
		}
	}
}

// The shipped text is PLACEHOLDER and has to look like it, in the document
// itself and not only in a migration comment. Nobody signs off on a privacy
// policy they mistook for a finished one.
func TestThePlaceholderTextIsVisiblyPlaceholder(t *testing.T) {
	t.Parallel()

	for _, locale := range policy.Locales {
		doc, _ := policy.For(locale)
		for _, marker := range []string{"[RUC]", "[correo PDP]"} {
			if !strings.Contains(doc.BodyMarkdown, marker) {
				t.Errorf("locale %s body no longer shows the %s placeholder", locale, marker)
			}
		}
	}
}

// The fingerprint is over the served text, so it must move when the served text
// moves and must not move otherwise. This pins the second half — the same input
// hashes the same way twice — and its real value is as a statement of intent
// beside seed_test.go, which pins the first.
func TestTheContentHashIsStable(t *testing.T) {
	t.Parallel()

	if first, second := policy.ContentHash(), policy.ContentHash(); first != second {
		t.Fatalf("content hash is not stable: %s then %s", first, second)
	}
	if len(policy.ContentHash()) != 64 {
		t.Fatalf("content hash is not a hex SHA-256: %q", policy.ContentHash())
	}
}
