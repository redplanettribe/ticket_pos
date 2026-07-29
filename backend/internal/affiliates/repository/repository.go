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
	// ClickCount is the raw number of visits to the Event page through this link:
	// no dedup and no visitor identification, so a buyer returning counts again.
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

// GetByIDForEvent returns one of an Event's Affiliate Links with its attribution
// figures, or nil when the Event has no link by that id. Scoped to the Event so
// a link id from elsewhere is simply absent rather than reachable.
//
// It picks the row out of the Event's own list rather than repeating that
// query's attribution arithmetic: an Event carries a handful of links, and one
// definition of what a link's figures are is worth more than the row skipped.
func (r *Repository) GetByIDForEvent(ctx context.Context, eventID, linkID string) (*AffiliateLink, error) {
	links, err := r.ListByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if link.ID == linkID {
			return &link, nil
		}
	}
	return nil, nil
}

// UpdateNameAndActive writes an Affiliate Link's two mutable fields and returns
// the stored row. The code is not among them: it is generated once and travels
// in URLs that outlive any edit, so nothing here can move it.
//
// Returns nil when the Event has no link by that id.
func (r *Repository) UpdateNameAndActive(ctx context.Context, eventID, linkID, name string, active bool) (*AffiliateLink, error) {
	var updated AffiliateLink
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE affiliate_links
		SET name = $3, active = $4, updated_at = NOW()
		WHERE id = $2 AND event_id = $1
		RETURNING `+affiliateLinkColumns,
		eventID, linkID, name, active,
	).Scan(
		&updated.ID, &updated.EventID, &updated.OrganizationID, &updated.Name,
		&updated.Code, &updated.Active, &updated.ClickCount,
		&updated.CreatedAt, &updated.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// DeleteIfNoHistory removes an Affiliate Link only while it has none, and
// reports whether it removed anything.
//
// The history test lives inside the DELETE rather than in a read before it, so a
// click or a checkout landing between the two cannot slip a link out from under
// its own past. History is any click at all, any attributed Ticket Sale of any
// status — a reversed one still names the link that drove it — and any PENDING
// Payment, which is not history yet but may still become some: a checkout begun
// under the link can be approved a minute from now, and the delete must not race
// the sale it would produce.
//
// A Payment that never became a sale is deliberately NOT history. Abandoned and
// declined checkouts are the commonest thing on the internet, and a link that
// collected only those drove nothing worth keeping it for — the earlier "any
// Payment at all" test left every such link undeletable forever, which is
// broader than the rule the feature promises (#148).
//
// The FK is what makes that safe rather than orphaning: payments.affiliate_link_id
// is ON DELETE SET NULL (migration 037), so an expired or failed Payment outlives
// the link and simply stops naming it. That matters for one real case — an
// expired Payment can still be confirmed into a sale if the provider approved it
// late (ApprovePaymentAndCommitSale accepts 'expired' as well as 'pending', ADR
// 0013) — and the honest outcome there is a sale recorded unattributed, never a
// dangling id. ticket_sales keeps its RESTRICT-by-default FK: an actual sale's
// attribution is never rewritten, and the clause above is what guarantees no
// such row exists when the DELETE lands.
func (r *Repository) DeleteIfNoHistory(ctx context.Context, eventID, linkID string) (bool, error) {
	result, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM affiliate_links al
		WHERE al.id = $2
		  AND al.event_id = $1
		  AND al.click_count = 0
		  AND NOT EXISTS (SELECT 1 FROM ticket_sales ts WHERE ts.affiliate_link_id = al.id)
		  AND NOT EXISTS (
			SELECT 1 FROM payments p
			WHERE p.affiliate_link_id = al.id AND p.status = 'pending'
		  )
	`, eventID, linkID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RecordClick counts one visit to an Event page reached through the given code,
// resolved by the two Storefront slugs the visitor's URL carries.
//
// One statement, no read first: clicks arrive concurrently from every visitor a
// promoter reaches, and a read-modify-write would lose them. A code that matches
// nothing live — unknown, mistyped, on the wrong Event, or deactivated — updates
// no row and is not an error; the caller has nothing to tell the buyer either
// way.
func (r *Repository) RecordClick(ctx context.Context, organizationSlug, eventSlug, code string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE affiliate_links al
		SET click_count = al.click_count + 1, updated_at = NOW()
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		WHERE al.event_id = e.id
		  AND al.active
		  AND al.code = $3
		  AND e.slug = $2
		  AND o.slug = $1
	`, organizationSlug, eventSlug, code)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
