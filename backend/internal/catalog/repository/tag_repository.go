package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Tag is a discovery facet in the shared, system-wide pool.
type Tag struct {
	ID           string
	CanonicalKey string
	DisplayName  string
	Curated      bool
	// DisplayNameES is the Tag's name in Spanish, set for Preset Tags alone and
	// null for every Custom Tag — a column constraint holds that, and ADR 0027
	// holds why (an Organization's own word is read as coined in every Locale).
	// Null on a Preset Tag too, for one promoted by flipping curated rather than
	// by a migration; resolution falls back to DisplayName there.
	DisplayNameES sql.NullString
}

// NormalizedTag is a resolved tag name ready to upsert into the pool: its
// canonical key (for uniqueness) and the display casing to store on first coin.
type NormalizedTag struct {
	CanonicalKey string
	DisplayName  string
}

const tagColumns = `id, canonical_key, display_name, curated, display_name_es`

// SearchTags returns pool Tags whose canonical key contains the (already
// canonicalized) query. Preset Tags come first, then the most-used Tags (by
// Event association count) so an established Tag outranks a near-duplicate
// one-off. An empty query returns the top Tags by that same ranking.
func (r *Repository) SearchTags(ctx context.Context, canonicalQuery string, limit int) ([]Tag, error) {
	like := "%" + escapeLike(canonicalQuery) + "%"
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+tagColumnsPrefixed("t")+`
		FROM tags t
		LEFT JOIN (
			SELECT tag_id, COUNT(*) AS cnt FROM event_tags GROUP BY tag_id
		) u ON u.tag_id = t.id
		WHERE t.canonical_key LIKE $1 ESCAPE '\'
		ORDER BY t.curated DESC, COALESCE(u.cnt, 0) DESC, t.display_name ASC
		LIMIT $2
	`, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]Tag, 0)
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated, &t.DisplayNameES); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListPopularCustomTags returns Custom Tags carried by at least one Event,
// most-used first, so the staff editor can surface the established custom
// vocabulary from across all Organizations before the organizer types. Usage
// counts every Event regardless of status; Preset Tags are excluded (they have
// their own always-visible chip row) and zero-usage Tags are omitted.
func (r *Repository) ListPopularCustomTags(ctx context.Context, limit int) ([]Tag, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+tagColumnsPrefixed("t")+`
		FROM tags t
		JOIN (
			SELECT tag_id, COUNT(*) AS cnt FROM event_tags GROUP BY tag_id
		) u ON u.tag_id = t.id
		WHERE t.curated = FALSE
		ORDER BY u.cnt DESC, t.display_name ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]Tag, 0)
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated, &t.DisplayNameES); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListAvailablePresetTags returns the Preset Tags (curated) carried by at least
// one published, discoverable, not-yet-ended Event, ordered by display name. It
// backs the explorer's derived chip bar: presets with no matching Event are
// omitted so no chip dead-ends. The pool is evaluated against now only (no q or
// date facet) so the bar stays stable as visitors refine their search.
func (r *Repository) ListAvailablePresetTags(ctx context.Context, now time.Time) ([]Tag, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+tagColumnsPrefixed("t")+`
		FROM tags t
		WHERE t.curated = TRUE
		  AND EXISTS (
		    SELECT 1
		    FROM event_tags et
		    JOIN events e ON e.id = et.event_id
		    WHERE et.tag_id = t.id
		      AND e.status = 'published'
		      AND e.discoverable = TRUE
		      AND COALESCE(e.ends_at, e.starts_at) >= $1
		  )
		ORDER BY t.display_name ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]Tag, 0)
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated, &t.DisplayNameES); err != nil {
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
		if err := rows.Scan(&t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated, &t.DisplayNameES); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListTagsByEventIDs batch-loads the Tags for a set of Events, keyed by Event
// ID, so listings can attach tags without a query per row. Each Event's Tags
// are Preset Tags first (curated DESC, display_name ASC). Events with no Tags
// are simply absent from the map.
func (r *Repository) ListTagsByEventIDs(ctx context.Context, eventIDs []string) (map[string][]Tag, error) {
	result := make(map[string][]Tag, len(eventIDs))
	if len(eventIDs) == 0 {
		return result, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT et.event_id, `+tagColumnsPrefixed("t")+`
		FROM event_tags et
		JOIN tags t ON t.id = et.tag_id
		WHERE et.event_id = ANY($1)
		ORDER BY t.curated DESC, t.display_name ASC
	`, eventIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var eventID string
		var t Tag
		if err := rows.Scan(&eventID, &t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated, &t.DisplayNameES); err != nil {
			return nil, err
		}
		result[eventID] = append(result[eventID], t)
	}
	return result, rows.Err()
}

// ListTagsByCanonicalKeys loads pool Tags for a set of canonical keys, in one
// query and in no particular order. A key naming no Tag is simply absent, which
// is the same degradation the Storefront's catalogue makes when it holds no copy
// for a key: a name nobody holds is not an error, it is a Tag that is gone.
func (r *Repository) ListTagsByCanonicalKeys(ctx context.Context, canonicalKeys []string) ([]Tag, error) {
	if len(canonicalKeys) == 0 {
		return []Tag{}, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+tagColumns+`
		FROM tags
		WHERE canonical_key = ANY($1)
	`, canonicalKeys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]Tag, 0, len(canonicalKeys))
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.CanonicalKey, &t.DisplayName, &t.Curated, &t.DisplayNameES); err != nil {
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
	return alias + ".id, " + alias + ".canonical_key, " + alias + ".display_name, " + alias + ".curated, " + alias + ".display_name_es"
}

// escapeLike escapes LIKE wildcards so a query is matched literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}
