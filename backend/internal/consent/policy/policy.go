// Package policy is the shape of the Privacy Policy: what a reader can be
// shown under one edition, and which stored artifact fills each part of it.
//
// THE TEXT NO LONGER LIVES HERE. Until #558 the policy body, the Short Notice
// and the three checkbox labels were Markdown files compiled into this binary
// (ADR 0036), and this package read them at startup. They are rows now
// (policy_version_artifacts, migration 109), and this package is what turns an
// edition's rows back into the document a surface renders.
//
// The move does not weaken the argument ADR 0036 made; it satisfies it
// somewhere else. What that ADR was defending was that the text a Policy
// Version's SHA-256 is taken over must be the text the reader is actually
// served — not a copy of it in a message catalog on its own deploy cadence,
// which nothing could keep honest. That property is now held by the DATABASE:
// the endpoint serves these rows, the fingerprint is recomputed over these rows
// on every cache fill, and migration 109 proved the rows against the hash every
// existing acceptance already points at. What the embedded files bought and the
// rows do not is a BUILD-TIME failure, and it bought less than it looked like —
// the seed drift test only ever guarded the newest edition, which is how a
// published edition's hash came to be rewritten three times without CI
// noticing. It is replaced by a stronger test over every edition
// (internal/consent/legal).
//
// What stays true, and is the reason this package still exists: the Storefront
// keeps its half of ADR 0027. The page's chrome — its heading, its "last
// updated" label, its link text — is catalog copy like everything else, and
// only the policy body, the Short Notice and the three checkbox labels are
// evidence served from here.
//
// EVERY ARTIFACT OF AN EDITION IS SERVED, and that invariant is what makes the
// hash honest: a slug this package does not render is bytes inside the
// fingerprint that no reader can be shown, so DocumentFrom refuses an edition
// that is missing one and nothing silently ignores an extra.
//
// The text is authored ONE LINE PER BLOCK — a paragraph, a list item or a table
// row is a single long line, never hard-wrapped. The Storefront renders it
// through @ticket-pos/ui's Markdown component, which runs remark-breaks and
// turns a wrapped line into a <br>; a legal document typeset with a break every
// 79 characters is a rendering artifact in a document whose rendering is the
// thing being attested to.
package policy

import (
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The artifact slugs one Policy Version publishes, in each language. The
// ORDINALS that decide the fingerprint are on the rows and not here: an
// operator adding an artifact must not need a deploy to change how an edition
// is hashed (legal.ContentHash).
const (
	// SlugShortNotice is the condensed notice shown inline above the consent
	// boxes at a capture moment.
	SlugShortNotice = "short-notice"
	// SlugPolicyAcceptanceLabel is the required checkbox's label.
	SlugPolicyAcceptanceLabel = "label-policy-acceptance"
	// SlugMarketingConsentLabel is the optional marketing checkbox's label.
	SlugMarketingConsentLabel = "label-marketing-consent"
	// SlugNetworkingConsentLabel is the optional networking checkbox's label.
	SlugNetworkingConsentLabel = "label-networking-consent"
	// SlugPolicy is the full Privacy Policy, the public page's body.
	SlugPolicy = "policy"
)

// MandatoryLocale is the language this notice MUST be published in: Spanish,
// because the Ley Orgánica de Protección de Datos Personales requires the notice
// to be given in it (#563).
//
// IT IS NOT terms.PrevailingLocale, AND THE TWO NAMES ARE THE POINT. They hold
// the same value today and rest on entirely different footings, and a shared
// name would flatten the stronger one into the weaker:
//
//   - "Prevailing" is a device for resolving a DISCREPANCY BETWEEN TWO TEXTS OF
//     ONE AGREEMENT — the Terms' own §37, a clause the Foundation wrote and
//     could amend tomorrow. It says which words WIN, not which words must exist.
//   - "Mandatory" here is a STATUTORY DUTY about a NOTICE, which is not an
//     agreement and has no second text to prevail over. Nobody at this platform
//     can amend it.
//
// So the Policy's guard is the stronger of the two, and the two documents can
// publish different language sets without either borrowing the other's reason.
//
// A CONSTANT, NEVER A COLUMN. A column is settable, so "unpublish Spanish" would
// become "set the column, then unpublish Spanish" — through the same editor, by
// the same one person, with no second pair of eyes anywhere in the platform
// (production holds ONE platform_operators row). As a constant it costs a code
// change, a review and a manual deploy, which is the strongest guard this design
// has.
//
// CONSULTED ONLY AT PUBLISH, never when validating history. A future change to
// the duty must not retroactively invalidate editions published honestly under
// the old one, so nothing that reads a published edition asks this question.
const MandatoryLocale = platform.LocaleES

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

// Document is one Locale's rendering of a Policy Version: everything a reader
// can be shown, and nothing else.
//
// Its fields are exactly the artifacts the fingerprint is taken over. A field
// here that no artifact fills would be a document with a hole in it; an
// artifact no field reads would be bytes in the fingerprint that no page can be
// checked against. Either one breaks the only property this whole arrangement
// exists to have, which is why DocumentFrom insists on the full set.
type Document struct {
	Locale        platform.Locale `json:"locale"`
	ShortNotice   string          `json:"short_notice"`
	ConsentLabels ConsentLabels   `json:"consent_labels"`
	BodyMarkdown  string          `json:"body_markdown"`
}

// DocumentFrom assembles one language's Document out of an edition's stored
// artifacts, reporting whether that edition publishes that language COMPLETELY.
//
// It never falls back to another language. A Locale an edition does not publish
// is answered with nothing — serving the document in a language the reader did
// not ask for would present a notice they may not be able to read as the notice
// they accepted — and an edition publishing a language with a missing artifact
// is treated as not publishing it at all, because a capture moment with a blank
// checkbox label beside it is worse than an honest 404.
func DocumentFrom(locale platform.Locale, artifacts []legal.Artifact) (Document, bool) {
	document := Document{Locale: locale}
	found := 0
	for _, artifact := range artifacts {
		if artifact.Locale != locale {
			continue
		}
		switch artifact.Slug {
		case SlugShortNotice:
			document.ShortNotice = artifact.Body
		case SlugPolicyAcceptanceLabel:
			document.ConsentLabels.PolicyAcceptance = artifact.Body
		case SlugMarketingConsentLabel:
			document.ConsentLabels.MarketingConsent = artifact.Body
		case SlugNetworkingConsentLabel:
			document.ConsentLabels.NetworkingConsent = artifact.Body
		case SlugPolicy:
			document.BodyMarkdown = artifact.Body
		default:
			// An artifact this binary does not render. It is inside the
			// fingerprint, so it is not ignorable — but it is also not this
			// function's to refuse: the service logs it, once per cache fill,
			// where a logger exists.
			continue
		}
		found++
	}
	if found != 5 {
		return Document{}, false
	}
	return document, true
}

// Documents assembles every language an edition publishes completely, keyed by
// Locale.
//
// This is what one cache fill produces: every language of one edition, built
// from ONE read, so that the text a surface renders and the fingerprint an
// acceptance records can never come from two different reads of the table.
func Documents(artifacts []legal.Artifact) map[platform.Locale]Document {
	documents := make(map[platform.Locale]Document, 2)
	for _, locale := range legal.Locales(artifacts) {
		if document, ok := DocumentFrom(locale, artifacts); ok {
			documents[locale] = document
		}
	}
	return documents
}
