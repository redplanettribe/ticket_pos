package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrPolicyVersionNotFound reports that the edition a Customer's row points at
// is not in `policy_versions`.
//
// It should be unreachable: `customers.policy_version_id` is a foreign key with
// ON DELETE RESTRICT, and editions are inserted by migration and never deleted.
// It exists so that the one caller — the Privacy page's read — can say the
// version is unknown rather than fail a page about somebody's rights over a row
// that cannot go missing.
var ErrPolicyVersionNotFound = errors.New("policy version not found")

// PolicyVersionByID reads one edition by its id, which is how the Privacy page
// learns the LABEL of the edition a Customer accepted.
//
// It is a second read beside CurrentPolicyVersion rather than a widening of it,
// because the two answer opposite questions: that one asks which edition is in
// effect now — the platform's finding at a capture moment — and this one asks
// what an edition recorded on a Customer's row is called. A Customer who
// accepted a superseded edition gets the label they actually agreed to, which
// is the only honest thing a settings page can show them.
//
// The id is never a client's. It comes off the Customer's own row, so nothing
// here lets a caller name an edition; the public policy endpoint still
// publishes no ids, for the reason PolicyView states.
func (r *Repository) PolicyVersionByID(ctx context.Context, id string) (PolicyVersion, error) {
	const query = `
		SELECT id, label, effective_date, content_hash
		FROM policy_versions
		WHERE id = $1
	`

	var version PolicyVersion
	err := r.db.Pool.QueryRowContext(ctx, query, id).Scan(
		&version.ID,
		&version.Label,
		&version.EffectiveDate,
		&version.ContentHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PolicyVersion{}, ErrPolicyVersionNotFound
	}
	if err != nil {
		return PolicyVersion{}, fmt.Errorf("policy version by id: %w", err)
	}
	return version, nil
}
