package service

import (
	"context"

	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
)

// The Legal Center (#561, spec #556): the fifth module this service composes,
// and the first that is about the platform's own words.
//
// It owns nothing, as the rest of this package owns nothing. The documents, the
// draft, the preimage rule and every refusal belong to the consent module; this
// side is only the operator namespace's door onto them, so consent never learns
// that operators exist (ADR 0015).
//
// WHY THE LEGAL CENTER IS AN OPERATOR SURFACE AND NEVER AN ORGANIZATION ONE.
// The Privacy Policy and the Términos y Condiciones are the PLATFORM's
// agreements with the people who use it — one text, published once, accepted by
// Customers of every Organization and by staff of every Organization. An
// Organization that could edit either would be rewriting the contract that
// other Organizations' Members and other venues' buyers are held to. The gate
// is the namespace's — the operator allowlist and no Membership at all — which
// is what makes that structural rather than remembered.

// LegalDocuments is what the operator surface needs from consent: the Legal
// Center's one mutable draft per document, and the published edition it is
// written against.
//
// Five methods, and still nothing that PUBLISHES — that arrives with its own
// confirmation step (#563) — and nothing that reaches a Consent Record: the
// Legal Center writes the platform's words, never anybody's evidence.
//
// The last two are #562's: they record what the operator has LOOKED AT. They
// record and do not render. The preview itself is drawn in the browser by the
// same component the Storefront renders, over text the workspace read already
// carried, so there is no "render this draft" call here and no preview route on
// the public side — the public route resolves what is current itself and refuses
// to be told which edition to serve.
type LegalDocuments interface {
	// LegalWorkspace reads one document's published edition and its draft
	// together. It answers LEGAL_DOCUMENT_NOT_FOUND for anything that is not
	// one of the two documents, which the handler maps to 404.
	LegalWorkspace(ctx context.Context, document string) (*consentsvc.OperatorLegalWorkspace, error)
	// SaveLegalDraft replaces the draft WHOLE — adding and removing artifacts
	// is nothing more than saving a different list. It answers the draft-shape
	// refusals (an unsupported language, an empty language set, a blank or
	// duplicated slug) and saves a half-translated draft happily.
	SaveLegalDraft(ctx context.Context, document string, input consentsvc.SaveLegalDraftInput) (*consentsvc.OperatorLegalWorkspace, error)
	// DiscardLegalDraft throws the draft away, leaving the document as its
	// published edition. Discarding when there is no draft is a success.
	DiscardLegalDraft(ctx context.Context, document string) (*consentsvc.OperatorLegalWorkspace, error)
	// PreviewLegalDraftCell records that one artifact was seen rendered in one
	// language. Idempotent, and remembered by the text that was on screen, so a
	// preview lapses by itself when the words are rewritten.
	PreviewLegalDraftCell(ctx context.Context, document string, input consentsvc.PreviewLegalDraftCellInput) (*consentsvc.OperatorLegalWorkspace, error)
	// SeeLegalDraftDiff records that the diff against the current edition was
	// put on screen. Remembered against both sides, so it lapses if somebody
	// publishes underneath the draft.
	SeeLegalDraftDiff(ctx context.Context, document string, by string) (*consentsvc.OperatorLegalWorkspace, error)
	// PublishLegalEdition publishes the draft as a new edition or as a
	// correction (#563). It answers every publish refusal — an incomplete
	// draft, an unpreviewed cell, an unseen diff, a structural or locale-set
	// change offered as a correction, a correction that corrects nothing, a
	// missing reason, an effective date that is not one or is not a night away,
	// and a draft that would stop publishing the language its document may not
	// be published without.
	PublishLegalEdition(ctx context.Context, document string, input consentsvc.PublishLegalEditionInput) (*consentsvc.OperatorLegalWorkspace, error)
}

// LegalWorkspace is the Legal Center's one read: what is published, what is
// being drafted, and the languages a draft may publish in.
func (s *Service) LegalWorkspace(ctx context.Context, document string) (*consentsvc.OperatorLegalWorkspace, error) {
	return s.legal.LegalWorkspace(ctx, document)
}

// SaveLegalDraft records the operator's work in progress. It publishes nothing:
// no reader's page changes, no fingerprint is computed and no acceptance is
// re-gated by this call.
func (s *Service) SaveLegalDraft(ctx context.Context, document string, input consentsvc.SaveLegalDraftInput) (*consentsvc.OperatorLegalWorkspace, error) {
	return s.legal.SaveLegalDraft(ctx, document, input)
}

// DiscardLegalDraft restores the draft to the current edition, so an experiment
// is not a commitment.
func (s *Service) DiscardLegalDraft(ctx context.Context, document string) (*consentsvc.OperatorLegalWorkspace, error) {
	return s.legal.DiscardLegalDraft(ctx, document)
}

// PreviewLegalDraftCell records that the operator has seen one artifact
// rendered as a reader will see it (#562). It changes no text and publishes
// nothing; it is the platform noticing that somebody looked.
func (s *Service) PreviewLegalDraftCell(ctx context.Context, document string, input consentsvc.PreviewLegalDraftCellInput) (*consentsvc.OperatorLegalWorkspace, error) {
	return s.legal.PreviewLegalDraftCell(ctx, document, input)
}

// SeeLegalDraftDiff records that the operator has been shown what this draft
// changes about the current edition — the other half of what #563 requires
// before a publish button exists.
func (s *Service) SeeLegalDraftDiff(ctx context.Context, document string, by string) (*consentsvc.OperatorLegalWorkspace, error) {
	return s.legal.SeeLegalDraftDiff(ctx, document, by)
}

// PublishLegalEdition is the sixth method and the first that a reader can see
// the effect of (#563): the draft becomes a published edition, or a correction
// to one.
//
// Still nothing owned here. The two acts, the lineage, the fingerprint, the
// overnight delay and every refusal belong to the consent module; this side is
// the operator namespace's door onto them, and it is the namespace's own gate —
// the operator allowlist, no Membership at all — that makes "an Org Admin cannot
// rewrite the contract other venues' buyers are held to" structural rather than
// remembered.
func (s *Service) PublishLegalEdition(ctx context.Context, document string, input consentsvc.PublishLegalEditionInput) (*consentsvc.OperatorLegalWorkspace, error) {
	return s.legal.PublishLegalEdition(ctx, document, input)
}
