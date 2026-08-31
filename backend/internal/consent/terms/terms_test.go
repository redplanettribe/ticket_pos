package terms_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The document is complete: a body and an acceptance label, in Spanish — the
// single legally prevailing text (§37), served under every Locale. An empty
// artifact would be a capture moment with nothing on it.
func TestTheTermsDocumentIsCompleteAndSpanish(t *testing.T) {
	t.Parallel()

	doc := terms.Current()
	if doc.Locale != platform.LocaleES {
		t.Errorf("locale = %q, want es", doc.Locale)
	}
	for name, text := range map[string]string{
		"body":             doc.BodyMarkdown,
		"acceptance label": doc.AcceptanceLabel,
	} {
		if strings.TrimSpace(text) == "" {
			t.Errorf("%s is empty", name)
		}
	}
}

// The published text identifies the Operator and says where to write — the
// same guard the policy's body carries, for the same reason: a contract that
// cannot say who is on the other side of it fails silently, with the page
// rendering and the hash verifying.
func TestThePublishedTextIdentifiesTheOperator(t *testing.T) {
	t.Parallel()

	doc := terms.Current()
	for _, want := range []string{"REDPLANETTRIBE", "1793228468001", "info@redplanettribe.org"} {
		if !strings.Contains(doc.BodyMarkdown, want) {
			t.Errorf("body does not carry %q", want)
		}
	}
}

// The draft's own header lines — "First Draft", "Versión 0.1", the draft date —
// are stripped from the served artifact (#535): what a person accepts is the
// edition the Terms Version row names, and a body that called itself a draft
// with its own version number would contradict the label beside it.
func TestTheDraftHeaderLinesAreStripped(t *testing.T) {
	t.Parallel()

	doc := terms.Current()
	for _, leftover := range []string{"First Draft", "Versión 0.1", "Fecha del borrador"} {
		if strings.Contains(doc.BodyMarkdown, leftover) {
			t.Errorf("body still carries the draft header %q", leftover)
		}
	}
}

// No bracketed placeholder survives into the published edition — the policy
// body's guard, applied to this document.
func TestThePublishedTextHasNoPlaceholdersLeft(t *testing.T) {
	t.Parallel()

	bracketed := regexp.MustCompile(`\[[A-ZÁÉÍÓÚÑ /]{4,}\]`)
	if found := bracketed.FindString(terms.Current().BodyMarkdown); found != "" {
		t.Errorf("body still carries the placeholder %s", found)
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
