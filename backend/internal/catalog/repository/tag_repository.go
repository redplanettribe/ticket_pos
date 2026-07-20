package repository

import (
	"context"
	"strings"
)

// Tag is a discovery facet in the shared, system-wide pool.
type Tag struct {
	ID           string
	CanonicalKey string
	DisplayName  string
	Curated      bool
}

// NormalizedTag is a resolved tag name ready to upsert into the pool: its
// canonical key (for uniqueness) and the display casing to store on first coin.
type NormalizedTag struct {
	CanonicalKey string
	DisplayName  string
}

const tagColumns = `id, canonical_key, display_name, curated`

// SearchTags returns pool Tags whose canonical key contains the (already
// canonicalized) query, Preset Tags first. An empty query returns the top Tags.
func (r *Repository) SearchTags(ctx context.Context, canonicalQuery string, limit int) ([]Tag, error) {
	like := "%" + escapeLike(canonicalQuery) + "%"
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+tagColumns+`
		FROM tags
		WHERE canonical_key LIKE $1 ESCAPE '\'
		ORDER BY curated DESC, display_name ASC
		LIMIT $2
	`, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]Tag, 0)
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListEventTags returns the Tags assigned to an Event, Preset Tags first.
func (r *Repository) ListEventTags(ctx context.Context, eventID string) ([]Tag, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+tagColumnsPrefixed("t")+`
		FROM event_tags et
		JOIN tags t ON t.id = et.tag_id
		WHERE et.event_id = $1
		ORDER BY t.curated DESC, t.display_name ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]Tag, 0)
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// SetEventTags replaces an Event's tag set with the given normalized Tags,
// coining any that do not yet exist in the shared pool (as Custom Tags). It
// runs in a single transaction and returns the Event's resulting Tags. On
// conflict the existing display casing is preserved (first coiner wins).
func (r *Repository) SetEventTags(ctx context.Context, eventID string, tags []NormalizedTag) ([]Tag, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	tagIDs := make([]string, 0, len(tags))
	for _, nt := range tags {
		var id string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO tags (canonical_key, display_name)
			VALUES ($1, $2)
			ON CONFLICT (canonical_key) DO UPDATE SET display_name = tags.display_name
			RETURNING id
		`, nt.CanonicalKey, nt.DisplayName).Scan(&id); err != nil {
			return nil, err
		}
		tagIDs = append(tagIDs, id)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM event_tags WHERE event_id = $1`, eventID); err != nil {
		return nil, err
	}
	for _, tagID := range tagIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO event_tags (event_id, tag_id) VALUES ($1, $2)
		`, eventID, tagID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.ListEventTags(ctx, eventID)
}

func tagColumnsPrefixed(alias string) string {
	return alias + ".id, " + alias + ".canonical_key, " + alias + ".display_name, " + alias + ".curated"
}

// escapeLike escapes LIKE wildcards so a query is matched literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}
