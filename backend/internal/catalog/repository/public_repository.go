package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PublicOrganization is a read-only Organization projection for Storefront listings.
type PublicOrganization struct {
	ID           string
	Name         string
	Slug         string
	LogoImageKey sql.NullString
	Currency     string
}

// PublicEventRow is an Event enriched with its Organization and Ticket Type
// aggregates for public listing and detail views.
type PublicEventRow struct {
	Event
	OrgName       string
	OrgSlug       string
	OrgLogoKey    sql.NullString
	OrgCurrency   string
	MinPriceCents sql.NullInt64
	AllSoldOut    sql.NullBool
	TicketCount   int
}

// PublicEventFilter constrains the global Storefront explorer query.
type PublicEventFilter struct {
	Query          string
	From           *time.Time
	To             *time.Time
	Now            time.Time
	Limit          int
	CursorStartsAt *time.Time
	CursorID       string
}

const publicEventColumns = `
	e.id, e.organization_id, e.name, e.slug, e.status,
	e.starts_at, e.ends_at, e.timezone, e.venue_name, e.venue_address,
	e.description, e.cover_image_key, e.discoverable, e.created_at,
	o.name, o.slug, o.logo_image_key, o.currency,
	tt.min_price, tt.all_sold_out, tt.ticket_count
`

const publicEventFrom = `
	FROM events e
	JOIN organizations o ON o.id = e.organization_id
	LEFT JOIN LATERAL (
		SELECT
			MIN(price_cents) AS min_price,
			BOOL_AND(sold_count >= capacity) AS all_sold_out,
			COUNT(*) AS ticket_count
		FROM ticket_types
		WHERE event_id = e.id
	) tt ON TRUE
`

func scanPublicEventRow(rows interface {
	Scan(dest ...any) error
}) (*PublicEventRow, error) {
	var row PublicEventRow
	var status string
	if err := rows.Scan(
		&row.ID, &row.OrganizationID, &row.Name, &row.Slug, &status,
		&row.StartsAt, &row.EndsAt, &row.Timezone, &row.VenueName, &row.VenueAddress,
		&row.Description, &row.CoverImageKey, &row.Discoverable, &row.CreatedAt,
		&row.OrgName, &row.OrgSlug, &row.OrgLogoKey, &row.OrgCurrency,
		&row.MinPriceCents, &row.AllSoldOut, &row.TicketCount,
	); err != nil {
		return nil, err
	}
	row.Status = EventStatus(status)
	return &row, nil
}

func collectPublicEventRows(rows *sql.Rows) ([]PublicEventRow, error) {
	defer rows.Close()
	var items []PublicEventRow
	for rows.Next() {
		row, err := scanPublicEventRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *row)
	}
	return items, rows.Err()
}

// GetPublicOrganizationBySlug loads an Organization projection for the Storefront.
func (r *Repository) GetPublicOrganizationBySlug(ctx context.Context, slug string) (*PublicOrganization, error) {
	var o PublicOrganization
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, name, slug, logo_image_key, currency
		FROM organizations
		WHERE slug = $1
	`, slug).Scan(&o.ID, &o.Name, &o.Slug, &o.LogoImageKey, &o.Currency)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// ListDiscoverableEvents returns published, discoverable, not-yet-ended Events
// across all Organizations, soonest first, for the global explorer.
func (r *Repository) ListDiscoverableEvents(ctx context.Context, filter PublicEventFilter) ([]PublicEventRow, error) {
	var cursorID any
	if filter.CursorID != "" {
		cursorID = filter.CursorID
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom+`
		WHERE e.status = 'published'
		  AND e.discoverable = TRUE
		  AND COALESCE(e.ends_at, e.starts_at) >= $1
		  AND ($2 = '' OR e.name ILIKE '%' || $2 || '%' OR o.name ILIKE '%' || $2 || '%')
		  AND ($3::timestamptz IS NULL OR e.starts_at >= $3)
		  AND ($4::timestamptz IS NULL OR e.starts_at <= $4)
		  AND (
		    $5::timestamptz IS NULL
		    OR e.starts_at > $5
		    OR (e.starts_at = $5 AND e.id > $6::uuid)
		  )
		ORDER BY e.starts_at ASC, e.id ASC
		LIMIT $7
	`,
		filter.Now,
		filter.Query,
		filter.From,
		filter.To,
		filter.CursorStartsAt,
		cursorID,
		filter.Limit,
	)
	if err != nil {
		return nil, err
	}
	return collectPublicEventRows(rows)
}

// ListDiscoverableEventsByOrganization returns all published, discoverable Events
// for one Organization (upcoming and past), soonest first.
func (r *Repository) ListDiscoverableEventsByOrganization(ctx context.Context, orgID string) ([]PublicEventRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom+`
		WHERE e.organization_id = $1
		  AND e.status = 'published'
		  AND e.discoverable = TRUE
		ORDER BY e.starts_at ASC, e.id ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	return collectPublicEventRows(rows)
}

// GetPublishedEventBySlug loads a single published Event by Organization and Event
// slug for the Storefront event page. Discoverability is not required: a published
// Event is always reachable by direct link.
func (r *Repository) GetPublishedEventBySlug(ctx context.Context, orgSlug, eventSlug string) (*PublicEventRow, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom+`
		WHERE o.slug = $1
		  AND e.slug = $2
		  AND e.status = 'published'
	`, orgSlug, eventSlug)
	result, err := scanPublicEventRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}
