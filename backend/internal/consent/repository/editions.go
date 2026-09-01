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
// effective date that has ARRIVED, ties broken by insertion order. That
// `effective_date <= CURRENT_DATE` is what makes a
// scheduled edition free — the query moves at midnight, so nothing fires, no
// job runs and no cache is invalidated by anything but its own age. Adding a
// publication hook here would quietly make the scheduled edition depend on that
// hook having run.
//
// `cancelled_at IS NULL` rides in the SAME predicate (#564, migration 113), and
// deliberately not in a second pass over the result: a withdrawn edition is
// retained and marked rather than deleted, so the only thing standing between it
// and becoming current on its own date is this line. It is the same shape as the
// rule beside it — a fact the database checks as it chooses the row — so a
// cancellation needs nothing to fire either.
//
// CURRENT_DATE is the database's day, which is UTC in every environment this
// runs in. That is the coarsest thing about this query and it is fine: an
// edition becoming current a few hours early or late relative to Ecuador is
// invisible to everyone, because what changes at the boundary is which label a
// re-acceptance is recorded under, not whether anyone is asked.
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
			  AND cancelled_at IS NULL
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
			  AND cancelled_at IS NULL
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

// TermsEditionByID reads ONE NAMED Terms Version together with all of its text
// — CurrentTermsEdition's query asked of an edition somebody holds an id for
// rather than of whichever is in effect right now.
//
// IT EXISTS FOR THE HELD-ANSWER RULE (#537, #587). A pending staff terms token
// pins the edition whose box was actually drawn, and the request that ticks
// that box arrives minutes later knowing only the token. Asking "does the
// edition this person was SHOWN carry the Adulthood Declaration Artifact?"
// therefore cannot be answered from the current edition: a publish inside the
// token's fifteen minutes would judge a submission against words that were
// never on screen — refusing somebody over a box they were never drawn, or
// silently dropping a declaration they made.
//
// No `effective_date` and no `cancelled_at` predicate, deliberately, and that
// is the whole difference from the query above. This is a read about the PAST:
// the edition was current when the box was issued, and an edition that has
// since been superseded — or was cancelled overnight — is still the edition
// whose text a person read. Filtering it out here would make the evidence path
// disagree with the evidence.
func (r *Repository) TermsEditionByID(ctx context.Context, id string) (TermsEdition, error) {
	const query = `
		SELECT v.id, v.label, v.effective_date, v.content_hash,
		       a.locale, a.slug, a.ordinal, a.body
		FROM terms_versions v
		LEFT JOIN terms_version_artifacts a ON a.version_id = v.id
		WHERE v.id = $1
		ORDER BY a.locale COLLATE "C", a.ordinal
	`

	rows, err := r.db.Pool.QueryContext(ctx, query, id)
	if err != nil {
		return TermsEdition{}, fmt.Errorf("terms edition by id: %w", err)
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
			return TermsEdition{}, fmt.Errorf("terms edition by id: %w", err)
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
		return TermsEdition{}, fmt.Errorf("terms edition by id: %w", err)
	}
	if !found {
		// An id nobody can produce without having been handed it by this
		// platform. It reads as the plain error it is rather than as "no current
		// version", because the two are different failures: one is an empty
		// lineage, this is a dangling reference.
		return TermsEdition{}, fmt.Errorf("terms edition %q not found", id)
	}
	return edition, nil
}
