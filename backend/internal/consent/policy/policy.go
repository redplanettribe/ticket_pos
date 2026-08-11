// Package policy holds the Privacy Policy and its Short Notice as embedded
// per-Locale documents, and computes the fingerprint a Policy Version is
// recorded under.
//
// THE ARTIFACT LIVES HERE AND NOT IN THE STOREFRONT CATALOG, and that is the
// whole design. A Policy Version records the SHA-256 "of the exact rendered
// notice + policy text" (#249) so that a compliance officer can prove what a
// person was shown when they accepted. A hash can only prove that if the bytes
// it was taken over are the same bytes that reached the reader. Copy sitting in
// `apps/storefront/messages/{en,es}.json` is compiled into a separate runtime,
// deployed on its own cadence, and reachable from no Go test — the hash would
// have been of a file nobody could show a court, and nothing would notice when
// the two drifted. Embedded here, the text the endpoint serves, the text the
// page renders and the text the hash covers are one thing, and
// `seed_test.go` fails the build the moment they stop being one thing.
//
// ADR 0036 records this decision. It is a deliberate, narrow exception to ADR
// 0027, which holds that the API
// stays Locale-unaware and that words for machine-keyed things live in the
// Storefront catalog. That rule is about COPY — words this product chooses for
// concepts the database owns, where the worst case of drift is an English chip
// in a Spanish page. This is not copy, it is EVIDENCE: a legal artifact whose
// integrity is the feature, whose editions are published rather than restyled,
// and which is quoted back in an audit. The Storefront keeps its half of ADR
// 0027 — the page's chrome (its heading, its "last updated" label, its link
// text) is catalog copy like everything else, and only the policy body, the
// Short Notice and the three checkbox labels come from here.
//
// EVERY EMBEDDED FILE IS SERVED, and that invariant is what makes the hash
// honest. There is no artifact here that a reader cannot reach, so no edit can
// change the fingerprint without changing what somebody sees, and no edit can
// change what somebody sees without changing the fingerprint.
//
// The files are authored ONE LINE PER BLOCK — a paragraph, a list item or a
// table row is a single long line, never hard-wrapped. The Storefront renders
// them through `@ticket-pos/ui`'s Markdown component, which runs remark-breaks
// and turns a wrapped line into a `<br>`; a legal document typeset with a break
// every 79 characters is a rendering artifact in a document whose rendering is
// the thing being attested to. Long lines in the source are the price of the
// served text being exactly the authored text.
package policy

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// artifacts holds the Privacy Policy artifact set: one directory per Locale,
// each with the policy body, the Short Notice, and the three consent labels.
//
//go:embed artifacts
var artifacts embed.FS

// ConsentLabels are the three checkbox labels shown at a capture moment, in one
// Locale.
//
// Three, and the shape says which is which rather than a list saying so at
// runtime: the required one and the two optional ones are different kinds of
// thing, not three rows of a table (ADR 0034, ADR 0035). A capture surface
// decides which boxes to SHOW from the Customer's recorded answers; it never
// has to decide what a box MEANS.
type ConsentLabels struct {
	// PolicyAcceptance is the required box. Without it no Customer Session is
	// established and no Online Sale completes.
	PolicyAcceptance string `json:"policy_acceptance"`
	// MarketingConsent is the optional marketing box, and it names the weekly
	// Follow Digest out loud because granting it turns the Digest on (ADR 0034).
	MarketingConsent string `json:"marketing_consent"`
	// NetworkingConsent is the optional networking box, and it names both
	// audiences — other attendees of the same event, and that event's organizers
	// — because those are the two the authorization actually covers.
	NetworkingConsent string `json:"networking_consent"`
}

// Document is one Locale's rendering of the current Policy Version: everything
// a reader can be shown, and nothing else.
//
// Its fields ARE the hash preimage (see ContentHash). A field added here that a
// reader never sees would put bytes in the fingerprint that no page can be
// checked against; a field a reader sees that is not here is text the
// fingerprint does not cover. Either one breaks the only property this whole
// arrangement exists to have.
type Document struct {
	Locale        platform.Locale `json:"locale"`
	ShortNotice   string          `json:"short_notice"`
	ConsentLabels ConsentLabels   `json:"consent_labels"`
	BodyMarkdown  string          `json:"body_markdown"`
}

// Locales are the Locales the Privacy Policy is published in, in the order the
// fingerprint walks them.
//
// Fixed, and not derived from the embedded directory listing: the order decides
// the hash, and a hash that depended on how a filesystem sorted names would be
// a hash that could change without the text changing.
var Locales = []platform.Locale{platform.LocaleEN, platform.LocaleES}

// documents is the artifact set, read once at startup. Reading the embedded
// files is infallible in a binary that compiled, so a missing or empty artifact
// panics here rather than serving a Customer a blank notice.
var documents = mustLoadAll()

// For reports the current Policy Version's artifact in one Locale, reporting
// whether the platform publishes that Locale at all.
//
// It never falls back to English. A Locale this platform does not serve is a
// caller's mistake, and answering it with a document in another language would
// be a notice the reader cannot read presented as the notice they accepted.
func For(locale platform.Locale) (Document, bool) {
	doc, ok := documents[locale]
	return doc, ok
}

// ContentHash is the SHA-256 of the whole artifact set, hex-encoded: the value a
// Policy Version row carries, and the thing #249's evidence requirement rests
// on.
//
// It is taken over the SERVED VALUES rather than over the raw files, because
// what has to be provable is what a reader was shown, not how the repository
// stored it. Trimming, and any future normalisation, therefore happens before
// the hash rather than after it.
//
// It covers EVERY Locale at once, because a Policy Version is one published
// edition of the policy and not one per language: a Customer accepts the
// edition, in whichever language they read it, and an edit to the Spanish text
// alone is still a new edition. Per-Locale hashes would let the two languages
// drift into separate editions under one version label, which is the exact
// thing the label exists to prevent.
//
// The preimage is length-framed. Concatenating "abc" + "de" and "ab" + "cde"
// gives the same bytes and would give the same hash, so every field is written
// as its byte length, a newline, and the field — moving a sentence from the
// Short Notice into a label cannot leave the fingerprint untouched.
func ContentHash() string {
	sum := sha256.New()
	for _, locale := range Locales {
		doc := documents[locale]
		writeField(sum, string(locale))
		writeField(sum, doc.ShortNotice)
		writeField(sum, doc.ConsentLabels.PolicyAcceptance)
		writeField(sum, doc.ConsentLabels.MarketingConsent)
		writeField(sum, doc.ConsentLabels.NetworkingConsent)
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
			Locale:       locale,
			ShortNotice:  mustRead(locale, "short-notice.md"),
			BodyMarkdown: mustRead(locale, "policy.md"),
			ConsentLabels: ConsentLabels{
				PolicyAcceptance:  mustRead(locale, "label-policy-acceptance.md"),
				MarketingConsent:  mustRead(locale, "label-marketing-consent.md"),
				NetworkingConsent: mustRead(locale, "label-networking-consent.md"),
			},
		}
	}
	return loaded
}

func mustRead(locale platform.Locale, name string) string {
	path := fmt.Sprintf("artifacts/%s/%s", locale, name)
	raw, err := artifacts.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("policy: read embedded artifact %s: %v", path, err))
	}
	// Trailing newlines are a text-editor convention, not part of the notice.
	// Trimming here rather than at render time keeps the hash over what is
	// served: see ContentHash.
	text := strings.TrimSpace(string(raw))
	if text == "" {
		panic(fmt.Sprintf("policy: embedded artifact %s is empty", path))
	}
	return text
}
