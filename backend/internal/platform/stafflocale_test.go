package platform

import "testing"

// The floor under every piece of staff mail (ADR 0041), as a table for the
// reason the Customer chain is one: the interesting rows are the ones where the
// storage says nothing, or says something this platform cannot write in.
//
// What a Spanish reader actually receives is asserted over HTTP against a real
// database, in integration/staff_mail_locale_test.go. This is the floor alone,
// including the two rows no API can reach: a column CHECK stands between the
// switcher and an unserved value, and the branch still has to hold, because the
// alternative to English is an email nobody can read.
func TestResolveStaffLocaleFallsToEnglish(t *testing.T) {
	cases := []struct {
		name   string
		stored string
		want   Locale
	}{
		{
			name:   "a stated language is written in",
			stored: "es",
			want:   LocaleES,
		},
		{
			name:   "English stated is English written",
			stored: "en",
			want:   LocaleEN,
		},
		{
			// The ordinary case, and the one the storage is absent-by-default for:
			// nobody is given a language by being invited, imported or paid out.
			// Absence is not a choice of English — it is English to the READER and
			// nothing at all to the next sign-in, which is still free to record what
			// it detects.
			name:   "absence is English to the reader",
			stored: "",
			want:   LocaleEN,
		},
		{
			name:   "a language this platform writes nothing in is not written",
			stored: "fr",
			want:   LocaleEN,
		},
		{
			name:   "and neither is something that is not a language",
			stored: "  ",
			want:   LocaleEN,
		},
		{
			// The spellings ParseLocale accepts arrive unchanged: this is a floor,
			// not a second parser.
			name:   "a language-and-region tag names its language",
			stored: "es-EC",
			want:   LocaleES,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveStaffLocale(tc.stored); got != tc.want {
				t.Fatalf("ResolveStaffLocale(%q) = %q, want %q", tc.stored, got, tc.want)
			}
		})
	}
}
