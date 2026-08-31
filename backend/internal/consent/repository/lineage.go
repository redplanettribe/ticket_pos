package repository

import (
	"context"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// PolicyLineage reads every published Policy Version as a legal.Edition: its
// id, its generation and revision, its dates, and whether its day has arrived
// (#560).
//
// EVERY row, with no LIMIT. Both version tables hold a handful of rows for the
// lifetime of the product — migration 060 says so and migration 110 declines to
// index them for the same reason — and the satisfying set is a question about
// the whole lineage, not about one row. Answering it in SQL would mean a
// window function whose ordering had to be kept in step by hand with the
// current-edition selectors; answering it in Go means one ordering rule
// (legal.GatingFloor) that a unit test can walk.
//
// `effective_date <= CURRENT_DATE` is computed BY THE DATABASE and carried back
// as `arrived`. That is the property the whole design rests on: the day moves
// on its own, so a scheduled edition takes effect at midnight with nothing
// fired and nothing notified. See legal.Edition.Arrived.
func (r *Repository) PolicyLineage(ctx context.Context) ([]legal.Edition, error) {
	editions, err := r.lineage(ctx, `
		SELECT id, generation, revision, effective_date, created_at,
		       effective_date <= CURRENT_DATE
		FROM policy_versions
		ORDER BY effective_date DESC, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("policy lineage: %w", err)
	}
	return editions, nil
}

// TermsLineage is PolicyLineage over the parallel table (ADR 0066). The two
// documents version independently and an edition of one must never re-gate the
// other, which is why this is a second read and not a parameter.
func (r *Repository) TermsLineage(ctx context.Context) ([]legal.Edition, error) {
	editions, err := r.lineage(ctx, `
		SELECT id, generation, revision, effective_date, created_at,
		       effective_date <= CURRENT_DATE
		FROM terms_versions
		ORDER BY effective_date DESC, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("terms lineage: %w", err)
	}
	return editions, nil
}

// SatisfyingPolicyEditions is the set of Policy edition ids that clear the
// gate: the gating floor and everything at or above it, corrections included.
//
// An acceptance naming any of these is an acceptance; anything else — a
// superseded edition, or no acceptance at all — is outstanding. Returns
// ErrNoCurrentPolicyVersion when no gating edition has taken effect, which is
// the same failure CurrentPolicyVersion reports and which migration 066 makes
// unreachable.
func (r *Repository) SatisfyingPolicyEditions(ctx context.Context) (legal.SatisfyingSet, error) {
	editions, err := r.PolicyLineage(ctx)
	if err != nil {
		return nil, err
	}
	set, ok := legal.Satisfying(editions)
	if !ok {
		return nil, ErrNoCurrentPolicyVersion
	}
	return set, nil
}

// SatisfyingTermsEditions is SatisfyingPolicyEditions over the Terms' own
// table. Migration 105 seeds edition 1, so the not-ok branch is unreachable in
// any migrated environment.
func (r *Repository) SatisfyingTermsEditions(ctx context.Context) (legal.SatisfyingSet, error) {
	editions, err := r.TermsLineage(ctx)
	if err != nil {
		return nil, err
	}
	set, ok := legal.Satisfying(editions)
	if !ok {
		return nil, ErrNoCurrentTermsVersion
	}
	return set, nil
}

// lineage runs one of the two identical reads. The query is a literal at each
// call site rather than a table name interpolated here, so that neither table
// can be reached by passing a string.
func (r *Repository) lineage(ctx context.Context, query string) ([]legal.Edition, error) {
	rows, err := r.db.Pool.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var editions []legal.Edition
	for rows.Next() {
		var edition legal.Edition
		if err := rows.Scan(
			&edition.ID,
			&edition.Lineage.Generation,
			&edition.Lineage.Revision,
			&edition.EffectiveDate,
			&edition.CreatedAt,
			&edition.Arrived,
		); err != nil {
			return nil, err
		}
		editions = append(editions, edition)
	}
	return editions, rows.Err()
}
