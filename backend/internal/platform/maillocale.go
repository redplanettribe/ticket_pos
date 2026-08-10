package platform

// ResolveMailLocale answers the one question every localized message asks: what
// language is this written in?
//
// It is the whole of ADR 0033's chain, and it is a function rather than three
// lines at each send site because the ORDER is the decision. A caller that
// consulted the Customer first would produce mail that is individually
// defensible and collectively wrong — an English receipt for a sale made in
// Spanish — and nothing about the values would say which caller got it right.
//
//  1. saleLocale, the language of the Storefront page the sale was completed
//     on. Better evidence than anything else here: it was collected at the
//     moment of the act the mail is about, from somebody who had just read a
//     whole page in that language.
//  2. rememberedLocale, the recipient's Mail Locale, remembered from the
//     Storefront they last signed in on. Older evidence, and usually absent.
//  3. DefaultLocale, always, as the floor.
//
// Both arguments are RAW as stored, empty when nothing is recorded, and are read
// through ParseLocale — so an unserved or malformed value is skipped rather than
// written in, and a caller cannot smuggle a language this platform has no copy
// in past the CHECK constraints by any other route. Nothing here can fail: a
// language is not worth refusing a purchase, a receipt or a sign-in over.
//
// Mail about no sale at all names "" for the sale and falls through the rest of
// the chain, which is why this takes strings and not a Ticket Sale — the Follow
// Digest resolves ("", the Customer's remembered Mail Locale) that way.
//
// THE PASSCODE NAMES "" FOR THE REMEMBERED ONE, not for the sale, and that
// asymmetry is deliberate rather than an oversight to tidy up. RequestOTP passes
// the language of the page the passcode was asked from and stops there: it never
// reads the stored Mail Locale, because the request is anonymous, and mail
// worded in a stranger's remembered language would be an oracle for whether the
// platform holds a Customer for that address — the disclosure the identical
// response body exists to prevent. Feeding the stored value in as a second
// candidate would be a security bug wearing the shape of a consistency fix.
func ResolveMailLocale(saleLocale, rememberedLocale string) Locale {
	for _, candidate := range []string{saleLocale, rememberedLocale} {
		if locale, ok := ParseLocale(candidate); ok {
			return locale
		}
	}
	return DefaultLocale
}
