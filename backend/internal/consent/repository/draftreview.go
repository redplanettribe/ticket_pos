package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Legal Center's review state (#562, migration 117): what the operator has
// actually looked at before the publish button is offered.
//
// A SEPARATE FILE FROM drafts.go, and separate calls from LegalDraft/
// SaveLegalDraft, because review state has the opposite lifecycle from the text
// it is about. The draft is written WHOLE — every save replaces every cell — and
// review state must SURVIVE a save that changed nothing, or pressing Save twice
// would un-preview an afternoon's reading. It is expired by comparison against a
// digest rather than by deletion, so the two never need to agree about when a
// cell "really" changed.

// LegalDraftPreview is one cell an operator has seen rendered, remembered by
// WHAT they saw.
type LegalDraftPreview struct {
	Locale platform.Locale
	Slug   string
	// BodyDigest is the digest of the text that was on screen. The service
	// compares it against the draft's current text: a preview of words that
	// have since been edited is not a preview of this draft.
	BodyDigest  string
	PreviewedBy string
	PreviewedAt time.Time
}

// LegalDraftDiffSeen is the diff an operator has been shown, remembered by the
// PAIR it was a diff of.
type LegalDraftDiffSeen struct {
	// DraftDigest is the digest of the draft's cells when the diff was shown.
	DraftDigest string
	// VersionID is the published edition it was taken against. A diff survives
	// only while both sides still hold.
	VersionID string
	SeenBy    string
	SeenAt    time.Time
}

// LegalDraftPreviews reads every cell previewed for a document, in the
// preimage's order so that callers never have to sort.
func (r *Repository) LegalDraftPreviews(ctx context.Context, document string) ([]LegalDraftPreview, error) {
	const query = `
		SELECT locale, slug, body_digest, previewed_by, previewed_at
		FROM legal_draft_previews
		WHERE document = $1
		ORDER BY locale COLLATE "C", slug
	`

	rows, err := r.db.Pool.QueryContext(ctx, query, document)
	if err != nil {
		return nil, fmt.Errorf("legal draft previews: %w", err)
	}
	defer rows.Close()

	var previews []LegalDraftPreview
	for rows.Next() {
		var preview LegalDraftPreview
		var locale string
		if err := rows.Scan(&locale, &preview.Slug, &preview.BodyDigest, &preview.PreviewedBy, &preview.PreviewedAt); err != nil {
			return nil, fmt.Errorf("legal draft previews: %w", err)
		}
		preview.Locale = platform.Locale(locale)
		previews = append(previews, preview)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legal draft previews: %w", err)
	}
	return previews, nil
}

// RecordLegalDraftPreview remembers that one cell was seen rendered, with the
// digest of the text that was rendered.
//
// AN UPSERT, because previewing the same cell twice is one fact and not two.
// Previewing it again AFTER an edit is what replaces a stale digest with a
// current one, which is the only way an expired preview comes back.
func (r *Repository) RecordLegalDraftPreview(ctx context.Context, document string, preview LegalDraftPreview) error {
	const upsert = `
		INSERT INTO legal_draft_previews (document, locale, slug, body_digest, previewed_by, previewed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (document, locale, slug) DO UPDATE
		SET body_digest = EXCLUDED.body_digest,
		    previewed_by = EXCLUDED.previewed_by,
		    previewed_at = EXCLUDED.previewed_at
	`
	if _, err := r.db.Pool.ExecContext(ctx, upsert,
		document, string(preview.Locale), preview.Slug, preview.BodyDigest, preview.PreviewedBy, preview.PreviewedAt,
	); err != nil {
		return fmt.Errorf("record legal draft preview: %w", err)
	}
	return nil
}

// ErrNoLegalDraftDiffSeen reports that nobody has been shown this draft's diff.
// The ordinary state of a fresh draft, and not a failure.
var ErrNoLegalDraftDiffSeen = errors.New("consent: no legal draft diff seen")

// LegalDraftDiffSeen reads the diff this draft's operator has been shown, if
// any.
func (r *Repository) LegalDraftDiffSeen(ctx context.Context, document string) (LegalDraftDiffSeen, error) {
	const query = `
		SELECT diff_seen_draft_digest, diff_seen_version_id, diff_seen_by, diff_seen_at
		FROM legal_drafts
		WHERE document = $1
	`

	var (
		digest    sql.NullString
		versionID sql.NullString
		seenBy    sql.NullString
		seenAt    sql.NullTime
	)
	err := r.db.Pool.QueryRowContext(ctx, query, document).Scan(&digest, &versionID, &seenBy, &seenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return LegalDraftDiffSeen{}, ErrNoLegalDraft
	}
	if err != nil {
		return LegalDraftDiffSeen{}, fmt.Errorf("legal draft diff seen: %w", err)
	}
	if !digest.Valid || !versionID.Valid {
		return LegalDraftDiffSeen{}, ErrNoLegalDraftDiffSeen
	}
	return LegalDraftDiffSeen{
		DraftDigest: digest.String,
		VersionID:   versionID.String,
		SeenBy:      seenBy.String,
		SeenAt:      seenAt.Time,
	}, nil
}

// RecordLegalDraftDiffSeen remembers that the diff between this draft and that
// published edition was put on screen.
//
// It writes onto the draft row rather than into a table of its own: there is
// exactly one draft per document, so there is exactly one diff to have seen, and
// a second table would only be a second place for it to disagree from.
func (r *Repository) RecordLegalDraftDiffSeen(ctx context.Context, document string, seen LegalDraftDiffSeen) error {
	const update = `
		UPDATE legal_drafts
		SET diff_seen_draft_digest = $2,
		    diff_seen_version_id = $3,
		    diff_seen_by = $4,
		    diff_seen_at = $5
		WHERE document = $1
	`
	result, err := r.db.Pool.ExecContext(ctx, update, document, seen.DraftDigest, seen.VersionID, seen.SeenBy, seen.SeenAt)
	if err != nil {
		return fmt.Errorf("record legal draft diff seen: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record legal draft diff seen: %w", err)
	}
	if affected == 0 {
		// No draft to have seen a diff of. The service refuses before it gets
		// here; this is the backstop that keeps the write from being silent.
		return ErrNoLegalDraft
	}
	return nil
}
