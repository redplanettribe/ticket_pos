package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What settling a reversed Sale's documents needs of storage (#476, parent
// #471, ADR 0060), all of it inside the CALLER'S transaction — the one that
// has just flipped the Ticket Sale to reversed: the Sale's own Sale
// Invoices, locked so that a Drainer round cannot sign one while the
// reversal decides to withdraw it, and the one write that withdraws a
// document never sent. Owing the Credit Note itself is OweInvoice, which
// already takes the transaction.

// LockSaleDocumentsForReversal reads every document of one Ticket Sale —
// its Sale Invoices and, since a reissue may be in flight (#484), their
// Credit Notes — in full, lines included since a Credit Note copies them,
// oldest first, under FOR UPDATE, in tx. The lock is what serialises the reversal against
// the Drainer: a round that holds the row's claim still has to UPDATE it to
// sign it, and that UPDATE waits on this lock and then finds the row
// withdrawn (SignOwedInvoice is guarded on the row being unsigned, and a
// withdrawn row is never signed because it is never claimed again).
//
// Ordinarily one row; the shape allows for what the schema allows.
func (r *Repository) LockSaleDocumentsForReversal(ctx context.Context, tx *sql.Tx, ticketSaleID string) ([]InvoiceRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE i.ticket_sale_id = $1
		ORDER BY i.created_at, i.id
		FOR UPDATE OF i
	`, ticketSaleID)
	if err != nil {
		return nil, fmt.Errorf("lock sale invoices for reversal: %w", err)
	}
	defer rows.Close()
	out, err := scanInvoices(rows)
	if err != nil {
		return nil, fmt.Errorf("lock sale invoices for reversal: %w", err)
	}
	for i := range out {
		if err := loadInvoiceChildrenFrom(ctx, tx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// WithdrawUnsignedInvoice records that a document never sent never will
// be (#476): its Sale was reversed first — the Sale Invoice, or, when a
// reissue was in flight, its unsent Credit Note and corrected factura too
// (#484). Written in tx, guarded on the
// row being unsigned — a withdrawn document has consumed no number and
// holds no bytes, which is what the state means (migration 098) — and it
// leaves the queue: next_attempt_at cleared, attention_since cleared with
// the state. A row already signed is left exactly as it is and reported
// so, because the Drainer got there first and the document is the Tax
// Authority's to answer now; the caller credits it instead.
func (r *Repository) WithdrawUnsignedInvoice(ctx context.Context, tx *sql.Tx, invoiceID string, at time.Time) (withdrawn bool, err error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			status = $2,
			next_attempt_at = NULL,
			attention_since = NULL,
			updated_at = $3
		WHERE id = $1 AND signed_xml IS NULL
	`, invoiceID, invoicing.InvoiceStatusWithdrawn, at)
	if err != nil {
		return false, fmt.Errorf("withdraw invoice: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("withdraw invoice: %w", err)
	}
	return n == 1, nil
}
