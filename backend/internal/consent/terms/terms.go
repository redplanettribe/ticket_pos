// Package terms is the shape of the Términos y Condiciones Generales: what a
// reader can be shown under one edition, and which stored artifact fills each
// part of it.
//
// It is the Terms' counterpart of internal/consent/policy, and everything that
// package's doc comment argues holds here unchanged — including that the text
// no longer lives in this binary. The body and the acceptance label are rows
// now (terms_version_artifacts, migration 109); this package turns an edition's
// rows into the document a surface renders.
//
// It is a SIBLING of the policy package, not a client of it, and the two still
// share no content: the Terms and the Privacy Policy version independently (ADR
// 0066), an edition of one never re-gates the other, and neither package can
// reach the other's artifacts. What they do share is the framing of the
// fingerprint, which is one rule and lives in internal/consent/legal.
//
// PUBLISHED IN BOTH LOCALES, WITH SPANISH PREVAILING. The Terms' own §37 makes
// the Spanish text prevail over any translation, and the English artifact says
// so in its own first line: it is a courtesy translation, published so that an
// English reader knows what they are ticking, and it never becomes the
// operative text. That is a statement about which words WIN, not about which
// words are PUBLISHED — so the lookup is per-Locale like the policy's, an
// unpublished Locale is refused rather than answered in a language nobody asked
// for, and both translations belong to one edition under one fingerprint. WHICH
// languages an edition publishes is now read from that edition's own rows: an
// edition published without its translation stops serving English the moment it
// becomes current, with no deploy either way.
//
// The text is authored ONE LINE PER BLOCK, for the policy package's reason: the
// Storefront renders it through the Markdown component that turns a wrapped
// line into a <br>, and the rendering is the thing being attested to.
package terms

import (
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The artifact slugs one Terms Version publishes, in each language. The
// ordinals that decide the fingerprint are on the rows, not here.
const (
	// SlugAcceptanceLabel is the mandatory, un-premarked checkbox's label (§3).
	SlugAcceptanceLabel = "label-terms-acceptance"
	// SlugTerms is the full Términos y Condiciones, the public page's body.
	SlugTerms = "terms"
)

// PrevailingLocale is the language whose words are the contract: Spanish, over
// any translation (§37).
//
// Named here rather than assumed at each call site, because several places need
// to point AT the operative text specifically — the Staff app's link out to the
// public terms page among them — and "es" spelled in each of them is a fact
// about the document that the document's own package should own.
const PrevailingLocale = platform.LocaleES

// Document is one Locale's rendering of a Terms Version: everything a reader
// can be shown, and nothing else. Its fields are exactly the artifacts the
// fingerprint covers (policy.Document's rule, for its reason).
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

// DocumentFrom assembles one language's Document out of an edition's stored
// artifacts, reporting whether that edition publishes that language completely.
//
// It never falls back, matching policy.DocumentFrom: a contract the reader
// cannot read must never be presented as the one they accepted, and a
// mandatory checkbox with a blank label beside it is the one thing §3 forbids
// outright. Both published Locales carry the same edition, so the choice is
// which words a reader is shown, never which contract binds them.
func DocumentFrom(locale platform.Locale, artifacts []legal.Artifact) (Document, bool) {
	document := Document{Locale: locale}
	found := 0
	for _, artifact := range artifacts {
		if artifact.Locale != locale {
			continue
		}
		switch artifact.Slug {
		case SlugAcceptanceLabel:
			document.AcceptanceLabel = artifact.Body
		case SlugTerms:
			document.BodyMarkdown = artifact.Body
		default:
			continue
		}
		found++
	}
	if found != 2 {
		return Document{}, false
	}
	return document, true
}

// Documents assembles every language an edition publishes completely, keyed by
// Locale: what one cache fill produces, from one read.
func Documents(artifacts []legal.Artifact) map[platform.Locale]Document {
	documents := make(map[platform.Locale]Document, 2)
	for _, locale := range legal.Locales(artifacts) {
		if document, ok := DocumentFrom(locale, artifacts); ok {
			documents[locale] = document
		}
	}
	return documents
}
