package service

import (
	"context"
	"errors"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
)

// The night the overnight delay buys (#564, spec #556, ADR 0067).
//
// A gating edition may not take effect the day it is published, so between the
// click and the midnight rollover there is a night in which the operator can
// change their mind. This file is what makes that night usable: the workspace
// carries a PERSISTENT BANNER for every edition still waiting, and each one can
// be withdrawn.
//
// CANCELLING IS UNGATED AND IMMEDIATE. No reason, no delay, no confirmation
// ceremony, no approval step, and no second person — undoing is always cheaper
// than doing. The publication it reverses re-gates the entire customer base and
// the whole staff platform; the withdrawal, before a word of the edition has
// been on any screen, moves nobody at all. Pricing the two acts alike is how an
// operator ends up publishing something they had changed their mind about.
//
// THE ROW IS RETAINED AND MARKED. What was nearly published survives in full —
// its artifacts, its fingerprint, its provenance — and its label stays spent, so
// no later edition can quietly reuse the number.
//
// AND NOTHING FIRES. A cancelled edition stops counting because every selector
// reads `cancelled_at IS NULL` in the same query that answers
// `effective_date <= CURRENT_DATE`, and because legal.GatingFloor and
// legal.Satisfying skip a marked row. There is no job, no cache invalidation and
// nothing to clean up at midnight: the query simply stops choosing the row,
// exactly as it started choosing a scheduled one.

// OperatorLegalScheduledEdition is one edition still waiting for its day, as the
// banner names it.
//
// IT CARRIES NO TEXT. The banner is a reminder that something is about to take
// effect, not a second reading surface — the words are the workspace's published
// edition and its draft, and an edition rendered here would be a third copy of
// them nobody diffed.
type OperatorLegalScheduledEdition struct {
	// VersionID is what the cancel control sends back.
	VersionID string `json:"version_id"`
	// Label is the edition's name, rendered from its lineage: `3`, or `2.1` for
	// a correction waiting with the edition it corrects.
	Label string `json:"label"`
	// EffectiveDate is the day it takes effect, YYYY-MM-DD — the other half of
	// what the banner has to say, because "an edition is coming" without a day
	// is not something anybody can act on.
	EffectiveDate string `json:"effective_date"`
	// Gating says whether this edition will re-gate everybody when its day
	// comes. Sent so the banner can distinguish the act that asks the whole
	// customer base again from a correction riding along with it.
	Gating bool `json:"gating"`
}

// scheduledEditions is the banner's data, read from a lineage the caller
// already has.
//
// NO SEPARATE READ. The workspace's publish plan reads the lineage to compute
// the next labels and the headcount, and this is the same lineage answering a
// second question about it — a second query could straddle a publication or a
// cancellation and describe neither state.
//
// The CANCEL CONTROL'S EXISTENCE IS MEMBERSHIP OF THIS LIST, and that is the
// whole implementation of "the control disappears once the date has passed".
// legal.Edition.Arrived is the database's own answer about its own day, so an
// edition leaves the list at midnight with nothing fired — the interface stops
// offering the act at the same instant the seam stops permitting it.
func scheduledEditions(editions []legal.Edition) []OperatorLegalScheduledEdition {
	scheduled := make([]OperatorLegalScheduledEdition, 0, len(editions))
	for _, edition := range legal.ScheduledEditions(editions) {
		scheduled = append(scheduled, OperatorLegalScheduledEdition{
			VersionID:     edition.ID,
			Label:         edition.Lineage.Label(),
			EffectiveDate: edition.EffectiveDate.Format("2006-01-02"),
			Gating:        edition.Lineage.Gating(),
		})
	}
	return scheduled
}

// CancelLegalEdition withdraws a scheduled edition and answers with the
// workspace as it now stands — whose banner no longer names it, and whose
// publish plan has gone back to what it said before the publication.
//
// IT TAKES NO REASON AND NO CONFIRMATION TOKEN. There is nothing to type and
// nothing to acknowledge; the request is the act.
//
// CANCELLING TWICE IS A SUCCESS, on DiscardLegalDraft's terms: what the caller
// asked for is exactly what they now have, and the response is identical either
// way. The FIRST cancellation's provenance is kept, because it is the one that
// describes the act — a second click by the same operator on a stale tab must
// not rewrite who withdrew the edition, or when.
func (s *Service) CancelLegalEdition(ctx context.Context, document, versionID, by string) (*OperatorLegalWorkspace, error) {
	document, err := parseLegalDocument(document)
	if err != nil {
		return nil, err
	}

	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		// An address with no edition in it. The same answer a nonexistent id
		// gets, because it is the same mistake: nothing was named.
		return nil, consent.ErrLegalEditionNotFound()
	}

	cancelled, err := s.repo.CancelLegalEdition(ctx, document, versionID, by, s.now())
	switch {
	case errors.Is(err, repository.ErrLegalEditionNotFound):
		return nil, consent.ErrLegalEditionNotFound()
	case errors.Is(err, repository.ErrLegalEditionAlreadyEffective):
		return nil, consent.ErrLegalEditionAlreadyEffective(cancelled.EffectiveDate.Format("2006-01-02"))
	case err != nil:
		return nil, err
	}

	if !cancelled.AlreadyCancelled {
		s.logger.Info("legal edition cancelled",
			"document", document,
			"label", cancelled.Label,
			"version_id", versionID,
			"effective_date", cancelled.EffectiveDate.Format("2006-01-02"),
			"operator", by,
		)
	}

	return s.LegalWorkspace(ctx, document)
}
