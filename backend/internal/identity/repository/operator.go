package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

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

// ListPlatformOperatorEmails returns every address on the platform operator
// allowlist, alphabetically.
//
// It is the allowlist read as a RECIPIENT LIST rather than as an authority
// check, and it has exactly one caller: the notice that tells the operators an
// Organization has asked to be paid (#179, ADR 0026). There is no subscription
// table and no preference to consult — presence on the allowlist is what makes
// somebody an operator (ADR 0015), so it is also what makes them somebody to
// tell.
//
// An empty allowlist returns no rows and no error. A platform with no operators
// has nobody to notify, which is a fact about the deployment rather than a
// failure of the request that provoked the read.
func (r *Repository) ListPlatformOperatorEmails(ctx context.Context) ([]string, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT email FROM platform_operators ORDER BY email ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		out = append(out, email)
	}
	return out, rows.Err()
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
		SELECT id, name, slug, currency, logo_image_key, house_designated_by, house_designated_at, created_at
		FROM organizations
		WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.LogoImageKey, &o.HouseDesignatedBy, &o.HouseDesignatedAt, &o.CreatedAt); err != nil {
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
		SELECT id, name, slug, currency, logo_image_key, house_designated_by, house_designated_at, created_at, COUNT(*) OVER() AS total
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
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.LogoImageKey, &o.HouseDesignatedBy, &o.HouseDesignatedAt, &o.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	return out, total, rows.Err()
}

// DesignateHouseOrganization stamps the House Organization designation on an
// Organization that does not carry one, and leaves one that does exactly as it
// is (#472, ADR 0060). The trail names the act that made it a House
// Organization, so a second designation by a colleague — the same toggle
// pressed while already on — rewrites nothing. Returns the Organization as it
// now stands, nil for an unknown id.
func (r *Repository) DesignateHouseOrganization(ctx context.Context, orgID, operator string, at time.Time) (*Organization, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE organizations
		SET house_designated_by = COALESCE(house_designated_by, $2),
		    house_designated_at = COALESCE(house_designated_at, $3)
		WHERE id = $1
		RETURNING id, name, slug, currency, logo_image_key, support_whatsapp, house_designated_by, house_designated_at, created_at
	`, orgID, operator, at)
	return scanOrganization(row)
}

// ClearHouseDesignation takes the designation back, emptying both halves of
// the trail together. Clearing what is already clear is not a refusal: the
// Organization is returned as it stands either way, nil for an unknown id.
func (r *Repository) ClearHouseDesignation(ctx context.Context, orgID string) (*Organization, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE organizations
		SET house_designated_by = NULL,
		    house_designated_at = NULL
		WHERE id = $1
		RETURNING id, name, slug, currency, logo_image_key, support_whatsapp, house_designated_by, house_designated_at, created_at
	`, orgID)
	return scanOrganization(row)
}

func scanOrganization(row *sql.Row) (*Organization, error) {
	var o Organization
	if err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Currency, &o.LogoImageKey, &o.SupportWhatsApp, &o.HouseDesignatedBy, &o.HouseDesignatedAt, &o.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}
