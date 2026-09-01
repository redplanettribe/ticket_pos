package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Legal Center's drafting store (#561, migration 111): one mutable draft
// per document, read and written WHOLE.
//
// There is no partial update here on purpose — no "save this cell", no "add
// this slug". A draft is one operator's picture of what the next edition says,
// and the only way two cells can contradict each other is if they were written
// by two calls that each thought they knew the rest. Save takes the whole
// draft, replaces the whole draft, and leaves nothing behind to reconcile.

// LegalDraft is one document's work in progress.
//
// It carries the same []legal.Artifact a published edition carries, so that
// publishing (#563) is a copy rather than a translation, and so the preimage
// rule sees exactly one shape of data whether it is hashing a draft for a
// preview or an edition for the record.
type LegalDraft struct {
	// Document is "policy" or "terms" — the primary key. The service validates
	// it; the CHECK on the table is the backstop.
	Document string
	// BaseVersionID is the edition the draft was opened from. It is not a
	// foreign key (see migration 111) and it is not evidence: it exists so the
	// editor can notice that the current edition has moved on underneath it.
	BaseVersionID string
	// PublishedLocales is the set of languages this draft intends to publish
	// in, EXPLICITLY, never inferred from which cells are filled.
	PublishedLocales []platform.Locale
	// Artifacts is every cell the operator has written, in (locale, ordinal)
	// order. A cell they have not written has no entry: absent and empty are
	// the same thing in a draft.
	Artifacts []legal.Artifact
	UpdatedBy string
	UpdatedAt time.Time
	CreatedAt time.Time
}

// ErrNoLegalDraft reports that a document has no draft — the ordinary state of
// affairs, and not a failure. The service answers it by handing back a draft
// materialised from the current edition, so the editor always has something to
// open.
var ErrNoLegalDraft = errors.New("consent: no legal draft")

// LegalDraft reads one document's draft whole: the row, its published-language
// set and every cell, in three queries under no transaction.
//
// NO TRANSACTION, unlike the edition reads beside it, and the difference is
// worth stating. CurrentPolicyEdition reads text and fingerprint in one query
// because a read that straddled a publication would render one edition's words
// beside another's evidence. A draft has no fingerprint and no evidence; the
// worst a straddled read here can do is show an operator the cell they saved a
// millisecond ago, and there is exactly one writer of any draft in practice.
func (r *Repository) LegalDraft(ctx context.Context, document string) (LegalDraft, error) {
	const head = `
		SELECT document, base_version_id, updated_by, updated_at, created_at
		FROM legal_drafts
		WHERE document = $1
	`

	var draft LegalDraft
	err := r.db.Pool.QueryRowContext(ctx, head, document).Scan(
		&draft.Document, &draft.BaseVersionID, &draft.UpdatedBy, &draft.UpdatedAt, &draft.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return LegalDraft{}, ErrNoLegalDraft
	}
	if err != nil {
		return LegalDraft{}, fmt.Errorf("legal draft: %w", err)
	}

	locales, err := r.draftLocales(ctx, document)
	if err != nil {
		return LegalDraft{}, err
	}
	draft.PublishedLocales = locales

	artifacts, err := r.draftArtifacts(ctx, document)
	if err != nil {
		return LegalDraft{}, err
	}
	draft.Artifacts = artifacts

	return draft, nil
}

func (r *Repository) draftLocales(ctx context.Context, document string) ([]platform.Locale, error) {
	const query = `
		SELECT locale
		FROM legal_draft_locales
		WHERE document = $1
		ORDER BY locale COLLATE "C"
	`

	rows, err := r.db.Pool.QueryContext(ctx, query, document)
	if err != nil {
		return nil, fmt.Errorf("legal draft locales: %w", err)
	}
	defer rows.Close()

	var locales []platform.Locale
	for rows.Next() {
		var locale string
		if err := rows.Scan(&locale); err != nil {
			return nil, fmt.Errorf("legal draft locales: %w", err)
		}
		locales = append(locales, platform.Locale(locale))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legal draft locales: %w", err)
	}
	return locales, nil
}

func (r *Repository) draftArtifacts(ctx context.Context, document string) ([]legal.Artifact, error) {
	// The same ORDER BY the edition reads use — ascending locale code, then
	// ordinal — because it is the preimage's order (legal.ContentHash), and a
	// draft that came back in a different order from the edition it will become
	// would make a preview's fingerprint disagree with the publication's.
	const query = `
		SELECT locale, slug, ordinal, body
		FROM legal_draft_artifacts
		WHERE document = $1
		ORDER BY locale COLLATE "C", ordinal
	`

	rows, err := r.db.Pool.QueryContext(ctx, query, document)
	if err != nil {
		return nil, fmt.Errorf("legal draft artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []legal.Artifact
	for rows.Next() {
		var artifact legal.Artifact
		var locale string
		if err := rows.Scan(&locale, &artifact.Slug, &artifact.Ordinal, &artifact.Body); err != nil {
			return nil, fmt.Errorf("legal draft artifacts: %w", err)
		}
		artifact.Locale = platform.Locale(locale)
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legal draft artifacts: %w", err)
	}
	return artifacts, nil
}

// SaveLegalDraft replaces a document's draft with the one handed in, creating it
// if there is none, in ONE transaction.
//
// REPLACE AND NOT MERGE. The cells and the language set are deleted and written
// again, so removing an artifact is the ordinary case rather than a second verb:
// an artifact the operator took out is simply not in the draft they saved. The
// alternative — an upsert per cell plus a delete for whatever is missing — is
// the same statement count with a state nobody can name in between.
func (r *Repository) SaveLegalDraft(ctx context.Context, draft LegalDraft, at time.Time) error {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save legal draft: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const upsert = `
		INSERT INTO legal_drafts (document, base_version_id, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (document) DO UPDATE
		SET base_version_id = EXCLUDED.base_version_id,
		    updated_by = EXCLUDED.updated_by,
		    updated_at = EXCLUDED.updated_at
	`
	if _, err := tx.ExecContext(ctx, upsert, draft.Document, draft.BaseVersionID, draft.UpdatedBy, at); err != nil {
		return fmt.Errorf("save legal draft: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM legal_draft_locales WHERE document = $1`, draft.Document); err != nil {
		return fmt.Errorf("save legal draft: %w", err)
	}
	for _, locale := range draft.PublishedLocales {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO legal_draft_locales (document, locale) VALUES ($1, $2)`,
			draft.Document, string(locale),
		); err != nil {
			return fmt.Errorf("save legal draft locale: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM legal_draft_artifacts WHERE document = $1`, draft.Document); err != nil {
		return fmt.Errorf("save legal draft: %w", err)
	}
	for _, artifact := range draft.Artifacts {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO legal_draft_artifacts (document, locale, slug, ordinal, body) VALUES ($1, $2, $3, $4, $5)`,
			draft.Document, string(artifact.Locale), artifact.Slug, artifact.Ordinal, artifact.Body,
		); err != nil {
			return fmt.Errorf("save legal draft artifact: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save legal draft: %w", err)
	}
	return nil
}

// DiscardLegalDraft deletes a document's draft. The cells and the language set
// go with it, by cascade.
//
// Discarding a draft that does not exist is a success: the caller asked for
// "no draft for this document" and that is what they have.
func (r *Repository) DiscardLegalDraft(ctx context.Context, document string) error {
	if _, err := r.db.Pool.ExecContext(ctx, `DELETE FROM legal_drafts WHERE document = $1`, document); err != nil {
		return fmt.Errorf("discard legal draft: %w", err)
	}
	return nil
}
