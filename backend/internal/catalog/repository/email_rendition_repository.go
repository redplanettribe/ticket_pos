package repository

import (
	"context"
)

// The email renditions (migration 144, spec #870): recorded after the save
// that changed an original has committed, and found again by the rendition
// fill when they are still missing.

// missingEmailRendition is the predicate the fill works through: an original
// whose rendition is null. The square derives from the Thumbnail, else the
// Cover Image.
const missingEmailRendition = `
	((cover_image_key IS NOT NULL AND email_wide_key IS NULL)
	 OR (COALESCE(thumbnail_key, cover_image_key) IS NOT NULL AND email_square_key IS NULL))
`

// RecordEmailWideRendition records key as the Event's wide rendition, only if
// the Cover Image is still source and no wide rendition has been recorded
// since. It reports whether the row took the key; false means a later save
// moved on (or a concurrent render won), and the caller deletes its object.
func (r *Repository) RecordEmailWideRendition(ctx context.Context, eventID, source, key string) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE events SET email_wide_key = $3
		WHERE id = $1 AND cover_image_key = $2 AND email_wide_key IS NULL
	`, eventID, source, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// RecordEmailSquareRendition records key as the Event's square rendition, on
// the terms of RecordEmailWideRendition, with source being the image the
// square derives from: the Thumbnail, else the Cover Image.
func (r *Repository) RecordEmailSquareRendition(ctx context.Context, eventID, source, key string) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE events SET email_square_key = $3
		WHERE id = $1 AND COALESCE(thumbnail_key, cover_image_key) = $2 AND email_square_key IS NULL
	`, eventID, source, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ListEventsMissingEmailRenditions returns up to limit Events, across every
// Organization, with an original whose rendition is null, in id order after
// afterID (empty for the start). Paging by id rather than re-reading the
// front means an original that fails to render cannot hold up the rest.
func (r *Repository) ListEventsMissingEmailRenditions(ctx context.Context, afterID string, limit int) ([]Event, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+eventColumns+`
		FROM events
		WHERE `+missingEmailRendition+`
		  AND ($1::text = '' OR id > NULLIF($1::text, '')::uuid)
		ORDER BY id
		LIMIT $2
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *e)
	}
	return events, rows.Err()
}

// CountEventsMissingEmailRenditions counts the Events the fill would still
// work on.
func (r *Repository) CountEventsMissingEmailRenditions(ctx context.Context) (int, error) {
	var n int
	err := r.db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE `+missingEmailRendition).Scan(&n)
	return n, err
}
