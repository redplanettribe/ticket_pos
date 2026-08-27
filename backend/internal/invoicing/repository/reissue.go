package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What the Sale Invoice Reissue needs of storage (#483, parent #478, ADR
// 0061): ONE transaction that reads the facts the reissue is decided on
// under the locks a reversal takes, hands them to the service to decide,
// and — if it decides to — owes the Credit Note and the corrected factura
// together. Nothing about the Sale is written here: the Sale row is locked
// to be READ, so that the decision and the reversal it might race are
// serialised, and released as it was.
//
// THE LOCK ORDER IS THE REVERSAL'S. A Sale Reversal takes the ticket_sales
// row FOR UPDATE first and the Sale's invoices FOR UPDATE after
// (sales/repository/sale_reverse.go, LockSaleDocumentsForReversal); this
// transaction takes them in the same order, so the two can only queue
// behind each other and never deadlock. Whichever commits first, the other
// decides on what it then finds: a reissue behind a reversal finds the Sale
// reversed and refuses; a reversal behind a reissue finds the superseded
// factura credited and the corrected one owed, and withdraws the latter as
// it withdraws any unsent document.

// ReissueSaleFacts is what the reissue is decided on beyond the factura's
// own row, read under the Sale's lock.
type ReissueSaleFacts struct {
	// SaleStatus is the Ticket Sale's status as it stands: committed, or
	// reversed. "" on a manual document, which has no Sale.
	SaleStatus string
	// SaleEmail is the address the Sale carries at this moment — after any
	// Sale Re-addressing — and what the corrected factura is issued to.
	SaleEmail string
	// ReissueInFlight says another reissue on the Sale has not settled: a
	// reissue Credit Note, or a Sale Invoice that supersedes another, is
	// still owed, pending or parked.
	ReissueInFlight bool
	// CreditedByAuthorized says an authorized Credit Note already stands
	// against the factura.
	CreditedByAuthorized bool
}

// ReissueSaleInvoice runs the reissue's transaction. decide is called with
// the factura as locked and the Sale facts; it returns the Credit Note and
// the corrected Sale Invoice to owe, or a domain error to refuse with, in
// which case nothing is written. It returns "" and no error when no
// document has the id.
func (r *Repository) ReissueSaleInvoice(ctx context.Context, facturaID string, decide func(factura *InvoiceRow, facts ReissueSaleFacts) (note, corrected invoicing.Invoice, err error)) (correctedID string, err error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin reissue: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The Sale first, and FOR UPDATE: the reversal's order.
	var ticketSaleID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT ticket_sale_id FROM invoicing_invoices WHERE id = $1`, facturaID).Scan(&ticketSaleID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reissue: read sale of invoice: %w", err)
	}
	var facts ReissueSaleFacts
	if ticketSaleID.Valid {
		if err := tx.QueryRowContext(ctx, `
			SELECT status, customer_email FROM ticket_sales WHERE id = $1 FOR UPDATE
		`, ticketSaleID.String).Scan(&facts.SaleStatus, &facts.SaleEmail); err != nil {
			return "", fmt.Errorf("reissue: lock sale: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT
				EXISTS (
					SELECT 1 FROM invoicing_invoices x
					WHERE x.ticket_sale_id = $1
					  AND x.status IN ('owed', 'pending', 'needs_attention')
					  AND ((x.kind = 'sale' AND x.supersedes_invoice_id IS NOT NULL)
					       OR (x.kind = 'credit_note' AND x.credit_note_reason = $3))),
				EXISTS (
					SELECT 1 FROM invoicing_invoices c
					WHERE c.credits_invoice_id = $2 AND c.status = 'authorized')
		`, ticketSaleID.String, facturaID, invoicing.CreditNoteReasonReissue).Scan(&facts.ReissueInFlight, &facts.CreditedByAuthorized); err != nil {
			return "", fmt.Errorf("reissue: read sale facts: %w", err)
		}
	}

	// Then the factura, in full, FOR UPDATE.
	factura, err := scanInvoice(tx.QueryRowContext(ctx, `SELECT `+invoiceColumns+invoiceFrom+` WHERE i.id = $1 FOR UPDATE OF i`, facturaID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reissue: lock invoice: %w", err)
	}
	if err := loadInvoiceChildrenFrom(ctx, tx, factura); err != nil {
		return "", err
	}

	note, corrected, err := decide(factura, facts)
	if err != nil {
		return "", err
	}
	if _, err := r.OweInvoice(ctx, tx, note); err != nil {
		return "", err
	}
	correctedID, err = r.OweInvoice(ctx, tx, corrected)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit reissue: %w", err)
	}
	return correctedID, nil
}
