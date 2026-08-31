package service

import (
	"context"
	"errors"

	"github.com/peter/ticket_pos/backend/internal/consent"
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
	// BodyMarkdown is the full Términos y Condiciones, markdown.
	BodyMarkdown string `json:"body_markdown"`
}

// CurrentTermsVersionID names the edition in effect, and nothing else about
// it. It exists for the checkout's held Terms answer (#537): the surface that
// shows the box snapshots which edition it showed, so the capture minutes
// later evidences that edition rather than whichever is current by then. The
// finding stays this module's; the caller only carries it.
func (s *Service) CurrentTermsVersionID(ctx context.Context) (string, error) {
	version, err := s.repo.CurrentTermsVersion(ctx)
	if errors.Is(err, repository.ErrNoCurrentTermsVersion) {
		return "", consent.ErrNoCurrentTermsVersion()
	}
	if err != nil {
		return "", err
	}
	return version.ID, nil
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

	document, ok := terms.For(locale)
	if !ok {
		return TermsView{}, consent.ErrTermsLocaleNotPublished()
	}

	version, err := s.repo.CurrentTermsVersion(ctx)
	if errors.Is(err, repository.ErrNoCurrentTermsVersion) {
		return TermsView{}, consent.ErrNoCurrentTermsVersion()
	}
	if err != nil {
		return TermsView{}, err
	}

	// A mismatch means the deployed binary is not the one that published this
	// edition. Not fatal, for CurrentPolicy's reason — the reader still gets the
	// current text — but never silent, because every acceptance recorded in this
	// state is evidence pointing at text that was not on screen.
	if version.ContentHash != terms.ContentHash() {
		s.logger.Error("terms version content hash does not match the embedded artifacts",
			"version", version.Label,
			"recorded_hash", version.ContentHash,
			"embedded_hash", terms.ContentHash(),
		)
	}

	return TermsView{
		Version:         version.Label,
		EffectiveDate:   version.EffectiveDate.Format("2006-01-02"),
		ContentHash:     version.ContentHash,
		Locale:          document.Locale,
		AcceptanceLabel: document.AcceptanceLabel,
		BodyMarkdown:    document.BodyMarkdown,
	}, nil
}
