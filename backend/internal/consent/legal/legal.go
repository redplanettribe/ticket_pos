// Package legal holds what the Privacy Policy and the Términos y Condiciones
// have in common now that both live in the database: the shape of one stored
// piece of text, and the rule that turns an edition's stored rows back into the
// fingerprint it was published under.
//
// IT HOLDS NO TEXT AND NO DOCUMENT SHAPE, deliberately. The policy and terms
// packages beside it stay siblings that share nothing about their CONTENT (ADR
// 0066): the two documents version independently, an edition of one must never
// re-gate the other, and neither is able to reach the other's artifacts. What
// they do share, and must share, is the FRAMING — because there is one preimage
// rule and a Go implementation that disagreed with the SQL one, or with itself
// between two documents, would produce a fingerprint that proves nothing.
//
// The rule used to be implicit in Go field order: policy.ContentHash walked a
// struct, terms.ContentHash walked a smaller one, and both walked a hand-pinned
// []Locale{en, es}. Neither survives text an operator can edit, so both became
// data — `ordinal` on the row, ascending locale code across rows — and the rule
// is written down here and in migration 109, which proves the two agree against
// every fingerprint this platform has ever published.
package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Artifact is one stored piece of a published edition: one language's version
// of one thing a reader can be shown.
//
// It is the row (policy_version_artifacts, terms_version_artifacts) with the
// version id left off, because an Artifact is only ever handled as part of the
// edition it was read with — the read that fetches text also fetches the
// version, its label, its effective date and its fingerprint, so that no
// surface can render text from one edition beside evidence naming another.
type Artifact struct {
	// Locale is the language this text is written in.
	Locale platform.Locale
	// Slug names which artifact this is, in the vocabulary the surfaces use
	// ("short-notice", "terms", "label-marketing-consent"). Its meaning belongs
	// to the document's own package; nothing here interprets it.
	Slug string
	// Ordinal is this artifact's position in the fingerprint preimage, within
	// its language. It is DATA rather than the order of fields in a Go struct,
	// because an operator adding or removing an artifact must not need a deploy
	// to change how an edition is hashed.
	Ordinal int
	// Body is the text, markdown, already trimmed — the bytes served and the
	// bytes hashed, with no normalisation step in between (the CHECK on the
	// column enforces it).
	Body string
}

// ContentHash is the SHA-256 of a whole edition, hex-encoded: the value the
// version row carries and every acceptance points at.
//
// THE PREIMAGE RULE, which migration 109's DO block implements in SQL and
// internal/consent/legal's preimage test proves reproduces every fingerprint
// this platform has published:
//
//   - Languages in ASCENDING LOCALE CODE order, compared as bytes. A rule now,
//     not the coincidence it was: it reproduces the old hand-pinned {en, es}
//     because "en" < "es", and it keeps a third language from landing wherever
//     a slice literal happened to put it.
//   - Each language's CODE first, as its own framed field, then that language's
//     artifacts in ORDINAL order.
//   - Every field framed: its BYTE length (len of the Go string, octet_length
//     in SQL), a newline, then the field. Without framing, moving a sentence
//     from the Short Notice into a label would leave the fingerprint untouched.
//
// It covers EVERY language at once, unchanged from when the text was embedded:
// an edition is one published version of the document and not one per
// language. A person accepts the edition, in whichever language they read it,
// and a correction to one translation alone is still a new edition.
//
// The argument is not sorted in place: callers hold the edition they read.
func ContentHash(artifacts []Artifact) string {
	ordered := slices.Clone(artifacts)
	slices.SortFunc(ordered, func(a, b Artifact) int {
		if a.Locale != b.Locale {
			return strings.Compare(string(a.Locale), string(b.Locale))
		}
		return a.Ordinal - b.Ordinal
	})

	sum := sha256.New()
	var locale platform.Locale
	for i, artifact := range ordered {
		if i == 0 || artifact.Locale != locale {
			locale = artifact.Locale
			writeField(sum, string(locale))
		}
		writeField(sum, artifact.Body)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func writeField(sum interface{ Write([]byte) (int, error) }, value string) {
	fmt.Fprintf(sum, "%d\n%s", len(value), value)
}

// Locales are the languages an edition is published in, ascending — read from
// the edition's OWN rows and never from a compile-time constant.
//
// That is what makes a language a publishing decision instead of a deploy: an
// edition published without its translation starts 404ing that language the
// moment it becomes current, with nothing to ship and nothing to remember.
func Locales(artifacts []Artifact) []platform.Locale {
	locales := make([]platform.Locale, 0, 2)
	for _, artifact := range artifacts {
		if !slices.Contains(locales, artifact.Locale) {
			locales = append(locales, artifact.Locale)
		}
	}
	slices.Sort(locales)
	return locales
}

// InLocale is the artifacts of one language, in ordinal order.
func InLocale(artifacts []Artifact, locale platform.Locale) []Artifact {
	var found []Artifact
	for _, artifact := range artifacts {
		if artifact.Locale == locale {
			found = append(found, artifact)
		}
	}
	slices.SortFunc(found, func(a, b Artifact) int { return a.Ordinal - b.Ordinal })
	return found
}
