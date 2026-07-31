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

// OrganizationsByIDs returns the named Organizations, unscoped by Membership,
// for an operator list whose rows arrived from another module.
//
// It exists so a cross-Organization list costs ONE read of this table rather
// than one per row: the operator's Payout Request queue (#176) is fifty rows
// belonging to up to fifty Organizations, and resolving them one at a time
// would be the classic N+1 in the middle of the surface an operator opens most
// often. It is the batch twin of GetOrganizationByID, and the caller decides
// what an id with no row means — here, nothing does: the requests reference
// `organizations` and cascade with it.
//
// An unknown or malformed id is silently absent rather than an error, which is
// what makes the caller's join a lookup rather than a second failure path.
func (r *Repository) OrganizationsByIDs(ctx context.Context, ids []string) ([]Organization, error) {
	out := make([]Organization, 0, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, name, slug, currency, logo_image_key, created_at
		FROM organizations
		WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.LogoImageKey, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
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
