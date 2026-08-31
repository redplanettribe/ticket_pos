// Package terms holds the Términos y Condiciones Generales as embedded
// per-Locale documents, and computes the fingerprint a Terms Version is
// recorded under.
//
// It is the Terms' counterpart of internal/consent/policy, and everything that
// package's doc comment argues holds here unchanged: the artifacts live in the
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
// PUBLISHED IN BOTH LOCALES, WITH SPANISH PREVAILING. The Terms' own §37 makes
// the Spanish text prevail over any translation, and the English artifact says
// so in its own first line: it is a courtesy translation, published so that an
// English reader knows what they are ticking, and it never becomes the
// operative text. That is a statement about which words WIN, not about which
// words are PUBLISHED — so the lookup is per-Locale like the policy's, an
// unpublished Locale is refused rather than answered in a language nobody
// asked for, and both translations belong to one edition under one fingerprint
// (see ContentHash).
//
// The files are authored ONE LINE PER BLOCK, for the policy package's reason:
// the Storefront renders them through the Markdown component that turns a
// wrapped line into a <br>, and the rendering is the thing being attested to.
package terms

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// artifacts holds the Terms artifact set: one directory per Locale, each with
// the document body and the acceptance checkbox label. Every embedded file is
// served — the invariant that makes the hash honest (see the policy package).
//
//go:embed artifacts
var artifacts embed.FS

// Document is one Locale's rendering of the current Terms Version:
// everything a reader can be shown, and nothing else. Its fields are the hash
// preimage (see ContentHash).
type Document struct {
	// Locale is the language the text below is written in. The Spanish document
	// is the single legally prevailing text (§37); the English one is the
	// courtesy translation that says so in its own first line.
	Locale platform.Locale `json:"locale"`
	// AcceptanceLabel is the mandatory, un-premarked checkbox's label, markdown
	// (§3). The UI renders it beside a link to the full text and may not reword
	// or pre-tick it.
	AcceptanceLabel string `json:"acceptance_label"`
	// BodyMarkdown is the full Términos y Condiciones, markdown.
	BodyMarkdown string `json:"body_markdown"`
}

// Locales are the Locales the Terms are published in, in the order the
// fingerprint walks them.
//
// Fixed, and not derived from the embedded directory listing, for the policy
// package's reason: the order decides the hash, and a hash that depended on how
// a filesystem sorted names would be a hash that could change without the text
// changing.
var Locales = []platform.Locale{platform.LocaleEN, platform.LocaleES}

// PrevailingLocale is the language whose words are the contract: Spanish, over
// any translation (§37).
//
// Named here rather than assumed at each call site, because several places need
// to point AT the operative text specifically — the Staff app's link out to the
// public terms page among them — and "es" spelled in each of them is a fact
// about the document that the document's own package should own.
const PrevailingLocale = platform.LocaleES

// documents is the artifact set, read once at startup; a missing or empty
// artifact panics rather than serving a reader a blank contract.
var documents = mustLoadAll()

// For reports the current Terms Version's artifact in one Locale, reporting
// whether the platform publishes that Locale at all.
//
// It never falls back, matching policy.For: a Locale this platform does not
// serve is a caller's mistake, and answering it with the document in another
// language would present a contract the reader cannot read as the one they
// accepted. Both published Locales carry the same edition, so the choice is
// which words a reader is shown, never which contract binds them.
func For(locale platform.Locale) (Document, bool) {
	doc, ok := documents[locale]
	return doc, ok
}

// ContentHash is the SHA-256 of the whole artifact set, hex-encoded: the value
// a Terms Version row carries.
//
// Taken over the SERVED VALUES, length-framed, for the policy package's
// reasons: what must be provable is what a reader was shown, and an unframed
// concatenation would let text move between fields without moving the
// fingerprint.
//
// It covers EVERY Locale at once, policy.ContentHash's rule and for its reason:
// a Terms Version is one published edition of the contract and not one per
// language. A person accepts the edition, in whichever language they read it,
// and a correction to the English translation alone is still a new edition —
// per-Locale hashes would let the two texts drift apart under one label, which
// is the exact thing the label exists to prevent. It is also what keeps §37
// honest: the prevailing Spanish text and the translation a reader was actually
// shown are fingerprinted together, so the evidence covers both.
func ContentHash() string {
	sum := sha256.New()
	for _, locale := range Locales {
		doc := documents[locale]
		writeField(sum, string(locale))
		writeField(sum, doc.AcceptanceLabel)
		writeField(sum, doc.BodyMarkdown)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func writeField(sum interface{ Write([]byte) (int, error) }, value string) {
	fmt.Fprintf(sum, "%d\n%s", len(value), value)
}

func mustLoadAll() map[platform.Locale]Document {
	loaded := make(map[platform.Locale]Document, len(Locales))
	for _, locale := range Locales {
		loaded[locale] = Document{
			Locale:          locale,
			AcceptanceLabel: mustRead(locale, "label-terms-acceptance.md"),
			BodyMarkdown:    mustRead(locale, "terms.md"),
		}
	}
	return loaded
}

func mustRead(locale platform.Locale, name string) string {
	path := fmt.Sprintf("artifacts/%s/%s", locale, name)
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
