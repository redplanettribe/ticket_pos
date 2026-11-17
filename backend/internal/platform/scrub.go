package platform

import (
	"regexp"
	"strings"
)

// What the API may send to its error reporting vendor about a person: nothing
// (ADR 0079). The scrubber runs inside this process before an event or a span
// leaves it, and is the first net; the vendor's server-side scrubbing is the
// second, never the first.
//
// It redacts the same shapes as the Next apps' scrubber
// (packages/sentry-rules/src/scrub.ts), so the three deployables send the
// same nothing:
//
//   - an email-shaped string becomes "[email]", including one URL-encoded as
//     %40, which is how an email turns up in a path;
//   - an identity-number-shaped string becomes "[identity-number]": a cédula
//     or RUC by the shape taxid.go defines (hasIdentityNumberShape), standing
//     alone, or a cédula written with a hyphen before its check digit;
//   - a URL loses its query string and fragment, where tokens and form values
//     travel; in free text, any word with a "?" inside it is cut there, since a
//     relative URL ("api/orders?token=...") has no other mark of being one.
//
// A UUID, an amount, and the 49-digit SRI clave de acceso are not shaped like
// any of these, and survive: they are what makes an event useful.
//
// The scrubber reads text. Cookies, headers, request bodies and the client's
// IP address are never attached to an event in the first place.

// Letters in any script: a buyer's address can be "josé.pérez@correo.ec".
var scrubEmail = regexp.MustCompile(`(?i)[\p{L}\p{N}._%+-]+(?:@|%40)[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)*\.\p{L}{2,}`)

// A query string: a "?" inside a word, with more of the word after it. A "?"
// that ends a word in a sentence is followed by a space, and stays.
var scrubQueryString = regexp.MustCompile(`([^\s"'<>?]+)\?[^\s"'<>]+`)

// A fragment following an absolute URL, or a path.
var scrubFragment = regexp.MustCompile(`(?i)((?:\b[a-z][a-z0-9+.-]*://|/)[^\s"'<>?#]*)#[^\s"'<>]*`)

// A run of digits, or a cédula written with a hyphen before its check digit:
// the candidates hasIdentityNumberShape judges.
var scrubDigits = regexp.MustCompile(`[0-9]{9}-[0-9]|[0-9]+`)

// The tokens what is redacted is replaced with.
const (
	redactedEmail          = "[email]"
	redactedIdentityNumber = "[identity-number]"
)

// ScrubText redacts email-shaped and identity-number-shaped strings in text
// and drops the query strings and fragments of the URLs in it. It is pure, and
// it is what runs on every string of an event and, through ScrubURL, of a
// span (ADR 0079).
func ScrubText(text string) string {
	text = scrubQueryString.ReplaceAllString(text, "$1")
	text = scrubFragment.ReplaceAllString(text, "$1")
	text = scrubEmail.ReplaceAllString(text, redactedEmail)
	return redactIdentityNumbers(text)
}

// ScrubURL is url without its query string or fragment, then scrubbed like any
// text: an outbound span's URL, a transaction's name.
func ScrubURL(url string) string {
	if cut := strings.IndexAny(url, "?#"); cut != -1 {
		url = url[:cut]
	}
	return ScrubText(url)
}

// ScrubError is err with its text scrubbed, while errors.Is and errors.As, and
// so ErrorClass, still see the error underneath: the recipient redaction's
// redactedError, with ScrubText for the rule.
func ScrubError(err error) error {
	if err == nil {
		return nil
	}
	return redactError(err, ScrubText)
}

// redactIdentityNumbers replaces every standalone identity-number-shaped run
// of digits: one with no letter or digit next to it, so a run inside a UUID, a
// longer number or a word survives.
func redactIdentityNumbers(text string) string {
	matches := scrubDigits.FindAllStringIndex(text, -1)
	if matches == nil {
		return text
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if standsAlone(text, start, end) && hasIdentityNumberShape(strings.Replace(text[start:end], "-", "", 1)) {
			out.WriteString(text[last:start])
			out.WriteString(redactedIdentityNumber)
			last = end
		}
	}
	out.WriteString(text[last:])
	return out.String()
}

// standsAlone reports whether text[start:end] has no ASCII letter or digit on
// either side.
func standsAlone(text string, start, end int) bool {
	return (start == 0 || !isASCIIAlphanumeric(text[start-1])) &&
		(end == len(text) || !isASCIIAlphanumeric(text[end]))
}

func isASCIIAlphanumeric(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// redactedError carries an error's text with something personal taken out,
// while errors.Is and errors.As still see the error underneath. The SES sender
// redacts a recipient with it, and the scrubber everything shaped like one.
type redactedError struct {
	text string
	err  error
}

func (e *redactedError) Error() string { return e.text }
func (e *redactedError) Unwrap() error { return e.err }

// redactError is err with redact applied to its text.
func redactError(err error, redact func(string) string) error {
	return &redactedError{text: redact(err.Error()), err: err}
}
