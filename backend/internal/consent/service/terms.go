package service

import (
	"context"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// TermsView is the current Terms Version as a reader sees it: which edition it
// is, when it took effect, and every word of it in one language.
//
// One payload for every surface, like PolicyView and for its reason: the terms
// page renders `body_markdown`, a capture moment renders `acceptance_label`,
// and splitting them would let the page and the checkbox beside it come from
// different editions during a deploy.
//
// The Terms Version's database id is NOT here, matching the Policy Version
// rule: which edition somebody accepted is the platform's finding about the
// moment, never the client's assertion about it.
type TermsView struct {
	// Version is the label a human names this edition by ("1").
	Version string `json:"version"`
	// EffectiveDate is the day this edition took effect, YYYY-MM-DD.
	EffectiveDate string `json:"effective_date"`
	// ContentHash is the fingerprint of the artifact set below, published so a
	// reader can hold the platform to it.
	ContentHash string `json:"content_hash"`
	// Locale is the language the text below is written in, and the language that
	// was asked for: both published languages carry this edition. Spanish is the
	// legally prevailing text (§37); English is the courtesy translation, and it
	// says so in its own first line.
	Locale platform.Locale `json:"locale"`
	// AcceptanceLabel is the mandatory checkbox's label, markdown. The UI may
	// not reword or pre-tick it.
	AcceptanceLabel string `json:"acceptance_label"`
	// AdulthoodDeclarationLabel is the Adulthood Declaration's own mandatory,
	// un-premarked checkbox label, markdown — ABSENT FROM THE PAYLOAD when the
	// edition in effect does not carry the Artifact (ADR 0069).
	//
	// A surface draws the second box iff this field arrives, which is why it is
	// omitempty rather than an empty string: "this edition does not ask" and
	// "this edition asks with nothing written beside the box" must not look the
	// same on the wire. The current edition carries no such Artifact, so today
	// this field is simply not there.
	AdulthoodDeclarationLabel string `json:"adulthood_declaration_label,omitempty"`
	// BodyMarkdown is the full Términos y Condiciones, markdown.
	BodyMarkdown string `json:"body_markdown"`
}

// CurrentTermsVersionID names the edition in effect, and nothing else about
// it. It exists for the checkout's held Terms answer (#537): the surface that
// shows the box snapshots which edition it showed, so the capture minutes
// later evidences that edition rather than whichever is current by then. The
// finding stays this module's; the caller only carries it.
func (s *Service) CurrentTermsVersionID(ctx context.Context) (string, error) {
	// FROM THE SAME CACHED EDITION THE TEXT WAS SERVED FROM, and never a second
	// read of the version row (#558). The surface calling this has just rendered
	// the acceptance label out of currentTermsEdition; a fresh read here could
	// answer about the edition that became current in between, snapshotting an
	// edition nobody was shown — the very failure the held id exists to prevent.
	edition, err := s.currentTermsEdition(ctx)
	if err != nil {
		return "", err
	}
	return edition.version.ID, nil
}

// CurrentTerms reports the Terms Version in effect, rendered in one Locale.
//
// It answers the Locale STRICTLY, exactly as CurrentPolicy does: a language
// this platform does not publish the Terms in is a 404, never a silent
// fallback. Serving a contract in a language the reader did not ask for, under
// their own language's address, would present text they may not be able to
// read as the text they accepted.
//
// WHICH LANGUAGE IS SHOWN IS NOT WHICH TEXT BINDS. Both published translations
// are one edition under one fingerprint (terms.ContentHash), and the Spanish
// text prevails over any translation (§37) — the English document says so in
// its own first line. An acceptance captured beside the English label is
// therefore an acceptance of the same edition, evidenced by the same hash, as
// one captured beside the Spanish.
func (s *Service) CurrentTerms(ctx context.Context, rawLocale string) (TermsView, error) {
	locale, ok := platform.ParseLocale(rawLocale)
	if !ok {
		return TermsView{}, consent.ErrTermsLocaleNotPublished()
	}

	edition, err := s.currentTermsEdition(ctx)
	if err != nil {
		return TermsView{}, err
	}

	view, published := edition.views[locale]
	if !published {
		return TermsView{}, consent.ErrTermsLocaleNotPublished()
	}
	return view, nil
}

// currentTermsEdition is THE read of the Terms' current edition — text,
// fingerprint and the version row's id — served from the cache and filled from
// one query when it is cold. currentPolicyEdition's shape, for its reasons:
// every path goes through here so that what was shown and what is recorded are
// two fields of one read (#558).
func (s *Service) currentTermsEdition(ctx context.Context) (termsEdition, error) {
	if cached, ok := s.termsCache.load(time.Now()); ok {
		return cached, nil
	}
	read, err := s.repo.CurrentTermsEdition(ctx)
	if errors.Is(err, repository.ErrNoCurrentTermsVersion) {
		return termsEdition{}, consent.ErrNoCurrentTermsVersion()
	}
	if err != nil {
		return termsEdition{}, err
	}
	edition := s.termsEditionFrom(read)
	s.termsCache.store(edition, time.Now())
	return edition, nil
}

// termsEdition is one filled cache entry: which edition row it is, and the
// answer for every language it publishes. The id stays off TermsView for the
// reason it stays off PolicyView — see policyEdition.
type termsEdition struct {
	version repository.TermsVersion
	views   map[platform.Locale]TermsView
}

// asksAdulthoodDeclaration reports whether this edition draws the Adulthood
// Declaration box at all: whether it carries the
// `label-adulthood-declaration` Artifact (#586, ADR 0069).
//
// IT IS A FACT ABOUT THE EDITION AND NOT ABOUT THE READER, so it is answered
// from the PREVAILING Locale's document and never from whichever language a
// particular surface happens to be rendering. The Spanish text is the contract
// (§37) and the English one is the courtesy translation that says so in its own
// first line, so "does this edition ask?" is a question about the operative
// text — and asking it per-Locale would make the box owed to a Spanish reader
// and not to an English one, which is a gate that varies by language.
//
// The corollary is worth stating, because it is the failure mode: an edition
// published carrying the Artifact in Spanish but NOT in English owes the box to
// everybody and can word it for only half of them, and the English reader's
// surface has no label to draw. That is an incomplete publish — the Legal
// Draft authors both languages in parallel columns for exactly this reason —
// and it fails LOUDLY, with a person unable to finish a sign-in, rather than
// quietly signing them in without a declaration. Loud is the right side of that
// trade: the alternative silently drops a required box.
func (e termsEdition) asksAdulthoodDeclaration() bool {
	return e.views[terms.PrevailingLocale].AdulthoodDeclarationLabel != ""
}

// termsEditionFrom turns one read of one edition into the cache entry, verifying
// the fingerprint on the way — policyEditionFrom's shape, for its reasons.
// Called once per cache fill.
func (s *Service) termsEditionFrom(edition repository.TermsEdition) termsEdition {
	version := edition.Version

	// Never silent, never fatal: the reader gets the current contract, and the
	// mismatch is logged because every acceptance recorded in this state points
	// at text that was not on screen (policyEditionFrom).
	if computed := legal.ContentHash(edition.Artifacts); computed != version.ContentHash {
		s.logger.Error("terms version content hash does not match its stored artifacts",
			"version", version.Label,
			"recorded_hash", version.ContentHash,
			"stored_hash", computed,
		)
	}

	documents := terms.Documents(edition.Artifacts)
	views := make(map[platform.Locale]TermsView, len(documents))
	for locale, document := range documents {
		views[locale] = TermsView{
			Version:         version.Label,
			EffectiveDate:   version.EffectiveDate.Format("2006-01-02"),
			ContentHash:     version.ContentHash,
			Locale:          document.Locale,
			AcceptanceLabel: document.AcceptanceLabel,
			// Empty when this edition does not ask, and carried straight through
			// when it does: what to show is a fact about the edition in effect,
			// derived here rather than configured (ADR 0069).
			AdulthoodDeclarationLabel: document.AdulthoodDeclarationLabel,
			BodyMarkdown:              document.BodyMarkdown,
		}
	}

	if missing := len(legal.Locales(edition.Artifacts)) - len(views); missing > 0 {
		s.logger.Error("terms version publishes a language with an incomplete artifact set",
			"version", version.Label,
			"incomplete_languages", missing,
		)
	}
	return termsEdition{version: version, views: views}
}
