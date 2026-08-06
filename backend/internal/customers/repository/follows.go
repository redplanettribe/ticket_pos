package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// OrganizationFollowRow is one Follow of an Organization, joined to enough of
// the Organization to render it: the same three public facts the Organization's
// own public profile publishes, and no id.
//
// The absence of an Organization id here is deliberate and matches the API
// surface above it. A Follow is addressed by the slug the Customer already sees
// in the address bar; nothing in this feature needs the internal identifier, and
// publishing one on a Customer-facing read would invite a client to key on it.
type OrganizationFollowRow struct {
	FollowedAt   time.Time
	Name         string
	Slug         string
	LogoImageKey sql.NullString
}

// TagFollowRow is one Follow of a Tag, joined to enough of the Tag to render it:
// the same three facts every other Tag surface publishes, and no id.
//
// The English display name alone, with no Spanish beside it. ADR 0027 puts
// Preset Tag copy in the Storefront's message catalogues keyed on the canonical
// key, and that is why the key is here — it is what a page words the Tag from,
// and what a copy edit to the display name cannot break. The name is the
// fallback for a Tag the catalogue does not know, which is every Custom Tag. The
// Spanish column that ADR 0030 added to `tags` is for the Follow Digest, which
// is mail and has no page to word it; nothing on this wire has a language.
type TagFollowRow struct {
	FollowedAt   time.Time
	CanonicalKey string
	DisplayName  string
	Curated      bool
}

// FollowOrganization records that a Customer Follows an Organization and returns
// the instant they began to, whether that is now or was some earlier visit.
//
// The ON CONFLICT clause is what makes the Follow idempotent, and the shape of
// it is the point. A plain DO NOTHING would return no row at all on a repeat,
// leaving the caller to issue a second read; a DO UPDATE that set followed_at to
// the new instant would let a retried request — a double tap, a client retry, a
// browser replaying a request after a flaky connection — quietly rewrite when
// the person subscribed. So the update writes the column back to its own stored
// value: the row is returned, the instant does not move, and following twice
// leaves exactly one Follow that still remembers the first time (#217).
func (r *Repository) FollowOrganization(ctx context.Context, customerID, organizationID string, now time.Time) (time.Time, error) {
	var followedAt time.Time
	err := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO customer_organization_follows (customer_id, organization_id, followed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (customer_id, organization_id)
		DO UPDATE SET followed_at = customer_organization_follows.followed_at
		RETURNING followed_at
	`, customerID, organizationID, now).Scan(&followedAt)
	if err != nil {
		return time.Time{}, err
	}
	return followedAt.UTC(), nil
}

// UnfollowOrganization removes one Follow. Removing a Follow that is not there
// is not an error and not reported as one: the caller asked for a state, and
// that state already holds.
func (r *Repository) UnfollowOrganization(ctx context.Context, customerID, organizationID string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM customer_organization_follows
		WHERE customer_id = $1 AND organization_id = $2
	`, customerID, organizationID)
	return err
}

// GetOrganizationFollow returns one Customer's Follow of one Organization, or
// nil when they do not Follow it. It exists so a write can answer with the same
// view the listing renders, rather than with a shape only this endpoint speaks.
func (r *Repository) GetOrganizationFollow(ctx context.Context, customerID, organizationID string) (*OrganizationFollowRow, error) {
	var row OrganizationFollowRow
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT f.followed_at, o.name, o.slug, o.logo_image_key
		FROM customer_organization_follows f
		JOIN organizations o ON o.id = f.organization_id
		WHERE f.customer_id = $1 AND f.organization_id = $2
	`, customerID, organizationID).Scan(&row.FollowedAt, &row.Name, &row.Slug, &row.LogoImageKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.FollowedAt = row.FollowedAt.UTC()
	return &row, nil
}

// ListOrganizationFollowsForCustomer returns everything one Customer Follows,
// most recently followed first.
//
// The customer id is the only scope, exactly as in the Customer Area read: there
// is no argument here by which one Customer's list could be aimed at another's.
//
// The ordering is `followed_at DESC` because the list is a record of decisions
// in the order they were made, and the most recent one is the one a person is
// most likely to be looking for having just made it. Ties — two Follows in the
// same transactional instant, which the fixed clock in tests makes ordinary —
// break on the slug so the order is total and a listing never shuffles between
// two reads.
func (r *Repository) ListOrganizationFollowsForCustomer(ctx context.Context, customerID string) ([]OrganizationFollowRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT f.followed_at, o.name, o.slug, o.logo_image_key
		FROM customer_organization_follows f
		JOIN organizations o ON o.id = f.organization_id
		WHERE f.customer_id = $1
		ORDER BY f.followed_at DESC, o.slug ASC
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	follows := []OrganizationFollowRow{}
	for rows.Next() {
		var row OrganizationFollowRow
		if err := rows.Scan(&row.FollowedAt, &row.Name, &row.Slug, &row.LogoImageKey); err != nil {
			return nil, err
		}
		row.FollowedAt = row.FollowedAt.UTC()
		follows = append(follows, row)
	}
	return follows, rows.Err()
}

// FollowTag records that a Customer Follows a Tag and returns the instant they
// began to (#218).
//
// Idempotent by the same ON CONFLICT as FollowOrganization, and for the same
// reasons: the row comes back on a repeat so no second read is needed, and the
// update writes followed_at back to its stored value so a retry cannot move when
// the person subscribed. The two are deliberately identical — a Customer cannot
// be expected to learn that one Follow control is safe to double-tap and the
// other is not.
func (r *Repository) FollowTag(ctx context.Context, customerID, tagID string, now time.Time) (time.Time, error) {
	var followedAt time.Time
	err := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO customer_tag_follows (customer_id, tag_id, followed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (customer_id, tag_id)
		DO UPDATE SET followed_at = customer_tag_follows.followed_at
		RETURNING followed_at
	`, customerID, tagID, now).Scan(&followedAt)
	if err != nil {
		return time.Time{}, err
	}
	return followedAt.UTC(), nil
}

// UnfollowTag removes one Follow of a Tag. Removing one that is not there is not
// an error: the caller asked for a state, and that state already holds.
func (r *Repository) UnfollowTag(ctx context.Context, customerID, tagID string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM customer_tag_follows
		WHERE customer_id = $1 AND tag_id = $2
	`, customerID, tagID)
	return err
}

// GetTagFollow returns one Customer's Follow of one Tag, or nil when they do not
// Follow it, so a write can answer with the same view the listing renders.
func (r *Repository) GetTagFollow(ctx context.Context, customerID, tagID string) (*TagFollowRow, error) {
	var row TagFollowRow
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT f.followed_at, t.canonical_key, t.display_name, t.curated
		FROM customer_tag_follows f
		JOIN tags t ON t.id = f.tag_id
		WHERE f.customer_id = $1 AND f.tag_id = $2
	`, customerID, tagID).Scan(&row.FollowedAt, &row.CanonicalKey, &row.DisplayName, &row.Curated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.FollowedAt = row.FollowedAt.UTC()
	return &row, nil
}

// ListTagFollowsForCustomer returns every Tag one Customer Follows, most
// recently followed first.
//
// Same shape and same ordering as the Organization listing beside it, ties
// broken on the canonical key where that one breaks on the slug — each kind
// ordered by the identifier it is addressed by. The two are merged into one list
// above this layer, which is where the order across kinds is settled; ordering
// each half here is what leaves that merge with nothing to decide but the
// interleaving.
func (r *Repository) ListTagFollowsForCustomer(ctx context.Context, customerID string) ([]TagFollowRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT f.followed_at, t.canonical_key, t.display_name, t.curated
		FROM customer_tag_follows f
		JOIN tags t ON t.id = f.tag_id
		WHERE f.customer_id = $1
		ORDER BY f.followed_at DESC, t.canonical_key ASC
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	follows := []TagFollowRow{}
	for rows.Next() {
		var row TagFollowRow
		if err := rows.Scan(&row.FollowedAt, &row.CanonicalKey, &row.DisplayName, &row.Curated); err != nil {
			return nil, err
		}
		row.FollowedAt = row.FollowedAt.UTC()
		follows = append(follows, row)
	}
	return follows, rows.Err()
}
