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
	OrgName    string
	OrgSlug    string
	OrgLogoKey sql.NullString
	// OrgSupportWhatsApp is the Organization's Support WhatsApp number (migration
	// 049). Selected by every query below because they share one column list and
	// one scanner, but READ only when building the Event detail: the listings
	// deliberately do not publish it. See ADR 0029 and the summary it builds.
	OrgSupportWhatsApp sql.NullString
	OrgCurrency        string
	MinPriceCents      sql.NullInt64
	AllSoldOut         sql.NullBool
	// AllClosed is true when every one of the Event's Ticket Types is past its
	// Sales Cutoff (ADR 0070). Invalid on an Event with no Ticket Types at all,
	// which reads as not closed: an Event that sells nothing has not stopped
	// selling anything.
	AllClosed   sql.NullBool
	TicketCount int
	// TicketsSold is the Event's Tickets Sold figure (ADR 0072): the Ticket Sale
	// Line quantities of its active Ticket Sales on every Sales Channel, summed
	// from the maintained per-Ticket-Type sold_count. Zero over an Event with no
	// Ticket Types. Raw and unfloored here; the service decides whether it is
	// enough to publish.
	TicketsSold int
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
	e.description, e.cover_image_key, e.cover_video_key, e.discoverable, e.fee_handling,
	e.registration_mode, e.registration_url, e.created_at,
	o.name, o.slug, o.logo_image_key, o.support_whatsapp, o.currency,
	tt.min_price, tt.all_sold_out, tt.all_closed, tt.ticket_count, tt.tickets_sold
`

// publicEventFrom is the shared FROM clause of the public Event queries. Its
// Ticket Type aggregates are hold-aware (ADR 0013): a Ticket Type counts as
// sold out once sold_count plus the quantities live Capacity Holds claim reach
// capacity, so the Storefront never advertises tickets that pending Payments
// already speak for. The holds sub-select is the shared sales.LiveHoldsSQL —
// reading the payments table directly, as the ADR prescribes for the public
// figures. cutoffExpr is the placeholder carrying the hold-window cutoff.
//
// min_price is the cheapest *effective* base price (ADR 0021): a Ticket Type
// under a live Promotion contributes its Promotional Price, so a listing card's
// "from" price is the cheapest ticket a Customer could actually buy right now
// rather than the cheapest List Price. The window is evaluated against
// nowExpr — the placeholder carrying the service's injected clock, never SQL
// now() — and is half-open, start inclusive and end exclusive, the same rule
// catalog.EffectiveBasePriceCents applies everywhere else. At most one
// Promotion exists per Ticket Type (a UNIQUE slot), so the join cannot fan the
// aggregate out.
//
// THE DERIVED FIGURES NARROW TO THE TICKET TYPES STILL OPEN IN TIME (ADR 0070).
// A Ticket Type past its Sales Cutoff is one nobody can buy, so it contributes
// neither a "from" price — the cheapest price a Customer could actually pay
// right now, which is the rule ADR 0021 wrote for Promotions applied to a wider
// question — nor a vote in the sold-out roll-up. That narrowing lands HERE, in
// the one subquery all three public readers share, rather than in three Go call
// sites that could drift apart.
//
// Judging all_sold_out over the still-open Ticket Types alone is what makes a
// mixed Event — some closed, the rest exhausted — read SOLD OUT: the FILTER
// leaves only the open ones, and they are all spoken for. An Event whose every
// Ticket Type has closed has nothing left to judge, so all_sold_out comes back
// NULL and lands on false, which is right — such an Event reads closed, and
// telling a half-empty room it is full is a claim the Organization has to
// answer for. all_closed is the separate BOOL_AND that says so.
//
// The closed test is the SQL statement of catalog.ClosedAt, and the only place
// that predicate is expressed twice: half-open at the closing end, so the
// cutoff instant itself is closed, and a NULL cutoff never closes. It is
// evaluated against nowExpr — the same injected clock the Promotion window
// reads, never SQL now().
//
// TICKETS_SOLD IS SUMMED FROM sold_count, AND THAT DIFFERS FROM THE STAFF
// STRIP ON PURPOSE (ADR 0072). The staff Sales summary sums the same figure off
// the Ticket Sale Lines, because the strip sits above the Sales list and an
// Org Admin reconciles the two by eye; a figure that could disagree with the
// rows beneath it would be worse than none. The public reads have no such
// list. They take the cheaper equivalent — the per-Ticket-Type sold_count the
// sale commit, the Sale Reversal and the Sale Import undo all maintain in the
// same transaction as the lines — as one more aggregate in a lateral all three
// readers already run, so the explorer gains no join and reads no sale lines.
// Both sources are kept correct by the same transactional maintenance, and
// nothing here is a bug waiting to be "fixed" into the slower query.
//
// Unlike min_price and all_sold_out it is NOT narrowed by the closed
// predicate: a Ticket Type past its Sales Cutoff can no longer be bought, but
// the tickets it sold are still going. Live Capacity Holds are likewise not in
// it — a pending Payment is not a sale — so the figure can sit beneath
// capacity on an Event all_sold_out already calls full.
func publicEventFrom(cutoffExpr, nowExpr string) string {
	closed := `(tt.sales_cutoff_at IS NOT NULL AND tt.sales_cutoff_at <= ` + nowExpr + `)`
	return `
	FROM events e
	JOIN organizations o ON o.id = e.organization_id
	LEFT JOIN LATERAL (
		SELECT
			MIN(CASE
				WHEN (p.starts_at IS NULL OR p.starts_at <= ` + nowExpr + `)
				 AND ` + nowExpr + ` < p.ends_at
				THEN p.promotional_price_cents
				ELSE tt.price_cents
			END) FILTER (WHERE NOT ` + closed + `) AS min_price,
			BOOL_AND(tt.sold_count + COALESCE(h.held, 0) >= tt.capacity)
				FILTER (WHERE NOT ` + closed + `) AS all_sold_out,
			BOOL_AND(` + closed + `) AS all_closed,
			COUNT(*) AS ticket_count,
			COALESCE(SUM(tt.sold_count), 0) AS tickets_sold
		FROM ticket_types tt
		LEFT JOIN ticket_type_promotions p ON p.ticket_type_id = tt.id
		LEFT JOIN (` + sales.LiveHoldsSQL(sales.HoldsFilter{CutoffExpr: cutoffExpr}) + `) h ON h.ticket_type_id = tt.id
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
		&row.Description, &row.CoverImageKey, &row.CoverVideoKey, &row.Discoverable, &row.FeeHandling,
		&row.RegistrationMode, &row.RegistrationURL, &row.CreatedAt,
		&row.OrgName, &row.OrgSlug, &row.OrgLogoKey, &row.OrgSupportWhatsApp, &row.OrgCurrency,
		&row.MinPriceCents, &row.AllSoldOut, &row.AllClosed, &row.TicketCount, &row.TicketsSold,
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
	rows, err := r.db.Pool.QueryContext(ctx, sales.LiveHoldsSQL(sales.HoldsFilter{CutoffExpr: "$2", EventExpr: "$1"}), eventID, cutoff)
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
		`+publicEventFrom("$9", "$1")+`
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
// for one Organization (upcoming and past), soonest first. now anchors both the
// hold-window cutoff for the sold-out aggregates and the Promotion windows the
// "from" price is built on.
func (r *Repository) ListDiscoverableEventsByOrganization(ctx context.Context, orgID string, now time.Time) ([]PublicEventRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom("$2", "$3")+`
		WHERE e.organization_id = $1
		  AND e.status = 'published'
		  AND e.discoverable = TRUE
		ORDER BY e.starts_at ASC, e.id ASC
	`, orgID, sales.HoldCutoff(now), now)
	if err != nil {
		return nil, err
	}
	return collectPublicEventRows(rows)
}

// GetPublishedEventBySlug loads a single published Event by Organization and Event
// slug for the Storefront event page. Discoverability is not required: a published
// Event is always reachable by direct link. now anchors both the hold-window
// cutoff for the sold-out aggregates and the Promotion windows the "from" price
// is built on.
func (r *Repository) GetPublishedEventBySlug(ctx context.Context, orgSlug, eventSlug string, now time.Time) (*PublicEventRow, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+publicEventColumns+`
		`+publicEventFrom("$3", "$4")+`
		WHERE o.slug = $1
		  AND e.slug = $2
		  AND e.status = 'published'
	`, orgSlug, eventSlug, sales.HoldCutoff(now), now)
	result, err := scanPublicEventRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}
