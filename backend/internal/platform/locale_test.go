package platform

import "testing"

func TestParseLocaleAcceptsTheLanguagesTheStorefrontServes(t *testing.T) {
	for raw, want := range map[string]Locale{
		"en":    LocaleEN,
		"es":    LocaleES,
		" ES ":  LocaleES,
		"en-US": LocaleEN,
		"es-EC": LocaleES,
	} {
		got, ok := ParseLocale(raw)
		if !ok || got != want {
			t.Fatalf("ParseLocale(%q) = %q, %v; want %q, true", raw, got, ok, want)
		}
	}
}

func TestParseLocaleRefusesALanguageNothingIsWrittenIn(t *testing.T) {
	for _, raw := range []string{"", "fr", "pt-BR", "english", "e"} {
		if got, ok := ParseLocale(raw); ok {
			t.Fatalf("ParseLocale(%q) = %q, true; want refused", raw, got)
		}
	}
}
