// Package repository provides hand-written SQL data access for Affiliate Links.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peter/ticket_pos/backend/internal/platform"
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
	CreatedAt  time.Time
	UpdatedAt  time.Time
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

// ListByEventID returns an Event's Affiliate Links, newest first.
func (r *Repository) ListByEventID(ctx context.Context, eventID string) ([]AffiliateLink, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+affiliateLinkColumns+`
		FROM affiliate_links
		WHERE event_id = $1
		ORDER BY created_at DESC, id DESC
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
		); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
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
