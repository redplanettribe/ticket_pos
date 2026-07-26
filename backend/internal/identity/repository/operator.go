package repository

import "context"

// IsPlatformOperator reports whether an email is on the platform operator
// allowlist (ADR 0015). The allowlist is the whole of operator authority: there
// is no row to update and no role to check, only presence.
func (r *Repository) IsPlatformOperator(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM platform_operators WHERE email = $1)
	`, email).Scan(&exists)
	return exists, err
}

// ListAllOrganizations returns one page of every Organization on the platform,
// name-ascending with an id tiebreaker so equal names keep a stable order across
// pages, plus the unpaginated total (ADR 0006). Unlike every other listing in
// this module it is not scoped to a Member: only the Platform Operator surface
// reaches it, and the gate for that lives in the middleware.
func (r *Repository) ListAllOrganizations(ctx context.Context, limit, offset int) ([]Organization, int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, name, slug, currency, logo_image_key, created_at, COUNT(*) OVER() AS total
		FROM organizations
		ORDER BY name ASC, id ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		out   []Organization
		total int
	)
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.LogoImageKey, &o.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	return out, total, rows.Err()
}
