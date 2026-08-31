package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TermsVersion is one published edition of the Términos y Condiciones, as the
// `terms_versions` row records it. Like PolicyVersion it carries no text: the
// text is embedded in the binary (internal/consent/terms), and this row ties
// the label, the effective date and the fingerprint together.
type TermsVersion struct {
	ID            string
	Label         string
	EffectiveDate time.Time
	ContentHash   string
}

// ErrNoCurrentTermsVersion reports that no Terms edition has taken effect. The
// service maps it to a domain error; migration 105 seeds edition 1, so like its
// policy counterpart it should be unreachable.
var ErrNoCurrentTermsVersion = errors.New("no current terms version")

// CurrentTermsVersion reads the Terms edition in effect: the latest effective
// date that has arrived, ties broken by insertion order — the same rule, the
// same scheduling property and the same exclusion of a withdrawn edition (#564)
// as CurrentPolicyVersion, over the parallel table (ADR 0066).
func (r *Repository) CurrentTermsVersion(ctx context.Context) (TermsVersion, error) {
	const query = `
		SELECT id, label, effective_date, content_hash
		FROM terms_versions
		WHERE effective_date <= CURRENT_DATE
		  AND cancelled_at IS NULL
		ORDER BY effective_date DESC, created_at DESC
		LIMIT 1
	`

	var version TermsVersion
	err := r.db.Pool.QueryRowContext(ctx, query).Scan(
		&version.ID,
		&version.Label,
		&version.EffectiveDate,
		&version.ContentHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TermsVersion{}, ErrNoCurrentTermsVersion
	}
	if err != nil {
		return TermsVersion{}, fmt.Errorf("current terms version: %w", err)
	}
	return version, nil
}
