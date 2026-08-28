package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// PaidOnlineSaleInTx describes an ALREADY RECORDED paid Online Sale for the
// invoicing seam, in the caller's transaction, in exactly the shape the
// checkout commit hands over the moment it records one (paidOnlineSaleOf):
// the buyer as the Sale snapshotted them, the Event's name, one line per
// Ticket Sale Line with the Ticket Type's name and the buyer price it was
// sold at, and the amount the approved Payment collected. It is the Sale
// Invoice Backfill's way in (#508, ADR 0064): an operator owing a document
// to an Uninvoiced House Sale must describe the sale as it was transacted,
// and the sale-commit spine's builder is the one description there is.
//
// THE AMOUNT IS THE PAYMENT'S, NOT THE LINES' SUM. At checkout the two are
// one fact written twice; here they may have drifted — a line repriced by
// hand, a sale the platform cannot invoice as it was transacted — and the
// builder on the far side refuses when they disagree. Reading the Payment
// is what makes that refusal a fact about the sale rather than a tautology.
// The Payment is the first approved one above zero, exactly as the
// candidate predicate on the invoicing side reads it.
//
// The names are read now, in the transaction, so the document's lines say
// what the catalog calls the Ticket Type today — the same rule the checkout
// applies at its own instant. Whether the sale QUALIFIES is not decided
// here: the caller has already locked the row and asked its own predicate.
func (r *Repository) PaidOnlineSaleInTx(ctx context.Context, tx *sql.Tx, ticketSaleID string) (sales.PaidOnlineSale, error) {
	var (
		out                sales.PaidOnlineSale
		taxType, taxNumber sql.NullString
		locale, provider   sql.NullString
		amountCents        int64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT ts.id, ts.organization_id, ts.event_id, e.name, ts.confirmation_ref,
		       ts.customer_email, ts.customer_first_name, ts.customer_last_name,
		       ts.customer_tax_id_type, ts.customer_tax_id_number, ts.locale, ts.payment_method,
		       COALESCE((
		           SELECT p.amount_cents FROM payments p
		           WHERE p.ticket_sale_id = ts.id AND p.status = 'approved' AND p.amount_cents > 0
		           ORDER BY p.created_at ASC, p.id ASC
		           LIMIT 1), 0)
		FROM ticket_sales ts
		JOIN events e ON e.id = ts.event_id
		WHERE ts.id = $1
	`, ticketSaleID).Scan(
		&out.TicketSaleID, &out.OrganizationID, &out.EventID, &out.EventName, &out.ConfirmationRef,
		&out.Buyer.Email, &out.Buyer.FirstName, &out.Buyer.LastName,
		&taxType, &taxNumber, &locale, &provider,
		&amountCents,
	); err != nil {
		return sales.PaidOnlineSale{}, fmt.Errorf("read paid online sale: %w", err)
	}
	if taxType.Valid && taxNumber.Valid {
		out.Buyer.TaxID = platform.SaleTaxID{Type: taxType.String, Number: taxNumber.String}
	}
	out.Locale = locale.String
	out.PaymentProvider = provider.String
	out.AmountCents = int(amountCents)

	// The lines in the order they were written, which is cart order.
	rows, err := tx.QueryContext(ctx, `
		SELECT tt.name, l.quantity, l.unit_price_cents
		FROM ticket_sale_lines l
		JOIN ticket_types tt ON tt.id = l.ticket_type_id
		WHERE l.ticket_sale_id = $1
		ORDER BY l.created_at ASC, l.id ASC
	`, ticketSaleID)
	if err != nil {
		return sales.PaidOnlineSale{}, fmt.Errorf("read paid online sale lines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line sales.PaidOnlineSaleLine
		if err := rows.Scan(&line.TicketTypeName, &line.Quantity, &line.UnitPriceCents); err != nil {
			return sales.PaidOnlineSale{}, fmt.Errorf("scan paid online sale line: %w", err)
		}
		out.Lines = append(out.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return sales.PaidOnlineSale{}, fmt.Errorf("read paid online sale lines: %w", err)
	}
	return out, nil
}
