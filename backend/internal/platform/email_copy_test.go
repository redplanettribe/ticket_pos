package platform

import (
	"strings"
	"testing"
)

// TestEveryMailCopyIsWrittenInBothLanguages is the backend's counterpart to the
// Storefront catalog's parity test (apps/storefront/lib/messages.test.ts).
//
// The Storefront holds two JSON files in key-for-key parity; this file holds one
// sentence in two languages side by side, so the shape of the failure it has to
// catch is different: not a missing key but a blank half — a line written in
// English with its Spanish left as "" to be filled in later, which renders as an
// empty subject or a gap in the middle of an email nobody would ever see in
// English.
//
// It walks the values themselves rather than a list somebody maintains, because
// translated() is the only way to make copy this platform sends and every call
// registers itself.
func TestEveryMailCopyIsWrittenInBothLanguages(t *testing.T) {
	if len(allMailCopy.all) == 0 {
		t.Fatal("no mail copy was registered; translated() is the only way to declare a sentence and something has stopped calling it")
	}

	for _, sentence := range allMailCopy.all {
		if strings.TrimSpace(sentence.en) == "" {
			t.Errorf("mail copy with Spanish %q has no English", sentence.es)
		}
		if strings.TrimSpace(sentence.es) == "" {
			t.Errorf("mail copy with English %q has no Spanish", sentence.en)
		}
	}
}

// TestMailCopyFallsBackToEnglishForALanguageNothingIsWrittenIn covers the one
// branch a reader cannot reach through the API: a Locale that is neither of the
// two, which ParseLocale and the mail_locale CHECK both refuse. English is the
// floor of the chain (ADR 0033), and an empty email is not.
func TestMailCopyFallsBackToEnglishForALanguageNothingIsWrittenIn(t *testing.T) {
	// Declared on a registry of this test's own, never through translated(): a
	// fixture in allMailCopy would be walked by the parity test above forever
	// after, which is a test asserting on its own furniture.
	sentence := (&copyRegistry{}).declare("English", "Español")

	if got := sentence.in(Locale("fr")); got != "English" {
		t.Fatalf("in(fr) = %q, want the English", got)
	}
	if got := sentence.in(""); got != "English" {
		t.Fatalf("in(\"\") = %q, want the English", got)
	}
}
