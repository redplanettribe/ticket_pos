package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
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
	// TagKeys is a set of canonical tag keys; an Event matches if it carries any
	// of them (OR within the facet). Empty means no tag constraint.
	TagKeys []string
}

const publicEventColumns = `
	e.id, e.organization_id, e.name, e.slug, e.status,
	e.starts_at, e.ends_at, e.timezone, e.venue_name, e.venue_address,
	e.description, e.cover_image_key, e.cover_video_key, e.discoverable, e.fee_handling, e.created_at,
	o.name, o.slug, o.logo_image_key, o.currency,
	tt.min_price, tt.all_sold_out, tt.ticket_count
`

// publicEventFrom is the shared FROM clause of the public Event queries. Its
// Ticket Type aggregates are hold-aware (ADR 0013): a Ticket Type counts as
// sold out once sold_count plus the quantities live Capacity Holds claim reach
// capacity, so the Storefront never advertises tickets that pending Payments
// already speak for. The holds sub-select is the shared sales.LiveHoldsSQL —
// reading the payments table directly, as the ADR prescribes for the public
// figures. cutoffExpr is the placeholder carrying the hold-window cutoff.
func publicEventFrom(cutoffExpr string) string {
	return `
	FROM events e
	JOIN organizations o ON o.id = e.organization_id
	LEFT JOIN LATERAL (
		SELECT
			MIN(tt.price_cents) AS min_price,
			BOOL_AND(tt.sold_count + COALESCE(h.held, 0) >= tt.capacity) AS all_sold_out,
			COUNT(*) AS ticket_count
		FROM ticket_types tt
		LEFT JOIN (` + sales.LiveHoldsSQL(cutoffExpr, "", "") + `) h ON h.ticket_type_id = tt.id
		WHERE tt.event_id = e.id
	) tt ON TRUE
`
}

func scanPublicEventRow(rows interface {
	Scan(dest ...any) error
}) (*PublicEventRow, error) {
	var row PublicEventRow
	var status string
	if err := rows.Scan(
		&row.ID, &row.OrganizationID, &row.Name, &row.Slug, &status,
		&row.StartsAt, &row.EndsAt, &row.Timezone, &row.VenueName, &row.VenueAddress,
		&row.Description, &row.CoverImageKey, &row.CoverVideoKey, &row.Discoverable, &row.FeeHandling, &row.CreatedAt,
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

// LiveCapacityHolds returns the quantities live Capacity Holds currently claim
// per Ticket Type on an Event: pending Payments created strictly after the
// cutoff (ADR 0013). Ticket Types with no live hold are absent from the map.
// It runs the shared sales.LiveHoldsSQL — the public remaining figures read
// the payments table directly, as the ADR prescribes, so the hold semantics
// stay defined in exactly one place.
func (r *Repository) LiveCapacityHolds(ctx context.Context, eventID string, cutoff time.Time) (map[string]int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, sales.LiveHoldsSQL("$2", "$1", ""), eventID, cutoff)
	if err != nil {
		return nil, err
	}
	return sales.ScanHeldQuantities(rows)
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

	var tagKeys any
	if len(filter.TagKeys) > 0 {
		tagKeys = filter.TagKeys
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom("$9")+`
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
		  AND (
		    $8::text[] IS NULL
		    OR EXISTS (
		      SELECT 1 FROM event_tags et
		      JOIN tags t ON t.id = et.tag_id
		      WHERE et.event_id = e.id AND t.canonical_key = ANY($8)
		    )
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
		tagKeys,
		sales.HoldCutoff(filter.Now),
	)
	if err != nil {
		return nil, err
	}
	return collectPublicEventRows(rows)
}

// ListDiscoverableEventsByOrganization returns all published, discoverable Events
// for one Organization (upcoming and past), soonest first. now anchors the
// hold-window cutoff for the sold-out aggregates.
func (r *Repository) ListDiscoverableEventsByOrganization(ctx context.Context, orgID string, now time.Time) ([]PublicEventRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom("$2")+`
		WHERE e.organization_id = $1
		  AND e.status = 'published'
		  AND e.discoverable = TRUE
		ORDER BY e.starts_at ASC, e.id ASC
	`, orgID, sales.HoldCutoff(now))
	if err != nil {
		return nil, err
	}
	return collectPublicEventRows(rows)
}

// GetPublishedEventBySlug loads a single published Event by Organization and Event
// slug for the Storefront event page. Discoverability is not required: a published
// Event is always reachable by direct link. now anchors the hold-window cutoff
// for the sold-out aggregates.
func (r *Repository) GetPublishedEventBySlug(ctx context.Context, orgSlug, eventSlug string, now time.Time) (*PublicEventRow, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom("$3")+`
		WHERE o.slug = $1
		  AND e.slug = $2
		  AND e.status = 'published'
	`, orgSlug, eventSlug, sales.HoldCutoff(now))
	result, err := scanPublicEventRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}
