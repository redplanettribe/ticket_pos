// Package service holds the consent module's business rules: which Policy
// Version is current, and what a reader is shown under it.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/policy"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Service is the consent module's business logic.
type Service struct {
	repo   *repository.Repository
	logger platform.Logger
	// now stamps every Consent Record. It is the SERVER's clock and never a
	// client's — a timestamp a browser could name is a timestamp an audit cannot
	// use — and it is a field rather than a call to time.Now so the integration
	// harness can capture evidence at a time it chose.
	now func() time.Time
}

// New builds the consent Service.
func New(repo *repository.Repository, logger platform.Logger) *Service {
	return &Service{repo: repo, logger: logger, now: time.Now}
}

// WithClock replaces the clock every Consent Record is stamped with. Same
// chaining shape as the other services'; used by tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// PolicyView is the current Policy Version as one reader sees it: which edition
// it is, when it took effect, and every word of it in their language.
//
// ONE PAYLOAD FOR TWO SURFACES, deliberately. The Privacy Policy page renders
// `body_markdown`; a capture moment renders `short_notice` and the three
// `consent_labels`. Splitting them into two endpoints would let the page and
// the checkbox beside it come from different editions during a deploy — the
// reader accepting one text while reading another — which is the failure the
// fingerprint exists to make impossible.
//
// The Policy Version's database id is NOT here. #251 records a Consent Record
// against it, and it will read it server-side from the same repository: an
// identifier on the wire would be an identifier a client could send back, and
// which edition somebody accepted is the platform's finding about the moment,
// never the client's assertion about it.
type PolicyView struct {
	// Version is the label a human names this edition by ("0-placeholder").
	Version string `json:"version"`
	// EffectiveDate is the day this edition took effect, YYYY-MM-DD, as the
	// document itself states it.
	EffectiveDate string `json:"effective_date"`
	// ContentHash is the fingerprint of the artifact set below, published so that
	// the page can show it and a reader can hold the platform to it.
	ContentHash string `json:"content_hash"`
	// Locale is the language everything below is written in.
	Locale platform.Locale `json:"locale"`
	// ShortNotice is the condensed notice shown inline at a capture moment,
	// markdown.
	ShortNotice string `json:"short_notice"`
	// ConsentLabels are the three checkbox labels, markdown.
	ConsentLabels policy.ConsentLabels `json:"consent_labels"`
	// BodyMarkdown is the full Privacy Policy, markdown.
	BodyMarkdown string `json:"body_markdown"`
}

// CurrentPolicy reports the Policy Version in effect, rendered in one Locale.
//
// The language is a parameter and not an Accept-Language header, because a
// Locale is a property of a page's address (platform.Locale) and this is the
// text of one page. It is parsed permissively — "es-EC" names Spanish — and
// answered strictly: a language the policy is not published in is refused
// rather than served in another one.
func (s *Service) CurrentPolicy(ctx context.Context, rawLocale string) (PolicyView, error) {
	locale, ok := platform.ParseLocale(rawLocale)
	if !ok {
		return PolicyView{}, consent.ErrPolicyLocaleNotPublished()
	}

	document, ok := policy.For(locale)
	if !ok {
		return PolicyView{}, consent.ErrPolicyLocaleNotPublished()
	}

	version, err := s.repo.CurrentPolicyVersion(ctx)
	if errors.Is(err, repository.ErrNoCurrentPolicyVersion) {
		return PolicyView{}, consent.ErrNoCurrentPolicyVersion()
	}
	if err != nil {
		return PolicyView{}, err
	}

	// The row's fingerprint and the text about to be served disagreeing means
	// the deployed binary is not the one that published this edition — a rolled
	// back backend, or a hand-edited row. It is not fatal: the reader still gets
	// a complete, current notice, and refusing to show a privacy policy is worse
	// for them than showing one whose fingerprint is stale. But it must not pass
	// silently, because every acceptance recorded in this state is evidence
	// pointing at text that was not on screen.
	if version.ContentHash != policy.ContentHash() {
		s.logger.Error("policy version content hash does not match the embedded artifacts",
			"version", version.Label,
			"recorded_hash", version.ContentHash,
			"embedded_hash", policy.ContentHash(),
		)
	}

	return PolicyView{
		Version:       version.Label,
		EffectiveDate: version.EffectiveDate.Format("2006-01-02"),
		ContentHash:   version.ContentHash,
		Locale:        document.Locale,
		ShortNotice:   document.ShortNotice,
		ConsentLabels: document.ConsentLabels,
		BodyMarkdown:  document.BodyMarkdown,
	}, nil
}
