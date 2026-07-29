// Package repository provides hand-written SQL data access for Affiliate Links.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// AffiliateLink is one named, trackable link to an Event's page.
type AffiliateLink struct {
	ID             string
	EventID        string
	OrganizationID string
	Name           string
	Code           string
	Active         bool
	// ClickCount is carried from the row so later tickets can show it without
	// reshaping the read; nothing counts into it yet.
	ClickCount int64
	// SalesCount and NetProceedsCents are this link's Affiliate Attribution
	// figures over ACTIVE attributed sales only: a Sale Reversal by any route
	// drops the sale from both, and a free attributed sale counts with nothing
	// beside it. Read alongside the row rather than stored on it.
	SalesCount       int
	NetProceedsCents int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// LinkTarget is where an Event's Affiliate Links point: the two slugs that make
// up the Storefront Event page path.
type LinkTarget struct {
	OrganizationSlug string
	EventSlug        string
}

// Repository provides SQL access for Affiliate Links.
type Repository struct {
	db *platform.DB
}

// New returns a repository backed by the given database pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

const affiliateLinkColumns = `id, event_id, organization_id, name, code, active, click_count, created_at, updated_at`

// prefixedColumns qualifies the column list for the reads that join: the list
// query aliases the table so it can hang the attribution figures off it.
func prefixedColumns(alias string) string {
	parts := strings.Split(affiliateLinkColumns, ", ")
	for i, part := range parts {
		parts[i] = alias + "." + part
	}
	return strings.Join(parts, ", ")
}

// GetLinkTarget returns the Storefront path parts for an Event in the given
// Organization, or nil when the Organization has no such Event. It doubles as
// the Event existence check every Affiliate Link operation starts from.
func (r *Repository) GetLinkTarget(ctx context.Context, organizationID, eventID string) (*LinkTarget, error) {
	var target LinkTarget
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT o.slug, e.slug
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		WHERE e.id = $1 AND e.organization_id = $2
	`, eventID, organizationID).Scan(&target.OrganizationSlug, &target.EventSlug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}

// ErrCodeTaken reports that the code already exists on the Event. The service
// retries with a fresh code rather than surfacing it.
var ErrCodeTaken = errors.New("affiliate link code taken")

// Create inserts an Affiliate Link. A code that collides within the Event comes
// back as ErrCodeTaken so the caller can generate another.
func (r *Repository) Create(ctx context.Context, link AffiliateLink) (*AffiliateLink, error) {
	var created AffiliateLink
	err := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO affiliate_links (event_id, organization_id, name, code)
		VALUES ($1, $2, $3, $4)
		RETURNING `+affiliateLinkColumns,
		link.EventID, link.OrganizationID, link.Name, link.Code,
	).Scan(
		&created.ID, &created.EventID, &created.OrganizationID, &created.Name,
		&created.Code, &created.Active, &created.ClickCount,
		&created.CreatedAt, &created.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return nil, ErrCodeTaken
	}
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// FindActiveLinkIDByCode returns the id of the Event's ACTIVE Affiliate Link
// carrying this code, or "" when the Event has no live link by that name — an
// unknown code, a mistyped one, or one that has since been deactivated. All
// three are the same answer here: nobody to attribute to. Never an error,
// because a checkout is never refused over a ref.
func (r *Repository) FindActiveLinkIDByCode(ctx context.Context, eventID, code string) (string, error) {
	var id string
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id FROM affiliate_links
		WHERE event_id = $1 AND code = $2 AND active
	`, eventID, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListByEventID returns an Event's Affiliate Links, newest first, each with its
// Affiliate Attribution figures.
//
// The figures are computed over the link's ACTIVE attributed Ticket Sales:
// a Sale Reversal by any route leaves the sale behind but takes it out of both
// columns, exactly as it drops out of the Event's Net Proceeds and the
// Organization's Withdrawable Balance. A free attributed sale counts in
// sales_count and adds nothing to the money, which is the honest answer rather
// than a hidden one. The channel filter restates the invariant that only an
// Online Sale can ever be attributed; nothing else can put an id on the column.
func (r *Repository) ListByEventID(ctx context.Context, eventID string) ([]AffiliateLink, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+prefixedColumns("al")+`,
		       COALESCE(stats.sales_count, 0),
		       COALESCE(stats.net_proceeds_cents, 0)
		FROM affiliate_links al
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS sales_count,
			       COALESCE(SUM(net.net_proceeds_cents), 0) AS net_proceeds_cents
			FROM ticket_sales ts
			JOIN LATERAL (
				SELECT COALESCE(SUM(`+sales.LineNetProceedsSQL+`), 0) AS net_proceeds_cents
				FROM ticket_sale_lines tsl
				WHERE tsl.ticket_sale_id = ts.id
			) net ON TRUE
			WHERE ts.affiliate_link_id = al.id
			  AND ts.status = 'active'
			  AND ts.channel = 'online'
		) stats ON TRUE
		WHERE al.event_id = $1
		ORDER BY al.created_at DESC, al.id DESC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := make([]AffiliateLink, 0)
	for rows.Next() {
		var link AffiliateLink
		if err := rows.Scan(
			&link.ID, &link.EventID, &link.OrganizationID, &link.Name,
			&link.Code, &link.Active, &link.ClickCount,
			&link.CreatedAt, &link.UpdatedAt,
			&link.SalesCount, &link.NetProceedsCents,
		); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
