package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
)

// WHAT THE EVIDENCE PACK NEEDS FROM THIS MODULE (#568, parent #556, ADR 0067):
// the exact bytes of the editions the record's acts name, and somewhere to
// record that a handover happened.
//
// THE ACTS THEMSELVES ARE NOT HERE. The pack walks CustomerConsentActs with its
// cursor — #566's read, the one the record screen serves — because an export
// telling a different story from the record it was exported from would be worse
// than no export. Nothing in this file is a second way to read a person's
// history, and nothing in it may become one: these are reads about EDITIONS,
// which are the platform's own published documents and belong to nobody.
//
// THIS PACKAGE DOES NOT IMPORT `consent/evidence`, and the direction matters.
// The pack builder imports THIS module for the act type, so that record.json
// carries the record screen's own struct and the two cannot drift; the arrow
// therefore points one way only, and the mapping into the builder's types
// happens one layer up, where both populations are already in hand.

// LegalEditionTextItem is one published edition and every word of it: what an
// act's edition id resolves to.
type LegalEditionTextItem struct {
	ID string
	// Label is produced by legal.Lineage.Label — the ONE function that ever
	// produces one — so a pack cannot name an edition differently from the
	// Legal Center that published it, or from the record screen beside it.
	Label string
	// Gating is whether publishing this edition re-gated everybody, which is
	// `revision = 0` and nothing else. It becomes the pack's `published_as`.
	//
	// A FACT FIXED AT PUBLICATION, which is the only kind of fact about an
	// edition a deterministic document may carry. "Does this edition satisfy
	// the gate today" is deliberately not offered here, because a caller given
	// it would put it in the pack and two packs over one record would then
	// disagree across a midnight.
	Gating        bool
	EffectiveDate time.Time
	// ContentHash is the fingerprint the version row carries. Read from the
	// row, never recomputed: the pack publishes the preimage so a reader can
	// recompute it themselves.
	ContentHash string
	Artifacts   []legal.Artifact
}

// PolicyEditionTexts resolves Policy edition ids to their text.
//
// IT IS NOT FILTERED BY DATE OR BY CANCELLATION, unlike every other edition
// read in this module. Those answer "what is in force", which is a question
// about today; this answers "what were these acts captured against", and a
// superseded, scheduled or withdrawn edition an act names is still the edition
// somebody was shown. An id naming no row is simply absent from the result.
func (s *Service) PolicyEditionTexts(ctx context.Context, ids []string) ([]LegalEditionTextItem, error) {
	rows, err := s.repo.PolicyEditionTexts(ctx, ids)
	if err != nil {
		return nil, err
	}
	return editionTextItems(rows), nil
}

// TermsEditionTexts is PolicyEditionTexts over the Terms' own lineage. Two
// methods and not one with a document parameter, following every other pair in
// this module: the two documents version independently and an edition of one
// must never be resolvable from the other's id space (ADR 0066).
func (s *Service) TermsEditionTexts(ctx context.Context, ids []string) ([]LegalEditionTextItem, error) {
	rows, err := s.repo.TermsEditionTexts(ctx, ids)
	if err != nil {
		return nil, err
	}
	return editionTextItems(rows), nil
}

func editionTextItems(rows []repository.LegalEditionText) []LegalEditionTextItem {
	items := make([]LegalEditionTextItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, LegalEditionTextItem{
			ID:            row.ID,
			Label:         row.Lineage.Label(),
			Gating:        row.Lineage.Gating(),
			EffectiveDate: row.EffectiveDate,
			ContentHash:   row.ContentHash,
			Artifacts:     row.Artifacts,
		})
	}
	return items
}

// EvidencePackHandover is what survives a generated pack.
type EvidencePackHandover struct {
	// SHA256 is the pack's own fingerprint — the value the filename is keyed
	// on (ADR 0067), the value stored, and the value #569's access log records
	// for the same export.
	SHA256    string
	SizeBytes int
	Acts      []EvidencePackAct
}

// EvidencePackAct is one act a pack disclosed, in migration 118's vocabulary.
type EvidencePackAct struct {
	Kind string
	ID   string
}

// RecordEvidencePack records that a pack was generated: its hash, its size and
// what it covered — and NOT the pack.
//
// It returns the row's id. See repository.RecordEvidencePack for why nothing
// else is kept.
func (s *Service) RecordEvidencePack(ctx context.Context, handover EvidencePackHandover) (string, error) {
	acts := make([]repository.EvidencePackAct, 0, len(handover.Acts))
	for _, act := range handover.Acts {
		acts = append(acts, repository.EvidencePackAct{Kind: act.Kind, ID: act.ID})
	}
	return s.repo.RecordEvidencePack(ctx, repository.EvidencePackHandover{
		SHA256:    handover.SHA256,
		SizeBytes: handover.SizeBytes,
		Acts:      acts,
	})
}
