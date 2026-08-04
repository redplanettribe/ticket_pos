package catalog

import (
	"net/url"
	"strings"
)

// RegistrationMode is how an Event takes sign-ups: it sells Ticket Types on this
// platform, or it carries a Registration Link and sends its audience elsewhere.
// Never both (ADR 0028).
type RegistrationMode string

const (
	// RegistrationModeTickets is the default and today's behaviour: the Event
	// sells Ticket Types here.
	RegistrationModeTickets RegistrationMode = "tickets"
	// RegistrationModeExternal is External Registration: the Event sells nothing
	// here and hands its audience to the Registration Link.
	RegistrationModeExternal RegistrationMode = "external"
)

// ParseRegistrationMode converts a submitted value into a RegistrationMode,
// reporting whether it is one this system knows.
func ParseRegistrationMode(raw string) (RegistrationMode, bool) {
	switch RegistrationMode(raw) {
	case RegistrationModeTickets:
		return RegistrationModeTickets, true
	case RegistrationModeExternal:
		return RegistrationModeExternal, true
	}
	return "", false
}

// RegistrationModeOrDefault reads a stored mode, falling back to the default for
// anything unrecognised. A row predating migration 048 — or one written by a
// future version this binary does not understand — reads as an ordinary ticketed
// Event, which is the safe answer: it sells tickets and hands nobody anywhere.
func RegistrationModeOrDefault(raw string) RegistrationMode {
	if mode, ok := ParseRegistrationMode(raw); ok {
		return mode
	}
	return RegistrationModeTickets
}

// IsValidRegistrationURL reports whether a Registration Link is one this platform
// will store.
//
// The rule is a scheme allowlist of https and nothing else. This is a security
// control, not a formatting preference: the stored value is destined for both an
// href on the Storefront Event page and a Location header on the redirect route,
// and an allowlist is the mechanism that keeps javascript: and data: out of both.
// A substring or prefix check is not a substitute — "https" appears inside
// plenty of strings that are not https URLs, and a parser is what decides what a
// browser will actually do with the value.
//
// It also refuses http, so a Customer is never downgraded to an insecure
// connection midway through their journey.
//
// There is deliberately NO host allowlist. Organizers legitimately use Luma,
// Eventbrite, Google Forms, Typeform, Notion and their own sites, and maintaining
// a list of approved hosts is a treadmill with no security benefit once the
// scheme is constrained.
func IsValidRegistrationURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	// A control character or space inside the value can split a Location header;
	// url.Parse tolerates some of them, so they are refused outright.
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	// Scheme comparison is case-insensitive by RFC 3986 and url.Parse already
	// lowercases it, so "JavaScript:" arrives here as "javascript".
	if parsed.Scheme != "https" {
		return false
	}
	// A scheme with no host is not somewhere a Customer can be sent.
	if parsed.Host == "" {
		return false
	}
	return true
}
