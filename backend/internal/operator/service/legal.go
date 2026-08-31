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
// Three methods and no more. There is deliberately nothing here that PUBLISHES
// — that arrives with its own confirmation step (#563) — and nothing that
// reaches a Consent Record: the Legal Center writes the platform's words, never
// anybody's evidence.
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
