package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TicketTypePromotion is a Ticket Type's one Promotion slot (ADR 0021).
type TicketTypePromotion struct {
	ID                    string
	TicketTypeID          string
	PromotionalPriceCents int
	StartsAt              sql.NullTime
	EndsAt                time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// PromotionParams holds the writable fields of a Promotion. Both writes take the
// whole window and price: a Promotion's parts only mean anything together, so
// there is no partial edit of one.
type PromotionParams struct {
	PromotionalPriceCents int
	StartsAt              sql.NullTime
	EndsAt                time.Time
}

const promotionColumns = `
	id, ticket_type_id, promotional_price_cents, starts_at, ends_at, created_at, updated_at
`

func scanPromotion(row interface {
	Scan(dest ...any) error
}) (*TicketTypePromotion, error) {
	var p TicketTypePromotion
	if err := row.Scan(
		&p.ID, &p.TicketTypeID, &p.PromotionalPriceCents,
		&p.StartsAt, &p.EndsAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetPromotionByTicketTypeID loads a Ticket Type's Promotion, or nil when the
// slot is empty.
func (r *Repository) GetPromotionByTicketTypeID(ctx context.Context, ticketTypeID string) (*TicketTypePromotion, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+promotionColumns+`
		FROM ticket_type_promotions
		WHERE ticket_type_id = $1
	`, ticketTypeID)
	return scanPromotion(row)
}

// ListPromotionsByEventID returns the Promotions of an Event's Ticket Types
// keyed by Ticket Type ID, so a list response costs one extra query rather than
// one per row.
func (r *Repository) ListPromotionsByEventID(ctx context.Context, orgID, eventID string) (map[string]TicketTypePromotion, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT p.id, p.ticket_type_id, p.promotional_price_cents,
		       p.starts_at, p.ends_at, p.created_at, p.updated_at
		FROM ticket_type_promotions p
		JOIN ticket_types tt ON tt.id = p.ticket_type_id
		WHERE tt.event_id = $1 AND tt.organization_id = $2
	`, eventID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	promotions := make(map[string]TicketTypePromotion)
	for rows.Next() {
		var p TicketTypePromotion
		if err := rows.Scan(
			&p.ID, &p.TicketTypeID, &p.PromotionalPriceCents,
			&p.StartsAt, &p.EndsAt, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		promotions[p.TicketTypeID] = p
	}
	return promotions, rows.Err()
}

// CreatePromotion fills a Ticket Type's Promotion slot. The column's UNIQUE
// constraint is what makes the slot one, so a second insert fails here rather
// than racing past a read-then-write check in the service.
func (r *Repository) CreatePromotion(
	ctx context.Context,
	ticketTypeID string,
	params PromotionParams,
	now time.Time,
) (*TicketTypePromotion, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO ticket_type_promotions (
			ticket_type_id, promotional_price_cents, starts_at, ends_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $5)
		RETURNING `+promotionColumns+`
	`, ticketTypeID, params.PromotionalPriceCents, nullTime(params.StartsAt), params.EndsAt, now)
	return scanPromotion(row)
}

// UpdatePromotion replaces the price and window of an existing Promotion.
func (r *Repository) UpdatePromotion(
	ctx context.Context,
	ticketTypeID string,
	params PromotionParams,
	now time.Time,
) (*TicketTypePromotion, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE ticket_type_promotions
		SET
			promotional_price_cents = $2,
			starts_at = $3,
			ends_at = $4,
			updated_at = $5
		WHERE ticket_type_id = $1
		RETURNING `+promotionColumns+`
	`, ticketTypeID, params.PromotionalPriceCents, nullTime(params.StartsAt), params.EndsAt, now)
	return scanPromotion(row)
}

// DeletePromotion empties a Ticket Type's Promotion slot.
func (r *Repository) DeletePromotion(ctx context.Context, ticketTypeID string) error {
	result, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM ticket_type_promotions WHERE ticket_type_id = $1
	`, ticketTypeID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// IsUniqueViolation reports whether an error is Postgres' unique_violation,
// which the Promotion slot's UNIQUE constraint raises when a second Promotion is
// inserted concurrently with the first.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
