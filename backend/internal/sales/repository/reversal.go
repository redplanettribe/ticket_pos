package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CustomerTicketSale is one Ticket Sale as the Customer-initiated Sale Reversal
// path needs to see it: enough to decide whether it may be reversed, and enough
// to reverse it and tell the buyer afterwards.
//
// It is not the Customer Area's row and must not become one. That row exists to
// be rendered; this one exists to be judged, so it carries the Payment Method
// and the Payment's client transaction id — facts a buyer is never shown — and
// none of the presentation the Area needs.
type CustomerTicketSale struct {
	ID              string
	EventID         string
	OrganizationID  string
	EventName       string
	ConfirmationRef string
	// Channel is the Sales Channel: only 'online' can be reversed by a Customer.
	Channel string
	// Status is 'active' or 'reversed'.
	Status string
	// PaymentMethod is how the sale was settled: 'free' when the platform settled
	// a zero-total checkout itself (ADR 0017), otherwise the Payment Provider that
	// collected the money. Null on channels that carry none.
	PaymentMethod sql.NullString
	// SoldAt is the Payment approval instant on an Online Sale — the sale commits
	// in the transaction that approves the Payment — so it opens the Reversal
	// Window without a second lookup.
	SoldAt time.Time
	// EventStartsAt closes the Reversal Window when it falls before the
	// Ecuadorian cutoff. Nullable in the schema; an Online Sale always has one,
	// since only a published Event is sellable and publishing requires a start.
	EventStartsAt sql.NullTime
	// ClientTransactionID is the Payment's id under OUR namespace, which is what
	// PaymentProvider.Reverse is keyed on. Empty on a sale with no Payment row —
	// every Online Sale has one, so this is empty only on the channels that
	// cannot be reversed here anyway.
	ClientTransactionID string
	// The buyer as this sale snapshotted them, for the void notice.
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
}

// GetCustomerTicketSale loads one Ticket Sale owned by one Customer.
//
// customerID is not a filter to be relaxed later: it is the authorization. It
// comes from the Customer Session and the sale is invisible to this query under
// any other Customer, so "this sale belongs to somebody else" and "there is no
// such sale" are the same absence here rather than two branches a caller could
// get the wrong way round.
//
// Ownership is the sale's own customer_id, written when the sale was recorded.
// Notably it is NOT the email on the sale: a Customer is keyed on their address
// as normalised (ADR 0011), and matching on the snapshot would let a later
// profile edit change who may reverse an old purchase.
func (r *Repository) GetCustomerTicketSale(ctx context.Context, customerID, saleID string) (*CustomerTicketSale, error) {
	var out CustomerTicketSale
	var clientTransactionID sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			ts.id, ts.event_id, ts.organization_id, e.name, ts.confirmation_ref,
			ts.channel, ts.status, ts.payment_method, ts.sold_at, e.starts_at,
			p.client_transaction_id,
			ts.customer_email, ts.customer_first_name, ts.customer_last_name
		FROM ticket_sales ts
		JOIN events e ON e.id = ts.event_id
		LEFT JOIN payments p ON p.ticket_sale_id = ts.id
		WHERE ts.id = $1 AND ts.customer_id = $2
	`, saleID, customerID).Scan(
		&out.ID, &out.EventID, &out.OrganizationID, &out.EventName, &out.ConfirmationRef,
		&out.Channel, &out.Status, &out.PaymentMethod, &out.SoldAt, &out.EventStartsAt,
		&clientTransactionID,
		&out.CustomerEmail, &out.CustomerFirstName, &out.CustomerLastName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.ClientTransactionID = clientTransactionID.String
	return &out, nil
}
