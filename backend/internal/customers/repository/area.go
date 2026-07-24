package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// TicketSaleLineRow is one Ticket Type and the quantity bought within a Ticket
// Sale, as the Customer Area shows it.
type TicketSaleLineRow struct {
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int    `json:"unit_price_cents"`
}

// TicketSaleRow is one of a Customer's Ticket Sales with everything the Customer
// Area needs to be meaningful at a glance: the Event and its date, the
// Organization that sold it, what was bought, and the Sale Confirmation
// reference the Customer can quote to a promoter.
type TicketSaleRow struct {
	ID               string
	ConfirmationRef  string
	SoldAt           time.Time
	Status           string
	AmountCents      int
	Currency         string
	Lines            []TicketSaleLineRow
	EventID          string
	EventName        string
	EventSlug        string
	EventStartsAt    sql.NullTime
	EventEndsAt      sql.NullTime
	EventTimezone    sql.NullString
	EventVenueName   sql.NullString
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
}

// ListTicketSalesForCustomer returns every Ticket Sale belonging to one
// Customer, across every Organization.
//
// customerID is the sole scope, and it comes from the Customer Session — never
// from anything in the request. There is deliberately no Organization, email, or
// customer parameter on this query: the only identifier it accepts is the one the
// caller proved they own, so there is no argument through which one Customer's
// purchase history could be aimed at another's.
//
// ticketSaleID, when non-empty, narrows further to that single sale. It is the
// Confirmation Link scope and can only ever shrink the result, never widen it.
//
// Ticket Sale Lines are aggregated per sale in a lateral subquery so a multi-line
// sale stays one row rather than fanning out. The rows come back newest purchase
// first; the service splits them into upcoming and past and orders each half by
// Event date, since that is what a Customer reads the list by.
func (r *Repository) ListTicketSalesForCustomer(ctx context.Context, customerID, ticketSaleID string) ([]TicketSaleRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			ts.id,
			ts.confirmation_ref,
			ts.sold_at,
			ts.status,
			lines.amount_cents,
			lines.ticket_types,
			org.currency,
			e.id, e.name, e.slug, e.starts_at, e.ends_at, e.timezone, e.venue_name,
			org.id, org.name, org.slug
		FROM ticket_sales ts
		JOIN events e ON e.id = ts.event_id
		JOIN organizations org ON org.id = ts.organization_id
		JOIN LATERAL (
			SELECT
				COALESCE(SUM(tsl.quantity * tsl.unit_price_cents), 0) AS amount_cents,
				COALESCE(
					json_agg(
						json_build_object(
							'ticket_type_name', tt.name,
							'quantity', tsl.quantity,
							'unit_price_cents', tsl.unit_price_cents
						)
						ORDER BY tt.sort_order, tt.name
					),
					'[]'::json
				) AS ticket_types
			FROM ticket_sale_lines tsl
			JOIN ticket_types tt ON tt.id = tsl.ticket_type_id
			WHERE tsl.ticket_sale_id = ts.id
		) lines ON TRUE
		WHERE ts.customer_id = $1
		  AND ($2 = '' OR ts.id = $2::uuid)
		ORDER BY ts.sold_at DESC, ts.id DESC
	`, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TicketSaleRow, 0)
	for rows.Next() {
		var s TicketSaleRow
		var linesJSON []byte
		if err := rows.Scan(
			&s.ID,
			&s.ConfirmationRef,
			&s.SoldAt,
			&s.Status,
			&s.AmountCents,
			&linesJSON,
			&s.Currency,
			&s.EventID, &s.EventName, &s.EventSlug, &s.EventStartsAt, &s.EventEndsAt, &s.EventTimezone, &s.EventVenueName,
			&s.OrganizationID, &s.OrganizationName, &s.OrganizationSlug,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(linesJSON, &s.Lines); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
