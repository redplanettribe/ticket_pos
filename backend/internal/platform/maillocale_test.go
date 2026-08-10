package platform

import "testing"

// The chain ADR 0033 decides, exercised as one table because the ORDER is the
// substance of the decision: every row below is a statement about which of two
// pieces of evidence wins, or about what happens when neither says anything.
func TestResolveMailLocaleFollowsTheChain(t *testing.T) {
	cases := []struct {
		name       string
		sale       string
		remembered string
		want       Locale
	}{
		{
			// The whole point of recording a Sale Locale: the page the buyer read
			// and pressed the button at the bottom of is better evidence than a
			// sign-in they made months ago on another device.
			name:       "the sale outranks what the Customer remembered",
			sale:       "es",
			remembered: "en",
			want:       LocaleES,
		},
		{
			name:       "and it outranks it the other way round too",
			sale:       "en",
			remembered: "es",
			want:       LocaleEN,
		},
		{
			// A box office sale or an import: no page produced it, so the recipient
			// is asked instead. This is the case the nullable column exists for.
			name:       "a sale that names no language falls through to the memory",
			sale:       "",
			remembered: "es",
			want:       LocaleES,
		},
		{
			// The evidence is discarded, never the mail: an unserved language is a
			// language this platform has no copy in, and it must not become a
			// reason a receipt goes unsent or a purchase fails.
			name:       "a language the platform does not serve is ignored on the sale",
			sale:       "fr",
			remembered: "es",
			want:       LocaleES,
		},
		{
			name:       "and ignored on the Customer",
			sale:       "",
			remembered: "fr",
			want:       LocaleEN,
		},
		{
			name:       "a language that is not a language at all is ignored the same way",
			sale:       "not-a-locale",
			remembered: "  ",
			want:       LocaleEN,
		},
		{
			// Every sale recorded before this feature, and every Customer who has
			// never signed in: English is what all of them were written in.
			name:       "English is the floor when nothing says otherwise",
			sale:       "",
			remembered: "",
			want:       LocaleEN,
		},
		{
			// The spellings ParseLocale already accepts reach the chain unchanged,
			// because the chain is not a second parser.
			name:       "a language-and-region tag names its language",
			sale:       "es-EC",
			remembered: "",
			want:       LocaleES,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveMailLocale(tc.sale, tc.remembered); got != tc.want {
				t.Fatalf("ResolveMailLocale(%q, %q) = %q, want %q", tc.sale, tc.remembered, got, tc.want)
			}
		})
	}
}
