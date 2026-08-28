package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The buyer's read of their own documents (#475, ADR 0060): every query
// here joins the document to its Ticket Sale AND to the Customer who owns
// that Sale, so a row is returned only to the Customer the Sale belongs to
// now. A Sale Re-addressing moves the Sale's Customer, and the documents go
// with the Sale; the previous address loses them, as it loses the Sale.

// ListSaleDocumentsForCustomer reads the documents of one Ticket Sale in
// the order they came to exist, provided the Sale belongs to the Customer;
// an empty list otherwise, which is also the answer for a Sale that owes
// none or does not exist — one answer, so ids cannot be probed.
func (r *Repository) ListSaleDocumentsForCustomer(ctx context.Context, customerID, ticketSaleID string) ([]InvoiceRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE i.ticket_sale_id = $2 AND ts.customer_id = $1
		ORDER BY i.created_at, i.id
	`, customerID, ticketSaleID)
	if err != nil {
		return nil, fmt.Errorf("list sale documents for customer: %w", err)
	}
	defer rows.Close()
	out := []InvoiceRow{}
	for rows.Next() {
		row, err := scanInvoice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan sale document: %w", err)
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// GetSaleDocumentForCustomer reads one document by id, provided it is the
// named Ticket Sale's and that Sale is the Customer's; nil otherwise, for
// the reason above. The row comes whole — lines, additional fields and
// attempts, as GetInvoice reads it — because it is what the buyer's
// downloads are served from, and the RIDE (#497, ADR 0062) draws the lines
// and fields: the same row the operator's RIDE is rendered from, so the two
// are the same bytes.
func (r *Repository) GetSaleDocumentForCustomer(ctx context.Context, customerID, ticketSaleID, invoiceID string) (*InvoiceRow, error) {
	row, err := scanInvoice(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE i.id = $3 AND i.ticket_sale_id = $2 AND ts.customer_id = $1
	`, customerID, ticketSaleID, invoiceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sale document for customer: %w", err)
	}
	if err := r.loadInvoiceChildren(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}
