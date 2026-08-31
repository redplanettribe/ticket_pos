package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PolicyEdition is one Policy Version and every word of it, in every language
// it publishes, READ TOGETHER.
//
// The togetherness is the point, and it is the failure this type exists to
// design out (#558). Two reads — text here, version row there — can straddle a
// publication: a capture surface would render one edition's words while the
// acceptance beside it recorded the next edition's fingerprint, producing
// evidence of text that was never on screen. One row set, one moment, one
// fingerprint.
type PolicyEdition struct {
	// Version is the edition: its id, label, effective date and content hash.
	Version PolicyVersion
	// Artifacts is every stored artifact of this edition, in every language it
	// publishes. WHICH languages those are is read from here and from nowhere
	// else — no compile-time list of Locales survives (legal.Locales).
	Artifacts []legal.Artifact
}

// TermsEdition is the Terms' counterpart of PolicyEdition, over the parallel
// table (ADR 0066), and read the same way for the same reason.
type TermsEdition struct {
	Version   TermsVersion
	Artifacts []legal.Artifact
}

// CurrentPolicyEdition reads the Policy Version in effect together with all of
// its text, in one query.
//
// THE CURRENT-EDITION RULE IS UNCHANGED and must stay unchanged: the latest
// effective date that has ARRIVED, ties broken by insertion order
// (CurrentPolicyVersion). That `effective_date <= CURRENT_DATE` is what makes a
// scheduled edition free — the query moves at midnight, so nothing fires, no
// job runs and no cache is invalidated by anything but its own age. Adding a
// publication hook here would quietly make the scheduled edition depend on that
// hook having run.
//
// A LEFT JOIN, so an edition with no artifacts at all is a version with empty
// text rather than "no current version": the two are different failures and the
// caller says different things about them.
func (r *Repository) CurrentPolicyEdition(ctx context.Context) (PolicyEdition, error) {
	const query = `
		WITH current AS (
			SELECT id, label, effective_date, content_hash
			FROM policy_versions
			WHERE effective_date <= CURRENT_DATE
			ORDER BY effective_date DESC, created_at DESC
			LIMIT 1
		)
		SELECT c.id, c.label, c.effective_date, c.content_hash,
		       a.locale, a.slug, a.ordinal, a.body
		FROM current c
		LEFT JOIN policy_version_artifacts a ON a.version_id = c.id
		ORDER BY a.locale COLLATE "C", a.ordinal
	`

	rows, err := r.db.Pool.QueryContext(ctx, query)
	if err != nil {
		return PolicyEdition{}, fmt.Errorf("current policy edition: %w", err)
	}
	defer rows.Close()

	var edition PolicyEdition
	found := false
	for rows.Next() {
		var (
			version PolicyVersion
			locale  sql.NullString
			slug    sql.NullString
			ordinal sql.NullInt64
			body    sql.NullString
		)
		if err := rows.Scan(
			&version.ID, &version.Label, &version.EffectiveDate, &version.ContentHash,
			&locale, &slug, &ordinal, &body,
		); err != nil {
			return PolicyEdition{}, fmt.Errorf("current policy edition: %w", err)
		}
		edition.Version = version
		found = true
		if !locale.Valid {
			continue
		}
		edition.Artifacts = append(edition.Artifacts, legal.Artifact{
			Locale:  platform.Locale(locale.String),
			Slug:    slug.String,
			Ordinal: int(ordinal.Int64),
			Body:    body.String,
		})
	}
	if err := rows.Err(); err != nil {
		return PolicyEdition{}, fmt.Errorf("current policy edition: %w", err)
	}
	if !found {
		return PolicyEdition{}, ErrNoCurrentPolicyVersion
	}
	return edition, nil
}

// CurrentTermsEdition reads the Terms Version in effect together with all of
// its text, in one query — CurrentPolicyEdition's shape and rules, over the
// Terms' own tables.
func (r *Repository) CurrentTermsEdition(ctx context.Context) (TermsEdition, error) {
	const query = `
		WITH current AS (
			SELECT id, label, effective_date, content_hash
			FROM terms_versions
			WHERE effective_date <= CURRENT_DATE
			ORDER BY effective_date DESC, created_at DESC
			LIMIT 1
		)
		SELECT c.id, c.label, c.effective_date, c.content_hash,
		       a.locale, a.slug, a.ordinal, a.body
		FROM current c
		LEFT JOIN terms_version_artifacts a ON a.version_id = c.id
		ORDER BY a.locale COLLATE "C", a.ordinal
	`

	rows, err := r.db.Pool.QueryContext(ctx, query)
	if err != nil {
		return TermsEdition{}, fmt.Errorf("current terms edition: %w", err)
	}
	defer rows.Close()

	var edition TermsEdition
	found := false
	for rows.Next() {
		var (
			version TermsVersion
			locale  sql.NullString
			slug    sql.NullString
			ordinal sql.NullInt64
			body    sql.NullString
		)
		if err := rows.Scan(
			&version.ID, &version.Label, &version.EffectiveDate, &version.ContentHash,
			&locale, &slug, &ordinal, &body,
		); err != nil {
			return TermsEdition{}, fmt.Errorf("current terms edition: %w", err)
		}
		edition.Version = version
		found = true
		if !locale.Valid {
			continue
		}
		edition.Artifacts = append(edition.Artifacts, legal.Artifact{
			Locale:  platform.Locale(locale.String),
			Slug:    slug.String,
			Ordinal: int(ordinal.Int64),
			Body:    body.String,
		})
	}
	if err := rows.Err(); err != nil {
		return TermsEdition{}, fmt.Errorf("current terms edition: %w", err)
	}
	if !found {
		return TermsEdition{}, ErrNoCurrentTermsVersion
	}
	return edition, nil
}
