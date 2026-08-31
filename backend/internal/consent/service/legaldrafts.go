package service

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Legal Center's drafting half (#561, spec #556): what an operator opens,
// saves and discards while writing the next edition of a legal document.
//
// IT LIVES IN THE CONSENT MODULE AND NOT IN THE OPERATOR ONE. The operator
// package owns no tables and never has (ADR 0015); it composes what other
// modules answer for. The Privacy Policy and the Términos y Condiciones are
// this module's documents, the preimage rule is this module's rule, and a draft
// is the thing that becomes an edition — so the operator surface asks here, the
// way it asks customers about a Customer's consents.
//
// NOTHING HERE PUBLISHES. Saving a draft writes no version row, no artifact
// row, no fingerprint and no evidence, and it invalidates no cache, because
// nothing a reader can see has changed. Publishing is #563.

// LegalDocumentPolicy and LegalDocumentTerms are the two documents the Legal
// Center can draft. They are the values of the `document` primary key on
// `legal_drafts`, and the only ones the API accepts.
const (
	LegalDocumentPolicy = "policy"
	LegalDocumentTerms  = "terms"
)

// legalDraftLocales is the menu of languages a draft may publish in: the
// platform's app locales, in preimage order.
//
// BOUNDED, and bounded HERE rather than by a CHECK constraint or by whatever
// the operator types. `platform.ParseLocale` is a closed switch, every mail
// template and every Storefront route is written in exactly these two
// languages, and the preimage orders locales by ascending code. A third
// language is not a row somebody adds in the Legal Center — it is a project
// (#281), and letting an operator half-create one here would publish an edition
// in a language no surface can render.
var legalDraftLocales = []platform.Locale{platform.LocaleEN, platform.LocaleES}

// OperatorLegalArtifact is one artifact across every language it is written in:
// the row of the editor's grid.
//
// THE TRANSPOSE HAPPENS HERE AND NOWHERE ELSE. Storage is one row per
// (artifact, language), because that is what the preimage hashes and what the
// published tables hold. The editor is a grid of artifacts by language, because
// that is what a person editing two columns in parallel is looking at. Somebody
// has to turn one into the other; doing it at this seam means the browser never
// holds the storage shape and the database never holds the editing shape.
type OperatorLegalArtifact struct {
	Slug string `json:"slug"`
	// Ordinal is the artifact's position in the fingerprint preimage. It is
	// derived from the order of this slice and never sent by the client — see
	// SaveLegalDraftInput.
	Ordinal int `json:"ordinal"`
	// Bodies maps a language token to the text written in it. A language with
	// no entry is a cell nobody has written.
	Bodies map[string]string `json:"bodies"`
}

// OperatorLegalEdition is the currently published edition of one document, as
// the Legal Center shows it: what the draft is being written against, and what
// discarding the draft returns to.
type OperatorLegalEdition struct {
	VersionID string `json:"version_id"`
	Label     string `json:"label"`
	// EffectiveDate is the day this edition took effect, YYYY-MM-DD.
	EffectiveDate string `json:"effective_date"`
	ContentHash   string `json:"content_hash"`
	// Locales is the languages this edition actually publishes, read from its
	// artifact rows and from no compile-time list (#558).
	Locales   []string                `json:"locales"`
	Artifacts []OperatorLegalArtifact `json:"artifacts"`
}

// OperatorLegalDraft is the one mutable draft of a document.
type OperatorLegalDraft struct {
	// Stored says whether this draft has ever been saved. A draft that has not
	// is a copy of the published edition, handed back so the editor always has
	// something to open and so "discard" and "never started" are the same
	// screen. It is reported rather than hidden because the editor says
	// "unsaved changes" about one and not the other.
	Stored bool `json:"stored"`
	// BaseVersionID is the edition this draft was opened from.
	BaseVersionID string `json:"base_version_id"`
	// BaseIsCurrent is false when the published edition has moved on since the
	// draft was started — somebody else published while it sat. The editor
	// warns; nothing here refuses, because the draft is still the operator's
	// work and only publishing has to care.
	BaseIsCurrent bool `json:"base_is_current"`
	// PublishedLocales is the explicit set of languages this draft intends to
	// publish in. Never inferred from which cells are filled.
	PublishedLocales []string                `json:"published_locales"`
	Artifacts        []OperatorLegalArtifact `json:"artifacts"`
	// UpdatedBy and UpdatedAt are absent on a draft that was never saved.
	UpdatedBy string     `json:"updated_by"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// OperatorLegalWorkspace is everything the Legal Center's editor needs for one
// document in ONE read: what is published, what is being drafted, and the menu
// of languages a draft may publish in.
//
// One payload rather than three endpoints, on CurrentPolicyEdition's reasoning
// one floor down: the editor compares the draft against the published edition
// cell by cell, and two reads could straddle a publication and produce a diff
// against text that was never on screen together.
type OperatorLegalWorkspace struct {
	Document string `json:"document"`
	// SupportedLocales is the menu, in preimage order — the bound on the
	// published-language set, sent so the editor does not hard-code it.
	SupportedLocales []string             `json:"supported_locales"`
	Published        OperatorLegalEdition `json:"published"`
	Draft            OperatorLegalDraft   `json:"draft"`
}

// SaveLegalDraftInput is a whole draft, as the operator has it on screen.
//
// WHOLE, never a patch. See repository.SaveLegalDraft for why.
type SaveLegalDraftInput struct {
	// PublishedLocales is the language set, bounded by legalDraftLocales.
	PublishedLocales []string
	// Artifacts is the slug list IN ORDER. The order is the ordinal — the
	// client does not send ordinals and could not be trusted with them, because
	// the ordinal is the fingerprint preimage's order (#541) and a client that
	// sent a list whose ordinals disagreed with its order would publish an
	// edition hashed differently from the one it displayed.
	Artifacts []SaveLegalDraftArtifact
	// UpdatedBy is taken from the Staff Session by the handler, never from the
	// body.
	UpdatedBy string
}

// SaveLegalDraftArtifact is one row of the editor's grid on its way back.
type SaveLegalDraftArtifact struct {
	Slug string
	// Bodies maps a language token to its text. A language absent, or present
	// and blank, is a cell nobody has written: both are stored as no row, so
	// that "empty" and "missing" cannot drift apart between the browser and the
	// completeness rule.
	Bodies map[string]string
}

// LegalWorkspace reads one document's published edition and its draft together.
func (s *Service) LegalWorkspace(ctx context.Context, document string) (*OperatorLegalWorkspace, error) {
	document, err := parseLegalDocument(document)
	if err != nil {
		return nil, err
	}

	published, err := s.publishedEdition(ctx, document)
	if err != nil {
		return nil, err
	}

	stored, err := s.repo.LegalDraft(ctx, document)
	switch {
	case errors.Is(err, repository.ErrNoLegalDraft):
		// No draft: hand back the published edition as one. This is the same
		// answer a discard produces, deliberately — "start again from the
		// current edition" and "never started" are one state, so there is no
		// third thing for the editor to render.
		return &OperatorLegalWorkspace{
			Document:         document,
			SupportedLocales: localeTokens(legalDraftLocales),
			Published:        published,
			Draft: OperatorLegalDraft{
				Stored:           false,
				BaseVersionID:    published.VersionID,
				BaseIsCurrent:    true,
				PublishedLocales: published.Locales,
				Artifacts:        published.Artifacts,
			},
		}, nil
	case err != nil:
		return nil, err
	}

	updatedAt := stored.UpdatedAt
	return &OperatorLegalWorkspace{
		Document:         document,
		SupportedLocales: localeTokens(legalDraftLocales),
		Published:        published,
		Draft: OperatorLegalDraft{
			Stored:           true,
			BaseVersionID:    stored.BaseVersionID,
			BaseIsCurrent:    stored.BaseVersionID == published.VersionID,
			PublishedLocales: localeTokens(stored.PublishedLocales),
			Artifacts:        groupArtifacts(stored.Artifacts),
			UpdatedBy:        stored.UpdatedBy,
			UpdatedAt:        &updatedAt,
		},
	}, nil
}

// SaveLegalDraft replaces a document's draft with what the operator has on
// screen and answers with the workspace as it now stands.
//
// IT VALIDATES SHAPE AND NOT READINESS. A language outside the menu, a blank
// slug, the same slug twice, an empty language set — each is a request the
// platform cannot store coherently, so each is refused. An artifact missing its
// Spanish, on the other hand, is SAVED: that is an afternoon's work in
// progress, and refusing it would mean the operator's only way to keep a
// half-translated draft was to leave the tab open. Publish is where
// incompleteness is refused (#563).
func (s *Service) SaveLegalDraft(ctx context.Context, document string, input SaveLegalDraftInput) (*OperatorLegalWorkspace, error) {
	document, err := parseLegalDocument(document)
	if err != nil {
		return nil, err
	}

	locales, err := parseDraftLocales(input.PublishedLocales)
	if err != nil {
		return nil, err
	}

	artifacts, err := draftArtifactRows(input.Artifacts)
	if err != nil {
		return nil, err
	}

	published, err := s.publishedEdition(ctx, document)
	if err != nil {
		return nil, err
	}

	// The base is whatever is published NOW when a draft is first saved, and
	// whatever it already was when one is saved again. Re-basing a draft on
	// every save would silently swallow the very fact the base exists to
	// surface: that somebody else published underneath this draft.
	base := published.VersionID
	if existing, err := s.repo.LegalDraft(ctx, document); err == nil {
		base = existing.BaseVersionID
	} else if !errors.Is(err, repository.ErrNoLegalDraft) {
		return nil, err
	}

	if err := s.repo.SaveLegalDraft(ctx, repository.LegalDraft{
		Document:         document,
		BaseVersionID:    base,
		PublishedLocales: locales,
		Artifacts:        artifacts,
		UpdatedBy:        input.UpdatedBy,
	}, s.now()); err != nil {
		return nil, err
	}

	return s.LegalWorkspace(ctx, document)
}

// DiscardLegalDraft throws the draft away and answers with the workspace, whose
// draft is now a copy of the published edition.
//
// Discarding a document with no draft is a success, not a 404: what the caller
// asked for is what they have.
func (s *Service) DiscardLegalDraft(ctx context.Context, document string) (*OperatorLegalWorkspace, error) {
	document, err := parseLegalDocument(document)
	if err != nil {
		return nil, err
	}
	if err := s.repo.DiscardLegalDraft(ctx, document); err != nil {
		return nil, err
	}
	return s.LegalWorkspace(ctx, document)
}

// publishedEdition reads whichever document's current edition, through the same
// repository calls every reader uses — NOT through the legal text cache beside
// it. The cache serves readers a document that is at most sixty seconds stale,
// which is exactly right for a Storefront page and exactly wrong for the screen
// an operator is about to write the next edition on.
func (s *Service) publishedEdition(ctx context.Context, document string) (OperatorLegalEdition, error) {
	switch document {
	case LegalDocumentPolicy:
		edition, err := s.repo.CurrentPolicyEdition(ctx)
		if err != nil {
			return OperatorLegalEdition{}, err
		}
		return editionView(edition.Version.ID, edition.Version.Label, edition.Version.EffectiveDate, edition.Version.ContentHash, edition.Artifacts), nil
	default:
		edition, err := s.repo.CurrentTermsEdition(ctx)
		if err != nil {
			return OperatorLegalEdition{}, err
		}
		return editionView(edition.Version.ID, edition.Version.Label, edition.Version.EffectiveDate, edition.Version.ContentHash, edition.Artifacts), nil
	}
}

func editionView(id, label string, effective time.Time, hash string, artifacts []legal.Artifact) OperatorLegalEdition {
	return OperatorLegalEdition{
		VersionID:     id,
		Label:         label,
		EffectiveDate: effective.Format("2006-01-02"),
		ContentHash:   hash,
		Locales:       localeTokens(legal.Locales(artifacts)),
		Artifacts:     groupArtifacts(artifacts),
	}
}

// groupArtifacts transposes storage rows into the editor's grid: one entry per
// slug, in ordinal order, carrying its text in each language it is written in.
func groupArtifacts(artifacts []legal.Artifact) []OperatorLegalArtifact {
	byslug := make(map[string]*OperatorLegalArtifact, len(artifacts))
	order := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		entry, ok := byslug[artifact.Slug]
		if !ok {
			entry = &OperatorLegalArtifact{Slug: artifact.Slug, Ordinal: artifact.Ordinal, Bodies: map[string]string{}}
			byslug[artifact.Slug] = entry
			order = append(order, artifact.Slug)
		}
		// The lowest ordinal any language gives a slug decides where the row
		// sits. They agree in every draft this platform writes — the service
		// stamps one ordinal per slug across every language — and this is what
		// happens if a hand-written row ever disagrees: a stable order rather
		// than one that depends on which language was read first.
		if artifact.Ordinal < entry.Ordinal {
			entry.Ordinal = artifact.Ordinal
		}
		entry.Bodies[string(artifact.Locale)] = artifact.Body
	}

	grid := make([]OperatorLegalArtifact, 0, len(order))
	for _, slug := range order {
		grid = append(grid, *byslug[slug])
	}
	sort.SliceStable(grid, func(i, j int) bool { return grid[i].Ordinal < grid[j].Ordinal })
	return grid
}

// draftArtifactRows flattens the editor's grid back into storage rows, stamping
// the ordinal from the slug's POSITION IN THE LIST.
func draftArtifactRows(grid []SaveLegalDraftArtifact) ([]legal.Artifact, error) {
	seen := make(map[string]bool, len(grid))
	rows := make([]legal.Artifact, 0, len(grid)*len(legalDraftLocales))
	for i, entry := range grid {
		slug := strings.TrimSpace(entry.Slug)
		if slug == "" {
			return nil, consent.ErrLegalDraftSlugRequired()
		}
		if seen[slug] {
			return nil, consent.ErrLegalDraftDuplicateSlug(slug)
		}
		seen[slug] = true

		for token, body := range entry.Bodies {
			locale, ok := platform.ParseLocale(token)
			if !ok || !slices.Contains(legalDraftLocales, locale) {
				return nil, consent.ErrLegalDraftLocaleUnsupported(token)
			}
			body = strings.TrimSpace(body)
			if body == "" {
				// A blank cell is stored as no row, so that "written and then
				// emptied" and "never written" are one state everywhere.
				continue
			}
			rows = append(rows, legal.Artifact{
				Locale:  locale,
				Slug:    slug,
				Ordinal: i + 1,
				Body:    body,
			})
		}
	}
	return rows, nil
}

func parseLegalDocument(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case LegalDocumentPolicy:
		return LegalDocumentPolicy, nil
	case LegalDocumentTerms:
		return LegalDocumentTerms, nil
	default:
		return "", consent.ErrLegalDocumentNotFound()
	}
}

// parseDraftLocales resolves the published-language set, bounded by the menu and
// ordered the way the preimage orders languages.
func parseDraftLocales(tokens []string) ([]platform.Locale, error) {
	if len(tokens) == 0 {
		return nil, consent.ErrLegalDraftLocalesRequired()
	}
	set := make(map[platform.Locale]bool, len(tokens))
	for _, token := range tokens {
		locale, ok := platform.ParseLocale(token)
		if !ok || !slices.Contains(legalDraftLocales, locale) {
			return nil, consent.ErrLegalDraftLocaleUnsupported(token)
		}
		set[locale] = true
	}
	locales := make([]platform.Locale, 0, len(set))
	for _, locale := range legalDraftLocales {
		if set[locale] {
			locales = append(locales, locale)
		}
	}
	return locales, nil
}

func localeTokens(locales []platform.Locale) []string {
	tokens := make([]string, 0, len(locales))
	for _, locale := range locales {
		tokens = append(tokens, string(locale))
	}
	return tokens
}
