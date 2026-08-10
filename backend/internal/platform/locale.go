package platform

import "strings"

// Locale is a language this platform serves, spelled the way the Storefront
// spells it in an address (`/{locale}/...`).
//
// A Locale is a property of a page, and the API deliberately does not have one:
// no read path takes an Accept-Language and no response is worded (ADR 0027).
// This type exists for the one thing written outside a page — a Customer's
// Mail Locale, which the Follow Digest is composed in (ADR 0030) — and it is
// the language token alone, never a language-and-region tag. Region decides
// number marks and month names, which only a page ever draws.
type Locale string

const (
	// LocaleEN is English.
	LocaleEN Locale = "en"
	// LocaleES is Spanish.
	LocaleES Locale = "es"
)

// DefaultLocale is what a Customer is read in when nothing about them says
// otherwise: someone whose record a box office sale or an import created has
// never been on a localized surface, and English is what every mail this
// platform has ever sent was written in.
const DefaultLocale = LocaleEN

// ParseLocale resolves a language token to a Locale this platform serves,
// reporting whether it is one.
//
// It is deliberately permissive about spelling and strict about the answer: a
// language-and-region tag ("es-EC") names the same language as its primary
// subtag, but a language nothing here is written in is not a Locale — the
// caller keeps what it had rather than remembering a language it cannot write.
func ParseLocale(raw string) (Locale, bool) {
	token := strings.ToLower(strings.TrimSpace(raw))
	if primary, _, found := strings.Cut(token, "-"); found {
		token = primary
	}
	switch Locale(token) {
	case LocaleEN:
		return LocaleEN, true
	case LocaleES:
		return LocaleES, true
	default:
		return "", false
	}
}
