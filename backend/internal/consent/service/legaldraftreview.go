package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Legal Center's review state (#562, spec #556): what the operator has
// LOOKED AT, as opposed to what they have written.
//
// Two facts live here, and #563 turns both into publish preconditions — every
// artifact previewed as a reader will see it, and the diff against the current
// edition seen. Publishing is the platform changing the agreement everybody is
// held to; "I only ever saw it in a textarea" and "I did not know what it
// changed" are the two ways that goes wrong quietly, and both are answerable by
// the platform rather than by a habit.
//
// A PREVIEW IS A LOGGER LINE AND NEVER A consent_access_log ROW (#545, amending
// #544). That table is arriving in #569 to record touches of somebody's DATA —
// an operator reading a Customer's consents — and the value of it is that every
// row in it is exactly that. An operator reading the platform's own unpublished
// words has touched nobody's data, so it is logged where operational facts are
// logged and nowhere else.
//
// THE PREVIEW ITSELF IS RENDERED IN THE BROWSER, by the same `Markdown`
// component the Storefront's privacy-policy page renders, over text the
// workspace read already carried. There is deliberately no server-side "render
// this draft" call and no preview ROUTE on the public side: the public route
// resolves what is current itself and refuses to be told which edition to serve,
// which is what keeps unpublished text unpublished. What crosses the wire here
// is only the RECORD that a preview happened.

// OperatorLegalCellRef names one cell of the editor's grid: one artifact in one
// language. The unit of both the preview rule and the diff.
type OperatorLegalCellRef struct {
	Slug   string `json:"slug"`
	Locale string `json:"locale"`
}

// OperatorLegalPreviewedCell is one cell an operator has seen rendered, and is
// STILL a preview of this draft — a preview of words that have since been
// rewritten is not reported at all.
type OperatorLegalPreviewedCell struct {
	Slug        string `json:"slug"`
	Locale      string `json:"locale"`
	PreviewedBy string `json:"previewed_by"`
	// PreviewedAt is RFC3339, stamped by the server's clock like every other
	// time this module records.
	PreviewedAt string `json:"previewed_at"`
}

// PreviewLegalDraftCellInput names the cell that was put on screen. The text is
// NOT sent: what was previewed is whatever the draft says, and a client that
// could name the text could claim to have previewed something else.
type PreviewLegalDraftCellInput struct {
	Slug   string
	Locale string
	// By is the operator's email, from the Staff Session and never the body.
	By string
}

// PreviewLegalDraftCell records that one cell of the draft was seen rendered.
//
// IDEMPOTENT: previewing the same cell twice is one fact. Previewing it again
// after an edit is how an expired preview comes back, because what is stored is
// the digest of what was on screen.
func (s *Service) PreviewLegalDraftCell(ctx context.Context, document string, input PreviewLegalDraftCellInput) (*OperatorLegalWorkspace, error) {
	document, err := parseLegalDocument(document)
	if err != nil {
		return nil, err
	}

	locale, ok := platform.ParseLocale(strings.TrimSpace(input.Locale))
	if !ok || !slices.Contains(legalDraftLocales, locale) {
		return nil, consent.ErrLegalDraftLocaleUnsupported(input.Locale)
	}
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return nil, consent.ErrLegalDraftSlugRequired()
	}

	draft, err := s.repo.LegalDraft(ctx, document)
	if errors.Is(err, repository.ErrNoLegalDraft) {
		// Nothing to preview. The editor never asks — it offers the preview only
		// once the draft is saved, because what a preview promises is a look at
		// the text that will be published, and unsaved text will not be.
		return nil, consent.ErrLegalDraftNotStored()
	}
	if err != nil {
		return nil, err
	}

	body, found := draftCellBody(draft.Artifacts, locale, slug)
	if !found {
		// A cell with no text is not a cell that can be previewed. It is a hole,
		// and the completeness rule already refuses to publish it.
		return nil, consent.ErrLegalDraftCellNotFound(slug, string(locale))
	}

	if err := s.repo.RecordLegalDraftPreview(ctx, document, repository.LegalDraftPreview{
		Locale:      locale,
		Slug:        slug,
		BodyDigest:  bodyDigest(body),
		PreviewedBy: input.By,
		PreviewedAt: s.now(),
	}); err != nil {
		return nil, err
	}

	// #545: a preview is an operational fact about the platform's own words, so
	// it is a log line. No consent_access_log row is written here, now or ever —
	// nobody's data was touched.
	s.logger.Info("legal draft artifact previewed",
		"document", document,
		"slug", slug,
		"locale", string(locale),
		"operator", input.By,
	)

	return s.LegalWorkspace(ctx, document)
}

// SeeLegalDraftDiff records that the diff between this draft and the CURRENT
// published edition was put on screen.
//
// Both sides are stamped, because a diff is a statement about a pair: if
// somebody publishes underneath the draft, the diff that was seen stops being a
// diff of anything current and the publish precondition lapses on its own.
func (s *Service) SeeLegalDraftDiff(ctx context.Context, document string, by string) (*OperatorLegalWorkspace, error) {
	document, err := parseLegalDocument(document)
	if err != nil {
		return nil, err
	}

	draft, err := s.repo.LegalDraft(ctx, document)
	if errors.Is(err, repository.ErrNoLegalDraft) {
		return nil, consent.ErrLegalDraftNotStored()
	}
	if err != nil {
		return nil, err
	}

	published, err := s.publishedEdition(ctx, document)
	if err != nil {
		return nil, err
	}

	if err := s.repo.RecordLegalDraftDiffSeen(ctx, document, repository.LegalDraftDiffSeen{
		DraftDigest: draftDigest(draft.Artifacts),
		VersionID:   published.VersionID,
		SeenBy:      by,
		SeenAt:      s.now(),
	}); err != nil {
		return nil, err
	}

	s.logger.Info("legal draft diff seen",
		"document", document,
		"against_version_id", published.VersionID,
		"operator", by,
	)

	return s.LegalWorkspace(ctx, document)
}

// draftReview assembles the review half of a stored draft's view: which cells
// are previewed AT THEIR CURRENT TEXT, which are not, and whether the diff on
// record is still a diff of this draft against what is published now.
//
// EXPIRY IS A COMPARISON AND NEVER A DELETION. Nothing invalidates review state
// when a draft is saved; a stored digest that no longer matches simply stops
// counting. That is what lets Save be pressed twice without un-previewing an
// afternoon's reading, and what makes it impossible to preview a paragraph,
// rewrite it, and still hold a green light over words nobody has seen.
func (s *Service) draftReview(
	ctx context.Context,
	document string,
	draft repository.LegalDraft,
	publishedVersionID string,
) (draftReviewView, error) {
	previews, err := s.repo.LegalDraftPreviews(ctx, document)
	if err != nil {
		return draftReviewView{}, err
	}
	digests := make(map[OperatorLegalCellRef]repository.LegalDraftPreview, len(previews))
	for _, preview := range previews {
		digests[OperatorLegalCellRef{Slug: preview.Slug, Locale: string(preview.Locale)}] = preview
	}

	view := draftReviewView{
		Previewed:   []OperatorLegalPreviewedCell{},
		PreviewGaps: []OperatorLegalCellRef{},
	}

	// THE CELLS THAT NEED PREVIEWING are the draft's own, in the languages the
	// draft intends to PUBLISH. A cell written in a language the draft has
	// dropped will not ship, so nobody has to have read it; a cell with no text
	// cannot be previewed at all, and is the completeness rule's refusal rather
	// than this one's.
	for _, artifact := range orderedDraftCells(draft.Artifacts) {
		if !slices.Contains(draft.PublishedLocales, artifact.Locale) {
			continue
		}
		ref := OperatorLegalCellRef{Slug: artifact.Slug, Locale: string(artifact.Locale)}
		preview, seen := digests[ref]
		if seen && preview.BodyDigest == bodyDigest(artifact.Body) {
			view.Previewed = append(view.Previewed, OperatorLegalPreviewedCell{
				Slug:        ref.Slug,
				Locale:      ref.Locale,
				PreviewedBy: preview.PreviewedBy,
				PreviewedAt: preview.PreviewedAt.UTC().Format(rfc3339Millis),
			})
			continue
		}
		view.PreviewGaps = append(view.PreviewGaps, ref)
	}
	view.PreviewedAll = len(view.PreviewGaps) == 0

	seen, err := s.repo.LegalDraftDiffSeen(ctx, document)
	switch {
	case errors.Is(err, repository.ErrNoLegalDraftDiffSeen), errors.Is(err, repository.ErrNoLegalDraft):
		return view, nil
	case err != nil:
		return draftReviewView{}, err
	}

	if seen.DraftDigest == draftDigest(draft.Artifacts) && seen.VersionID == publishedVersionID {
		view.SeenDiff = true
		view.DiffSeenBy = seen.SeenBy
		view.DiffSeenAt = seen.SeenAt.UTC().Format(rfc3339Millis)
	}
	return view, nil
}

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"

// draftReviewView is the review half of OperatorLegalDraft, assembled apart from
// it so the drafting file stays about the text.
type draftReviewView struct {
	Previewed    []OperatorLegalPreviewedCell
	PreviewGaps  []OperatorLegalCellRef
	PreviewedAll bool
	SeenDiff     bool
	DiffSeenBy   string
	DiffSeenAt   string
}

// draftCellBody finds one cell's text, if the operator has written any.
func draftCellBody(artifacts []legal.Artifact, locale platform.Locale, slug string) (string, bool) {
	for _, artifact := range artifacts {
		if artifact.Locale == locale && artifact.Slug == slug {
			return artifact.Body, artifact.Body != ""
		}
	}
	return "", false
}

// orderedDraftCells returns the draft's cells in a stable order — ascending
// language, then ordinal, then slug — so that the preview gaps an operator is
// shown are in the same order twice running.
func orderedDraftCells(artifacts []legal.Artifact) []legal.Artifact {
	ordered := slices.Clone(artifacts)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Locale != ordered[j].Locale {
			return ordered[i].Locale < ordered[j].Locale
		}
		if ordered[i].Ordinal != ordered[j].Ordinal {
			return ordered[i].Ordinal < ordered[j].Ordinal
		}
		return ordered[i].Slug < ordered[j].Slug
	})
	return ordered
}

// bodyDigest and draftDigest fingerprint a DRAFT, for the single purpose of
// noticing that it has changed since somebody looked at it.
//
// THEY ARE NOT legal.ContentHash, AND THAT IS DELIBERATE. ContentHash computes
// the published preimage (#541): the value a Consent Record points at, that a
// reader is shown, and that must be recomputable from the database in ten years.
// Migration 111 is explicit that no draft text is ever hashed in that sense, and
// putting an evidence fingerprint on unpublished words would be exactly the
// confusion it was avoiding — somebody would eventually compare one to the other
// and believe the answer meant something. These are review state: a private,
// domain-separated digest that may be changed at any time without a migration,
// because nothing outside this file has ever been promised a value from it.
func bodyDigest(body string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("legal-draft-review/v1/body\n%d\n%s", len(body), body)))
	return hex.EncodeToString(sum[:])
}

func draftDigest(artifacts []legal.Artifact) string {
	var preimage strings.Builder
	preimage.WriteString("legal-draft-review/v1/draft\n")
	for _, artifact := range orderedDraftCells(artifacts) {
		fmt.Fprintf(&preimage, "%s\n%s\n%d\n%d\n%s\n",
			artifact.Locale, artifact.Slug, artifact.Ordinal, len(artifact.Body), artifact.Body)
	}
	sum := sha256.Sum256([]byte(preimage.String()))
	return hex.EncodeToString(sum[:])
}
