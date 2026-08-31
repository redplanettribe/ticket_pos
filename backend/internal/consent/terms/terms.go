// Package terms holds the Términos y Condiciones Generales as an embedded
// document, and computes the fingerprint a Terms Version is recorded under.
//
// It is the Terms' counterpart of internal/consent/policy, and everything that
// package's doc comment argues holds here unchanged: the artifact lives in the
// backend and not in a message catalog because the hash on a Terms Version is
// EVIDENCE — the SHA-256 of the exact text a person accepted (#533, ADR 0066) —
// and a hash can only prove that if the bytes it covers are the bytes the
// reader was served. The seed drift test (seed_test.go) fails the build the
// moment the served text and the seeded fingerprint stop agreeing, exactly as
// the policy's does.
//
// It is a SIBLING of the policy package, not a client of it, and the two share
// no code on purpose: the Terms and the Privacy Policy version independently
// (ADR 0066), an edition of one never re-gates the other, and the cheapest way
// to keep that true is for neither artifact set to be able to reach the other's
// fingerprint.
//
// SPANISH ONLY, and served under every Locale. The Terms' own §37 makes the
// Spanish text prevail over any translation, and no translation is published at
// launch — so unlike the policy, which answers an unpublished Locale with a
// 404, the Terms are the same Spanish document whoever asks. That is why this
// package exposes one Document rather than a per-Locale lookup: there is
// exactly one text a person can accept.
//
// The file is authored ONE LINE PER BLOCK, for the policy package's reason: the
// Storefront renders it through the Markdown component that turns a wrapped
// line into a <br>, and the rendering is the thing being attested to.
package terms

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// artifacts holds the Terms artifact set: the Spanish document and the Spanish
// acceptance checkbox label. Every embedded file is served — the invariant that
// makes the hash honest (see the policy package).
//
//go:embed artifacts
var artifacts embed.FS

// Document is the current Terms Version's text: everything a reader can be
// shown, and nothing else. Its fields are the hash preimage (see ContentHash).
type Document struct {
	// Locale is the language the text below is written in — always Spanish, the
	// single legally prevailing text (§37), whatever language the reader asked
	// in.
	Locale platform.Locale `json:"locale"`
	// AcceptanceLabel is the mandatory, un-premarked checkbox's label, markdown
	// (§3). The UI renders it beside a link to the full text and may not reword
	// or pre-tick it.
	AcceptanceLabel string `json:"acceptance_label"`
	// BodyMarkdown is the full Términos y Condiciones, markdown.
	BodyMarkdown string `json:"body_markdown"`
}

// document is the artifact set, read once at startup; a missing or empty
// artifact panics rather than serving a reader a blank contract.
var document = mustLoad()

// Current reports the Terms document. One document, not a per-Locale lookup:
// the Spanish text is the only text there is (§37), and it is served under
// every Locale rather than 404ing the English page.
func Current() Document {
	return document
}

// ContentHash is the SHA-256 of the whole artifact set, hex-encoded: the value
// a Terms Version row carries.
//
// Taken over the SERVED VALUES, length-framed, for the policy package's
// reasons: what must be provable is what a reader was shown, and an unframed
// concatenation would let text move between fields without moving the
// fingerprint.
func ContentHash() string {
	sum := sha256.New()
	writeField(sum, string(document.Locale))
	writeField(sum, document.AcceptanceLabel)
	writeField(sum, document.BodyMarkdown)
	return hex.EncodeToString(sum.Sum(nil))
}

func writeField(sum interface{ Write([]byte) (int, error) }, value string) {
	fmt.Fprintf(sum, "%d\n%s", len(value), value)
}

func mustLoad() Document {
	return Document{
		Locale:          platform.LocaleES,
		AcceptanceLabel: mustRead("label-terms-acceptance.md"),
		BodyMarkdown:    mustRead("terms.md"),
	}
}

func mustRead(name string) string {
	path := fmt.Sprintf("artifacts/es/%s", name)
	raw, err := artifacts.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("terms: read embedded artifact %s: %v", path, err))
	}
	// Trailing newlines are a text-editor convention, not part of the contract;
	// trimming before the hash keeps the hash over what is served.
	text := strings.TrimSpace(string(raw))
	if text == "" {
		panic(fmt.Sprintf("terms: embedded artifact %s is empty", path))
	}
	return text
}
