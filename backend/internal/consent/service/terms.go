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
// is, when it took effect, and every word of it.
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
	// Locale is the language the text below is written in — always "es", the
	// single legally prevailing text (§37), whatever language was asked for.
	Locale platform.Locale `json:"locale"`
	// AcceptanceLabel is the mandatory checkbox's label, markdown. The UI may
	// not reword or pre-tick it.
	AcceptanceLabel string `json:"acceptance_label"`
	// BodyMarkdown is the full Términos y Condiciones, markdown.
	BodyMarkdown string `json:"body_markdown"`
}

// CurrentTerms reports the Terms Version in effect.
//
// Unlike CurrentPolicy it takes no Locale and refuses nobody: the Terms are
// published in Spanish only, the Spanish text prevails over any translation
// (§37), and serving it under every Locale is the ruling of #533 — an English
// reader gets the one legally operative document rather than a 404. The
// handler still binds a {locale} path parameter so the address shape matches
// the policy's; whatever it says, this is the answer.
func (s *Service) CurrentTerms(ctx context.Context) (TermsView, error) {
	document := terms.Current()

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
