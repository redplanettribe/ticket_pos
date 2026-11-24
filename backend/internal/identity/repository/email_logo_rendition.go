package repository

import (
	"context"
)

// The Logo email rendition (migration 145, spec #870, #877): recorded after
// the save that changed the Logo has committed, and found again by the
// rendition fill when it is still missing.

// RecordEmailLogoRendition records key as the Organization's Logo rendition,
// only if the Logo is still source and no rendition has been recorded since.
// It reports whether the row took the key; false means a later save moved on
// (or a concurrent render won), and the caller deletes its object.
func (r *Repository) RecordEmailLogoRendition(ctx context.Context, organizationID, source, key string) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE organizations SET email_logo_key = $3
		WHERE id = $1 AND logo_image_key = $2 AND email_logo_key IS NULL
	`, organizationID, source, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// OrganizationLogo is an Organization's id and Logo key, as the rendition fill
// needs them.
type OrganizationLogo struct {
	OrganizationID string
	LogoKey        string
}

// ListOrganizationsMissingEmailLogo returns up to limit Organizations with a
// Logo whose rendition is null, in id order after afterID (empty for the
// start). Paging by id means a Logo that fails to render cannot hold up the
// rest.
func (r *Repository) ListOrganizationsMissingEmailLogo(ctx context.Context, afterID string, limit int) ([]OrganizationLogo, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, logo_image_key
		FROM organizations
		WHERE logo_image_key IS NOT NULL AND email_logo_key IS NULL
		  AND ($1::text = '' OR id > NULLIF($1::text, '')::uuid)
		ORDER BY id
		LIMIT $2
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logos []OrganizationLogo
	for rows.Next() {
		var l OrganizationLogo
		if err := rows.Scan(&l.OrganizationID, &l.LogoKey); err != nil {
			return nil, err
		}
		logos = append(logos, l)
	}
	return logos, rows.Err()
}

// CountOrganizationsMissingEmailLogo counts the Organizations the fill would
// still work on.
func (r *Repository) CountOrganizationsMissingEmailLogo(ctx context.Context) (int, error) {
	var n int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM organizations
		WHERE logo_image_key IS NOT NULL AND email_logo_key IS NULL
	`).Scan(&n)
	return n, err
}
